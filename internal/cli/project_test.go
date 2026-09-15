package cli_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/config"
)

func TestProjectInitRegistersTheProject(t *testing.T) {
	h := newHarness(t)

	if err := h.run("project", "init", "--id", "demo"); err != nil {
		t.Fatalf("project init: %v", err)
	}

	reg, err := config.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Lookup(h.root); !ok {
		t.Error("the project was not registered")
	}
}

// init moved under project; leaving a working top-level alias would leave two
// spellings of one command in --help forever.
func TestTopLevelInitIsGone(t *testing.T) {
	h := newHarness(t)

	err := h.run("init", "--id", "demo")

	if err == nil {
		t.Fatal("top-level init still works")
	}
}

func TestProjectListShowsRegisteredProjects(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	if err := h.run("project", "list"); err != nil {
		t.Fatalf("project list: %v", err)
	}

	out := h.out.String()
	if !strings.Contains(out, "vpb") || !strings.Contains(out, h.root) {
		t.Errorf("output does not list the project and its root:\n%s", out)
	}
}

func TestProjectListOnAnEmptyRegistry(t *testing.T) {
	h := newHarness(t)

	if err := h.run("project", "list"); err != nil {
		t.Fatalf("project list: %v", err)
	}
	if !strings.Contains(h.out.String(), "envctl project init") {
		t.Errorf("output does not suggest how to register one:\n%s", h.out)
	}
}

func TestProjectMoveUpdatesTheRegistryAndTheRenderState(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	st, err := config.LoadState("vpb")
	if err != nil {
		t.Fatal(err)
	}
	st.Record(config.RenderEntry{
		Worktree: filepath.Join(h.root, "main"), Output: "app", Path: "src/.env",
	})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	newRoot := t.TempDir()
	if err := h.run("project", "move", "vpb", newRoot); err != nil {
		t.Fatalf("project move: %v", err)
	}

	reg, err := config.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	e, _ := reg.Find("vpb")
	if e.Root != newRoot {
		t.Errorf("root = %q, want %q", e.Root, newRoot)
	}

	after, err := config.LoadState("vpb")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.Find(filepath.Join(newRoot, "main"), "app"); !ok {
		t.Error("the render record was not rebased onto the new root")
	}
}

func TestProjectMoveRejectsARootThatDoesNotExist(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	err := h.run("project", "move", "vpb", filepath.Join(t.TempDir(), "nope"))

	if err == nil {
		t.Fatal("project move accepted a nonexistent root")
	}
}

func TestProjectMoveRejectsAnUnknownProject(t *testing.T) {
	h := newHarness(t)

	err := h.run("project", "move", "nope", t.TempDir())

	if err == nil {
		t.Fatal("project move accepted an unknown id")
	}
	// Pinned to the friendlier message, not just any error: SetRoot's own
	// internal "no project registered" guard would also make err non-nil,
	// so a bare err != nil check cannot tell the command's early, more
	// helpful check apart from that fallback.
	if !strings.Contains(err.Error(), "envctl project list") {
		t.Errorf("error does not point at envctl project list: %v", err)
	}
}

// The old root is left behind on disk by every test above (t.TempDir() is
// not removed mid-test), so this is really asserting on behavior the
// implementation already has -- there is no RED for a missing feature here,
// only for a missing assertion.
func TestProjectMoveWarnsWhenTheOldRootStillExists(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	newRoot := t.TempDir()
	if err := h.run("project", "move", "vpb", newRoot); err != nil {
		t.Fatalf("project move: %v", err)
	}

	stderr := h.errb.String()
	if !strings.Contains(stderr, h.root) {
		t.Errorf("stderr does not mention the old root %q:\n%s", h.root, stderr)
	}
	if !strings.Contains(stderr, "envctl project init") {
		t.Errorf("stderr does not suggest envctl project init:\n%s", stderr)
	}
}
