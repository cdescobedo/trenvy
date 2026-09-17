package ui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/ui"
)

// overviewFixture builds a 3-profile, 3-variable matrix where exactly one
// cell is missing -- BBB under "staging", the middle profile, not the first
// -- so a jump test that happens to land on (0,0) cannot pass by accident.
func overviewFixture(t *testing.T) (*ui.Matrix, []ui.Problem) {
	t.Helper()
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev", "staging", "prod"}},
		Vars: map[string]manifest.Var{
			"AAA": {Value: s("a")},
			"BBB": {Profiles: map[string]string{"dev": "b", "prod": "c"}}, // missing under staging
			"CCC": {Value: s("c")},
		},
	}
	mx := ui.NewMatrix(m, nil)
	return mx, ui.Problems(mx, nil)
}

// The header repeats trenvy status's facts verbatim: project id, active
// profile and its source, manifest path, worktree, backend name -- the spec
// asks for all six; only project/profile/backend shipped originally (Fix 5).
func TestOverviewRendersHeaderFacts(t *testing.T) {
	mx, ps := overviewFixture(t)
	m := ui.New(ui.Params{
		Project:      "demo",
		Profile:      "staging",
		Source:       "--profile",
		Backend:      "keychain",
		ManifestPath: "/config/projects/demo.toml",
		Worktree:     "/home/demo/app",
		Matrix:       mx,
		Problems:     ps,
	})

	v := m.View()
	for _, want := range []string{"demo", "staging", "--profile", "keychain", "/config/projects/demo.toml", "/home/demo/app"} {
		if !strings.Contains(v, want) {
			t.Errorf("overview header missing %q:\n%s", want, v)
		}
	}
}

// A healthy project is the state this screen is in most of the time, so a
// blank-looking panel here would read as broken rather than as good news.
func TestOverviewNoProblemsReadsAsHealthy(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Vars:    map[string]manifest.Var{"OK": {Value: s("v")}},
	}
	mx := ui.NewMatrix(m, nil)
	ps := ui.Problems(mx, nil)
	if len(ps) != 0 {
		t.Fatalf("setup: fixture has problems, want none: %+v", ps)
	}
	mod := ui.New(testParams(mx, ps))

	v := mod.View()
	if !strings.Contains(strings.ToLower(v), "no problems") {
		t.Errorf("overview does not say plainly that nothing needs attention:\n%s", v)
	}
}

// Each problem's line must carry both its Detail (what's wrong) and its Fix
// (the command that resolves it) -- a list that named the problem without
// its fix would defeat the Overview's whole purpose.
func TestOverviewListsEachProblemWithItsFix(t *testing.T) {
	mx, ps := overviewFixture(t)
	if len(ps) != 1 {
		t.Fatalf("setup: want exactly one problem, got %+v", ps)
	}
	m := ui.New(testParams(mx, ps))

	v := m.View()
	if !strings.Contains(v, ps[0].Detail) {
		t.Errorf("overview missing problem detail %q:\n%s", ps[0].Detail, v)
	}
	if !strings.Contains(v, ps[0].Fix) {
		t.Errorf("overview missing problem fix %q:\n%s", ps[0].Fix, v)
	}
}

// The load-bearing jump test: BBB/staging is neither the first row nor the
// first column of the fixture's 3x3 matrix, so a jump that lands on (0,0)
// cannot pass this even though it might pass a weaker test.
func TestEnterOnProblemJumpsToExactCell(t *testing.T) {
	mx, ps := overviewFixture(t)
	m := ui.New(testParams(mx, ps))

	m, _ = m.Update(ui.KeyType(tea.KeyEnter))

	if m.Tab() != ui.TabVariables {
		t.Fatalf("tab after enter = %v, want Variables", m.Tab())
	}
	const wantRow, wantCol = 1, 1 // BBB (2nd of AAA,BBB,CCC), staging (2nd of dev,staging,prod)
	if m.Row() != wantRow || m.Col() != wantCol {
		t.Errorf("cursor after jump = (%d,%d), want (%d,%d) -- BBB/staging", m.Row(), m.Col(), wantRow, wantCol)
	}
}

