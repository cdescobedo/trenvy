package style

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/termcolor"
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
// file in the tree allowed to hold a colour literal. Palette C is one
// palette; ThemeFromTerminal builds the second from the terminal's own
// colours, and both go through the palette struct below.
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

// palette is every role's swatch. Palette C is one; ThemeFromTerminal
// builds another from the terminal's colours. NewTheme and ContrastPairs
// read through it so both palettes share one role table and one contrast
// table.
type palette struct {
	title, text, muted, accent, danger, ok, warn, staged   Swatch
	terminal, header, selection, border                    Swatch
	pillDangerFG, pillDangerBG, pillStagedFG, pillStagedBG Swatch
}

var paletteC = palette{
	title: swTitle, text: swText, muted: swMuted, accent: swAccent,
	danger: swDanger, ok: swOK, warn: swWarn, staged: swStaged,
	terminal: swTerminal, header: swHeader, selection: swSelection, border: swBorder,
	pillDangerFG: swPillDangerFG, pillDangerBG: swPillDangerBG,
	pillStagedFG: swPillStagedFG, pillStagedBG: swPillStagedBG,
}

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
func ContrastPairs() []ContrastPair { return pairsFor(paletteC) }

func pairsFor(p palette) []ContrastPair {
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
	add("terminal", p.terminal,
		fg{"title", p.title}, fg{"text", p.text}, fg{"muted", p.muted}, fg{"accent", p.accent},
		fg{"danger", p.danger}, fg{"ok", p.ok}, fg{"warn", p.warn}, fg{"staged", p.staged})
	add("header", p.header, fg{"title", p.title}, fg{"text", p.text}, fg{"muted", p.muted}, fg{"accent", p.accent})
	add("selection", p.selection, fg{"title", p.title}, fg{"muted", p.muted}, fg{"accent", p.accent})
	ps = append(ps,
		ContrastPair{Name: "danger pill", FG: p.pillDangerFG, BG: p.pillDangerBG},
		ContrastPair{Name: "staged pill", FG: p.pillStagedFG, BG: p.pillStagedBG})
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
func NewTheme(r *lipgloss.Renderer) Theme { return newTheme(r, paletteC) }

func newTheme(r *lipgloss.Renderer, p palette) Theme {
	fg := func(s Swatch) lipgloss.Style { return r.NewStyle().Foreground(s.adaptive()) }
	pill := func(f, b Swatch) lipgloss.Style {
		return r.NewStyle().Bold(true).Foreground(f.adaptive()).Background(b.adaptive()).Padding(0, 1)
	}
	return Theme{
		Title:  fg(p.title).Bold(true),
		Text:   fg(p.text),
		Muted:  fg(p.muted),
		Accent: fg(p.accent).Bold(true),
		Danger: fg(p.danger).Bold(true),
		OK:     fg(p.ok),
		Warn:   fg(p.warn),
		Staged: fg(p.staged).Bold(true),

		Key:     fg(p.accent).Bold(true),
		KeyDesc: fg(p.muted),

		PillDanger: pill(p.pillDangerFG, p.pillDangerBG),
		PillStaged: pill(p.pillStagedFG, p.pillStagedBG),

		Selected:    r.NewStyle().Bold(true).Foreground(p.title.adaptive()).Background(p.selection.adaptive()),
		Header:      r.NewStyle().Background(p.header.adaptive()),
		TabActive:   r.NewStyle().Bold(true).Foreground(p.accent.adaptive()).Background(p.selection.adaptive()).Padding(0, 1),
		TabInactive: fg(p.muted).Padding(0, 1),

		borderFocused: p.accent.adaptive(),
		borderBlurred: p.border.adaptive(),
		r:             r,
	}
}

// ThemeFor applies TRENVY_THEME to r and builds palette C. Detection of the
// terminal's background can guess wrong, and with palette C a wrong guess
// makes text unreadable (spec Risks), so "light" or "dark" overrides it;
// empty, "auto" and "palette" keep detection (the cli asks the terminal for
// its colours first on empty and "auto"; spec 2026-10-06 §4). Anything else
// is an error rather than a guess.
func ThemeFor(r *lipgloss.Renderer, mode string) (Theme, error) {
	switch mode {
	case "", "auto", "palette":
	case "light":
		r.SetHasDarkBackground(false)
	case "dark":
		r.SetHasDarkBackground(true)
	default:
		return Theme{}, fmt.Errorf("TRENVY_THEME=%q: use auto, palette, light or dark, or leave it unset", mode)
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

// ThemeFromTerminal builds a theme from the terminal's own colours (spec
// 2026-10-06 §2): its roles mapped from the reply, then each nudged toward
// white (dark background) or black (light) until it reaches 4.5:1 against
// every surface it is drawn on. ok is false when the reply lacks a colour
// a role needs; the caller then uses palette C for the whole theme.
func ThemeFromTerminal(r *lipgloss.Renderer, rep termcolor.Reply) (Theme, bool) {
	p, ok := terminalPalette(rep)
	if !ok {
		return Theme{}, false
	}
	return newTheme(r, p), true
}

// TerminalContrastPairs is ContrastPairs for the palette built from rep.
func TerminalContrastPairs(rep termcolor.Reply) ([]ContrastPair, bool) {
	p, ok := terminalPalette(rep)
	if !ok {
		return nil, false
	}
	return pairsFor(p), true
}

const minContrast = 4.5

func terminalPalette(rep termcolor.Reply) (palette, bool) {
	if !rep.Complete() {
		return palette{}, false
	}
	// The header and selection surfaces sit a little toward the foreground.
	// Near mid-grey that can leave a surface on the far side of the
	// threshold, where neither white nor black reaches 4.5:1 on all three
	// surfaces, so the shift shrinks until every role fits. At zero shift
	// the surfaces equal the background, and the better of white and black
	// always reaches at least 4.58:1 against any single colour.
	for shift := 12; shift >= 0; shift -= 2 {
		if p, ok := buildTerminalPalette(rep, float64(shift)/100); ok {
			return p, true
		}
	}
	return palette{}, false // unreachable: shift 0 always fits
}

func buildTerminalPalette(rep termcolor.Reply, shift float64) (palette, bool) {
	bg, fg := rep.Background, rep.Foreground
	target := termcolor.RGB{}
	if rep.Dark() {
		target = termcolor.RGB{R: 255, G: 255, B: 255}
	}
	header, selection := termcolor.Mix(bg, fg, shift/2), termcolor.Mix(bg, fg, shift)
	allSurfaces := []termcolor.RGB{bg, header, selection}
	fits := true
	nudge := func(c termcolor.RGB, against ...termcolor.RGB) termcolor.RGB {
		for i := 0; i <= 100; i++ {
			cand := termcolor.Mix(c, target, float64(i)/100)
			passes := true
			for _, s := range against {
				if termcolor.Contrast(cand, s) < minContrast {
					passes = false
					break
				}
			}
			if passes {
				return cand
			}
		}
		fits = false
		return target
	}
	title := fg
	if rep.HasPalette[15] && termcolor.Contrast(rep.Palette[15], bg) >= termcolor.Contrast(fg, bg) {
		title = rep.Palette[15]
	}
	sw := func(c termcolor.RGB) Swatch { return Swatch{c.Hex(), c.Hex()} }
	danger := nudge(rep.Palette[1], bg)
	staged := nudge(rep.Palette[5], bg)
	p := palette{
		title:  sw(nudge(title, allSurfaces...)),
		text:   sw(nudge(fg, allSurfaces...)),
		muted:  sw(nudge(termcolor.Mix(fg, bg, 0.40), allSurfaces...)),
		accent: sw(nudge(rep.Palette[4], allSurfaces...)),
		danger: sw(danger),
		ok:     sw(nudge(rep.Palette[2], bg)),
		warn:   sw(nudge(rep.Palette[3], bg)),
		staged: sw(staged),

		terminal:  sw(bg),
		header:    sw(header),
		selection: sw(selection),
		border:    sw(termcolor.Mix(fg, bg, 0.70)),

		pillDangerFG: sw(bg), pillDangerBG: sw(nudge(danger, bg)),
		pillStagedFG: sw(bg), pillStagedBG: sw(nudge(staged, bg)),
	}
	return p, fits
}
