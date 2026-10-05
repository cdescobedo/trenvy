package style_test

import (
	"io"
	"regexp"
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

type token struct {
	name  string
	apply func(style.Styler, string) string
	sgr   string // exact parameters between "\x1b[" and "m"
}

// tokens is the spec's colour table, one row per styled token. Shared is
// absent on purpose: it earns no ink (TestSharedIsUnstyled).
func tokens() []token {
	return []token{
		{"Missing", style.Styler.Missing, "31"},
		{"Foreign", style.Styler.Foreign, "31"},
		{"Stored", style.Styler.Stored, "32"},
		{"Fresh", style.Styler.Fresh, "32"},
		{"Stale", style.Styler.Stale, "33"},
		{"Modified", style.Styler.Modified, "33"},
		{"SecretMark", style.Styler.SecretMark, "33"},
		{"Override", style.Styler.Override, "36"},
		{"Staged", style.Styler.Staged, "35"},
		{"Optional", style.Styler.Optional, "90"},
		{"Absent", style.Styler.Absent, "90"},
		{"Unknown", style.Styler.Unknown, "90"},
		{"Cursor", style.Styler.Cursor, "7"},
		// lipgloss v1.1.0 writes underline as "4;4" even for a bare
		// Underline(true) (checked against a plain NewStyle), so the
		// doubled 4 is the library's, not a slip in ActiveTab.
		{"ActiveTab", style.Styler.ActiveTab, "1;4;4"},
	}
}

// ansi renders on the 16-colour profile, where base colours "1".."6" and "8"
// become SGR 31-36 and 90 -- the codes the spec's table implies.
func ansi() style.Styler {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI)
	return style.New(r)
}

// Pins the exact escape, not merely that one exists: a wrong colour constant
// must fail here.
func TestEveryTokenEmitsItsExactColour(t *testing.T) {
	s := ansi()
	for _, tok := range tokens() {
		// One character: lipgloss wraps each rune of an underlined string
		// in its own escape, so a longer word would only obscure the code.
		want := "\x1b[" + tok.sgr + "mx\x1b[0m"
		if got := tok.apply(s, "x"); got != want {
			t.Errorf("%s = %q, want %q", tok.name, got, want)
		}
	}
}

// Colour is additive: stripping the escapes must give back the input exactly.
// A token that lost, added or altered a character stops reading under NO_COLOR.
func TestEveryTokenPreservesItsTextExactly(t *testing.T) {
	s := ansi()
	for _, tok := range tokens() {
		for _, word := range []string{"MISSING", "stored", "unknown (--quick)", "a b"} {
			if got := ansiRE.ReplaceAllString(tok.apply(s, word), ""); got != word {
				t.Errorf("%s(%q) text became %q", tok.name, word, got)
			}
		}
	}
}

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

// The default renderer under `go test` is Ascii, which is what keeps the
// existing strings.Contains assertions in other packages working untouched.
func TestDefaultStylerEmitsNoEscapes(t *testing.T) {
	s := style.New(lipgloss.NewRenderer(io.Discard))
	if got := s.Missing("MISSING"); got != "MISSING" {
		t.Errorf("default Styler coloured its input: %q", got)
	}
}

// The zero Styler is load-bearing: every existing test in internal/manage and
// internal/cli builds its fixture without one, so if the zero value ever
// panics or starts colouring, those suites break in bulk.
func TestZeroStylerIsSafeAndPlain(t *testing.T) {
	var z style.Styler
	for _, tok := range tokens() {
		if got := tok.apply(z, "word"); got != "word" {
			t.Errorf("zero Styler.%s changed its input: %q", tok.name, got)
		}
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

// The zero Styler emits no escapes because it carries no style rules, not
// because it lacks a renderer (see Styler's doc). Its one departure from the
// identity is tab expansion, which this pins so the doc's claim is checked.
func TestZeroStylerExpandsTabsAndNothingElse(t *testing.T) {
	if got := (style.Styler{}).Missing("x\ty"); got != "x    y" {
		t.Errorf("zero Styler on a tab = %q, want %q", got, "x    y")
	}
	if got := (style.Styler{}).Missing("MISSING"); got != "MISSING" {
		t.Errorf("zero Styler changed a tab-free string: %q", got)
	}
}
