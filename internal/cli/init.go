package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
)

func newInitCmd(opts *Options) *cobra.Command {
	var id, root string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Register this project and create its manifest",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if root == "" {
				root = opts.Dir
			}
			abs, err := filepath.Abs(root)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", root, err)
			}
			if id == "" {
				id = filepath.Base(abs)
			}

			reg, err := config.LoadRegistry()
			if err != nil {
				return err
			}
			if err := reg.Add(config.Entry{ID: id, Root: abs}); err != nil {
				return err
			}

			salt, err := manifest.NewSalt()
			if err != nil {
				return err
			}
			// A process target is the only one that needs no further decisions,
			// so a fresh manifest is valid and immediately usable with exec.
			m := &manifest.Manifest{
				Project: manifest.Project{ID: id, DefaultTarget: "local", Salt: salt},
				Targets: map[string]manifest.Target{
					"local": {Dest: manifest.DestProcess, Include: []string{"*"}},
				},
				Vars: map[string]manifest.Var{},
			}
			if err := m.Validate(); err != nil {
				return err
			}
			if err := manifest.Save(config.ManifestPath(id), m); err != nil {
				return err
			}
			if err := reg.Save(); err != nil {
				return err
			}

			fmt.Fprintf(opts.Stdout, "registered %s\n  root      %s\n  manifest  %s\n",
				id, abs, config.ManifestPath(id))
			fmt.Fprintf(opts.Stdout, "\nnext: envctl import <sample .env or .json>, or envctl edit\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "project id (default: the root directory's name)")
	cmd.Flags().StringVar(&root, "root", "",
		"project root: the directory holding every worktree (default: the current directory)")
	return cmd
}
