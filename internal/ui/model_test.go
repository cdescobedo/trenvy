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

// editCell drives e, clears the prefilled buffer, types newValue and enters
// it, returning the model with the edit staged. Factored out because every
// staging test needs the same sequence and the sequence itself is not what
// each test is asserting on.
func editCell(m ui.Model, newValue string) ui.Model {
	m, _ = m.Update(ui.KeyRune('e'))
	for range m.Buffer() {
		m, _ = m.Update(ui.KeyType(tea.KeyBackspace))
	}
	for _, r := range newValue {
		m, _ = m.Update(ui.KeyRune(r))
	}
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	return m
}

// newTestModel's fixture sorts to OVERRIDE, SHARED, TOKEN; row 0 / col 0 is
// OVERRIDE under dev, which already carries a profile override ("b"), so
// editing it exercises the override path without any cursor movement.

func TestEditEntersModeAndPrefillsCurrentValue(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2')) // Variables tab

	m, _ = m.Update(ui.KeyRune('e'))
	if m.Mode() != ui.ModeEditValue {
		t.Fatalf("mode after e = %v, want ModeEditValue", m.Mode())
	}
	if m.Buffer() != "b" {
		t.Errorf("buffer = %q, want prefilled with the current override %q", m.Buffer(), "b")
	}
}

// The Kind and Profile on the staged Change are the load-bearing fields: a
// set-value that does not name its profile is silent data corruption once
// Task 9 writes it back, so this pins the exact shape rather than merely
// that a change exists.
func TestEditCommitsSetValueForOneProfileOnly(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m = editCell(m, "NEWVAL")

	if m.Mode() != ui.ModeBrowse {
		t.Errorf("mode after enter = %v, want ModeBrowse", m.Mode())
	}
	staged := m.Staged()
	if len(staged) != 1 {
		t.Fatalf("Staged() = %+v, want exactly one change", staged)
	}
	want := ui.Change{Kind: ui.ChangeSetValue, Name: "OVERRIDE", Profile: "dev", Value: "NEWVAL"}
	if staged[0] != want {
		t.Errorf("Staged()[0] = %+v, want %+v", staged[0], want)
	}
}

// A staged edit must change what the matrix shows immediately: the view
// must contain the new value AND must no longer show the pre-edit
// classification for that cell. Asserting only the first half would still
// pass if the view showed both the new and the old.
func TestStagedEditReflectsImmediatelyInView(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))

	before := lineFor(t, m.View(), "OVERRIDE")
	if !strings.Contains(before, "override") {
		t.Fatalf("sanity check: expected pre-edit classification %q:\n%s", "override", before)
	}

	m = editCell(m, "NEWVAL")

	after := lineFor(t, m.View(), "OVERRIDE")
	if !strings.Contains(after, "NEWVAL") {
		t.Errorf("view does not show the staged value:\n%s", after)
	}
	if strings.Contains(after, "override") {
		t.Errorf("view still shows the pre-edit classification after staging:\n%s", after)
	}
}

// Esc must abandon the edit without staging anything, matching internal/tui.
func TestEscCancelsInputModeWithoutStaging(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m, _ = m.Update(ui.KeyRune('e'))
	m, _ = m.Update(ui.KeyRune('X'))
	m, _ = m.Update(ui.KeyType(tea.KeyEsc))

	if m.Mode() != ui.ModeBrowse {
		t.Errorf("mode after esc = %v, want ModeBrowse", m.Mode())
	}
	if len(m.Staged()) != 0 {
		t.Errorf("Staged() = %+v, want none after esc", m.Staged())
	}
}

func TestAddStagesAddVarAndAppearsInView(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m, _ = m.Update(ui.KeyRune('a'))
	if m.Mode() != ui.ModeAddName {
		t.Fatalf("mode after a = %v, want ModeAddName", m.Mode())
	}
	for _, r := range "NEWVAR" {
		m, _ = m.Update(ui.KeyRune(r))
	}
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))

	staged := m.Staged()
	want := ui.Change{Kind: ui.ChangeAddVar, Name: "NEWVAR"}
	if len(staged) != 1 || staged[0] != want {
		t.Fatalf("Staged() = %+v, want exactly %+v", staged, want)
	}
	if !strings.Contains(m.View(), "NEWVAR") {
		t.Errorf("added variable does not appear in the view:\n%s", m.View())
	}
}

