package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/envctl/internal/config"
)

func newProjectCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Register, list, move and remove projects",
	}
	cmd.AddCommand(newInitCmd(opts), newProjectListCmd(opts))
	return cmd
}

func newProjectListCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered projects and their roots",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			reg, err := config.LoadRegistry()
			if err != nil {
				return err
			}
			if len(reg.Projects) == 0 {
				fmt.Fprintln(opts.Stdout, "no projects registered")
				fmt.Fprintln(opts.Stdout, "  register one with: envctl project init")
				return nil
			}
			w := tabwriter.NewWriter(opts.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tROOT")
			for _, p := range reg.Projects {
				fmt.Fprintf(w, "%s\t%s\n", p.ID, p.Root)
			}
			return w.Flush()
		},
	}
}
