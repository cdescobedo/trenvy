package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
)

func TestInitRegistersTheProjectAndWritesAManifest(t *testing.T) {
	h := newHarness(t)

	if err := h.run("project", "init", "--id", "vpb"); err != nil {
		t.Fatalf("init: %v", err)
	}

	reg, err := config.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := reg.Lookup(h.root)
	if !ok || entry.ID != "vpb" {
		t.Fatalf("registry Lookup = (%+v, %v)", entry, ok)
	}

	m, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatalf("Load manifest: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("a freshly created manifest does not validate: %v", err)
	}
	if m.Project.Salt == "" {
		t.Error("init did not generate a salt")
	}
}

// The project root is the directory holding the worktrees, not the worktree
// the user happens to be standing in. Registering a worktree would defeat the
// whole design: a new worktree would then need registering too.
func TestInitRegistersTheProjectRootNotTheWorktree(t *testing.T) {
	h := newHarness(t)
	worktree := filepath.Join(h.root, "main")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	h.opts.Dir = worktree

	if err := h.run("project", "init", "--id", "vpb", "--root", h.root); err != nil {
		t.Fatalf("init: %v", err)
	}

	reg, _ := config.LoadRegistry()
	entry, ok := reg.Lookup(filepath.Join(h.root, "FEAT-4001"))
	if !ok || entry.ID != "vpb" {
		t.Errorf("a sibling worktree does not resolve: (%+v, %v)", entry, ok)
	}
}

func TestInitDefaultsTheIDToTheRootDirectoryName(t *testing.T) {
	h := newHarness(t)
	if err := h.run("project", "init"); err != nil {
		t.Fatalf("init: %v", err)
	}
	reg, _ := config.LoadRegistry()
	entry, _ := reg.Lookup(h.root)
	if entry.ID != filepath.Base(h.root) {
		t.Errorf("ID = %q, want %q", entry.ID, filepath.Base(h.root))
	}
}

func TestInitRefusesToRegisterTwice(t *testing.T) {
	h := newHarness(t)
	if err := h.run("project", "init", "--id", "vpb"); err != nil {
		t.Fatal(err)
	}
	err := h.run("project", "init", "--id", "other")
	if err == nil {
		t.Fatal("init succeeded on an already-registered root")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("error is unclear: %v", err)
	}
}

func TestInitGivesEachProjectADifferentSalt(t *testing.T) {
	h1 := newHarness(t)
	if err := h1.run("project", "init", "--id", "a"); err != nil {
		t.Fatal(err)
	}
	ma, _ := manifest.Load(config.ManifestPath("a"))

	h2 := newHarness(t) // fresh config dir
	if err := h2.run("project", "init", "--id", "b"); err != nil {
		t.Fatal(err)
	}
	mb, _ := manifest.Load(config.ManifestPath("b"))

	if ma.Project.Salt == mb.Project.Salt {
		t.Error("two projects share a salt")
	}
}

// A fresh project needs no output: exec and shell are the verb, and the old
// model's dummy process target existed only so `exec local` had a noun. init
// must therefore produce a manifest that validates with no outputs at all.
func TestInitCreatesAManifestWithNoOutputs(t *testing.T) {
	h := newHarness(t)
	if err := h.run("project", "init", "--id", "vpb"); err != nil {
		t.Fatalf("init: %v", err)
	}

	m, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("a freshly created manifest does not validate: %v", err)
	}
	if len(m.Outputs) != 0 {
		t.Errorf("Outputs = %v, want none", m.Outputs)
	}
	if m.Project.Output != "" {
		t.Errorf("project.output = %q, want empty", m.Project.Output)
	}
}
