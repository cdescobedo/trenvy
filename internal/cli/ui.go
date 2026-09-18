package cli

import (
	"errors"
	"fmt"
	"sort"
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

			// Loaded once, outside the loop below: the switcher lists what was
			// registered when the command started, and a registry change made
			// from another session mid-session is only ever seen the next time
			// `trenvy ui` runs, the same staleness every other command here
			// already accepts for a config loaded once at the top of RunE.
			reg, err := config.LoadRegistry()
			if err != nil {
				return err
			}
			projects := registryIDs(reg)

			// One iteration per project the switcher sends the session to. Each
			// one runs to completion (RunUI returns) before the next begins --
			// the previous project's model has already ended by then, exactly
			// like a quit, so there is never more than one live session.
			for {
				mx, err := matrixFromManifest(app)
				if err != nil {
					return err
				}
				states, unverified := outputStates(opts, app, mx)
				ps := ui.Problems(mx, states)
				// Header facts mirror status.go's, from the same places: app.Profile
				// for the active profile and where it came from, config.ManifestPath
				// and app.Worktree for the same manifest path and worktree status
				// prints, ResolveBackendName for the same backend name status prints.
				// Named fields, not positional args: Params exists precisely so these
				// adjacent strings can't be transposed here and still compile.
				m := ui.New(ui.Params{
					Project:                   app.Entry.ID,
					Profile:                   app.Profile.Name,
					Source:                    app.Profile.Source,
					ManifestPath:              config.ManifestPath(app.Entry.ID),
					Worktree:                  app.Worktree,
					Backend:                   store.ResolveBackendName(opts.Backend),
					Matrix:                    mx,
					Problems:                  ps,
					OutputFreshnessUnverified: unverified,
					Projects:                  projects,
					Outputs:                   buildOutputRows(app.Manifest, states),
				})

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

				// SwitchTo, not Saved(), is what the switcher reports -- a switch
				// must never be mistaken for either a write (Saved() stays false)
				// or a plain quit (which returns instead of looping again).
				if id, ok := final.SwitchTo(); ok {
					app, err = opts.resolveAppByID(id)
					if err != nil {
						return err
					}
					continue
				}

				if !final.Saved() {
					fmt.Fprintln(opts.Stdout, "no changes written")
					return nil
				}
				return commitStaged(opts, app, final.Staged())
			}
		},
	}
}

