package style_test

import (
	"io"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cdescobedo/trenvy/internal/style"
)

// Colour is additive: the Ascii profile must lose escapes and nothing else.
//
// This deliberately does not assert that NO_COLOR is honoured. A renderer over
// io.Discard is Ascii whether or not NO_COLOR is set, so such a test would
// measure that io.Discard is not a terminal. The NO_COLOR guarantee is checked
// against a real binary instead (see the restyle task report).
func TestAsciiProfileKeepsEveryWord(t *testing.T) {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.Ascii)
	s := style.New(r)
	for _, word := range []string{"MISSING", "stored", "optional", "STALE", "FOREIGN", "unknown (--quick)"} {
		if got := s.Missing(word); got != word {
			t.Errorf("Ascii profile changed %q into %q", word, got)
		}
	}
}
