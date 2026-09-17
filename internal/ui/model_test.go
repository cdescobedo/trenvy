package ui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/ui"
)

// newTestModel builds a model over a two-profile, three-variable fixture:
// one shared, one overridden, one secret so the matrix has something under
// every Cell kind the later tabs will need to render.
func newTestModel(t *testing.T) ui.Model {
	t.Helper()
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev", "prod"}},
		Vars: map[string]manifest.Var{
			"SHARED":   {Value: s("v")},
			"OVERRIDE": {Value: s("a"), Profiles: map[string]string{"dev": "b"}},
			"TOKEN":    {Secret: true},
		},
	}
	mx := ui.NewMatrix(m, nil)
	ps := ui.Problems(mx, nil)
	return ui.New("demo", mx, ps)
}

func TestTabSwitchingByNumberAndTab(t *testing.T) {
	m := newTestModel(t)
	if m.Tab() != ui.TabOverview {
		t.Fatalf("opens on %v, want Overview", m.Tab())
	}
	m, _ = m.Update(ui.KeyRune('2'))
	if m.Tab() != ui.TabVariables {
		t.Errorf("after '2' = %v, want Variables", m.Tab())
	}
	m, _ = m.Update(ui.KeyType(tea.KeyTab))
	if m.Tab() != ui.TabProfiles {
		t.Errorf("after tab = %v, want Profiles", m.Tab())
	}
}

// tab from the last tab must return to the first rather than run off the end.
func TestTabWrapsAround(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('4'))
	if m.Tab() != ui.TabOutputs {
		t.Fatalf("after '4' = %v, want Outputs", m.Tab())
	}
	m, _ = m.Update(ui.KeyType(tea.KeyTab))
	if m.Tab() != ui.TabOverview {
		t.Errorf("tab from Outputs = %v, want it to wrap to Overview", m.Tab())
	}
}

// Profiles and Outputs are in the bar but inert in phase 1. They must say so
// and name the CLI that does the job, rather than showing an empty panel.
func TestInertTabsNameTheirCLI(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('3'))
	if v := m.View(); !strings.Contains(v, "trenvy profile") {
		t.Errorf("Profiles tab does not name its CLI:\n%s", v)
	}
	m, _ = m.Update(ui.KeyRune('4'))
	if v := m.View(); !strings.Contains(v, "trenvy output") {
		t.Errorf("Outputs tab does not name its CLI:\n%s", v)
	}
}

// The tab bar itself must list every tab name, so a user can see where '2',
// tab, and shift+tab will land before they press anything.
func TestTabBarListsAllTabs(t *testing.T) {
	m := newTestModel(t)
	v := m.View()
	for _, name := range []string{"Overview", "Variables", "Profiles", "Outputs"} {
		if !strings.Contains(v, name) {
			t.Errorf("tab bar missing %q:\n%s", name, v)
		}
	}
}

// allCellsModel builds a model whose matrix visits every Cell kind under a
// single variable each, so the rendering test can assert on the exact
// vocabulary from Task 1 rather than on whichever states happen to show up
// in newTestModel's fixture.
func allCellsModel(t *testing.T) ui.Model {
	t.Helper()
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev", "prod"}},
		Vars: map[string]manifest.Var{
			"SHARED_VAR":    {Value: s("v")},
			"OVERRIDE_VAR":  {Value: s("a"), Profiles: map[string]string{"dev": "b"}},
			"STORED_SECRET": {Secret: true},
			"MISSING_VAR":   {},
			"OPTIONAL_VAR":  {Optional: true},
		},
	}
	stored := map[string]map[string]bool{
		"STORED_SECRET": {"dev": true, "prod": true},
	}
	mx := ui.NewMatrix(m, stored)
	ps := ui.Problems(mx, nil)
	mod := ui.New("demo", mx, ps)
	mod, _ = mod.Update(ui.KeyRune('2')) // Variables tab
	return mod
}

// The matrix is the audit surface: a user reads it to decide what is wrong
// with their project, so it must show every variable name, every profile as
// a column heading, and the exact Cell words -- not abbreviations or symbols.
func TestVariablesTabRendersMatrix(t *testing.T) {
	m := allCellsModel(t)
	v := m.View()

	for _, want := range []string{"SHARED_VAR", "OVERRIDE_VAR", "STORED_SECRET", "MISSING_VAR", "OPTIONAL_VAR"} {
		if !strings.Contains(v, want) {
			t.Errorf("matrix missing variable name %q:\n%s", want, v)
		}
	}
	for _, want := range []string{"dev", "prod"} {
		if !strings.Contains(v, want) {
			t.Errorf("matrix missing profile heading %q:\n%s", want, v)
		}
	}
	for _, want := range []string{"shared", "override", "stored", "MISSING", "optional"} {
		if !strings.Contains(v, want) {
			t.Errorf("matrix missing cell word %q:\n%s", want, v)
		}
	}
}

