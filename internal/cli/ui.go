package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/store"
	"github.com/cdescobedo/trenvy/internal/ui"
)

func newUiCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "ui",
		Short: "Manage this project's variables across every profile, interactively",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}

			mx, err := matrixFromManifest(app)
			if err != nil {
				return err
			}
			ps := ui.Problems(mx, outputStates(opts, app, mx))
			m := ui.New(app.Entry.ID, mx, ps)

			final, err := opts.RunUI(m, func(name, profile string) (string, error) {
				s, err := app.store()
				if err != nil {
					return "", err
				}
				return s.Get(app.Cfg.Keyring.ServiceName(app.Manifest.Project.ID, profile, name))
			})
			if err != nil {
				return err
			}
			if !final.Saved() {
				fmt.Fprintln(opts.Stdout, "no changes written")
				return nil
			}
			return commitStaged(opts, app, final.Staged())
		},
	}
}

// commitStaged writes w's staged changes: keyring writes, then keyring
// deletes, then the manifest. The manifest is the only inventory of which
// keyring items exist, so a failure before it is saved leaves at worst a
// keyring item nothing references -- an unreferenced secret is inert,
// because nothing ever looks for it. The reverse order would risk the
// opposite: a manifest entry declaring a secret that was never stored, or a
// manifest missing an entry whose secret still sits in the keyring, either
// of which is data loss or an item nothing can ever find again.
//
// staged changes carry no cross-run identity -- internal/ui holds them only
// in memory, and the terminal session that produced them has already ended
// by the time this runs (RunUI returned once w was pressed). So a failure
// partway through cannot be resumed: there is no session left to press w
// again in. What is reported is exactly how much of THIS run already
// landed; everything else that was staged is gone, and the only way back is
// a fresh trenvy ui session redoing it by hand.
func commitStaged(opts *Options, app *appContext, staged []ui.Change) error {
	if len(staged) == 0 {
		return nil
	}

	type keyOp struct {
		del     bool
		service string
		value   string
	}
	var writes, deletes []keyOp

	for _, c := range staged {
		switch c.Kind {
		case ui.ChangeSetSecret:
			writes = append(writes, keyOp{
				service: app.Cfg.Keyring.ServiceName(app.Manifest.Project.ID, c.Profile, c.Name),
				value:   c.Value,
			})
		case ui.ChangeDeleteVar:
			// Deleting a variable must remove its keyring item under EVERY
			// declared profile, so this loop uses its own p when building
			// each service name -- never app.service(c.Name), which binds
			// only the active profile and would delete that one profile's
			// item once per profile while leaving every other profile's
			// secret orphaned in the keyring with no manifest entry left to
			// find it by.
			//
			// Presence is read from the manifest as loaded, before the
			// mutation loop below runs, so a var deleted in the same batch
			// it was added in (staged as both add-var and delete-var) is
			// correctly seen as never having existed, never as secret.
			if v, ok := app.Manifest.Vars[c.Name]; ok && v.Secret {
				for _, p := range app.Manifest.Project.Profiles {
					deletes = append(deletes, keyOp{
						del:     true,
						service: app.Cfg.Keyring.ServiceName(app.Manifest.Project.ID, p, c.Name),
					})
				}
			}
		}
	}

	// Stored and deleted are counted separately, never folded into one
	// number: a blurred "applied 3 of 5" cannot tell a reader whether the
	// missing two are secrets never stored (something still needs a value)
	// or secrets never removed (a credential the user meant to revoke is
	// still live) -- and those two states call for opposite next steps. A
	// user reading this message is deciding what to do about their secrets
	// right now, and a count that hides which kind of operation stalled
	// gives back exactly what reporting partial progress was for.
	storedTotal, deletedTotal := len(writes), len(deletes)
	storedApplied, deletedApplied := 0, 0
	if storedTotal+deletedTotal > 0 {
		s, err := app.store()
		if err != nil {
			return err
		}
		for _, op := range writes {
			if err := s.Set(op.service, op.value); err != nil {
				return partialCommitError("store", op.service, err,
					storedApplied, storedTotal, deletedApplied, deletedTotal)
			}
			storedApplied++
		}
		for _, op := range deletes {
			if err := s.Delete(op.service); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					// A declared secret that was never actually stored under
					// this profile is the ordinary case, not a failure, and
					// must not inflate the applied count.
					continue
				}
				return partialCommitError("delete", op.service, err,
					storedApplied, storedTotal, deletedApplied, deletedTotal)
			}
			deletedApplied++
		}
	}

	// Only now does the manifest change, and only in memory -- Validate runs
	// before anything reaches disk, so a rejected manifest cannot leave a
	// half-applied shape behind.
	for _, c := range staged {
		switch c.Kind {
		case ui.ChangeSetValue:
			// A per-profile override, never the shared Value: editing one
			// cell in the matrix must not change what every other profile
			// resolves to, and Classify already reads Profiles[profile]
			// before Value for exactly this reason.
			v := app.Manifest.Vars[c.Name]
			if v.Profiles == nil {
				v.Profiles = map[string]string{}
			}
			v.Profiles[c.Profile] = c.Value
			app.Manifest.Vars[c.Name] = v
		case ui.ChangeAddVar:
			if _, exists := app.Manifest.Vars[c.Name]; !exists {
				app.Manifest.Vars[c.Name] = manifest.Var{}
			}
		case ui.ChangeDeleteVar:
			delete(app.Manifest.Vars, c.Name)
		case ui.ChangeSetSecret:
			v := app.Manifest.Vars[c.Name]
			v.Secret = true
			v.Value = nil
			v.Profiles = nil
			app.Manifest.Vars[c.Name] = v
		}
	}

	if err := app.Manifest.Validate(); err != nil {
		if progress := commitProgress(storedApplied, storedTotal, deletedApplied, deletedTotal); progress != "" {
			return fmt.Errorf("%w\n  %s; the manifest was not saved\n"+
				"  everything else staged this session is gone -- open trenvy ui again and redo it",
				err, progress)
		}
		return err
	}

	if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
		return err
	}

	fmt.Fprintf(opts.Stdout, "saved %d changes\n", len(staged))
	return nil
}

