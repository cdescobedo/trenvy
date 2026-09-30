package style_test

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cdescobedo/trenvy/internal/style"
)

// forced returns a Styler that really emits colour. Under `go test` the
// global lipgloss renderer resolves to termenv.Ascii and strips every escape,
// so without this no test could tell a styled token from a plain one.
//
// termenv.WithProfile() as a NewRenderer option does NOT work -- verified
// 2026-09-30, it returns the string unstyled. SetColorProfile is the only
// call that takes effect.
func forced() style.Styler {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.TrueColor)
	return style.New(r)
}

func TestTokensCarryColourWhenTheProfileAllowsIt(t *testing.T) {
	s := forced()
	for name, got := range map[string]string{
		"Missing":  s.Missing("MISSING"),
		"Stored":   s.Stored("stored"),
		"Optional": s.Optional("optional"),
		"Override": s.Override("override"),
		"Stale":    s.Stale("STALE"),
		"Foreign":  s.Foreign("FOREIGN"),
	} {
		if !strings.Contains(got, "\x1b[") {
			t.Errorf("%s emitted no escape: %q", name, got)
		}
	}
}

// Colour is additive. Every method must return its input unchanged apart from
// escapes -- a token that loses a character stops reading under NO_COLOR.
func TestTokensPreserveTheirTextExactly(t *testing.T) {
	s := forced()
	for _, word := range []string{"MISSING", "stored", "optional", "STALE", "unknown (--quick)"} {
		if got := lipgloss.NewStyle().Render(s.Missing(word)); !strings.Contains(got, word) {
			t.Errorf("Missing(%q) lost its text: %q", word, got)
		}
	}
}

// The default renderer under `go test` is Ascii, which is what keeps the 126
// existing strings.Contains assertions in internal/ui working untouched.
func TestDefaultStylerEmitsNoEscapes(t *testing.T) {
	s := style.New(lipgloss.NewRenderer(io.Discard))
	if got := s.Missing("MISSING"); got != "MISSING" {
		t.Errorf("default Styler coloured its input: %q", got)
	}
}

// The zero Styler is load-bearing: every existing test in internal/ui and
// internal/cli builds its fixture without one, so if the zero value ever
// panics or starts colouring, those suites break in bulk.
func TestZeroStylerIsSafeAndPlain(t *testing.T) {
	var z style.Styler
	if got := z.Missing("MISSING"); got != "MISSING" {
		t.Errorf("zero Styler changed its input: %q", got)
	}
	if got := z.Stale("STALE"); got != "STALE" {
		t.Errorf("zero Styler changed its input: %q", got)
	}
	if got := z.Panel("Variables", "body", 40); got != "body" {
		t.Errorf("zero Styler.Panel should pass the body through: %q", got)
	}
}

// shared is the common case and earns no ink, per the spec's colour table.
func TestSharedIsUnstyled(t *testing.T) {
	if got := forced().Shared("shared"); got != "shared" {
		t.Errorf("Shared should not colour: %q", got)
	}
}

// panel rebuilds its top edge as a plain string to splice the title in, so any
// styling on the border would survive on the sides and vanish from the top.
// Whatever Panel does about colour, every line must agree.
func TestPanelEdgeAndSidesAgreeOnColour(t *testing.T) {
	lines := strings.Split(forced().Panel("Title", "body", 20), "\n")
	top := strings.Contains(lines[0], "\x1b[")
	for i, l := range lines {
		if got := strings.Contains(l, "\x1b["); got != top {
			t.Errorf("line %d styled=%v but the top edge styled=%v: %q", i, got, top, l)
		}
	}
}
