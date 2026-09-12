package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// RenderEntry records one render: what was written, where, and from what.
//
// Sum is the salted digest of the resolved values, so a change to the manifest
// or the keyring makes the file stale. File is the sha256 of the bytes written,
// so a hand edit is detectable as something different from staleness.
type RenderEntry struct {
	Worktree string    `toml:"worktree"`
	Target   string    `toml:"target"`
	Path     string    `toml:"path"`
	Sum      string    `toml:"sum"`
	File     string    `toml:"file"`
	At       time.Time `toml:"at"`
}

// State is one project's render bookkeeping.
//
// It lives beside the manifest rather than inside the rendered file because a
// json target has no comment syntax, and because public/env.json is served to
// browsers and must carry nothing but the application's own configuration.
type State struct {
	id      string
	Renders []RenderEntry `toml:"render"`
}

// LoadState reads a project's state, returning an empty state when absent.
func LoadState(id string) (*State, error) {
	s := &State{id: id}
	data, err := os.ReadFile(StatePath(id))
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", StatePath(id), err)
	}
	if err := toml.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", StatePath(id), err)
	}
	return s, nil
}

// Find returns the record for one worktree and target.
func (s *State) Find(worktree, target string) (RenderEntry, bool) {
	worktree = filepath.Clean(worktree)
	for _, e := range s.Renders {
		if filepath.Clean(e.Worktree) == worktree && e.Target == target {
			return e, true
		}
	}
	return RenderEntry{}, false
}

// Record stores an entry, replacing any existing one for the same worktree and
// target.
func (s *State) Record(e RenderEntry) {
	e.Worktree = filepath.Clean(e.Worktree)
	for i, existing := range s.Renders {
		if filepath.Clean(existing.Worktree) == e.Worktree && existing.Target == e.Target {
			s.Renders[i] = e
			return
		}
	}
	s.Renders = append(s.Renders, e)
}

// Forget removes a record, used when a rendered file is cleaned.
func (s *State) Forget(worktree, target string) {
	worktree = filepath.Clean(worktree)
	out := s.Renders[:0]
	for _, e := range s.Renders {
		if filepath.Clean(e.Worktree) == worktree && e.Target == target {
			continue
		}
		out = append(out, e)
	}
	s.Renders = out
}

// Save writes the state back to disk.
func (s *State) Save() error {
	data, err := toml.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	path := StatePath(s.id)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
