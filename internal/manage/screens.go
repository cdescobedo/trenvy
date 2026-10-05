package manage

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// screen is one of the interface's screens (spec §1). Phase 3 builds all three.
type screen int

const (
	scrVariables screen = iota
	scrOutputs
	scrProfiles
	screenCount // sentinel: keep last
)

func (s screen) title() string {
	switch s {
	case scrOutputs:
		return "Outputs"
	case scrProfiles:
		return "Profiles"
	}
	return "Variables"
}

// switchScreen handles 1, 2, tab and shift+tab. It reports whether the key
// was a screen switch. Switching keeps each screen's cursor where it was.
func (m *Model) switchScreen(k tea.KeyMsg) bool {
	switch {
	case keyIs(k, "1"):
		m.scr = scrVariables
	case keyIs(k, "2"):
		m.scr = scrOutputs
	case keyIs(k, "3"):
		m.scr = scrProfiles
	case keyIs(k, "tab"):
		m.scr = (m.scr + 1) % screenCount
	case keyIs(k, "shift+tab"):
		m.scr = (m.scr + screenCount - 1) % screenCount
	default:
		return false
	}
	return true
}

// viewTabs is the tab row. The active tab is bracketed outside its style:
// a zero Theme renders TabActive and TabInactive alike, and the active
// screen must still read without colour.
func (m Model) viewTabs(w int) string {
	var parts []string
	for s := scrVariables; s < screenCount; s++ {
		label := s.title()
		if s == m.scr {
			parts = append(parts, "["+m.th.TabActive.Render(label)+"]")
		} else {
			parts = append(parts, m.th.TabInactive.Render(label))
		}
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(" " + strings.Join(parts, " "))
}

func (m Model) screenKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.scr {
	case scrOutputs:
		return m.outputsKey(k)
	case scrProfiles:
		return m.profilesKey(k)
	}
	return m.variablesKey(k)
}

func (m Model) screenView(w, h int) string {
	switch m.scr {
	case scrOutputs:
		return m.viewOutputs(w, h)
	case scrProfiles:
		return m.viewProfiles(w, h)
	}
	return m.viewVariables(w, h)
}

func (m Model) screenHints() []hint {
	switch m.scr {
	case scrOutputs:
		return m.outputsHints()
	case scrProfiles:
		return m.profilesHints()
	}
	return m.variablesHints()
}
