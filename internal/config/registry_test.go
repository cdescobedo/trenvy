package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
)

func TestLookupFindsAnAncestor(t *testing.T) {
	r := &config.Registry{Projects: []config.Entry{
		{ID: "vpb", Root: "/w/demo-backend"},
	}}

	// Every worktree under the registered root resolves to the same project.
	// This is what makes a newly created worktree need no setup at all.
	tests := map[string]string{
		"root":     "/w/demo-backend",
		"worktree": "/w/demo-backend/main",
		"deep":     "/w/demo-backend/FEAT-4001/src/DemoApp.Api",
	}
	for name, dir := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := r.Lookup(dir)
			if !ok {
				t.Fatalf("Lookup(%q) found nothing", dir)
			}
			if got.ID != "vpb" {
				t.Errorf("Lookup(%q).ID = %q, want vpb", dir, got.ID)
			}
		})
	}
}

func TestLookupMissesUnrelatedDirs(t *testing.T) {
	r := &config.Registry{Projects: []config.Entry{{ID: "vpb", Root: "/w/demo-backend"}}}
	if _, ok := r.Lookup("/w/other"); ok {
		t.Error("Lookup matched an unrelated directory")
	}
	// A sibling whose name merely shares a prefix must not match.
	if _, ok := r.Lookup("/w/demo-backend-old"); ok {
		t.Error("Lookup matched on a string prefix rather than a path boundary")
	}
}

func TestLookupPrefersTheDeepestMatch(t *testing.T) {
	r := &config.Registry{Projects: []config.Entry{
		{ID: "outer", Root: "/w"},
		{ID: "inner", Root: "/w/mono/services/api"},
	}}
	got, ok := r.Lookup("/w/mono/services/api/cmd")
	if !ok {
		t.Fatal("Lookup found nothing")
	}
	if got.ID != "inner" {
		t.Errorf("Lookup.ID = %q, want inner (deepest match wins)", got.ID)
	}
}

