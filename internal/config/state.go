package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/cdescobedo/trenvy/internal/fsutil"
)

// RenderEntry records one render: what was written, where, and from what.
//
// Sum is the salted digest of the resolved values, so a change to the manifest
// or the keyring makes the file stale. File is the sha256 of the bytes written,
// so a hand edit is detectable as something different from staleness.
type RenderEntry struct {
	Worktree string    `toml:"worktree"`
	Output   string    `toml:"output"`
	Path     string    `toml:"path"`
	Sum      string    `toml:"sum"`
	File     string    `toml:"file"`
	At       time.Time `toml:"at"`
}

// State is one project's render bookkeeping, plus what this machine is
// currently working on.
//
// It lives beside the manifest rather than inside the rendered file because a
// json output has no comment syntax, and because public/env.json is served to
// browsers and must carry nothing but the application's own configuration.
type State struct {
	id      string
	Renders []RenderEntry `toml:"render"`

	// Active is the profile this machine is currently working on, set by
	// `profile use` and cleared by `profile use --clear`. It is distinct
	// from the manifest's default_profile: default is what the project IS,
	// active is what has been done to it on this machine. omitempty keeps a
	// project that has never run `profile use` free of an `active = ''` key
	// that would mean nothing.
	Active string `toml:"active,omitempty"`
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

// Find returns the record for one worktree and output.
func (s *State) Find(worktree, output string) (RenderEntry, bool) {
	worktree = filepath.Clean(worktree)
	for _, e := range s.Renders {
		if filepath.Clean(e.Worktree) == worktree && e.Output == output {
			return e, true
		}
	}
	return RenderEntry{}, false
}

// Record stores an entry, replacing any existing one for the same worktree and
// output.
func (s *State) Record(e RenderEntry) {
	e.Worktree = filepath.Clean(e.Worktree)
	for i, existing := range s.Renders {
		if filepath.Clean(existing.Worktree) == e.Worktree && existing.Output == e.Output {
			s.Renders[i] = e
			return
		}
	}
	s.Renders = append(s.Renders, e)
}

// RebaseWorktrees rewrites every render record whose worktree sits under
// oldRoot so that it sits under newRoot instead, returning how many moved.
//
// Without this, a project move silently breaks the render guard: the
// worktree path is absolute, Find stops matching, and guard.Inspect
// classifies an already-rendered file as FOREIGN because it has no record --
// the very guard meant to protect the user starts fighting them.
//
// Matching reuses underOrEqual, the same path-boundary check Registry.Lookup
// uses, so /old/proj-other is not treated as inside /old/proj just because it
// shares a string prefix.
func (s *State) RebaseWorktrees(oldRoot, newRoot string) int {
	oldRoot = filepath.Clean(oldRoot)
	newRoot = filepath.Clean(newRoot)
	moved := 0
	for i, e := range s.Renders {
		wt := filepath.Clean(e.Worktree)
		if !underOrEqual(wt, oldRoot) {
			continue
		}
		rel, err := filepath.Rel(oldRoot, wt)
		if err != nil {
			continue
		}
		s.Renders[i].Worktree = filepath.Clean(filepath.Join(newRoot, rel))
		moved++
	}
	return moved
}

// RenameOutput repoints every render record from one output name to another,
// returning how many moved.
//
// Render records key on the output's name, so renaming it in the manifest
// alone makes State.Find miss and guard.Inspect classify every already
// rendered file as FOREIGN -- render then refuses to touch a file it wrote
// itself. This is the same coupling RebaseWorktrees exists for.
func (s *State) RenameOutput(old, replacement string) int {
	moved := 0
	for i, e := range s.Renders {
		if e.Output != old {
			continue
		}
		s.Renders[i].Output = replacement
		moved++
	}
	return moved
}

// MarkStale blanks the Sum of every record for output, in every worktree,
// returning how many it blanked.
//
// Sum covers only the resolved values, so a change that rewrites the file
// without changing a value -- the output's format -- leaves the record
// reading fresh for a file in the old format. A blank Sum never matches,
// so guard.Inspect reads that file STALE, which render may overwrite;
// forgetting the record instead would read FOREIGN, which it refuses.
func (s *State) MarkStale(output string) int {
	n := 0
	for i, e := range s.Renders {
		if e.Output == output {
			s.Renders[i].Sum = ""
			n++
		}
	}
	return n
}

// Forget removes a record, used when a rendered file is cleaned.
func (s *State) Forget(worktree, output string) {
	worktree = filepath.Clean(worktree)
	out := s.Renders[:0]
	for _, e := range s.Renders {
		if filepath.Clean(e.Worktree) == worktree && e.Output == output {
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
	if err := fsutil.WriteAtomic(path, data); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
