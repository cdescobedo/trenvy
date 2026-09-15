package cli

import (
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/render"
	"github.com/cdescobedo/envctl/internal/resolve"
)

// maskedSecret stands in for a secret's value in a table that was not asked
// to reveal anything. It carries the tag so a reader can tell a masked secret
// from a plain value that happens to look like bullets.
const maskedSecret = "••••••••  (keyring)"

// unsetValue marks a variable that is listed but resolves to nothing under
// the active profile. With no output named nothing does any selecting, so the
// row belongs to the profile, not to an output.
const unsetValue = "(unset)"

func newGetCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "get <NAME>",
		Short: "Print one variable's value",
		Long: "Print one variable's value, secret or plain, and nothing else.\n\n" +
			"Only the named variable is resolved, so reading one value costs at most\n" +
			"one keyring read and does not depend on any other secret being stored.\n\n" +
			"An output decides a file's format, path and audience -- none of which\n" +
			"mean anything for a single value printed to stdout, and naming a\n" +
			"variable explicitly makes an output's subset irrelevant. The profile\n" +
			"is the only axis left that affects what a value resolves to, so get\n" +
			"takes exactly one argument.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: opts.completeVarNames(anyVar),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name := args[0]

			// Two ways to have no value, each with its own fix: the name is
			// unknown, or it is optional and unset under the active profile.
			// Collapsing them into one message would send the reader looking
			// in the wrong place.
			if _, ok := app.Manifest.Vars[name]; !ok {
				return fmt.Errorf("%s is not in the manifest of %s", name, app.Entry.ID)
			}

			value, ok, err := resolve.Value(app.Manifest, app.Profile.Name, name, app.secretLookup())
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%s is optional and has no value for profile %q", name, app.Profile.Name)
			}

			// The value and a newline, on stdout, alone: $(envctl get X) and
			// `envctl get X | pbcopy` both depend on nothing else being here.
			fmt.Fprintln(opts.Stdout, value)
			return nil
		},
	}
}

func newShowCmd(opts *Options) *cobra.Command {
	var reveal bool
	var format string

	c := &cobra.Command{
		Use:   "show [output]",
		Short: "List the profile's variables, or one output's subset, secrets masked",
		Long: "List variables and their values, with secrets masked unless --reveal.\n\n" +
			"With no argument, show lists every variable the active profile resolves.\n" +
			"\"What are my values\" is a question about the profile; an output is a\n" +
			"filter on that answer, not a precondition for asking it, so a project\n" +
			"with no outputs at all can still be inspected.\n\n" +
			"Naming an output restricts the list to what that output selects.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Reject a bad format before anything is resolved, so the message
			// is about the flag rather than about the first variable that
			// happens to be unset.
			switch format {
			case "", manifest.FormatDotenv, manifest.FormatJSON:
			default:
				return fmt.Errorf("unsupported format %q; use dotenv or json", format)
			}

			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			// Deliberately not outputOrDefault: project.output says where
			// render's bytes land, and show writes nothing. Inheriting that
			// narrowing would make a bare `show` answer for one file rather
			// than for the project. An empty output means the whole profile.
			output := firstOrEmpty(args)
			if output != "" {
				if _, ok := app.Manifest.Outputs[output]; !ok {
					return fmt.Errorf("%w: %q", resolve.ErrUnknownOutput, output)
				}
			}

			// A masked dotenv file is a file full of bullets, so asking for a
			// format is asking for the values.
			if format != "" {
				reveal = true
			}
			if !reveal {
				return showMasked(opts, app, output)
			}

			values, err := resolveForShow(app, output)
			if err != nil {
				return err
			}
			switch format {
			case manifest.FormatDotenv:
				data, err := render.Dotenv(values)
				if err != nil {
					return err
				}
				_, err = opts.Stdout.Write(data)
				return err
			case manifest.FormatJSON:
				data, err := render.JSON(values)
				if err != nil {
					return err
				}
				_, err = opts.Stdout.Write(data)
				return err
			}

			w := tabwriter.NewWriter(opts.Stdout, 0, 0, 2, ' ', 0)
			for _, name := range values.Names {
				fmt.Fprintf(w, "%s\t%s\n", name, values.Values[name])
			}
			return w.Flush()
		},
	}
	c.Flags().BoolVar(&reveal, "reveal", false, "print secret values instead of masking them")
	c.Flags().StringVar(&format, "format", "",
		"emit dotenv or json on stdout instead of a table; implies --reveal")
	return c
}

// resolveForShow resolves the whole profile when no output was named, or just
// that output's subset otherwise. It is the read half of show's rule that an
// output filters the answer rather than gating the question.
func resolveForShow(app *appContext, output string) (resolve.Resolved, error) {
	if output == "" {
		return resolve.All(app.Manifest, app.Profile.Name, app.secretLookup())
	}
	return resolve.Values(app.Manifest, app.Profile.Name, output, app.secretLookup())
}

// showMasked lists the variables without reading a single secret, narrowed to
// one output only when one was named. The store is never opened: a masked
// table has no use for a value, and on a prompting backend reading one per
// secret just to print bullets would make `envctl show` cost more than
// `envctl render`.
func showMasked(opts *Options, app *appContext, output string) error {
	names := make([]string, 0, len(app.Manifest.Vars))
	for name := range app.Manifest.Vars {
		if output != "" {
			sel, err := resolve.Selects(app.Manifest, output, name)
			if err != nil {
				return err
			}
			if !sel {
				continue
			}
		}
		names = append(names, name)
	}
	sort.Strings(names)

	w := tabwriter.NewWriter(opts.Stdout, 0, 0, 2, ' ', 0)
	for _, name := range names {
		fmt.Fprintf(w, "%s\t%s\n", name, maskedDisplay(app, app.Profile.Name, name))
	}
	return w.Flush()
}

// maskedDisplay is what one variable looks like with secrets withheld. The
// profile is an explicit parameter rather than read off app, because it is
// the axis that decides the value: an output only ever decides which
// variables are selected, never what a selected one is worth.
func maskedDisplay(app *appContext, profile, name string) string {
	v := app.Manifest.Vars[name]
	if v.Secret {
		return maskedSecret
	}
	// Secrets are handled above and both the profile and the name are known
	// good here, so the only error resolve.Value can still return for a plain
	// variable is "required and unset" -- which is a row to print, not a
	// reason to hide every other variable.
	value, ok, err := resolve.Value(app.Manifest, profile, name, nil)
	if err != nil || !ok {
		return unsetValue
	}
	return value
}
