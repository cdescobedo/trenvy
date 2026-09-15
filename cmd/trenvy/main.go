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
// A child's own *exec.ExitError -- from `trenvy exec` or `trenvy shell` -- is
// surfaced verbatim: its exit code becomes trenvy's, with no "trenvy:"
// prefix. Without this, a child exiting 7 makes trenvy exit 1 and print
// "trenvy: exit status 7", which reads as a trenvy failure and is unusable
// in a Makefile or CI script that branches on the exit code. Every other
// error keeps the existing "trenvy: <message>" behaviour.
func report(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if code := ee.ExitCode(); code >= 0 {
			return code
		}
		// ExitCode is -1 when the child was killed by a signal rather than
		// exiting. os.Exit(-1) surfaces as 255, which is indistinguishable
		// from a real exit status of 255; shells report a signalled child as
		// 128+signal, so match that.
		if ws, ok := ee.Sys().(interface{ Signal() syscall.Signal }); ok {
			if sig := ws.Signal(); sig > 0 {
				return 128 + int(sig)
			}
		}
		return 128
	}
	fmt.Fprintf(stderr, "trenvy: %v\n", err)
	return 1
}
