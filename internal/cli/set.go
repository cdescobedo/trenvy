package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/store"
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
					"%s is a secret; set its value with: envctl secret set %s", name, name)
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
			if shared {
				fmt.Fprintf(opts.Stdout, "set %s for every profile\n", name)
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
	cmd := &cobra.Command{Use: "secret", Short: "Manage secret values in the keyring"}
	cmd.AddCommand(newSecretSetCmd(opts), newSecretRmCmd(opts))
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

	if isTerminal(opts.Stdin) {
		f := opts.Stdin.(interface{ Fd() uintptr })
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
			if err := s.Delete(app.service(name)); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			fmt.Fprintf(opts.Stdout, "deleted %s from the keyring\n", name)
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return c
}

func newUnsetCmd(opts *Options) *cobra.Command {
	var yes bool

	c := &cobra.Command{
		Use:               "unset <NAME>",
		Short:             "Remove a variable from the manifest and its secret from the keyring",
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
				for _, p := range profiles {
					svc := app.Cfg.Keyring.ServiceName(app.Manifest.Project.ID, p, name)
					if err := s.Delete(svc); err != nil && !errors.Is(err, store.ErrNotFound) {
						return err
					}
				}
			}
			delete(app.Manifest.Vars, name)
			if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
				return err
			}
			fmt.Fprintf(opts.Stdout, "removed %s\n", name)
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
