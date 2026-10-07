package termcolor

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

// chunks returns each string from one Read, then io.EOF.
type chunks []string

func (c *chunks) Read(p []byte) (int, error) {
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

type record struct {
	replies []Reply
	notes   []bool
}

func drain(t *testing.T, src io.Reader) (string, *record) {
	t.Helper()
	rec := &record{}
	f := NewFilter(src)
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
		got, rec := drain(t, strings.NewReader(keys+ghosttyMoon+"b"))
		if got != keys+"b" {
			t.Fatalf("k=%d: keys = %q, want %q", k, got, keys+"b")
		}
		if len(rec.replies) != 1 || !rec.replies[0].Complete() || !rec.replies[0].Notifies {
			t.Fatalf("k=%d: replies = %+v", k, rec.replies)
		}
	}
}

// The esc key is a lone ESC in a short read. It must reach bubbletea at
// once, not wait for the next keystroke.
func TestFilterPassesTheEscKeyAtOnce(t *testing.T) {
	f := NewFilter(&chunks{"\x1b", "q"})
	buf := make([]byte, 256)
	if n, err := f.Read(buf); err != nil || string(buf[:n]) != "\x1b" {
		t.Fatalf("first Read = %q, %v; want the ESC at once", buf[:n], err)
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
