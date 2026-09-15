package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/trenvy/internal/guard"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/render"
	"github.com/cdescobedo/trenvy/internal/resolve"
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
			// TRENVY_PROFILE is already in env: prepareEnv exports it for
			// every child, because it is functional -- resolve.Profile reads
			// it, so a trenvy run inside the subshell stays on the profile
			// the subshell was loaded with. TRENVY_PROJECT is added only here
			// because it is purely cosmetic: a marker the user's prompt can
			// pick up, so a shell carrying secrets looks different from a
			// clean one. A one-shot exec has no prompt to mark.
			env = append(env, "TRENVY_PROJECT="+app.Entry.ID)

			child := exec.Command(sh)
			child.Env = env
			child.Dir = opts.Dir
			fmt.Fprintf(opts.Stderr, "trenvy: %s/%s loaded; exit to leave\n",
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
	// nothing the user asked for can be found. TRENVY_PASSPHRASE is stripped:
	// it unlocks the file secret backend, and a child process has no
	// business inheriting it just because it inherits everything else.
	env := make([]string, 0, len(os.Environ())+len(values.Names))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TRENVY_PASSPHRASE=") {
			continue
		}
		env = append(env, kv)
	}
	for _, n := range values.Names {
		env = append(env, n+"="+values.Values[n])
	}
	// The profile the child inherits must be the one these values came from.
	// A script run under exec may itself call trenvy -- deploy.sh running
	// `trenvy get` -- and without this that nested call falls back to
	// default_profile and reads a different keyring namespace than its parent
	// was told to use. It does not fail; it quietly reads the wrong secret.
	//
	// Appended last on purpose: an inherited TRENVY_PROFILE is already in env
	// above, and os/exec deduplicates on Start keeping the last occurrence, so
	// a --profile flag beats a stale export rather than losing to it.
	env = append(env, resolve.ProfileEnvVar+"="+app.Profile.Name)
	return app, env, nil
}

// warnStaleRenders reports every rendered file in this worktree that no
// longer matches the manifest.
//
// A stale .env silently beats an injected environment in frameworks that load
// dotenv files over the process environment, which makes this the single most
// confusing failure this tool can have. Every output is considered, not just
// one: exec names no output, so there is no "the output being run" to scope
// this to, and any rendered file in the worktree can do the shadowing.
//
// The State.Find gate is a cost guard rather than a correctness one -- with no
// entry, Inspect answers Absent or Foreign, neither of which warns -- but it
// skips the stat and the digest for outputs this worktree never rendered.
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
		subset, err := narrow(app.Manifest, name, all)
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
				"trenvy: %s is %s and may override what is injected; run: trenvy render %s\n",
				o.Path, st, name)
		}
	}
}

// narrow restricts an already-resolved set to what one output selects. It
// takes the manifest rather than the appContext it is always called with, so
// that it is a pure function of its arguments and testable without a project.
func narrow(m *manifest.Manifest, output string, all resolve.Resolved) (resolve.Resolved, error) {
	out := resolve.Resolved{
		Profile: all.Profile,
		Output:  output,
		Values:  map[string]string{},
	}
	for _, name := range all.Names {
		sel, err := resolve.Selects(m, output, name)
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
// trenvy from one meant for the child.
//
// An argument before the dash is refused by name rather than treated as the
// command. `trenvy exec local -- cmd` was the old spelling, and running
// "local" as a program would be the worst possible answer to stale muscle
// memory.
func commandAfterDash(cmd *cobra.Command, args []string) ([]string, error) {
	at := cmd.ArgsLenAtDash()
	if at < 0 {
		return nil, fmt.Errorf("a command must follow --; usage: trenvy exec -- <command>")
	}
	if at > 0 {
		return nil, fmt.Errorf(
			"exec takes no argument before --: the target axis is gone, and values now\n"+
				"  come from the profile. Drop %q, or choose values with --profile;\n"+
				"  usage: trenvy exec -- <command>", args[0])
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("a command must follow --; usage: trenvy exec -- <command>")
	}
	return args, nil
}
