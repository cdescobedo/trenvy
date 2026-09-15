package cli

import (
	"errors"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/sample"
	"github.com/cdescobedo/trenvy/internal/store"
	"github.com/cdescobedo/trenvy/internal/tui"
)

func newEditCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Edit this project's variables interactively",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}

			rows, err := rowsFromManifest(app)
			if err != nil {
				return err
			}
			final, err := opts.RunTUI(
				tui.New(app.Entry.ID, rows),
				func(name string) (string, error) {
					s, err := app.store()
					if err != nil {
						return "", err
					}
					return s.Get(app.service(name))
				},
			)
			if err != nil {
				return err
			}
			if !final.Saved() {
				fmt.Fprintln(opts.Stdout, "no changes written")
				return nil
			}
			return applyRows(opts, app, final.AllRows())
		},
	}
}

// rowsFromManifest builds the table. Presence comes from Has, never Get: the
// table must render without paying an authorization prompt per secret.
func rowsFromManifest(app *appContext) ([]tui.Row, error) {
	var rows []tui.Row
	for _, name := range sortedVarNames(app.Manifest) {
		v := app.Manifest.Vars[name]
		row := tui.Row{Name: name, Secret: v.Secret, Overrides: len(v.Profiles)}
		if v.Value != nil {
			row.Value = *v.Value
		}
		if v.Secret {
			s, err := app.store()
			if err != nil {
				return nil, err
			}
			present, err := s.Has(app.service(name))
			if err != nil {
				return nil, err
			}
			row.Present = present
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// storeOp is one deferred keyring write, collected while applyRows computes
// the manifest's new shape and carried out only once that shape has passed
// Validate. Writing the keyring first would leave a secret in the keyring
// with no corresponding manifest entry whenever validation then failed.
type storeOp struct {
	del     bool // Set when false, Delete when true.
	service string
	value   string
}

// partialWriteError reports a keyring write that failed partway through a
// save, in the shape unset, profile rm and project rm all use: name the item,
// say how far the run got, say what survived, say what to do.
//
// "of %d" rather than a bare count: edit's ops are a mixed batch of stores and
// deletes, so the denominator is what tells the reader whether a run stopped
// at the start or near the end -- and it reads correctly at zero, which a bare
// count does not.
//
// Re-running edit reopens the table from the manifest on disk, so the session
// that failed is genuinely lost rather than merely postponed. The message says
// "edit again", not "re-run", because those are not the same promise.
func partialWriteError(verb, service string, err error, applied, total int) error {
	return fmt.Errorf(
		"%s %s: %w\n  already applied %d of %d keyring writes; the manifest was not"+
			" saved, so this session's edits were not kept -- fix this and edit again",
		verb, service, err, applied, total)
}

// applyRows writes the result of an editing session: plain values into the
// manifest, secret values into the keyring, deletions to both.
//
// rows must be AllRows(), never Rows(): if a filter was active when the user
// saved, Rows() would only contain the matching subset, and the "removed from
// the table" sweep below would then delete every variable the filter hid.
//
// Secrets are written only when the row changed, so an unchanged session does
// not rewrite every item — which on macOS would be pointless keychain churn.
func applyRows(opts *Options, app *appContext, rows []tui.Row) error {
	kept := map[string]bool{}
	var ops []storeOp

	for _, r := range rows {
		if r.Deleted {
			if r.Secret {
				ops = append(ops, storeOp{del: true, service: app.service(r.Name)})
			}
			delete(app.Manifest.Vars, r.Name)
			continue
		}
		kept[r.Name] = true

		v := app.Manifest.Vars[r.Name]
		wasSecret := v.Secret
		v.Secret = r.Secret
		if r.Secret {
			v.Value = nil
			if r.Dirty && r.Value != "" {
				ops = append(ops, storeOp{service: app.service(r.Name), value: r.Value})
			}
		} else {
			value := r.Value
			v.Value = &value
			if wasSecret {
				// The variable just stopped being secret: its old keyring
				// item is no longer referenced by the manifest at all, and
				// leaving it behind is worse than a merely stale value — a
				// later re-toggle to secret without setting a fresh value
				// would (per the write-only-when-dirty guard above) silently
				// resurrect it via a subsequent reveal.
				ops = append(ops, storeOp{del: true, service: app.service(r.Name)})
			}
		}
		app.Manifest.Vars[r.Name] = v
	}

	// Rows removed from the table entirely are deletions too.
	for name := range app.Manifest.Vars {
		if !kept[name] {
			delete(app.Manifest.Vars, name)
		}
	}

	// Validate before anything is written, so a rejected manifest cannot
	// leave secrets in the keyring that nothing references.
	if err := app.Manifest.Validate(); err != nil {
		return err
	}

	// Then the keyring, and only then the manifest -- same ordering as
	// `secret set`, for the same reason. The store is resolved lazily, so an
	// unusable backend reports itself here, before the manifest is touched.
	//
	// A failure partway through cannot be unwound: every op before it has
	// already hit the keyring for good. What is left depends on which kind
	// stopped -- an applied Set leaves an item the unsaved manifest does not
	// reference, an applied Delete leaves a still-declared secret whose item
	// is gone and which status will report MISSING. Either way the count has
	// to be reported, because a user told nothing happened is a user who does
	// not go looking.
	//
	// applied counts only ops that changed the keyring. A Delete for an item
	// that was never stored is the ordinary case, not a failure, so
	// ErrNotFound is skipped and must not inflate the count.
	if len(ops) > 0 {
		s, err := app.store()
		if err != nil {
			return err
		}
		applied := 0
		for _, op := range ops {
			if op.del {
				if err := s.Delete(op.service); err != nil {
					if errors.Is(err, store.ErrNotFound) {
						continue
					}
					return partialWriteError("delete", op.service, err, applied, len(ops))
				}
				applied++
				continue
			}
			if err := s.Set(op.service, op.value); err != nil {
				return partialWriteError("store", op.service, err, applied, len(ops))
			}
			applied++
		}
	}

	if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
		return err
	}

	fmt.Fprintf(opts.Stdout, "saved %d variables\n", len(app.Manifest.Vars))
	return nil
}

func sortedVarNames(m *manifest.Manifest) []string {
	out := make([]string, 0, len(m.Vars))
	for n := range m.Vars {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// reviewImport is the import review: the same table, pre-filled from the
// sample. Reveal is stubbed out to ErrNotFound because nothing being reviewed
// is in the keyring yet -- these rows are candidates, not stored secrets.
func reviewImport(opts *Options, projectID string, entries []sample.Entry) ([]sample.Entry, error) {
	rows := make([]tui.Row, 0, len(entries))
	for _, e := range entries {
		row := tui.Row{Name: e.Name, Secret: e.GuessSecret, Source: "sample"}
		if !e.GuessSecret {
			row.Value = e.Value
		}
		rows = append(rows, row)
	}

	final, err := opts.RunTUI(
		tui.New(projectID+" (import)", rows),
		func(string) (string, error) { return "", store.ErrNotFound },
	)
	if err != nil {
		return nil, err
	}
	if !final.Saved() {
		return nil, errors.New("import cancelled")
	}

	out := make([]sample.Entry, 0, len(final.AllRows()))
	for _, r := range final.AllRows() {
		if r.Deleted {
			continue
		}
		out = append(out, sample.Entry{Name: r.Name, Value: r.Value, GuessSecret: r.Secret})
	}
	return out, nil
}
