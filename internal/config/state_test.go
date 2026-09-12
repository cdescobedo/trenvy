package config_test

import (
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
