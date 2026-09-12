// Command envctl manages a project's environment variables: plain values in a
// central manifest, secrets in the OS keyring, rendered into any worktree.
package main

import (
	"fmt"
	"os"

	"github.com/cdescobedo/envctl/internal/cli"
)

func main() {
	if err := cli.Execute(cli.Default(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "envctl: %v\n", err)
		os.Exit(1)
	}
}
