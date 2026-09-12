package cli

import (
	"errors"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/sample"
	"github.com/cdescobedo/envctl/internal/store"
	"github.com/cdescobedo/envctl/internal/tui"
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
				func(name string) (string, error) { return app.Store.Get(app.service(name)) },
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
		row := tui.Row{Name: name, Secret: v.Secret, Overrides: len(v.Targets)}
		if v.Value != nil {
			row.Value = *v.Value
		}
		if v.Secret {
			present, err := app.Store.Has(app.service(name))
			if err != nil {
				return nil, err
			}
			row.Present = present
		}
		rows = append(rows, row)
	}
	return rows, nil
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

	for _, r := range rows {
		if r.Deleted {
			if r.Secret {
				if err := app.Store.Delete(app.service(r.Name)); err != nil &&
					!errors.Is(err, store.ErrNotFound) {
					return err
				}
			}
			delete(app.Manifest.Vars, r.Name)
			continue
		}
		kept[r.Name] = true

		v := app.Manifest.Vars[r.Name]
		v.Secret = r.Secret
		if r.Secret {
			v.Value = nil
			if r.Dirty && r.Value != "" {
				if err := app.Store.Set(app.service(r.Name), r.Value); err != nil {
					return err
				}
			}
		} else {
			value := r.Value
			v.Value = &value
		}
		app.Manifest.Vars[r.Name] = v
	}

	// Rows removed from the table entirely are deletions too.
	for name := range app.Manifest.Vars {
		if !kept[name] {
			delete(app.Manifest.Vars, name)
		}
	}

	if err := app.Manifest.Validate(); err != nil {
		return err
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

// reviewImport replaces the Task 19 stub: the import review is the same table,
// pre-filled from the sample.
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
