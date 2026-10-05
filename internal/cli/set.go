package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/render"
	"github.com/cdescobedo/trenvy/internal/resolve"
	"github.com/cdescobedo/trenvy/internal/store"
)

func newSetCmd(opts *Options) *cobra.Command {
	var shared bool

	c := &cobra.Command{
		Use:               "set <NAME> <VALUE>",
		Short:             "Set a plain value for the active profile",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: opts.completeVarNames(isPlain),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name, value := args[0], args[1]

			if existing, ok := app.Manifest.Vars[name]; ok && existing.Secret {
				// Naming the value here would print a secret-adjacent string to
				// the terminal and the shell history; the name is enough.
				// This holds for --shared too: the hazard is the channel (argv
				// and shell history), not which destination the value lands in.
				return fmt.Errorf(
					"%s is a secret; set its value with: trenvy -p %s secret set %s",
					name, app.Profile.Name, name)
			}

			v := app.Manifest.Vars[name]
			if shared {
				v.Value = &value
			} else {
				// A value written while standing on a profile belongs to that
				// profile. Writing Value instead would silently change every
				// other profile that has no override of its own.
				if v.Profiles == nil {
					v.Profiles = map[string]string{}
				}
				v.Profiles[app.Profile.Name] = value
			}
			app.Manifest.Vars[name] = v

			if err := app.Manifest.Validate(); err != nil {
				return err
			}
			if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
				return err
			}
			warnUnreachableInDotenv(opts, app.Manifest, name)
			if shared {
				// v.Profiles is untouched by the --shared branch above, so it
				// still lists exactly the profiles whose own override makes
				// them immune to the default just written -- resolve.Value
				// consults Profiles[profile] before Value. Naming them here
				// is free, and is what keeps this line from claiming an
				// effect on profiles it did not actually have.
				fmt.Fprintf(opts.Stdout, "set %s as the default for every profile without its own value\n", name)
				if len(v.Profiles) > 0 {
					overridden := make([]string, 0, len(v.Profiles))
					for p := range v.Profiles {
						overridden = append(overridden, p)
					}
					sort.Strings(overridden)
					fmt.Fprintf(opts.Stdout, "  unaffected (their own value takes precedence): %s\n",
						strings.Join(overridden, ", "))
				}
			} else {
				fmt.Fprintf(opts.Stdout, "set %s for profile %s\n", name, app.Profile.Name)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&shared, "shared", false,
		"write the default used by every profile without its own value")
	return c
}

func newSecretCmd(opts *Options) *cobra.Command {
	cmd := newGroupCmd("secret", "Manage secret values in the keyring")
	cmd.AddCommand(newSecretSetCmd(opts), newSecretRmCmd(opts), newSecretDemoteCmd(opts))
	return cmd
}

func newSecretSetCmd(opts *Options) *cobra.Command {
	var fromStdin bool

	c := &cobra.Command{
		Use:               "set <NAME>",
		Short:             "Store or rotate a secret in the keyring",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: opts.completeVarNames(isSecret),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name := args[0]

			value, err := readSecret(opts, name, fromStdin)
			if err != nil {
				return err
			}
			if value == "" {
				return fmt.Errorf("%s: empty value, nothing stored", name)
			}

			// Validate before anything is written, so a rejected manifest
			// cannot leave a secret in the keyring that nothing references.
			v := app.Manifest.Vars[name]
			v.Secret = true
			v.Value = nil // a secret never keeps a plain value
			app.Manifest.Vars[name] = v
			if err := app.Manifest.Validate(); err != nil {
				return err
			}

			// Then the keyring, and only then the manifest. The store is
			// resolved lazily, so this is also where an unusable backend (the
			// file backend with no passphrase, say) first reports itself --
			// before the manifest has been touched. Of the two ways a partial
			// write can end, an unreferenced keyring item is the benign one:
			// the manifest is the authoritative inventory, so nothing looks
			// for it. The reverse -- a manifest entry whose secret was never
			// stored -- shows up in status as MISSING and reads as data loss.
			s, err := app.store()
			if err != nil {
				return err
			}
			if err := s.Set(app.service(name), value); err != nil {
				return err
			}
			if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
				return err
			}

			// Report the length, never the value.
			fmt.Fprintf(opts.Stdout, "stored %s (%d bytes)\n", name, len(value))
			return nil
		},
	}
	c.Flags().BoolVar(&fromStdin, "stdin", false, "read the value from stdin instead of prompting")
	return c
}