// A deleted row must stay visible and marked, not vanish: removing it would
// silently shift every row below it under a cursor the user did not move,
// exactly what Task 5's clamping exists to prevent.
func TestDeleteMarksRowWithoutRemovingIt(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))

	m, _ = m.Update(ui.KeyRune('d'))

	staged := m.Staged()
	want := ui.Change{Kind: ui.ChangeDeleteVar, Name: "OVERRIDE"}
	if len(staged) != 1 || staged[0] != want {
		t.Fatalf("Staged() = %+v, want exactly %+v", staged, want)
	}

	v := m.View()
	if !strings.Contains(v, "OVERRIDE") {
		t.Fatalf("deleted row vanished from the view instead of being marked:\n%s", v)
	}
	line := lineFor(t, v, "OVERRIDE")
	if !strings.Contains(line, "deleted") {
		t.Errorf("deleted row not marked in the view:\n%s", line)
	}
	if m.Row() != 0 {
		t.Errorf("row cursor moved to %d after delete, want it to stay at 0", m.Row())
	}
}

// d is a toggle: pressing it again on the same row must un-stage the delete,
// since the user has not saved anything yet and can still change their mind.
func TestDeleteTogglesOff(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m, _ = m.Update(ui.KeyRune('d'))
	m, _ = m.Update(ui.KeyRune('d'))

	if len(m.Staged()) != 0 {
		t.Errorf("Staged() = %+v, want empty after toggling delete off", m.Staged())
	}
	if strings.Contains(lineFor(t, m.View(), "OVERRIDE"), "deleted") {
		t.Errorf("row still marked deleted after toggling off:\n%s", m.View())
	}
}

// The load-bearing filter assertion: after narrowing, the cursor must land
// on a row that is still visible, not merely have the row count shrink under
// it. Positioning on the last row first means a clamp that only resets to 0
// unconditionally cannot be mistaken for one that actually keeps the cursor
// in range.
func TestFilterNarrowsRowsAndKeepsCursorOnAVisibleRow(t *testing.T) {
	m := newTestModel(t) // OVERRIDE, SHARED, TOKEN
	m, _ = m.Update(ui.KeyRune('2'))
	m, _ = m.Update(ui.KeyRune('j'))
	m, _ = m.Update(ui.KeyRune('j'))
	if m.Row() != 2 {
		t.Fatalf("setup: row = %d, want 2 (TOKEN)", m.Row())
	}

	m, _ = m.Update(ui.KeyRune('/'))
	if m.Mode() != ui.ModeFilter {
		t.Fatalf("mode after / = %v, want ModeFilter", m.Mode())
	}
	for _, r := range "SHA" {
		m, _ = m.Update(ui.KeyRune(r))
	}
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))

	if m.Mode() != ui.ModeBrowse {
		t.Errorf("mode after filter enter = %v, want ModeBrowse", m.Mode())
	}

	v := m.View()
	if strings.Contains(v, "OVERRIDE") || strings.Contains(v, "TOKEN") {
		t.Errorf("filter did not narrow rows:\n%s", v)
	}
	if !strings.Contains(v, "SHARED") {
		t.Errorf("filter hid the matching row:\n%s", v)
	}

	if m.Row() != 0 {
		t.Fatalf("row cursor = %d after filtering to one row, want 0 (the only visible row)", m.Row())
	}
	line := lineFor(t, v, "SHARED")
	if !strings.HasPrefix(line, ">") {
		t.Errorf("cursor marker did not move onto the visible SHARED row:\n%q", line)
	}
}

