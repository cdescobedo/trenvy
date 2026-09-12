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
		return cleanFallback(dir), nil
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return cleanFallback(dir), nil
	}
	return filepath.Clean(root), nil
}

// cleanFallback resolves symlinks before cleaning, so that two spellings of
// the same non-git directory (e.g. one through a symlinked ancestor) produce
// the same worktree root and therefore the same config.State key. git itself
// already returns a symlink-resolved path via rev-parse, so this keeps the
// fallback consistent with the git-backed case. If the directory cannot be
// resolved (it may not exist), the cleaned path is used as-is.
func cleanFallback(dir string) string {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	return filepath.Clean(resolved)
}
