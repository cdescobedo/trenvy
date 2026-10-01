package manage

import tea "github.com/charmbracelet/bubbletea"

// hint is one footer entry: a key and what it does right now.
type hint struct{ key, desc string }

// maxFooterHints caps the footer, "? keys" included (spec §1).
const maxFooterHints = 6

// footer caps hs and ends it with "? keys". Callers order hints by
// importance, so the cap drops the least important; ? lists everything.
func footer(hs []hint) []hint {
	if len(hs) > maxFooterHints-1 {
		hs = hs[:maxFooterHints-1]
	}
	return append(hs, hint{"?", "keys"})
}

// verbs is the ? overlay's list: the fixed verbs, the same on every screen
// (spec §1). Phase 1 has one screen, so screen switching is not listed
// until phase 2 adds a second -- a key that does nothing is not a hint.
var verbs = []hint{
	{"↑↓  j k", "move"},
	{"←→  h l", "switch pane"},
	{"enter", "the primary action on the selection"},
	{"a", "add"},
	{"d", "mark deleted, or remove a value (again to unmark)"},
	{"u", "undo the staged change on the selection"},
	{"v", "reveal or hide the value under the cursor"},
	{"/", "filter by name"},
	{"!", "show only rows with problems"},
	{".", "more actions for the selection"},
	{"w", "review and save"},
	{"q", "quit (asks first if anything is unsaved)"},
	{"ctrl+c", "quit now, writing nothing"},
	{"esc", "close the panel on top"},
	{"?", "this list"},
}

// keyIs matches a key by its tea name. A paste's name is bracketed
// ("[text]"), so pasted text never triggers a shortcut.
func keyIs(k tea.KeyMsg, names ...string) bool {
	s := k.String()
	for _, n := range names {
		if s == n {
			return true
		}
	}
	return false
}
