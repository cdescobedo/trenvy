package cli

import (
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
)

func newProfileCmd(opts *Options) *cobra.Command {
	// The parent runs on its own: "which profile am I on, and why" is one
	// question, and making it a subcommand would split it in two.
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Show the active profile; manage the project's profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			fmt.Fprintf(opts.Stdout, "%s  (%s)\n", app.Profile.Name, app.Profile.Source)
			return nil
		},
	}
	cmd.AddCommand(newProfileListCmd(opts), newProfileAddCmd(opts))
	return cmd
}

func newProfileListCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the project's profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			names := append([]string(nil), app.Manifest.Project.Profiles...)
			sort.Strings(names)

			w := tabwriter.NewWriter(opts.Stdout, 0, 0, 2, ' ', 0)
			for _, n := range names {
				marker := " "
				if n == app.Profile.Name {
					marker = "*"
				}
				note := ""
				if n == app.Manifest.Project.DefaultProfile {
					note = "(default)"
				}
				fmt.Fprintf(w, "%s %s\t%s\n", marker, n, note)
			}
			return w.Flush()
		},
	}
}

func newProfileAddCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "add <name>",
		Short: "Declare a new profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name := args[0]
			for _, p := range app.Manifest.Project.Profiles {
				if p == name {
					return fmt.Errorf("profile %q is already declared", name)
				}
			}
			app.Manifest.Project.Profiles = append(app.Manifest.Project.Profiles, name)
			// Validate before saving so an illegal name -- one that would
			// reshape the keyring namespace -- never reaches disk.
			if err := app.Manifest.Validate(); err != nil {
				return err
			}
			if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
				return err
			}
			fmt.Fprintf(opts.Stdout, "added profile %s\n", name)
			fmt.Fprintf(opts.Stdout, "  its secrets are empty; store them with: envctl -p %s secret set <NAME>\n", name)
			return nil
		},
	}
}
