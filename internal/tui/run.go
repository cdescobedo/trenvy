package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// host adapts Model to tea.Model and performs the one piece of I/O the state
// machine asks for: fetching a secret when a row is revealed.
type host struct {
	m      Model
	reveal func(name string) (string, error)
}

func (h host) Init() tea.Cmd { return nil }

func (h host) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if req, ok := msg.(RevealRequestMsg); ok {
		name := req.Name
		return h, func() tea.Msg {
			v, err := h.reveal(name)
			if err != nil {
				// Send nothing on failure — including a genuinely missing
				// secret (store.ErrNotFound). RevealedMsg unconditionally
				// marks the row Revealed, and display() shows Revealed rows
				// verbatim, so turning an error into RevealedMsg{Value: ""}
				// would render a blank field instead of the true MISSING /
				// dots state. A nil message is dropped by the event loop
				// without reaching Update, so the row is left exactly as it
				// was: un-revealed.
				return nil
			}
			return RevealedMsg{Name: name, Value: v}
		}
	}
	if _, ok := msg.(SaveMsg); ok {
		return h, tea.Quit
	}
	next, cmd := h.m.Update(msg)
	h.m = next
	return h, cmd
}

func (h host) View() string { return h.m.View() }

// Run drives the model to completion and returns its final state.
func Run(m Model, reveal func(string) (string, error)) (Model, error) {
	p := tea.NewProgram(host{m: m, reveal: reveal})
	final, err := p.Run()
	if err != nil {
		return m, fmt.Errorf("run tui: %w", err)
	}
	h, ok := final.(host)
	if !ok {
		return m, fmt.Errorf("run tui: unexpected final model %T", final)
	}
	return h.m, nil
}
