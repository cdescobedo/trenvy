package ui

import (
	"fmt"
	"sort"
)

// Problem is one thing wrong with the project, paired with the command that
// fixes it, for the Overview tab.
type Problem struct {
	Kind    string // "missing", "output"
	Name    string // variable or output name
	Profile string // empty for output problems
	Detail  string // human sentence
	Fix     string // the command or key that fixes it
}

// Problems walks the matrix for CellMissing entries and the outputs map for
// unhealthy render states, and returns both as a stable, sorted list.
//
// outputs maps an output name to its render state (fresh, STALE, MODIFIED,
// FOREIGN, absent). Computing that state means reading the rendered file and
// the state file, which is I/O this package does not do — the caller passes
// the answer in.
func Problems(mx *Matrix, outputs map[string]string) []Problem {
	var ps []Problem

	for _, name := range mx.Names {
		for _, profile := range mx.Profiles {
			if mx.Cell(name, profile) != CellMissing {
				continue
			}
			var fix string
			if mx.Vars[name].Secret {
				fix = fmt.Sprintf("trenvy -p %s secret set %s", profile, name)
			} else {
				fix = fmt.Sprintf("trenvy -p %s set %s <value>", profile, name)
			}
			ps = append(ps, Problem{
				Kind:    "missing",
				Name:    name,
				Profile: profile,
				Detail:  fmt.Sprintf("%s has no value under %s", name, profile),
				Fix:     fix,
			})
		}
	}

	// fresh means the render matches the manifest; absent means it has never
	// been rendered in this worktree, the normal state of a fresh clone.
	// Reporting absent as a problem would put a permanent false alarm in
	// front of every new user.
	for name, state := range outputs {
		if state == "fresh" || state == "absent" {
			continue
		}
		ps = append(ps, Problem{
			Kind:   "output",
			Name:   name,
			Detail: fmt.Sprintf("%s is %s", name, state),
			Fix:    fmt.Sprintf("trenvy render %s", name),
		})
	}

	// The Overview re-renders on every keystroke; an unstable order would
	// jump rows under the user's cursor and misdirect enter-to-jump later.
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].Name != ps[j].Name {
			return ps[i].Name < ps[j].Name
		}
		return ps[i].Profile < ps[j].Profile
	})

	return ps
}
