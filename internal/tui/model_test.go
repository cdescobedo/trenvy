package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/tui"
)

func key(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	panic("unknown key " + s)
}

func press(m tui.Model, keys ...string) tui.Model {
	for _, k := range keys {
		m, _ = m.Update(key(k))
	}
	return m
}

// chars splits s into single-rune key presses for press().
func chars(s string) []string {
	out := make([]string, 0, len(s))
	for _, r := range s {
		out = append(out, string(r))
	}
	return out
}

func fixture() tui.Model {
	return tui.New("vpb", []tui.Row{
		{Name: "APP_URL", Value: "http://+:8080"},
		{Name: "ALPHA_TOKEN", Secret: true, Present: true},
		{Name: "BETA_TOKEN", Secret: true, Present: false},
	})
}

func TestCursorMoves(t *testing.T) {
	m := press(fixture(), "j", "j")
	if m.Cursor() != 2 {
		t.Errorf("Cursor = %d, want 2", m.Cursor())
	}
	m = press(m, "k")
	if m.Cursor() != 1 {
		t.Errorf("Cursor = %d, want 1", m.Cursor())
	}
}

func TestCursorStopsAtTheEnds(t *testing.T) {
	t.Run("clamps at the top", func(t *testing.T) {
		m := press(fixture(), "k", "k")
		if m.Cursor() != 0 {
			t.Errorf("Cursor = %d, want 0", m.Cursor())
		}
	})

	t.Run("clamps at the bottom", func(t *testing.T) {
		m := press(fixture(), "j", "j", "j", "j", "j")
		if m.Cursor() != 2 {
			t.Errorf("Cursor = %d, want the last row", m.Cursor())
		}
	})
}

func TestEditReplacesAValueAndMarksItDirty(t *testing.T) {
	m := press(fixture(), "e", "x", "y", "enter")
	row := m.Rows()[0]
	if row.Value != "xy" {
		t.Errorf("Value = %q, want xy", row.Value)
	}
	if !row.Dirty {
		t.Error("the row is not marked dirty")
	}
}

func TestEscapeAbandonsAnEdit(t *testing.T) {
	m := press(fixture(), "e", "z", "esc")
	row := m.Rows()[0]
	if row.Value != "http://+:8080" {
		t.Errorf("Value = %q, want the original", row.Value)
	}
	if row.Dirty {
		t.Error("an abandoned edit marked the row dirty")
	}
	if m.Mode() != tui.ModeBrowse {
		t.Errorf("Mode = %v, want browse", m.Mode())
	}
}

func TestToggleSecretClearsThePlainValue(t *testing.T) {
	m := press(fixture(), "s")
	row := m.Rows()[0]
	if !row.Secret {
		t.Fatal("the row was not marked secret")
	}
	// Keeping the old plain value would put a now-secret value in the
	// manifest, which is the one place a secret must never be.
	if row.Value != "" {
		t.Errorf("Value = %q, want cleared", row.Value)
	}
	if !row.Dirty {
		t.Error("the row is not marked dirty")
	}
}

func TestDeleteMarksRatherThanRemoves(t *testing.T) {
	m := press(fixture(), "d")

	t.Run("marks the row deleted without removing it", func(t *testing.T) {
		rows := m.Rows()
		if len(rows) != 3 {
			t.Fatalf("len(Rows) = %d; deletion must be deferred to save", len(rows))
		}
		if !rows[0].Deleted {
			t.Error("the row is not marked deleted")
		}
	})

	// Pressing d again undoes it: nothing is destroyed before save.
	m = press(m, "d")

	t.Run("a second press undoes the deletion", func(t *testing.T) {
		if m.Rows()[0].Deleted {
			t.Error("a second d did not undo the deletion")
		}
	})
}

func TestRevealOnlyAffectsTheSelectedRow(t *testing.T) {
	m := press(fixture(), "j")
	m, cmd := m.Update(key("r"))
	if cmd == nil {
		t.Fatal("reveal issued no command to fetch the value")
	}
	m, _ = m.Update(tui.RevealedMsg{Name: "ALPHA_TOKEN", Value: "s3cret"})

	if got := m.Rows()[1].Value; got != "s3cret" {
		t.Errorf("revealed value = %q", got)
	}
	if !m.Rows()[1].Revealed {
		t.Error("revealing did not mark the row Revealed")
	}
	if m.Rows()[1].Dirty {
		t.Error("revealing marked the row dirty; it changed nothing")
	}
	if m.Rows()[2].Value != "" {
		t.Error("revealing one row revealed another")
	}
	if m.Rows()[2].Revealed {
		t.Error("revealing one row marked another Revealed")
	}
}

func TestAddCreatesARow(t *testing.T) {
	m := press(fixture(), "a", "N", "E", "W", "enter")
	rows := m.Rows()
	if len(rows) != 4 {
		t.Fatalf("len(Rows) = %d, want 4", len(rows))
	}
	var found bool
	for _, r := range rows {
		if r.Name == "NEW" {
			found = true
			if !r.Dirty {
				t.Error("a new row is not dirty")
			}
		}
	}
	if !found {
		t.Errorf("no NEW row: %+v", rows)
	}
}

