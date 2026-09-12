package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/guard"
	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/render"
	"github.com/cdescobedo/envctl/internal/resolve"
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
			out := opts.Stdout

			fmt.Fprintf(out, "project   %s\n", app.Entry.ID)
			fmt.Fprintf(out, "manifest  %s\n", config.ManifestPath(app.Entry.ID))
			fmt.Fprintf(out, "worktree  %s\n", app.Worktree)
			fmt.Fprintf(out, "backend   %s\n\n", backendLabel(opts))

			names := make([]string, 0, len(app.Manifest.Vars))
			secrets := 0
			for n, v := range app.Manifest.Vars {
				names = append(names, n)
				if v.Secret {
					secrets++
				}
			}
			sort.Strings(names)
			fmt.Fprintf(out, "vars      %d total, %d secret\n", len(names), secrets)

			missing := 0
			for _, n := range names {
				if !app.Manifest.Vars[n].Secret {
					continue
				}
				// Has, never Get: presence must not cost an authorization
				// prompt, and a value must never reach this output.
				ok, err := app.Store.Has(app.service(n))
				if err != nil {
					return err
				}
				if ok {
					fmt.Fprintf(out, "  present  %s\n", n)
				} else {
					missing++
					fmt.Fprintf(out, "  MISSING  %s          -> envctl secret set %s\n", n, n)
				}
			}

			fmt.Fprintln(out, "\ntargets")
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			for _, name := range sortedTargetNames(app.Manifest) {
				t := app.Manifest.Targets[name]
				if t.Dest != manifest.DestFile {
					fmt.Fprintf(w, "  %s\tprocess\t\n", name)
					continue
				}
				state := describeTarget(app, name, t, missing > 0)
				fmt.Fprintf(w, "  %s\t%s\t%s\n", name, t.Path, state)
			}
			return w.Flush()
		},
	}
}

// describeTarget classifies a file target without reading any secret value.
// When a secret is missing the values cannot be resolved, so freshness is
// genuinely unknown rather than fresh.
func describeTarget(app *appContext, name string, t manifest.Target, secretsMissing bool) string {
	abs := filepath.Join(app.Worktree, t.Path)
	entry, have := app.State.Find(app.Worktree, name)

	if secretsMissing {
		st, err := guard.Inspect(abs, "", entry, have)
		if err != nil {
			return "?"
		}
		if st == guard.StatusAbsent {
			return "not rendered"
		}
		return "unknown (a secret is missing)"
	}

	values, err := resolve.Values(app.Manifest, name, app.secretLookup())
	if err != nil {
		return "?"
	}
	st, err := guard.Inspect(abs, render.Sum(app.Manifest.Project.Salt, values), entry, have)
	if err != nil {
		return "?"
	}
	if st == guard.StatusAbsent {
		return "not rendered"
	}
	return st.String()
}

func sortedTargetNames(m *manifest.Manifest) []string {
	out := make([]string, 0, len(m.Targets))
	for n := range m.Targets {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func backendLabel(opts *Options) string {
	if opts.Backend != "" {
		return opts.Backend
	}
	return "default"
}
