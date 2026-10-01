package session_test

import (
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
)

func TestProblemsFindsMissingCellsPerProfile(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev", "prod"}},
		Vars: map[string]manifest.Var{
			"OK":   {Value: s("v")},
			"ONLY": {Profiles: map[string]string{"dev": "x"}},
		},
	}
	ps := session.Problems(session.NewMatrix(m, nil), nil)

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
	if ps := session.Problems(session.NewMatrix(m, nil), nil); len(ps) != 0 {
		t.Errorf("Problems = %+v, want none", ps)
	}
}

func TestProblemsReportsUnhealthyOutputs(t *testing.T) {
	m := &manifest.Manifest{Project: manifest.Project{Profiles: []string{"dev"}}}
	ps := session.Problems(session.NewMatrix(m, nil),
		map[string]string{"good": "fresh", "bad": "FOREIGN"})

	if len(ps) != 1 || ps[0].Name != "bad" {
		t.Errorf("Problems = %+v, want only the FOREIGN output", ps)
	}
}

// unknown must never leak CLI vocabulary (a flag like --quick) into the
// Overview, and must not offer `trenvy render` as the fix -- rendering does
// not resolve not-knowing, and can fail outright on a hand-edited file, which
// is exactly the case "unknown" cannot rule out. trenvy status is the command
// that actually resolves the question, so that's the Fix this state gets.
func TestProblemsGivesUnknownItsOwnFixNamingStatus(t *testing.T) {
	m := &manifest.Manifest{Project: manifest.Project{Profiles: []string{"dev"}}}
	ps := session.Problems(session.NewMatrix(m, nil), map[string]string{"app": "unknown"})

	if len(ps) != 1 || ps[0].Name != "app" {
		t.Fatalf("Problems = %+v, want exactly one problem for app", ps)
	}
	if ps[0].Fix != "trenvy status" {
		t.Errorf("Fix = %q, want %q", ps[0].Fix, "trenvy status")
	}
	if strings.Contains(ps[0].Detail, "--quick") {
		t.Errorf("Detail leaks a CLI flag name: %q", ps[0].Detail)
	}
	if strings.Contains(ps[0].Detail, "render") {
		t.Errorf("Detail mentions render, misleading advice for an unknown state: %q", ps[0].Detail)
	}
}

// absent means "never rendered in this worktree" — the normal state of a
// fresh clone, not a problem. Pinning it here so it doesn't regress into a
// permanent false alarm for every new user.
func TestProblemsIgnoresAbsentOutput(t *testing.T) {
	m := &manifest.Manifest{Project: manifest.Project{Profiles: []string{"dev"}}}
	ps := session.Problems(session.NewMatrix(m, nil), map[string]string{"out": session.StateAbsent})
	if len(ps) != 0 {
		t.Errorf("Problems = %+v, want none for an absent output", ps)
	}
}

// Fix 2 (CRITICAL, whole-branch review): StateUnverified means the same
// thing StateUnknown used to get blamed for BEFORE `trenvy ui` always ran in
// --quick mode -- a fact this view cannot check without reading a secret --
// but unlike StateUnknown it must NOT become a Problem: the caller
// (cli.outputStates) reaches it for EVERY secret-bearing output that is not
// absent, FOREIGN or hand-edited, on EVERY launch, because ui never turns
// --quick off. Flagging it here would be a permanent, unfixable-from-inside
// alarm on every healthy project with secrets -- precisely what an earlier
// fix on this state already removed once by choosing StateFresh, and
// precisely what choosing StateFresh then went on to hide a genuine STALE
// output behind. Neither extreme is right; StateUnverified is the third
// option, and it must render through the plain no-problem path like
// StateFresh and StateAbsent, not the flagged one like StateUnknown.
func TestProblemsIgnoresUnverifiedOutput(t *testing.T) {
	m := &manifest.Manifest{Project: manifest.Project{Profiles: []string{"dev"}}}
	ps := session.Problems(session.NewMatrix(m, nil), map[string]string{"out": session.StateUnverified})
	if len(ps) != 0 {
		t.Errorf("Problems = %+v, want none for an unverified output -- flagging it would be a permanent alarm", ps)
	}
}

// Secrets get a different fix command than plain variables.
func TestProblemsFixNamesSecretSet(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Vars:    map[string]manifest.Var{"TOKEN": {Secret: true}},
	}
	ps := session.Problems(session.NewMatrix(m, nil), nil)
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

