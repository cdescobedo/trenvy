package ui_test

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/ui"
)

// q with nothing staged has nothing to lose by quitting immediately -- the
// confirmation exists to protect staged work, not to slow down a clean exit.
func TestQWithNothingStagedQuitsImmediately(t *testing.T) {
	m := newTestModel(t)

	m, cmd := m.Update(ui.KeyRune('q'))

	if m.Saved() {
		t.Errorf("Saved() = true after q, want false")
	}
	if cmd == nil {
		t.Fatal("q with nothing staged did not return a command to end the session")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q's command = %T, want tea.Quit", cmd())
	}
}

// q with staged changes must not quit outright: it has something to lose,
// so it asks first, mirroring internal/tui's ModeConfirmQuit.
func TestQWithStagedChangesAsksForConfirmation(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2')) // Variables tab, so e actually opens
	m = editCell(m, "NEWVAL")
	staged := m.Staged()

	m, cmd := m.Update(ui.KeyRune('q'))

	if m.Mode() != ui.ModeConfirmQuit {
		t.Fatalf("mode after q with staged changes = %v, want ModeConfirmQuit", m.Mode())
	}
	if cmd != nil {
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Error("q with staged changes must not quit before confirming")
		}
	}
	if got := m.Staged(); len(got) != len(staged) {
		t.Errorf("Staged() = %+v while awaiting confirmation, want untouched %+v", got, staged)
	}
}

// The load-bearing assertion: what survives a decline. Staging a change,
// pressing q, then declining, must leave Staged() unchanged, must not mark
// the model saved, must not quit, and must return the user to browse mode on
// exactly the tab/row/col they left -- not just show a prompt that then
// forgets where they were.
func TestQConfirmationDecliningLeavesStagedAndCursorUntouched(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2')) // Variables tab
	m, _ = m.Update(ui.KeyRune('l')) // move column so position is non-default
	m = editCell(m, "NEWVAL")
	staged := m.Staged()
	wantTab, wantRow, wantCol := m.Tab(), m.Row(), m.Col()

	m, _ = m.Update(ui.KeyRune('q'))
	if m.Mode() != ui.ModeConfirmQuit {
		t.Fatalf("mode after q = %v, want ModeConfirmQuit (test setup)", m.Mode())
	}

	// Every key but y answers no -- exercise one that is not esc or n, to
	// prove the rule is "not y", not a hard-coded list of refusal keys.
	m, cmd := m.Update(ui.KeyRune('x'))

	if m.Mode() != ui.ModeBrowse {
		t.Errorf("mode after declining = %v, want ModeBrowse", m.Mode())
	}
	if m.Saved() {
		t.Error("Saved() = true after declining a quit, want false")
	}
	if _, ok := m.SwitchTo(); ok {
		t.Error("declining a quit must not report a switch")
	}
	if cmd != nil {
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Error("declining a quit must not end the session")
		}
	}
	if got := m.Staged(); len(got) != len(staged) || !reflect.DeepEqual(got[0], staged[0]) {
		t.Errorf("Staged() = %+v after declining, want untouched %+v", got, staged)
	}
	if m.Tab() != wantTab || m.Row() != wantRow || m.Col() != wantCol {
		t.Errorf("cursor after declining = (tab %v, row %d, col %d), want (%v, %d, %d) -- unchanged",
			m.Tab(), m.Row(), m.Col(), wantTab, wantRow, wantCol)
	}
}

// Confirming with y ends the session without ever setting Saved() -- a
// discarded quit must never be mistaken for a write.
func TestQConfirmationAcceptingQuitsWithoutSaving(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m = editCell(m, "NEWVAL")

	m, _ = m.Update(ui.KeyRune('q'))
	m, cmd := m.Update(ui.KeyRune('y'))

	if m.Saved() {
		t.Errorf("Saved() = true after confirming a quit, want false")
	}
	if cmd == nil {
		t.Fatal("confirming a quit did not return a command to end the session")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("confirming a quit's command = %T, want tea.Quit", cmd())
	}
}

// ctrl+c must cancel immediately from browse mode, staged changes or not --
// a user who cannot remember any other key must always have this one.
func TestCtrlCCancelsImmediatelyFromBrowseMode(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m = editCell(m, "NEWVAL")

	m, cmd := m.Update(ui.KeyType(tea.KeyCtrlC))

	if m.Saved() {
		t.Error("Saved() = true after ctrl+c, want false")
	}
	if cmd == nil {
		t.Fatal("ctrl+c did not return a command to end the session")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl+c's command = %T, want tea.Quit", cmd())
	}
}

// ctrl+c must not fall through to "no" the way every other key does from
// the quit confirmation itself -- it is an unconditional cancel, not one
// more answer to the prompt.
func TestCtrlCCancelsImmediatelyFromQuitConfirmation(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m = editCell(m, "NEWVAL")
	m, _ = m.Update(ui.KeyRune('q'))
	if m.Mode() != ui.ModeConfirmQuit {
		t.Fatalf("mode after q = %v, want ModeConfirmQuit (test setup)", m.Mode())
	}

	m, cmd := m.Update(ui.KeyType(tea.KeyCtrlC))

	if m.Saved() {
		t.Error("Saved() = true after ctrl+c from the quit confirmation, want false")
	}
	if cmd == nil {
		t.Fatal("ctrl+c from the quit confirmation did not return a command to end the session")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl+c's command = %T, want tea.Quit", cmd())
	}
}

// With nothing staged, the status line must not lie: no "0 ... changes", no
// offer to save or discard something that does not exist.
func TestStatusLineAtZeroStagedDoesNotOfferSaveOrDiscard(t *testing.T) {
	m := newTestModel(t)

	v := m.View()

	lower := strings.ToLower(v)
	if strings.Contains(lower, "save") || strings.Contains(lower, "discard") {
		t.Errorf("status line offers save/discard with nothing staged:\n%s", v)
	}
	if !strings.Contains(lower, "no staged changes") {
		t.Errorf("status line does not say plainly that nothing is staged:\n%s", v)
	}
}

// With staged changes, the status line must show the count and both the
// save and discard keys.
func TestStatusLineShowsStagedCountAndKeys(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('2'))
	m = editCell(m, "NEWVAL")

	v := m.View()

	if !strings.Contains(v, "1 staged change") {
		t.Errorf("status line does not show the staged count:\n%s", v)
	}
	if !strings.Contains(v, "w") || !strings.Contains(v, "save") {
		t.Errorf("status line does not name w to save:\n%s", v)
	}
	if !strings.Contains(v, "q") || !strings.Contains(v, "discard") {
		t.Errorf("status line does not name q to discard:\n%s", v)
	}
}
