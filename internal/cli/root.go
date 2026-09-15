// Package cli wires the commands. Every command is driven through Execute so
// that tests can run the whole CLI in-process against a temp config directory
// and an in-memory secret store.
package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"sync"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/resolve"
	"github.com/cdescobedo/trenvy/internal/store"
	"github.com/cdescobedo/trenvy/internal/tui"
)

// Options are the seams that make the CLI testable: streams, the working
// directory, and how a secret store is obtained.
type Options struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Dir     string
	Backend string
	// Profile is the --profile flag. Empty means fall back to TRENVY_PROFILE
	// and then the manifest's default_profile.
	Profile  string
	NewStore func(cfg *config.Config, backend string) (store.Store, error)

	// Run executes a prepared child process. Swappable so tests can assert on
	// the environment and argv without spawning anything.
	Run func(*exec.Cmd) error

	// RunTUI drives the interactive table. Swappable so tests can supply a
	// finished model instead of a terminal session.
	RunTUI func(tui.Model, func(string) (string, error)) (tui.Model, error)
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
			// store.Open falls back to TRENVY_BACKEND and then the platform
			// default when backend is empty, so the precedence here is
			// simply flag > env var > platform default. Nothing else in the
			// CLI reimplements that precedence.
			return store.Open(backend, currentAccount())
		},
		Run: func(c *exec.Cmd) error {
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			return c.Run()
		},
		RunTUI: tui.Run,
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
//
// The secret store is opened lazily (see store()), not here: o.NewStore can
// fail outright on a project with zero secrets (the file backend errors with
// no passphrase configured, for instance), and a project that never touches a
// secret must not pay for a store it never uses.
type appContext struct {
	Cfg      *config.Config
	Entry    config.Entry
	Manifest *manifest.Manifest
	State    *config.State
	Worktree string
	Profile  resolve.ActiveProfile

	opts      Options
	storeOnce sync.Once
	storeVal  store.Store
	storeErr  error
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
			"%s is not inside a registered trenvy project\n  register one with: trenvy project init", o.Dir)
	}

	m, err := manifest.Load(config.ManifestPath(entry.ID))
	if err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}

	prof, err := resolve.Profile(m, o.Profile, os.Getenv(resolve.ProfileEnvVar))
	if err != nil {
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

	return &appContext{
		Cfg: cfg, Entry: entry, Manifest: m,
		State: st, Worktree: wt, Profile: prof, opts: o,
	}, nil
}

// store opens the secret store on first use and memoizes the result (or the
// error) for the rest of this command's run.
func (a *appContext) store() (store.Store, error) {
	a.storeOnce.Do(func() {
		a.storeVal, a.storeErr = a.opts.NewStore(a.Cfg, a.opts.Backend)
	})
	return a.storeVal, a.storeErr
}

// secretLookup adapts the store to resolve.SecretLookup, applying the
// configured service-name pattern.
func (a *appContext) secretLookup() resolve.SecretLookup {
	return func(name string) (string, error) {
		s, err := a.store()
		if err != nil {
			return "", err
		}
		return s.Get(a.service(name))
	}
}

func (a *appContext) service(name string) string {
	return a.Cfg.Keyring.ServiceName(a.Manifest.Project.ID, a.Profile.Name, name)
}

// outputOrDefault returns name, or the manifest's default output when empty.
//
// An empty result is its own error rather than a lookup miss: a project that
// only ever feeds exec and shell legitimately declares no outputs at all, so
// "" here means nothing was configured, not that something was misspelled.
//
// cmd supplies its own path for the hint. Several commands share this, so a
// hard-coded example is wrong for all but one of them -- and telling someone
// who ran `trenvy clean` to run `trenvy render` hands them the opposite of
// what they asked for.
func (a *appContext) outputOrDefault(cmd *cobra.Command, name string) (string, error) {
	if name == "" {
		name = a.Manifest.Project.Output
	}
	if name == "" {
		return "", fmt.Errorf("no output given and project.output is not set;\n"+
			"  define one in the manifest, or name it: %s <output>", cmd.CommandPath())
	}
	if _, ok := a.Manifest.Outputs[name]; !ok {
		return "", fmt.Errorf("%w: %q", resolve.ErrUnknownOutput, name)
	}
	return name, nil
}

// Execute runs the CLI with args.
func Execute(opts Options, args []string) error {
	root := &cobra.Command{
		Use:           "trenvy",
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
	root.PersistentFlags().StringVarP(&opts.Profile, "profile", "p", "",
		"value set to use: overrides TRENVY_PROFILE and the manifest default")

	root.AddCommand(
		newProjectCmd(&opts),
		newStatusCmd(&opts),
		newImportCmd(&opts),
		newEditCmd(&opts),
		newSetCmd(&opts),
		newGetCmd(&opts),
		newShowCmd(&opts),
		newSecretCmd(&opts),
		newUnsetCmd(&opts),
		newRenderCmd(&opts),
		newCleanCmd(&opts),
		newExecCmd(&opts),
		newShellCmd(&opts),
		newProfileCmd(&opts),
	)

	if err := root.RegisterFlagCompletionFunc("profile", opts.completeProfileNames()); err != nil {
		return err
	}

	return root.Execute()
}