// commitProgress renders how far a commit got as "stored X of Y secrets,
// deleted A of B keyring items", omitting whichever clause the batch never
// contained at all: a batch with no deletions in it reporting "deleted 0 of
// 0" would be noise at the exact moment a reader needs a message they can
// trust. Returns "" when the batch touched the keyring not at all.
func commitProgress(storedApplied, storedTotal, deletedApplied, deletedTotal int) string {
	var parts []string
	if storedTotal > 0 {
		parts = append(parts, fmt.Sprintf("stored %d of %d secrets", storedApplied, storedTotal))
	}
	if deletedTotal > 0 {
		parts = append(parts, fmt.Sprintf("deleted %d of %d keyring items", deletedApplied, deletedTotal))
	}
	return strings.Join(parts, ", ")
}

// partialCommitError reports a keyring write or delete that failed partway
// through committing staged ui changes, in the shape this codebase's other
// partial-failure errors (edit's applyRows, unset, profile rm, project rm)
// all use: name the item, say how far the run got, say the manifest was not
// saved, name the way back.
//
// The session that staged these changes has already ended by the time this
// runs, so "open trenvy ui again" is never a resume -- it is a fresh session
// that has to redo everything this run did not finish.
func partialCommitError(verb, service string, err error, storedApplied, storedTotal, deletedApplied, deletedTotal int) error {
	return fmt.Errorf(
		"%s %s: %w\n  %s; the manifest was not saved\n"+
			"  everything else staged this session is gone -- open trenvy ui again and redo it",
		verb, service, err, commitProgress(storedApplied, storedTotal, deletedApplied, deletedTotal))
}

