package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// host adapts Model to tea.Model and performs the one piece of I/O the state
// machine asks for: fetching a secret when a cell is revealed. Mirrors
// internal/tui's host, except reveal takes (name, profile) rather than just a
// name: this package's matrix has one cell per profile, so a name alone would
// be ambiguous about which keyring entry a reveal on the "dev" column wants
// versus "prod".
type host struct {
	m      Model
	reveal func(name, profile string) (string, error)
}

func (h host) Init() tea.Cmd { return nil }

func (h host) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if req, ok := msg.(RevealRequestMsg); ok {
		name, profile := req.Name, req.Profile
		return h, func() tea.Msg {
			v, err := h.reveal(name, profile)
			if err != nil {
				// Send nothing on failure -- including a genuinely missing
				// secret (store.ErrNotFound). RevealedMsg unconditionally
				// marks the cell Revealed, and View shows a Revealed cell
				// verbatim, so turning an error into RevealedMsg{Value: ""}
				// would render a blank cell instead of the true MISSING /
				// masked state. A nil message is dropped by the event loop
				// without reaching Update, so the cell is left exactly as it
				// was: un-revealed. See internal/tui's host for the same
				// reasoning over its flat table.
				return nil
			}
			return RevealedMsg{Name: name, Profile: profile, Value: v}
		}
	}
	next, cmd := h.m.Update(msg)
	h.m = next
	return h, cmd
}

func (h host) View() string { return h.m.View() }

// Run drives the model to completion and returns its final state.
func Run(m Model, reveal func(name, profile string) (string, error)) (Model, error) {
	p := tea.NewProgram(host{m: m, reveal: reveal})
	final, err := p.Run()
	if err != nil {
		return m, fmt.Errorf("run ui: %w", err)
	}
	h, ok := final.(host)
	if !ok {
		return m, fmt.Errorf("run ui: unexpected final model %T", final)
	}
	return h.m, nil
}
