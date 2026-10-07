package termcolor

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// chunks returns each string from one Read, then io.EOF. A pause between
// two strings is a gap longer than the filter waits for more input.
type chunks []string

const pause = "\x00pause"

func (c *chunks) Read(p []byte) (int, error) {
	for len(*c) > 0 && (*c)[0] == pause {
		*c = (*c)[1:]
	}
	if len(*c) == 0 {
		return 0, io.EOF
	}
	n := copy(p, (*c)[0])
	(*c)[0] = (*c)[0][n:]
	if (*c)[0] == "" {
		*c = (*c)[1:]
	}
	return n, nil
}

// ready reports whether more input arrives within the filter's wait: the
// next string does, unless a pause comes first.
func (c *chunks) ready() bool { return len(*c) > 0 && (*c)[0] != pause }

// newTestFilter gives the filter a deterministic wait for sources that say
// when more input is ready.
func newTestFilter(src io.Reader) *Filter {
	f := NewFilter(src)
	if r, ok := src.(interface{ ready() bool }); ok {
		f.more = func(time.Duration) bool { return r.ready() }
	}
	return f
}

// reads returns what each Read returned, skipping empty reads.
func reads(t *testing.T, src io.Reader) []string {
	t.Helper()
	f := newTestFilter(src)
	var got []string
	buf := make([]byte, 256)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			got = append(got, string(buf[:n]))
		}
		if err != nil {
			return got
		}
	}
}

type record struct {
	replies []Reply
	notes   []bool
}

