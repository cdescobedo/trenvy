package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/trenvy/internal/config"
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
		newOutputAddCmd(opts),
		newOutputDefaultCmd(opts),
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

// requireValidOutputName reports a bad name as the argument problem it is.
//
// Manifest.Validate enforces the same rule, but by the time it runs the name is
// already a manifest key, so its message reads as "your config file is broken"
// when the user simply mistyped an argument. Both paths call ValidOutputName, so
// this is one rule with two call sites, not two rules.
func requireValidOutputName(name string) error {
	if manifest.ValidOutputName(name) {
		return nil
	}
	return fmt.Errorf("%q: output names may use letters, digits, - and _ only, "+
		"and may not begin with -", name)
}

func newOutputAddCmd(opts *Options) *cobra.Command {
	var format, path string
	var include, exclude []string

	c := &cobra.Command{
		Use:   "add <name>",
		Short: "Define a new output",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name := args[0]
			if err := requireValidOutputName(name); err != nil {
				return err
			}
			if _, exists := app.Manifest.Outputs[name]; exists {
				return fmt.Errorf("output %q is already defined", name)
			}
			if other, exists := outputAtPath(app.Manifest, path); exists {
				return fmt.Errorf("output %q already renders to %s; "+
					"two outputs on one path overwrite each other", other, path)
			}
			if app.Manifest.Outputs == nil {
				app.Manifest.Outputs = map[string]manifest.Output{}
			}
			app.Manifest.Outputs[name] = manifest.Output{
				Format: format, Path: path, Include: include, Exclude: exclude,
			}
			// Validate before saving: the name, path and format rules all
			// live in Validate, so an illegal output never reaches disk.
			if err := app.Manifest.Validate(); err != nil {
				return err
			}
			if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
				return err
			}
			fmt.Fprintf(opts.Stdout, "added output %s  %s -> %s\n", name, format, path)
			if app.Manifest.Project.Output == "" {
				fmt.Fprintf(opts.Stdout,
					"  make it the default with: trenvy output default %s\n", name)
			}
			return nil
		},
	}
	c.Flags().StringVar(&format, "format", "", "dotenv or json")
	c.Flags().StringVar(&path, "path", "", "path within a worktree")
	c.Flags().StringSliceVar(&include, "include", nil, "globs to include")
	c.Flags().StringSliceVar(&exclude, "exclude", nil, "globs to exclude")
	_ = c.MarkFlagRequired("format")
	_ = c.MarkFlagRequired("path")
	return c
}

func newOutputDefaultCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:               "default <name>",
		Short:             "Set the output render uses when given no argument",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: opts.completeOutputNames(),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name := args[0]
			if _, ok := app.Manifest.Outputs[name]; !ok {
				return fmt.Errorf("no output named %q; list them with: trenvy output list", name)
			}
			app.Manifest.Project.Output = name
			if err := app.Manifest.Validate(); err != nil {
				return err
			}
			if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
				return err
			}
			fmt.Fprintf(opts.Stdout, "default output is now %s\n", name)
			return nil
		},
	}
}
