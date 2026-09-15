package tui

import (
	"fmt"
	"strings"
)

const (
	nameWidth  = 30
	valueWidth = 44
)

// View renders the table. A secret row shows its plaintext value only when
// Revealed is set — which happens only via the RevealedMsg handler in
// Update, in response to the user pressing r on that row — and otherwise
// shows the dots-in-keyring state or MISSING. Revealed, not a non-empty
// Value, is what distinguishes "the user asked to see this" from a row that
// simply happens to carry a value (e.g. one built directly by a caller): see
// display below and the masking tests in view_test.go, which are a hard
// constraint rather than a formatting preference.
func (m Model) View() string {
	var b strings.Builder

	secrets := 0
	for _, r := range m.rows {
		if r.Secret {
			secrets++
		}
	}
	fmt.Fprintf(&b, " %s\t%d vars · %d secrets\n\n", m.title, len(m.rows), secrets)
	fmt.Fprintf(&b, "   %-*s  SEC  OVR  %s\n", nameWidth, "NAME", "VALUE")

	for i, r := range m.Rows() {
		cursor := "  "
		if i == m.cursor {
			cursor = " >"
		}
		sec := "·"
		if r.Secret {
			sec = "*"
		}
		ovr := "·"
		if r.Overrides > 0 {
			ovr = fmt.Sprintf("[%d]", r.Overrides)
		}

		flags := ""
		if r.Dirty {
			flags += " *"
		}
		if r.Deleted {
			flags += " (del)"
		}

		fmt.Fprintf(&b, "%s %-*s   %s   %-3s  %s%s\n",
			cursor, nameWidth, truncate(r.Name, nameWidth), sec, ovr, display(r), flags)
	}

	switch m.mode {
	case ModeEditValue:
		fmt.Fprintf(&b, "\n value: %s_\n", m.buffer)
	case ModeAddName:
		fmt.Fprintf(&b, "\n new variable name: %s_%s\n", m.buffer, addNameWarning(m))
	case ModeFilter:
		fmt.Fprintf(&b, "\n filter: %s_\n", m.buffer)
	case ModeConfirmQuit:
		fmt.Fprintf(&b, "\n unsaved changes. quit anyway? [y/N]\n")
	default:
		b.WriteString("\n ")
		for _, k := range Keys {
			fmt.Fprintf(&b, "%s %s  ", k.Key, k.Help)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// addNameWarning surfaces the model's silent refusal to add a name that
// already exists in the table. Update drops that add without a trace, because
// a duplicate key would otherwise collapse two variables into one in the
// manifest map on save — but a refusal the user cannot see is a name that
// vanishes as they type it. The warning is derived here, from state Update
// already keeps, so the refusal becomes visible without the state machine
// growing a field to remember it.
func addNameWarning(m Model) string {
	if m.mode != ModeAddName || m.buffer == "" {
		return ""
	}
	for _, r := range m.AllRows() {
		if r.Name == m.buffer {
			return "  (already exists)"
		}
	}
	return ""
}

// display decides what a row shows. A secret shows its Value only once the
// user has explicitly revealed it (Revealed); otherwise dots or MISSING.
func display(r Row) string {
	if r.Secret {
		if r.Revealed {
			return truncate(r.Value, valueWidth)
		}
		if r.Present {
			return "••••••••  (in the keyring)"
		}
		return "——        (MISSING)"
	}
	return truncate(r.Value, valueWidth)
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(runes[:n-1]) + "…"
}
