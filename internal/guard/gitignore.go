// Package guard holds the refusals: the checks that stop envctl writing a
// secret somewhere it should not go, or over something it did not write.
package guard

import (
	"errors"
	"os/exec"
)

// GitIgnored reports whether relPath is ignored inside the worktree at root.
//
// Outside a git repository it returns true: nothing can be committed from
// there, so the guard has nothing to protect against and must not block the
// write.
func GitIgnored(worktreeRoot, relPath string) (bool, error) {
	cmd := exec.Command("git", "check-ignore", "-q", "--", relPath)
	cmd.Dir = worktreeRoot
	err := cmd.Run()
	if err == nil {
		return true, nil // exit 0: ignored
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case 1:
			return false, nil // not ignored
		case 128:
			return true, nil // not a git repository
		}
	}
	// git missing entirely: treat as not-a-repository rather than failing a
	// render the user has every right to perform.
	return true, nil
}
