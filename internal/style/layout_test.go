package style_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/style"
)

// The whole point of the change: a styled cell carries ANSI escapes that
// text/tabwriter counted as visible width, shifting the row. lipgloss.Width
// measures what the terminal draws.
func TestColumnsIgnoresAnsiEscapesWhenMeasuring(t *testing.T) {
	plain := style.Columns([][]string{{"MISSING", "x"}, {"ok", "y"}}, 2)
	styled := style.Columns([][]string{{"\x1b[31mMISSING\x1b[0m", "x"}, {"ok", "y"}}, 2)

	stripped := strings.ReplaceAll(strings.ReplaceAll(styled, "\x1b[31m", ""), "\x1b[0m", "")
	if stripped != plain {
		t.Errorf("escapes changed the layout:\nplain:\n%q\nstyled, stripped:\n%q", plain, stripped)
	}
}

// Review Focus 1. A double-width glyph occupies two terminal columns but one
// rune; measuring by rune count puts the next column one place left.
func TestColumnsMeasuresDoubleWidthGlyphs(t *testing.T) {
	got := style.Columns([][]string{{"日本", "a"}, {"xy", "b"}}, 1)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), got)
	}
	// "日本" draws 4 columns, "xy" draws 2, so "xy" needs 2 extra spaces
	// before the gap to put both second cells in the same column.
	// strings.Index returns a byte offset, and "日本" is six bytes, so compare
	// the drawn width of everything before the second cell instead.
	col := func(line, cell string) int {
		return lipgloss.Width(line[:strings.Index(line, cell)])
	}
	if col(lines[0], "a") != col(lines[1], "b") {
		t.Errorf("second column misaligned:\n%q\n%q", lines[0], lines[1])
	}
}

// Review Focus 2. profile.go emits two cells for a profile with no note and
// three for one with a note; indexing past a short row must not panic.
func TestColumnsHandlesRaggedRows(t *testing.T) {
	got := style.Columns([][]string{{"*", "prod"}, {" ", "dev", "(default)"}}, 2)
	if !strings.Contains(got, "(default)") {
		t.Errorf("lost the third cell: %q", got)
	}
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if strings.TrimRight(line, " ") != line {
			t.Errorf("row has trailing whitespace: %q", line)
		}
	}
}

// Review Focus 3. A filter matching nothing, or a project with no outputs.
func TestColumnsHandlesEmptyGrid(t *testing.T) {
	if got := style.Columns(nil, 2); got != "" {
		t.Errorf("Columns(nil) = %q, want empty", got)
	}
	if got := style.Columns([][]string{}, 2); got != "" {
		t.Errorf("Columns(empty) = %q, want empty", got)
	}
}
