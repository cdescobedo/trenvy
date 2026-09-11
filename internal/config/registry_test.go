package config_test

import (
	"path/filepath"
	"testing"

	"github.com/cdescobedo/envctl/internal/config"
)

func TestLookupFindsAnAncestor(t *testing.T) {
	r := &config.Registry{Projects: []config.Entry{
		{ID: "vpb", Root: "/w/demo-backend"},
	}}

	// Every worktree under the registered root resolves to the same project.
	// This is what makes a newly created worktree need no setup at all.
	for _, dir := range []string{
		"/w/demo-backend",
		"/w/demo-backend/main",
		"/w/demo-backend/FEAT-4001/src/DemoApp.Api",
	} {
		got, ok := r.Lookup(dir)
		if !ok {
			t.Fatalf("Lookup(%q) found nothing", dir)
		}
		if got.ID != "vpb" {
			t.Errorf("Lookup(%q).ID = %q, want vpb", dir, got.ID)
		}
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

func TestAddRejectsDuplicateIDAndRoot(t *testing.T) {
	r := &config.Registry{}
	if err := r.Add(config.Entry{ID: "vpb", Root: "/w/vpb"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Add(config.Entry{ID: "vpb", Root: "/w/other"}); err == nil {
		t.Error("Add accepted a duplicate id")
	}
	if err := r.Add(config.Entry{ID: "other", Root: "/w/vpb"}); err == nil {
		t.Error("Add accepted a duplicate root")
	}
}

func TestRegistryRoundTripsThroughDisk(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENVCTL_CONFIG_DIR", dir)

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
