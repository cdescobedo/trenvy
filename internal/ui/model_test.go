package ui_test

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/ui"
)

// testParams fills in header facts that most tests never look at, so each
// test only has to supply the matrix and problems that actually vary. A test
// asserting on the header itself (see overview_test.go) builds a ui.Params
// directly instead of going through this helper.
func testParams(mx *ui.Matrix, ps []ui.Problem) ui.Params {
	return ui.Params{Project: "demo", Profile: "dev", Source: "default", Backend: "keychain", Matrix: mx, Problems: ps}
}

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
	return ui.New(testParams(mx, ps))
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

// Profiles is still inert (2b's job) and must say so, naming the CLI that
// does the job today rather than showing an empty panel. The Outputs tab is
// no longer fully inert -- Task 5 wires a/r/d/D -- but an empty project has
// nothing for those keys to act on yet, so its own empty state still names
// the CLI as a fallback (see output_test.go's own coverage of the key).
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
	mod := ui.New(testParams(mx, ps))
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
	if !reflect.DeepEqual(staged[0], want) {
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
	if len(staged) != 1 || !reflect.DeepEqual(staged[0], want) {
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
	if len(staged) != 1 || !reflect.DeepEqual(staged[0], want) {
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

// d then e must not resurrect the row with an untouched profile silently
// wiped (the Fix 1 bug): pressing e on a row staged for deletion cancels the
// delete before opening the edit, so the row is never both "staged for
// deletion" and "staged for an edit" for commitStaged to reconcile badly.
// The cancellation is visible immediately -- the "(deleted)" marker
// disappears -- so nothing about it is silent the way the discarded delete
// used to be.
func TestEditCancelsAPendingDelete(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m, _ = m.Update(ui.KeyRune('d')) // stage delete for OVERRIDE (row 0)

	m = editCell(m, "NEWVAL") // e must cancel the delete, then stage the edit

	staged := m.Staged()
	want := ui.Change{Kind: ui.ChangeSetValue, Name: "OVERRIDE", Profile: "dev", Value: "NEWVAL"}
	if len(staged) != 1 || !reflect.DeepEqual(staged[0], want) {
		t.Fatalf("Staged() = %+v, want exactly %+v (the delete must be cancelled, not merely outlived)", staged, want)
	}
	if strings.Contains(lineFor(t, m.View(), "OVERRIDE"), "deleted") {
		t.Errorf("row still marked deleted after editing it:\n%s", m.View())
	}
}

// e then d is the reverse order: the row already has a staged edit when d is
// pressed. d must refuse rather than delete out from under it -- deleting
// would run commitStaged's read-modify-write on a variable the same batch
// deletes, the exact zero-value-reconstruction mechanism that wiped an
// untouched profile in the Fix 1 bug. Refusing keeps the two orders
// consistent: whichever of d/e is pressed second, the row ends up edited,
// never deleted-with-a-lost-edit or resurrected-with-a-gap.
func TestDeleteRefusesOverAStagedEdit(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m = editCell(m, "NEWVAL")        // stage an edit for OVERRIDE (row 0)
	m, _ = m.Update(ui.KeyRune('d')) // must not delete out from under it

	staged := m.Staged()
	want := ui.Change{Kind: ui.ChangeSetValue, Name: "OVERRIDE", Profile: "dev", Value: "NEWVAL"}
	if len(staged) != 1 || !reflect.DeepEqual(staged[0], want) {
		t.Fatalf("Staged() = %+v, want exactly %+v (d must refuse, not add a delete over a staged edit)", staged, want)
	}
	if strings.Contains(lineFor(t, m.View(), "OVERRIDE"), "deleted") {
		t.Errorf("row marked deleted despite d being refused:\n%s", m.View())
	}
}

// Fix wave round 2, concern 2: a refusal that says nothing is the same class
// of defect this project has already paid for twice -- e on a secret cell
// was a silent no-op until Ruling 3, and s was promised everywhere and
// implemented nowhere until Task 13 pressed it. d's refusal over a staged
// edit must say why, and what to do about it, in the same place the UI
// already talks to the user -- derived from state Update already keeps
// (mirroring internal/tui's addNameWarning), not a new mutable "message"
// field, and shown whether or not d was actually pressed: it is true the
// moment the row has both a staged edit and the cursor on it.
func TestDeleteRefusalExplainsWhy(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m = editCell(m, "NEWVAL") // stage an edit for OVERRIDE (row 0)

	v := m.View()
	if !strings.Contains(v, "d is disabled") {
		t.Errorf("no explanation for d's refusal:\n%s", v)
	}
	if !strings.Contains(v, "OVERRIDE") {
		t.Errorf("explanation does not name the row:\n%s", v)
	}
	if !strings.Contains(v, "q discards everything staged") {
		t.Errorf("explanation does not say what to do about it:\n%s", v)
	}

	// Pressing d must not change the message or the state: it is still
	// refused, and the explanation is still there, not a one-shot flash
	// that disappears once "seen".
	m, _ = m.Update(ui.KeyRune('d'))
	if !strings.Contains(m.View(), "d is disabled") {
		t.Errorf("explanation vanished after pressing the refused key:\n%s", m.View())
	}
}

// A row with no staged edit must show no explanation at all -- the message
// is specific to the one condition that blocks d, not a permanent fixture
// of the Variables tab.
func TestNoDeleteExplanationWithoutAStagedEdit(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))

	if v := m.View(); strings.Contains(v, "d is disabled") {
		t.Errorf("explanation shown despite no staged edit:\n%s", v)
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

// Fix 6: a committed filter has no other on-screen trace once rows are
// narrowed -- rows just vanish, with nothing saying why. This pins that the
// active filter stays visible once it has actually hidden something, not
// only in the "no rows match" special case (see viewVariables).
func TestActiveFilterStaysVisibleOnceRowsAreNarrowed(t *testing.T) {
	m := newTestModel(t) // OVERRIDE, SHARED, TOKEN
	m, _ = m.Update(ui.KeyRune('2'))
	m, _ = m.Update(ui.KeyRune('/'))
	for _, r := range "SHA" {
		m, _ = m.Update(ui.KeyRune(r))
	}
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))

	v := m.View()
	if !strings.Contains(v, `filter: "SHA"`) {
		t.Errorf("active filter is not shown once rows are narrowed:\n%s", v)
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
	mod := ui.New(testParams(mx, ps))
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
	if len(staged) != 1 || !reflect.DeepEqual(staged[0], want) {
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

// Fix 6: r over a cell with a staged set-secret must not even ask to fetch.
// cellText already shows "secret (pending)" for this cell regardless of
// what the keyring holds, so fetching the current value would cost a real
// Get -- on macOS, a real authorization prompt -- for an answer that is
// never displayed and immediately discarded.
func TestRevealRefusesOverAStagedSetSecret(t *testing.T) {
	m := secretsModel(t)             // rows: OTHER_SECRET(0), TOKEN(1); cols: dev(0), prod(1)
	m, _ = m.Update(ui.KeyRune('j')) // TOKEN row, dev column
	m, _ = m.Update(ui.KeyRune('e'))
	for _, r := range "newvalue" {
		m, _ = m.Update(ui.KeyRune(r))
	}
	m, _ = m.Update(ui.KeyType(tea.KeyEnter)) // stages set-secret for TOKEN/dev

	_, cmd := m.Update(ui.KeyRune('r'))
	if cmd != nil {
		t.Errorf("r over a staged set-secret produced a command, want none -- it must not fetch")
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

// sameStaged reports whether got and want hold exactly the same Changes in
// the same order -- used where a decline must leave Staged() untouched, not
// merely the same length. `s` stages several Changes per promotion (one
// set-secret per profile plus the promotion itself, see stagePromotion), so
// a decline that cleared only some of them would still pass a check that
// only compared counts while leaving a half-promotion behind.
func sameStaged(got, want []ui.Change) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			return false
		}
	}
	return true
}

// promoteModel builds a model with three declared profiles and one variable
// named PROMOTE_ME, shaped per test by whichever Var the caller supplies.
// Three profiles, not two: the shared-value case must show one set-secret
// per DECLARED profile, and two profiles would not distinguish "one per
// profile with a value" from "one per profile that exists at all".
func promoteModel(t *testing.T, v manifest.Var) ui.Model {
	t.Helper()
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev", "staging", "prod"}},
		Vars:    map[string]manifest.Var{"PROMOTE_ME": v},
	}
	mx := ui.NewMatrix(m, nil)
	ps := ui.Problems(mx, nil)
	mod := ui.New(testParams(mx, ps))
	mod, _ = mod.Update(ui.KeyRune('2')) // Variables tab
	return mod
}

// s on a variable overridden in two of three profiles must stage a
// set-secret carrying each override's actual value, and nothing for the
// third profile, which has no value to move. The exact Value on each
// set-secret is the load-bearing assertion here (see Task 1's brief): a
// promotion that stages the right Kind and Profile but an empty Value would
// still pass a looser test while silently destroying the value at commit,
// since manifest.Validate forbids a secret carrying one.
func TestPromoteStagesSetSecretForOverriddenProfilesOnly(t *testing.T) {
	m := promoteModel(t, manifest.Var{Profiles: map[string]string{"dev": "d-val", "staging": "s-val"}})
	m, _ = m.Update(ui.KeyRune('s'))

	// Per-profile overrides only, no shared Value: nothing here is
	// surprising, so s must stage immediately rather than opening the
	// confirmation the shared-value case needs (see
	// TestPromoteSharedValueOpensConfirmationBeforeStaging).
	if m.Mode() != ui.ModeBrowse {
		t.Fatalf("mode after s on an override-only variable = %v, want ModeBrowse: nothing to confirm here", m.Mode())
	}

	staged := m.Staged()
	want := []ui.Change{
		{Kind: ui.ChangeSetSecret, Name: "PROMOTE_ME", Profile: "dev", Value: "d-val"},
		{Kind: ui.ChangeSetSecret, Name: "PROMOTE_ME", Profile: "staging", Value: "s-val"},
		{Kind: ui.ChangePromoteSecret, Name: "PROMOTE_ME"},
	}
	if len(staged) != len(want) {
		t.Fatalf("Staged() = %+v, want %+v", staged, want)
	}
	for i := range want {
		if !reflect.DeepEqual(staged[i], want[i]) {
			t.Errorf("Staged()[%d] = %+v, want %+v", i, staged[i], want[i])
		}
	}
}

// A variable with only a shared Value has no per-profile override anywhere,
// so s must stage one set-secret per DECLARED profile (mx.Profiles), all
// carrying that same shared value -- not one shared keyring item, which
// cannot exist, and not one per profile that happens to already show a
// cell. This is the honest translation the spec calls for, not a bug: a
// shared value promoted to a secret becomes N identical keyring items that
// can diverge later if only one is changed.
func TestPromoteSharedValueStagesOnePerDeclaredProfile(t *testing.T) {
	m := promoteModel(t, manifest.Var{Value: s("shared-val")})
	m, _ = m.Update(ui.KeyRune('s'))
	m, _ = m.Update(ui.KeyRune('y')) // confirm the shared-value promotion

	staged := m.Staged()
	gotValues := map[string]string{}
	for _, c := range staged {
		if c.Kind == ui.ChangeSetSecret {
			gotValues[c.Profile] = c.Value
		}
	}
	wantProfiles := []string{"dev", "staging", "prod"}
	if len(gotValues) != len(wantProfiles) {
		t.Fatalf("staged set-secret profiles = %+v, want one per declared profile %+v", gotValues, wantProfiles)
	}
	for _, profile := range wantProfiles {
		if gotValues[profile] != "shared-val" {
			t.Errorf("set-secret for %s carries %q, want the shared value %q", profile, gotValues[profile], "shared-val")
		}
	}
	foundPromote := false
	for _, c := range staged {
		if c.Kind == ui.ChangePromoteSecret && c.Name == "PROMOTE_ME" {
			foundPromote = true
		}
	}
	if !foundPromote {
		t.Errorf("Staged() = %+v, missing a promote-secret change for the row", staged)
	}
}

// s on a variable whose value is shared across profiles must not stage
// anything until the promotion is confirmed: staging first and asking after
// would let the keyring writes and the manifest flip happen before the user
// ever reads what s is about to do.
func TestPromoteSharedValueOpensConfirmationBeforeStaging(t *testing.T) {
	m := promoteModel(t, manifest.Var{Value: s("shared-val")})
	m, cmd := m.Update(ui.KeyRune('s'))

	if m.Mode() != ui.ModeConfirmPromote {
		t.Fatalf("mode after s on a shared value = %v, want ModeConfirmPromote", m.Mode())
	}
	if len(m.Staged()) != 0 {
		t.Fatalf("Staged() = %+v after s alone, want none until the confirmation is answered", m.Staged())
	}
	if cmd != nil {
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Error("opening the promote confirmation must not quit")
		}
	}
}

// The confirmation must name what promoting does, not just ask whether the
// user is sure: a shared value becomes one keyring item per declared
// profile, and editing one afterward will not touch the others. A prompt
// that only asked "are you sure?" would teach people to press y without
// reading it.
func TestPromoteConfirmationNamesTheConsequence(t *testing.T) {
	m := promoteModel(t, manifest.Var{Value: s("shared-val")})
	m, _ = m.Update(ui.KeyRune('s'))

	v := m.View()
	if !strings.Contains(v, "PROMOTE_ME") {
		t.Errorf("confirmation does not name the variable:\n%s", v)
	}
	if !strings.Contains(v, "3 separate keyring items") {
		t.Errorf("confirmation does not say how many keyring items result:\n%s", v)
	}
	if !strings.Contains(v, "one per declared profile") {
		t.Errorf("confirmation does not say one item per declared profile:\n%s", v)
	}
	if !strings.Contains(v, "changing one afterward will not change the others") {
		t.Errorf("confirmation does not warn that the items can diverge later:\n%s", v)
	}
}

// The load-bearing assertion for decline: Staged() must come back exactly
// what it was before s was pressed, not merely the same length. s stages
// several Changes per promotion (one set-secret per profile plus the
// promotion itself), so a decline that cleared only some of them would leave
// a half-promotion -- values stored as secrets while the manifest still
// calls the variable plain, or the reverse -- and a length-only check would
// not catch it.
func TestPromoteSharedValueDeclineStagesNothing(t *testing.T) {
	mf := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev", "staging", "prod"}},
		Vars: map[string]manifest.Var{
			"OTHER":      {Profiles: map[string]string{"dev": "x", "staging": "x", "prod": "x"}},
			"PROMOTE_ME": {Value: s("shared-val")},
		},
	}
	mx := ui.NewMatrix(mf, nil)
	ps := ui.Problems(mx, nil)
	m := ui.New(testParams(mx, ps))
	m, _ = m.Update(ui.KeyRune('2')) // Variables tab

	// Stage an unrelated edit first, so declining the promotion must leave
	// this untouched rather than merely leaving Staged() empty.
	m = editCell(m, "changed") // OTHER, row 0, col 0 (alphabetically before PROMOTE_ME)
	before := m.Staged()

	m, _ = m.Update(ui.KeyRune('j')) // move to PROMOTE_ME
	m, _ = m.Update(ui.KeyRune('s'))
	if m.Mode() != ui.ModeConfirmPromote {
		t.Fatalf("mode after s on a shared value = %v, want ModeConfirmPromote (test setup)", m.Mode())
	}

	// Every key but y answers no -- exercise one that is not esc or n, to
	// prove the rule is "not y", mirroring TestQConfirmationDecliningLeavesStagedAndCursorUntouched.
	m, cmd := m.Update(ui.KeyRune('x'))

	if m.Mode() != ui.ModeBrowse {
		t.Errorf("mode after declining = %v, want ModeBrowse", m.Mode())
	}
	if cmd != nil {
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Error("declining a promotion must not end the session")
		}
	}
	if got := m.Staged(); !sameStaged(got, before) {
		t.Errorf("Staged() = %+v after declining, want untouched %+v", got, before)
	}
}

