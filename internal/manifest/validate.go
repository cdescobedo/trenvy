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
// variables and outputs, never values.
func (m *Manifest) Validate() error {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if m.Project.ID == "" {
		add("project.id is required")
	}
	if len(m.Project.Profiles) == 0 {
		add("project.profiles must name at least one profile")
	}
	seen := map[string]bool{}
	for _, p := range m.Project.Profiles {
		if seen[p] {
			add("project.profiles lists %q twice", p)
		}
		seen[p] = true
		if !validProfileName(p) {
			add("profile %q: names may use letters, digits, - and _ only; "+
				"a profile name becomes a segment of the keyring service name", p)
		}
	}
	if m.Project.DefaultProfile == "" {
		add("project.default_profile is required")
	} else if !seen[m.Project.DefaultProfile] {
		add("project.default_profile %q is not a declared profile", m.Project.DefaultProfile)
	}
	// A pre-profile manifest is refused by name rather than half-read. toml
	// ignores unknown keys, so an old [targets.*] block would otherwise
	// vanish and the project would look like one with no outputs at all --
	// which is now a legal shape, so nothing downstream would complain.
	if len(m.LegacyTargets) > 0 || m.Project.LegacyDefaultTarget != "" {
		add("this manifest predates profiles: [targets] and default_target are gone.\n" +
			"    Targets split into profiles (values) and outputs (format, path, subset),\n" +
			"    and process targets are gone because exec and shell are the verb.\n" +
			"    Recreate it with: trenvy project init, then trenvy import, then re-enter secrets")
	}
	// No output is required: a project that only ever feeds exec and shell
	// has nowhere to write, and the old model's dummy process target existed
	// only so that `exec local` had a noun to name.
	if m.Project.Output != "" {
		if _, ok := m.Outputs[m.Project.Output]; !ok {
			add("project.output %q is not a defined output", m.Project.Output)
		}
	}

	for _, name := range sortedKeys(m.Outputs) {
		o := m.Outputs[name]
		if o.Path == "" {
			add("output %q: path is required", name)
		} else if err := checkRelPath(o.Path); err != nil {
			add("output %q: %v", name, err)
		}
		if o.Format != FormatDotenv && o.Format != FormatJSON {
			add("output %q: format must be %q or %q", name, FormatDotenv, FormatJSON)
		}
	}

	for _, name := range sortedKeys(m.Vars) {
		v := m.Vars[name]
		if v.Secret && v.Value != nil {
			add("var %q: a secret may not also carry a plain value; "+
				"the manifest must never contain a secret value", name)
		}
		if v.Secret && len(v.Profiles) > 0 {
			add("var %q: a secret may not carry a plain per-profile value; "+
				"its value belongs in the keyring, per profile", name)
		}
		if !v.Secret && v.Value == nil && len(v.Profiles) == 0 && !v.Optional {
			add("var %q: needs value, a profile value, secret = true, or optional = true", name)
		}
		for _, pname := range sortedKeys(v.Profiles) {
			if !seen[pname] {
				add("var %q: override names undeclared profile %q", name, pname)
			}
		}
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

// checkRelPath rejects paths that are absolute or that escape the worktree.
// An output path is always resolved against a worktree root, so neither is
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

// validProfileName allows letters, digits, hyphen and underscore. The
// restriction exists because the name is interpolated into the keyring
// service name: a "/" there would reshape the namespace silently rather than
// fail, and two profiles could collide on one keyring item.
func validProfileName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_':
		default:
			return false
		}
	}
	return true
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
