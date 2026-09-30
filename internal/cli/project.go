package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/store"
)

func newProjectCmd(opts *Options) *cobra.Command {
	cmd := newGroupCmd("project", "Register, list, move and remove projects")
	cmd.AddCommand(
		newInitCmd(opts), newProjectListCmd(opts),
		newProjectMoveCmd(opts), newProjectRmCmd(opts),
	)
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
				fmt.Fprintln(opts.Stdout, "  register one with: trenvy project init")
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
		ValidArgsFunction: func(c *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 1 {
				// The second argument is a directory on disk, so hand it to
				// the shell rather than inventing candidates.
				return nil, cobra.ShellCompDirectiveFilterDirs
			}
			return opts.completeProjectIDs()(c, args, toComplete)
		},
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
				return fmt.Errorf("no project registered with id %q; see: trenvy project list", id)
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
					"trenvy: %s still exists -- if this was a copy rather than a move,\n"+
						"  register the new one instead with: trenvy project init --root %s\n",
					old.Root, abs)
			}
			return nil
		},
	}
}

// newProjectRmCmd deletes a project outright: its manifest, its render state,
// its registry entry and every keyring item it owns in the currently
// selected secret backend.
//
// The backend qualifier matters: a secret stored under a different backend
// (--backend, TRENVY_BACKEND, or a platform default that changed since it was
// stored) is not touched by this command, but the manifest deleted here was
// that secret's only inventory. Once it's gone, nothing records that the
// secret exists or which backend holds it -- so it survives, just unfindable.
//
// It deliberately does not use resolveApp. Every other project-scoped command
// asks "which project owns this directory?"; this one is told an id, and must
// work from anywhere on disk -- including from outside every registered root,
// which is exactly where someone stands after deleting a project's checkout.
func newProjectRmCmd(opts *Options) *cobra.Command {
	var yes bool

	c := &cobra.Command{
		Use:   "rm <id>",
		Short: "Delete a project: its manifest, state, registration and its secrets in the current backend",
		Long: "Delete a project: its manifest, state, registration and the secrets it owns\n" +
			"in the currently selected secret backend.\n\n" +
			"Like every other command, this scopes to one backend (the --backend flag,\n" +
			"TRENVY_BACKEND, or the platform default). A secret stored under a\n" +
			"different backend is not touched -- but the manifest this command deletes\n" +
			"was that secret's only inventory, so once it is gone there is no record\n" +
			"left that the secret exists, or where.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: opts.completeProjectIDs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			reg, err := config.LoadRegistry()
			if err != nil {
				return err
			}
			if _, ok := reg.Find(id); !ok {
				return fmt.Errorf("no project registered with id %q; see: trenvy project list", id)
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			m, err := manifest.Load(config.ManifestPath(id))
			if err != nil {
				return err
			}
			st, err := config.LoadState(id)
			if err != nil {
				return err
			}

			secrets := secretNames(m)
			items := len(secrets) * len(m.Project.Profiles)

			// Typed id, not y/N. This is the only command that destroys a
			// whole project's secrets with no undo, and y/N is one keystroke
			// away from a habit; typing the id makes the answer specific to
			// the project being removed.
			if !yes {
				fmt.Fprintf(opts.Stderr,
					"This deletes the manifest, the render state, the registry entry,\n"+
						"and up to %d keyring items (%d secrets across %d profiles) in the current\n"+
						"backend. It cannot be undone.\n",
					items, len(secrets), len(m.Project.Profiles))
				if len(st.Renders) > 0 {
					fmt.Fprintf(opts.Stderr, "Rendered files are NOT removed; %d remain:\n", len(st.Renders))
					for _, e := range st.Renders {
						fmt.Fprintf(opts.Stderr, "  %s\n", filepath.Join(e.Worktree, e.Path))
					}
				}
				fmt.Fprintf(opts.Stderr, "Type the project id to confirm: ")
				line, err := bufio.NewReader(opts.Stdin).ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) {
					return err
				}
				if strings.TrimSpace(line) != id {
					return errors.New("aborted")
				}
			}

			// Keyring first, and abort on the first failure. The manifest is
			// the only inventory of which items exist, so destroying it before
			// them would strand the remainder with no record. Aborting here
			// leaves the manifest, the state file and the registry entry
			// intact, which is what keeps the command re-runnable.
			//
			// The error says how many items are already gone rather than
			// claiming nothing was removed: by the time a delete fails, every
			// item before it has been destroyed permanently, and telling
			// someone their secrets are intact is how they end up not
			// re-running and losing the rest.
			//
			// The service name is built from cfg directly with the profile
			// being iterated: app.service would bind whichever profile is
			// active, and would therefore delete one profile's item len(
			// Profiles) times while leaving the others untouched.
			//
			// deleted counts only items actually removed. A declared secret
			// that was never stored under a given profile is the ordinary
			// case, not a failure, so ErrNotFound is skipped -- and it must
			// not inflate the count reported back.
			deleted := 0
			if items > 0 {
				s, err := opts.NewStore(cfg, opts.Backend)
				if err != nil {
					return err
				}
				for _, profile := range m.Project.Profiles {
					for _, n := range secrets {
						svc := cfg.Keyring.ServiceName(id, profile, n)
						if err := s.Delete(svc); err != nil {
							if errors.Is(err, store.ErrNotFound) {
								continue
							}
							return fmt.Errorf(
								"delete %s: %w\n  %d items were already deleted; the manifest,"+
									" state and registry are untouched -- fix this and re-run",
								svc, err, deleted)
						}
						deleted++
					}
				}
			}

			// manifest.Load above already failed if the file were missing, so
			// no test can reach this ErrNotExist branch -- it is unreachable
			// except in the window where a second `project rm` for the same id
			// is running concurrently. It stays anyway: on that path this run's
			// keyring work is already done, and erroring out over a file the
			// other run removed would leave the state file and the registry
			// entry behind for no reason.
			if err := os.Remove(config.ManifestPath(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if err := os.Remove(config.StatePath(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			// The result is discarded deliberately: reg.Find above already
			// proved the entry is there, so a false here could only mean the
			// registry changed underneath this run, and the Save below writes
			// the same absence either way.
			_ = reg.Remove(id)
			if err := reg.Save(); err != nil {
				return err
			}

			fmt.Fprintf(opts.Stdout, "removed %s (%d keyring items deleted)\n", id, deleted)
			// Rendered files stay: they live in worktrees, they may have been
			// hand-edited, and `clean` already guards exactly that case. Say
			// so rather than leaving them silently behind.
			if len(st.Renders) > 0 {
				fmt.Fprintf(opts.Stdout, "  %d rendered files were left in place\n", len(st.Renders))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return c
}
