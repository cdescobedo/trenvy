package style_test

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cdescobedo/trenvy/internal/style"
	"github.com/cdescobedo/trenvy/internal/termcolor"
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
	for _, mode := range []string{"", "auto", "palette", "light", "dark"} {
		if _, err := style.ThemeFor(lipgloss.NewRenderer(nil), mode); err != nil {
			t.Errorf("ThemeFor(%q) = %v", mode, err)
		}
	}
	_, err := style.ThemeFor(lipgloss.NewRenderer(nil), "solarized")
	if err == nil || !strings.Contains(err.Error(), "auto, palette, light or dark") {
		t.Errorf("ThemeFor(solarized) = %v, want an error naming auto, palette, light or dark", err)
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

func omarchyReplies(t *testing.T) map[string]termcolor.Reply {
	t.Helper()
	data, err := os.ReadFile("testdata/omarchy-themes.txt")
	if err != nil {
		t.Fatal(err)
	}
	hex := func(s string) termcolor.RGB {
		v, err := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
		if err != nil {
			t.Fatalf("bad colour %q", s)
		}
		return termcolor.RGB{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}
	}
	out := map[string]termcolor.Reply{}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) != 10 || strings.HasPrefix(line, "#") {
			continue
		}
		var rep termcolor.Reply
		rep.Background, rep.HasBackground = hex(f[2]), true
		rep.Foreground, rep.HasForeground = hex(f[3]), true
		for i, idx := range []int{1, 2, 3, 4, 5, 15} { // red green yellow blue magenta bright_foreground
			rep.Palette[idx], rep.HasPalette[idx] = hex(f[4+i]), true
		}
		out[f[0]] = rep
	}
	if len(out) != 22 {
		t.Fatalf("read %d themes, want 22", len(out))
	}
	return out
}

// Review Focus 4: raw theme colours fail 4.5:1 in 94 pairs across these
// themes; the built theme must pass every pair, light and dark alike.
func TestTerminalThemesPassContrast(t *testing.T) {
	reps := omarchyReplies(t)
	synthetic := map[string]termcolor.Reply{}
	for name, bg := range map[string]termcolor.RGB{"black": {}, "white": {R: 255, G: 255, B: 255}, "grey": {R: 128, G: 128, B: 128}} {
		var rep termcolor.Reply
		rep.Background, rep.HasBackground = bg, true
		rep.Foreground, rep.HasForeground = termcolor.RGB{R: 128, G: 128, B: 128}, true
		for i := 1; i <= 5; i++ {
			rep.Palette[i], rep.HasPalette[i] = termcolor.RGB{R: 120, G: 130, B: 140}, true
		}
		synthetic["synthetic-"+name] = rep
	}
	// Near mid-grey with a white foreground, a selection shifted toward the
	// foreground lands where neither white nor black passes; the builder
	// must shrink the shift rather than fail.
	for _, g := range []uint8{100, 104, 108, 112, 116, 120} {
		var rep termcolor.Reply
		rep.Background, rep.HasBackground = termcolor.RGB{R: g, G: g, B: g}, true
		rep.Foreground, rep.HasForeground = termcolor.RGB{R: 255, G: 255, B: 255}, true
		for i := 1; i <= 5; i++ {
			rep.Palette[i], rep.HasPalette[i] = termcolor.RGB{R: 200, G: 120, B: 120}, true
		}
		synthetic[fmt.Sprintf("synthetic-grey-%d-white-fg", g)] = rep
	}
	for name, rep := range synthetic {
		reps[name] = rep
	}
	reps["ghostty-moon-recorded"] = ghosttyMoonReply(t)
	for name, rep := range reps {
		pairs, ok := style.TerminalContrastPairs(rep)
		if !ok {
			t.Errorf("%s: not built", name)
			continue
		}
		if len(pairs) < 17 {
			t.Errorf("%s: %d pairs, want the full table", name, len(pairs))
		}
		for _, p := range pairs {
			if r := ratio(t, p.FG.Dark, p.BG.Dark); r < 4.5 {
				t.Errorf("%s: %s is %.2f:1 (%s on %s)", name, p.Name, r, p.FG.Dark, p.BG.Dark)
			}
			if p.FG.Light != p.FG.Dark || p.BG.Light != p.BG.Dark {
				t.Errorf("%s: %s differs between halves", name, p.Name)
			}
		}
	}
}

func TestThemeFromTerminalNeedsACompleteReply(t *testing.T) {
	var rep termcolor.Reply
	rep.Background, rep.HasBackground = termcolor.RGB{R: 30, G: 30, B: 30}, true
	if _, ok := style.ThemeFromTerminal(lipgloss.NewRenderer(io.Discard), rep); ok {
		t.Error("a reply without a foreground or palette built a theme")
	}
}

