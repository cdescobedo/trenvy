package style_test

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cdescobedo/trenvy/internal/style"
)

// luminance is WCAG 2.x relative luminance of a #rrggbb colour.
func luminance(t *testing.T, hex string) float64 {
	t.Helper()
	v, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil {
		t.Fatalf("bad colour %q", hex)
	}
	ch := func(c uint64) float64 {
		s := float64(c) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(v>>16&0xff) + 0.7152*ch(v>>8&0xff) + 0.0722*ch(v&0xff)
}

func ratio(t *testing.T, a, b string) float64 {
	la, lb := luminance(t, a), luminance(t, b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Spec §0: every text role against every surface it is drawn on is at least
// 4.5:1 in both light and dark. A future palette -- including a theme loaded
// from the system later -- has to keep passing this.
func TestThemeContrast(t *testing.T) {
	pairs := style.ContrastPairs()
	if len(pairs) < 17 {
		t.Fatalf("ContrastPairs() has %d pairs; the spec's table has at least 17", len(pairs))
	}
	for _, p := range pairs {
		for mode, fg, bg := "light", p.FG.Light, p.BG.Light; ; mode, fg, bg = "dark", p.FG.Dark, p.BG.Dark {
			if r := ratio(t, fg, bg); r < 4.5 {
				t.Errorf("%s (%s): %s on %s is %.2f:1, want >= 4.5", p.Name, mode, fg, bg, r)
			}
			if mode == "dark" {
				break
			}
		}
	}
}

func TestZeroThemeRendersPlain(t *testing.T) {
	var th style.Theme
	if got := th.Danger.Render("MISSING"); got != "MISSING" {
		t.Errorf("zero Theme Danger = %q, want plain", got)
	}
}

func TestAsciiThemeEmitsNoEscapes(t *testing.T) {
	r := lipgloss.NewRenderer(nil)
	r.SetColorProfile(termenv.Ascii)
	th := style.NewTheme(r)
	out := th.Title.Render("a") + th.Accent.Render("b") + th.PillDanger.Render("c") + th.Pane("x", 10, 4, true)
	if strings.Contains(out, "\x1b[") {
		t.Errorf("Ascii profile emitted an escape: %q", out)
	}
}

func TestTrueColorThemeColoursItsRoles(t *testing.T) {
	r := lipgloss.NewRenderer(nil)
	r.SetColorProfile(termenv.TrueColor)
	r.SetHasDarkBackground(true)
	if got := style.NewTheme(r).Accent.Render("w"); !strings.Contains(got, "\x1b[") {
		t.Errorf("TrueColor Accent = %q, want an escape", got)
	}
}

func TestThemeForAcceptsLightDarkOrEmpty(t *testing.T) {
	for _, mode := range []string{"", "light", "dark"} {
		if _, err := style.ThemeFor(lipgloss.NewRenderer(nil), mode); err != nil {
			t.Errorf("ThemeFor(%q) = %v", mode, err)
		}
	}
	_, err := style.ThemeFor(lipgloss.NewRenderer(nil), "solarized")
	if err == nil || !strings.Contains(err.Error(), "light") || !strings.Contains(err.Error(), "dark") {
		t.Errorf("ThemeFor(solarized) = %v, want an error naming light and dark", err)
	}
}

func TestThemeForForcesTheBackground(t *testing.T) {
	r := lipgloss.NewRenderer(nil)
	if _, err := style.ThemeFor(r, "light"); err != nil {
		t.Fatal(err)
	}
	if r.HasDarkBackground() {
		t.Error("TRENVY_THEME=light left the renderer on a dark background")
	}
}

// Measured 2026-09-30: lipgloss v1.1.0 draws borders outside Width/Height,
// and MaxHeight clips the bottom border. Pane must still come out exactly
// width x height with both borders, whatever the body.
func TestPaneIsExactlyItsSize(t *testing.T) {
	r := lipgloss.NewRenderer(nil)
	r.SetColorProfile(termenv.Ascii)
	th := style.NewTheme(r)
	body := strings.Repeat("a very long line that must not wrap the pane taller\n", 20)
	out := th.Pane(body, 30, 8, false)
	lines := strings.Split(out, "\n")
	if len(lines) != 8 {
		t.Fatalf("Pane has %d lines, want 8:\n%s", len(lines), out)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 30 {
			t.Errorf("line %d is %d wide, want 30: %q", i, w, l)
		}
	}
	if !strings.HasPrefix(lines[7], "╰") {
		t.Errorf("bottom border missing: %q", lines[7])
	}
}
