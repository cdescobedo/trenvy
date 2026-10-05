package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/session"
)

// gitIgnoring makes dir a git repository that ignores the given paths,
// which render's gitignore guard requires before it writes.
func gitIgnoring(t *testing.T, dir string, paths ...string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(strings.Join(paths, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The interface's Render now and `trenvy render` must be one code path.
func TestRenderOutputWritesTheFileAndRecordsIt(t *testing.T) {
	opts, app := newOutputsApp(t)
	gitIgnoring(t, app.Worktree, ".env")
	rep, err := renderOutput(opts, app, "local", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := rep.String(); got != "wrote .env  (1 vars, 0 from the keyring, 0600)" {
		t.Errorf("report = %q", got)
	}
	info, err := os.Stat(filepath.Join(app.Worktree, ".env"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("file: %v, %v", info, err)
	}
	if _, ok := app.State.Find(app.Worktree, "local"); !ok {
		t.Error("no render record")
	}
}

func TestCleanOutputRemovesTheFileAndForgetsTheRecord(t *testing.T) {
	opts, app := newOutputsApp(t)
	gitIgnoring(t, app.Worktree, ".env")
	if _, err := renderOutput(opts, app, "local", false, false); err != nil {
		t.Fatal(err)
	}
	got, err := cleanOutput(app, "local", false)
	if err != nil || got != "removed .env" {
		t.Fatalf("clean = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(app.Worktree, ".env")); !os.IsNotExist(err) {
		t.Error("the file is still there")
	}
	if _, ok := app.State.Find(app.Worktree, "local"); ok {
		t.Error("the render record was not forgotten")
	}
}

func TestCheckOutputReadsFreshnessForReal(t *testing.T) {
	opts, app := newOutputsApp(t)
	gitIgnoring(t, app.Worktree, ".env")
	if _, err := renderOutput(opts, app, "local", false, false); err != nil {
		t.Fatal(err)
	}
	if got := checkOutput(opts, app, "local"); got != session.StateFresh {
		t.Fatalf("right after render = %q, want %q", got, session.StateFresh)
	}
	v := app.Manifest.Vars["APP_URL"]
	changed := "changed"
	v.Value = &changed
	app.Manifest.Vars["APP_URL"] = v
	if got := checkOutput(opts, app, "local"); got != session.StateStale {
		t.Errorf("after a value change = %q, want %q", got, session.StateStale)
	}
}