// A variable with both a per-profile override and a shared Value must have
// the override win for that profile, per resolve.Value's precedence, which
// Classify already mirrors. Reading Value before Profiles here would make
// the promotion disagree with what the matrix cell claims -- the cell says
// "override" while the keyring would silently get the shared value instead.
func TestPromoteOverrideWinsOverSharedValue(t *testing.T) {
	m := promoteModel(t, manifest.Var{Value: s("shared-val"), Profiles: map[string]string{"dev": "override-val"}})
	m, _ = m.Update(ui.KeyRune('s'))
	m, _ = m.Update(ui.KeyRune('y')) // confirm: staging/prod still fall back to the shared value

	got := map[string]string{}
	for _, c := range m.Staged() {
		if c.Kind == ui.ChangeSetSecret {
			got[c.Profile] = c.Value
		}
	}
	want := map[string]string{"dev": "override-val", "staging": "shared-val", "prod": "shared-val"}
	if len(got) != len(want) {
		t.Fatalf("staged set-secret profiles = %+v, want %+v", got, want)
	}
	for profile, value := range want {
		if got[profile] != value {
			t.Errorf("profile %s staged value = %q, want %q", profile, got[profile], value)
		}
	}
}

// s on a variable already secret must stage nothing -- there is no key that
// demotes a secret back to plain -- and must say why in the view, naming the
// row. A silent no-op here is the wart this project has removed three times
// already (e on a secret cell before Ruling 3, s itself promised everywhere
// and unimplemented until phase 1's Task 13, d over a staged edit in the
// phase-1 fix wave); this is not a fourth.
func TestPromoteOnAlreadySecretStagesNothingAndExplainsWhy(t *testing.T) {
	m := promoteModel(t, manifest.Var{Secret: true})
	m, _ = m.Update(ui.KeyRune('s'))

	if len(m.Staged()) != 0 {
		t.Fatalf("Staged() = %+v, want none: s must not touch an already-secret variable", m.Staged())
	}
	v := m.View()
	if !strings.Contains(v, "PROMOTE_ME") {
		t.Errorf("explanation does not name the row:\n%s", v)
	}
	if !strings.Contains(v, "already a secret") {
		t.Errorf("no explanation for s doing nothing on an already-secret row:\n%s", v)
	}
}

