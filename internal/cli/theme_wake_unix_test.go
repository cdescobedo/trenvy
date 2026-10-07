//go:build !windows

package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/cancelreader"

	"github.com/cdescobedo/trenvy/internal/manage"
)

const da1Reply = "\x1b[?62;22c"

// answeringTTY is the terminal's output side: it records what trenvy
// writes and, when trenvy asks for DA1, answers on the input side.
type answeringTTY struct {
	mu     sync.Mutex
	wrote  bytes.Buffer
	answer func()
}

func (a *answeringTTY) Write(p []byte) (int, error) {
	a.mu.Lock()
	a.wrote.Write(p)
	a.mu.Unlock()
	if bytes.Contains(p, []byte("\x1b[c")) && a.answer != nil {
		a.answer()
	}
	return len(p), nil
}

func (a *answeringTTY) String() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.wrote.String()
}

// wakeTerminal is a pipe standing in for the terminal's input, and an
// output that answers a DA1 query with reply.
func wakeTerminal(t *testing.T, reply string) (in *os.File, inW *os.File, out *answeringTTY) {
	t.Helper()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inR.Close(); inW.Close() })
	out = &answeringTTY{answer: func() { _, _ = io.WriteString(inW, reply) }}
	return inR, inW, out
}

// program runs o the way bubbletea does: OnStart, then reads through
// cancelreader.NewReader(o.Input), which waits for the terminal's fd to
// be readable before each Read. next returns the next non-empty read, or
// false after d with none.
func program(t *testing.T, o manage.RunOptions) (next func(d time.Duration) (string, bool)) {
	t.Helper()
	if o.Input == nil {
		t.Fatal("no input filter")
	}
	cr, err := cancelreader.NewReader(o.Input)
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan string, 64)
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := cr.Read(buf)
			if n > 0 {
				ch <- string(buf[:n])
			}
			if err != nil {
				close(ch)
				return
			}
		}
	}()
	if o.OnStart != nil {
		o.OnStart(func(tea.Msg) {})
	}
	t.Cleanup(func() {
		if o.OnExit != nil {
			o.OnExit()
		}
		cr.Cancel()
	})
	return func(d time.Duration) (string, bool) {
		select {
		case s, ok := <-ch:
			return s, ok
		case <-time.After(d):
			return "", false
		}
	}
}

// seeded builds both kinds of liveTheme that carry the round's keys.
func seeded(t *testing.T, in io.Reader, out io.Writer, keys string) map[string]*liveTheme {
	t.Helper()
	live := newLiveTheme(lipgloss.NewRenderer(io.Discard), moonReply(), in, out)
	live.filter.Seed([]byte(keys))
	return map[string]*liveTheme{"live": live, "keysOnly": keysOnly(in, out, []byte(keys))}
}

// (1) bubbletea's reader waits for the terminal's fd, so keys the filter
// already holds would wait for the next keystroke. They must arrive with
// no further input.
func TestSeededKeyArrivesWithNoFurtherInput(t *testing.T) {
	for _, kind := range []string{"live", "keysOnly"} {
		t.Run(kind, func(t *testing.T) {
			in, _, out := wakeTerminal(t, da1Reply)
			next := program(t, seeded(t, in, out, "x")[kind].runOptions())
			if got, ok := next(time.Second); !ok || got != "x" {
				t.Errorf("first read = %q (%v), want the seeded x with no further input; wrote %q", got, ok, out.String())
			}
		})
	}
}

// (2) A seeded esc is the esc key on its own: not merged with the reply
// that wakes the reader, and not with a key typed 300 ms later (that would
// read as alt+j).
func TestSeededEscStaysALoneEsc(t *testing.T) {
	for _, kind := range []string{"live", "keysOnly"} {
		t.Run(kind, func(t *testing.T) {
			in, inW, out := wakeTerminal(t, da1Reply)
			next := program(t, seeded(t, in, out, "\x1b")[kind].runOptions())
			go func() { time.Sleep(300 * time.Millisecond); _, _ = io.WriteString(inW, "j") }()
			var got []string
			for len(got) < 2 {
				s, ok := next(2 * time.Second)
				if !ok {
					break
				}
				got = append(got, s)
			}
			if len(got) != 2 || got[0] != "\x1b" || got[1] != "j" {
				t.Errorf("reads = %q, want [\"\\x1b\" \"j\"]", got)
			}
		})
	}
}

// (3) With nothing seeded, start asks the terminal nothing beyond turning
// on the dark/light announcements.
func TestEmptySeedWritesNoWake(t *testing.T) {
	in, _, out := wakeTerminal(t, da1Reply)
	live := newLiveTheme(lipgloss.NewRenderer(io.Discard), moonReply(), in, out)
	live.start(func(tea.Msg) {})
	time.Sleep(50 * time.Millisecond)
	if got := out.String(); got != "\x1b[?2031h" {
		t.Errorf("start wrote %q, want only \\x1b[?2031h", got)
	}
	live.exit()
	if keysOnly(in, out, nil) != nil {
		t.Error("no keys: want no filter at all")
	}
}

// (4) A paste during the round makes a seed longer than bubbletea's
// 256-byte read. It is scanned like any input: a key at the boundary is not
// split, and the reply cut at its end is joined with the rest of the late
// round, never passed as keys.
func TestLongSeedIsScannedLikeAnyInput(t *testing.T) {
	keys := strings.Repeat("a", 254) + "\x1b[A" + strings.Repeat("b", 32)
	seed := keys + "\x1b]11;rgb:22"
	if len(seed) != 300 {
		t.Fatalf("seed is %d bytes, want 300", len(seed))
	}
	for _, kind := range []string{"live", "keysOnly"} {
		t.Run(kind, func(t *testing.T) {
			// The late round's rest and its DA1, then the wake's DA1.
			in, _, out := wakeTerminal(t, "22/2424/3636\x1b\\"+da1Reply+da1Reply)
			next := program(t, seeded(t, in, out, seed)[kind].runOptions())
			var got []string
			for {
				s, ok := next(500 * time.Millisecond)
				if !ok {
					break
				}
				got = append(got, s)
			}
			whole := false
			for _, r := range got {
				if len(r) > 256 {
					t.Errorf("a read of %d bytes", len(r))
				}
				whole = whole || strings.Contains(r, "\x1b[A")
			}
			if joined := strings.Join(got, ""); joined != keys {
				t.Errorf("keys = %q, want the seed's keys and no reply bytes", joined)
			}
			if !whole {
				t.Errorf("the up arrow was split across reads %q", got)
			}
		})
	}
}
