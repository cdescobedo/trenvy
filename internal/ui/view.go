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

// outputFreshnessCaveat is the one sentence added, never per output, when at
// least one output's freshness could not be verified without reading a
// secret (see Model.outputFreshnessUnverified). Both the Overview's healthy
// line and the Outputs tab need this exact fact in this exact wording -- two
// copies of it could drift the way two implementations of the state itself
// already did once (see outputStates' doc in internal/cli).
const outputFreshnessCaveat = "A secret's value changing since the last render, with the file otherwise untouched, is not checked here -- trenvy status checks it.\n"

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
	if m.filter != "" {
		// A committed filter otherwise leaves no trace on screen once rows
		// are narrowed: they simply vanish, with nothing here saying why --
		// the empty-result message above is the only place that named it,
		// and only when the filter hid EVERY row. / then enter with nothing
		// typed clears it, which is worth saying here since there is no
		// dedicated key for "clear the filter".
		fmt.Fprintf(&b, "filter: %q  (press / then enter with nothing typed to clear)\n\n", m.filter)
	}
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
	case ModeBrowse:
		if reason := m.deleteBlockedReason(); reason != "" {
			fmt.Fprintf(&b, "\n%s\n", reason)
		}
		if reason := m.promoteBlockedReason(); reason != "" {
			fmt.Fprintf(&b, "\n%s\n", reason)
		}
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

// statusLine reports what would be lost by quitting without w. With nothing
// staged there is nothing to save or discard, so it says exactly that rather
// than a count of zero next to keys that would do nothing -- "0 unsaved
// changes" or "w save" over an empty stage reads as a broken prompt, not an
// honest one, the same failure Task 11's one-project switcher panel avoided
// by saying plainly there was nothing to switch to instead of rendering an
// empty list.
func (m Model) statusLine() string {
	n := len(m.staged)
	if n == 0 {
		return "no staged changes"
	}
	plural := "s"
	if n == 1 {
		plural = ""
	}
	return fmt.Sprintf("%d staged change%s -- w save, q discard", n, plural)
}

func truncateName(s string) string {
	runes := []rune(s)
	if len(runes) <= nameColumnWidth {
		return s
	}
	return string(runes[:nameColumnWidth-1]) + "…"
}
