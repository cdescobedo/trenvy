package resolve

import (
	"errors"
	"fmt"
	"path"
	"sort"

	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/store"
)

// ErrUnknownTarget is returned when a target name is not defined.
var ErrUnknownTarget = errors.New("unknown target")

// ErrUnknownVar is returned when a variable name is not in the manifest.
var ErrUnknownVar = errors.New("unknown variable")

// SecretLookup fetches one secret by variable name. It is a function rather
// than a Store so that resolution does not need to know about service-name
// patterns, and so tests can supply values without a store at all.
type SecretLookup func(varName string) (string, error)

// Resolved is a target's fully resolved variable set. Names is sorted, which
// is what makes rendering and hashing deterministic.
type Resolved struct {
	Target string
	Names  []string
	Values map[string]string
}

// Values resolves every variable selected by target.
//
// For each variable the order is: per-target override, then default value,
// then the keyring for secrets. A variable with no value is an error unless it
// is optional, in which case it is omitted.
func Values(m *manifest.Manifest, target string, secret SecretLookup) (Resolved, error) {
	t, ok := m.Targets[target]
	if !ok {
		return Resolved{}, fmt.Errorf("%w: %q", ErrUnknownTarget, target)
	}

	out := Resolved{Target: target, Values: map[string]string{}}

	names := make([]string, 0, len(m.Vars))
	for name := range m.Vars {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		// Selection happens before any value is fetched, so a secret this
		// target excludes never costs an authorization prompt.
		sel, err := selected(name, t.Include, t.Exclude)
		if err != nil {
			return Resolved{}, fmt.Errorf("target %q: %w", target, err)
		}
		if !sel {
			continue
		}
		val, ok, err := Value(m, target, name, secret)
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

// Value resolves one variable for one target, applying the same precedence as
// Values: per-target override, then default value, then the keyring.
//
// ok is false when the variable resolves to nothing at all, which only an
// optional variable may do. Resolving one name reads at most one secret, so
// asking for a plain variable costs nothing and asking for a secret costs one
// authorization rather than one per variable in the target.
//
// Selection is deliberately not applied here: a caller asking for a name it
// already chose wants that name's value, and a caller iterating a target has
// already filtered. Use Selects to ask whether a target covers a name.
func Value(m *manifest.Manifest, target, name string, secret SecretLookup) (string, bool, error) {
	if _, ok := m.Targets[target]; !ok {
		return "", false, fmt.Errorf("%w: %q", ErrUnknownTarget, target)
	}
	v, ok := m.Vars[name]
	if !ok {
		return "", false, fmt.Errorf("%w: %q", ErrUnknownVar, name)
	}

	if override, ok := v.Targets[target]; ok {
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
				"%s is not in the keyring; run: envctl secret set %s", name, name)
		}
		if err != nil {
			return "", false, fmt.Errorf("read secret %s: %w", name, err)
		}
		return val, true, nil
	}
	if v.Optional {
		return "", false, nil
	}
	return "", false, fmt.Errorf("%s has no value for target %q", name, target)
}

// Selects reports whether target's include and exclude globs cover name.
func Selects(m *manifest.Manifest, target, name string) (bool, error) {
	t, ok := m.Targets[target]
	if !ok {
		return false, fmt.Errorf("%w: %q", ErrUnknownTarget, target)
	}
	sel, err := selected(name, t.Include, t.Exclude)
	if err != nil {
		return false, fmt.Errorf("target %q: %w", target, err)
	}
	return sel, nil
}

// TargetHasSecret reports whether target selects at least one secret
// variable, without reading any value. status uses this under --quick to
// decide whether freshness for a target is even knowable without a read.
func TargetHasSecret(m *manifest.Manifest, target string) (bool, error) {
	t, ok := m.Targets[target]
	if !ok {
		return false, fmt.Errorf("%w: %q", ErrUnknownTarget, target)
	}
	for name, v := range m.Vars {
		if !v.Secret {
			continue
		}
		sel, err := selected(name, t.Include, t.Exclude)
		if err != nil {
			return false, fmt.Errorf("target %q: %w", target, err)
		}
		if sel {
			return true, nil
		}
	}
	return false, nil
}

// selected applies the target's include and exclude globs. An empty include
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
