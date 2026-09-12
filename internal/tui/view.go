package tui

import (
	"fmt"
	"strings"
)

const (
	nameWidth  = 30
	valueWidth = 44
)

// View renders the table. A secret row is always masked: it shows either the
// dots-in-keyring state or MISSING, never the plaintext value, even if the
// row's Value happens to be populated (e.g. by a reveal fetch). Reveal exists
// to make the value available to the state machine (and, from there, the
// process that saves it), not to print it — see display below and the
// masking tests in view_test.go, which are a hard constraint rather than a
// formatting preference.
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

// addNameWarning surfaces the model's silent refusal (Task 24) to add a name
// that already exists in the table. Update refuses that add without a trace,
// because a duplicate key would otherwise silently collapse two variables
// into one in the manifest map on save — but the refusal must not be
// invisible to the user, so the view flags it here using only the model's
// public surface (Mode, Buffer, AllRows). This adds no state to the model and
// changes nothing in Update; the state machine stays pure.
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

// display decides what a row shows. A secret always shows dots or MISSING —
// its Value is never printed, no matter how it got populated.
func display(r Row) string {
	if r.Secret {
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