// commitStaged writes w's staged changes in six phases, because Task 6's new
// operations depend on each other in ways the four Change kinds Tasks 1-4
// shipped never did:
//
//  1. Structural additions (add-var, add-output): pure in-memory map
//     inserts. These run FIRST because nothing else may read an added
//     entry's absence -- in particular, phase 5 below may stage a
//     rename-output or remove-output for a row THIS SAME SESSION also
//     added (outputIdentityNames includes a staged add-output's name, so
//     selecting and acting on it before w is a normal session), and that
//     phase's mutation needs the key to already exist in the map.
//  2. Keyring writes (set-secret).
//  3. Keyring deletes (delete-var's secret removal), after every write --
//     phase 1's own rule, carried forward unchanged.
//  4. The state file: a staged rename repoints that output's render
//     records (State.RenameOutput), and a staged removal forgets them
//     outright (State.Forget, across every worktree) -- the staged-model
//     equivalent of `trenvy output rm --force`, not its refusal, since d
//     on this tab has already asked for that. Both save before the
//     manifest, for the same reason `trenvy output rename` already
//     established (see State.RenameOutput's doc): only this order is
//     recoverable by re-running the same session's equivalent by hand.
//  5. Every remaining manifest mutation, including every REMOVAL
//     (delete-var, remove-output) and the rest (set-value, promote-secret,
//     rename-output's manifest half, default-output). Removals live here,
//     never in phase 1's loop, so an add-output and a remove-output for the
//     SAME row in one batch always nets to "never existed" regardless of
//     which the caller listed first in staged -- getting this backwards is
//     the Fix-1-shaped bug: an addition applied AFTER its own removal
//     resurrects exactly what the user staged getting rid of.
//  6. Validate, then save the manifest.
//
// The manifest is the only inventory of which keyring items exist, so a
// failure before phase 6 leaves at worst a keyring item nothing references
// -- inert, because nothing ever looks for it. The reverse (manifest before
// keyring) would risk the opposite: a manifest entry declaring a secret that
// was never stored, or missing one whose secret still sits in the keyring,
// either of which is data loss or an item nothing can ever find again.
//
// staged changes carry no cross-run identity -- internal/ui holds them only
// in memory, and the terminal session that produced them has already ended
// by the time this runs (RunUI returned once w was pressed). So a failure
// partway through cannot be resumed: there is no session left to press w
// again in. What is reported is exactly how much of THIS run already
// landed; everything else that was staged is gone, and the only way back is
// a fresh trenvy ui session redoing it by hand -- except for a keyring
// write/delete or a state save that already landed for real, which phase 4
// and phase 6's error paths report so the message never claims something
// gone that a retry would actually find already done.
//
// Adding a kind (Task 8's set-globs) means adding one case to whichever
// phase above it belongs in -- never restructuring the phases themselves.
func commitStaged(opts *Options, app *appContext, staged []ui.Change) error {
	if len(staged) == 0 {
		return nil
	}

	// Phase 1: structural additions. add-var and add-output only ever
	// insert a key that does not exist yet, so applying them before
	// anything else is always safe -- nothing here reads an added entry's
	// OLD value, because there isn't one.
	for _, c := range staged {
		switch c.Kind {
		case ui.ChangeAddVar:
			if _, exists := app.Manifest.Vars[c.Name]; !exists {
				app.Manifest.Vars[c.Name] = manifest.Var{}
			}
		case ui.ChangeAddOutput:
			if app.Manifest.Outputs == nil {
				app.Manifest.Outputs = map[string]manifest.Output{}
			}
			if _, exists := app.Manifest.Outputs[c.Name]; !exists {
				app.Manifest.Outputs[c.Name] = manifest.Output{Format: c.Format, Path: c.Path}
			}
		}
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
			// Presence is read from app.Manifest as it stands after phase
			// 1 -- which never touches an EXISTING var's secretness, only
			// ever inserting brand new plain ones -- so this still agrees
			// with "as loaded" for every name delete-var could target: a
			// var deleted in the same batch it was added in (staged as
			// both add-var and delete-var) is correctly seen as never
			// having existed, never as secret.
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
		// Phase 2: keyring writes.
		for _, op := range writes {
			if err := s.Set(op.service, op.value); err != nil {
				return partialCommitError("store", op.service, err,
					storedApplied, storedTotal, deletedApplied, deletedTotal, 0, 0, false)
			}
			storedApplied++
		}
		// Phase 3: keyring deletes, after every write above -- phase 1's
		// own rule, unchanged.
		for _, op := range deletes {
			if err := s.Delete(op.service); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					// A declared secret that was never actually stored under
					// this profile is the ordinary case, not a failure, and
					// must not inflate the applied count.
					continue
				}
				return partialCommitError("delete", op.service, err,
					storedApplied, storedTotal, deletedApplied, deletedTotal, 0, 0, false)
			}
			deletedApplied++
		}
	}

	// removedOutputNames marks every output this batch removes. Computed
	// before phase 4, which needs it: forgetting a removed output's render
	// records is itself state-file work, not phase 5's manifest mutation.
	// deletedNames (below, by phase 5) is its counterpart for the
	// Variables tab -- kept where it is, since nothing before phase 5
	// needs it.
	removedOutputNames := make(map[string]bool, len(staged))
	for _, c := range staged {
		if c.Kind == ui.ChangeRemoveOutput {
			removedOutputNames[c.Name] = true
		}
	}

	// Phase 4: the state file. Two kinds touch it.
	//
	// A staged rename must repoint any render record under the output's
	// OLD name, and that repointing must be saved to disk before the
	// manifest ever reflects the new name -- see State.RenameOutput's own
	// doc and internal/cli/output.go's `output rename`, which established
	// this order first: only state-before-manifest is recoverable by
	// re-running the same rename, because the old name is still defined in
	// the manifest for the retry to find. Manifest-first would leave the
	// old name undefined and nothing left to re-rename.
	//
	// A staged removal forgets every render record under the removed name,
	// in every worktree that ever rendered it -- state/<id>.toml is one
	// file spanning all of them, the same file `trenvy output rm` reads,
	// and State.Find is keyed on the output NAME alone. Leaving a record
	// behind is not harmless: output names are short and meant to be
	// reused, and the next output created under this same name would have
	// its first render compared against the OLD output's stale digests --
	// reported MODIFIED or FOREIGN for a file nothing is wrong with. This
	// is the staged-model equivalent of `trenvy output rm --force`, not
	// its refusal without --force: refusing here would mean refusing after
	// w has already committed the rest of the batch, the wrong moment, and
	// pressing d on this tab has already asked for exactly this. What
	// --force does not skip, and neither does this, is naming every file
	// left on disk -- forgotten, printed once the whole commit succeeds,
	// in the exact words `output rm --force` already uses: removing the
	// manifest entry is not removing the file, and a file that may carry
	// secrets must never be silently forgotten about.
	//
	// Both RenameOutput and Forget only ever mutate app.State in memory;
	// Save runs once, unconditionally, whenever either kind was staged --
	// never only when a record actually moved or was found -- mirroring
	// `output rename`'s own unconditional Save after RenameOutput: a
	// project with no render records yet still deserves a state file that
	// agrees with the manifest the moment one is ever recorded.
	renamedRecords, renamedAny := 0, false
	for _, c := range staged {
		if c.Kind == ui.ChangeRenameOutput {
			renamedRecords += app.State.RenameOutput(c.Name, c.Value)
			renamedAny = true
		}
	}
	var forgotten []config.RenderEntry
	for _, e := range app.State.Renders {
		if removedOutputNames[e.Output] {
			forgotten = append(forgotten, e)
		}
	}
	for _, e := range forgotten {
		app.State.Forget(e.Worktree, e.Output)
	}
	if renamedAny || len(forgotten) > 0 {
		if err := app.State.Save(); err != nil {
			return partialCommitError("save state for output changes", config.StatePath(app.Entry.ID), err,
				storedApplied, storedTotal, deletedApplied, deletedTotal, 0, 0, false)
		}
	}
	// stateSaved records whether the Save above actually ran (and, since it
	// would have already returned on error, actually landed) -- distinct
	// from renamedRecords/len(forgotten) being nonzero, which are true the
	// instant the in-memory mutations above run, whether or not Save ever
	// gets called. Phase 6's failure path below must not credit a save
	// that never happened.
	stateSaved := renamedAny || len(forgotten) > 0

	// deletedNames marks every variable this batch also deletes, so the
	// set-value/set-secret cases below can refuse to read-modify-write a Var
	// that delete-var has removed, or is about to. That read-modify-write
	// against an already-deleted entry is the actual data-loss mechanism
	// behind the Fix 1 bug: Vars[c.Name] on a name delete already handled
	// returns a ZERO Var, and writing it back silently resurrects the
	// variable with every profile but the one this change touches missing.
	// internal/ui's own d and e now refuse to stage this combination in the
	// first place (beginEdit cancels a pending delete; toggleDelete refuses
	// a new one over a staged edit -- see their doc comments), so reaching
	// this branch from a real `trenvy ui` session should not be possible.
	// It stays here anyway: a Change slice is a plain exported value, and
	// this function must not trust that every caller built one the only way
	// internal/ui's keys currently allow.
	deletedNames := make(map[string]bool, len(staged))
	for _, c := range staged {
		if c.Kind == ui.ChangeDeleteVar {
			deletedNames[c.Name] = true
		}
	}

	// Phase 5: every remaining manifest mutation, including every removal.
	// Only now does the manifest change, and only in memory -- Validate
	// runs before anything reaches disk, so a rejected manifest cannot
	// leave a half-applied shape behind.
	for _, c := range staged {
		switch c.Kind {
		case ui.ChangeSetValue:
			if deletedNames[c.Name] {
				continue
			}
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
		case ui.ChangeDeleteVar:
			delete(app.Manifest.Vars, c.Name)
		case ui.ChangePromoteSecret:
			// The row-level record from s (see stagePromotion's doc), never
			// ChangeSetSecret: a set-secret also arrives here from editing an
			// ALREADY-secret cell's value, which must not flip anything --
			// the var is secret already, and re-deriving that from the wrong
			// signal is exactly what would make a promotion that moves zero
			// values (every profile already secret with none to move, or a
			// freshly added var promoted before it is ever given a value)
			// silently fail to flip at all, because no set-secret is staged
			// to ride along with it in that case.
			if deletedNames[c.Name] {
				continue
			}
			v := app.Manifest.Vars[c.Name]
			v.Secret = true
			v.Value = nil
			v.Profiles = nil
			app.Manifest.Vars[c.Name] = v
		case ui.ChangeRenameOutput:
			if removedOutputNames[c.Name] {
				continue
			}
			o, ok := app.Manifest.Outputs[c.Name]
			if !ok {
				// Phase 1 already ran, so the only way this row is
				// missing is a hand-built Change naming an identity that
				// never existed -- nothing to rename.
				continue
			}
			delete(app.Manifest.Outputs, c.Name)
			app.Manifest.Outputs[c.Value] = o
			// Mirrors `trenvy output rename`'s own coupling
			// (internal/cli/output.go): a rename of the row holding
			// project.output must repoint it, or Validate refuses every
			// commit after this one for naming an output that no longer
			// exists.
			if app.Manifest.Project.Output == c.Name {
				app.Manifest.Project.Output = c.Value
			}
		case ui.ChangeRemoveOutput:
			delete(app.Manifest.Outputs, c.Name)
			// Mirrors `trenvy output rm`'s own clearedDefault: removing the
			// output that held project.output must clear it, the same
			// reversible-edit reasoning that command already documents --
			// refusing until the user repoints would be ceremony here too.
			if app.Manifest.Project.Output == c.Name {
				app.Manifest.Project.Output = ""
			}
		case ui.ChangeDefaultOutput:
			if removedOutputNames[c.Name] {
				continue
			}
			app.Manifest.Project.Output = c.Name
		}
	}

	// Phase 6: validate, then save the manifest.
	if err := app.Manifest.Validate(); err != nil {
		// The warning below must print regardless of whether this batch
		// touched the keyring or the state file at all: a batch of pure
		// manifest edits (an add-var with nothing else set on it, say) has
		// no progress to report, but it can still carry other staged edits
		// that are about to be lost right along with it. The bare error
		// used to reach the caller unwarned on exactly that batch shape --
		// the one Fix 3 was filed for.
		if progress := commitProgress(storedApplied, storedTotal, deletedApplied, deletedTotal, renamedRecords, len(forgotten), stateSaved); progress != "" {
			return fmt.Errorf("%w\n  %s; the manifest was not saved\n"+
				"  everything else staged this session is gone -- open trenvy ui again and redo it",
				err, progress)
		}
		return fmt.Errorf("%w\n  everything else staged this session is gone -- open trenvy ui again and redo it", err)
	}

	if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
		return err
	}

	fmt.Fprintf(opts.Stdout, "saved %d changes\n", len(staged))
	// Naming the file, not just the removed output: matches `trenvy output
	// rm --force`'s own "left in place: %s/%s" verbatim, because a user who
	// has learned that command's wording should not have to learn a second
	// one here for the same fact.
	for _, e := range forgotten {
		fmt.Fprintf(opts.Stdout, "  left in place: %s/%s\n", e.Worktree, e.Path)
	}
	return nil
}

