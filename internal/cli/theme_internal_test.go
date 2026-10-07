package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/style"
	"github.com/cdescobedo/trenvy/internal/termcolor"
)

func TestPickThemeValues(t *testing.T) {
	for _, v := range []string{"", "auto", "palette", "light", "dark"} {
		t.Setenv("TRENVY_THEME", v)
		opts := &Options{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}}
		_, live, err := pickTheme(opts)
		if err != nil || live != nil {
			t.Errorf("%q: live=%v err=%v (not a terminal: no query, no live theme)", v, live, err)
		}
	}
	t.Setenv("TRENVY_THEME", "neon")
	if _, _, err := pickTheme(&Options{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}}); err == nil ||
		!strings.Contains(err.Error(), "auto, palette, light or dark") {
		t.Errorf("bad value: %v", err)
	}
}

func moonReply() termcolor.Reply {
	rep, _ := termcolor.Parse([]byte("\x1b]11;rgb:2222/2424/3636\x1b\\\x1b]10;rgb:c8c8/d3d3/f5f5\x1b\\" +
		"\x1b]4;1;rgb:ffff/7575/7f7f\x1b\\\x1b]4;2;rgb:c3c3/e8e8/8d8d\x1b\\\x1b]4;3;rgb:ffff/c7c7/7777\x1b\\" +
		"\x1b]4;4;rgb:8282/aaaa/ffff\x1b\\\x1b]4;5;rgb:c0c0/9999/ffff\x1b\\"))
	return rep
}

func TestLiveThemeSendsANewThemeOnlyWhenColoursChange(t *testing.T) {
	var out bytes.Buffer
	l := newLiveTheme(lipgloss.NewRenderer(&out), moonReply(), strings.NewReader(""), &out)
	var sent []tea.Msg
	l.start(func(m tea.Msg) { sent = append(sent, m) })
	if !strings.Contains(out.String(), "\x1b[?2031h") {
		t.Errorf("start did not enable mode 2031: %q", out.String())
	}
	l.onReply(moonReply())
	if len(sent) != 0 {
		t.Errorf("an unchanged reply sent %d messages", len(sent))
	}
	day := moonReply()
	day.Background = termcolor.RGB{R: 0xe1, G: 0xe2, B: 0xe7}
	day.Foreground = termcolor.RGB{R: 0x37, G: 0x60, B: 0xbf}
	l.onReply(day)
	if len(sent) != 1 {
		t.Fatalf("sent %d messages after a change, want 1", len(sent))
	}
	if _, ok := sent[0].(manage.ThemeMsg); !ok {
		t.Errorf("sent %T, want manage.ThemeMsg", sent[0])
	}
	out.Reset()
	l.onNotify(false)
	if out.String() != string(termcolor.QueryBytes()) {
		t.Errorf("a notification wrote %q, want one query round", out.String())
	}
	l.exit()
	if !strings.Contains(out.String(), "\x1b[?2031l") {
		t.Errorf("exit did not disable mode 2031: %q", out.String())
	}
}

func TestLiveThemePollsOnlyWithoutAnnouncementsOrSSH(t *testing.T) {
	rep := moonReply()
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CONNECTION", "")
	if l := newLiveTheme(lipgloss.NewRenderer(&bytes.Buffer{}), rep, strings.NewReader(""), &bytes.Buffer{}); l.poll == 0 {
		t.Error("a terminal that does not announce changes should be polled")
	}
	rep.Notifies = true
	if l := newLiveTheme(lipgloss.NewRenderer(&bytes.Buffer{}), rep, strings.NewReader(""), &bytes.Buffer{}); l.poll != 0 {
		t.Error("a terminal that announces changes should not be polled")
	}
	rep.Notifies = false
	t.Setenv("SSH_TTY", "/dev/ttys001")
	if l := newLiveTheme(lipgloss.NewRenderer(&bytes.Buffer{}), rep, strings.NewReader(""), &bytes.Buffer{}); l.poll != 0 {
		t.Error("an SSH session should not be polled")
	}
}

func TestNilLiveThemeIsInert(t *testing.T) {
	var l *liveTheme
	o := l.runOptions()
	if o.Input != nil || o.OnStart != nil || o.OnExit != nil || o.Poll != 0 || o.ReportFocus || l.requery() != nil {
		t.Error("a nil liveTheme must be inert")
	}
}

