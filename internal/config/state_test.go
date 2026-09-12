package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cdescobedo/envctl/internal/config"
)

func TestStateRecordAndFind(t *testing.T) {
	t.Setenv("ENVCTL_CONFIG_DIR", t.TempDir())
	s, err := config.LoadState("p")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	e := config.RenderEntry{
		Worktree: "/w/main",
		Target:   "docker",
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

// A rendered file is per-worktree: the same target in a different worktree is
// a different record, and one being fresh says nothing about the other.
func TestStateKeysOnWorktreeAndTarget(t *testing.T) {
	t.Setenv("ENVCTL_CONFIG_DIR", t.TempDir())
	s, _ := config.LoadState("p")
	s.Record(config.RenderEntry{Worktree: "/w/main", Target: "docker", Sum: "one"})
	s.Record(config.RenderEntry{Worktree: "/w/FEAT-4001", Target: "docker", Sum: "two"})

	main, _ := s.Find("/w/main", "docker")
	other, _ := s.Find("/w/FEAT-4001", "docker")
	if main.Sum != "one" || other.Sum != "two" {
		t.Errorf("records collided: %q %q", main.Sum, other.Sum)
	}
	if _, ok := s.Find("/w/main", "web"); ok {
		t.Error("Find matched a different target")
	}
}

func TestStateRecordReplacesRatherThanAppends(t *testing.T) {
	t.Setenv("ENVCTL_CONFIG_DIR", t.TempDir())
	s, _ := config.LoadState("p")
	s.Record(config.RenderEntry{Worktree: "/w", Target: "t", Sum: "one"})
	s.Record(config.RenderEntry{Worktree: "/w", Target: "t", Sum: "two"})

	if len(s.Renders) != 1 {
		t.Errorf("Renders has %d entries, want 1", len(s.Renders))
	}
	got, _ := s.Find("/w", "t")
	if got.Sum != "two" {
		t.Errorf("Sum = %q, want the newer value", got.Sum)
	}
}

func TestStateForgetRemovesTheRecord(t *testing.T) {
	t.Setenv("ENVCTL_CONFIG_DIR", t.TempDir())
	s, _ := config.LoadState("p")
	s.Record(config.RenderEntry{Worktree: "/w", Target: "t", Sum: "one"})
	s.Forget("/w", "t")
	if _, ok := s.Find("/w", "t"); ok {
		t.Error("Find still returns a forgotten record")
	}
}

func TestStateRoundTripsThroughDisk(t *testing.T) {
	t.Setenv("ENVCTL_CONFIG_DIR", t.TempDir())
	s, _ := config.LoadState("p")
	s.Record(config.RenderEntry{Worktree: "/w", Target: "t", Sum: "abc", At: time.Now().UTC().Truncate(time.Second)})
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
	t.Setenv("ENVCTL_CONFIG_DIR", t.TempDir())
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
	t.Setenv("ENVCTL_CONFIG_DIR", tmpDir)
	s, _ := config.LoadState("p")
	s.Record(config.RenderEntry{Worktree: "/w", Target: "t", Sum: "abc"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Stat the file that Save created
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
	t.Setenv("ENVCTL_CONFIG_DIR", tmpDir)
	s, _ := config.LoadState("p")
	s.Record(config.RenderEntry{Worktree: "/w", Target: "t", Sum: "abc"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Stat the directory that Save created
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

func TestStateSaveTightensExistingFilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ENVCTL_CONFIG_DIR", tmpDir)

	// Pre-create the file at 0o644
	stateDir := filepath.Join(tmpDir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(stateDir, "p.toml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Verify it's at 0o644
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o644 {
		t.Errorf("initial file mode = %#o, want 0644", perm)
	}

	// Save should tighten it to 0o600
	s, _ := config.LoadState("p")
	s.Record(config.RenderEntry{Worktree: "/w", Target: "t", Sum: "abc"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify it's now 0o600
	fi, err = os.Stat(path)
	if err != nil {
		t.Fatalf("Stat after Save: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("after Save file mode = %#o, want 0600", perm)
	}
}
