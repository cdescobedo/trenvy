// Command trenvy manages a project's environment variables: plain values in a
// central manifest, secrets in the OS keyring, rendered into any worktree.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/cdescobedo/trenvy/internal/cli"
)

func main() {
	err := cli.Execute(cli.Default(), os.Args[1:])
	os.Exit(report(err, os.Stderr))
}

// report writes a diagnostic for err (if any) to stderr and returns the
// process exit code.
//
// A *cli.ChildExitError -- which exec and shell wrap around the error from
// running the command the user actually asked for -- is surfaced verbatim:
// its exit code becomes trenvy's, with no "trenvy:" prefix. Without this, a
// child exiting 7 makes trenvy exit 1 and print "trenvy: exit status 7",
// which reads as a trenvy failure and is unusable in a Makefile or CI script
// that branches on the exit code.
//
// The match is on *cli.ChildExitError specifically, not on a bare
// *exec.ExitError: errors.As unwraps the whole chain, and the macOS Keychain
// backend also shells out to security(1), so a bare-ExitError match would
// treat ANY internal subprocess failure -- a Keychain write gone wrong, for
// instance -- as if it were the exit status of the user's own command, and
// print nothing at all. Every error that is not a *cli.ChildExitError keeps
// the "trenvy: <message>" behaviour, including one that merely contains an
// *exec.ExitError somewhere in its chain.
func report(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var cee *cli.ChildExitError
	if errors.As(err, &cee) {
		var ee *exec.ExitError
		if errors.As(cee, &ee) {
			if code := ee.ExitCode(); code >= 0 {
				return code
			}
			// ExitCode is -1 when the child was killed by a signal rather
			// than exiting. os.Exit(-1) surfaces as 255, which is
			// indistinguishable from a real exit status of 255; shells
			// report a signalled child as 128+signal, so match that.
			if ws, ok := ee.Sys().(interface{ Signal() syscall.Signal }); ok {
				if sig := ws.Signal(); sig > 0 {
					return 128 + int(sig)
				}
			}
			return 128
		}
	}
	fmt.Fprintf(stderr, "trenvy: %v\n", err)
	return 1
}
