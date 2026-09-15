package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/envctl/internal/config"
)

func newProjectCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Register, list, move and remove projects",
	}
	cmd.AddCommand(newInitCmd(opts), newProjectListCmd(opts), newProjectMoveCmd(opts))
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

func newProjectMoveCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "move <id> <new-root>",
		Short: "Repoint a project at a new root after its directory moved",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, newRoot := args[0], args[1]

			abs, err := filepath.Abs(newRoot)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", newRoot, err)
			}
			info, err := os.Stat(abs)
			if err != nil {
				return fmt.Errorf("new root %s: %w", abs, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("new root %s is not a directory", abs)
			}

			reg, err := config.LoadRegistry()
			if err != nil {
				return err
			}
			old, ok := reg.Find(id)
			if !ok {
				return fmt.Errorf("no project registered with id %q; see: envctl project list", id)
			}
			if err := reg.SetRoot(id, abs); err != nil {
				return err
			}

			st, err := config.LoadState(id)
			if err != nil {
				return err
			}
			moved := st.RebaseWorktrees(old.Root, abs)

			// State is saved before the registry deliberately: the directory
			// has already moved on disk by the time this command runs, so
			// state's rebased worktree paths are the ones that now match
			// reality. If we crash here, the registry still names the old
			// root; Lookup from the new location then fails loudly (no
			// project registered for this directory) and rerunning `project
			// move` repairs it, since old.Root is still recoverable from the
			// registry. Saving the registry first would do the opposite: a
			// crash before the state save would leave state pointing at a
			// root that is no longer registered anywhere, so a retry could
			// never recover the old root to rebase from, and the guard would
			// permanently misclassify the rendered file as FOREIGN.
			if err := st.Save(); err != nil {
				return err
			}
			if err := reg.Save(); err != nil {
				return err
			}

			fmt.Fprintf(opts.Stdout, "moved %s\n  from  %s\n  to    %s\n", id, old.Root, abs)
			fmt.Fprintf(opts.Stdout, "  %d render records rebased\n", moved)
			if _, err := os.Stat(old.Root); err == nil {
				fmt.Fprintf(opts.Stderr,
					"envctl: %s still exists -- if this was a copy rather than a move,\n"+
						"  register the new one instead with: envctl project init --root %s\n",
					old.Root, abs)
			}
			return nil
		},
	}
}