// A variable no output selects and a variable two outputs select both reach
// the Overview; a variable exactly one output selects does not. This is
// Task 9's Step 1, both directions and the non-flagged middle case in one
// test so a fix for one side can't silently break the other.
func TestProblemsFlagsMembershipAtZeroAndSeveralNotOne(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Outputs: map[string]manifest.Output{
			"api": {Format: manifest.FormatDotenv, Path: "api.env", Include: []string{"API_*", "SHARED"}},
			"web": {Format: manifest.FormatDotenv, Path: "web.env", Include: []string{"WEB_*", "SHARED"}},
		},
		Vars: map[string]manifest.Var{
			"API_KEY": {Value: s("a")}, // exactly one output (api) -- must NOT appear
			"WEB_URL": {Value: s("w")}, // exactly one output (web) -- must NOT appear
			"SHARED":  {Value: s("s")}, // both outputs -- must appear
			"ORPHAN":  {Value: s("o")}, // no output -- must appear
		},
	}
	ps := session.Problems(session.NewMatrix(m, nil), nil)

	byName := map[string]session.Problem{}
	for _, p := range ps {
		if p.Kind == "membership" {
			byName[p.Name] = p
		}
	}
	if len(byName) != 2 {
		t.Fatalf("membership problems = %+v, want exactly SHARED and ORPHAN", byName)
	}
	if _, ok := byName["API_KEY"]; ok {
		t.Errorf("API_KEY is selected by exactly one output and must not be flagged: %+v", ps)
	}
	if _, ok := byName["WEB_URL"]; ok {
		t.Errorf("WEB_URL is selected by exactly one output and must not be flagged: %+v", ps)
	}

	orphan, ok := byName["ORPHAN"]
	if !ok {
		t.Fatalf("ORPHAN (selected by no output) was not flagged: %+v", ps)
	}
	if !strings.Contains(orphan.Detail, "ORPHAN") {
		t.Errorf("ORPHAN Detail = %q, want it to name the variable", orphan.Detail)
	}

	shared, ok := byName["SHARED"]
	if !ok {
		t.Fatalf("SHARED (selected by two outputs) was not flagged: %+v", ps)
	}
	if !strings.Contains(shared.Detail, "api") || !strings.Contains(shared.Detail, "web") {
		t.Errorf("SHARED Detail = %q, want it to name both outputs (api, web)", shared.Detail)
	}
}

// Fix 6 (Minor, whole-branch review): a healthy default two-output project
// -- both outputs selecting everything, the shape `trenvy output add` with
// no --include/--exclude produces -- puts every variable in BOTH outputs by
// construction, so the old "reaches more than one output" rule flagged
// EVERY variable, every time, on a project doing nothing wrong. The
// zero-output case was already suppressed as noise for exactly this
// reason (see TestProblemsSuppressesMembershipWhenProjectHasNoOutputs); this
// is its mirror for the several-outputs side. Every variable here reaches
// the same count (2), which is trivially also the modal count, so this
// covers the uniform case round 2's per-variable rule (below) must still
// get right, not just the case that motivated it originally.
func TestProblemsSuppressesReachesSeveralWhenEveryVariableHasTheSameCount(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Outputs: map[string]manifest.Output{
			"docker": {Format: manifest.FormatDotenv, Path: "docker.env", Include: []string{"*"}},
			"public": {Format: manifest.FormatJSON, Path: "public.json", Include: []string{"*"}},
		},
		Vars: map[string]manifest.Var{
			"APP_URL": {Value: s("a")}, // selected by both outputs, same as every other var here
			"TIMEOUT": {Value: s("t")}, // ditto -- uniform membership, nothing stands out
		},
	}
	ps := session.Problems(session.NewMatrix(m, nil), nil)
	for _, p := range ps {
		if p.Kind == "membership" {
			t.Errorf("Problems flagged membership on a uniform two-output project: %+v", p)
		}
	}
}

// Fix 6, round 2 (Minor, whole-branch review): the shape round 1 got wrong,
// and the one a user hits FIRST -- not an edge case. Round 1 suppressed
// "reaches several outputs" only when EVERY variable's count matched every
// other variable's, project-wide. The moment a project starts uniform (ten
// variables in both of two outputs) and someone uses the membership editor
// to carve an exception into ONE variable -- reaching a third output, the
// entire purpose of the editor Task 8 built -- round 1's rule un-suppressed
// ALL ELEVEN variables, burying the one genuine outlier under ten warnings
// that carry no information. The fix compares each variable's count to the
// MODAL count (the number of outputs most variables reach) instead of
// requiring the whole project to agree: exactly one warning, naming only
// the variable that actually differs from its peers.
func TestProblemsFlagsOnlyTheOutlierAmongManyUniformVariables(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Outputs: map[string]manifest.Output{
			"api":     {Format: manifest.FormatDotenv, Path: "api.env", Include: []string{"*"}},
			"web":     {Format: manifest.FormatDotenv, Path: "web.env", Include: []string{"*"}},
			"backend": {Format: manifest.FormatDotenv, Path: "backend.env", Include: []string{"ODD_ONE_OUT"}},
		},
		Vars: map[string]manifest.Var{
			// Ten variables in exactly two outputs (api, web) -- the modal,
			// unremarkable shape once ODD_ONE_OUT is also considered.
			"V01": {Value: s("1")}, "V02": {Value: s("1")}, "V03": {Value: s("1")}, "V04": {Value: s("1")},
			"V05": {Value: s("1")}, "V06": {Value: s("1")}, "V07": {Value: s("1")}, "V08": {Value: s("1")},
			"V09": {Value: s("1")}, "V10": {Value: s("1")},
			// The one outlier: also selected by "backend", so it reaches
			// THREE outputs while its ten peers reach two.
			"ODD_ONE_OUT": {Value: s("1")},
		},
	}
	ps := session.Problems(session.NewMatrix(m, nil), nil)

	var membership []session.Problem
	for _, p := range ps {
		if p.Kind == "membership" {
			membership = append(membership, p)
		}
	}
	if len(membership) != 1 {
		t.Fatalf("membership problems = %+v, want exactly one (the outlier), not one per variable", membership)
	}
	if membership[0].Name != "ODD_ONE_OUT" {
		t.Errorf("flagged %q, want the actual outlier ODD_ONE_OUT", membership[0].Name)
	}
	if !strings.Contains(membership[0].Detail, "3 outputs") {
		t.Errorf("Detail = %q, want it to say 3 outputs", membership[0].Detail)
	}
}