func TestThemeFromTerminalUsesTheThemesColours(t *testing.T) {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.TrueColor)
	rep := omarchyReplies(t)["tokyo-night"]
	th, ok := style.ThemeFromTerminal(r, rep)
	if !ok {
		t.Fatal("not built")
	}
	// tokyo-night's foreground #a9b1d6 passes as-is on its background, so text is unchanged.
	if got := th.Text.Render("x"); !strings.Contains(got, "38;2;169;177;214") {
		t.Errorf("text = %q, want the theme's foreground", got)
	}
}

// ghosttyMoon is Ghostty's answer to the round, recorded on Tokyo Night
// Moon (the same recording internal/termcolor's tests parse).
const ghosttyMoon = "\x1b]11;rgb:2222/2424/3636\x1b\\\x1b]10;rgb:c8c8/d3d3/f5f5\x1b\\" +
	"\x1b]4;0;rgb:1b1b/1d1d/2b2b\x1b\\\x1b]4;1;rgb:ffff/7575/7f7f\x1b\\\x1b]4;2;rgb:c3c3/e8e8/8d8d\x1b\\" +
	"\x1b]4;3;rgb:ffff/c7c7/7777\x1b\\\x1b]4;4;rgb:8282/aaaa/ffff\x1b\\\x1b]4;5;rgb:c0c0/9999/ffff\x1b\\" +
	"\x1b]4;6;rgb:8686/e1e1/fcfc\x1b\\\x1b]4;7;rgb:8282/8b8b/b8b8\x1b\\\x1b]4;8;rgb:4444/4a4a/7373\x1b\\" +
	"\x1b]4;9;rgb:ffff/7575/7f7f\x1b\\\x1b]4;10;rgb:c3c3/e8e8/8d8d\x1b\\\x1b]4;11;rgb:ffff/c7c7/7777\x1b\\" +
	"\x1b]4;12;rgb:8282/aaaa/ffff\x1b\\\x1b]4;13;rgb:c0c0/9999/ffff\x1b\\\x1b]4;14;rgb:8686/e1e1/fcfc\x1b\\" +
	"\x1b]4;15;rgb:c8c8/d3d3/f5f5\x1b\\\x1b[?2031;2$y\x1b[?62;22c"

func ghosttyMoonReply(t *testing.T) termcolor.Reply {
	t.Helper()
	rep, da1 := termcolor.Parse([]byte(ghosttyMoon))
	if !da1 || !rep.Complete() {
		t.Fatalf("the Moon recording did not parse: %+v", rep)
	}
	return rep
}

// Each role takes its colour from the terminal's own: accent and keys from
// blue (palette 4), danger red (1), ok green (2), warn yellow (3), staged
// magenta (5), text the foreground. A colour is nudged only when it fails
// 4.5:1, so where the raw colour already passes on every surface the role
// is drawn on, the role is that colour exactly.
func TestThemeFromTerminalMapsRolesOnMoon(t *testing.T) {
	rep := ghosttyMoonReply(t)
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.TrueColor)
	th, ok := style.ThemeFromTerminal(r, rep)
	if !ok {
		t.Fatal("Moon did not build")
	}
	// The surfaces each role is drawn on, from the contrast table.
	pairs, _ := style.TerminalContrastPairs(rep)
	drawnOn := map[string][]string{}
	for _, p := range pairs {
		if role, _, ok := strings.Cut(p.Name, " on "); ok {
			drawnOn[role] = append(drawnOn[role], p.BG.Dark)
		}
	}
	drawnOn["key"] = drawnOn["accent"]
	colour := func(s lipgloss.Style) string {
		c, ok := s.GetForeground().(lipgloss.AdaptiveColor)
		if !ok || c.Light != c.Dark {
			t.Fatalf("foreground %#v is not one terminal colour", s.GetForeground())
		}
		return c.Dark
	}
	if colour(th.Key) != colour(th.Accent) {
		t.Errorf("key %s differs from accent %s", colour(th.Key), colour(th.Accent))
	}
	roles := []struct {
		name  string
		style lipgloss.Style
		raw   termcolor.RGB
	}{
		{"accent", th.Accent, rep.Palette[4]},
		{"key", th.Key, rep.Palette[4]},
		{"danger", th.Danger, rep.Palette[1]},
		{"ok", th.OK, rep.Palette[2]},
		{"warn", th.Warn, rep.Palette[3]},
		{"staged", th.Staged, rep.Palette[5]},
		{"text", th.Text, rep.Foreground},
	}
	checked := 0
	for _, role := range roles {
		if len(drawnOn[role.name]) == 0 {
			t.Fatalf("%s is on no surface in the contrast table", role.name)
		}
		passes := true
		for _, bg := range drawnOn[role.name] {
			if ratio(t, role.raw.Hex(), bg) < 4.5 {
				passes = false
			}
		}
		if !passes {
			t.Logf("%s: raw %s needs a nudge on Moon; not compared", role.name, role.raw.Hex())
			continue
		}
		checked++
		if got := colour(role.style); got != role.raw.Hex() {
			t.Errorf("%s = %s, want the terminal's %s unchanged", role.name, got, role.raw.Hex())
		}
	}
	if checked == 0 {
		t.Error("no role passed unnudged on Moon; the mapping was not checked")
	}
}