// The next program in the runManage loop (the interface after setup, or
// after a project switch) must open on the colours the terminal has now,
// not on the ones it had at startup: once a switch is recorded, a later
// identical reply sends nothing.
func TestLiveThemeCarriesTheCurrentThemeToTheNextProgram(t *testing.T) {
	var out bytes.Buffer
	r := lipgloss.NewRenderer(&out)
	l := newLiveTheme(r, moonReply(), strings.NewReader(""), &out)
	startup, _ := style.ThemeFromTerminal(r, moonReply())
	day := moonReply()
	day.Background = termcolor.RGB{R: 0xe1, G: 0xe2, B: 0xe7}
	day.Foreground = termcolor.RGB{R: 0x37, G: 0x60, B: 0xbf}
	l.start(func(tea.Msg) {})
	l.onReply(day)
	l.exit()
	want, _ := style.ThemeFromTerminal(r, day)
	got := l.theme(startup)
	if got.Text.GetForeground() != want.Text.GetForeground() {
		t.Errorf("next program's text colour = %v, want the switched-to %v", got.Text.GetForeground(), want.Text.GetForeground())
	}
	var none *liveTheme
	if g := none.theme(startup); g.Text.GetForeground() != startup.Text.GetForeground() {
		t.Error("a nil liveTheme must hand back the fallback")
	}
}

// A theme built from the terminal's colours tells the renderer which side
// the background is on, so lipgloss never asks the terminal itself (termenv
// reads the reply straight off the tty, racing the input filter for the
// user's keys) once the program runs.
func TestTerminalThemeSetsTheRenderersBackground(t *testing.T) {
	day := moonReply()
	day.Background = termcolor.RGB{R: 0xe1, G: 0xe2, B: 0xe7}
	day.Foreground = termcolor.RGB{R: 0x37, G: 0x60, B: 0xbf}
	r := lipgloss.NewRenderer(&bytes.Buffer{})
	if _, ok := terminalTheme(r, day); !ok {
		t.Fatal("Day did not build a theme")
	}
	if r.HasDarkBackground() {
		t.Error("a light terminal left the renderer on a dark background")
	}
	var out bytes.Buffer
	l := newLiveTheme(lipgloss.NewRenderer(&out), moonReply(), strings.NewReader(""), &out)
	l.start(func(tea.Msg) {})
	l.onReply(day)
	l.exit()
	if l.r.HasDarkBackground() {
		t.Error("a switch to a light terminal left the renderer on a dark background")
	}
}

// After a program exits, nothing may ask the terminal again: the terminal
// is back in cooked mode and its reply would echo at the shell prompt. The
// poll, focus, notification and signal paths all end in requery.
func TestLiveThemeAsksNothingAfterExit(t *testing.T) {
	var out bytes.Buffer
	l := newLiveTheme(lipgloss.NewRenderer(&out), moonReply(), strings.NewReader(""), &out)
	if err := l.requery(); err != nil || out.Len() != 0 {
		t.Errorf("before start: wrote %q, err %v", out.String(), err)
	}
	l.start(func(tea.Msg) {})
	out.Reset()
	if err := l.requery(); err != nil || out.String() != string(termcolor.QueryBytes()) {
		t.Errorf("while running: wrote %q, err %v; want one round", out.String(), err)
	}
	l.exit()
	out.Reset()
	l.onNotify(true)
	if err := l.requery(); err != nil || out.Len() != 0 {
		t.Errorf("after exit: wrote %q, err %v; want nothing", out.String(), err)
	}
}