// Fix 6, round 2: when there is no single most-common count -- here, half
// the variables reach two outputs and half reach three, an even split with
// no majority -- "differs from the rest" has no rest to differ from. The
// decided answer is silence: guessing which half is the "normal" one and
// flagging the other would be exactly the kind of confident-but-arbitrary
// call this whole fix wave keeps finding and removing.
func TestProblemsStaysSilentWhenThereIsNoClearMajorityCount(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Outputs: map[string]manifest.Output{
			"a": {Format: manifest.FormatDotenv, Path: "a.env", Include: []string{"*"}},
			"b": {Format: manifest.FormatDotenv, Path: "b.env", Include: []string{"*"}},
			"c": {Format: manifest.FormatDotenv, Path: "c.env", Include: []string{"PAIR_ONE", "PAIR_TWO"}},
		},
		Vars: map[string]manifest.Var{
			// Two variables at three outputs (a, b, c)...
			"PAIR_ONE": {Value: s("1")}, "PAIR_TWO": {Value: s("1")},
			// ...and two at exactly two outputs (a, b) -- an even 2-vs-2
			// split, no count is more common than the other.
			"OTHER_ONE": {Value: s("1")}, "OTHER_TWO": {Value: s("1")},
		},
	}
	ps := session.Problems(session.NewMatrix(m, nil), nil)
	for _, p := range ps {
		if p.Kind == "membership" {
			t.Errorf("Problems flagged membership with no clear majority count: %+v", p)
		}
	}
}

// Neither membership condition is an error -- exec and shell need no output
// at all, and nothing stops two outputs from carrying the same variable on
// purpose -- so the wording must read as a fact, not a defect. This pins the
// words that must NOT appear, the same shape as
// TestProblemsGivesUnknownItsOwnFixNamingStatus above.
func TestProblemsMembershipWordingIsNotAlarming(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Outputs: map[string]manifest.Output{
			"out": {Format: manifest.FormatDotenv, Path: "out.env", Include: []string{"NONE_OF_THESE"}},
		},
		Vars: map[string]manifest.Var{"ORPHAN": {Value: s("o")}},
	}
	ps := session.Problems(session.NewMatrix(m, nil), nil)
	if len(ps) != 1 {
		t.Fatalf("Problems = %+v, want exactly one", ps)
	}
	for _, bad := range []string{"error", "missing", "invalid", "broken", "wrong"} {
		if strings.Contains(strings.ToLower(ps[0].Detail), bad) {
			t.Errorf("Detail = %q, reads like an error (contains %q)", ps[0].Detail, bad)
		}
	}
}

// trenvy render fixes nothing here -- there is no output to render this
// variable into. Five Things #2: a real command that does not address the
// stated problem is the same failure as a fabricated one.
func TestProblemsMembershipFixIsNotRender(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Outputs: map[string]manifest.Output{
			"out": {Format: manifest.FormatDotenv, Path: "out.env", Include: []string{"NONE_OF_THESE"}},
		},
		Vars: map[string]manifest.Var{"ORPHAN": {Value: s("o")}},
	}
	ps := session.Problems(session.NewMatrix(m, nil), nil)
	if len(ps) != 1 {
		t.Fatalf("Problems = %+v, want exactly one", ps)
	}
	if strings.Contains(ps[0].Fix, "render") {
		t.Errorf("Fix = %q, `trenvy render` fixes nothing here -- there is nothing to render it into", ps[0].Fix)
	}
	if !strings.Contains(ps[0].Fix, "Outputs tab") {
		t.Errorf("Fix = %q, want it to point at the Outputs tab, the actual way to change membership", ps[0].Fix)
	}
}

// A project with no outputs at all (the exec/shell-only shape) must not warn
// once per variable -- every variable is at zero there by definition, and
// that is normal, not broken (see viewOutputs' own placeholder for the same
// case). Five Things #3.
func TestProblemsSuppressesMembershipWhenProjectHasNoOutputs(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Vars: map[string]manifest.Var{
			"ONE": {Value: s("1")}, "TWO": {Value: s("2")}, "THREE": {Value: s("3")},
		},
	}
	ps := session.Problems(session.NewMatrix(m, nil), nil)
	if len(ps) != 0 {
		t.Errorf("Problems = %+v, want none: an exec/shell-only project has every variable at zero by definition", ps)
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
	ps := session.Problems(session.NewMatrix(m, nil), nil)

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
