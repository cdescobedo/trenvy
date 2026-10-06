package manage

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/style"
)

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
// (spec §1).
var verbs = []hint{
	{"↑↓  j k", "move"},
	{"←→  h l", "switch pane"},
	{"1 2 3  tab", "switch screen (shift+tab goes back)"},
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

// hintKeys is the keys of hs, for tests' FooterKeys.
func hintKeys(hs []hint) []string {
	var out []string
	for _, h := range hs {
		out = append(out, h.key)
	}
	return out
}

// footerLine draws hs as the footer line, in at most w cells.
func footerLine(th style.Theme, hs []hint, w int) string {
	var parts []string
	for _, h := range hs {
		parts = append(parts, th.Key.Render(h.key)+" "+th.KeyDesc.Render(h.desc))
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(" " + strings.Join(parts, "   "))
}
