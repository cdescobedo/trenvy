package ui_test

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/style"
	"github.com/cdescobedo/trenvy/internal/ui"
)

// panelStyler has a renderer, unlike the zero Styler: Panel on a zero Styler
// returns the body unchanged, so a test built on newTestModel alone could
// never see a border at any width and would prove nothing about the floor.
// The writer is io.Discard, so the profile is Ascii: borders, no colour.
func panelStyler() style.Styler {
	return style.New(lipgloss.NewRenderer(io.Discard))
}

func panelModel(t *testing.T, st style.Styler, width int) ui.Model {
	t.Helper()
	m := allCellsModelStyled(t, st)
	if width > 0 {
		m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
	}
	return m
}

// The floor is a boundary, so test both sides of it.
func TestPanelsAppearAtSeventyTwoColumnsAndNotBelow(t *testing.T) {
	for _, tc := range []struct {
		width      int
		wantBorder bool
	}{
		{71, false},
		{72, true},
	} {
		out := panelModel(t, panelStyler(), tc.width).View()
		if got := strings.Contains(out, "╭"); got != tc.wantBorder {
			t.Errorf("width %d: border=%v, want %v\n%s", tc.width, got, tc.wantBorder, out)
		}
	}
}

// Every existing test builds a model and never sends a WindowSizeMsg, so
// width zero must mean flat. The styler has a renderer here: with the zero
// Styler this assertion would hold even if width zero drew panels.
func TestZeroWidthRendersFlat(t *testing.T) {
	if out := panelModel(t, panelStyler(), 0).View(); strings.Contains(out, "╭") {
		t.Errorf("a model that never got a WindowSizeMsg drew a border:\n%s", out)
	}
}

// Below the floor the layout is byte-identical to no width at all.
func TestNarrowWidthIsByteIdenticalToNoWidth(t *testing.T) {
	flat := panelModel(t, panelStyler(), 0).View()
	if got := panelModel(t, panelStyler(), 71).View(); got != flat {
		t.Errorf("width 71 differs from width 0:\n%s\n---\n%s", got, flat)
	}
}

// Every tab's body is a region, and the panel must not lose a word.
func TestEveryTabIsPanelledAndKeepsItsWords(t *testing.T) {
	for _, key := range []rune{'1', '2', '3', '4'} {
		flat := panelModel(t, panelStyler(), 0)
		wide := panelModel(t, panelStyler(), 80)
		flat, _ = flat.Update(ui.KeyRune(key))
		wide, _ = wide.Update(ui.KeyRune(key))
		out := wide.View()
		if !strings.Contains(out, "╭") {
			t.Errorf("tab %c: no border at width 80:\n%s", key, out)
		}
		for _, w := range strings.Fields(flat.View()) {
			if !strings.Contains(out, w) {
				t.Errorf("tab %c: word %q lost by the panel:\n%s", key, w, out)
			}
		}
	}
}

// Panel pads by visible width; a body line whose escapes were miscounted
// would push its right border out of the column.
func TestColouredPanelKeepsItsRightBorderStraight(t *testing.T) {
	m := panelModel(t, colourStyler(), 80)
	m, _ = m.Update(ui.KeyRune('2'))
	out := m.View()
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("no colour in output; the test proves nothing:\n%q", out)
	}
	var seen int
	for _, line := range strings.Split(out, "\n") {
		plain := stripANSI(line)
		if strings.HasPrefix(plain, "│") || strings.HasPrefix(plain, "╭") || strings.HasPrefix(plain, "╰") {
			seen++
			if w := lipgloss.Width(plain); w != 80 {
				t.Errorf("panel line is %d columns, want 80: %q", w, plain)
			}
		}
	}
	if seen < 3 {
		t.Fatalf("only %d panel lines found:\n%s", seen, out)
	}
}