// The view must mark the row pending-secret immediately after s, so the
// user sees the promotion take effect before ever pressing w -- mirroring
// how a staged set-value already shows up instantly (see
// TestStagedEditReflectsImmediatelyInView).
func TestPromoteMarksRowPendingSecretInView(t *testing.T) {
	m := promoteModel(t, manifest.Var{Value: s("shared-val")})
	m, _ = m.Update(ui.KeyRune('s'))
	m, _ = m.Update(ui.KeyRune('y')) // confirm the shared-value promotion

	line := lineFor(t, m.View(), "PROMOTE_ME")
	if strings.Count(line, "secret (pending)") != 3 {
		t.Errorf("row after promotion = %q, want secret (pending) for every declared profile", line)
	}
}

// w is what Task 9's cli-side commit relies on to know whether to write
// anything at all: Staged() is populated the same way whether or not w was
// ever pressed, so Saved() has to be the one bit that tells the two apart.
func TestWMarksSavedAndQuits(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m = editCell(m, "NEWVAL")

	if m.Saved() {
		t.Fatalf("Saved() = true before w was ever pressed")
	}

	m, cmd := m.Update(ui.KeyRune('w'))
	if !m.Saved() {
		t.Errorf("Saved() = false after w, want true")
	}
	if cmd == nil {
		t.Fatal("w did not return a command to end the session")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("w's command = %T, want tea.Quit", cmd())
	}
	if len(m.Staged()) != 1 {
		t.Errorf("w must not clear Staged(); the cli commits from it after Run returns")
	}
}

