package session_test

import (
	"reflect"
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
)

// TestMembershipCountsOutputsPerVariable pins the basic shape: a variable no
// output selects counts zero, one selected by exactly one output counts one,
// and one selected by every output counts every output.
func TestMembershipCountsOutputsPerVariable(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Outputs: map[string]manifest.Output{
			"api": {Format: manifest.FormatDotenv, Path: "api.env", Include: []string{"API_*"}},
			"web": {Format: manifest.FormatDotenv, Path: "web.env", Include: []string{"WEB_*"}},
		},
		Vars: map[string]manifest.Var{
			"API_KEY":  {Value: s("a")},
			"WEB_URL":  {Value: s("w")},
			"SHARED":   {Value: s("s")}, // matches neither include list below
			"EVERYONE": {Value: s("e")},
		},
	}
	// EVERYONE is picked up by both outputs via a second, catch-all output.
	m.Outputs["all"] = manifest.Output{Format: manifest.FormatDotenv, Path: "all.env", Include: []string{"EVERYONE"}}

	counts := session.Membership(m)

	want := map[string]int{
		"API_KEY":  1,
		"WEB_URL":  1,
		"SHARED":   0,
		"EVERYONE": 1,
	}
	for name, wantCount := range want {
		if counts[name] != wantCount {
			t.Errorf("Membership()[%q] = %d, want %d (counts=%v)", name, counts[name], wantCount, counts)
		}
	}
}

// TestMembershipUsesResolveSelectsNotAStringMatch mirrors
// TestGlobsEditorDerivesCheckedStateViaResolveSelects: PUBLIC_* is a pattern,
// not a literal name, so a hand-rolled "is this name in Include" check would
// undercount PUBLIC_URL (matches the pattern but isn't a literal member) and
// overcount PUBLIC_KEY (matches the pattern but is excluded).
func TestMembershipUsesResolveSelectsNotAStringMatch(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Outputs: map[string]manifest.Output{
			"out": {Format: manifest.FormatDotenv, Path: "out.env",
				Include: []string{"PUBLIC_*"}, Exclude: []string{"PUBLIC_KEY"}},
		},
		Vars: map[string]manifest.Var{
			"PUBLIC_URL":   {Value: s("u")},
			"PUBLIC_KEY":   {Value: s("k")},
			"SECRET_TOKEN": {Value: s("t")},
		},
	}
	counts := session.Membership(m)
	if counts["PUBLIC_URL"] != 1 {
		t.Errorf("PUBLIC_URL = %d, want 1 (matches PUBLIC_* and is not excluded)", counts["PUBLIC_URL"])
	}
	if counts["PUBLIC_KEY"] != 0 {
		t.Errorf("PUBLIC_KEY = %d, want 0 (matches PUBLIC_* but is excluded)", counts["PUBLIC_KEY"])
	}
	if counts["SECRET_TOKEN"] != 0 {
		t.Errorf("SECRET_TOKEN = %d, want 0 (does not match PUBLIC_*)", counts["SECRET_TOKEN"])
	}
}

// TestMembershipEveryVariableIsZeroWithNoOutputs is the exec/shell-only
// shape: a manifest with no outputs at all still reports every variable, all
// at zero, rather than an empty map that would make Problems unable to tell
// "no outputs defined" from "Membership was never asked".
func TestMembershipEveryVariableIsZeroWithNoOutputs(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Vars:    map[string]manifest.Var{"ONLY_VAR": {Value: s("v")}},
	}
	counts := session.Membership(m)
	if got, ok := counts["ONLY_VAR"]; !ok || got != 0 {
		t.Errorf("Membership() = %v, want ONLY_VAR present at 0", counts)
	}
}

func TestOutputsForNamesEveryOutputSelectingEachVariable(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Outputs: map[string]manifest.Output{
			"local": {Format: manifest.FormatDotenv, Path: ".env"},
			"web":   {Format: manifest.FormatJSON, Path: "web.json", Include: []string{"PUBLIC_*"}},
		},
		Vars: map[string]manifest.Var{"PUBLIC_URL": {Value: s("u")}, "DB": {Value: s("d")}},
	}
	got := session.OutputsFor(session.NewMatrix(m, nil))
	if !reflect.DeepEqual(got["PUBLIC_URL"], []string{"local", "web"}) || !reflect.DeepEqual(got["DB"], []string{"local"}) {
		t.Errorf("OutputsFor = %v", got)
	}
}