func TestFilterNarrowsTheVisibleRows(t *testing.T) {
	m := press(fixture(), "/", "A", "L", "P", "enter")

	t.Run("narrows to the matching row", func(t *testing.T) {
		if len(m.Rows()) != 1 || m.Rows()[0].Name != "ALPHA_TOKEN" {
			t.Errorf("Rows = %+v, want only the matching row", m.Rows())
		}
	})

	// Escape clears it, so nothing is permanently hidden.
	m = press(m, "esc")

	t.Run("escape clears the filter", func(t *testing.T) {
		if len(m.Rows()) != 3 {
			t.Errorf("len(Rows) = %d after clearing the filter, want 3", len(m.Rows()))
		}
	})
}

func TestWriteSignalsSave(t *testing.T) {
	m := press(fixture(), "e", "x", "enter")
	m, cmd := m.Update(key("w"))
	if !m.Saved() {
		t.Error("w did not mark the model saved")
	}
	if cmd == nil {
		t.Error("w issued no command")
	}
}

// Nothing is written until w, so quitting with unsaved work must not lose it
// silently, and killing the process must change nothing at all.
func TestQuitWithUnsavedChangesAsksFirst(t *testing.T) {
	m := press(fixture(), "e", "x", "enter")
	m = press(m, "q")

	t.Run("asks for confirmation instead of quitting", func(t *testing.T) {
		if m.Mode() != tui.ModeConfirmQuit {
			t.Errorf("Mode = %v, want a quit confirmation", m.Mode())
		}
		if m.Cancelled() {
			t.Error("the model quit without confirmation")
		}
	})

	m = press(m, "y")

	t.Run("confirming quits", func(t *testing.T) {
		if !m.Cancelled() {
			t.Error("confirming did not cancel")
		}
	})
}

// Ctrl+C must cancel immediately no matter what mode the user is in.
// bubbletea delivers it as an ordinary KeyMsg rather than SIGINT, so without
// a handler in every mode, a user in the middle of typing a value has no way
// out except knowing to press esc first.
func TestCtrlCCancelsImmediatelyFromEditMode(t *testing.T) {
	m := press(fixture(), "e", "x", "y") // mid-edit, nothing committed yet
	if m.Mode() != tui.ModeEditValue {
		t.Fatalf("Mode = %v, want edit (test setup)", m.Mode())
	}

	m = press(m, "ctrl+c")

	if !m.Cancelled() {
		t.Error("ctrl+c from edit mode did not cancel")
	}
}

// Ctrl+C is an unconditional, immediate cancel, unlike q -- which asks first
// when there are unsaved changes. Even from the quit confirmation itself,
// ctrl+c must not fall through to "no" the way any other key does.
func TestCtrlCCancelsImmediatelyFromQuitConfirmation(t *testing.T) {
	m := press(fixture(), "e", "x", "enter") // dirty
	m = press(m, "q")
	if m.Mode() != tui.ModeConfirmQuit {
		t.Fatalf("Mode = %v, want confirm-quit (test setup)", m.Mode())
	}

	m = press(m, "ctrl+c")

	if !m.Cancelled() {
		t.Error("ctrl+c from the quit confirmation did not cancel")
	}
}

func TestQuitWithNoChangesLeavesImmediately(t *testing.T) {
	m := press(fixture(), "q")
	if !m.Cancelled() {
		t.Error("q with a clean model did not quit")
	}
}

// A duplicate name must never be creatable: the manifest stores variables in
// a map, so two rows sharing a name would silently collapse on save.
func TestAddRejectsADuplicateName(t *testing.T) {
	keys := append([]string{"a"}, append(chars("ALPHA_TOKEN"), "enter")...)
	m := press(fixture(), keys...)
	rows := m.Rows()
	if len(rows) != 3 {
		t.Fatalf("len(Rows) = %d, want 3 (a duplicate name must be rejected)", len(rows))
	}
}

// realIndex must resolve the cursor by filtered position, not by name.
// Two rows sharing a name, both visible under a filter, must let an edit on
// the second land on the second — resolving by name would always hit the
// first match instead, silently corrupting the wrong row.
func TestFilteredEditLandsOnTheCorrectRowWhenNamesCollide(t *testing.T) {
	m := tui.New("t", []tui.Row{
		{Name: "DUP", Value: "first"},
		{Name: "OTHER", Value: "other"},
		{Name: "DUP", Value: "second"},
	})

	keys := append([]string{"/"}, append(chars("DUP"), "enter", "j")...)
	m = press(m, keys...)
	m = press(m, "e", "X", "enter")

	all := m.AllRows()
	if all[2].Value != "X" {
		t.Errorf("all[2].Value = %q, want X (the second DUP row, selected by position)", all[2].Value)
	}
	if all[0].Value != "first" {
		t.Errorf("all[0].Value = %q, want unchanged; the edit landed on the wrong row", all[0].Value)
	}
}

// Reveal deliberately does not mark a row dirty because it changes nothing.
// Toggling secret off afterward must not destroy that revealed value: only
// the plain-to-secret transition may clear Value, never the reverse.
func TestToggleSecretOffPreservesARevealedValue(t *testing.T) {
	m := press(fixture(), "j") // ALPHA_TOKEN: secret, present
	m, cmd := m.Update(key("r"))
	if cmd == nil {
		t.Fatal("reveal issued no command to fetch the value")
	}
	m, _ = m.Update(tui.RevealedMsg{Name: "ALPHA_TOKEN", Value: "s3cret"})

	m = press(m, "s") // toggle secret off
	row := m.Rows()[1]
	if row.Secret {
		t.Fatal("the row is still marked secret")
	}
	if row.Value != "s3cret" {
		t.Errorf("Value = %q, want the revealed value preserved", row.Value)
	}
}
