// Package style owns trenvy's rendered vocabulary: the colour of every token
// that carries meaning, and the two containers those tokens sit in.
//
// It exists because the same words -- MISSING, stored, STALE, optional --
// are rendered by both internal/manage and internal/cli. Defined twice, they
// would drift, and a token that means one thing in `trenvy status` and
// another in `trenvy ui` is worse than an unstyled one.
package style

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Columns lays out a grid, padding every column to its widest visible cell.
//
// It replaces text/tabwriter, which counts runes (utf8.RuneCount), not
// terminal columns, and that goes wrong in two directions. An ANSI escape is
// several runes that draw nothing, so tabwriter OVERcounts a styled cell. A
// double-width glyph (the lock emoji phase 1 of the TUI hit) is one rune but
// two columns, so tabwriter UNDERcounts it. Either way the row shifts.
// lipgloss.Width measures what the terminal draws.
//
// A row's last non-empty cell is never padded, and trailing EMPTY cells ("")
// are not printed, so the gap is never written after a row's last cell. A
// row's last cell as given does not widen its column; see the width pass. A cell
// holding only whitespace is not empty and is kept as given. (text/tabwriter
// padded a trailing empty cell to its column width, which callers had to
// work around by omitting the cell.)
//
// Known limit: a tab inside a cell is measured as zero columns
// (lipgloss.Width("\t") == 0) but a terminal advances it to the next tab
// stop, so a cell holding a tab misaligns its row and, in a non-last column,
// every cell after it. Columns does not strip or replace tabs: the cells are
// data this package does not own, and silently rewriting a value the user set
// is a worse failure than a misaligned row. See TestColumnsMeasuresATabAsZeroWidth.
func Columns(rows [][]string, gap int) string {
	if len(rows) == 0 {
		return ""
	}
	// Widths are decided from the rows exactly as the caller passed them. A
	// row's last cell, as given, is printed but never measured: it has nothing
	// after it to align against, so it must not widen its column. A caller
	// that wants a cell kept out of the width table passes a shorter row. A
	// trailing "" is a cell like any other here, so the cell before it still
	// counts -- the same distinction tabwriter drew between "name\n" and
	// "name\t\n".
	var widths []int
	for _, row := range rows {
		for i, cell := range row[:max(len(row)-1, 0)] {
			w := lipgloss.Width(cell)
			switch {
			case i >= len(widths):
				widths = append(widths, w)
			case w > widths[i]:
				widths[i] = w
			}
		}
	}

	// Trimming governs printing only: a trailing empty cell is not written,
	// and so the last cell actually printed is never padded. Empty cells in
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
