package ui_test

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/ui"
)

// fiveVarMatrix gives the editor something to show checkboxes for: five
// plain variables, none of them special, so every test below is exercising
// the editor's own machinery rather than any one variable's own quirks.
// Names are multi-letter and non-overlapping (no name is a substring of
// another) so lineFor can find a row unambiguously.
func fiveVarMatrix() *ui.Matrix {
	names := []string{"ALPHA", "BRAVO", "CHARLIE", "DELTA", "ECHO"}
	vars := make(map[string]manifest.Var, len(names))
	for _, n := range names {
		vars[n] = manifest.Var{Value: s("v")}
	}
	return &ui.Matrix{Names: names, Profiles: []string{"dev"}, Vars: vars}
}

// globsEditModel builds a model with a real matrix (so the editor has
// variables to list) and the given Outputs rows, switched straight to the
// Outputs tab -- mirroring outputsModelOnTab, but this feature also needs
// mx.Names, which outputsParams deliberately leaves nil for tests that don't
// care about it.
func globsEditModel(t *testing.T, mx *ui.Matrix, rows []ui.OutputRow) ui.Model {
	t.Helper()
	m := ui.New(ui.Params{
		Project: "demo", Profile: "dev", Source: "default", Backend: "keychain",
		Matrix: mx, Outputs: rows,
	})
	m, _ = m.Update(ui.KeyRune('4'))
	return m
}

// TestEnterOnOutputOpensGlobsEditor is the first assertion of the brief's
// Step 1: enter on an output row opens the editor, listing every variable
// the matrix knows about.
func TestEnterOnOutputOpensGlobsEditor(t *testing.T) {
	rows := []ui.OutputRow{{Name: "out", Format: "dotenv", Path: ".env"}}
	m := globsEditModel(t, fiveVarMatrix(), rows)

	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	if m.Mode() != ui.ModeOutputGlobs {
		t.Fatalf("mode after enter on an output row = %v, want ModeOutputGlobs", m.Mode())
	}
	v := m.View()
	for _, want := range []string{"ALPHA", "BRAVO", "CHARLIE", "DELTA", "ECHO"} {
		if !strings.Contains(v, want) {
			t.Errorf("editor view is missing variable %q:\n%s", want, v)
		}
	}
}

// TestGlobsEditorDerivesCheckedStateViaResolveSelects is Five Things #3: the
// boxes must reflect what resolve.Selects says is selected today, not a
// naive string match against Include. PUBLIC_* is a pattern, not a literal
// name, so a parser that just checked "is this name in Include" would get
// every one of these three cases wrong.
func TestGlobsEditorDerivesCheckedStateViaResolveSelects(t *testing.T) {
	names := []string{"PUBLIC_URL", "PUBLIC_KEY", "SECRET_TOKEN"}
	vars := make(map[string]manifest.Var, len(names))
	for _, n := range names {
		vars[n] = manifest.Var{Value: s("v")}
	}
	mx := &ui.Matrix{Names: names, Profiles: []string{"dev"}, Vars: vars}
	rows := []ui.OutputRow{{
		Name: "out", Format: "dotenv", Path: ".env",
		Include: []string{"PUBLIC_*"}, Exclude: []string{"PUBLIC_KEY"},
	}}
	m := globsEditModel(t, mx, rows)
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))

	v := m.View()
	if !strings.Contains(lineFor(t, v, "PUBLIC_URL"), "[x]") {
		t.Errorf("PUBLIC_URL matches PUBLIC_* and is not excluded -- should be checked:\n%s", v)
	}
	if !strings.Contains(lineFor(t, v, "PUBLIC_KEY"), "[ ]") {
		t.Errorf("PUBLIC_KEY matches PUBLIC_* but is excluded -- should be unchecked:\n%s", v)
	}
	if !strings.Contains(lineFor(t, v, "SECRET_TOKEN"), "[ ]") {
		t.Errorf("SECRET_TOKEN does not match PUBLIC_* -- should be unchecked:\n%s", v)
	}
}