// commitProgress renders how far a commit got as "stored X of Y secrets,
// deleted A of B keyring items, repointed N render record(s), forgot M
// render record(s)", omitting whichever clause the batch never touched at
// all: a batch with no deletions in it reporting "deleted 0 of 0" would be
// noise at the exact moment a reader needs a message they can trust. The
// two render-record clauses only ever appear when stateSaved is true -- the
// state file's Save actually landed -- because that is real, persisted
// progress a retry would find already done, unlike an in-memory manifest
// edit that a failed commit leaves nowhere at all. Returns "" when the
// batch touched none of these.
func commitProgress(storedApplied, storedTotal, deletedApplied, deletedTotal, movedRecords, forgottenRecords int, stateSaved bool) string {
	var parts []string
	if storedTotal > 0 {
		parts = append(parts, fmt.Sprintf("stored %d of %d secrets", storedApplied, storedTotal))
	}
	if deletedTotal > 0 {
		parts = append(parts, fmt.Sprintf("deleted %d of %d keyring items", deletedApplied, deletedTotal))
	}
	if stateSaved && movedRecords > 0 {
		parts = append(parts, fmt.Sprintf("repointed %d render record(s)", movedRecords))
	}
	if stateSaved && forgottenRecords > 0 {
		parts = append(parts, fmt.Sprintf("forgot %d render record(s)", forgottenRecords))
	}
	return strings.Join(parts, ", ")
}

