package termcolor

import (
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/muesli/cancelreader"
)

func TestQueryReadsARound(t *testing.T) {
	inR, inW, _ := os.Pipe()
	defer inR.Close()
	defer inW.Close()
	var sent strings.Builder
	go func() { time.Sleep(5 * time.Millisecond); io.WriteString(inW, ghosttyMoon) }()
	cr, err := cancelreader.NewReader(inR)
	if err != nil {
		t.Fatal(err)
	}
	round, err := query(cr, cr.Cancel, &sent, 500*time.Millisecond)
	rep := round.Reply
	if err != nil || !rep.Complete() || rep.Background.Hex() != "#222436" {
		t.Fatalf("rep = %+v err = %v", rep, err)
	}
	if sent.String() != string(QueryBytes()) {
		t.Errorf("sent %q", sent.String())
	}
}

// countingReader counts Read calls still in flight.
type countingReader struct {
	r        io.Reader
	inflight atomic.Int32
}

func (c *countingReader) Read(p []byte) (int, error) {
	c.inflight.Add(1)
	defer c.inflight.Add(-1)
	return c.r.Read(p)
}

// A reply that arrives in two writes must not end the round early: only DA1
// ends it.
func TestQueryWaitsForDA1AcrossWrites(t *testing.T) {
	inR, inW, _ := os.Pipe()
	defer inR.Close()
	defer inW.Close()
	i := strings.Index(ghosttyMoon, "\x1b]4;3;")
	if i < 0 {
		t.Fatal("ghosttyMoon has no palette 3 entry to split at")
	}
	go func() {
		io.WriteString(inW, ghosttyMoon[:i])
		time.Sleep(30 * time.Millisecond)
		io.WriteString(inW, ghosttyMoon[i:])
	}()
	cr, err := cancelreader.NewReader(inR)
	if err != nil {
		t.Fatal(err)
	}
	round, _ := query(cr, cr.Cancel, io.Discard, 500*time.Millisecond)
	if rep := round.Reply; !rep.Complete() {
		t.Fatalf("incomplete: %+v", rep)
	}
}

// Review Focus 3: a terminal that answers nothing.
func TestQueryGivesUpWithoutStealingInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.File read deadlines do not work on Windows pipes; the startup query there is exercised by hand")
	}
	inR, inW, _ := os.Pipe()
	defer inR.Close()
	defer inW.Close()
	cr, err := cancelreader.NewReader(inR)
	if err != nil {
		t.Fatal(err)
	}
	c := &countingReader{r: cr}
	start := time.Now()
	round, _ := query(c, cr.Cancel, io.Discard, 100*time.Millisecond)
	if n := c.inflight.Load(); n != 0 {
		cr.Cancel() // unblock it so the deferred Close cannot hang the test
		t.Errorf("reader still blocked: %d in flight", n)
	}
	if took := time.Since(start); took > 400*time.Millisecond {
		t.Errorf("took %v", took)
	}
	if round.Reply.HasBackground || round.Done {
		t.Error("silence parsed as a colour")
	}
	// The next keystroke must reach whoever reads next, not the query.
	io.WriteString(inW, "k")
	buf := make([]byte, 4)
	inR.SetReadDeadline(time.Now().Add(time.Second))
	n, err := inR.Read(buf)
	if err != nil || string(buf[:n]) != "k" {
		t.Errorf("after the query, read %q, %v: the keystroke was stolen", buf[:n], err)
	}
}

// feed is a terminal whose bytes are already waiting when the round
// starts; after them a Read blocks until cancel, as cancelreader's does.
type feed struct {
	ch        chan string
	cancelled chan struct{}
	once      sync.Once
}

func newFeed(chunks ...string) *feed {
	f := &feed{ch: make(chan string, len(chunks)), cancelled: make(chan struct{})}
	for _, c := range chunks {
		f.ch <- c
	}
	return f
}

func (f *feed) Read(p []byte) (int, error) {
	select {
	case s := <-f.ch:
		return copy(p, s), nil
	case <-f.cancelled:
		return 0, cancelreader.ErrCanceled
	}
}

func (f *feed) cancel() bool { f.once.Do(func() { close(f.cancelled) }); return true }

// A terminal slower than the timeout (over SSH, say) answers after the
// round has given up. What came in time is kept, the round says it is not
// done, and the rest, arriving through the input filter, is removed and
// handed over instead of being typed into the interface.
func TestQueryHandsALateRoundToTheFilter(t *testing.T) {
	cut := strings.Index(ghosttyMoon, "\x1b]4;3;") + len("\x1b]4;3;rgb:ff")
	f := newFeed("k" + ghosttyMoon[:cut])
	round, err := query(f, f.cancel, io.Discard, 30*time.Millisecond)
	if err != nil || round.Done {
		t.Fatalf("round = %+v, err %v: a round without DA1 is not done", round, err)
	}
	if !round.Reply.HasBackground || round.Reply.Complete() {
		t.Errorf("in-time part = %+v, want the background and no more", round.Reply)
	}
	filter := newTestFilter(&chunks{ghosttyMoon[cut:], "q"})
	filter.Seed(round.Keys)
	got, rec := drainFilter(t, filter)
	if got != "kq" {
		t.Errorf("keys = %q, want kq: the late answer leaked as keystrokes", got)
	}
	if len(rec.replies) != 1 || !round.Reply.Merge(rec.replies[0]).Complete() || !rec.replies[0].Notifies {
		t.Errorf("late replies = %+v, want the rest of the round", rec.replies)
	}
}

// Keys typed while the round runs, before, between and after the replies,
// reach the interface first, unchanged and in order.
func TestQueryKeepsKeysTypedDuringTheRound(t *testing.T) {
	afterFG := strings.Index(ghosttyMoon, "\x1b]4;0;")
	afterRed := strings.Index(ghosttyMoon, "\x1b]4;2;")
	in := "a" + ghosttyMoon[:afterFG] + "b" + ghosttyMoon[afterFG:afterRed] + "\x1b[A" + ghosttyMoon[afterRed:] + "z"
	split := len(in) / 2
	f := newFeed(in[:split], in[split:])
	round, err := query(f, f.cancel, io.Discard, time.Second)
	if err != nil || !round.Done || !round.Reply.Complete() {
		t.Fatalf("round = %+v, err %v", round, err)
	}
	if string(round.Keys) != "ab\x1b[Az" {
		t.Errorf("round keys = %q, want %q", round.Keys, "ab\x1b[Az")
	}
	filter := newTestFilter(&chunks{"e"})
	filter.Seed(round.Keys)
	if got, _ := drainFilter(t, filter); got != "ab\x1b[Aze" {
		t.Errorf("keys = %q, want the round's keys first, then the next", got)
	}
}
