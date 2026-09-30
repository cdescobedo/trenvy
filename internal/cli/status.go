package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/guard"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/render"
	"github.com/cdescobedo/trenvy/internal/resolve"
	"github.com/cdescobedo/trenvy/internal/store"
	"github.com/cdescobedo/trenvy/internal/style"
)

func newStatusCmd(opts *Options) *cobra.Command {
	var quick bool

	c := &cobra.Command{
		Use:   "status",
		Short: "Show the resolved project, its secrets and its outputs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			out := opts.Stdout

			fmt.Fprintf(out, "project   %s\n", app.Entry.ID)
			// The profile decides every value below, and a stale exported
			// TRENVY_PROFILE is invisible until something names it -- so the
			// header reports where the name came from, not just what it is.
			fmt.Fprintf(out, "profile   %s  (%s)\n", app.Profile.Name, app.Profile.Source)
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
			var vrows [][]string
			for _, n := range names {
				v := app.Manifest.Vars[n]

				// A plain variable's value lives in the manifest, so its state
				// is decided here without touching the store. The case worth
				// reporting is a value that exists under some OTHER profile:
				// the manifest validates, and render fails only once you ask
				// for this profile. status is where that should surface.
				if !v.Secret {
					switch {
					case v.Profiles[app.Profile.Name] != "" || v.Value != nil:
						vrows = append(vrows, []string{"  " + opts.Styler.Stored("present"), n})
					case v.Optional:
						vrows = append(vrows, []string{"  " + opts.Styler.Optional("optional"), n, "(omitted when unset)"})
					default:
						missing++
						vrows = append(vrows, []string{"  " + opts.Styler.Missing("MISSING"), n,
							fmt.Sprintf("-> trenvy -p %s set %s <value>", app.Profile.Name, n)})
					}
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
				switch {
				case ok:
					vrows = append(vrows, []string{"  " + opts.Styler.Stored("present"), n})
				case v.Optional:
					vrows = append(vrows, []string{"  " + opts.Styler.Optional("optional"), n, "(omitted when unset)"})
				default:
					missing++
					vrows = append(vrows, []string{"  " + opts.Styler.Missing("MISSING"), n,
						fmt.Sprintf("-> trenvy -p %s secret set %s", app.Profile.Name, n)})
				}
			}
			if _, err := io.WriteString(out, style.Columns(vrows, 2)); err != nil {
				return err
			}

			fmt.Fprintln(out, "\noutputs")
			var orows [][]string
			for _, name := range sortedOutputNames(app.Manifest) {
				o := app.Manifest.Outputs[name]
				state := describeOutput(opts, app, name, o, missing > 0, quick)
				orows = append(orows, []string{"  " + name, o.Path, state})
			}
			_, err = io.WriteString(out, style.Columns(orows, 2))
			return err
		},
	}
	c.Flags().BoolVar(&quick, "quick", false,
		"skip reading secret values; freshness for an output with at least one secret is reported as unknown")
	return c
}

// outputState is the typed classification behind describeOutput's prose. It
// mirrors guard.Status's five real answers (Fresh, Absent, Stale, Modified,
// Foreign) plus three ways status could not produce one at all.
//
// It exists because describeOutput's return value is prose for a terminal --
// "not rendered", "unknown (--quick)" -- and a second consumer of the same
// classification (ui.go's outputStates) parsing that prose as if it were a
// stable machine interface is exactly how this package and internal/ui went
// out of sync twice in one task: once over "unknown (--quick)", which a
// prefix match happened to catch, and once over "not rendered" vs
// ui.Problems' "absent", which no prefix match would have. classifyOutput is
// the one place that decides; describeOutput and outputStates each render
// that decision into their own vocabulary and never need to agree with each
// other by spelling.
type outputState int

const (
	outputFresh outputState = iota
	outputAbsent
	outputStale
	outputModified
	outputForeign
	// outputSecretsMissing: a required secret is missing under the active
	// profile, so resolve.Values would error instead of producing a value to
	// hash -- there is nothing to compare against the render record.
	outputSecretsMissing
	// outputQuickSkipped: the file exists, is not hand-edited and has a
	// render record -- so it is not outputAbsent, outputForeign or
	// outputModified -- but --quick (or ui.go's always-quick freshness pass)
	// deliberately did not read the secret this output selects, and telling
	// fresh from stale needs that value. Absence, a foreign file and a hand
	// edit are all decided from the file and the state record alone, with no
	// value required; only this one distinction is actually unknown.
	outputQuickSkipped
	// outputErrored: guard.Inspect or resolve.Values itself failed; the
	// detail is already written to opts.Stderr by classifyOutput.
	outputErrored
)