func drain(t *testing.T, src io.Reader) (string, *record) {
	t.Helper()
	rec := &record{}
	f := newTestFilter(src)
	f.SetHandlers(func(r Reply) { rec.replies = append(rec.replies, r) }, func(d bool) { rec.notes = append(rec.notes, d) })
	var out bytes.Buffer
	buf := make([]byte, 256)
	for {
		n, err := f.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return out.String(), rec
}

func TestFilterPassesKeysUnchanged(t *testing.T) {
	in := "abc\x1b[A\x1b[I\x1b[O\x1b[200~pasted\x1b[201~\r"
	got, rec := drain(t, &chunks{in})
	if got != in || len(rec.replies) != 0 {
		t.Errorf("got %q, replies %d", got, len(rec.replies))
	}
}

func TestFilterRemovesARoundAndDeliversIt(t *testing.T) {
	got, rec := drain(t, &chunks{"a" + ghosttyMoon + "b"})
	if got != "ab" {
		t.Errorf("keys = %q, want ab", got)
	}
	if len(rec.replies) != 1 || !rec.replies[0].Complete() || !rec.replies[0].Notifies {
		t.Fatalf("replies = %+v", rec.replies)
	}
}

// Review Focus 1.
func TestFilterJoinsAReplySplitAcrossReads(t *testing.T) {
	got, rec := drain(t, &chunks{"x\x1b]11;rgb:22", "22/2424/3636\x1b\\y", "\x1b[?62;22c"})
	if got != "xy" {
		t.Errorf("keys = %q, want xy", got)
	}
	if len(rec.replies) != 1 || rec.replies[0].Background.Hex() != "#222436" {
		t.Errorf("replies = %+v", rec.replies)
	}
}

// Review Focus 2: the probe's stuck q, pinned.
func TestFilterPassesAKeyAfterANotification(t *testing.T) {
	for _, in := range []*chunks{{"\x1b[?997;2nq"}, {"\x1b[?997;2n", "q"}} {
		got, rec := drain(t, in)
		if got != "q" || len(rec.notes) != 1 || rec.notes[0] {
			t.Errorf("got %q, notes %v", got, rec.notes)
		}
	}
}

func TestFilterReleasesAnUnfinishedSequence(t *testing.T) {
	got, _ := drain(t, &chunks{"\x1b]11;rgb:22", "zz", "k"})
	if got != "\x1b]11;rgb:22zzk" {
		t.Errorf("got %q: held bytes must pass through, in order, after one read", got)
	}
}

func TestFilterNeverReturnsMoreThanAsked(t *testing.T) {
	f := NewFilter(&chunks{"\x1b]11;rgb:22", "0123456789"})
	small := make([]byte, 8)
	var out bytes.Buffer
	for {
		n, err := f.Read(small)
		if n > len(small) {
			t.Fatalf("Read returned %d > %d", n, len(small))
		}
		out.Write(small[:n])
		if err != nil {
			break
		}
	}
	if out.String() != "\x1b]11;rgb:220123456789" {
		t.Errorf("got %q", out.String())
	}
}

// A round is longer than bubbletea's 256-byte reads, so the terminal's
// answer is cut wherever a read fills up: inside a reply, right after an
// ESC, or between one reply and the next. Shifting the round by k keys
// moves that cut across every byte of it.
func TestFilterRemovesARoundCutAnywhereByFullReads(t *testing.T) {
	for k := 0; k < 256; k++ {
		keys := strings.Repeat("a", k)
		got, rec := drain(t, &chunks{keys + ghosttyMoon + "b"})
		if got != keys+"b" {
			t.Fatalf("k=%d: keys = %q, want %q", k, got, keys+"b")
		}
		if len(rec.replies) != 1 || !rec.replies[0].Complete() || !rec.replies[0].Notifies {
			t.Fatalf("k=%d: replies = %+v", k, rec.replies)
		}
	}
}

// The esc key is a lone ESC with nothing right behind it. It must reach
// bubbletea on its own, not wait for the next keystroke.
func TestFilterPassesTheEscKeyAtOnce(t *testing.T) {
	for _, in := range []*chunks{{"\x1b"}, {"\x1b", pause, "q"}} {
		got := reads(t, in)
		if len(got) == 0 || got[0] != "\x1b" {
			t.Errorf("reads = %q; want the ESC alone first", got)
		}
	}
}

// An OSC reply that was cut short and never finished must not reach past
// the keys after it to a later reply's terminator.
func TestFilterKeepsKeysAfterABrokenReply(t *testing.T) {
	got, rec := drain(t, &chunks{"\x1b]11;rgb:22q\x1b]10;rgb:c8c8/d3d3/f5f5\x1b\\\x1b[?62;22c"})
	if got != "\x1b]11;rgb:22q" {
		t.Errorf("got %q: the broken reply and the q must pass through", got)
	}
	if len(rec.replies) != 1 || !rec.replies[0].HasForeground || rec.replies[0].HasBackground {
		t.Errorf("replies = %+v", rec.replies)
	}
}

// Only the sequences the filter asked for are removed.
func TestFilterPassesSequencesItDoesNotOwn(t *testing.T) {
	for _, in := range []string{"\x1b[?997;3n", "\x1b[?1;2$y", "\x1b[?1u", "\x1b]12;rgb:00/00/00\x1b\\"} {
		got, rec := drain(t, &chunks{in})
		if got != in || len(rec.notes) != 0 {
			t.Errorf("%q: got %q, notes %v", in, got, rec.notes)
		}
	}
}

// A read that ends in an error (EOF, a closed terminal) holds nothing back:
// there may be no next read to release it.
func TestFilterReleasesHeldBytesOnError(t *testing.T) {
	got, _ := drain(t, iotest.DataErrReader(strings.NewReader("k\x1b]11;rgb:22")))
	if got != "k\x1b]11;rgb:22" {
		t.Errorf("got %q", got)
	}
}

// Handlers may be swapped while a read is under way.
func TestFilterSetHandlersDuringReads(t *testing.T) {
	pr, pw := io.Pipe()
	f := NewFilter(pr)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 256)
		for {
			if _, err := f.Read(buf); err != nil {
				return
			}
		}
	}()
	for i := 0; i < 50; i++ {
		f.SetHandlers(func(Reply) {}, func(bool) {})
		_, _ = pw.Write([]byte("\x1b[?997;1n" + ghosttyMoon))
		f.SetHandlers(nil, nil)
	}
	_ = pw.Close()
	<-done
}

