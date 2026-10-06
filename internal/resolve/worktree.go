// Package resolve turns a working directory, a profile and an output into concrete
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
// the specific checkout an output's relative path resolves against. Keeping
// them separate is what lets one manifest serve every worktree, including ones
// created long after the project was registered.
func WorktreeRoot(dir string) (string, error) {
	if root, ok := GitWorktreeRoot(dir); ok {
		return root, nil
	}
	return cleanFallback(dir), nil
}

// GitWorktreeRoot returns the root of the worktree containing dir and true, or
// false when dir is not inside a repository (or the tool is unavailable).
//
// WorktreeRoot folds that false into "dir itself", which is the right answer
// when the job is to locate a project. Callers that must distinguish the two
// cases need this instead: a directory that merely happens to hold a sample
// file is not a worktree root, and treating it as one collapses every
// relative path to a bare basename.
func GitWorktreeRoot(dir string) (string, bool) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		// Not a repository, or the tool is unavailable. Neither is an error:
		// a project need not be version controlled.
		return "", false
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", false
	}
	return filepath.Clean(root), true
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

// ProjectRootGuess is the project root setup offers for dir. In a bare-repo
// layout -- <root>/.bare beside sibling worktrees -- that is <root>, so every
// worktree finds the project; anywhere else it is dir, as `project init`
// defaults. The answer is spelled from dir as given rather than from git's
// symlink-resolved paths, so the registry's root matches what a shell inside
// it reports as its working directory.
func ProjectRootGuess(dir string) string {
	top, ok := GitWorktreeRoot(dir)
	if !ok {
		return dir
	}
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return dir
	}
	common := filepath.Clean(strings.TrimSpace(string(out)))
	if resolved, err := filepath.EvalSymlinks(common); err == nil {
		common = resolved
	}
	// An ordinary repository keeps its common dir inside the worktree
	// (<top>/.git); a bare layout keeps it beside the worktrees.
	if filepath.Dir(common) != filepath.Dir(top) {
		return dir
	}
	for a := filepath.Clean(dir); ; a = filepath.Dir(a) {
		if r, err := filepath.EvalSymlinks(a); err == nil && r == top {
			return filepath.Dir(a)
		}
		if filepath.Dir(a) == a {
			return dir
		}
	}
}
