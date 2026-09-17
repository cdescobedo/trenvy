package ui

import (
	"sort"

	"github.com/cdescobedo/trenvy/internal/manifest"
)

// Matrix is the rows-by-profiles grid behind the management TUI.
type Matrix struct {
	Names    []string                  // sorted variable names
	Profiles []string                  // manifest order
	Vars     map[string]manifest.Var
	Stored   map[string]map[string]bool // name -> profile -> keyring presence
}

// NewMatrix builds a matrix from a manifest and keyring presence map.
func NewMatrix(m *manifest.Manifest, stored map[string]map[string]bool) *Matrix {
	names := make([]string, 0, len(m.Vars))
	for name := range m.Vars {
		names = append(names, name)
	}
	sort.Strings(names)

	// Profiles keep the manifest's order while Names are sorted. Variable
	// names have no meaningful order, so sorting makes the view stable
	// between renders; profile order is the user's own choice, and
	// re-sorting it would reshuffle their columns for no reason.
	profiles := make([]string, len(m.Project.Profiles))
	copy(profiles, m.Project.Profiles)

	return &Matrix{
		Names:    names,
		Profiles: profiles,
		Vars:     m.Vars,
		Stored:   stored,
	}
}

// Cell reports how a variable resolves under one profile.
// It delegates to Classify, passing the keyring presence from Stored.
func (mx *Matrix) Cell(name, profile string) Cell {
	v := mx.Vars[name]
	// A nil map read returns false, which is the correct default: no keyring item known.
	return Classify(v, profile, mx.Stored[name][profile])
}
