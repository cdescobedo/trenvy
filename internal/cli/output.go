package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/trenvy/internal/manifest"
)

// newOutputCmd groups the commands that manage a project's outputs.
//
// It mirrors the profile group deliberately. The two are the tool's two axes
// -- an output decides where values go, a profile decides what they are -- and
// a user who has learned one should not have to learn the other.
func newOutputCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "output",
		Short: "List and manage this project's outputs",
	}
	cmd.AddCommand(
		newOutputListCmd(opts),
	)
	return cmd
}

func newOutputListCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the project's outputs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			if len(app.Manifest.Outputs) == 0 {
				fmt.Fprintf(opts.Stdout,
					"no outputs; exec and shell need none.\n"+
						"  add one with: trenvy output add <name> --format dotenv --path <path>\n")
				return nil
			}
			names := make([]string, 0, len(app.Manifest.Outputs))
			for name := range app.Manifest.Outputs {
				names = append(names, name)
			}
			sort.Strings(names)

			fmt.Fprintf(opts.Stdout, "%-12s %-8s %-32s %s\n", "NAME", "FORMAT", "PATH", "SELECTS")
			for _, name := range names {
				o := app.Manifest.Outputs[name]
				marked := name
				if name == app.Manifest.Project.Output {
					marked = name + " *"
				}
				fmt.Fprintf(opts.Stdout, "%-12s %-8s %-32s %s\n",
					marked, o.Format, o.Path, selectsSummary(o))
			}
			return nil
		},
	}
}

// selectsSummary renders an output's globs as prose, because the globs
// themselves do not say which of the two futures applies: whether a variable
// added tomorrow lands in this file or stays out of it.
//
// Include and Exclude are independent in resolve.selected -- include decides
// membership, then exclude is subtracted -- so the exclusions are appended
// whatever the include list says. Reporting only the include list would tell
// a reader a variable reaches this file when resolve keeps it out.
func selectsSummary(o manifest.Output) string {
	sel := "all"
	if len(o.Include) > 0 && !(len(o.Include) == 1 && o.Include[0] == "*") {
		sel = strings.Join(o.Include, ", ")
	}
	if len(o.Exclude) > 0 {
		sel += " except " + strings.Join(o.Exclude, ", ")
	}
	return sel
}
