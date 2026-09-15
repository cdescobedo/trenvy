package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
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

// The id becomes a path segment of the manifest and state files, so init must
// judge it before it writes anything. Registering first and failing later
// would leave a manifest under projects/team/ that no command can reach.
func TestInitRefusesAnIDWithAPathSeparator(t *testing.T) {
	h := newHarness(t)

	err := h.run("project", "init", "--id", "team/app")

	if err == nil {
		t.Fatal("init accepted an id containing a separator")
	}
	if !strings.Contains(err.Error(), "--id") {
		t.Errorf("error does not name the flag to fix: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(os.Getenv("TRENVY_CONFIG_DIR"), "projects"))
	if len(entries) > 0 {
		t.Errorf("init wrote %d entry under projects/ despite rejecting the id", len(entries))
	}
}

func TestInitRefusesAnIDThatEscapesTheConfigDir(t *testing.T) {
	h := newHarness(t)
	cfg := os.Getenv("TRENVY_CONFIG_DIR")

	err := h.run("project", "init", "--id", "../../escape")

	if err == nil {
		t.Fatal("init accepted an id containing ..")
	}
	if !strings.Contains(err.Error(), "--id") {
		t.Errorf("error does not name the flag to fix: %v", err)
	}
	// projects/../../escape.toml resolves to a sibling of the config dir.
	escaped := filepath.Join(filepath.Dir(cfg), "escape.toml")
	if _, statErr := os.Stat(escaped); statErr == nil {
		t.Errorf("init wrote %s, outside the config directory", escaped)
	}
}

// The default id is the root directory's name, which the user did not choose
// with trenvy in mind. Refusing is right, but the error has to point at the
// way out rather than at the directory.
func TestInitRefusesADirectoryNameThatCannotBeAnID(t *testing.T) {
	h := newHarness(t)
	root := filepath.Join(h.root, "my project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	err := h.run("project", "init", "--root", root)

	if err == nil {
		t.Fatal("init accepted a directory name with a space as the id")
	}
	if !strings.Contains(err.Error(), "--id") {
		t.Errorf("error does not point at --id: %v", err)
	}
}
