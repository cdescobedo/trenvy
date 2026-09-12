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
	return &cobra.Command{
		Use:   "set <NAME> <VALUE>",
		Short: "Set a plain (non-secret) variable",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name, value := args[0], args[1]

			if existing, ok := app.Manifest.Vars[name]; ok && existing.Secret {
				// Naming the value here would print a secret-adjacent string to
				// the terminal and the shell history; the name is enough.
				return fmt.Errorf(
					"%s is a secret; set its value with: envctl secret set %s", name, name)
			}

			v := app.Manifest.Vars[name]
			v.Value = &value
			app.Manifest.Vars[name] = v

			if err := app.Manifest.Validate(); err != nil {
				return err
			}
			if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
				return err
			}
			fmt.Fprintf(opts.Stdout, "set %s\n", name)
			return nil
		},
	}
}

func newSecretCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{Use: "secret", Short: "Manage secret values in the keyring"}
	cmd.AddCommand(newSecretSetCmd(opts), newSecretRmCmd(opts))
	return cmd
}

func newSecretSetCmd(opts *Options) *cobra.Command {
	var fromStdin bool

	c := &cobra.Command{
		Use:   "set <NAME>",
		Short: "Store or rotate a secret in the keyring",
		Args:  cobra.ExactArgs(1),
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

			if err := app.Store.Set(app.service(name), value); err != nil {
				return err
			}

			v := app.Manifest.Vars[name]
			v.Secret = true
			v.Value = nil // a secret never keeps a plain value
			app.Manifest.Vars[name] = v
			if err := app.Manifest.Validate(); err != nil {
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
		Use:   "rm <NAME>",
		Short: "Delete a secret's value from the keyring, keeping the variable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name := args[0]
			if !yes {
				ok, err := confirm(opts, fmt.Sprintf("Delete %s from the keyring?", name))
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("aborted")
				}
			}
			if err := app.Store.Delete(app.service(name)); err != nil && !errors.Is(err, store.ErrNotFound) {
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
		Use:   "unset <NAME>",
		Short: "Remove a variable from the manifest and its secret from the keyring",
		Args:  cobra.ExactArgs(1),
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
			if !yes {
				ok, err := confirm(opts, fmt.Sprintf("Remove %s from %s?", name, app.Entry.ID))
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("aborted")
				}
			}

			if v.Secret {
				if err := app.Store.Delete(app.service(name)); err != nil && !errors.Is(err, store.ErrNotFound) {
					return err
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
