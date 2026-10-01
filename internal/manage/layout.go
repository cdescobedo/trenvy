package manage

import "github.com/charmbracelet/lipgloss"

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

// window returns at most height of lines, scrolled so that line focus is
// visible.
func window(lines []string, focus, height int) []string {
	if height <= 0 {
		return nil
	}
	if len(lines) <= height {
		return lines
	}
	start := 0
	if focus >= height {
		start = focus - height + 1
	}
	if start+height > len(lines) {
		start = len(lines) - height
	}
	return lines[start : start+height]
}