// fakeTerminal makes pickTheme see two terminals, pipes standing in for
// them, whose startup round comes back as round. Colour is forced: the
// renderer would otherwise find a pipe and fall back to no colour.
func fakeTerminal(t *testing.T, round termcolor.Round) *Options {
	t.Helper()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	inW.Close() // nothing more is typed: reads after the seed end at EOF
	t.Cleanup(func() { inR.Close(); outR.Close(); outW.Close() })
	go func() { _, _ = io.Copy(io.Discard, outR) }()
	t.Setenv("TRENVY_THEME", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_CONNECTION", "")
	oldIs, oldQuery := isTerminal, queryTerminal
	t.Cleanup(func() { isTerminal, queryTerminal = oldIs, oldQuery })
	isTerminal = func(*os.File) bool { return true }
	queryTerminal = func(*os.File, io.Writer, time.Duration) (termcolor.Round, error) { return round, nil }
	return &Options{Stdin: inR, Stdout: outW}
}

// readKeys reads what the program would read first through o's input.
func readKeys(t *testing.T, o manage.RunOptions) string {
	t.Helper()
	if o.Input == nil {
		t.Fatal("no input filter")
	}
	buf := make([]byte, 256)
	n, _ := o.Input.Read(buf)
	return string(buf[:n])
}

func paletteCText() lipgloss.TerminalColor {
	return style.NewTheme(lipgloss.NewRenderer(io.Discard)).Text.GetForeground()
}

// Review F1: a terminal slower than the 100 ms round answers after it. The
// interface must still read through the filter, or that answer is typed
// into the focused field; it opens on palette C, and the keys typed during
// the round come first.
func TestPickThemeFiltersARoundThatTimedOut(t *testing.T) {
	var partial termcolor.Reply
	partial.Background, partial.HasBackground = termcolor.RGB{R: 0x22, G: 0x24, B: 0x36}, true
	opts := fakeTerminal(t, termcolor.Round{Reply: partial, Done: false, Keys: []byte("x")})
	th, live, err := pickTheme(opts)
	if err != nil || live == nil {
		t.Fatalf("live = %v, err %v: a round that timed out still needs the filter", live, err)
	}
	if th.Text.GetForeground() != paletteCText() {
		t.Errorf("text = %v, want palette C until the terminal's colours arrive", th.Text.GetForeground())
	}
	o := live.runOptions()
	if o.OnStart == nil || !o.ReportFocus || o.Poll != pollEvery {
		t.Errorf("run options = %+v, want the live machinery with the poll (Notifies unknown)", o)
	}
	if got := readKeys(t, o); got != "x" {
		t.Errorf("first read = %q, want the key typed during the round", got)
	}
}

// The terminal answered DA1 and its colours build a theme: as before, with
// the round's keys first.
func TestPickThemeFollowsATerminalThatAnswered(t *testing.T) {
	rep := moonReply()
	rep.Notifies = true
	opts := fakeTerminal(t, termcolor.Round{Reply: rep, Done: true, Keys: []byte("y")})
	th, live, err := pickTheme(opts)
	if err != nil || live == nil {
		t.Fatalf("live = %v, err %v", live, err)
	}
	if th.Text.GetForeground() == paletteCText() {
		t.Error("the terminal's colours were not used")
	}
	if got := readKeys(t, live.runOptions()); got != "y" {
		t.Errorf("first read = %q, want the key typed during the round", got)
	}
}

// The terminal answered DA1 without colours that build a theme: it said
// no, so palette C and nothing follows it. Keys typed during the round
// still arrive, through a filter that does nothing else.
func TestPickThemeTakesNoForAnAnswer(t *testing.T) {
	var partial termcolor.Reply
	partial.Background, partial.HasBackground = termcolor.RGB{R: 0x22, G: 0x24, B: 0x36}, true
	th, live, err := pickTheme(fakeTerminal(t, termcolor.Round{Reply: partial, Done: true}))
	if err != nil || live != nil {
		t.Fatalf("live = %v, err %v: want no live theme", live, err)
	}
	if th.Text.GetForeground() != paletteCText() {
		t.Error("want palette C")
	}
	_, live, err = pickTheme(fakeTerminal(t, termcolor.Round{Reply: partial, Done: true, Keys: []byte("z")}))
	if err != nil || live == nil {
		t.Fatalf("live = %v, err %v: the round's keys need a way in", live, err)
	}
	o := live.runOptions()
	if o.OnStart != nil || o.OnExit != nil || o.ReportFocus || o.Poll != 0 {
		t.Errorf("run options = %+v, want the filter alone", o)
	}
	if got := readKeys(t, o); got != "z" {
		t.Errorf("first read = %q, want the key typed during the round", got)
	}
}

// After a round that timed out, the first complete reply (the late round,
// or a re-query) switches the program from palette C to the terminal's
// colours, even when everything but DA1 had already arrived in time.
func TestLiveThemeSwitchesWhenALateRoundCompletes(t *testing.T) {
	opts := fakeTerminal(t, termcolor.Round{Reply: moonReply(), Done: false})
	th, live, err := pickTheme(opts)
	if err != nil || live == nil {
		t.Fatalf("live = %v, err %v", live, err)
	}
	if th.Text.GetForeground() != paletteCText() || live.theme(th).Text.GetForeground() != paletteCText() {
		t.Fatal("a round that timed out must open on palette C")
	}
	var sent []tea.Msg
	live.start(func(m tea.Msg) { sent = append(sent, m) })
	defer live.exit()
	live.onReply(moonReply())
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want the terminal's theme", len(sent))
	}
	msg, ok := sent[0].(manage.ThemeMsg)
	if !ok || msg.Theme.Text.GetForeground() == paletteCText() {
		t.Errorf("sent %+v, want the terminal's colours", sent[0])
	}
	if live.theme(th).Text.GetForeground() == paletteCText() {
		t.Error("the next program would open on palette C")
	}
	live.onReply(moonReply())
	if len(sent) != 1 {
		t.Errorf("an unchanged reply sent again: %d messages", len(sent))
	}
}

