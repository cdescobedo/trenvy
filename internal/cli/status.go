package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStatusCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the resolved project, its secrets and its targets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			fmt.Fprintf(opts.Stdout, "project   %s\n", app.Entry.ID)
			fmt.Fprintf(opts.Stdout, "worktree  %s\n", app.Worktree)
			return nil
		},
	}
}
