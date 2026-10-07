package cli

import (
	"io"
	"os"
	"runtime"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/style"
	"github.com/cdescobedo/trenvy/internal/termcolor"
)

const (
	// queryTimeout caps the startup round: a terminal that answers nothing,
	// not even DA1, must not stall startup (spec 2026-10-06 §1).
	queryTimeout = 100 * time.Millisecond
	// pollEvery is the re-query timer, the safety net for a switch the
	// terminal does not announce (spec 2026-10-06 §3).
	pollEvery = 5 * time.Second
)

// How pickTheme reaches the terminal; the tests stand in for it.
var (
	themeGOOS     = runtime.GOOS
	isTerminal    = func(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }
	queryTerminal = termcolor.Query
)

// pickTheme chooses the interface's colours from TRENVY_THEME (spec
// 2026-10-06 §4). Unset or "auto" asks the terminal, when stdin and stdout
// are both terminals, and uses its colours when they build a readable
// theme; then the returned liveTheme follows a switch while the interface
// runs. Windows is asked nothing until the Windows Terminal probe shows how
// replies arrive there (§5): cancelreader cannot always cancel a read on
// the console, so the round could wait for, and eat, the next key. A round the timeout cut short also gets a liveTheme,
// on palette C: the rest of the answer is still coming, and only its
// filter keeps it out of the keys. A terminal that answered without usable
// colours gets palette C and nothing that follows it.
func pickTheme(opts *Options) (style.Theme, *liveTheme, error) {
	mode := os.Getenv("TRENVY_THEME")
	r := lipgloss.NewRenderer(opts.Stdout)
	if mode != "" && mode != "auto" {
		th, err := style.ThemeFor(r, mode)
		return th, nil, err
	}
	stdin, inOK := terminalFile(opts.Stdin)
	stdout, outOK := terminalFile(opts.Stdout)
	if !inOK || !outOK || themeGOOS == "windows" {
		th, err := style.ThemeFor(r, "")
		return th, nil, err
	}
	round, _ := queryTerminal(stdin, stdout, queryTimeout)
	if !round.Done {
		th, err := style.ThemeFor(r, "")
		live := newLiveTheme(r, round.Reply, stdin, stdout)
		live.shown = false
		live.filter.Seed(round.Keys)
		return th, live, err
	}
	if th, ok := terminalTheme(r, round.Reply); ok {
		live := newLiveTheme(r, round.Reply, stdin, stdout)
		live.filter.Seed(round.Keys)
		return th, live, nil
	}
	th, err := style.ThemeFor(r, "")
	return th, keysOnly(stdin, round.Keys), err
}

// terminalFile returns v as a file when it is a terminal.
func terminalFile(v any) (*os.File, bool) {
	f, ok := v.(*os.File)
	if !ok || !isTerminal(f) {
		return nil, false
	}
	return f, true
}

// terminalTheme builds the theme from the terminal's colours and tells r
// which side the background is on, by its luminance (rep.Dark). Left to
// detect, lipgloss would ask the terminal at the first frame and read the
// answer straight off the tty, racing the input filter for the user's keys.
func terminalTheme(r *lipgloss.Renderer, rep termcolor.Reply) (style.Theme, bool) {
	th, ok := style.ThemeFromTerminal(r, rep)
	if ok {
		r.SetHasDarkBackground(rep.Dark())
	}
	return th, ok
}

// liveTheme follows the terminal's colours while the interface runs (spec
// 2026-10-06 §3). One serves every program a command runs (setup, then the
// interface, then each project switch): its Filter outlives them, and
// start and exit bracket each one. A nil *liveTheme is valid and inert.
type liveTheme struct {
	r      *lipgloss.Renderer
	out    io.Writer
	filter *termcolor.Filter
	poll   time.Duration // pollEvery, or 0 over SSH or when the terminal announces changes (spec Risks 3)

	// follows is false for a liveTheme that only hands the startup
	// round's keys to the program (keysOnly): no queries, no handlers.
	follows bool

	mu   sync.Mutex
	send func(tea.Msg)
	last termcolor.Reply
	// shown: the program shows the theme built from last. False on palette
	// C after a round that timed out, until a complete reply arrives.
	shown bool
	stop  func()
}

