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
	var noTUI, force, noOutput bool

	cmd := &cobra.Command{
		Use:   "import <sample file>",
		Short: "Seed the manifest from a sample .env or .json file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Checked first, before the sample is even read and before the
			// interactive review runs: catching a flag typo only after would
			// discard a whole review session.
			if noOutput && outputName != "" {
				return fmt.Errorf("--output and --no-output cannot both be given")
			}
			if outputName != "" {
				// Checked here, not left to Manifest.Validate, so a bad
				// --output name is reported as the argument problem it is
				// instead of "invalid manifest", and before anything is
				// written to disk. output add and rename already call this
				// at output.go:116 and :179; import named outputs too.
				if err := requireValidOutputName(outputName); err != nil {
					return err
				}
			}

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

			rep, err := importInto(app.Manifest, app.Worktree, samplePath, entries, outputName, noOutput, force)
			if err != nil {
				return err
			}
			if err := app.Manifest.Validate(); err != nil {
				return err
			}
			if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
				return err
			}

			fmt.Fprintf(opts.Stdout, "imported %d variables (%d secret) from %s\n",
				len(entries), rep.Secrets, samplePath)
			if rep.Secrets > 0 {
				fmt.Fprintf(opts.Stdout,
					"next: trenvy -p %s secret set <NAME> for each secret, then trenvy status\n",
					app.Profile.Name)
			}
			if rep.Created != "" {
				o := app.Manifest.Outputs[rep.Created]
				fmt.Fprintf(opts.Stdout, "created output %q  %s -> %s\n", rep.Created, o.Format, o.Path)
				fmt.Fprintf(opts.Stdout, "  rename it with: trenvy output rename %s <name>\n", rep.Created)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&outputName, "output", "", "name the output import creates (default: derived from the sample's directory)")
	cmd.Flags().BoolVar(&noOutput, "no-output", false, "import the variables only, creating no output")
	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "skip the interactive review")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite variables already in the manifest")
	return cmd
}

// importReport is what importInto did to the manifest. Output is the output
// the sample renders to, whether created or reused ("" with noOutput);
// Created is set only when a new one was made.
type importReport struct {
	Output, Created string
	Secrets         int
}

// importInto applies a parsed sample to m in memory: the variables (refusing
// a conflict without force), then the output (reusing one on the same path,
// deriving or checking the name, claiming the default when none). It neither
// validates nor saves.
func importInto(m *manifest.Manifest, worktree, samplePath string, entries []sample.Entry, outputName string, noOutput, force bool) (importReport, error) {
	var rep importReport
	for _, e := range entries {
		v := manifest.Var{Secret: e.GuessSecret}
		if !e.GuessSecret {
			// Only a plain value is carried over. A placeholder is not
			// a secret, so storing it would be worse than storing
			// nothing: status would report the secret as present.
			value := e.Value
			v.Value = &value
		}
		if existing, exists := m.Vars[e.Name]; exists && !force && !manifest.SameVar(existing, v) {
			return rep, fmt.Errorf(
				"%s is already in the manifest; re-run with --force to overwrite", e.Name)
		}
		m.Vars[e.Name] = v
		if e.GuessSecret {
			rep.Secrets++
		}
	}

	if noOutput {
		return rep, nil
	}
	o, err := outputFor(samplePath, worktree)
	if err != nil {
		return rep, err
	}
	name := outputName
	if existing, ok := outputAtPath(m, o.Path); ok {
		// Two outputs on one path clobber each other on render,
		// and the newcomer's state record would make the
		// incumbent look MODIFIED.
		name = existing
	} else {
		if name == "" {
			name = deriveOutputName(o.Path, o.Format, m.Outputs)
		} else if _, exists := m.Outputs[name]; exists {
			// Different rule from the path-reuse branch above:
			// reusing an output whose Path already matches is
			// correct, but redefining one by NAME -- same name,
			// different path -- would silently replace its
			// format and path and orphan whatever it used to
			// render. output add refuses this at output.go:123;
			// import must too.
			return rep, fmt.Errorf("output %q is already defined", name)
		}
		if m.Outputs == nil {
			m.Outputs = map[string]manifest.Output{}
		}
		m.Outputs[name] = o
		rep.Created = name
	}
	// Claim the default only when nothing already holds that
	// role -- a user who picked one keeps it.
	if m.Project.Output == "" {
		m.Project.Output = name
	}
	rep.Output = name
	return rep, nil
}

// outputAtPath returns the name of the output already rendering to path.
func outputAtPath(m *manifest.Manifest, path string) (string, bool) {
	for name, o := range m.Outputs {
		if o.Path == path {
			return name, true
		}
	}
	return "", false
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
		Format: format,
		Path:   real,
		// Include is left nil rather than []string{"*"}: resolve.selected
		// treats an empty include as "everything" before path.Match ever
		// runs, while a literal "*" goes through path.Match, whose "*"
		// does not cross "/" -- silently dropping a variable named
		// GROUP/KEY instead of letting render/dotenv.go report its "not a
		// valid shell identifier" error. The two are identical for every
		// name that works today; session.SelectsSummary renders both as "all".
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
