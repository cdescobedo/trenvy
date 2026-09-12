package tui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/envctl/internal/tui"
)

func keyRune(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func TestViewMasksSecretsByDefault(t *testing.T) {
	m := tui.New("vpb", []tui.Row{
		{Name: "ALPHA_TOKEN", Secret: true, Present: true, Value: "s3cretvalue"},
	})
	out := m.View()
	if strings.Contains(out, "s3cretvalue") {
		t.Errorf("the view printed a secret value:\n%s", out)
	}
	if !strings.Contains(out, "•") {
		t.Errorf("the view does not mask the value:\n%s", out)
	}
}

// TestViewShowsARevealedSecretValue covers the other half of the masking
// constraint: once the user has genuinely revealed a secret (Revealed is set
// only by the RevealedMsg handler in Update, never by direct construction),
// the view must show it — a reveal key that reveals nothing would be worse
// than the masking bug it replaces.
func TestViewShowsARevealedSecretValue(t *testing.T) {
	m := tui.New("vpb", []tui.Row{
		{Name: "ALPHA_TOKEN", Secret: true, Present: true, Value: "s3cretvalue", Revealed: true},
		{Name: "OTHER_SECRET", Secret: true, Present: true},
	})
	out := m.View()
	if !strings.Contains(out, "s3cretvalue") {
		t.Errorf("a revealed secret value is not shown:\n%s", out)
	}
	if !strings.Contains(out, "•") {
		t.Errorf("the unrevealed secret row is not masked:\n%s", out)
	}
}

func TestViewMarksAMissingSecret(t *testing.T) {
	m := tui.New("vpb", []tui.Row{
		{Name: "ALPHA_TOKEN", Secret: true, Present: false},
	})
	if !strings.Contains(m.View(), "MISSING") {
		t.Errorf("a missing secret is not flagged:\n%s", m.View())
	}
}

func TestViewShowsPlainValues(t *testing.T) {
	m := tui.New("vpb", []tui.Row{{Name: "URLS", Value: "http://+:8080"}})
	if !strings.Contains(m.View(), "http://+:8080") {
		t.Errorf("a plain value is not shown:\n%s", m.View())
	}
}

func TestViewShowsTheKeyHelp(t *testing.T) {
	out := tui.New("vpb", nil).View()
	for _, k := range tui.Keys {
		if !strings.Contains(out, k.Help) {
			t.Errorf("help line is missing %q:\n%s", k.Help, out)
		}
	}
}

func TestViewMarksDeletedAndDirtyRows(t *testing.T) {
	m := tui.New("vpb", []tui.Row{
		{Name: "GONE", Value: "x", Deleted: true},
		{Name: "CHANGED", Value: "y", Dirty: true},
	})
	out := m.View()
	if !strings.Contains(out, "GONE") || !strings.Contains(out, "CHANGED") {
		t.Fatalf("rows missing:\n%s", out)
	}
	if !strings.Contains(out, "del") {
		t.Errorf("a deleted row is not marked:\n%s", out)
	}
	if !strings.Contains(out, "*") {
		t.Errorf("a dirty row is not marked:\n%s", out)
	}
}

func TestViewTruncatesLongValues(t *testing.T) {
	long := strings.Repeat("x", 500)
	out := tui.New("vpb", []tui.Row{{Name: "LONG", Value: long}}).View()
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 200 {
			t.Errorf("line is %d chars; long values must be truncated", len(line))
		}
	}
}

// TestViewWarnsWhenAddingADuplicateName covers Ruling 2: the model itself
// silently refuses to add a name that already exists (Task 24), which would
// otherwise make the user's typed name vanish with no explanation. The view
// is where that refusal becomes visible.
func TestViewWarnsWhenAddingADuplicateName(t *testing.T) {
	m := tui.New("vpb", []tui.Row{{Name: "EXISTING", Value: "x"}})
	m, _ = m.Update(keyRune('a'))
	if m.Mode() != tui.ModeAddName {
		t.Fatalf("mode = %v, want ModeAddName", m.Mode())
	}
	for _, r := range "EXISTING" {
		m, _ = m.Update(keyRune(r))
	}
	out := m.View()
	if !strings.Contains(out, "already exists") {
		t.Errorf("no warning shown for a duplicate name:\n%s", out)
	}
}

func TestViewNoWarningForANewName(t *testing.T) {
	m := tui.New("vpb", []tui.Row{{Name: "EXISTING", Value: "x"}})
	m, _ = m.Update(keyRune('a'))
	for _, r := range "BRAND_NEW" {
		m, _ = m.Update(keyRune(r))
	}
	out := m.View()
	if strings.Contains(out, "already exists") {
		t.Errorf("unexpected warning for a new name:\n%s", out)
	}
}