// partialCommitError reports a keyring write, a keyring delete, or a state
// save that failed partway through committing staged ui changes, in the
// shape this codebase's other partial-failure errors (edit's applyRows,
// unset, profile rm, project rm) all use: name the item, say how far the run
// got, say the manifest was not saved, name the way back.
//
// The session that staged these changes has already ended by the time this
// runs, so "open trenvy ui again" is never a resume -- it is a fresh session
// that has to redo everything this run did not finish.
func partialCommitError(verb, service string, err error, storedApplied, storedTotal, deletedApplied, deletedTotal, movedRecords, forgottenRecords int, stateSaved bool) error {
	return fmt.Errorf(
		"%s %s: %w\n  %s; the manifest was not saved\n"+
			"  everything else staged this session is gone -- open trenvy ui again and redo it",
		verb, service, err, commitProgress(storedApplied, storedTotal, deletedApplied, deletedTotal, movedRecords, forgottenRecords, stateSaved))
}

// registryIDs lists every registered project id, in registration order --
// the same order `trenvy project list` already uses, so the switcher and the
// CLI's own listing never disagree about how projects are ordered.
func registryIDs(reg *config.Registry) []string {
	ids := make([]string, len(reg.Projects))
	for i, e := range reg.Projects {
		ids[i] = e.ID
	}
	return ids
}

