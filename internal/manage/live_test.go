package manage_test

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/style"
)

func trueColorTheme() style.Theme {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.TrueColor)
	return style.NewTheme(r)
}

// Review Focus 5: a switch with an edit staged and an overlay open.
func TestThemeMsgKeepsStateAndOverlays(t *testing.T) {
	f := &fakeDeps{}
	m := press(newModel(f), "j", "enter", "enter") // APP_MODE detail, edit overlay open
	m = typeText(m, "staged-value")
	before := m.View()
	m = send(m, manage.ThemeMsg{Theme: trueColorTheme()})
	after := m.View()
	if !strings.Contains(after, "staged-value") {
		t.Errorf("the open edit lost its text:\n%s", after)
	}
	if strings.Contains(before, "\x1b[") || !strings.Contains(after, "\x1b[") {
		t.Errorf("the theme did not change: before had escapes=%v, after=%v", strings.Contains(before, "\x1b["), strings.Contains(after, "\x1b["))
	}
	m = press(m, "enter")
	if got := len(m.Staged()); got != 1 {
		t.Errorf("staged = %d after the theme change, want 1", got)
	}
}

func TestFocusAsksTheTerminalAgain(t *testing.T) {
	asked := 0
	f := &fakeDeps{}
	d := f.deps()
	d.Requery = func() error { asked++; return nil }
	m := sized(manage.New(fixture(), d, noTheme()), 100, 30)
	m = send(m, tea.FocusMsg{})
	if asked != 1 {
		t.Errorf("Requery calls = %d after focus, want 1", asked)
	}
}

func TestPollAsksAgainOnlyWhenSet(t *testing.T) {
	f := &fakeDeps{}
	m := manage.New(fixture(), f.deps(), noTheme())
	if m.Init() != nil {
		t.Error("Init scheduled a timer without Poll")
	}
	m = m.WithRun(manage.RunOptions{Poll: 5 * time.Second})
	if m.Init() == nil {
		t.Error("Init did not schedule the poll timer")
	}
}

func TestSetupTakesAThemeMsg(t *testing.T) {
	s := newSetupAt(setupInput(), 100, 30)
	next, _ := s.Update(manage.ThemeMsg{Theme: trueColorTheme()})
	if !strings.Contains(next.(manage.Setup).View(), "\x1b[") {
		t.Error("setup did not take the new theme")
	}
}