// A clamp that merely resets the cursor to 0 on every filter would also pass
// TestFilterNarrowsRowsAndKeepsCursorOnAVisibleRow, since that test's filter
// happens to leave exactly one match. This test filters to two matches
// (OVERRIDE, SHARED -- both contain "R"; TOKEN does not) with the cursor
// starting past the end of that narrowed list, and pins the clamp to the
// last valid index rather than 0, to catch that shortcut.
func TestFilterClampsToLastValidIndexNotAlwaysZero(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m, _ = m.Update(ui.KeyRune('j'))
	m, _ = m.Update(ui.KeyRune('j'))
	if m.Row() != 2 {
		t.Fatalf("setup: row = %d, want 2 (TOKEN)", m.Row())
	}

	m, _ = m.Update(ui.KeyRune('/'))
	for _, r := range "R" {
		m, _ = m.Update(ui.KeyRune(r))
	}
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))

	v := m.View()
	if strings.Contains(v, "TOKEN") {
		t.Fatalf("filter did not hide TOKEN:\n%s", v)
	}
	if !strings.Contains(v, "OVERRIDE") || !strings.Contains(v, "SHARED") {
		t.Fatalf("filter hid a matching row:\n%s", v)
	}

	if m.Row() != 1 {
		t.Fatalf("row cursor = %d after filtering to 2 rows, want 1 (the last valid index)", m.Row())
	}
	line := lineFor(t, v, "SHARED")
	if !strings.HasPrefix(line, ">") {
		t.Errorf("cursor marker not on SHARED, the row the clamp should land on:\n%q", line)
	}
}

// secretsModel builds a model with two already-stored secrets, so a
// RevealedMsg's effect on one can be checked against the other: revealing
// TOKEN must never cause OTHER_SECRET, or TOKEN's other profile, to show a
// value too.
func secretsModel(t *testing.T) ui.Model {
	t.Helper()
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev", "prod"}},
		Vars: map[string]manifest.Var{
			"TOKEN":        {Secret: true},
			"OTHER_SECRET": {Secret: true},
		},
	}
	stored := map[string]map[string]bool{
		"TOKEN":        {"dev": true, "prod": true},
		"OTHER_SECRET": {"dev": true, "prod": true},
	}
	mx := ui.NewMatrix(m, stored)
	ps := ui.Problems(mx, nil)
	mod := ui.New("demo", mx, ps)
	mod, _ = mod.Update(ui.KeyRune('2')) // Variables tab
	return mod
}

// The load-bearing assertion for this task: a secret typed into the matrix
// must never be echoed to the screen. Checking for a mask character present
// would pass even if the plaintext sat right next to it, so this asserts the
// literal typed string is entirely absent from View() instead.
func TestEditOnSecretCellMasksTypingAndStagesSetSecret(t *testing.T) {
	m := newTestModel(t) // sorted: OVERRIDE(0), SHARED(1), TOKEN(2)
	m, _ = m.Update(ui.KeyRune('2'))
	m, _ = m.Update(ui.KeyRune('j'))
	m, _ = m.Update(ui.KeyRune('j'))
	if m.Row() != 2 {
		t.Fatalf("setup: row = %d, want 2 (TOKEN)", m.Row())
	}

	m, _ = m.Update(ui.KeyRune('e'))
	if m.Mode() != ui.ModeEditSecret {
		t.Fatalf("mode after e on a secret cell = %v, want ModeEditSecret", m.Mode())
	}

	const typed = "s3cret-value"
	for _, r := range typed {
		m, _ = m.Update(ui.KeyRune(r))
	}
	if v := m.View(); strings.Contains(v, typed) {
		t.Fatalf("secret value echoed to the screen while typing:\n%s", v)
	}

	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	if m.Mode() != ui.ModeBrowse {
		t.Errorf("mode after enter = %v, want ModeBrowse", m.Mode())
	}

	staged := m.Staged()
	want := ui.Change{Kind: ui.ChangeSetSecret, Name: "TOKEN", Profile: "dev", Value: typed}
	if len(staged) != 1 || staged[0] != want {
		t.Fatalf("Staged() = %+v, want exactly %+v", staged, want)
	}

	if v := m.View(); strings.Contains(v, typed) {
		t.Errorf("staged secret value leaked into the grid before it was ever revealed:\n%s", v)
	}
}