// lineFor returns the line of v naming the given variable, or fails the test:
// every assertion below needs to know which row a marker landed on, not just
// that the word "secret" appears somewhere in the view.
func lineFor(t *testing.T, v, name string) string {
	t.Helper()
	for _, line := range strings.Split(v, "\n") {
		if strings.Contains(line, name) {
			return line
		}
	}
	t.Fatalf("no line for %q in:\n%s", name, v)
	return ""
}

// A secret row is marked with the word "secret" in its own column, matching
// the vocabulary already on screen (shared, stored, optional, MISSING) --
// not a glyph. A glyph's width is a font's opinion, and text/tabwriter sizes
// columns by rune count, not display width, so a symbol here can throw off
// the very alignment tabwriter exists to guarantee. Plain variables must not
// carry the word at all.
func TestVariablesTabMarksSecretRowsWithWord(t *testing.T) {
	m := allCellsModel(t)
	v := m.View()

	if line := lineFor(t, v, "STORED_SECRET"); !strings.Contains(line, "secret") {
		t.Errorf("secret row not marked with the word %q:\n%s", "secret", line)
	}
	if line := lineFor(t, v, "SHARED_VAR"); strings.Contains(line, "secret") {
		t.Errorf("plain variable row wrongly marked secret:\n%s", line)
	}
}

// j/k and the arrow keys move the row cursor without ever touching the
// column, and both edges clamp rather than wrap: a variable list can be
// long, and wrapping from the last row to the first would put a later
// delete key on the wrong variable.
func TestRowCursorMovesAndClamps(t *testing.T) {
	m := newTestModel(t) // 3 vars: OVERRIDE, SHARED, TOKEN (sorted)
	m, _ = m.Update(ui.KeyRune('2'))
	if m.Row() != 0 || m.Col() != 0 {
		t.Fatalf("starts at (%d,%d), want (0,0)", m.Row(), m.Col())
	}

	// Top edge clamps: k on the first row must not go negative.
	m, _ = m.Update(ui.KeyRune('k'))
	if m.Row() != 0 {
		t.Errorf("k at top row = %d, want clamped to 0", m.Row())
	}
	m, _ = m.Update(ui.KeyType(tea.KeyUp))
	if m.Row() != 0 {
		t.Errorf("up at top row = %d, want clamped to 0", m.Row())
	}

	m, _ = m.Update(ui.KeyRune('j'))
	if m.Row() != 1 || m.Col() != 0 {
		t.Errorf("after j = (%d,%d), want (1,0)", m.Row(), m.Col())
	}
	m, _ = m.Update(ui.KeyType(tea.KeyDown))
	if m.Row() != 2 || m.Col() != 0 {
		t.Errorf("after down = (%d,%d), want (2,0)", m.Row(), m.Col())
	}

	// Bottom edge clamps: there are 3 rows (indices 0-2); one more down must
	// not run off the end.
	m, _ = m.Update(ui.KeyRune('j'))
	if m.Row() != 2 {
		t.Errorf("j past last row = %d, want clamped to 2", m.Row())
	}
	m, _ = m.Update(ui.KeyType(tea.KeyDown))
	if m.Row() != 2 {
		t.Errorf("down past last row = %d, want clamped to 2", m.Row())
	}
}

// h/l and the arrow keys move the column cursor without touching the row,
// clamping at both edges of the 2-profile fixture (dev, prod).
func TestColumnCursorMovesAndClamps(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m, _ = m.Update(ui.KeyRune('j')) // move off row 0 so a column move can't be mistaken for a row move

	// Left edge clamps.
	m, _ = m.Update(ui.KeyRune('h'))
	if m.Col() != 0 {
		t.Errorf("h at first column = %d, want clamped to 0", m.Col())
	}
	m, _ = m.Update(ui.KeyType(tea.KeyLeft))
	if m.Col() != 0 {
		t.Errorf("left at first column = %d, want clamped to 0", m.Col())
	}

	m, _ = m.Update(ui.KeyRune('l'))
	if m.Col() != 1 || m.Row() != 1 {
		t.Errorf("after l = (%d,%d), want (1,1)", m.Row(), m.Col())
	}

	// Right edge clamps: 2 profiles (indices 0-1); one more right must not
	// run off the end.
	m, _ = m.Update(ui.KeyRune('l'))
	if m.Col() != 1 {
		t.Errorf("l past last column = %d, want clamped to 1", m.Col())
	}
	m, _ = m.Update(ui.KeyType(tea.KeyRight))
	if m.Col() != 1 {
		t.Errorf("right past last column = %d, want clamped to 1", m.Col())
	}
	if m.Row() != 1 {
		t.Errorf("column movement changed the row to %d, want 1", m.Row())
	}
}
