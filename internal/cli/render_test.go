package cli_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/resolve"
)

// gitInit turns the harness root into a repo with a .gitignore, since the
// render guard refuses to write a path git would track.
func (h *harness) gitInit(ignore string) {
	h.t.Helper()
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = h.root
	if out, err := cmd.CombinedOutput(); err != nil {
		h.t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(h.root, ".gitignore"), []byte(ignore), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func TestRenderWritesTheOutputFile(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k3y")

	if err := h.run("render", "docker"); err != nil {
		t.Fatalf("render: %v", err)
	}

	path := filepath.Join(h.root, "src", ".env")
	body := mustReadFile(t, path)
	if !strings.Contains(body, `APP_URL="http://+:8080"`) {
		t.Errorf("missing plain value:\n%s", body)
	}
	if !strings.Contains(body, `ALPHA_TOKEN="k3y"`) {
		t.Errorf("missing secret value:\n%s", body)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600", got)
	}
}

func TestRenderRecordsState(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k")

	if err := h.run("render", "docker"); err != nil {
		t.Fatal(err)
	}
	st, err := config.LoadState("vpb")
	if err != nil {
		t.Fatal(err)
	}
	// resolve.WorktreeRoot shells out to `git rev-parse --show-toplevel`,
	// which returns a symlink-resolved path (e.g. macOS's /var ->
	// /private/var). Compare against the same resolved form h.root names on
	// disk, since that is what render recorded.
	root, err := filepath.EvalSymlinks(h.root)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := st.Find(root, "docker")
	if !ok {
		t.Fatal("render recorded no state entry")
	}
	if entry.Sum == "" || entry.File == "" {
		t.Errorf("state entry is incomplete: %+v", entry)
	}
}

// The guard that matters most: a rendered secret must never be committable.
func TestRenderRefusesAPathGitWouldTrack(t *testing.T) {
	h := newHarness(t)
	h.gitInit("node_modules/\n")
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k")

	err := h.run("render", "docker")
	if err == nil {
		t.Fatal("render wrote to a path git would track")
	}
	if !strings.Contains(err.Error(), ".gitignore") {
		t.Errorf("error does not explain the fix: %v", err)
	}
	if !strings.Contains(err.Error(), "src/.env") {
		t.Errorf("error does not name the path to add: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(h.root, "src", ".env")); statErr == nil {
		t.Error("the file was written despite the refusal")
	}
}

// The escape hatch on the guard that matters most needs its own coverage: a
// refactor that inverted the flag (skip GitIgnored's result rather than the
// call) would otherwise still pass every other render test.
func TestRenderAllowTrackedSkipsTheGitignoreGuard(t *testing.T) {
	h := newHarness(t)
	h.gitInit("node_modules/\n") // does not cover src/.env
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k")

	if err := h.run("render", "docker"); err == nil {
		t.Fatal("render wrote to a path git would track")
	}

	if err := h.run("render", "docker", "--allow-tracked"); err != nil {
		t.Fatalf("render --allow-tracked: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.root, "src", ".env")); err != nil {
		t.Errorf("--allow-tracked did not write the file: %v", err)
	}
}

func TestRenderRefusesToClobberAForeignFile(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k")

	dir := filepath.Join(h.root, "src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	handWritten := "A=hand written\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(handWritten), 0o600); err != nil {
		t.Fatal(err)
	}

	err := h.run("render", "docker")
	if err == nil {
		t.Fatal("render clobbered a file it did not write")
	}
	if got := mustReadFile(t, filepath.Join(dir, ".env")); got != handWritten {
		t.Error("the hand-written file was modified")
	}

	if err := h.run("render", "docker", "--force"); err != nil {
		t.Fatalf("render --force: %v", err)
	}
	if got := mustReadFile(t, filepath.Join(dir, ".env")); got == handWritten {
		t.Error("--force did not overwrite")
	}
}

// Successor to the old "render refuses a process target": there is no second
// kind of output any more, so the remaining way to name something render
// cannot write is to name something that does not exist. The refusal must
// still say what was wrong rather than writing an empty file to "".
func TestRenderRejectsAnUnknownOutput(t *testing.T) {
	h := newHarness(t)
	h.gitInit("")
	h.register("vpb", fixtureManifest(t))

	err := h.run("render", "local")
	if err == nil {
		t.Fatal("render accepted an output that is not defined")
	}
	if !errors.Is(err, resolve.ErrUnknownOutput) {
		t.Errorf("error = %v, want ErrUnknownOutput", err)
	}
	if !strings.Contains(err.Error(), "local") {
		t.Errorf("error does not name the output: %v", err)
	}
}

func TestRenderReportsAMissingSecretWithTheFix(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t))
	// ALPHA_TOKEN deliberately not seeded.

	err := h.run("render", "docker")
	if err == nil {
		t.Fatal("render succeeded with a missing secret")
	}
	if !strings.Contains(err.Error(), "envctl secret set ALPHA_TOKEN") {
		t.Errorf("error does not give the fix: %v", err)
	}
}

func TestCleanRemovesTheFileAndForgetsTheState(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k")
	if err := h.run("render", "docker"); err != nil {
		t.Fatal(err)
	}

	if err := h.run("clean", "docker"); err != nil {
		t.Fatalf("clean: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.root, "src", ".env")); err == nil {
		t.Error("the file is still there")
	}
	st, _ := config.LoadState("vpb")
	// Same resolved-vs-raw path mismatch as TestRenderRecordsState: Record
	// and Forget key off the git-resolved worktree root, so the lookup here
	// must use the same resolved form or it is vacuously not-found.
	root, err := filepath.EvalSymlinks(h.root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Find(root, "docker"); ok {
		t.Error("the state entry was not forgotten")
	}
}

// The FOREIGN freshness state clean must refuse: a user's hand-written file
// that envctl never rendered must survive a bare `envctl clean`, the same as
// render's guard, and only come off with --force.
func TestCleanRefusesAForeignFile(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t))

	dir := filepath.Join(h.root, "src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	handWritten := "A=hand written\n"
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(handWritten), 0o600); err != nil {
		t.Fatal(err)
	}

	err := h.run("clean", "docker")
	if err == nil {
		t.Fatal("clean removed a file envctl has no record of writing")
	}
	if got := mustReadFile(t, path); got != handWritten {
		t.Error("the foreign file was modified")
	}

	if err := h.run("clean", "docker", "--force"); err != nil {
		t.Fatalf("clean --force: %v", err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("--force did not remove the foreign file")
	}
}

func TestCleanOnAnAlreadyCleanOutputIsNotAnError(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t))
	if err := h.run("clean", "docker"); err != nil {
		t.Errorf("clean on a missing file: %v", err)
	}
}

// An output is no longer implied by a project: one that only ever feeds exec
// and shell has none at all. A bare `render` there must say what is missing
// and how to name one, not fail on an empty map lookup.
func TestRenderWithNoOutputAndNoDefaultSaysSo(t *testing.T) {
	h := newHarness(t)
	m, err := manifest.Parse([]byte(`
[project]
id = "proc"
profiles = ["dev"]
default_profile = "dev"
salt = "abc123"

[vars.APP_MODE]
value = "development"
`))
	if err != nil {
		t.Fatal(err)
	}
	h.register("proc", m)

	err = h.run("render")

	if err == nil {
		t.Fatal("render succeeded with no output named and none configured")
	}
	if !strings.Contains(err.Error(), "project.output") {
		t.Errorf("error does not name the missing setting: %v", err)
	}
	if !strings.Contains(err.Error(), "envctl render <output>") {
		t.Errorf("error does not say how to name one: %v", err)
	}
}
