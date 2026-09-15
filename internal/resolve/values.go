package resolve

import (
	"errors"
	"fmt"
	"path"
	"sort"

	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/store"
)

// ErrUnknownOutput is returned when an output name is not defined.
var ErrUnknownOutput = errors.New("unknown output")

// ErrUnknownVar is returned when a variable name is not in the manifest.
var ErrUnknownVar = errors.New("unknown variable")

// SecretLookup fetches one secret by variable name. It is a function rather
// than a Store so that resolution does not need to know about service-name
// patterns, and so tests can supply values without a store at all.
type SecretLookup func(varName string) (string, error)

// Resolved is one output's fully resolved variable set under one profile.
// Names is sorted, which is what makes rendering and hashing deterministic.
//
// Output is empty for a set that belongs to no output -- what All returns.
type Resolved struct {
	Profile string
	Output  string
	Names   []string
	Values  map[string]string
}

// Values resolves every variable the output selects, under profile.
//
// The two string parameters are the two axes and they are not
// interchangeable: profile decides what each variable is worth, output
// decides which variables are in the set at all.
func Values(m *manifest.Manifest, profile, output string, secret SecretLookup) (Resolved, error) {
	o, ok := m.Outputs[output]
	if !ok {
		return Resolved{}, fmt.Errorf("%w: %q", ErrUnknownOutput, output)
	}

	out := Resolved{Profile: profile, Output: output, Values: map[string]string{}}

	for _, name := range sortedVarNames(m) {
		// Selection happens before any value is fetched, so a secret this
		// output excludes never costs an authorization prompt.
		sel, err := selected(name, o.Include, o.Exclude)
		if err != nil {
			return Resolved{}, fmt.Errorf("output %q: %w", output, err)
		}
		if !sel {
			continue
		}
		val, ok, err := Value(m, profile, name, secret)
		if err != nil {
			return Resolved{}, err
		}
		if !ok {
			continue
		}
		out.Names = append(out.Names, name)
		out.Values[name] = val
	}
	return out, nil
}

// All resolves every variable in the manifest under profile, with no output
// subset. exec and shell use it: an output's include list is about a file's
// audience, and a process wants its whole environment.
func All(m *manifest.Manifest, profile string, secret SecretLookup) (Resolved, error) {
	out := Resolved{Profile: profile, Values: map[string]string{}}
	for _, name := range sortedVarNames(m) {
		val, ok, err := Value(m, profile, name, secret)
		if err != nil {
			return Resolved{}, err
		}
		if !ok {
			continue
		}
		out.Names = append(out.Names, name)
		out.Values[name] = val
	}
	return out, nil
}

// sortedVarNames is the iteration order both resolvers use. Go randomises map
// iteration, so without sorting here a rendered file's byte order -- and so
// its recorded file digest -- would differ run to run.
func sortedVarNames(m *manifest.Manifest) []string {
	names := make([]string, 0, len(m.Vars))
	for name := range m.Vars {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Value resolves one variable under one profile, applying the precedence:
// per-profile value, then the shared default, then the keyring.
//
// ok is false when the variable resolves to nothing at all, which only an
// optional variable may do. Resolving one name reads at most one secret, so
// asking for a plain variable costs nothing and asking for a secret costs one
// authorization rather than one per variable in an output.
//
// Value deliberately does not validate that profile is declared: every CLI
// command resolves and validates the active profile before its body runs, so
// a name reaching here has already been checked against project.profiles.
func Value(m *manifest.Manifest, profile, name string, secret SecretLookup) (string, bool, error) {
	v, ok := m.Vars[name]
	if !ok {
		return "", false, fmt.Errorf("%w: %q", ErrUnknownVar, name)
	}

	if override, ok := v.Profiles[profile]; ok {
		return override, true, nil
	}
	if v.Value != nil {
		return *v.Value, true, nil
	}
	if v.Secret {
		val, err := secret(name)
		if errors.Is(err, store.ErrNotFound) {
			if v.Optional {
				return "", false, nil
			}
			return "", false, fmt.Errorf(
				"%s is not in the keyring for profile %q; run: envctl secret set %s",
				name, profile, name)
		}
		if err != nil {
			return "", false, fmt.Errorf("read secret %s: %w", name, err)
		}
		return val, true, nil
	}
	if v.Optional {
		return "", false, nil
	}
	return "", false, fmt.Errorf("%s has no value for profile %q", name, profile)
}

// Selects reports whether output's include and exclude globs cover name.
func Selects(m *manifest.Manifest, output, name string) (bool, error) {
	o, ok := m.Outputs[output]
	if !ok {
		return false, fmt.Errorf("%w: %q", ErrUnknownOutput, output)
	}
	sel, err := selected(name, o.Include, o.Exclude)
	if err != nil {
		return false, fmt.Errorf("output %q: %w", output, err)
	}
	return sel, nil
}

// OutputHasSecret reports whether output selects at least one secret
// variable, without reading any value. status uses this under --quick to
// decide whether freshness for an output is even knowable without a read.
func OutputHasSecret(m *manifest.Manifest, output string) (bool, error) {
	o, ok := m.Outputs[output]
	if !ok {
		return false, fmt.Errorf("%w: %q", ErrUnknownOutput, output)
	}
	for name, v := range m.Vars {
		if !v.Secret {
			continue
		}
		sel, err := selected(name, o.Include, o.Exclude)
		if err != nil {
			return false, fmt.Errorf("output %q: %w", output, err)
		}
		if sel {
			return true, nil
		}
	}
	return false, nil
}

// selected applies the output's include and exclude globs. An empty include
// list means everything; exclude is subtracted afterwards.
func selected(name string, include, exclude []string) (bool, error) {
	in := len(include) == 0
	for _, pat := range include {
		ok, err := path.Match(pat, name)
		if err != nil {
			return false, fmt.Errorf("invalid include pattern %q: %w", pat, err)
		}
		if ok {
			in = true
			break
		}
	}
	if !in {
		return false, nil
	}
	for _, pat := range exclude {
		ok, err := path.Match(pat, name)
		if err != nil {
			return false, fmt.Errorf("invalid exclude pattern %q: %w", pat, err)
		}
		if ok {
			return false, nil
		}
	}
	return true, nil
}
