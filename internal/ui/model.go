package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Tab is one of the panels in the management interface's tab bar.
type Tab int

const (
	TabOverview Tab = iota
	TabVariables
	TabProfiles
	TabOutputs

	tabCount // sentinel: keep last, used to wrap tab arithmetic
)

// String names the tab for the bar and for test failure messages.
func (t Tab) String() string {
	switch t {
	case TabOverview:
		return "Overview"
	case TabVariables:
		return "Variables"
	case TabProfiles:
		return "Profiles"
	case TabOutputs:
		return "Outputs"
	}
	return "unknown"
}

// Model is the management interface's state. Update is a pure function:
// state in, state out, no I/O — mirroring internal/tui, this package's
// sibling flat-table model. Any data the model needs from outside (the
// keyring, the registry) arrives as a message; Update never fetches it.
type Model struct {
	project  string
	mx       *Matrix
	problems []Problem
	tab      Tab
}

// New builds a model over one project's matrix and its detected problems.
//
// The signature takes exactly what this task needs. It is expected to grow:
// a later task's project switcher needs the list of registered projects,
// which this package cannot read itself since it does no I/O. That addition
// is deliberately left unresolved here rather than guessed.
func New(project string, mx *Matrix, ps []Problem) Model {
	return Model{project: project, mx: mx, problems: ps, tab: TabOverview}
}

// Tab reports the active tab.
func (m Model) Tab() Tab { return m.tab }

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update advances the state machine.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyTab:
			m.tab = (m.tab + 1) % tabCount
			return m, nil
		case tea.KeyShiftTab:
			m.tab = (m.tab - 1 + tabCount) % tabCount
			return m, nil
		}
		switch msg.String() {
		case "1":
			m.tab = TabOverview
		case "2":
			m.tab = TabVariables
		case "3":
			m.tab = TabProfiles
		case "4":
			m.tab = TabOutputs
		}
		return m, nil
	}
	return m, nil
}

// View renders the tab bar and the active tab's body.
func (m Model) View() string {
	var b strings.Builder

	for t := Tab(0); t < tabCount; t++ {
		marker := " "
		if t == m.tab {
			marker = ">"
		}
		fmt.Fprintf(&b, "%s%d:%s  ", marker, int(t)+1, t)
	}
	b.WriteString("\n\n")

	switch m.tab {
	case TabOverview:
		b.WriteString(m.viewOverview())
	case TabVariables:
		b.WriteString(m.viewVariables())
	case TabProfiles:
		b.WriteString(m.viewProfiles())
	case TabOutputs:
		b.WriteString(m.viewOutputs())
	}

	return b.String()
}

func (m Model) viewOverview() string {
	if len(m.problems) == 0 {
		return "No problems found.\n"
	}
	var b strings.Builder
	for _, p := range m.problems {
		fmt.Fprintf(&b, "%s -- %s\n", p.Detail, p.Fix)
	}
	return b.String()
}

func (m Model) viewVariables() string {
	if m.mx == nil {
		return ""
	}
	return fmt.Sprintf("%d variables across %d profiles.\n", len(m.mx.Names), len(m.mx.Profiles))
}

// viewProfiles is a placeholder: profile management is `trenvy profile`'s job
// today (add, default, list, rm, use), not this tab's, until a later task
// wires it up. Naming that command here rather than leaving the panel empty
// tells the user where to go instead of looking broken.
func (m Model) viewProfiles() string {
	return "Profile management is not built into this view yet.\n" +
		"Use `trenvy profile` (add, default, list, rm, use) from the command line.\n"
}

// viewOutputs mirrors viewProfiles: `trenvy output` (add, default, list,
// rename, rm) already does this job.
func (m Model) viewOutputs() string {
	return "Output management is not built into this view yet.\n" +
		"Use `trenvy output` (add, default, list, rename, rm) from the command line.\n"
}

// KeyRune returns the message for a single rune key press, so callers can
// drive a model without depending on bubbletea's key types. Mirrors
// internal/tui.KeyW.
func KeyRune(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// KeyType returns the message for a non-rune key, e.g. tea.KeyTab.
func KeyType(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
