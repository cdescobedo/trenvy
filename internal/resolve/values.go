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
		v := m.Vars[name]

		if override, ok := v.Targets[target]; ok {
			out.Names = append(out.Names, name)
			out.Values[name] = override
			continue
		}
		if v.Value != nil {
			out.Names = append(out.Names, name)
			out.Values[name] = *v.Value
			continue
		}
		if v.Secret {
			val, err := secret(name)
			if errors.Is(err, store.ErrNotFound) {
				if v.Optional {
					continue
				}
				return Resolved{}, fmt.Errorf(
					"%s is not in the keyring; run: envctl secret set %s", name, name)
			}
			if err != nil {
				return Resolved{}, fmt.Errorf("read secret %s: %w", name, err)
			}
			out.Names = append(out.Names, name)
			out.Values[name] = val
			continue
		}
		if v.Optional {
			continue
		}
		return Resolved{}, fmt.Errorf("%s has no value for target %q", name, target)
	}

	return out, nil
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