// TestGlobsEditorSpaceTogglesCheckbox covers Step 1's "space toggles".
func TestGlobsEditorSpaceTogglesCheckbox(t *testing.T) {
	rows := []ui.OutputRow{{Name: "out", Format: "dotenv", Path: ".env"}}
	m := globsEditModel(t, fiveVarMatrix(), rows)
	m, _ = m.Update(ui.KeyType(tea.KeyEnter)) // cursor starts on ALPHA

	// An output with no Include/Exclude at all selects everything today, so
	// every box starts checked.
	if !strings.Contains(lineFor(t, m.View(), "ALPHA"), "[x]") {
		t.Fatalf("ALPHA should start checked (no globs means everything selected):\n%s", m.View())
	}
	m, _ = m.Update(ui.KeyType(tea.KeySpace))
	if !strings.Contains(lineFor(t, m.View(), "ALPHA"), "[ ]") {
		t.Errorf("space did not uncheck ALPHA:\n%s", m.View())
	}
	m, _ = m.Update(ui.KeyType(tea.KeySpace))
	if !strings.Contains(lineFor(t, m.View(), "ALPHA"), "[x]") {
		t.Errorf("space did not re-check ALPHA:\n%s", m.View())
	}
}

// TestGlobsEditorStatusLineNamesBothFutures pins the two sentences verbatim:
// Five Things #1 says the whole point of this editor is that a user can tell
// which future applies to a variable that does not exist yet, and it must
// change the instant `m` is pressed.
func TestGlobsEditorStatusLineNamesBothFutures(t *testing.T) {
	rows := []ui.OutputRow{{Name: "out", Format: "dotenv", Path: ".env"}}
	m := globsEditModel(t, fiveVarMatrix(), rows)
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))

	wantExcept := "everything except: a variable added later WILL appear in this file"
	if v := m.View(); !strings.Contains(v, wantExcept) {
		t.Errorf("editor view = %q\nwant it to contain %q", v, wantExcept)
	}

	m, _ = m.Update(ui.KeyRune('m'))
	wantOnly := "only these: a variable added later WILL NOT appear in this file"
	if v := m.View(); !strings.Contains(v, wantOnly) {
		t.Errorf("editor view after m = %q\nwant it to contain %q", v, wantOnly)
	}
}

// TestGlobsEditorSwitchingModeDoesNotMoveCheckboxes is Five Things #2, the
// property Task 7 already proved for WriteGlobs itself: this pins it at the
// Model level, where a bug could reintroduce it by re-deriving checked from
// the new mode's globs instead of leaving the map alone.
func TestGlobsEditorSwitchingModeDoesNotMoveCheckboxes(t *testing.T) {
	// A pattern that matches none of the five names starts every box
	// unchecked and the editor in ModeOnlyThese (Include is non-empty), so
	// checking exactly three is an unambiguous, deliberate action rather
	// than starting from "everything checked" and hoping unchecking the
	// other two reads the same.
	rows := []ui.OutputRow{{Name: "out", Format: "dotenv", Path: ".env", Include: []string{"NONE_OF_THESE"}}}
	m := globsEditModel(t, fiveVarMatrix(), rows)
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))

	for _, name := range []string{"ALPHA", "BRAVO", "CHARLIE", "DELTA", "ECHO"} {
		if !strings.Contains(lineFor(t, m.View(), name), "[ ]") {
			t.Fatalf("%s should start unchecked (Include matches nothing):\n%s", name, m.View())
		}
	}

	// Check ALPHA, CHARLIE and ECHO (three of five); leave BRAVO and DELTA
	// unchecked.
	check := func(mm ui.Model, presses int) ui.Model {
		for i := 0; i < presses; i++ {
			mm, _ = mm.Update(ui.KeyRune('j'))
		}
		mm, _ = mm.Update(ui.KeyType(tea.KeySpace))
		return mm
	}
	m = check(m, 0) // ALPHA
	m, _ = m.Update(ui.KeyRune('j'))
	m, _ = m.Update(ui.KeyRune('j'))
	m, _ = m.Update(ui.KeyType(tea.KeySpace)) // CHARLIE
	m, _ = m.Update(ui.KeyRune('j'))
	m, _ = m.Update(ui.KeyRune('j'))
	m, _ = m.Update(ui.KeyType(tea.KeySpace)) // ECHO

	assertChecked := func(v string) {
		t.Helper()
		want := map[string]bool{"ALPHA": true, "BRAVO": false, "CHARLIE": true, "DELTA": false, "ECHO": true}
		for name, checked := range want {
			line := lineFor(t, v, name)
			box := "[ ]"
			if checked {
				box = "[x]"
			}
			if !strings.Contains(line, box) {
				t.Errorf("%s: line = %q, want box %s", name, line, box)
			}
		}
	}
	assertChecked(m.View())

	m, _ = m.Update(ui.KeyRune('m')) // switch mode: must not move a single box
	assertChecked(m.View())
}