// An output-kind problem has no matrix cell: Profile is empty. Enter must
// not guess a cell for it -- it switches to the Outputs tab instead of
// leaving the matrix cursor's prior position looking like an answer.
func TestEnterOnOutputProblemSwitchesToOutputsNotAWrongCell(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev", "prod"}},
		Vars:    map[string]manifest.Var{"AAA": {Value: s("a")}},
	}
	mx := ui.NewMatrix(m, nil)
	ps := ui.Problems(mx, map[string]string{"deploy.env": ui.StateStale})
	if len(ps) != 1 || ps[0].Kind != "output" {
		t.Fatalf("setup: want exactly one output problem, got %+v", ps)
	}
	mod := ui.New(testParams(mx, ps))

	mod, _ = mod.Update(ui.KeyType(tea.KeyEnter))

	if mod.Tab() != ui.TabOutputs {
		t.Errorf("tab after enter on an output problem = %v, want Outputs", mod.Tab())
	}
	if mod.Row() != 0 || mod.Col() != 0 {
		t.Errorf("enter on an output problem moved the matrix cursor to (%d,%d), want it untouched", mod.Row(), mod.Col())
	}
}

// The Overview needs its own cursor: reusing row/col for the problem list
// would mean moving through problems silently moves the Variables matrix
// cursor too, and jumping would then land wherever that stray movement left
// it rather than where the user actually selected. This fixture has two
// problems whose fix cells are neither adjacent nor at (0,0), so moving the
// overview cursor once and then jumping must land on the SECOND problem's
// cell precisely.
func twoProblemFixture(t *testing.T) (*ui.Matrix, []ui.Problem) {
	t.Helper()
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev", "staging", "prod"}},
		Vars: map[string]manifest.Var{
			"AAA": {Profiles: map[string]string{"dev": "a-dev", "prod": "a-prod"}},       // missing staging
			"ZZZ": {Profiles: map[string]string{"dev": "z-dev", "staging": "z-staging"}}, // missing prod
		},
	}
	mx := ui.NewMatrix(m, nil)
	ps := ui.Problems(mx, nil)
	return mx, ps
}

func TestOverviewCursorMovesIndependentlyOfMatrixCursor(t *testing.T) {
	mx, ps := twoProblemFixture(t)
	if len(ps) != 2 {
		t.Fatalf("setup: want exactly two problems, got %+v", ps)
	}
	m := ui.New(testParams(mx, ps))

	// Moving the overview cursor must never touch the matrix cursor.
	m, _ = m.Update(ui.KeyRune('j'))
	if m.Row() != 0 || m.Col() != 0 {
		t.Fatalf("moving the overview cursor changed the matrix cursor to (%d,%d)", m.Row(), m.Col())
	}

	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	if m.Tab() != ui.TabVariables {
		t.Fatalf("tab after enter = %v, want Variables", m.Tab())
	}
	const wantRow, wantCol = 1, 2 // ZZZ (2nd of AAA,ZZZ), prod (3rd of dev,staging,prod)
	if m.Row() != wantRow || m.Col() != wantCol {
		t.Errorf("cursor after jump = (%d,%d), want (%d,%d) -- ZZZ/prod, the SECOND problem", m.Row(), m.Col(), wantRow, wantCol)
	}
}

// The reverse direction: moving the Variables matrix cursor around and
// switching tabs must never move the overview's own cursor. This is the
// regression Task 5's implementer flagged -- a shared field would make this
// fail.
func TestMatrixCursorMovementDoesNotAffectOverviewCursor(t *testing.T) {
	mx, ps := twoProblemFixture(t)
	m := ui.New(testParams(mx, ps))

	m, _ = m.Update(ui.KeyRune('j')) // overview cursor -> problem[1] (ZZZ/prod)
	m, _ = m.Update(ui.KeyRune('2')) // Variables tab
	m, _ = m.Update(ui.KeyRune('l')) // move the matrix cursor around
	m, _ = m.Update(ui.KeyRune('l'))
	m, _ = m.Update(ui.KeyRune('j'))
	m, _ = m.Update(ui.KeyRune('1')) // back to Overview

	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	if m.Tab() != ui.TabVariables {
		t.Fatalf("tab after enter = %v, want Variables", m.Tab())
	}
	const wantRow, wantCol = 1, 2 // still ZZZ/prod: the matrix fiddling above must not have moved it
	if m.Row() != wantRow || m.Col() != wantCol {
		t.Errorf("cursor after jump = (%d,%d), want (%d,%d); matrix cursor movement leaked into the overview cursor", m.Row(), m.Col(), wantRow, wantCol)
	}
}
