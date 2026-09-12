package manifest

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// ValidationError collects every problem found in one pass, so that a user
// fixing a manifest sees all of them rather than one per run.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "invalid manifest:\n  " + strings.Join(e.Problems, "\n  ")
}

// Validate checks the manifest's internal consistency. Error messages name
// variables and targets, never values.
func (m *Manifest) Validate() error {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if m.Project.ID == "" {
		add("project.id is required")
	}
	if m.Project.DefaultTarget == "" {
		add("project.default_target is required")
	} else if _, ok := m.Targets[m.Project.DefaultTarget]; !ok {
		add("project.default_target %q is not a defined target", m.Project.DefaultTarget)
	}
	if len(m.Targets) == 0 {
		add("at least one target is required")
	}

	for _, name := range sortedKeys(m.Targets) {
		t := m.Targets[name]
		switch t.Dest {
		case DestProcess:
			if t.Path != "" {
				add("target %q: dest = %q takes no path", name, DestProcess)
			}
		case DestFile:
			if t.Path == "" {
				add("target %q: dest = %q requires path", name, DestFile)
			} else if err := checkRelPath(t.Path); err != nil {
				add("target %q: %v", name, err)
			}
			if t.Format != FormatDotenv && t.Format != FormatJSON {
				add("target %q: format must be %q or %q", name, FormatDotenv, FormatJSON)
			}
		default:
			add("target %q: dest must be %q or %q", name, DestFile, DestProcess)
		}
	}

	for _, name := range sortedKeys(m.Vars) {
		v := m.Vars[name]
		if v.Secret && len(v.Targets) > 0 {
			add("var %q: a secret may not have per-target values; "+
				"storing one credential under several keyring items invites silent drift", name)
		}
		if v.Secret && v.Value != nil {
			add("var %q: a secret may not also carry a plain value; "+
				"the manifest must never contain a secret value", name)
		}
		if !v.Secret && v.Value == nil && len(v.Targets) == 0 && !v.Optional {
			add("var %q: needs value, targets, secret = true, or optional = true", name)
		}
		for _, tname := range sortedKeys(v.Targets) {
			if _, ok := m.Targets[tname]; !ok {
				add("var %q: override names unknown target %q", name, tname)
			}
		}
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

// checkRelPath rejects paths that are absolute or that escape the worktree.
// A target path is always resolved against a worktree root, so neither is
// meaningful and both are dangerous.
func checkRelPath(p string) error {
	if filepath.IsAbs(p) {
		return fmt.Errorf("path %q must be relative to the worktree root", p)
	}
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q would escape the worktree root", p)
	}
	return nil
}

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
