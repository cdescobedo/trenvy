// Package termcolor asks a terminal for its colours and reads the answers:
// the background (OSC 11), the foreground (OSC 10) and the 16-colour
// palette (OSC 4), followed by DA1 (CSI c), which every terminal answers.
// A DA1 reply with no colour reply before it means the terminal does not
// support the query, so nobody waits out a timeout. It has no UI imports.
package termcolor

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type RGB struct{ R, G, B uint8 }

func (c RGB) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

func (c RGB) Luminance() float64 {
	ch := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

func Contrast(a, b RGB) float64 {
	la, lb := a.Luminance(), b.Luminance()
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func Mix(a, b RGB, t float64) RGB {
	m := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return RGB{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B)}
}

type Reply struct {
	Background, Foreground       RGB
	HasBackground, HasForeground bool
	Palette                      [16]RGB
	HasPalette                   [16]bool
	// Notifies: the terminal supports DEC mode 2031 (it announces
	// dark/light changes), from its DECRQM reply.
	Notifies bool
}

// Dark judges by what is on screen. Never by CSI ? 996 n: Ghostty answers
// that with the OS appearance, not its theme.
func (r Reply) Dark() bool {
	return Contrast(r.Background, RGB{}) < Contrast(r.Background, RGB{255, 255, 255})
}

// Complete is what a theme needs: background, foreground and palette 1-5.
func (r Reply) Complete() bool {
	if !r.HasBackground || !r.HasForeground {
		return false
	}
	for i := 1; i <= 5; i++ {
		if !r.HasPalette[i] {
			return false
		}
	}
	return true
}

func (r Reply) Merge(o Reply) Reply {
	if o.HasBackground {
		r.Background, r.HasBackground = o.Background, true
	}
	if o.HasForeground {
		r.Foreground, r.HasForeground = o.Foreground, true
	}
	for i := range o.Palette {
		if o.HasPalette[i] {
			r.Palette[i], r.HasPalette[i] = o.Palette[i], true
		}
	}
	r.Notifies = r.Notifies || o.Notifies
	return r
}

func QueryBytes() []byte {
	var b strings.Builder
	b.WriteString("\x1b]11;?\x1b\\\x1b]10;?\x1b\\")
	for i := 0; i < 16; i++ {
		fmt.Fprintf(&b, "\x1b]4;%d;?\x1b\\", i)
	}
	b.WriteString("\x1b[?2031$p\x1b[c")
	return []byte(b.String())
}

var (
	oscRe    = regexp.MustCompile(`\x1b\](10|11|4;([0-9]+));([^\x07\x1b]*)(?:\x07|\x1b\\)`)
	da1Re    = regexp.MustCompile(`\x1b\[\?[0-9;]*c`)
	notifyRe = regexp.MustCompile(`\x1b\[\?997;([12])n`)
	decrqmRe = regexp.MustCompile(`\x1b\[\?2031;([0-9])\$y`)
)

func Parse(b []byte) (Reply, bool) {
	var rep Reply
	for _, m := range oscRe.FindAllSubmatch(b, -1) {
		c, ok := parseColor(string(m[3]))
		if !ok {
			continue
		}
		switch {
		case string(m[1]) == "11":
			rep.Background, rep.HasBackground = c, true
		case string(m[1]) == "10":
			rep.Foreground, rep.HasForeground = c, true
		default:
			if i, err := strconv.Atoi(string(m[2])); err == nil && i >= 0 && i < 16 {
				rep.Palette[i], rep.HasPalette[i] = c, true
			}
		}
	}
	if m := decrqmRe.FindSubmatch(b); m != nil {
		rep.Notifies = m[1][0] >= '1' && m[1][0] <= '3'
	}
	return rep, da1Re.Match(b)
}

func ParseNotify(b []byte) (dark, ok bool) {
	m := notifyRe.FindSubmatch(b)
	if m == nil {
		return false, false
	}
	return string(m[1]) == "1", true
}

func parseColor(s string) (RGB, bool) {
	if strings.HasPrefix(s, "#") {
		if len(s) != 7 {
			return RGB{}, false
		}
		v, err := strconv.ParseUint(s[1:], 16, 32)
		if err != nil {
			return RGB{}, false
		}
		return RGB{uint8(v >> 16), uint8(v >> 8), uint8(v)}, true
	}
	var body string
	switch {
	case strings.HasPrefix(s, "rgb:"):
		body = s[4:]
	case strings.HasPrefix(s, "rgba:"):
		body = s[5:]
	default:
		return RGB{}, false
	}
	parts := strings.Split(body, "/")
	if len(parts) < 3 {
		return RGB{}, false
	}
	var ch [3]uint8
	for i := 0; i < 3; i++ {
		p := parts[i]
		if len(p) < 1 || len(p) > 4 {
			return RGB{}, false
		}
		v, err := strconv.ParseUint(p, 16, 32)
		if err != nil {
			return RGB{}, false
		}
		max := math.Pow(16, float64(len(p))) - 1
		ch[i] = uint8(math.Round(float64(v) / max * 255))
	}
	return RGB{ch[0], ch[1], ch[2]}, true
}
