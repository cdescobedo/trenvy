package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/resolve"
	"github.com/cdescobedo/trenvy/internal/store"
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
				// A row with no note omits the tab entirely -- a trailing tab
				// before an empty cell would still be padded to the column
				// width by tabwriter, leaving trailing whitespace on the line.
				if note == "" {
					fmt.Fprintf(w, "%s %s\n", marker, n)
				} else {
					fmt.Fprintf(w, "%s %s\t%s\n", marker, n, note)
				}
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
			fmt.Fprintf(opts.Stdout, "  its secrets are empty; store them with: trenvy -p %s secret set <NAME>\n", name)
			return nil
		},
	}
}

// newProfileUseCmd sets this machine's active profile for the project,
// writing it to the state file (config.State.Active) so every later command
// against this project on this machine -- a new terminal, a script, a
// different tab -- picks it up, not just the shell that ran this one.
//
// It writes nothing to stdout. Keeping stdout empty is what makes
// `eval "$(trenvy profile use x)"`, if anyone still has that in a script
// from before this command persisted anything, a harmless no-op instead of
// executing arbitrary confirmation text as shell commands. Everything this
// command reports -- confirmation and warning alike -- goes to stderr
// instead, which `$(...)` never captures, so it reaches the terminal whether
// or not the caller still wraps the command in eval.
//
// TRENVY_PROFILE outranks the active selection this command just wrote (see
// resolve.Profile's precedence), so when it is set this says so and names
// the fix. Without that, someone who still has it exported would see `use`
// appear to do nothing -- twice over, since their own stale export is both
// the cause and, on stdout alone, invisible.
func newProfileUseCmd(opts *Options) *cobra.Command {
	var clear bool

	cmd := &cobra.Command{
		Use:   "use <name>",
		Short: "Set this machine's active profile for the project",
		Long: "Set this machine's active profile for the project, persisting it to\n" +
			"the project's state file. It applies to every later command run against\n" +
			"this project from this machine, not just the current shell.\n\n" +
			"trenvy profile use --clear forgets the selection, falling back to\n" +
			"TRENVY_PROFILE or the manifest's default_profile.\n\n" +
			"To change the project's default instead, use: trenvy profile default <name>",
		Args: func(cmd *cobra.Command, args []string) error {
			if clear {
				if len(args) != 0 {
					return errors.New("--clear takes no profile name")
				}
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		ValidArgsFunction: opts.completeProfileNames(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if clear {
				return clearActiveProfile(opts)
			}

			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name := args[0]
			if err := requireDeclaredProfile(app, name); err != nil {
				return err
			}

			app.State.Active = name
			if err := app.State.Save(); err != nil {
				return err
			}

			fmt.Fprintf(opts.Stderr, "trenvy: active profile is now %s\n", name)
			warnIfProfileEnvVarOverrides(opts)
			return nil
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "clear this machine's active profile selection")
	return cmd
}

// clearActiveProfile empties the state file's Active field.
//
// It resolves the project through resolveActiveEntry, not resolveApp: an
// active profile the manifest no longer declares (a hand edit, or a state
// file left behind by a version of `profile rm` that did not clear Active)
// makes resolveApp's resolve.Profile call refuse to load at all -- and
// repairing exactly that is this command's whole purpose. Routing --clear
// through resolveApp would make the escape hatch unreachable in precisely
// the situation it exists for.
func clearActiveProfile(opts *Options) error {
	entry, err := resolveActiveEntry(opts)
	if err != nil {
		return err
	}
	st, err := config.LoadState(entry.ID)
	if err != nil {
		return err
	}
	st.Active = ""
	if err := st.Save(); err != nil {
		return err
	}
	fmt.Fprintln(opts.Stderr, "trenvy: active profile selection cleared")
	warnIfProfileEnvVarOverrides(opts)
	return nil
}

// resolveActiveEntry finds the project registered at opts.Dir well enough to
// read and write its state file, stopping short of resolving the active
// profile the way resolveApp does. See clearActiveProfile for why that
// matters.
func resolveActiveEntry(opts *Options) (config.Entry, error) {
	reg, err := config.LoadRegistry()
	if err != nil {
		return config.Entry{}, err
	}
	entry, ok := reg.Lookup(opts.Dir)
	if !ok {
		return config.Entry{}, fmt.Errorf(
			"%s is not inside a registered trenvy project\n  register one with: trenvy project init", opts.Dir)
	}
	return entry, nil
}

// warnIfProfileEnvVarOverrides tells the user when TRENVY_PROFILE is set in
// the environment: it outranks whatever `use` or `use --clear` just wrote to
// the state file (see resolve.Profile's precedence), so without this, the
// change looks like it did nothing until the shell holding that export exits.
func warnIfProfileEnvVarOverrides(opts *Options) {
	if v := os.Getenv(resolve.ProfileEnvVar); v != "" {
		fmt.Fprintf(opts.Stderr,
			"trenvy: warning: %s=%s is still set in this shell and outranks this change;\n"+
				"  run: unset %s\n", resolve.ProfileEnvVar, v, resolve.ProfileEnvVar)
	}
}

func newProfileDefaultCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:               "default <name>",
		Short:             "Set the project's default profile",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: opts.completeProfileNames(),
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
		Use:               "rm <name>",
		Short:             "Remove a profile, its overrides and its secrets",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: opts.completeProfileNames(),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name := args[0]
			if err := requireDeclaredProfile(app, name); err != nil {
				return err
			}

			// Three refusals, checked before anything is touched. The last
			// profile is a flat no -- there is no command that makes it
			// removable -- while the other two name what has to change first.
			if len(app.Manifest.Project.Profiles) == 1 {
				return fmt.Errorf(
					"%s is the last profile; a project with none cannot resolve anything", name)
			}
			if name == app.Manifest.Project.DefaultProfile {
				return fmt.Errorf(
					"%s is the default profile; point it elsewhere first: trenvy profile default <other>",
					name)
			}
			if name == app.Profile.Name {
				return fmt.Errorf(
					"%s is the active profile (from %s); escape it with -p <other>, or: trenvy profile use --clear",
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
			// deleted counts only items actually removed -- most declared
			// secrets are never stored under every profile, so ErrNotFound
			// on an individual item is the ordinary case, not a failure, and
			// must not inflate the count reported to the user.
			deleted := 0
			if len(secrets) > 0 {
				s, err := app.store()
				if err != nil {
					return err
				}
				for _, n := range secrets {
					svc := app.Cfg.Keyring.ServiceName(app.Manifest.Project.ID, name, n)
					if err := s.Delete(svc); err != nil {
						if errors.Is(err, store.ErrNotFound) {
							continue
						}
						// Nothing below this has run yet, so the profile is
						// still fully declared and this command is safe to
						// re-run once the underlying store problem is fixed.
						// svc (the full service name), not n (the bare
						// variable), matches what unset and project rm name
						// in the equivalent error; and the count of secrets
						// already gone permanently is stated rather than
						// implied to be zero.
						return fmt.Errorf(
							"deleting %s: %w; %d secrets were already deleted, profile %s was not removed, re-run to finish",
							svc, err, deleted, name)
					}
					deleted++
				}
			}

			// A fresh slice, not an in-place filter over the same backing
			// array: this is the most destructive function in the package,
			// and a fresh allocation is one less aliasing hazard for anyone
			// auditing it later.
			profiles := make([]string, 0, len(app.Manifest.Project.Profiles)-1)
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

			// The refusal above only catches the profile currently resolved
			// as active; a --profile or TRENVY_PROFILE override lets rm
			// proceed while State.Active still names the one just removed.
			// Left alone, that is precisely the dangling reference `profile
			// use --clear` exists to escape -- reachable here with no hand
			// edit at all, so it is closed in the same operation rather than
			// left for someone to discover the next time every command fails.
			clearedActive := false
			if app.State.Active == name {
				app.State.Active = ""
				if err := app.State.Save(); err != nil {
					return fmt.Errorf(
						"profile %s removed but clearing the active selection failed: %w; run: trenvy profile use --clear",
						name, err)
				}
				clearedActive = true
			}

			fmt.Fprintf(opts.Stdout, "removed profile %s (%d secrets deleted)\n", name, deleted)
			if clearedActive {
				fmt.Fprintln(opts.Stdout, "  it was this machine's active profile; that selection is now cleared")
			}
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
