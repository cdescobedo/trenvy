package style

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Swatch is one colour role's value on a light and on a dark background.
type Swatch struct{ Light, Dark string }

func (s Swatch) adaptive() lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: s.Light, Dark: s.Dark}
}

// Palette C (spec 2026-09-30 §0): trenvy's own truecolor palette, chosen
// over the 16-colour theme-following one because the user found that one
// washed out. Every foreground/background pair Theme draws is listed in
// ContrastPairs and checked at >= 4.5:1 by TestThemeContrast; that test,
// not this comment, is what keeps a later change honest. This is the only
// file in the tree allowed to hold a colour literal, so that a later
// "follow the system theme" phase only has to produce these roles.
var (
	swTitle  = Swatch{"#0f172a", "#f1f5f9"}
	swText   = Swatch{"#334155", "#cbd5e1"}
	swMuted  = Swatch{"#556275", "#94a3b8"}
	swAccent = Swatch{"#1d4ed8", "#93c5fd"}
	swDanger = Swatch{"#b91c1c", "#fca5a5"}
	swOK     = Swatch{"#15803d", "#86efac"}
	swWarn   = Swatch{"#92400e", "#fcd34d"}
	swStaged = Swatch{"#7e22ce", "#d8b4fe"}

	// swTerminal is the background the contrast check assumes for text on no
	// surface. Theme never paints it; the terminal's own background shows.
	swTerminal  = Swatch{"#ffffff", "#1e1e1e"}
	swHeader    = Swatch{"#f1f5f9", "#1e293b"}
	swSelection = Swatch{"#e8eef5", "#253247"}
	swBorder    = Swatch{"#cbd5e1", "#334155"}

	swPillDangerFG = Swatch{"#991b1b", "#fecaca"}
	swPillDangerBG = Swatch{"#fee2e2", "#7f1d1d"}
	swPillStagedFG = Swatch{"#6b21a8", "#e9d5ff"}
	swPillStagedBG = Swatch{"#f3e8ff", "#581c87"}
)

// ContrastPair is one foreground Theme draws on one background.
type ContrastPair struct {
	Name   string
	FG, BG Swatch
}

// ContrastPairs lists every text-on-surface combination Theme produces.
// Semantic marks (ok, warn, danger, staged) are never drawn on the
// selection or header surfaces -- the interface places them outside --
// which is why those pairs are absent: green on the light selection is
// 4.29:1 and would fail.
func ContrastPairs() []ContrastPair {
	var ps []ContrastPair
	type fg = struct {
		n  string
		sw Swatch
	}
	add := func(bgName string, bg Swatch, fgs ...fg) {
		for _, f := range fgs {
			ps = append(ps, ContrastPair{Name: f.n + " on " + bgName, FG: f.sw, BG: bg})
		}
	}
	add("terminal", swTerminal,
		fg{"title", swTitle}, fg{"text", swText}, fg{"muted", swMuted}, fg{"accent", swAccent},
		fg{"danger", swDanger}, fg{"ok", swOK}, fg{"warn", swWarn}, fg{"staged", swStaged})
	add("header", swHeader, fg{"title", swTitle}, fg{"text", swText}, fg{"muted", swMuted}, fg{"accent", swAccent})
	add("selection", swSelection, fg{"title", swTitle}, fg{"muted", swMuted}, fg{"accent", swAccent})
	ps = append(ps,
		ContrastPair{Name: "danger pill", FG: swPillDangerFG, BG: swPillDangerBG},
		ContrastPair{Name: "staged pill", FG: swPillStagedFG, BG: swPillStagedBG})
	return ps
}

// Theme is every visual role the interface uses (spec §0's levels). Screens
// reference roles, never colours.
//
// The zero value renders plain text with no escapes, exactly like Styler's,
// so tests that assert on words build a Theme{} and never see colour.
type Theme struct {
	Title, Text, Muted, Accent, Danger, OK, Warn, Staged lipgloss.Style
	Key, KeyDesc                                         lipgloss.Style
	PillDanger, PillStaged                               lipgloss.Style
	Selected, Header, TabActive, TabInactive             lipgloss.Style

	borderFocused, borderBlurred lipgloss.TerminalColor
	r                            *lipgloss.Renderer
}

