package cli_test

import (
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
