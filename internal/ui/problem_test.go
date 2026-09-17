package ui_test

import (
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/ui"
)

func TestProblemsFindsMissingCellsPerProfile(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev", "prod"}},
		Vars: map[string]manifest.Var{
			"OK":   {Value: s("v")},
			"ONLY": {Profiles: map[string]string{"dev": "x"}},
		},
	}
	ps := ui.Problems(ui.NewMatrix(m, nil), nil)

	if len(ps) != 1 {
		t.Fatalf("Problems = %+v, want exactly one", ps)
	}
	if ps[0].Name != "ONLY" || ps[0].Profile != "prod" {
		t.Errorf("problem = %+v, want ONLY under prod", ps[0])
	}
	if !strings.Contains(ps[0].Fix, "set ONLY") {
		t.Errorf("Fix = %q, want it to name the set", ps[0].Fix)
	}
}

// An optional variable resolves to nothing on purpose. Reporting it as a
// problem would train the user to ignore the list.
func TestProblemsIgnoresOptional(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Vars:    map[string]manifest.Var{"OPT": {Optional: true}},
	}
	if ps := ui.Problems(ui.NewMatrix(m, nil), nil); len(ps) != 0 {
		t.Errorf("Problems = %+v, want none", ps)
	}
}

func TestProblemsReportsUnhealthyOutputs(t *testing.T) {
	m := &manifest.Manifest{Project: manifest.Project{Profiles: []string{"dev"}}}
	ps := ui.Problems(ui.NewMatrix(m, nil),
		map[string]string{"good": "fresh", "bad": "FOREIGN"})

	if len(ps) != 1 || ps[0].Name != "bad" {
		t.Errorf("Problems = %+v, want only the FOREIGN output", ps)
	}
}

// absent means "never rendered in this worktree" — the normal state of a
// fresh clone, not a problem. Pinning it here so it doesn't regress into a
// permanent false alarm for every new user.
func TestProblemsIgnoresAbsentOutput(t *testing.T) {
	m := &manifest.Manifest{Project: manifest.Project{Profiles: []string{"dev"}}}
	ps := ui.Problems(ui.NewMatrix(m, nil), map[string]string{"out": "absent"})
	if len(ps) != 0 {
		t.Errorf("Problems = %+v, want none for an absent output", ps)
	}
}

// Secrets get a different fix command than plain variables.
func TestProblemsFixNamesSecretSet(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Vars:    map[string]manifest.Var{"TOKEN": {Secret: true}},
	}
	ps := ui.Problems(ui.NewMatrix(m, nil), nil)
	if len(ps) != 1 {
		t.Fatalf("Problems = %+v, want exactly one", ps)
	}
	if !strings.Contains(ps[0].Fix, "secret set TOKEN") {
		t.Errorf("Fix = %q, want it to name secret set", ps[0].Fix)
	}
	if !strings.HasPrefix(ps[0].Fix, "trenvy -p dev ") {
		t.Errorf("Fix = %q, want it to carry the profile flag", ps[0].Fix)
	}
}

// Problems must sort by name then profile so the Overview list holds still
// between renders. Several problems across profiles, asserted in full
// expected order — not "contains" — so an accidentally-passing run (map
// iteration happening to come out sorted) can't slip by.
func TestProblemsAreSortedByNameThenProfile(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"prod", "dev", "stage"}},
		Vars: map[string]manifest.Var{
			"ZEBRA": {},
			"ALPHA": {},
			"MID":   {},
		},
	}
	ps := ui.Problems(ui.NewMatrix(m, nil), nil)

	want := []struct {
		name    string
		profile string
	}{
		{"ALPHA", "dev"}, {"ALPHA", "prod"}, {"ALPHA", "stage"},
		{"MID", "dev"}, {"MID", "prod"}, {"MID", "stage"},
		{"ZEBRA", "dev"}, {"ZEBRA", "prod"}, {"ZEBRA", "stage"},
	}
	if len(ps) != len(want) {
		t.Fatalf("Problems returned %d entries, want %d: %+v", len(ps), len(want), ps)
	}
	for i, w := range want {
		if ps[i].Name != w.name || ps[i].Profile != w.profile {
			t.Errorf("ps[%d] = {Name: %q, Profile: %q}, want {%q, %q}",
				i, ps[i].Name, ps[i].Profile, w.name, w.profile)
		}
	}
}