// buildOutputRows assembles the Outputs tab's rows: the same facts `trenvy
// output list` prints (name, format, path, globs as prose via
// ui.SelectsSummary) plus the render-freshness state outputStates already
// computed above for Problems -- read out of that same states map, never
// reclassified here. A second pass over app.Manifest.Outputs that decided
// freshness on its own is exactly the duplicate computation this task was
// warned against; this function only reshapes data outputStates already
// produced.
func buildOutputRows(m *manifest.Manifest, states map[string]string) []ui.OutputRow {
	names := make([]string, 0, len(m.Outputs))
	for name := range m.Outputs {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]ui.OutputRow, 0, len(names))
	for _, name := range names {
		o := m.Outputs[name]
		rows = append(rows, ui.OutputRow{
			Name:      name,
			Format:    o.Format,
			Path:      o.Path,
			Selects:   ui.SelectsSummary(o),
			State:     states[name],
			IsDefault: name == m.Project.Output,
		})
	}
	return rows
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
//
// Because quick is never the user's own choice here, this function treats
// classifyOutput's outputQuickSkipped differently than status.go's own
// describeOutput does -- see the case below.
// The bool return says whether at least one output's freshness could not be
// determined here because doing so would require reading a secret's value
// (the outputQuickSkipped case below) -- ui.Problems has no way to surface
// that itself, since flagging it there would recreate the exact permanent,
// unfixable false alarm Fix 2 removed. The caller uses it to caveat the
// Overview's own healthy-state line, once, rather than per output.
func outputStates(opts *Options, app *appContext, mx *ui.Matrix) (map[string]string, bool) {
	missing := false
	for _, name := range mx.Names {
		if mx.Cell(name, app.Profile.Name) == ui.CellMissing {
			missing = true
			break
		}
	}

	unverified := false
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
		case outputQuickSkipped:
			// Deliberately NOT ui.StateUnknown here, unlike outputSecretsMissing
			// and outputErrored below. classifyOutput only reaches this case
			// once it has already ruled out absent, foreign and hand-edited
			// (Fix 2) -- the one thing left unresolved is a secret's value
			// changing since the last render with the file otherwise
			// untouched, and that is invisible to any check that does not
			// read it. ui always classifies with quick=true; it is never the
			// user's own deliberate tradeoff the way `status --quick` is, so
			// flagging this here would be exactly the "permanent, unfixable
			// false problem" Fix 2 exists to remove -- nothing pressable
			// inside `trenvy ui` could ever clear it, because ui never stops
			// being quick. `trenvy status` (no --quick) remains the command
			// that actually reads the value and can tell fresh from stale.
			// The caller is told via the bool return instead, so the
			// Overview can say so once rather than not at all.
			states[name] = ui.StateFresh
			unverified = true
		case outputSecretsMissing, outputErrored:
			// Both mean "not resolved to an answer", for different reasons --
			// a missing secret (a real, actionable gap: `trenvy secret set`
			// clears it) or a read failure already logged to stderr.
			// ui.Problems' Fix for StateUnknown names `trenvy status`, which
			// is the right next step for both. Neither sets unverified: both
			// already surface as their own Problem, so the empty-list case
			// this bool exists for cannot occur alongside them.
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
	return states, unverified
}
