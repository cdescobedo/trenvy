// Package style owns trenvy's rendered vocabulary: the colour of every token
// that carries meaning, and the two containers those tokens sit in.
//
// It exists because the same words -- MISSING, stored, STALE, optional --
// are rendered by both internal/ui and internal/cli. Defined twice, they
// would drift, and a token that means one thing in `trenvy status` and
// another in `trenvy ui` is worse than an unstyled one.
package style

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Columns lays out a grid, padding every column to its widest visible cell.
//
// It replaces text/tabwriter, which measures bytes. An ANSI escape inside a
// cell counts toward tabwriter's idea of width and shifts the whole row --
// the same class of defect as a double-width glyph, which phase 1 of the TUI
// hit with a lock emoji. lipgloss.Width measures what the terminal draws.
//
// A row's last non-empty cell is never padded, and trailing empty cells are
// ignored, so no line carries trailing whitespace.
// tabwriter padded a trailing empty cell to its column width, which callers
// worked around by hand (see the note this replaces in internal/cli/profile.go).
func Columns(rows [][]string, gap int) string {
	if len(rows) == 0 {
		return ""
	}
	// A trailing empty cell is dropped before measuring or padding, so the
	// gap is never written after a row's last visible cell. Empty cells in
	// the middle of a row are kept: they hold their column open.
	trimmed := make([][]string, len(rows))
	for i, row := range rows {
		end := len(row)
		for end > 0 && row[end-1] == "" {
			end--
		}
		trimmed[i] = row[:end]
	}
	rows = trimmed

	var widths []int
	for _, row := range rows {
		for i, cell := range row {
			w := lipgloss.Width(cell)
			switch {
			case i >= len(widths):
				widths = append(widths, w)
			case w > widths[i]:
				widths[i] = w
			}
		}
	}

	var b strings.Builder
	for _, row := range rows {
		for i, cell := range row {
			b.WriteString(cell)
			if i < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-lipgloss.Width(cell)+gap))
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// panel wraps body in a rounded border with title on the top edge. It is
// unexported and reached through Styler.Panel, so a caller never has to hold
// a renderer of its own. The TUI uses it; the CLI never does, because the CLI
// has no width guarantee and is routinely piped.
func panel(r *lipgloss.Renderer, title, body string, width int) string {
	border := lipgloss.RoundedBorder()
	boxed := r.NewStyle().
		Border(border).
		Width(width - 2).
		Render(strings.TrimRight(body, "\n"))

	lines := strings.Split(boxed, "\n")
	if title == "" || len(lines) == 0 {
		return boxed
	}
	// Splice " title " into the top edge after the corner, keeping the
	// border's total width: the run of horizontals shrinks by exactly what
	// the title occupies. Measured with lipgloss.Width, never len, so a
	// double-width glyph in the title cannot push the corner out.
	label := " " + title + " "
	run := width - 2 - lipgloss.Width(label)
	if run < 1 {
		return boxed // too narrow to title; the border alone still reads
	}
	lines[0] = border.TopLeft + label + strings.Repeat(border.Top, run) + border.TopRight
	return strings.Join(lines, "\n")
}
