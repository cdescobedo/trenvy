package termcolor

import "testing"

// Recorded from Ghostty 1.3.1 on macOS (Tokyo Night Moon), outside tmux.
const ghosttyMoon = "\x1b]11;rgb:2222/2424/3636\x1b\\\x1b]10;rgb:c8c8/d3d3/f5f5\x1b\\" +
	"\x1b]4;0;rgb:1b1b/1d1d/2b2b\x1b\\\x1b]4;1;rgb:ffff/7575/7f7f\x1b\\\x1b]4;2;rgb:c3c3/e8e8/8d8d\x1b\\" +
	"\x1b]4;3;rgb:ffff/c7c7/7777\x1b\\\x1b]4;4;rgb:8282/aaaa/ffff\x1b\\\x1b]4;5;rgb:c0c0/9999/ffff\x1b\\" +
	"\x1b]4;6;rgb:8686/e1e1/fcfc\x1b\\\x1b]4;7;rgb:8282/8b8b/b8b8\x1b\\\x1b]4;8;rgb:4444/4a4a/7373\x1b\\" +
	"\x1b]4;9;rgb:ffff/7575/7f7f\x1b\\\x1b]4;10;rgb:c3c3/e8e8/8d8d\x1b\\\x1b]4;11;rgb:ffff/c7c7/7777\x1b\\" +
	"\x1b]4;12;rgb:8282/aaaa/ffff\x1b\\\x1b]4;13;rgb:c0c0/9999/ffff\x1b\\\x1b]4;14;rgb:8686/e1e1/fcfc\x1b\\" +
	"\x1b]4;15;rgb:c8c8/d3d3/f5f5\x1b\\\x1b[?2031;2$y\x1b[?62;22c"

// Recorded inside tmux 3.7c after switching Ghostty to Tokyo Night Day:
// a BEL-terminated background and a partial answer.
const tmuxDayPartial = "\x1b]11;rgb:e1e1/e2e2/e7e7\a\x1b]10;rgb:3737/6060/bfbf\x1b\\\x1b]4;1;rgb:f5f5/2a2a/6565\x1b\\"

func TestParseGhosttyMoon(t *testing.T) {
	rep, da1 := Parse([]byte(ghosttyMoon))
	if !da1 {
		t.Fatal("DA1 not seen")
	}
	if !rep.Complete() || rep.Background.Hex() != "#222436" || rep.Foreground.Hex() != "#c8d3f5" {
		t.Fatalf("rep = %+v", rep)
	}
	if rep.Palette[1].Hex() != "#ff757f" || rep.Palette[4].Hex() != "#82aaff" || !rep.HasPalette[15] {
		t.Errorf("palette = %v", rep.Palette)
	}
	if !rep.Dark() {
		t.Error("#222436 is dark")
	}
	if !rep.Notifies {
		t.Error("Ghostty answered DECRQM 2031 with 2 (supported, off)")
	}
}

func TestParsePartialAndBEL(t *testing.T) {
	rep, da1 := Parse([]byte(tmuxDayPartial))
	if da1 || rep.Complete() {
		t.Fatalf("partial reply reported complete or DA1: %+v %v", rep, da1)
	}
	if rep.Background.Hex() != "#e1e2e7" || rep.Dark() || rep.Foreground.Hex() != "#3760bf" || rep.Palette[1].Hex() != "#f52a65" {
		t.Errorf("rep = %+v", rep)
	}
}

func TestParseColorForms(t *testing.T) {
	for in, want := range map[string]string{
		"rgb:f/0/8":                "#ff0088",
		"rgb:ff/00/88":             "#ff0088",
		"rgb:fff/000/888":          "#ff0088",
		"rgb:ffff/0000/8888":       "#ff0088",
		"rgba:ffff/0000/8888/ffff": "#ff0088",
		"#ff0088":                  "#ff0088",
	} {
		c, ok := parseColor(in)
		if !ok || c.Hex() != want {
			t.Errorf("parseColor(%q) = %s %v, want %s", in, c.Hex(), ok, want)
		}
	}
	for _, bad := range []string{"", "rgb:", "rgb:gg/00/00", "rgb:1/2", "red", "#12345"} {
		if _, ok := parseColor(bad); ok {
			t.Errorf("parseColor(%q) accepted", bad)
		}
	}
}

func TestParseIgnoresGarbageAndSilence(t *testing.T) {
	rep, da1 := Parse([]byte("\x1b[?62;22c"))
	if !da1 || rep.HasBackground || rep.HasForeground {
		t.Errorf("DA1-only: %+v %v", rep, da1)
	}
	rep, _ = Parse([]byte("\x1b]11;nonsense\x1b\\\x1b]4;99;rgb:ff/ff/ff\x1b\\junk"))
	if rep.HasBackground || rep.HasPalette != [16]bool{} {
		t.Errorf("garbage parsed: %+v", rep)
	}
}

func TestParseNotify(t *testing.T) {
	if dark, ok := ParseNotify([]byte("\x1b[?997;1n")); !ok || !dark {
		t.Error("997;1 is dark")
	}
	if dark, ok := ParseNotify([]byte("\x1b[?997;2n")); !ok || dark {
		t.Error("997;2 is light")
	}
	if _, ok := ParseNotify([]byte("q")); ok {
		t.Error("q is not a notification")
	}
}

func TestQueryBytesEndsWithDA1(t *testing.T) {
	q := string(QueryBytes())
	if q[:7] != "\x1b]11;?\x1b" || q[len(q)-3:] != "\x1b[c" {
		t.Errorf("QueryBytes = %q", q)
	}
}

func TestContrastAndMix(t *testing.T) {
	black, white := RGB{}, RGB{255, 255, 255}
	if c := Contrast(black, white); c < 20.9 || c > 21.1 {
		t.Errorf("black/white = %.2f", c)
	}
	if m := Mix(black, white, 0.5); m.Hex() != "#808080" {
		t.Errorf("Mix = %s", m.Hex())
	}
}
