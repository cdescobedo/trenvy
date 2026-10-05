package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/trenvy/internal/sample"
	"github.com/cdescobedo/trenvy/internal/store"
	"github.com/cdescobedo/trenvy/internal/tui"
)

func newEditCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open the interactive interface (the same as trenvy ui)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			return runManage(opts, app)
		},
	}
}

// reviewImport is the import review: internal/tui's table, pre-filled from the
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
