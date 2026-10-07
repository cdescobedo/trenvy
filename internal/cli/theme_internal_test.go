package cli

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