// TestEscFromGlobsEditorStagesNothing covers Step 1's "esc returns without
// staging".
func TestEscFromGlobsEditorStagesNothing(t *testing.T) {
	rows := []ui.OutputRow{{Name: "out", Format: "dotenv", Path: ".env"}}
	m := globsEditModel(t, fiveVarMatrix(), rows)
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	m, _ = m.Update(ui.KeyType(tea.KeySpace))
	m, _ = m.Update(ui.KeyType(tea.KeyEsc))

	if m.Mode() != ui.ModeBrowse {
		t.Fatalf("mode after esc = %v, want ModeBrowse", m.Mode())
	}
	if len(m.Staged()) != 0 {
		t.Errorf("Staged() = %+v, want empty after esc", m.Staged())
	}
}

// TestGlobsEditorEnterStagesSetGlobs covers Step 1's "saving stages
// set-globs", and pins the exact Include it writes for the mode the editor
// was left in.
func TestGlobsEditorEnterStagesSetGlobs(t *testing.T) {
	rows := []ui.OutputRow{{Name: "out", Format: "dotenv", Path: ".env", Include: []string{"NONE_OF_THESE"}}}
	m := globsEditModel(t, fiveVarMatrix(), rows)
	m, _ = m.Update(ui.KeyType(tea.KeyEnter)) // open, ModeOnlyThese (Include is non-empty)
	m, _ = m.Update(ui.KeyType(tea.KeySpace)) // check ALPHA
	m, _ = m.Update(ui.KeyType(tea.KeyEnter)) // save

	if m.Mode() != ui.ModeBrowse {
		t.Fatalf("mode after saving = %v, want ModeBrowse", m.Mode())
	}
	staged := m.Staged()
	want := ui.Change{Kind: ui.ChangeSetGlobs, Name: "out", Include: []string{"ALPHA"}, Exclude: nil}
	if len(staged) != 1 || !reflect.DeepEqual(staged[0], want) {
		t.Fatalf("Staged() = %+v, want exactly %+v", staged, want)
	}
}

// TestOutputWithStagedRemoveRefusesToOpenTheEditor is Five Things #4: editing
// the membership of an output the same batch removes is a contradiction
// commitStaged would have to resolve arbitrarily, the same shape as the
// phase-1 Critical where d then e on one row diverged by keypress order.
func TestOutputWithStagedRemoveRefusesToOpenTheEditor(t *testing.T) {
	rows := []ui.OutputRow{{Name: "out", Format: "dotenv", Path: ".env"}}
	m := globsEditModel(t, fiveVarMatrix(), rows)
	m, _ = m.Update(ui.KeyRune('d')) // stage remove-output

	m, _ = m.Update(ui.KeyType(tea.KeyEnter)) // must refuse to open the editor
	if m.Mode() != ui.ModeBrowse {
		t.Fatalf("mode after enter on a staged-remove row = %v, want to stay ModeBrowse", m.Mode())
	}
	v := m.View()
	if !strings.Contains(v, "disabled") {
		t.Errorf("no explanation for enter's refusal:\n%s", v)
	}
	if !strings.Contains(v, "out") {
		t.Errorf("explanation does not name the row:\n%s", v)
	}
}

