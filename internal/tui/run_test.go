package tui

import (
	"errors"
	"strings"
	"testing"
)

// TestHostRevealFailureLeavesRowMasked covers Finding 1 from the round-2
// review: a failed (or genuinely absent) reveal must not render as a blank
// value. Before the fix, any reveal error was turned into
// RevealedMsg{Value: ""}, and since RevealedMsg unconditionally sets
// Revealed, display() would show that empty value instead of MISSING or
// dots. The fix is to send no message at all on failure, leaving the row
// exactly as it was — this is a white-box test (package tui, not tui_test)
// because host is unexported.
func TestHostRevealFailureLeavesRowMasked(t *testing.T) {
	m := New("vpb", []Row{{Name: "ALPHA_TOKEN", Secret: true, Present: false}})
	h := host{m: m, reveal: func(string) (string, error) {
		return "", errors.New("not found")
	}}

	_, cmd := h.Update(RevealRequestMsg{Name: "ALPHA_TOKEN"})
	if cmd == nil {
		t.Fatal("reveal issued no command to fetch the value")
	}

	msg := cmd()
	if msg != nil {
		t.Errorf("a failed reveal produced a message (%#v); it must produce none", msg)
	}

	// host.m is untouched by the RevealRequestMsg branch itself (it only
	// returns a command); the row must still render MISSING.
	if out := h.m.View(); !strings.Contains(out, "MISSING") {
		t.Errorf("row does not still show MISSING after a failed reveal:\n%s", out)
	}
}
