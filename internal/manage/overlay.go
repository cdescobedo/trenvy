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
// diagnosis #11: the old list was cut off at 110 columns).
type helpOverlay struct{}

func (helpOverlay) update(_ *Model, k tea.KeyMsg) (bool, tea.Cmd) { return keyIs(k, "?"), nil }

func (helpOverlay) view(m *Model, w, h int) string {
	descW := min(w, 76) - 4 - 10
	var body []string
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
	return m.panel("Keys", body, w, h)
}

func (helpOverlay) hints(*Model) []hint { return []hint{{"esc", "close"}} }
