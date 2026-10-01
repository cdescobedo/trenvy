package manage

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// Run drives m in the terminal's alternate screen until it quits.
func Run(m Model) (Model, error) {
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return m, fmt.Errorf("run interface: %w", err)
	}
	fm, ok := final.(Model)
	if !ok {
		return m, fmt.Errorf("run interface: unexpected final model %T", final)
	}
	return fm, nil
}
