package resolve_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cdescobedo/trenvy/internal/resolve"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

// newRepo creates a git repo with one commit and returns its path.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "add", "README")
	run(t, dir, "git", "commit", "-qm", "init")
	return dir
}

func TestWorktreeRootFromRepoSubdirectory(t *testing.T) {
	repo := newRepo(t)
	sub := filepath.Join(repo, "src", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolve.WorktreeRoot(sub)
	if err != nil {
		t.Fatalf("WorktreeRoot: %v", err)
	}
	assertSamePath(t, got, repo)
}

// The behaviour that must never regress: a linked worktree resolves to its own
// root, not to the main worktree, so one manifest can serve many worktrees.
func TestWorktreeRootInLinkedWorktree(t *testing.T) {
	repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(repo), "wt-FEAT-4001")
	run(t, repo, "git", "worktree", "add", "-q", wt)
	t.Cleanup(func() { _ = os.RemoveAll(wt) })

	got, err := resolve.WorktreeRoot(wt)
	if err != nil {
		t.Fatalf("WorktreeRoot: %v", err)
	}
	assertSamePath(t, got, wt)
}

func TestWorktreeRootFallsBackToDirOutsideGit(t *testing.T) {
	dir := t.TempDir()
	got, err := resolve.WorktreeRoot(dir)
	if err != nil {
		t.Fatalf("WorktreeRoot: %v", err)
	}
	assertSamePath(t, got, dir)
}

// Two spellings of the same non-git directory must resolve to the same
// worktree root, or staleness tracking silently splits into two
// config.State keys for what is really one project.
func TestWorktreeRootFallbackResolvesSymlinksToTheSameRoot(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link-to-real")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	gotReal, err := resolve.WorktreeRoot(real)
	if err != nil {
		t.Fatalf("WorktreeRoot(real): %v", err)
	}
	gotLink, err := resolve.WorktreeRoot(link)
	if err != nil {
		t.Fatalf("WorktreeRoot(link): %v", err)
	}
	if gotReal != gotLink {
		t.Errorf("WorktreeRoot(real) = %q, WorktreeRoot(link) = %q, want equal", gotReal, gotLink)
	}
}

// macOS resolves /var to /private/var, so paths must be compared after
// EvalSymlinks or these tests fail for reasons unrelated to the logic.
func assertSamePath(t *testing.T, got, want string) {
	t.Helper()
	g, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", got, err)
	}
	w, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", want, err)
	}
	if g != w {
		t.Errorf("WorktreeRoot = %q, want %q", g, w)
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

// bareLayout builds <root>/.bare with worktrees <root>/main and <root>/other,
// the layout this project itself uses.
func bareLayout(t *testing.T) string {
	t.Helper()
	requireGit(t)
	src := newRepo(t)
	root := t.TempDir()
	run(t, root, "git", "clone", "-q", "--bare", src, ".bare")
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ./.bare\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, root, "git", "worktree", "add", "-q", "main")
	run(t, root, "git", "worktree", "add", "-q", "-b", "other", "other")
	return root
}

func TestProjectRootGuessInABareLayout(t *testing.T) {
	root := bareLayout(t)
	for _, dir := range []string{filepath.Join(root, "main"), filepath.Join(root, "other")} {
		if got := resolve.ProjectRootGuess(dir); got != root {
			t.Errorf("ProjectRootGuess(%s) = %s, want %s", dir, got, root)
		}
	}
	sub := filepath.Join(root, "main", "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolve.ProjectRootGuess(sub); got != root {
		t.Errorf("from a subdirectory: %s, want %s", got, root)
	}
}

func TestProjectRootGuessElsewhereIsTheDirectory(t *testing.T) {
	requireGit(t)
	plain := t.TempDir()
	if got := resolve.ProjectRootGuess(plain); got != plain {
		t.Errorf("outside git: %s, want %s", got, plain)
	}
	repo := t.TempDir()
	run(t, repo, "git", "init", "-q")
	if got := resolve.ProjectRootGuess(repo); got != repo {
		t.Errorf("ordinary repository: %s, want %s", got, repo)
	}
}