// readSecret takes a value without echoing it and without putting it in argv.
func readSecret(opts *Options, name string, fromStdin bool) (string, error) {
	if fromStdin {
		data, err := io.ReadAll(opts.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		// Strip only the trailing newline a pipe adds. Surrounding whitespace
		// can be part of a secret and must survive.
		return strings.TrimRight(string(data), "\r\n"), nil
	}

	// A single assertion, not isTerminal followed by a second one to recover
	// the fd it already had and discarded: term.ReadPassword needs the fd
	// itself, not just whether it points at a terminal.
	if f, ok := opts.Stdin.(interface{ Fd() uintptr }); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprintf(opts.Stderr, "Value for %s: ", name)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(opts.Stderr)
		if err != nil {
			return "", fmt.Errorf("read value: %w", err)
		}
		return string(b), nil
	}

	line, err := bufio.NewReader(opts.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read value: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func newSecretRmCmd(opts *Options) *cobra.Command {
	var yes bool

	c := &cobra.Command{
		Use:               "rm <NAME>",
		Short:             "Delete a secret's value from the keyring, keeping the variable",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: opts.completeVarNames(isSecret),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name := args[0]
			if !yes {
				// secret rm only ever touches the active profile's item, so the
				// prompt names it -- unlike unset, which clears every profile.
				ok, err := confirm(opts, fmt.Sprintf(
					"Delete %s from the keyring for profile %q?", name, app.Profile.Name))
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("aborted")
				}
			}
			s, err := app.store()
			if err != nil {
				return err
			}
			if err := s.Delete(app.service(name)); err != nil {
				if !errors.Is(err, store.ErrNotFound) {
					return err
				}
				// Against a profile with nothing stored, this is not a
				// deletion -- and in a tool whose headline hazard is acting
				// on the wrong profile, telling the user "deleted" here
				// would read identically to a real one, when in fact
				// nothing was ever there to remove.
				fmt.Fprintf(opts.Stdout, "%s was not stored for profile %s; nothing to delete\n",
					name, app.Profile.Name)
				return nil
			}
			fmt.Fprintf(opts.Stdout, "deleted %s from the keyring\n", name)
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return c
}

// newSecretDemoteCmd turns a secret back into a plain variable. Making a
// variable secret is otherwise one-way, so this is the way back.
//
// It acts on every declared profile, not the active one: a variable has one
// kind, so leaving any profile's keyring item behind would strand a value
// nothing references. The manifest is saved before any keyring item is
// deleted, so a failure partway leaves every value safe in the manifest; the
// worst case is unreferenced keyring items, which the error counts.
func newSecretDemoteCmd(opts *Options) *cobra.Command {
	var yes bool

	c := &cobra.Command{
		Use:               "demote <NAME>",
		Short:             "Turn a secret back into a plain variable, moving its values into the manifest",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: opts.completeVarNames(isSecret),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name := args[0]
			v := app.Manifest.Vars[name]
			if !v.Secret {
				return fmt.Errorf("%s is not a secret", name)
			}

			s, err := app.store()
			if err != nil {
				return err
			}
			// Get, not Has: the values are the point. This is one of the few
			// places, beside an explicit reveal and Check now, that reads secrets,
			// and it may raise one keyring prompt per profile. Values are never printed.
			profiles := app.Manifest.Project.Profiles
			values := map[string]string{}
			var found []string
			for _, p := range profiles {
				val, err := s.Get(app.Cfg.Keyring.ServiceName(app.Manifest.Project.ID, p, name))
				if err != nil {
					if errors.Is(err, store.ErrNotFound) {
						continue
					}
					return err
				}
				values[p] = val
				found = append(found, p)
			}
			if len(found) == 0 && !v.Optional {
				return fmt.Errorf(
					"nothing stored for %s under any profile -- store one with: trenvy -p %s secret set %s, or remove it with: trenvy unset %s",
					name, app.Profile.Name, name, name)
			}

			if !yes {
				question := fmt.Sprintf(
					"Demote %s? Its values move out of the keyring into the manifest in plain text (%s), and those keyring items are deleted.",
					name, strings.Join(found, ", "))
				if len(found) == 0 {
					question = fmt.Sprintf(
						"Demote %s? Nothing is stored (no values were stored under any profile), so it becomes an optional plain variable.", name)
				}
				ok, err := confirm(opts, question)
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("aborted")
				}
			}

			v.Secret = false
			v.Value, v.Profiles = nil, nil
			if len(found) > 0 && len(found) == len(profiles) && allEqual(values) {
				shared := values[found[0]]
				v.Value = &shared
			} else if len(found) > 0 {
				v.Profiles = values
			}
			app.Manifest.Vars[name] = v
			if err := app.Manifest.Validate(); err != nil {
				return err
			}
			if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
				return err
			}

			done := 0
			for _, p := range found {
				svc := app.Cfg.Keyring.ServiceName(app.Manifest.Project.ID, p, name)
				if err := s.Delete(svc); err != nil {
					if errors.Is(err, store.ErrNotFound) {
						// Already gone: not a failure, but not a deletion
						// this command made, so it is not counted.
						continue
					}
					return fmt.Errorf("delete %s: %w\n  deleted %d of %d keyring items; the manifest already holds the values in plain text -- the rest are unreferenced and can be removed in your keyring's own tool",
						svc, err, done, len(found))
				}
				done++
			}
			if len(found) == 0 {
				fmt.Fprintf(opts.Stdout, "demoted %s: no values were stored; it is now an optional plain variable\n", name)
				return nil
			}
			fmt.Fprintf(opts.Stdout, "demoted %s: %d plain values (%s), deleted %d keyring items\n",
				name, len(found), strings.Join(found, ", "), done)
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return c
}

func allEqual(values map[string]string) bool {
	first, set := "", false
	for _, val := range values {
		if !set {
			first, set = val, true
		} else if val != first {
			return false
		}
	}
	return true
}

func newUnsetCmd(opts *Options) *cobra.Command {
	var yes bool

	c := &cobra.Command{
		Use:               "unset <NAME>",
		Short:             "Remove a variable from the manifest and its secret from every profile's keyring",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: opts.completeVarNames(anyVar),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name := args[0]
			v, ok := app.Manifest.Vars[name]
			if !ok {
				return fmt.Errorf("%s is not in the manifest", name)
			}
			profiles := app.Manifest.Project.Profiles
			if !yes {
				// unset drops the variable and every profile's keyring item,
				// not just the active one -- the prompt says so, because
				// nothing in the name "unset" tells you that.
				question := fmt.Sprintf("Remove %s from %s?", name, app.Entry.ID)
				if v.Secret {
					question = fmt.Sprintf(
						"Remove %s from %s and delete its secret in %d profiles?",
						name, app.Entry.ID, len(profiles))
				}
				ok, err := confirm(opts, question)
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("aborted")
				}
			}

			// cleared counts only profiles actually cleared, and is
			// reported in the success message below -- unset's own
			// prompt promises "delete its secret in N profiles", so the
			// success line says how many it actually cleared rather
			// than leaving that unstated.
			cleared := 0
			if v.Secret {
				s, err := app.store()
				if err != nil {
					return err
				}
				// Every profile, not just the active one: the variable is
				// leaving the manifest, so no profile's item may stay
				// reachable behind it. app.service is deliberately not used
				// here -- it binds the ACTIVE profile, so calling it inside
				// this loop would delete that one profile's item len(profiles)
				// times while leaving the rest untouched (the same hazard
				// caught in profile rm and project rm).
				//
				// A declared secret that was never stored under a given
				// profile is the ordinary case, not a failure -- most
				// projects only fill a secret in under one profile at a
				// time -- so ErrNotFound is skipped, and it must not
				// inflate the count reported below.
				for _, p := range profiles {
					svc := app.Cfg.Keyring.ServiceName(app.Manifest.Project.ID, p, name)
					if err := s.Delete(svc); err != nil {
						if errors.Is(err, store.ErrNotFound) {
							continue
						}
						// Nothing below this has run: the manifest still
						// declares the variable, so this is safe to re-run
						// once the underlying store problem is fixed. Say how
						// many profiles are already cleared rather than
						// implying nothing happened -- by the time a delete
						// fails, every profile before it has been cleared
						// permanently, and false reassurance is how the rest
						// never gets cleaned up.
						return fmt.Errorf(
							"delete %s: %w\n  %d profiles' items were already deleted; %s is"+
								" still in the manifest -- fix this and re-run",
							svc, err, cleared, name)
					}
					cleared++
				}
			}
			delete(app.Manifest.Vars, name)
			if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
				return err
			}
			if v.Secret {
				fmt.Fprintf(opts.Stdout, "removed %s (%d secrets deleted)\n", name, cleared)
			} else {
				fmt.Fprintf(opts.Stdout, "removed %s\n", name)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return c
}

func confirm(opts *Options, question string) (bool, error) {
	fmt.Fprintf(opts.Stderr, "%s [y/N] ", question)
	line, err := bufio.NewReader(opts.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

// warnUnreachableInDotenv reports, without refusing, that a name just written
// cannot appear in a dotenv output that selects it.
//
// The rule belongs to the format rather than to trenvy. A json output takes any
// key, and a process environment carries the variable to the child perfectly
// well -- Python reads "feature.flag" from os.environ and Node from
// process.env; only shell syntax cannot address it, because $feature.flag
// parses as $feature followed by .flag. Dotenv is the one destination whose
// file is sourced by a shell, so it is the one that needs an identifier.
//
// Hence a warning rather than a rule: refusing the name would stop a json-only
// project from using a key its own config format allows. It fires only when a
// dotenv output would actually have to write the name, so a project with none
// is never nagged, and render still errors if the situation is reached anyway.
func warnUnreachableInDotenv(opts *Options, m *manifest.Manifest, name string) {
	if render.ValidateDotenvName(name) == nil {
		return
	}
	var blocked []string
	for out, o := range m.Outputs {
		if o.Format != manifest.FormatDotenv {
			continue
		}
		// An output that excludes the variable cannot be broken by it.
		if sel, err := resolve.Selects(m, out, name); err != nil || !sel {
			continue
		}
		blocked = append(blocked, out)
	}
	if len(blocked) == 0 {
		return
	}
	sort.Strings(blocked)

	noun := "output"
	if len(blocked) > 1 {
		noun = "outputs"
	}
	fmt.Fprintf(opts.Stderr,
		"trenvy: warning: %q is not a valid shell identifier, so `render` will refuse "+
			"the dotenv %s it belongs to: %s\n", name, noun, strings.Join(blocked, ", "))
	fmt.Fprintf(opts.Stderr,
		"  the value is stored, and works in json outputs and under exec; "+
			"rename the variable, or add it to that output's exclude list\n")
}
