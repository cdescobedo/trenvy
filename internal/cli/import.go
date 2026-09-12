package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/resolve"
	"github.com/cdescobedo/envctl/internal/sample"
)

func newImportCmd(opts *Options) *cobra.Command {
	var targetName string
	var noTUI, force bool

	cmd := &cobra.Command{
		Use:   "import <sample file>",
		Short: "Seed the manifest from a sample .env or .json file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			samplePath := args[0]
			data, err := os.ReadFile(samplePath)
			if err != nil {
				return fmt.Errorf("read %s: %w", samplePath, err)
			}
			entries, err := sample.Parse(samplePath, data)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				return fmt.Errorf("%s contains no variables", samplePath)
			}

			if !noTUI {
				entries, err = reviewImport(opts, app.Entry.ID, entries)
				if err != nil {
					return err
				}
			}

			for _, e := range entries {
				if _, exists := app.Manifest.Vars[e.Name]; exists && !force {
					return fmt.Errorf(
						"%s is already in the manifest; re-run with --force to overwrite", e.Name)
				}
				v := manifest.Var{Secret: e.GuessSecret}
				if !e.GuessSecret {
					// Only a plain value is carried over. A placeholder is not
					// a secret, so storing it would be worse than storing
					// nothing: status would report the secret as present.
					value := e.Value
					v.Value = &value
				}
				app.Manifest.Vars[e.Name] = v
			}

			if targetName != "" {
				tgt, err := targetFor(samplePath)
				if err != nil {
					return err
				}
				app.Manifest.Targets[targetName] = tgt
			}
			if err := app.Manifest.Validate(); err != nil {
				return err
			}
			if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
				return err
			}

			secrets := 0
			for _, e := range entries {
				if e.GuessSecret {
					secrets++
				}
			}
			fmt.Fprintf(opts.Stdout, "imported %d variables (%d secret) from %s\n",
				len(entries), secrets, samplePath)
			if secrets > 0 {
				fmt.Fprintf(opts.Stdout, "next: envctl secret set <NAME> for each secret, then envctl status\n")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&targetName, "target", "", "also create a file target rendering to the sample's real path")
	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "skip the interactive review")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite variables already in the manifest")
	return cmd
}

// targetFor guesses a target from the sample's own location: a sample at
// src/Api/.env_example is a sample of src/Api/.env.
//
// The resulting path is stored relative to the worktree that will actually
// render it: the worktree containing the sample itself, not the directory
// import happened to be run from. In the documented workflow those two are
// different — import is run from the project root, which holds every
// worktree but is not itself one, while the sample lives inside a worktree
// such as main/. Using the wrong base bakes that worktree's own name into the
// stored path, so a later render re-interprets it against a different
// worktree root and doubles the segment (main/main/src/Api/.env).
func targetFor(samplePath string) (manifest.Target, error) {
	abs, err := filepath.Abs(samplePath)
	if err != nil {
		abs = samplePath
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}

	wt, err := resolve.WorktreeRoot(filepath.Dir(abs))
	if err != nil {
		return manifest.Target{}, fmt.Errorf("resolve the worktree for %s: %w", samplePath, err)
	}
	rel, err := filepath.Rel(wt, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return manifest.Target{}, fmt.Errorf(
			"%s is not inside a worktree under the project root; "+
				"--target needs a sample checked into a worktree so it can guess the rendered path",
			samplePath)
	}

	format := manifest.FormatDotenv
	if strings.Contains(strings.ToLower(rel), ".json") {
		format = manifest.FormatJSON
	}

	// Strip the example suffix from the filename only: an earlier version
	// used ReplaceAll over the whole relative path, which also mangled any
	// directory segment that happened to contain the same substring (e.g.
	// "test_example_project/.env_example" lost "_example" from the directory
	// too). TrimSuffix on the base name alone cannot touch a directory name.
	//
	// Only the first matching suffix is stripped: trying every suffix in
	// sequence on the shrinking base would, for a name like
	// "app.dist.example", strip ".example" and then also ".dist" from what's
	// left, over-stripping a name that only ever had one placeholder suffix.
	dir, base := filepath.Split(rel)
	for _, suffix := range []string{".example", "_example", ".sample", ".dist"} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimSuffix(base, suffix)
			break
		}
	}
	real := filepath.Join(dir, base)

	return manifest.Target{
		Dest:    manifest.DestFile,
		Format:  format,
		Path:    real,
		Include: []string{"*"},
	}, nil
}