// NewTheme builds palette C through r. The renderer is injected for the
// same reason Styler's is: the global one resolves Ascii under go test.
func NewTheme(r *lipgloss.Renderer) Theme {
	fg := func(s Swatch) lipgloss.Style { return r.NewStyle().Foreground(s.adaptive()) }
	pill := func(f, b Swatch) lipgloss.Style {
		return r.NewStyle().Bold(true).Foreground(f.adaptive()).Background(b.adaptive()).Padding(0, 1)
	}
	return Theme{
		Title:  fg(swTitle).Bold(true),
		Text:   fg(swText),
		Muted:  fg(swMuted),
		Accent: fg(swAccent).Bold(true),
		Danger: fg(swDanger).Bold(true),
		OK:     fg(swOK),
		Warn:   fg(swWarn),
		Staged: fg(swStaged).Bold(true),

		Key:     fg(swAccent).Bold(true),
		KeyDesc: fg(swMuted),

		PillDanger: pill(swPillDangerFG, swPillDangerBG),
		PillStaged: pill(swPillStagedFG, swPillStagedBG),

		Selected:    r.NewStyle().Bold(true).Foreground(swTitle.adaptive()).Background(swSelection.adaptive()),
		Header:      r.NewStyle().Background(swHeader.adaptive()),
		TabActive:   r.NewStyle().Bold(true).Foreground(swAccent.adaptive()).Background(swSelection.adaptive()).Padding(0, 1),
		TabInactive: fg(swMuted).Padding(0, 1),

		borderFocused: swAccent.adaptive(),
		borderBlurred: swBorder.adaptive(),
		r:             r,
	}
}

// ThemeFor applies TRENVY_THEME to r and builds the theme. Detection of the
// terminal's background can guess wrong, and with palette C a wrong guess
// makes text unreadable (spec Risks), so "light" or "dark" overrides it;
// empty keeps detection. Anything else is an error rather than a guess.
func ThemeFor(r *lipgloss.Renderer, mode string) (Theme, error) {
	switch mode {
	case "":
	case "light":
		r.SetHasDarkBackground(false)
	case "dark":
		r.SetHasDarkBackground(true)
	default:
		return Theme{}, fmt.Errorf("TRENVY_THEME=%q: use light or dark, or leave it unset to detect", mode)
	}
	return NewTheme(r), nil
}

// OnHeader returns s drawn on the header surface. Each header segment is
// rendered separately, and a segment's reset would otherwise end the
// header's background at its first styled word.
func (t Theme) OnHeader(s lipgloss.Style) lipgloss.Style { return s.Inherit(t.Header) }

func (t Theme) newStyle() lipgloss.Style {
	if t.r == nil {
		return lipgloss.NewStyle()
	}
	return t.r.NewStyle()
}

// Pane draws body in a rounded box of exactly width x height cells, borders
// included. Lines are cut to fit rather than wrapped, and extra lines are
// dropped: callers wrap and scroll their own content (internal/manage's
// window), and a pane that grew would push the footer off screen. lipgloss
// v1.1.0 draws borders outside Width/Height and its MaxHeight clips the
// bottom border (both measured 2026-09-30), hence the arithmetic here.
func (t Theme) Pane(body string, width, height int, focused bool) string {
	if width < 3 || height < 3 {
		return ""
	}
	inner := width - 2
	lines := strings.Split(body, "\n")
	if len(lines) > height-2 {
		lines = lines[:height-2]
	}
	cut := t.newStyle().MaxWidth(inner)
	for i, l := range lines {
		lines[i] = cut.Render(l)
	}
	st := t.newStyle().Border(lipgloss.RoundedBorder()).Width(inner).Height(height - 2)
	c := t.borderBlurred
	if focused {
		c = t.borderFocused
	}
	if c != nil {
		st = st.BorderForeground(c)
	}
	return st.Render(strings.Join(lines, "\n"))
}
