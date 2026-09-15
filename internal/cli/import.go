package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/resolve"
	"github.com/cdescobedo/trenvy/internal/sample"
)

func newImportCmd(opts *Options) *cobra.Command {
	var outputName string
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

			if outputName != "" {
				out, err := outputFor(samplePath, app.Worktree)
				if err != nil {
					return err
				}
				app.Manifest.Outputs[outputName] = out
				// project init creates no outputs, so it has nothing to
				// default to; this is the first moment an output exists at
				// all. Claim it as the default only when nothing already
				// holds that role -- a user who picked one keeps it.
				if app.Manifest.Project.Output == "" {
					app.Manifest.Project.Output = outputName
				}
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
				fmt.Fprintf(opts.Stdout,
					"next: trenvy -p %s secret set <NAME> for each secret, then trenvy status\n",
					app.Profile.Name)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&outputName, "output", "", "also create an output rendering to the sample's real path")
	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "skip the interactive review")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite variables already in the manifest")
	return cmd
}

// outputFor guesses an output from the sample's own location: a sample at
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
func outputFor(samplePath, fallbackRoot string) (manifest.Output, error) {
	abs, err := filepath.Abs(samplePath)
	if err != nil {
		abs = samplePath
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}

	// Only a real repository root may serve as the base. resolve.WorktreeRoot
	// would answer "the sample's own directory" for an unversioned project,
	// which passes filepath.Rel happily and silently collapses the path to a
	// bare basename -- an output written to the worktree root instead of
	// src/Api/.env. An unversioned project falls back to the worktree the
	// command is running in, which is what the project root is.
	base, ok := resolve.GitWorktreeRoot(filepath.Dir(abs))
	if !ok {
		base = fallbackRoot
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return manifest.Output{}, fmt.Errorf(
			"%s is not inside a worktree under the project root; "+
				"--output needs a sample checked into a worktree so it can guess the rendered path",
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

	return manifest.Output{
		Format:  format,
		Path:    real,
		Include: []string{"*"},
	}, nil
}

// deriveOutputName picks a short name for the output import creates, from the
// directory the sample lives in -- the part of the path that distinguishes one
// output from another.
//
// The last dot-separated segment is kept because the .NET layout this tool was
// built against names directories Company.Product.Api, where "api" is the
// memorable half. That is wrong for a directory like my.app.v2, which yields
// "v2"; the trade is accepted because dots in directory names are otherwise
// rare and `trenvy output rename` is one command away. import prints the name
// it chose for exactly that reason.
func deriveOutputName(relPath, format string, taken map[string]manifest.Output) string {
	base := "root"
	if dir := filepath.Dir(relPath); dir != "." {
		base = filepath.Base(dir)
	}
	base = strings.ToLower(base)
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		base = base[i+1:]
	}

	var b strings.Builder
	for _, c := range base {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
			continue
		}
		b.WriteByte('-')
	}
	name := b.String()
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-")
	if !manifest.ValidOutputName(name) {
		name = format
	}

	out := name
	for i := 2; ; i++ {
		if _, clash := taken[out]; !clash {
			return out
		}
		out = fmt.Sprintf("%s-%d", name, i)
	}
}