// TestOutputDeleteRefusesOverStagedSetGlobs is the symmetric direction of
// the refusal above: d after a membership edit must refuse too, or the two
// keys pressed in the opposite order would reach commitStaged with both a
// removal and a membership edit staged for the same row.
func TestOutputDeleteRefusesOverStagedSetGlobs(t *testing.T) {
	rows := []ui.OutputRow{{Name: "out", Format: "dotenv", Path: ".env"}}
	m := globsEditModel(t, fiveVarMatrix(), rows)
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	m, _ = m.Update(ui.KeyType(tea.KeySpace))
	m, _ = m.Update(ui.KeyType(tea.KeyEnter)) // stage set-globs

	m, _ = m.Update(ui.KeyRune('d')) // must refuse

	staged := m.Staged()
	if len(staged) != 1 || staged[0].Kind != ui.ChangeSetGlobs {
		t.Fatalf("Staged() = %+v, want exactly the set-globs change (d must refuse over it)", staged)
	}
	if !strings.Contains(m.View(), "disabled") {
		t.Errorf("no explanation for d's refusal:\n%s", m.View())
	}
}

// TestOutputsTableReflectsAStagedMembershipEditBeforeSave mirrors cellText's
// own precedence for the Variables tab: a staged edit must be visible in the
// Outputs table's SELECTS column the instant it is staged, not only after w
// writes it to disk.
func TestOutputsTableReflectsAStagedMembershipEditBeforeSave(t *testing.T) {
	rows := []ui.OutputRow{{Name: "out", Format: "dotenv", Path: ".env", Selects: "all"}}
	m := globsEditModel(t, fiveVarMatrix(), rows)
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	m, _ = m.Update(ui.KeyType(tea.KeySpace)) // uncheck ALPHA -- everything-except mode
	m, _ = m.Update(ui.KeyType(tea.KeyEnter)) // save: excludes ALPHA

	line := lineFor(t, m.View(), "out")
	if !strings.Contains(line, "ALPHA") {
		t.Errorf("outputs table does not reflect the staged membership edit yet:\n%s", line)
	}
}

// Fix 5 (Minor, whole-branch review): View() appends the persistent keyHints
// footer below every tab's own body regardless of mode, and keyHints only
// ever checked m.tab (always TabOutputs while this editor is open), so it
// kept advertising a/r/D/d and "enter edit membership" -- every one of them
// inert here, since updateOutputGlobs only recognizes space, m, enter and
// esc. The editor's OWN inline footer already gets this right; the
// persistent one at the bottom of the screen must say the same thing, not a
// second, wrong one directly below it.
func TestKeyHintsInsideMembershipEditorShowOnlyTheKeysThatWork(t *testing.T) {
	rows := []ui.OutputRow{{Name: "out", Format: "dotenv", Path: ".env"}}
	m := globsEditModel(t, fiveVarMatrix(), rows)
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	if m.Mode() != ui.ModeOutputGlobs {
		t.Fatalf("mode after enter on an output row = %v, want ModeOutputGlobs", m.Mode())
	}

	v := m.View()
	for _, inert := range []string{"a add", "r rename", "D default", "d delete", "edit membership"} {
		if strings.Contains(v, inert) {
			t.Errorf("membership editor's footer advertises inert key %q:\n%s", inert, v)
		}
	}
	for _, works := range []string{"space toggle", "switch mode", "enter save", "esc cancel"} {
		if !strings.Contains(v, works) {
			t.Errorf("membership editor's footer does not name working key %q:\n%s", works, v)
		}
	}
}