// Esc must abandon a secret edit without staging anything, matching e on a
// plain cell and internal/tui.
func TestEscCancelsSecretEditWithoutStaging(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m, _ = m.Update(ui.KeyRune('j'))
	m, _ = m.Update(ui.KeyRune('j')) // TOKEN row
	m, _ = m.Update(ui.KeyRune('e'))
	m, _ = m.Update(ui.KeyRune('X'))
	m, _ = m.Update(ui.KeyType(tea.KeyEsc))

	if m.Mode() != ui.ModeBrowse {
		t.Errorf("mode after esc = %v, want ModeBrowse", m.Mode())
	}
	if len(m.Staged()) != 0 {
		t.Errorf("Staged() = %+v, want none after esc", m.Staged())
	}
}

// r must ask the host to fetch, not fetch anything itself: Update stays pure,
// and a stray reveal must never risk a keyring prompt on its own. The command
// it returns is checked for the exact name and profile under the cursor, and
// the model itself must come back unchanged -- fetching, and folding the
// result back in, is entirely the host's job (via RevealedMsg).
func TestRevealKeyEmitsRequestWithoutFetching(t *testing.T) {
	m := secretsModel(t) // rows: OTHER_SECRET(0), TOKEN(1); cols: dev(0), prod(1)
	m, _ = m.Update(ui.KeyRune('j'))
	m, _ = m.Update(ui.KeyRune('l'))
	if m.Row() != 1 || m.Col() != 1 {
		t.Fatalf("setup: (row,col) = (%d,%d), want (1,1) TOKEN/prod", m.Row(), m.Col())
	}

	before := m.View()
	next, cmd := m.Update(ui.KeyRune('r'))
	if cmd == nil {
		t.Fatalf("r produced no command, want a RevealRequestMsg command")
	}
	if next.View() != before {
		t.Errorf("r changed the view by itself; reveal must wait for RevealedMsg:\n%s", next.View())
	}

	got, ok := cmd().(ui.RevealRequestMsg)
	want := ui.RevealRequestMsg{Name: "TOKEN", Profile: "prod"}
	if !ok || got != want {
		t.Errorf("cmd() = %#v, want %#v", got, want)
	}
}

// A RevealedMsg must only ever light up the one cell it names: not the whole
// row (TOKEN's other profile), and not a different secret entirely.
func TestRevealedMsgShowsValueOnlyForThatCell(t *testing.T) {
	m := secretsModel(t)

	m, _ = m.Update(ui.RevealedMsg{Name: "TOKEN", Profile: "dev", Value: "s3cret-dev"})

	v := m.View()
	tokenLine := lineFor(t, v, "TOKEN")
	if !strings.Contains(tokenLine, "s3cret-dev") {
		t.Errorf("revealed cell does not show its value:\n%s", tokenLine)
	}
	if !strings.Contains(tokenLine, "stored") {
		t.Errorf("TOKEN/prod must stay masked after revealing only TOKEN/dev:\n%s", tokenLine)
	}

	otherLine := lineFor(t, v, "OTHER_SECRET")
	if strings.Contains(otherLine, "s3cret-dev") {
		t.Errorf("a different secret must never show a value revealed for TOKEN:\n%s", otherLine)
	}
	if !strings.Contains(otherLine, "stored") {
		t.Errorf("OTHER_SECRET must stay masked after revealing an unrelated secret:\n%s", otherLine)
	}
}

// A filter matching nothing must not panic View, and must leave every row
// hidden rather than falling back to showing them all.
func TestFilterWithNoMatches(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m, _ = m.Update(ui.KeyRune('/'))
	for _, r := range "ZZZZ" {
		m, _ = m.Update(ui.KeyRune(r))
	}
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))

	v := m.View()
	for _, name := range []string{"OVERRIDE", "SHARED", "TOKEN"} {
		if strings.Contains(v, name) {
			t.Errorf("expected no rows to match filter, but found %q:\n%s", name, v)
		}
	}
}
