package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/cdescobedo/envctl/internal/fsutil"
)

// ErrNoProject is returned when a directory belongs to no registered project.
var ErrNoProject = errors.New("no envctl project registered for this directory")

// Entry maps a project root on disk to a project id.
type Entry struct {
	ID   string `toml:"id"`
	Root string `toml:"root"`
}

// Registry is the set of registered projects.
type Registry struct {
	Projects []Entry `toml:"project"`
}

// Lookup finds the project owning dir by walking up the path. The deepest
// registered ancestor wins, which allows a project nested inside a monorepo
// to shadow the outer one.
//
// Matching is on path boundaries, not string prefixes: /w/repo-old is not
// inside /w/repo.
func (r *Registry) Lookup(dir string) (Entry, bool) {
	dir = filepath.Clean(dir)
	var best Entry
	var bestLen int
	found := false

	for _, e := range r.Projects {
		root := filepath.Clean(e.Root)
		if !underOrEqual(dir, root) {
			continue
		}
		if !found || len(root) > bestLen {
			best, bestLen, found = e, len(root), true
		}
	}
	return best, found
}

func underOrEqual(dir, root string) bool {
	if dir == root {
		return true
	}
	return strings.HasPrefix(dir, root+string(filepath.Separator))
}

// Add registers a project. Both id and root must be unique: two entries with
// the same id would collide in the keyring namespace, and two with the same
// root would make Lookup ambiguous.
func (r *Registry) Add(e Entry) error {
	for _, existing := range r.Projects {
		if existing.ID == e.ID {
			return fmt.Errorf("project id %q is already registered for %s", e.ID, existing.Root)
		}
		if filepath.Clean(existing.Root) == filepath.Clean(e.Root) {
			return fmt.Errorf("%s is already registered as project %q", e.Root, existing.ID)
		}
	}
	e.Root = filepath.Clean(e.Root)
	r.Projects = append(r.Projects, e)
	return nil
}

// LoadRegistry reads registry.toml, returning an empty registry when absent.
func LoadRegistry() (*Registry, error) {
	data, err := os.ReadFile(RegistryPath())
	if errors.Is(err, fs.ErrNotExist) {
		return &Registry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", RegistryPath(), err)
	}
	var r Registry
	if err := toml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse %s: %w", RegistryPath(), err)
	}
	return &r, nil
}

// Save writes the registry back to disk.
func (r *Registry) Save() error {
	data, err := toml.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", Dir(), err)
	}
	if err := fsutil.WriteAtomic(RegistryPath(), data); err != nil {
		return fmt.Errorf("write %s: %w", RegistryPath(), err)
	}
	return nil
}
