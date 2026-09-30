package style_test

import (
	"io"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cdescobedo/trenvy/internal/style"
)

// Colour is additive: the Ascii profile must lose escapes and nothing else,
// for every styled token in the spec's table.
//
// This deliberately does not assert that NO_COLOR is honoured. A renderer over
// io.Discard is Ascii whether or not NO_COLOR is set, so such a test would
// measure that io.Discard is not a terminal. The guarantee is established
// against a real binary, on a project that has a MISSING variable:
//
//	NO_COLOR=1 trenvy status | cat -v | grep -c '\^\['        -> 0
//	CLICOLOR_FORCE=1 trenvy status | cat -v | grep -c '\^\['  -> non-zero
//
// The second command matters: without it, "0 escapes under NO_COLOR" is
// indistinguishable from "this fixture had nothing to colour".
func TestAsciiProfileKeepsEveryWord(t *testing.T) {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.Ascii)
	s := style.New(r)
	for _, tok := range tokens() {
		for _, word := range []string{"MISSING", "stored", "optional", "STALE", "FOREIGN", "unknown (--quick)"} {
			if got := tok.apply(s, word); got != word {
				t.Errorf("Ascii profile changed %s(%q) into %q", tok.name, word, got)
			}
		}
	}
}
