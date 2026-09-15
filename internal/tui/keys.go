package tui

import tea "github.com/charmbracelet/bubbletea"

// Mode is the state machine's current input mode.
type Mode int

const (
	// ModeBrowse is the resting mode: the cursor moves and each key in Keys
	// acts on the row under it.
	ModeBrowse Mode = iota
	// ModeEditValue is typing a replacement value for the selected row.
	ModeEditValue
	// ModeAddName is typing the name of a row to add.
	ModeAddName
	// ModeFilter is typing the substring that narrows the visible rows.
	ModeFilter
	// ModeConfirmQuit is the question asked when q is pressed with unsaved
	// changes. Every key but y answers no -- except ctrl+c, which cancels.
	ModeConfirmQuit
)

// String names the mode for the status line and for test failure messages.
func (m Mode) String() string {
	switch m {
	case ModeBrowse:
		return "browse"
	case ModeEditValue:
		return "edit"
	case ModeAddName:
		return "add"
	case ModeFilter:
		return "filter"
	case ModeConfirmQuit:
		return "confirm-quit"
	}
	return "unknown"
}

// Keys, as a single table so the help line and the handler cannot drift.
var Keys = []struct{ Key, Help string }{
	{"e", "edit"},
	{"s", "secret"},
	{"r", "reveal"},
	{"a", "add"},
	{"d", "delete"},
	{"/", "filter"},
	{"w", "write"},
	{"q", "quit"},
}

// KeyW returns the message for the write key, so callers can drive a model to
// the saved state without depending on bubbletea's key types.
func KeyW() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")} }