// A round cut anywhere into two arrivals that
// follow each other closely is joined, whatever the reads look like.
func TestFilterJoinsARoundCutAnywhereInTwoArrivals(t *testing.T) {
	for _, k := range []int{0, 1, 7, 128, 200, 255} {
		s := strings.Repeat("a", k) + ghosttyMoon + "b"
		for cut := 1; cut < len(s); cut++ {
			got, rec := drain(t, &chunks{s[:cut], s[cut:]})
			if got != s[:k]+"b" {
				t.Fatalf("k=%d cut=%d: keys = %q", k, cut, got)
			}
			if len(rec.replies) != 1 || !rec.replies[0].Complete() {
				t.Fatalf("k=%d cut=%d: replies = %+v", k, cut, rec.replies)
			}
		}
	}
}

// When replies are removed from a full read,
// bubbletea sees a short read and takes its end as a key boundary. A key
// cut by that read must still reach it whole, in one Read.
func TestFilterKeepsAKeyWholeAtAReadBoundary(t *testing.T) {
	round := "\x1b]11;rgb:2222/2424/3636\x1b\\\x1b[?62;22c"
	for _, key := range []string{"\xc3\xa9", "\x1b[1;5C", "\x1b[15~"} {
		for at := 1; at < len(key); at++ {
			keys := strings.Repeat("a", 256-len(round)-at) + key + "z"
			got := reads(t, &chunks{round + keys})
			if strings.Join(got, "") != keys {
				t.Fatalf("%q at %d: got %q", key, at, got)
			}
			whole := false
			for _, r := range got {
				whole = whole || strings.Contains(r, key)
			}
			if !whole {
				t.Errorf("%q cut at %d across reads %q", key, at, got)
			}
		}
	}
}

// A reply cut right after its opening ESC is not
// an esc key followed by typed characters.
func TestFilterJoinsAReplyCutAfterItsESC(t *testing.T) {
	got, rec := drain(t, &chunks{"k\x1b", "]11;rgb:2222/2424/3636\x1b\\\x1b[?62;22c"})
	if got != "k" || len(rec.replies) != 1 || rec.replies[0].Background.Hex() != "#222436" {
		t.Errorf("keys %q, replies %+v", got, rec.replies)
	}
}

// Alt+[ with nothing right behind it is delivered on
// its own, not merged with the next key (ESC [ A would be the up arrow).
func TestFilterDeliversAltBracketAlone(t *testing.T) {
	got := reads(t, &chunks{"\x1b[", pause, "A"})
	if len(got) != 2 || got[0] != "\x1b[" || got[1] != "A" {
		t.Errorf("reads = %q, want [\"\\x1b[\" \"A\"]", got)
	}
}

// A round whose DA1 never came through leaves nothing
// behind for the next round.
func TestFilterForgetsAnUnfinishedRound(t *testing.T) {
	got, rec := drain(t, &chunks{
		"\x1b]10;rgb:c8c8/d3d3/f5f5\x1b\\\x1b[?62;", pause,
		"22c\x1b]11;rgb:2222/2424/3636\x1b\\\x1b[?62;22c",
	})
	if got != "\x1b[?62;22c" {
		t.Errorf("keys = %q, want the cut DA1 passed through", got)
	}
	if len(rec.replies) != 1 || !rec.replies[0].HasBackground || rec.replies[0].HasForeground {
		t.Errorf("replies = %+v, want the second round's background only", rec.replies)
	}
}

// ESC O is SS3 only when a final byte follows it. Alt+O right before a
// reply must not take the reply's ESC as its third byte: the reply would
// pass as keys and its background would be lost.
func TestFilterTakesSS3OnlyWithAFinalByte(t *testing.T) {
	round := "\x1b]11;rgb:2222/2424/3636\x1b\\\x1b[?62;22c"
	for _, in := range []*chunks{{"\x1bO" + round}, {"\x1bO", round}} {
		got, rec := drain(t, in)
		if got != "\x1bO" {
			t.Errorf("keys = %q, want alt+O alone", got)
		}
		if len(rec.replies) != 1 || rec.replies[0].Background.Hex() != "#222436" {
			t.Errorf("replies = %+v, want the background", rec.replies)
		}
	}
	keys := "\x1bOP\x1bOA"
	if got, _ := drain(t, &chunks{keys}); got != keys {
		t.Errorf("SS3 keys = %q, want %q", got, keys)
	}
}
