package ui

import (
	"fmt"
	"strings"
	"text/tabwriter"
)

// nameColumnWidth bounds a variable name before it is truncated with "…".
// DEBUG and NEWTONSOFT_JSONSCHEMA_LICENSE_KEY both need to fit in the same
// grid, so this is generous rather than tuned to a typical name.
const nameColumnWidth = 40

// viewVariables renders the profile matrix: one row per variable, one column
// per profile, using the exact Cell words from cell.go (shared, override,
// stored, MISSING, optional) as the audit surface a user reads to decide
// what is wrong with their project -- abbreviating or re-casing them would
// undermine that. text/tabwriter, not hand-computed padding, does the
// alignment: a name longer than expected must not misalign a column and put
// a value under the wrong profile, which is what internal/cli/status.go's
// use of tabwriter also guards against.
//
// A secret row is marked with the word "secret" in its own tab-separated
// column, not a glyph. tabwriter sizes columns by rune count, and a
// double-width symbol (the terminal's opinion, not the rune count's) throws
// that off in exactly the row it is meant to call out -- on the one view
// whose whole job is trustworthy alignment. A word also matches the
// vocabulary already on screen instead of adding a symbol nothing else uses.
func (m Model) viewVariables() string {
	if m.mx == nil || len(m.mx.Names) == 0 {
		return "No variables defined.\n"
	}

	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)

	header := append([]string{"", "NAME", ""}, m.mx.Profiles...)
	fmt.Fprintln(w, strings.Join(header, "\t"))

	for row, name := range m.mx.Names {
		cursor := " "
		if row == m.row {
			cursor = ">"
		}
		secretWord := ""
		if m.mx.Vars[name].Secret {
			secretWord = "secret"
		}
		cells := []string{cursor, truncateName(name), secretWord}

		for col, profile := range m.mx.Profiles {
			cell := string(m.mx.Cell(name, profile))
			// The column cursor marks one cell within the row, not the whole
			// row: ">" already says which row is selected, so this is the
			// only signal for which profile within it.
			if row == m.row && col == m.col {
				cell = "[" + cell + "]"
			}
			cells = append(cells, cell)
		}
		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}

	// Flush into a strings.Builder cannot fail: Write never returns an
	// error, so there is nothing for a caller to do with one here, and
	// View must stay a plain string, no error, to stay pure.
	_ = w.Flush()
	return b.String()
}

func truncateName(s string) string {
	runes := []rune(s)
	if len(runes) <= nameColumnWidth {
		return s
	}
	return string(runes[:nameColumnWidth-1]) + "…"
}