func TestAddRejectsDuplicateID(t *testing.T) {
	r := &config.Registry{}
	if err := r.Add(config.Entry{ID: "vpb", Root: "/w/vpb"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Add(config.Entry{ID: "vpb", Root: "/w/other"}); err == nil {
		t.Error("Add accepted a duplicate id")
	}
}

func TestAddRejectsDuplicateRoot(t *testing.T) {
	r := &config.Registry{}
	if err := r.Add(config.Entry{ID: "vpb", Root: "/w/vpb"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Add(config.Entry{ID: "other", Root: "/w/vpb"}); err == nil {
		t.Error("Add accepted a duplicate root")
	}
}

func TestRegistryRoundTripsThroughDisk(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TRENVY_CONFIG_DIR", dir)

	r, err := config.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry on empty dir: %v", err)
	}
	root := filepath.Join(dir, "repo")
	if err := r.Add(config.Entry{ID: "p", Root: root}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	again, err := config.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	got, ok := again.Lookup(filepath.Join(root, "worktree"))
	if !ok || got.ID != "p" {
		t.Errorf("after reload Lookup = (%+v, %v)", got, ok)
	}
}

func TestAddCleansTrailingSeparator(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TRENVY_CONFIG_DIR", dir)

	r := &config.Registry{}
	// Register with trailing separator
	if err := r.Add(config.Entry{ID: "p", Root: "/w/vpb/"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify we can resolve a directory inside the root
	got, ok := r.Lookup("/w/vpb/main")
	if !ok {
		t.Fatal("Lookup failed to find ancestor with cleaned path")
	}
	if got.ID != "p" {
		t.Errorf("Lookup.ID = %q, want p", got.ID)
	}

	// Verify the persisted entry is canonical (no trailing separator)
	again, err := config.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if len(again.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(again.Projects))
	}
	if again.Projects[0].Root != "/w/vpb" {
		t.Errorf("persisted Root = %q, want /w/vpb (canonical)", again.Projects[0].Root)
	}
}

func TestSaveCreatesDirWithCorrectMode(t *testing.T) {
	tempDir := t.TempDir()
	// Use a non-existent subdirectory so Save() actually creates it
	configDir := filepath.Join(tempDir, "trenvy-config")
	t.Setenv("TRENVY_CONFIG_DIR", configDir)

	r := &config.Registry{}
	if err := r.Add(config.Entry{ID: "p", Root: "/w/vpb"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Check directory permissions
	dirInfo, err := os.Stat(config.Dir())
	if err != nil {
		t.Fatalf("Stat config dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("config dir mode = %o, want 0700", dirInfo.Mode().Perm())
	}

	// Check registry.toml file permissions
	registryPath := config.RegistryPath()
	fileInfo, err := os.Stat(registryPath)
	if err != nil {
		t.Fatalf("Stat registry file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("registry.toml mode = %o, want 0600", fileInfo.Mode().Perm())
	}
}

func TestSetRootMovesAProject(t *testing.T) {
	r := &config.Registry{}
	if err := r.Add(config.Entry{ID: "p", Root: "/old/p"}); err != nil {
		t.Fatal(err)
	}

	if err := r.SetRoot("p", "/new/p"); err != nil {
		t.Fatalf("SetRoot: %v", err)
	}

	e, ok := r.Find("p")
	if !ok || e.Root != "/new/p" {
		t.Errorf("entry = %+v, want root /new/p", e)
	}
}

func TestSetRootRejectsARootAnotherProjectOwns(t *testing.T) {
	r := &config.Registry{}
	if err := r.Add(config.Entry{ID: "a", Root: "/x/a"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(config.Entry{ID: "b", Root: "/x/b"}); err != nil {
		t.Fatal(err)
	}

	err := r.SetRoot("a", "/x/b")

	if err == nil || !strings.Contains(err.Error(), "b") {
		t.Errorf("error = %v, want it to name the owning project", err)
	}
}

// Task 12 wires Remove to `project rm`, a destructive command, so both
// branches need pinning before it ships uncovered.
func TestRemoveDropsARegisteredProject(t *testing.T) {
	r := &config.Registry{}
	if err := r.Add(config.Entry{ID: "a", Root: "/x/a"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(config.Entry{ID: "b", Root: "/x/b"}); err != nil {
		t.Fatal(err)
	}

	if ok := r.Remove("a"); !ok {
		t.Error("Remove returned false for a registered id")
	}

	if _, ok := r.Find("a"); ok {
		t.Error("the entry is still findable after Remove")
	}
	if len(r.Projects) != 1 || r.Projects[0].ID != "b" {
		t.Errorf("Projects = %+v, want only b left", r.Projects)
	}
}

func TestRemoveOnAnUnknownIDReportsFalseAndChangesNothing(t *testing.T) {
	r := &config.Registry{}
	if err := r.Add(config.Entry{ID: "a", Root: "/x/a"}); err != nil {
		t.Fatal(err)
	}

	if ok := r.Remove("nope"); ok {
		t.Error("Remove returned true for an id that was never registered")
	}
	if len(r.Projects) != 1 || r.Projects[0].ID != "a" {
		t.Errorf("Projects = %+v, want a left untouched", r.Projects)
	}
}

func TestRegistrySaveTightensExistingFilePermissions(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TRENVY_CONFIG_DIR", tempDir)

	// Pre-create the registry file at 0o644
	registryPath := config.RegistryPath()
	if err := os.WriteFile(registryPath, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Verify it's at 0o644
	fi, err := os.Stat(registryPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o644 {
		t.Errorf("initial file mode = %#o, want 0644", perm)
	}

	// Save should tighten it to 0o600
	r := &config.Registry{}
	if err := r.Add(config.Entry{ID: "p", Root: "/w/vpb"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify it's now 0o600
	fi, err = os.Stat(registryPath)
	if err != nil {
		t.Fatalf("Stat after Save: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("after Save file mode = %#o, want 0600", perm)
	}
}
