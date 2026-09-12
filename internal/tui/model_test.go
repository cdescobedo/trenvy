package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/envctl/internal/tui"
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
	}
	panic("unknown key " + s)
}

func press(m tui.Model, keys ...string) tui.Model {
	for _, k := range keys {
		m, _ = m.Update(key(k))
	}
	return m
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
	if m.Rows()[1].Dirty {
		t.Error("revealing marked the row dirty; it changed nothing")
	}
	if m.Rows()[2].Value != "" {
		t.Error("revealing one row revealed another")
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
	m := press(fixture(), "/", "B", "A", "L", "enter")

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

func TestQuitWithNoChangesLeavesImmediately(t *testing.T) {
	m := press(fixture(), "q")
	if !m.Cancelled() {
		t.Error("q with a clean model did not quit")
	}
}
