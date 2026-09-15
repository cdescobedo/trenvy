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

// unsetValue marks a variable the output selects but that resolves to nothing.
const unsetValue = "(unset)"

func newGetCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "get <NAME> [output]",
		Short: "Print one variable's value",
		Long: "Print one variable's value, secret or plain, and nothing else.\n\n" +
			"Only the named variable is resolved, so reading one value costs at most\n" +
			"one keyring read and does not depend on the output's other secrets\n" +
			"being stored.",
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: opts.completeVarNames(anyVar),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name := args[0]
			output, err := app.outputOrDefault(secondOrEmpty(args))
			if err != nil {
				return err
			}

			// Three ways to have no value, each with its own fix: the name is
			// unknown, the output does not select it, or it is optional and
			// unset under the active profile. Collapsing them into one
			// message would send the reader looking in the wrong place.
			if _, ok := app.Manifest.Vars[name]; !ok {
				return fmt.Errorf("%s is not in the manifest of %s", name, app.Entry.ID)
			}
			sel, err := resolve.Selects(app.Manifest, output, name)
			if err != nil {
				return err
			}
			if !sel {
				return fmt.Errorf("%s is not included in output %q", name, output)
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
		Short: "List an output's variables, with secrets masked unless revealed",
		Args:  cobra.MaximumNArgs(1),
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
			output, err := app.outputOrDefault(firstOrEmpty(args))
			if err != nil {
				return err
			}

			// A masked dotenv file is a file full of bullets, so asking for a
			// format is asking for the values.
			if format != "" {
				reveal = true
			}
			if !reveal {
				return showMasked(opts, app, output)
			}

			values, err := resolve.Values(app.Manifest, app.Profile.Name, output, app.secretLookup())
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

// showMasked lists the output's variables without reading a single secret.
// The store is never opened: a masked table has no use for a value, and on a
// prompting backend reading one per secret just to print bullets would make
// `envctl show` cost more than `envctl render`.
func showMasked(opts *Options, app *appContext, output string) error {
	names := make([]string, 0, len(app.Manifest.Vars))
	for name := range app.Manifest.Vars {
		sel, err := resolve.Selects(app.Manifest, output, name)
		if err != nil {
			return err
		}
		if sel {
			names = append(names, name)
		}
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

func secondOrEmpty(args []string) string {
	if len(args) > 1 {
		return args[1]
	}
	return ""
}
