// Package resolve turns a working directory and a target name into concrete
// values and paths.
package resolve

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreeRoot returns the root of the git worktree containing dir, or dir
// itself when it is not inside a repository.
//
// This is deliberately distinct from the project root in the registry. The
// project root is the directory holding every worktree; the worktree root is
// the specific checkout a target's relative path resolves against. Keeping
// them separate is what lets one manifest serve every worktree, including ones
// created long after the project was registered.
func WorktreeRoot(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		// Not a repository, or git is unavailable. Neither is an error: a
		// project need not be version controlled.
		return filepath.Clean(dir), nil
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return filepath.Clean(dir), nil
	}
	return filepath.Clean(root), nil
}
