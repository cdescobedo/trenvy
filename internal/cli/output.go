package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/ui"
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
		newOutputRenameCmd(opts),
		newOutputRmCmd(opts),
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
					marked, o.Format, o.Path, ui.SelectsSummary(o))
			}
			return nil
		},
	}
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

func newOutputRenameCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:               "rename <old> <new>",
		Short:             "Rename an output, keeping its render records",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: opts.completeOutputNames(),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			old, name := args[0], args[1]
			o, ok := app.Manifest.Outputs[old]
			if !ok {
				return fmt.Errorf("no output named %q; list them with: trenvy output list", old)
			}
			if err := requireValidOutputName(name); err != nil {
				return err
			}
			if _, taken := app.Manifest.Outputs[name]; taken {
				return fmt.Errorf("output %q is already defined", name)
			}

			// State first, then the manifest. Either order leaves the files
			// FOREIGN if the second write fails, but only this one is
			// recoverable by re-running the same command: the old name is
			// still defined, so a second run rebases zero records, renames
			// the key, and the two files agree again. Manifest-first leaves
			// the old name undefined and this command can no longer find it.
			moved := app.State.RenameOutput(old, name)
			if err := app.State.Save(); err != nil {
				return err
			}
			delete(app.Manifest.Outputs, old)
			app.Manifest.Outputs[name] = o
			if app.Manifest.Project.Output == old {
				app.Manifest.Project.Output = name
			}
			if err := app.Manifest.Validate(); err != nil {
				return err
			}
			if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
				return err
			}

			fmt.Fprintf(opts.Stdout, "renamed output %s to %s\n", old, name)
			if moved > 0 {
				fmt.Fprintf(opts.Stdout, "  %d render record(s) repointed\n", moved)
			}
			return nil
		},
	}
}

// newOutputRmCmd removes an output.
//
// --yes and --force answer two different questions and do not substitute for
// each other: --yes answers "are you sure", --force answers "drop the render
// records that still point at real files". Collapsing them into one flag
// would let an unattended run silently orphan files it can no longer reach
// with `trenvy clean`, since Forget deletes the very record clean needs.
func newOutputRmCmd(opts *Options) *cobra.Command {
	var yes, force bool

	c := &cobra.Command{
		Use:               "rm <name>",
		Short:             "Remove an output",
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

			var rendered []config.RenderEntry
			for _, e := range app.State.Renders {
				if e.Output == name {
					rendered = append(rendered, e)
				}
			}
			if len(rendered) > 0 && !force {
				var b strings.Builder
				fmt.Fprintf(&b, "output %q has been rendered in %d worktree(s):\n", name, len(rendered))
				for _, e := range rendered {
					fmt.Fprintf(&b, "  %s\n", e.Worktree)
				}
				b.WriteString("  remove the files first: trenvy clean " + name + "  (in each worktree)\n")
				b.WriteString("  or drop the records and leave the files: trenvy output rm " + name + " --force")
				return errors.New(b.String())
			}

			if !yes {
				ok, err := confirm(opts, fmt.Sprintf("Remove output %q?", name))
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("aborted")
				}
			}

			// State first, then the manifest, for the reason given in rename:
			// re-running after a failed manifest write still finds the name
			// and Forget is idempotent, but a manifest write that lands first
			// leaves the name undefined and this command can no longer find
			// the records to drop.
			for _, e := range rendered {
				app.State.Forget(e.Worktree, name)
			}
			if len(rendered) > 0 {
				if err := app.State.Save(); err != nil {
					return err
				}
			}
			delete(app.Manifest.Outputs, name)
			clearedDefault := app.Manifest.Project.Output == name
			if clearedDefault {
				app.Manifest.Project.Output = ""
			}
			if err := app.Manifest.Validate(); err != nil {
				return err
			}
			if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
				return err
			}

			fmt.Fprintf(opts.Stdout, "removed output %s\n", name)
			for _, e := range rendered {
				fmt.Fprintf(opts.Stdout, "  left in place: %s/%s\n", e.Worktree, e.Path)
			}
			if clearedDefault {
				fmt.Fprintf(opts.Stdout,
					"  it was the default; bare `trenvy render` now needs an output argument\n")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	c.Flags().BoolVar(&force, "force", false,
		"drop render records and leave the rendered files in place")
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
