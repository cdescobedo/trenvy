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
	"github.com/cdescobedo/envctl/internal/render"
	"github.com/cdescobedo/envctl/internal/resolve"
)

func newExecCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "exec -- <command> [args...]",
		Short: "Run a command with the profile's variables in its environment",
		Long: "Run a command with every variable the active profile resolves in its\n" +
			"environment.\n\n" +
			"There is no output argument: an output's include list describes a file's\n" +
			"audience, and a process wants its whole environment. Choose the values\n" +
			"with --profile.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rest, err := commandAfterDash(cmd, args)
			if err != nil {
				return err
			}
			_, env, err := prepareEnv(opts)
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
		Use:   "shell",
		Short: "Start a subshell with the profile's variables loaded",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, env, err := prepareEnv(opts)
			if err != nil {
				return err
			}

			sh := os.Getenv("SHELL")
			if sh == "" {
				sh = "/bin/sh"
				if runtime.GOOS == "windows" {
					sh = "cmd.exe"
				}
			}
			// A marker the user's prompt can pick up, so a shell carrying
			// secrets is visibly different from a clean one. ENVCTL_PROFILE
			// is the same variable resolve.Profile reads, so an envctl run
			// from inside the subshell stays on the profile the subshell was
			// loaded with rather than silently falling back to the default.
			env = append(env,
				resolve.ProfileEnvVar+"="+app.Profile.Name,
				"ENVCTL_PROJECT="+app.Entry.ID)

			child := exec.Command(sh)
			child.Env = env
			child.Dir = opts.Dir
			fmt.Fprintf(opts.Stderr, "envctl: %s/%s loaded; exit to leave\n",
				app.Entry.ID, app.Profile.Name)
			return opts.Run(child)
		},
	}
}

// prepareEnv resolves the active profile's values onto a copy of the parent
// environment and warns about any rendered file that no longer matches.
func prepareEnv(opts *Options) (*appContext, []string, error) {
	app, err := opts.resolveApp()
	if err != nil {
		return nil, nil, err
	}
	// All, not Values: no output subset applies to a process. An output's
	// include list is about what belongs in one file, and narrowing a child's
	// environment by it would mean `exec` handing over less than the project
	// actually has.
	values, err := resolve.All(app.Manifest, app.Profile.Name, app.secretLookup())
	if err != nil {
		return nil, nil, err
	}

	warnStaleRenders(opts, app, values)

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

// warnStaleRenders reports every rendered file in this worktree that no
// longer matches the manifest.
//
// A stale .env silently beats an injected environment in frameworks that load
// dotenv files over the process environment, which makes this the single most
// confusing failure this tool can have. Only outputs envctl has actually
// rendered into this worktree are checked -- one that was never rendered here
// cannot shadow anything.
//
// The comparison costs no extra secret read: each output's set is the already
// resolved full set narrowed by that output's own globs, which is exactly
// what resolve.Values would have produced for it.
func warnStaleRenders(opts *Options, app *appContext, all resolve.Resolved) {
	for _, name := range sortedOutputNames(app.Manifest) {
		entry, have := app.State.Find(app.Worktree, name)
		if !have {
			continue
		}
		o := app.Manifest.Outputs[name]
		subset, err := narrow(app, name, all)
		if err != nil {
			continue
		}
		st, err := guard.Inspect(
			filepath.Join(app.Worktree, o.Path),
			render.Sum(app.Manifest.Project.Salt, subset), entry, have)
		if err != nil {
			continue
		}
		if st == guard.StatusStale || st == guard.StatusModified {
			fmt.Fprintf(opts.Stderr,
				"envctl: %s is %s and may override what is injected; run: envctl render %s\n",
				o.Path, st, name)
		}
	}
}

// narrow restricts an already-resolved set to what one output selects.
func narrow(app *appContext, output string, all resolve.Resolved) (resolve.Resolved, error) {
	out := resolve.Resolved{
		Profile: all.Profile,
		Output:  output,
		Values:  map[string]string{},
	}
	for _, name := range all.Names {
		sel, err := resolve.Selects(app.Manifest, output, name)
		if err != nil {
			return resolve.Resolved{}, err
		}
		if !sel {
			continue
		}
		out.Names = append(out.Names, name)
		out.Values[name] = all.Values[name]
	}
	return out, nil
}

// commandAfterDash returns the command following --. Cobra requires the
// separator: without it there is no reliable way to tell a flag meant for
// envctl from one meant for the child.
//
// An argument before the dash is refused by name rather than treated as the
// command. `envctl exec local -- cmd` was the old spelling, and running
// "local" as a program would be the worst possible answer to stale muscle
// memory.
func commandAfterDash(cmd *cobra.Command, args []string) ([]string, error) {
	at := cmd.ArgsLenAtDash()
	if at < 0 {
		return nil, fmt.Errorf("no command given; usage: envctl exec -- <command>")
	}
	if at > 0 {
		return nil, fmt.Errorf(
			"exec takes no argument before --: the target axis is gone, and values now\n"+
				"  come from the profile. Drop %q, or choose values with --profile;\n"+
				"  usage: envctl exec -- <command>", args[0])
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("no command given; usage: envctl exec -- <command>")
	}
	return args, nil
}
