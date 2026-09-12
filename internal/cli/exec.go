package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/envctl/internal/guard"
	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/render"
	"github.com/cdescobedo/envctl/internal/resolve"
)

func newExecCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "exec [target] -- <command> [args...]",
		Short: "Run a command with the target's variables in its environment",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, rest, err := splitTargetAndCommand(cmd, args)
			if err != nil {
				return err
			}
			if len(rest) == 0 {
				return fmt.Errorf("no command given; usage: envctl exec [target] -- <command>")
			}
			_, env, err := prepareEnv(opts, target)
			if err != nil {
				return err
			}

			// The command and its arguments come from argv; the values go in
			// the environment. A secret never appears on a command line.
			child := exec.Command(rest[0], rest[1:]...)
			child.Env = env
			child.Dir = opts.Dir
			return opts.Run(child)
		},
	}
}

func newShellCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "shell [target]",
		Short: "Start a subshell with the target's variables loaded",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, env, err := prepareEnv(opts, firstOrEmpty(args))
			if err != nil {
				return err
			}
			name, _ := app.targetOrDefault(firstOrEmpty(args))

			sh := os.Getenv("SHELL")
			if sh == "" {
				sh = "/bin/sh"
				if runtime.GOOS == "windows" {
					sh = "cmd.exe"
				}
			}
			// A marker the user's prompt can pick up, so a shell carrying
			// secrets is visibly different from a clean one.
			env = append(env, "ENVCTL_TARGET="+name, "ENVCTL_PROJECT="+app.Entry.ID)

			child := exec.Command(sh)
			child.Env = env
			child.Dir = opts.Dir
			fmt.Fprintf(opts.Stderr, "envctl: %s/%s loaded; exit to leave\n", app.Entry.ID, name)
			return opts.Run(child)
		},
	}
}

// prepareEnv resolves a target's values onto a copy of the parent environment
// and warns about any rendered file that no longer matches the manifest.
func prepareEnv(opts *Options, target string) (*appContext, []string, error) {
	app, err := opts.resolveApp()
	if err != nil {
		return nil, nil, err
	}
	name, err := app.targetOrDefault(target)
	if err != nil {
		return nil, nil, err
	}
	values, err := resolve.Values(app.Manifest, name, app.secretLookup())
	if err != nil {
		return nil, nil, err
	}

	warnStaleRender(opts, app, name)

	// Inheriting the parent environment matters: without it PATH is empty and
	// nothing the user asked for can be found. ENVCTL_PASSPHRASE is stripped:
	// it unlocks the file secret backend, and a child process has no
	// business inheriting it just because it inherits everything else.
	env := make([]string, 0, len(os.Environ())+len(values.Names))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ENVCTL_PASSPHRASE=") {
			continue
		}
		env = append(env, kv)
	}
	for _, n := range values.Names {
		env = append(env, n+"="+values.Values[n])
	}
	return app, env, nil
}

// warnStaleRender reports when the target actually being run's rendered file
// no longer matches the manifest.
//
// A stale .env silently beats an injected environment in frameworks that load
// dotenv files over the process environment, which makes this the single most
// confusing failure this tool can have. Only the target this invocation
// selected is checked: another target's staleness is not relevant here, and
// checking every file target in the manifest would re-resolve (and so
// re-read) every secret once per target on every exec.
func warnStaleRender(opts *Options, app *appContext, name string) {
	t, ok := app.Manifest.Targets[name]
	if !ok || t.Dest != manifest.DestFile {
		return
	}
	values, err := resolve.Values(app.Manifest, name, app.secretLookup())
	if err != nil {
		return
	}
	entry, have := app.State.Find(app.Worktree, name)
	st, err := guard.Inspect(
		filepath.Join(app.Worktree, t.Path),
		render.Sum(app.Manifest.Project.Salt, values), entry, have)
	if err != nil {
		return
	}
	if st == guard.StatusStale || st == guard.StatusModified {
		fmt.Fprintf(opts.Stderr,
			"envctl: %s is %s and may override what is injected; run: envctl render %s\n",
			t.Path, st, name)
	}
}

// splitTargetAndCommand separates an optional target from the command after
// --. Cobra requires the -- separator here: without it there is no reliable
// way to tell a bare target ("envctl exec local") from a command with the
// same name, so a missing dash is an error rather than an empty target.
func splitTargetAndCommand(cmd *cobra.Command, args []string) (string, []string, error) {
	at := cmd.ArgsLenAtDash()
	if at < 0 {
		return "", nil, fmt.Errorf("no command given; usage: envctl exec [target] -- <command>")
	}
	if at == 0 {
		return "", args, nil
	}
	return args[0], args[at:], nil
}
