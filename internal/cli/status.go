package cli

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/guard"
	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/render"
	"github.com/cdescobedo/envctl/internal/resolve"
	"github.com/cdescobedo/envctl/internal/store"
)

func newStatusCmd(opts *Options) *cobra.Command {
	var quick bool

	c := &cobra.Command{
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
			fmt.Fprintf(out, "backend   %s (%s)\n\n", store.ResolveBackendName(opts.Backend), runtime.GOOS)

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
			vw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			for _, n := range names {
				if !app.Manifest.Vars[n].Secret {
					continue
				}
				s, err := app.store()
				if err != nil {
					return err
				}
				// Has, never Get: presence must not cost an authorization
				// prompt, and a value must never reach this output.
				ok, err := s.Has(app.service(n))
				if err != nil {
					return err
				}
				if ok {
					fmt.Fprintf(vw, "  present\t%s\n", n)
				} else {
					missing++
					fmt.Fprintf(vw, "  MISSING\t%s\t-> envctl secret set %s\n", n, n)
				}
			}
			if err := vw.Flush(); err != nil {
				return err
			}

			fmt.Fprintln(out, "\ntargets")
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			for _, name := range sortedTargetNames(app.Manifest) {
				t := app.Manifest.Targets[name]
				if t.Dest != manifest.DestFile {
					fmt.Fprintf(w, "  %s\tprocess\n", name)
					continue
				}
				state := describeTarget(opts, app, name, t, missing > 0, quick)
				fmt.Fprintf(w, "  %s\t%s\t%s\n", name, t.Path, state)
			}
			return w.Flush()
		},
	}
	c.Flags().BoolVar(&quick, "quick", false,
		"skip reading secret values; freshness for a target with at least one secret is reported as unknown")
	return c
}

// describeTarget classifies a file target. Freshness is an HMAC over the
// resolved values, so detecting a keyring change fundamentally requires
// reading it -- that read is status's default behaviour and its whole
// purpose. --quick skips it: any target selecting at least one secret then
// reports its freshness as unknown rather than paying for the read.
func describeTarget(opts *Options, app *appContext, name string, t manifest.Target, secretsMissing, quick bool) string {
	abs := filepath.Join(app.Worktree, t.Path)
	entry, have := app.State.Find(app.Worktree, name)

	if secretsMissing {
		st, err := guard.Inspect(abs, "", entry, have)
		if err != nil {
			fmt.Fprintf(opts.Stderr, "envctl: status: %s: %v\n", name, err)
			return "?"
		}
		if st == guard.StatusAbsent {
			return "not rendered"
		}
		return "unknown (a secret is missing)"
	}

	if quick {
		hasSecret, err := resolve.TargetHasSecret(app.Manifest, name)
		if err != nil {
			fmt.Fprintf(opts.Stderr, "envctl: status: %s: %v\n", name, err)
			return "?"
		}
		if hasSecret {
			return "unknown (--quick)"
		}
	}

	values, err := resolve.Values(app.Manifest, name, app.secretLookup())
	if err != nil {
		fmt.Fprintf(opts.Stderr, "envctl: status: %s: %v\n", name, err)
		return "?"
	}
	st, err := guard.Inspect(abs, render.Sum(app.Manifest.Project.Salt, values), entry, have)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "envctl: status: %s: %v\n", name, err)
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
