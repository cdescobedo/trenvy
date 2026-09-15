package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/resolve"
	"github.com/cdescobedo/envctl/internal/store"
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
	cmd.AddCommand(
		newProfileListCmd(opts), newProfileAddCmd(opts),
		newProfileUseCmd(opts), newProfileDefaultCmd(opts), newProfileRmCmd(opts),
	)
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

// newProfileUseCmd switches the profile for one shell session.
//
// A child process cannot mutate its parent shell's environment, so this emits
// an export for the shell to evaluate rather than pretending to set anything.
// Stdout carries the export and nothing else, so `eval "$(...)"` is safe; the
// hint for someone who ran it bare goes to stderr, where an eval cannot see it.
func newProfileUseCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Emit a shell export switching profile for this session",
		Long: "Emit a shell export switching profile for this session.\n\n" +
			"Use it as:  eval \"$(envctl profile use prod)\"\n\n" +
			"To change the project's default instead, use: envctl profile default <name>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name := args[0]
			if err := requireDeclaredProfile(app, name); err != nil {
				return err
			}
			fmt.Fprintf(opts.Stdout, "export %s=%s\n", resolve.ProfileEnvVar, name)
			if isTerminal(opts.Stdout) {
				fmt.Fprintf(opts.Stderr,
					"envctl: nothing changed -- run it as: eval \"$(envctl profile use %s)\"\n", name)
			}
			return nil
		},
	}
}

func newProfileDefaultCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "default <name>",
		Short: "Set the project's default profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name := args[0]
			if err := requireDeclaredProfile(app, name); err != nil {
				return err
			}
			app.Manifest.Project.DefaultProfile = name
			if err := app.Manifest.Validate(); err != nil {
				return err
			}
			if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
				return err
			}
			fmt.Fprintf(opts.Stdout, "default profile is now %s\n", name)
			return nil
		},
	}
}

func newProfileRmCmd(opts *Options) *cobra.Command {
	var yes bool

	c := &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a profile, its overrides and its secrets",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name := args[0]
			if err := requireDeclaredProfile(app, name); err != nil {
				return err
			}

			// Three refusals, each naming the command that unblocks it.
			if len(app.Manifest.Project.Profiles) == 1 {
				return fmt.Errorf(
					"%s is the last profile; a project with none cannot resolve anything", name)
			}
			if name == app.Manifest.Project.DefaultProfile {
				return fmt.Errorf(
					"%s is the default profile; point it elsewhere first: envctl profile default <other>",
					name)
			}
			if name == app.Profile.Name {
				return fmt.Errorf(
					"%s is the active profile (from %s); run this from another profile",
					name, app.Profile.Source)
			}

			secrets := secretNames(app.Manifest)
			if !yes {
				ok, err := confirm(opts, fmt.Sprintf(
					"Remove profile %q and delete %d secrets stored under it?", name, len(secrets)))
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("aborted")
				}
			}

			// Keyring first: the manifest is the only inventory of which items
			// exist, so destroying it before the items would strand them.
			if len(secrets) > 0 {
				s, err := app.store()
				if err != nil {
					return err
				}
				for _, n := range secrets {
					svc := app.Cfg.Keyring.ServiceName(app.Manifest.Project.ID, name, n)
					if err := s.Delete(svc); err != nil && !errors.Is(err, store.ErrNotFound) {
						return err
					}
				}
			}

			profiles := app.Manifest.Project.Profiles[:0]
			for _, p := range app.Manifest.Project.Profiles {
				if p != name {
					profiles = append(profiles, p)
				}
			}
			app.Manifest.Project.Profiles = profiles
			for vn, v := range app.Manifest.Vars {
				if _, ok := v.Profiles[name]; ok {
					delete(v.Profiles, name)
					app.Manifest.Vars[vn] = v
				}
			}
			if err := app.Manifest.Validate(); err != nil {
				return err
			}
			if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
				return err
			}
			fmt.Fprintf(opts.Stdout, "removed profile %s (%d secrets deleted)\n", name, len(secrets))
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return c
}

func requireDeclaredProfile(app *appContext, name string) error {
	for _, p := range app.Manifest.Project.Profiles {
		if p == name {
			return nil
		}
	}
	return fmt.Errorf("%w %q; declared profiles: %s",
		resolve.ErrUnknownProfile, name, strings.Join(app.Manifest.Project.Profiles, ", "))
}

// secretNames lists every secret variable, sorted, so a count or a loop over
// keyring items is deterministic.
func secretNames(m *manifest.Manifest) []string {
	var out []string
	for name, v := range m.Vars {
		if v.Secret {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// isTerminal reports whether w is a terminal. Used only to decide whether a
// human is looking, never to change what goes to stdout.
func isTerminal(w io.Writer) bool {
	f, ok := w.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(int(f.Fd()))
}
