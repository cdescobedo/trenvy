package tui

import (
	"errors"
	"strings"
	"testing"
)

// A failed reveal -- including one for a secret that is genuinely absent --
// must leave the row masked, not blank. Turning the error into
// RevealedMsg{Value: ""} would be enough to break it: RevealedMsg sets
// Revealed unconditionally, and display() shows a Revealed row verbatim, so
// the row would render as empty rather than MISSING or dots.
//
// White-box (package tui, not tui_test) because host is unexported.
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
