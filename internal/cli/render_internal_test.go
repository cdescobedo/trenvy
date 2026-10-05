package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
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

// renderedLocal is newOutputsApp with local rendered to .env for real.
func renderedLocal(t *testing.T) (*Options, *appContext) {
	t.Helper()
	opts, app := newOutputsApp(t)
	gitIgnoring(t, app.Worktree, ".env", "moved.env")
	if _, err := renderOutput(opts, app, "local", false, false); err != nil {
		t.Fatal(err)
	}
	return opts, app
}

func reresolve(t *testing.T, opts *Options) *appContext {
	t.Helper()
	app, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	return app
}

// assertFormatEditLanded checks a committed json format edit on a
// rendered local: the dotenv file on disk reads stale, quick or checked,
// and Render now can write it again.
func assertFormatEditLanded(t *testing.T, opts *Options) {
	t.Helper()
	app := reresolve(t, opts)
	if f := app.Manifest.Outputs["local"].Format; f != "json" {
		t.Fatalf("format = %q, want json", f)
	}
	if got := stateWord(classifyOutput(opts, app, "local", app.Manifest.Outputs["local"], false, true)); got != session.StateStale {
		t.Errorf("state after a format edit = %q, want %q: the file on disk is still dotenv", got, session.StateStale)
	}
	if got := checkOutput(opts, app, "local"); got != session.StateStale {
		t.Errorf("Check now after a format edit = %q, want %q", got, session.StateStale)
	}
	if _, err := renderOutput(opts, app, "local", false, false); err != nil {
		t.Errorf("Render now after a format edit: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(app.Worktree, ".env")); !strings.HasPrefix(strings.TrimSpace(string(b)), "{") {
		t.Errorf("the file was not rewritten as json:\n%s", b)
	}
}

// assertPathEditLanded checks a committed path edit of a rendered local to
// moved.env: the old file is named as left in place, stays on disk, and
// its record is gone.
func assertPathEditLanded(t *testing.T, opts *Options, worktree string, left []string) {
	t.Helper()
	if want := worktree + "/.env"; len(left) != 1 || left[0] != want {
		t.Errorf("leftInPlace = %v, want [%s]", left, want)
	}
	app := reresolve(t, opts)
	if p := app.Manifest.Outputs["local"].Path; p != "moved.env" {
		t.Fatalf("path = %q, want moved.env", p)
	}
	if _, ok := app.State.Find(worktree, "local"); ok {
		t.Error("the render record for the old path is still there")
	}
	if _, err := os.Stat(filepath.Join(worktree, ".env")); err != nil {
		t.Errorf("the old file should stay on disk: %v", err)
	}
}

// I2: a format edit leaves the file in the old format, so the state must
// not read fresh.
func TestCommitFormatEditMakesTheRenderedFileStale(t *testing.T) {
	opts, app := renderedLocal(t)
	if _, err := commitStaged(opts, app, []session.Change{{Kind: session.ChangeSetOutputFormat, Name: "local", Value: "json"}}); err != nil {
		t.Fatal(err)
	}
	assertFormatEditLanded(t, opts)
}

// I2: a path edit forgets the old file, which may hold secrets, so it is
// named the way a removal names it.
func TestCommitPathEditNamesTheOldFileAndForgetsItsRecord(t *testing.T) {
	opts, app := renderedLocal(t)
	res, err := commitStaged(opts, app, []session.Change{{Kind: session.ChangeSetOutputPath, Name: "local", Value: "moved.env"}})
	if err != nil {
		t.Fatal(err)
	}
	assertPathEditLanded(t, opts, app.Worktree, res.leftInPlace)
}

// Phase 4 retry gate for a format edit: the state save fails, the retry
// of the whole batch ends where one clean commit would.
func TestCommitRetryAfterAStateSaveFailureOnAFormatEdit(t *testing.T) {
	opts, app := renderedLocal(t)
	batch := []session.Change{{Kind: session.ChangeSetOutputFormat, Name: "local", Value: "json"}}
	restore := lockDir(t, filepath.Dir(config.StatePath("vpb")))
	if _, err := commitStaged(opts, app, batch); err == nil {
		t.Fatal("first commit succeeded; the state directory is read-only")
	}
	restore()
	if f := loadVPB(t).Outputs["local"].Format; f != "dotenv" {
		t.Fatalf("manifest changed on a failed commit: format %q", f)
	}
	if _, err := commitStaged(opts, reresolve(t, opts), batch); err != nil {
		t.Fatalf("retry: %v", err)
	}
	assertFormatEditLanded(t, opts)
}

// Phase 4 retry gate for a path edit, through leftCarry as the interface
// commits: the old file is still named after the retry.
func TestCommitRetryAfterAStateSaveFailureOnAPathEdit(t *testing.T) {
	opts, app := renderedLocal(t)
	batch := []session.Change{{Kind: session.ChangeSetOutputPath, Name: "local", Value: "moved.env"}}
	var carry leftCarry
	restore := lockDir(t, filepath.Dir(config.StatePath("vpb")))
	if _, err := carry.commit(opts, app, batch); err == nil {
		t.Fatal("first commit succeeded; the state directory is read-only")
	}
	restore()
	if p := loadVPB(t).Outputs["local"].Path; p != ".env" {
		t.Fatalf("manifest changed on a failed commit: path %q", p)
	}
	res, err := carry.commit(opts, reresolve(t, opts), batch)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	assertPathEditLanded(t, opts, app.Worktree, res.LeftInPlace)
}

// The same retry after the state save landed and the manifest save failed:
// the retry finds no record to forget, so leftCarry is what names the file.
func TestCommitRetryAfterAManifestSaveFailureOnAPathEdit(t *testing.T) {
	opts, app := renderedLocal(t)
	batch := []session.Change{{Kind: session.ChangeSetOutputPath, Name: "local", Value: "moved.env"}}
	var carry leftCarry
	restore := lockDir(t, filepath.Dir(config.ManifestPath("vpb")))
	if _, err := carry.commit(opts, app, batch); err == nil || !strings.Contains(err.Error(), "left in place: "+app.Worktree+"/.env") {
		t.Fatalf("first commit: err = %v, want a failure naming the old file", err)
	}
	restore()
	res, err := carry.commit(opts, reresolve(t, opts), batch)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	assertPathEditLanded(t, opts, app.Worktree, res.LeftInPlace)
}
