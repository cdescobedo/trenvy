package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cdescobedo/trenvy/internal/config"
)

func TestStateRecordAndFind(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", t.TempDir())
	s, err := config.LoadState("p")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	e := config.RenderEntry{
		Worktree: "/w/main",
		Output:   "docker",
		Path:     "src/.env",
		Sum:      "abc",
		File:     "def",
		At:       time.Now().UTC().Truncate(time.Second),
	}
	s.Record(e)

	got, ok := s.Find("/w/main", "docker")
	if !ok {
		t.Fatal("Find returned nothing")
	}
	if got.Sum != "abc" || got.File != "def" {
		t.Errorf("Find = %+v", got)
	}
}

// A rendered file is per-worktree: the same output in a different worktree is
// a different record, and one being fresh says nothing about the other.
func TestStateKeysOnWorktreeAndOutput(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", t.TempDir())
	s, _ := config.LoadState("p")
	s.Record(config.RenderEntry{Worktree: "/w/main", Output: "docker", Sum: "one"})
	s.Record(config.RenderEntry{Worktree: "/w/FEAT-4001", Output: "docker", Sum: "two"})

	main, _ := s.Find("/w/main", "docker")
	other, _ := s.Find("/w/FEAT-4001", "docker")
	if main.Sum != "one" || other.Sum != "two" {
		t.Errorf("records collided: %q %q", main.Sum, other.Sum)
	}
	if _, ok := s.Find("/w/main", "web"); ok {
		t.Error("Find matched a different output")
	}
}

func TestStateRecordReplacesRatherThanAppends(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", t.TempDir())
	s, _ := config.LoadState("p")
	s.Record(config.RenderEntry{Worktree: "/w", Output: "t", Sum: "one"})
	s.Record(config.RenderEntry{Worktree: "/w", Output: "t", Sum: "two"})

	if len(s.Renders) != 1 {
		t.Errorf("Renders has %d entries, want 1", len(s.Renders))
	}
	got, _ := s.Find("/w", "t")
	if got.Sum != "two" {
		t.Errorf("Sum = %q, want the newer value", got.Sum)
	}
}

func TestStateForgetRemovesTheRecord(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", t.TempDir())
	s, _ := config.LoadState("p")
	s.Record(config.RenderEntry{Worktree: "/w", Output: "t", Sum: "one"})
	s.Forget("/w", "t")
	if _, ok := s.Find("/w", "t"); ok {
		t.Error("Find still returns a forgotten record")
	}
}

func TestStateRoundTripsThroughDisk(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", t.TempDir())
	s, _ := config.LoadState("p")
	s.Record(config.RenderEntry{Worktree: "/w", Output: "t", Sum: "abc", At: time.Now().UTC().Truncate(time.Second)})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	again, err := config.LoadState("p")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	got, ok := again.Find("/w", "t")
	if !ok || got.Sum != "abc" {
		t.Errorf("after reload Find = (%+v, %v)", got, ok)
	}
}

func TestLoadStateOnMissingFileIsEmptyNotAnError(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", t.TempDir())
	s, err := config.LoadState("never-rendered")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(s.Renders) != 0 {
		t.Errorf("Renders = %v, want empty", s.Renders)
	}
}

func TestStateSaveWritesFileWith0600Mode(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TRENVY_CONFIG_DIR", tmpDir)
	s, _ := config.LoadState("p")
	s.Record(config.RenderEntry{Worktree: "/w", Output: "t", Sum: "abc"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := config.StatePath("p")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat %s: %v", path, err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %#o, want 0600", perm)
	}
}

func TestStateSaveCreatesDir0700(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TRENVY_CONFIG_DIR", tmpDir)
	s, _ := config.LoadState("p")
	s.Record(config.RenderEntry{Worktree: "/w", Output: "t", Sum: "abc"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := config.StatePath("p")
	dir := filepath.Dir(path)
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat %s: %v", dir, err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %#o, want 0700", perm)
	}
}

// RenderEntry.Worktree is absolute. Leave it stale after a move and Find stops
// matching, so guard.Inspect sees a rendered file with no record, calls it
// FOREIGN, and render refuses without --force -- the guard fighting the user.
func TestRebaseWorktreesRewritesEveryEntryUnderTheOldRoot(t *testing.T) {
	s := &config.State{}
	s.Record(config.RenderEntry{Worktree: "/old/proj/main", Output: "app", Path: "src/.env"})
	s.Record(config.RenderEntry{Worktree: "/old/proj/feat", Output: "app", Path: "src/.env"})

	n := s.RebaseWorktrees("/old/proj", "/new/proj")

	if n != 2 {
		t.Errorf("rebased %d entries, want 2", n)
	}
	if _, ok := s.Find("/new/proj/main", "app"); !ok {
		t.Error("the main worktree entry was not rebased")
	}
}

// The relative position under the root must survive at any depth, not just
// one level down -- this pins filepath.Rel/filepath.Join rather than a
// single-segment substitution that would happen to pass at depth one.
func TestRebaseWorktreesPreservesDeepNesting(t *testing.T) {
	s := &config.State{}
	s.Record(config.RenderEntry{Worktree: "/old/proj/feat/sub", Output: "app"})

	n := s.RebaseWorktrees("/old/proj", "/new/proj")

	if n != 1 {
		t.Errorf("rebased %d entries, want 1", n)
	}
	if _, ok := s.Find("/new/proj/feat/sub", "app"); !ok {
		t.Error("the two-level-deep worktree entry was not rebased to the matching depth")
	}
}

// A path that merely shares a string prefix is not inside the root.
func TestRebaseWorktreesLeavesASiblingPathAlone(t *testing.T) {
	s := &config.State{}
	s.Record(config.RenderEntry{Worktree: "/old/proj-other/main", Output: "app"})

	if n := s.RebaseWorktrees("/old/proj", "/new/proj"); n != 0 {
		t.Errorf("rebased %d entries, want 0", n)
	}
}

func TestRenameOutputMovesOnlyMatchingRecords(t *testing.T) {
	s := &config.State{Renders: []config.RenderEntry{
		{Worktree: "/w/main", Output: "app", Path: "a.env"},
		{Worktree: "/w/feat", Output: "app", Path: "a.env"},
		{Worktree: "/w/main", Output: "web", Path: "e.json"},
	}}

	moved := s.RenameOutput("app", "docker")

	if moved != 2 {
		t.Errorf("moved = %d, want 2", moved)
	}
	for _, e := range s.Renders {
		if e.Output == "app" {
			t.Errorf("a record still names the old output: %+v", e)
		}
	}
	if s.Renders[2].Output != "web" {
		t.Errorf("an unrelated record was rewritten: %+v", s.Renders[2])
	}
}

func TestStateSaveTightensExistingFilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TRENVY_CONFIG_DIR", tmpDir)

	stateDir := filepath.Join(tmpDir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(stateDir, "p.toml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o644 {
		t.Errorf("initial file mode = %#o, want 0644", perm)
	}

	// Save should tighten it to 0o600
	s, _ := config.LoadState("p")
	s.Record(config.RenderEntry{Worktree: "/w", Output: "t", Sum: "abc"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fi, err = os.Stat(path)
	if err != nil {
		t.Fatalf("Stat after Save: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("after Save file mode = %#o, want 0600", perm)
	}
}