// classifyOutput decides one output's state. Freshness is an HMAC over the
// resolved values, so detecting a keyring change fundamentally requires
// reading it -- that read is status's default behaviour and its whole
// purpose. --quick skips it: any output selecting at least one secret then
// reports its freshness as unknown rather than paying for the read.
func classifyOutput(opts *Options, app *appContext, name string, o manifest.Output, secretsMissing, quick bool) outputState {
	abs := filepath.Join(app.Worktree, o.Path)
	entry, have := app.State.Find(app.Worktree, name)

	if secretsMissing {
		st, err := guard.Inspect(abs, "", entry, have)
		if err != nil {
			fmt.Fprintf(opts.Stderr, "trenvy: status: %s: %v\n", name, err)
			return outputErrored
		}
		if st == guard.StatusAbsent {
			return outputAbsent
		}
		return outputSecretsMissing
	}

	if quick {
		hasSecret, err := resolve.OutputHasSecret(app.Manifest, name)
		if err != nil {
			fmt.Fprintf(opts.Stderr, "trenvy: status: %s: %v\n", name, err)
			return outputErrored
		}
		if hasSecret {
			// Absence and "a file exists that we have no record of" are both
			// decidable from the file and the state record alone, with no
			// secret value needed -- this used to skip straight to
			// outputQuickSkipped without ever calling guard.Inspect, so an
			// output that had simply never been rendered (the ordinary state
			// of a fresh clone) was indistinguishable from a genuinely stale
			// one. Passing "" as wantSum mirrors the secretsMissing branch
			// above: entry.Sum is never actually "" for a real render, so
			// the one case this cannot rule out (the file matches its own
			// record, and only fresh-versus-stale remains) still correctly
			// falls through to outputQuickSkipped below.
			st, err := guard.Inspect(abs, "", entry, have)
			if err != nil {
				fmt.Fprintf(opts.Stderr, "trenvy: status: %s: %v\n", name, err)
				return outputErrored
			}
			switch st {
			case guard.StatusAbsent:
				return outputAbsent
			case guard.StatusForeign:
				return outputForeign
			case guard.StatusModified:
				return outputModified
			default:
				return outputQuickSkipped
			}
		}
	}

	values, err := resolve.Values(app.Manifest, app.Profile.Name, name, app.secretLookup())
	if err != nil {
		fmt.Fprintf(opts.Stderr, "trenvy: status: %s: %v\n", name, err)
		return outputErrored
	}
	st, err := guard.Inspect(abs, render.Sum(app.Manifest.Project.Salt, values), entry, have)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "trenvy: status: %s: %v\n", name, err)
		return outputErrored
	}
	switch st {
	case guard.StatusAbsent:
		return outputAbsent
	case guard.StatusFresh:
		return outputFresh
	case guard.StatusStale:
		return outputStale
	case guard.StatusModified:
		return outputModified
	case guard.StatusForeign:
		return outputForeign
	default:
		// guard.Status has exactly the five cases above; reaching this means
		// that enum grew a case this switch does not know about.
		return outputErrored
	}
}

// describeOutput renders classifyOutput's decision as the prose status
// prints, coloured through opts.Styler. Its words are pinned to what they
// were before classifyOutput existed -- this function is a formatter now, not
// a decision, and status's text must not move by one byte just because the
// decision moved to its own type. Colour wraps the words and never replaces
// them, so a plain Styler reproduces the old output exactly.
func describeOutput(opts *Options, app *appContext, name string, o manifest.Output, secretsMissing, quick bool) string {
	switch classifyOutput(opts, app, name, o, secretsMissing, quick) {
	case outputAbsent:
		return opts.Styler.Absent("not rendered")
	case outputFresh:
		return opts.Styler.Fresh(guard.StatusFresh.String())
	case outputStale:
		return opts.Styler.Stale(guard.StatusStale.String())
	case outputModified:
		return opts.Styler.Modified(guard.StatusModified.String())
	case outputForeign:
		return opts.Styler.Foreign(guard.StatusForeign.String())
	case outputSecretsMissing:
		return opts.Styler.Unknown("unknown (a secret is missing)")
	case outputQuickSkipped:
		return opts.Styler.Unknown("unknown (--quick)")
	default: // outputErrored
		return "?"
	}
}

func sortedOutputNames(m *manifest.Manifest) []string {
	out := make([]string, 0, len(m.Outputs))
	for n := range m.Outputs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