// q ends the session without ever setting Saved(), which is the other half
// of the "quit without w writes nothing" promise: the cli only has Saved()
// to check, so a quit path that left it true would write staged changes the
// user never asked to keep. With staged changes present, q asks first
// (Task 12, see quit_test.go) -- this confirms the "yes" branch still leaves
// Saved() false.
func TestQQuitsWithoutSaving(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m = editCell(m, "NEWVAL")

	m, _ = m.Update(ui.KeyRune('q'))
	m, cmd := m.Update(ui.KeyRune('y'))
	if m.Saved() {
		t.Errorf("Saved() = true after q, want false")
	}
	if cmd == nil {
		t.Fatal("q did not return a command to end the session")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q's command = %T, want tea.Quit", cmd())
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

// Fix 4: the UI must document its own keys rather than relying entirely on
// the README. The hint line is persistent -- shown on every tab, regardless
// of staged count -- unlike statusLine, which only names w/q and only once
// something is staged. `s` promotes a variable to a secret (Task 1) and must
// appear here now, alongside the other keys that act on the row or cell
// under the cursor.
func TestKeyHintLineIsAlwaysVisible(t *testing.T) {
	m := newTestModel(t) // opens on Overview, nothing staged
	v := m.View()
	for _, want := range []string{"e edit", "a add", "d delete", "s promote", "filter", "? help"} {
		if !strings.Contains(v, want) {
			t.Errorf("hint line missing %q:\n%s", want, v)
		}
	}
}

// ? opens the full key list from any tab, mirroring p. It must name every
// key this package actually implements, including ones the short hint line
// leaves out (r, w, q, ctrl+c), and s's entry must say its promotion is
// one-way -- a bare "toggle secret" would be a lie in the direction that
// matters, since there is no key that turns a secret back into a plain
// variable.
func TestHelpKeyShowsTheFullKeyList(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('?'))
	if m.Mode() != ui.ModeHelp {
		t.Fatalf("mode after ? = %v, want ModeHelp", m.Mode())
	}
	v := m.View()
	for _, want := range []string{"e", "r", "a", "d", "D", "s", "/", "p", "w", "q", "ctrl+c", "?"} {
		if !strings.Contains(v, want) {
			t.Errorf("help panel missing key %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, "promote") {
		t.Errorf("help panel does not describe what s does:\n%s", v)
	}
	if !strings.Contains(v, "one-way") {
		t.Errorf("help panel does not say s's promotion is one-way:\n%s", v)
	}
}

// a, r and d mean different things depending on the tab; the full key list
// must say so rather than describing only their Variables-tab meaning, and
// must name D, which exists only on the Outputs tab.
func TestHelpKeyDescribesOutputsTabKeys(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('?'))
	v := m.View()
	for _, want := range []string{"rename", "default", "output"} {
		if !strings.Contains(v, want) {
			t.Errorf("help panel does not describe the Outputs tab's keys (missing %q):\n%s", want, v)
		}
	}
}

// esc backs out of the help panel, mirroring the switcher's own esc-cancels
// convention, without disturbing anything else about the model.
func TestEscClosesHelp(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('?'))
	m, _ = m.Update(ui.KeyType(tea.KeyEsc))
	if m.Mode() != ui.ModeBrowse {
		t.Errorf("mode after esc from help = %v, want ModeBrowse", m.Mode())
	}
}
