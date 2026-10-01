package manage

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// overlay is a panel drawn over the body: an edit field, the . menu, the
// save review, a confirmation, the key list. Only the top one gets keys.
// esc always closes the top overlay and does nothing else (spec §1); the
// Model handles esc before an overlay sees it.
//
// Overlays are pointers, updated in place. A Model value is copied on
// every Update, and those copies share the overlays; nothing reads an old
// Model after Update returns, so the sharing is harmless.
type overlay interface {
	update(m *Model, k tea.KeyMsg) (done bool, cmd tea.Cmd)
	view(m *Model, w, h int) string
	hints(m *Model) []hint
}

// panel is how every overlay draws: a title, a blank line, the body, in a
// focused pane no wider than 76 columns and no taller than h.
func (m *Model) panel(title string, body []string, w, h int) string {
	return m.panelAt(title, body, 0, w, h)
}

// panelAt is panel scrolled so body line focus stays visible.
func (m *Model) panelAt(title string, body []string, focus, w, h int) string {
	pw := min(w, 76)
	lines := append([]string{m.th.Title.Render(title), ""}, body...)
	ph := min(h, len(lines)+2)
	return m.th.Pane(strings.Join(window(lines, focus+2, ph-2), "\n"), pw, ph, true)
}

// choice is one answer a confirmOverlay offers.
type choice struct {
	key, label string
	run        func(m *Model) tea.Cmd
}

// confirmOverlay asks one question. esc is always "no" and needs no choice.
type confirmOverlay struct {
	title, text string
	choices     []choice
}

func (c *confirmOverlay) update(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	for _, ch := range c.choices {
		if keyIs(k, ch.key) {
			return true, ch.run(m)
		}
	}
	return false, nil
}

func (c *confirmOverlay) view(m *Model, w, h int) string {
	body := strings.Split(m.th.Text.Width(min(w, 76)-4).Render(c.text), "\n")
	return m.panel(c.title, body, w, h)
}

func (c *confirmOverlay) hints(*Model) []hint {
	var hs []hint
	for _, ch := range c.choices {
		hs = append(hs, hint{ch.key, ch.label})
	}
	return append(hs, hint{"esc", "cancel"})
}

// helpOverlay is the full key list. It wraps rather than truncates (spec
// diagnosis #11: the old list was cut off at 110 columns), and on a short
// terminal it scrolls, saying which way there is more (spec §1: "never
// truncates").
type helpOverlay struct{ off int }

func (o *helpOverlay) update(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	w, h := m.size()
	_, _, maxOff := o.layout(m, w, h-chromeLines)
	switch {
	case keyIs(k, "j", "down"):
		o.off = clamp(o.off+1, 0, maxOff)
	case keyIs(k, "k", "up"):
		o.off = clamp(o.off-1, 0, maxOff)
	}
	return keyIs(k, "?"), nil
}

// layout is the help body for a w x h body area, how many of its lines fit
// in the panel, and the furthest it can scroll.
func (o *helpOverlay) layout(m *Model, w, h int) (body []string, rows, maxOff int) {
	descW := min(w, 76) - 4 - 10
	for _, v := range verbs {
		desc := strings.Split(m.th.KeyDesc.Width(descW).Render(v.desc), "\n")
		for i, d := range desc {
			k := ""
			if i == 0 {
				k = v.key
			}
			body = append(body, lipgloss.JoinHorizontal(lipgloss.Top, m.th.Key.Width(10).Render(k), d))
		}
	}
	// Two borders, the title and the blank line under it.
	rows = max(0, min(h, len(body)+4)-4)
	// Scrolling needs a row for an indicator and one for the body. minHeight
	// keeps rows above that; below it the pane simply cuts.
	if len(body) <= rows || rows < 2 {
		return body, rows, 0
	}
	// Scrolled to the end, the first row is "↑ more" and the rest is body.
	return body, rows, max(0, len(body)-(rows-1))
}

func (o *helpOverlay) view(m *Model, w, h int) string {
	body, rows, maxOff := o.layout(m, w, h)
	if maxOff == 0 {
		return m.panel("Keys", body, w, h)
	}
	off := clamp(o.off, 0, maxOff) // a resize can shrink maxOff
	var shown []string
	n := rows
	if off > 0 {
		shown = append(shown, m.th.Muted.Render("↑ more"))
		n--
	}
	more := off+n < len(body)
	if more {
		n--
	}
	if n < 1 && off > 0 { // too short for both: a verb and ↓ more win
		shown, n = nil, n+1
	}
	shown = append(shown, body[off:off+max(0, n)]...)
	if more {
		shown = append(shown, m.th.Muted.Render("↓ more"))
	}
	return m.panel("Keys", shown, w, h)
}

func (o *helpOverlay) hints(m *Model) []hint {
	w, h := m.size()
	if _, _, maxOff := o.layout(m, w, h-chromeLines); maxOff > 0 {
		return []hint{{"↑↓ j k", "scroll"}, {"esc", "close"}}
	}
	return []hint{{"esc", "close"}}
}
