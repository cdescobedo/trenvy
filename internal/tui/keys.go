package tui

// Mode is the state machine's current input mode.
type Mode int

const (
	ModeBrowse Mode = iota
	ModeEditValue
	ModeAddName
	ModeFilter
	ModeConfirmQuit
)

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
	{"t", "targets"},
	{"r", "reveal"},
	{"a", "add"},
	{"d", "delete"},
	{"/", "filter"},
	{"w", "write"},
	{"q", "quit"},
}
