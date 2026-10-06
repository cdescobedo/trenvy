package manage

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/style"
)

// Layout constants (spec §1).
const (
	stackBelow   = 80  // narrower than this, the panes stack
	wideFrom     = 120 // from this width the list previews the active value
	minWidth     = 40
	minHeight    = 10
	chromeLines  = 4 // header, tabs, message, footer
	listMin      = 24
	listMax      = 36
	previewWidth = 24
	panelMax     = 76 // the widest an overlay panel draws
	// maxMessageLines is how far the message line may wrap; the body gives
	// up a line when it does.
	maxMessageLines = 2
	// listChrome is everything in a list row but the name: two borders,
	// the one-cell cursor column and a space, a space, the six-cell
	// "secret" tag, a space and the one-cell mark.
	listChrome = 13
)

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return max(lo, min(v, hi))
}

// listWidth is the list pane's width: the longest name plus its chrome,
// clamped, plus the preview column on a wide terminal.
func (m Model) listWidth(w int) int {
	longest := 0
	for _, n := range m.allNames() {
		longest = max(longest, lipgloss.Width(n))
	}
	lw := clamp(longest+listChrome, listMin, listMax)
	if w >= wideFrom {
		lw += previewWidth + 1
	}
	return lw
}

// wrap breaks s into lines of at most width cells, at spaces where it can
// and inside a word where it must, so nothing is cut.
func wrap(s string, width int) []string {
	lines := strings.Split(lipgloss.NewStyle().Width(max(1, width)).Render(s), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return lines
}

// window returns at most height of lines, scrolled so that line focus is
// visible.
func window(lines []string, focus, height int) []string {
	return windowRow(lines, focus, focus, height)
}

// windowRow is window for a row that may span several lines: it shows
// lines first..last whole when they fit, and the row's start when not.
func windowRow(lines []string, first, last, height int) []string {
	if height <= 0 {
		return nil
	}
	if len(lines) <= height {
		return lines
	}
	start := 0
	if last >= height {
		start = last - height + 1
	}
	start = min(start, first)
	start = clamp(start, 0, len(lines)-height)
	return lines[start : start+height]
}

// termSize is the terminal's size, with a default before the first
// WindowSizeMsg arrives.
func termSize(w, h int) (int, int) {
	if w == 0 {
		w = 100
	}
	if h == 0 {
		h = 30
	}
	return w, h
}

// tooSmall is the sentence drawn instead of the interface on a terminal
// below minWidth x minHeight, or "" when it is big enough.
func tooSmall(w, h int) string {
	if w < minWidth || h < minHeight {
		return fmt.Sprintf("trenvy needs at least %d×%d -- this terminal is %d×%d", minWidth, minHeight, w, h)
	}
	return ""
}

// messageLines is the message, wrapped rather than cut so its end -- often
// the way to undo or recover -- survives. Past maxMessageLines the last line
// is cut, ending in ….
func messageLines(th style.Theme, msg message, w int) []string {
	st := th.Text
	if msg.isErr {
		st = th.Danger
	}
	lines := wrap(display(msg.text), w-2)
	if len(lines) > maxMessageLines {
		rest := strings.Join(lines[maxMessageLines-1:], " ")
		lines = append(lines[:maxMessageLines-1], cutTo(rest, w-2))
	}
	for i, l := range lines {
		lines[i] = " " + st.Render(l)
	}
	return lines
}
