// Command envctl manages a project's environment variables: plain values in a
// central manifest, secrets in the OS keyring, rendered into any worktree.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/cdescobedo/envctl/internal/cli"
)

func main() {
	err := cli.Execute(cli.Default(), os.Args[1:])
	os.Exit(report(err, os.Stderr))
}

// report writes a diagnostic for err (if any) to stderr and returns the
// process exit code.
//
// A child's own *exec.ExitError -- from `envctl exec` or `envctl shell` -- is
// surfaced verbatim: its exit code becomes envctl's, with no "envctl:"
// prefix. Without this, a child exiting 7 makes envctl exit 1 and print
// "envctl: exit status 7", which reads as an envctl failure and is unusable
// in a Makefile or CI script that branches on the exit code. Every other
// error keeps the existing "envctl: <message>" behaviour.
func report(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	fmt.Fprintf(stderr, "envctl: %v\n", err)
	return 1
}
