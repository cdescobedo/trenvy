// Package cli wires the commands. Every command is driven through Execute so
// that tests can run the whole CLI in-process against a temp config directory
// and an in-memory secret store.
package cli

import (
	"fmt"
	"io"
	"os"
	"os/user"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/resolve"
	"github.com/cdescobedo/envctl/internal/store"
)

// Options are the seams that make the CLI testable: streams, the working
// directory, and how a secret store is obtained.
type Options struct {
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	Dir      string
	Backend  string
	NewStore func(cfg *config.Config, backend string) (store.Store, error)
}

// Default returns Options wired to the real process.
func Default() Options {
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	return Options{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Dir:    dir,
		NewStore: func(cfg *config.Config, backend string) (store.Store, error) {
			// store.Open falls back to ENVCTL_BACKEND and then the platform
			// default when backend is empty, so the precedence here is
			// simply flag > env var > platform default. Nothing else in the
			// CLI reimplements that precedence.
			return store.Open(backend, currentAccount())
		},
	}
}

func currentAccount() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// appContext is everything a command needs about the project it is operating
// on. Commands that do not need a project never build one.
type appContext struct {
	Cfg      *config.Config
	Entry    config.Entry
	Manifest *manifest.Manifest
	State    *config.State
	Worktree string
	Store    store.Store
}

// resolveApp locates the project owning Opts.Dir and loads everything about it.
func (o Options) resolveApp() (*appContext, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	reg, err := config.LoadRegistry()
	if err != nil {
		return nil, err
	}
	entry, ok := reg.Lookup(o.Dir)
	if !ok {
		return nil, fmt.Errorf(
			"%s is not inside a registered envctl project\n  register one with: envctl init", o.Dir)
	}

	m, err := manifest.Load(config.ManifestPath(entry.ID))
	if err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}

	st, err := config.LoadState(entry.ID)
	if err != nil {
		return nil, err
	}
	wt, err := resolve.WorktreeRoot(o.Dir)
	if err != nil {
		return nil, err
	}
	sec, err := o.NewStore(cfg, o.Backend)
	if err != nil {
		return nil, err
	}

	return &appContext{
		Cfg: cfg, Entry: entry, Manifest: m,
		State: st, Worktree: wt, Store: sec,
	}, nil
}

// secretLookup adapts the store to resolve.SecretLookup, applying the
// configured service-name pattern.
func (a *appContext) secretLookup() resolve.SecretLookup {
	return func(name string) (string, error) {
		return a.Store.Get(a.service(name))
	}
}

func (a *appContext) service(name string) string {
	return a.Cfg.Keyring.ServiceName(a.Manifest.Project.ID, name)
}

// targetOrDefault returns name, or the manifest's default target when empty.
func (a *appContext) targetOrDefault(name string) (string, error) {
	if name == "" {
		name = a.Manifest.Project.DefaultTarget
	}
	if _, ok := a.Manifest.Targets[name]; !ok {
		return "", fmt.Errorf("%w: %q", resolve.ErrUnknownTarget, name)
	}
	return name, nil
}

// Execute runs the CLI with args.
func Execute(opts Options, args []string) error {
	root := &cobra.Command{
		Use:           "envctl",
		Short:         "Project environment variables, with secrets in the OS keyring",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetArgs(args)
	root.SetIn(opts.Stdin)
	root.SetOut(opts.Stdout)
	root.SetErr(opts.Stderr)
	root.PersistentFlags().StringVar(&opts.Backend, "backend", "",
		"secret backend: keychain, system, file, mem")

	root.AddCommand(
		newInitCmd(&opts),
		newProjectsCmd(&opts),
		newStatusCmd(&opts),
		newImportCmd(&opts),
		newSetCmd(&opts),
		newSecretCmd(&opts),
		newUnsetCmd(&opts),
		newRenderCmd(&opts),
		newCleanCmd(&opts),
	)

	return root.Execute()
}