// matrixFromManifest loads the presence-aware matrix behind the ui command.
//
// Presence must come from Has, never Get: a Get can raise a macOS
// authorization prompt, and this loop touches every secret for every
// profile -- a Get here would mean one prompt per secret per profile on
// every launch, the thing that makes a tool nobody opens.
//
// The loop uses its own profile p when building each service name, never
// app.service(name) -- app.service binds the ACTIVE profile, so calling it
// here would report the active profile's presence for every column instead
// of each column's own (the same hazard set.go's profile-rm sweep and
// profile.go's rm guard against).
func matrixFromManifest(app *appContext) (*ui.Matrix, error) {
	hasSecrets := false
	for _, v := range app.Manifest.Vars {
		if v.Secret {
			hasSecrets = true
			break
		}
	}
	if !hasSecrets {
		return ui.NewMatrix(app.Manifest, nil), nil
	}

	s, err := app.store()
	if err != nil {
		return nil, err
	}

	stored := make(map[string]map[string]bool, len(app.Manifest.Vars))
	for name, v := range app.Manifest.Vars {
		if !v.Secret {
			continue
		}
		row := make(map[string]bool, len(app.Manifest.Project.Profiles))
		for _, p := range app.Manifest.Project.Profiles {
			ok, err := s.Has(app.Cfg.Keyring.ServiceName(app.Manifest.Project.ID, p, name))
			if err != nil {
				return nil, err
			}
			row[p] = ok
		}
		stored[name] = row
	}

	return ui.NewMatrix(app.Manifest, stored), nil
}

// outputStates computes each output's render-freshness label for the
// Overview tab, reusing status.go's classifyOutput -- the same guard.Inspect
// decision status.go's own describeOutput renders -- rather than a second
// implementation of freshness that could disagree with it. It switches on
// classifyOutput's typed outputState, never on describeOutput's rendered
// prose: parsing "not rendered" or "unknown (--quick)" as if they were a
// stable interface is exactly how this function and ui.Problems went out of
// sync (twice, in one task) before classifyOutput existed.
//
// Whether a required variable is missing under the active profile is read
// off mx itself (already built, and already exercised by internal/ui's own
// tests) instead of re-deriving status.go's inline missing-count loop, which
// is not itself a reusable function -- duplicating it here would be exactly
// the second implementation the guard-reuse rule warns against.
//
// quick is always true here, unlike status's own default: detecting a keyring
// change requires reading the secret's value, and ui opens eagerly on every
// launch, over every output, not on a deliberate ask the way `status`
// (without --quick) is. Passing false here would mean the load path this
// task is scoped to auditing for Get calls turns around and calls Get anyway,
// once per secret an output references -- the exact prompt storm Has exists
// to avoid.
func outputStates(opts *Options, app *appContext, mx *ui.Matrix) map[string]string {
	missing := false
	for _, name := range mx.Names {
		if mx.Cell(name, app.Profile.Name) == ui.CellMissing {
			missing = true
			break
		}
	}

	states := make(map[string]string, len(app.Manifest.Outputs))
	for name, o := range app.Manifest.Outputs {
		switch st := classifyOutput(opts, app, name, o, missing, true); st {
		case outputFresh:
			states[name] = ui.StateFresh
		case outputAbsent:
			states[name] = ui.StateAbsent
		case outputStale:
			states[name] = ui.StateStale
		case outputModified:
			states[name] = ui.StateModified
		case outputForeign:
			states[name] = ui.StateForeign
		case outputSecretsMissing, outputQuickSkipped, outputErrored:
			// All three mean "not resolved to an answer", for different
			// reasons -- a missing secret, a deliberate --quick-style skip,
			// or a read failure already logged to stderr. ui.Problems' Fix
			// for StateUnknown names `trenvy status`, which is the right
			// next step for all three.
			states[name] = ui.StateUnknown
		default:
			// classifyOutput's outputState has exactly the cases above. A
			// default that silently mapped an unhandled one to "" (or to
			// StateFresh, or to StateUnknown) would make this exhaustiveness
			// check pointless: a new case added there and forgotten here
			// would either suppress a real problem or manufacture a
			// permanent false one, and nothing would say which. Panicking is
			// safe here specifically because the only way to reach it is a
			// programming error in this same package, not anything a project
			// or a user can trigger.
			panic(fmt.Sprintf("ui: outputStates: unhandled output state %d for %q", st, name))
		}
	}
	return states
}
