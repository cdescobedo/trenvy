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
	if m.mx == nil {
		return "No variables defined.\n"
	}
	names := m.visibleNames()
	if len(names) == 0 {
		if m.filter != "" {
			return fmt.Sprintf("No variables match filter %q.\n", m.filter)
		}
		return "No variables defined.\n"
	}

	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)

	header := append([]string{"", "NAME", ""}, m.mx.Profiles...)
	fmt.Fprintln(w, strings.Join(header, "\t"))

	for row, name := range names {
		cursor := " "
		if row == m.row {
			cursor = ">"
		}
		secretWord := ""
		if m.mx.Vars[name].Secret {
			secretWord = "secret"
		}
		nameCell := truncateName(name)
		if m.isDeleted(name) {
			// Marked, not removed: the user is staging an intention they can
			// still abandon with q, and a row that vanishes gives them
			// nothing to change their mind about.
			nameCell += " (deleted)"
		}
		cells := []string{cursor, nameCell, secretWord}

		for col, profile := range m.mx.Profiles {
			// cellText, not mx.Cell directly: a staged edit must be visible
			// the instant it is staged, not only after w writes it to disk.
			cell := m.cellText(name, profile)
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

	switch m.mode {
	case ModeEditValue:
		fmt.Fprintf(&b, "\n value: %s_\n", m.buffer)
	case ModeEditSecret:
		// The literal buffer must never appear here: this line, not just the
		// grid, is part of View(), and a secret typed in plain sight defeats
		// masking as surely as printing it in the matrix would, with the
		// added risk of surviving in terminal scrollback. Asterisks by count
		// give typing feedback without the content.
		fmt.Fprintf(&b, "\n secret value: %s_\n", strings.Repeat("*", len(m.buffer)))
	case ModeAddName:
		fmt.Fprintf(&b, "\n new variable name: %s_\n", m.buffer)
	case ModeFilter:
		fmt.Fprintf(&b, "\n filter: %s_\n", m.buffer)
	}

	return b.String()
}

// viewSwitcher renders the panel `p` opens: every registered project, the
// active one marked, and a cursor moved with j/k before enter chooses one.
// It stands alone, replacing the tab bar and the active tab's body, rather
// than folding into one tab's view the way edit/add/filter do -- unlike
// those, the switcher is not scoped to Variables, so there is no single tab
// body it belongs inside.
//
// Register and remove are deliberately absent, here and from the trailing
// help line: they stay on `trenvy project` (init, rm) as the user's own
// decision, on the grounds that they are rare and destructive enough that an
// in-UI shortcut here would make removing a project easier than the risk
// warrants.
func (m Model) viewSwitcher() string {
	var b strings.Builder
	b.WriteString("Switch project\n\n")

	if len(m.projects) <= 1 {
		b.WriteString("Only this one project is registered -- there is nothing to switch to.\n\n")
		b.WriteString("press esc to go back\n")
		return b.String()
	}

	for i, id := range m.projects {
		cursor := " "
		if i == m.switchRow {
			cursor = ">"
		}
		marker := ""
		if id == m.project {
			marker = " (current)"
		}
		fmt.Fprintf(&b, "%s %s%s\n", cursor, id, marker)
	}

	if m.mode == ModeConfirmSwitch {
		fmt.Fprintf(&b, "\nswitching discards this session's staged changes -- they exist "+
			"only in memory and cannot be recovered afterward. switch to %q anyway? [y/N]\n",
			m.pendingSwitch)
		return b.String()
	}

	b.WriteString("\npress enter to switch, esc to cancel\n")
	return b.String()
}

func truncateName(s string) string {
	runes := []rune(s)
	if len(runes) <= nameColumnWidth {
		return s
	}
	return string(runes[:nameColumnWidth-1]) + "…"
}
