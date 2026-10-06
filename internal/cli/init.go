package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
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
			// The id must be judged before anything is registered or
			// written: it is a path segment of the manifest and state
			// files, so a bad one is a bad path, not a bad label.
			derived := id == ""
			if derived {
				id = filepath.Base(abs)
			}
			if err := manifest.ValidateProjectID(id); err != nil {
				if derived {
					return fmt.Errorf("the directory name %q cannot be a project id: "+
						"%v; pass one with --id", id, err)
				}
				return fmt.Errorf("--id %q: %v", id, err)
			}

			m, err := newProjectManifest(id)
			if err != nil {
				return err
			}
			if err := registerProject(id, abs, m); err != nil {
				return err
			}

			fmt.Fprintf(opts.Stdout, "registered %s\n  root      %s\n  manifest  %s\n",
				id, abs, config.ManifestPath(id))
			fmt.Fprintf(opts.Stdout, "\nnext: trenvy import <sample .env or .json>, or trenvy edit\n")
			fmt.Fprintf(opts.Stdout, "add a second value set with: trenvy profile add <name>\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "project id (default: the root directory's name)")
	cmd.Flags().StringVar(&root, "root", "",
		"project root: the directory holding every worktree (default: the current directory)")
	return cmd
}

// newProjectManifest is `project init`'s manifest: one profile, a fresh
// salt, nothing else.
//
// No outputs, and no project.output. An output is a file -- its format,
// path and subset -- and a fresh project has no way to guess any of the
// three. It needs none either: exec and shell are the verb, so a manifest
// with one profile and nothing else is valid and immediately usable.
// `trenvy import --output` or a hand edit adds a file destination when
// there is one.
func newProjectManifest(id string) (*manifest.Manifest, error) {
	salt, err := manifest.NewSalt()
	if err != nil {
		return nil, err
	}
	return &manifest.Manifest{
		Project: manifest.Project{
			ID:             id,
			Profiles:       []string{"dev"},
			DefaultProfile: "dev",
			Salt:           salt,
		},
		Outputs: map[string]manifest.Output{},
		Vars:    map[string]manifest.Var{},
	}, nil
}

// registerProject adds id at root to the registry in memory (refusing a
// taken id or root), validates m, saves the manifest, then saves the
// registry. Nothing is written when the add or the validation fails.
func registerProject(id, root string, m *manifest.Manifest) error {
	reg, err := config.LoadRegistry()
	if err != nil {
		return err
	}
	if err := reg.Add(config.Entry{ID: id, Root: root}); err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if err := manifest.Save(config.ManifestPath(id), m); err != nil {
		return err
	}
	return reg.Save()
}