// Review F2: on Windows the startup query could leave its read blocked until
// the next key (cancelreader cannot always cancel there) and eat that key.
// Until the Windows Terminal probe shows how replies arrive, nothing is
// asked: palette C, with today's detection, and nothing follows.
func TestPickThemeAsksNothingOnWindows(t *testing.T) {
	rep := moonReply()
	opts := fakeTerminal(t, termcolor.Round{Reply: rep, Done: true})
	asked := false
	queryTerminal = func(*os.File, io.Writer, time.Duration) (termcolor.Round, error) {
		asked = true
		return termcolor.Round{Reply: rep, Done: true}, nil
	}
	oldGOOS := themeGOOS
	t.Cleanup(func() { themeGOOS = oldGOOS })
	themeGOOS = "windows"
	th, live, err := pickTheme(opts)
	if err != nil || asked || live != nil {
		t.Errorf("asked = %v, live = %v, err %v: want no query and no live theme", asked, live, err)
	}
	if th.Text.GetForeground() != paletteCText() {
		t.Error("want palette C")
	}
}

// Review F6: with no colour to show (NO_COLOR, or a terminal without
// colour), there is nothing to follow: no round, no live machinery.
func TestPickThemeAsksNothingWithoutColour(t *testing.T) {
	opts := fakeTerminal(t, termcolor.Round{Reply: moonReply(), Done: true})
	asked := false
	queryTerminal = func(*os.File, io.Writer, time.Duration) (termcolor.Round, error) {
		asked = true
		return termcolor.Round{Reply: moonReply(), Done: true}, nil
	}
	t.Setenv("NO_COLOR", "1")
	_, live, err := pickTheme(opts)
	if err != nil || asked || live != nil {
		t.Errorf("asked = %v, live = %v, err %v: want no query and no live theme", asked, live, err)
	}
}

// Review F7: a reply with a background that still builds no theme says
// which side the terminal is on. Palette C is drawn for that side, and
// lipgloss, told so, never asks the terminal itself mid-program (it would
// read the answer straight off the tty, racing the filter for keys).
func TestPaletteCFallbackTakesTheSideFromTheReply(t *testing.T) {
	render := func(dark bool) string {
		r := lipgloss.NewRenderer(io.Discard)
		r.SetColorProfile(termenv.ANSI)
		r.SetHasDarkBackground(dark)
		return style.NewTheme(r).Text.Render("x")
	}
	if render(true) == render(false) {
		t.Fatal("palette C's text looks the same on both sides; the test cannot tell them apart")
	}
	for _, done := range []bool{true, false} {
		for _, bg := range []termcolor.RGB{{R: 0xe1, G: 0xe2, B: 0xe7}, {R: 0x22, G: 0x24, B: 0x36}} {
			var rep termcolor.Reply
			rep.Background, rep.HasBackground = bg, true
			th, _, err := pickTheme(fakeTerminal(t, termcolor.Round{Reply: rep, Done: done}))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := th.Text.Render("x"), render(rep.Dark()); got != want {
				t.Errorf("done=%v background %s: text = %q, want %q (dark=%v)", done, bg.Hex(), got, want, rep.Dark())
			}
		}
	}
}

// A reply that, even merged with what came before, builds no theme
// changes nothing: the program keeps its theme and is sent nothing.
func TestLiveThemeKeepsItsThemeOnAReplyThatBuildsNone(t *testing.T) {
	var bgOnly, fgOnly termcolor.Reply
	bgOnly.Background, bgOnly.HasBackground = termcolor.RGB{R: 0x22, G: 0x24, B: 0x36}, true
	fgOnly.Foreground, fgOnly.HasForeground = termcolor.RGB{R: 0xc8, G: 0xd3, B: 0xf5}, true
	th, live, err := pickTheme(fakeTerminal(t, termcolor.Round{Reply: bgOnly, Done: false}))
	if err != nil || live == nil {
		t.Fatalf("live = %v, err %v", live, err)
	}
	var sent []tea.Msg
	live.start(func(m tea.Msg) { sent = append(sent, m) })
	defer live.exit()
	live.onReply(fgOnly)
	if len(sent) != 0 {
		t.Errorf("sent %d messages for a reply that builds no theme", len(sent))
	}
	if live.theme(th).Text.GetForeground() != th.Text.GetForeground() {
		t.Error("the next program would not open on the current theme")
	}
}
