package cli

import (
	"fmt"

	"github.com/spf13/cobra"

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

			_, err = opts.RunUI(m, func(name, profile string) (string, error) {
				s, err := app.store()
				if err != nil {
					return "", err
				}
				return s.Get(app.Cfg.Keyring.ServiceName(app.Manifest.Project.ID, profile, name))
			})
			return err
		},
	}
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