// newLiveTheme follows from rep, the startup answer. in is the terminal
// (os.Stdin) in production; see termcolor.NewFilter.
func newLiveTheme(r *lipgloss.Renderer, rep termcolor.Reply, in io.Reader, out io.Writer) *liveTheme {
	l := &liveTheme{r: r, out: out, filter: termcolor.NewFilter(in), last: rep, poll: pollEvery, follows: true, shown: true}
	if os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CONNECTION") != "" || rep.Notifies {
		l.poll = 0
	}
	l.filter.SetHandlers(l.onReply, l.onNotify)
	return l
}

// keysOnly carries the keys typed during a round that found no theme to
// the first program, through a filter that does nothing else. nil when
// there are none: the program reads the terminal as it always has.
func keysOnly(in io.Reader, keys []byte) *liveTheme {
	if len(keys) == 0 {
		return nil
	}
	f := termcolor.NewFilter(in)
	f.Seed(keys)
	return &liveTheme{filter: f}
}

// runOptions is how each program runs: reading through the filter, with
// focus reporting, the poll and start/exit around it.
func (l *liveTheme) runOptions() manage.RunOptions {
	if l == nil {
		return manage.RunOptions{}
	}
	if !l.follows {
		return manage.RunOptions{Input: l.filter}
	}
	return manage.RunOptions{Input: l.filter, ReportFocus: true, Poll: l.poll, OnStart: l.start, OnExit: l.exit}
}

// theme is the theme the next program opens on: the terminal's colours as
// last seen, so a switch during setup or before a project switch carries
// over. fallback when l is nil.
func (l *liveTheme) theme(fallback style.Theme) style.Theme {
	if l == nil {
		return fallback
	}
	l.mu.Lock()
	rep, shown := l.last, l.shown
	l.mu.Unlock()
	if !shown {
		return fallback
	}
	if th, ok := terminalTheme(l.r, rep); ok {
		return th
	}
	return fallback
}

// start runs before each program: it asks the terminal to announce
// dark/light switches (DEC mode 2031) and listens for SIGUSR2.
func (l *liveTheme) start(send func(tea.Msg)) {
	l.mu.Lock()
	l.send = send
	l.mu.Unlock()
	_, _ = io.WriteString(l.out, "\x1b[?2031h")
	stop := watchSignal(func() { _ = l.requery() })
	l.mu.Lock()
	l.stop = stop
	l.mu.Unlock()
}

// exit runs after each program and undoes start: the terminal is left as
// found. Clearing send first fences every path into requery (the poll,
// focus, notifications, SIGUSR2): the program has quit and the terminal is
// back in cooked mode, where a reply would echo at the shell prompt.
func (l *liveTheme) exit() {
	l.mu.Lock()
	stop := l.stop
	l.stop, l.send = nil, nil
	l.mu.Unlock()
	_, _ = io.WriteString(l.out, "\x1b[?2031l")
	if stop != nil {
		stop()
	}
}

// requery asks the terminal again, in one Write so the round is never
// interleaved with a frame. The answer arrives through the filter. It asks
// only while a program runs (between start and exit), and holds the lock
// while it writes so exit cannot slip in between the check and the write.
func (l *liveTheme) requery() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.send == nil {
		return nil
	}
	_, err := l.out.Write(termcolor.QueryBytes())
	return err
}

// onReply takes a round's answer from the filter. Only a change that still
// builds a readable theme reaches the program, so an unchanged poll
// redraws nothing.
func (l *liveTheme) onReply(rep termcolor.Reply) {
	l.mu.Lock()
	merged := l.last.Merge(rep)
	if merged == l.last && l.shown {
		l.mu.Unlock()
		return
	}
	th, ok := terminalTheme(l.r, merged)
	if !ok {
		l.mu.Unlock()
		return
	}
	l.last, l.shown = merged, true
	send := l.send
	l.mu.Unlock()
	// Outside the lock: Send waits for the program's event loop.
	if send != nil {
		send(manage.ThemeMsg{Theme: th})
	}
}

// onNotify takes the terminal's announcement of a dark/light switch. The
// announcement does not carry the colours, so it asks for them.
func (l *liveTheme) onNotify(bool) { _ = l.requery() }
