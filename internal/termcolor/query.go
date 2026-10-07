package termcolor

import (
	"bytes"
	"io"
	"os"
	"time"

	"github.com/muesli/cancelreader"
	"golang.org/x/term"
)

// Round is what the startup query got back.
type Round struct {
	// Reply is the colours that arrived before the round ended.
	Reply Reply
	// Done: the terminal answered DA1, so the whole answer is in. False
	// when the timeout ended the round: the rest may still be on its way,
	// and only an input filter can keep it out of the keys.
	Done bool
	// Keys is every byte the round read that is not a complete reply:
	// keys typed while it ran, in order. An unfinished sequence at the end
	// is kept too, for the input filter to join with what follows (see
	// Filter.Seed).
	Keys []byte
}

// Query asks the terminal once, before the interface starts (spec
// 2026-10-06 §1). It never leaves a reader behind: a reader still blocked on
// stdin after the round would swallow the user's first keystroke.
func Query(in *os.File, out io.Writer, timeout time.Duration) (Round, error) {
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return Round{}, err
	}
	defer term.Restore(int(in.Fd()), state)
	restoreVT := enableVT(out)
	defer restoreVT()
	cr, err := cancelreader.NewReader(in)
	if err != nil {
		return Round{}, err
	}
	defer cr.Close()
	return query(cr, cr.Cancel, out, timeout)
}

func query(r io.Reader, cancel func() bool, w io.Writer, timeout time.Duration) (Round, error) {
	if _, err := w.Write(QueryBytes()); err != nil {
		return Round{}, err
	}
	type chunk struct {
		b   []byte
		err error
	}
	ch := make(chan chunk)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 512)
		for {
			n, err := r.Read(buf)
			ch <- chunk{append([]byte(nil), buf[:n]...), err}
			if err != nil {
				return
			}
		}
	}()
	var got bytes.Buffer
	deadline := time.After(timeout)
	finish := func() (Round, error) {
		cancel()
		// Drain until the reader goroutine exits, so nothing reads stdin after us.
		for {
			select {
			case c := <-ch:
				got.Write(c.b)
			case <-done:
				rep, da1 := Parse(got.Bytes())
				return Round{Reply: rep, Done: da1, Keys: keys(got.Bytes())}, nil
			}
		}
	}
	for {
		select {
		case c := <-ch:
			got.Write(c.b)
			if _, da1 := Parse(got.Bytes()); da1 || c.err != nil {
				return finish()
			}
		case <-deadline:
			return finish()
		}
	}
}

// keys removes the complete replies from b, the way the input filter
// does, and keeps the rest.
func keys(b []byte) []byte {
	var out []byte
	for i := 0; i < len(b); {
		k, n := next(b[i:])
		if k == partial {
			return append(out, b[i:]...)
		}
		if k != reply {
			out = append(out, b[i:i+n]...)
		}
		i += n
	}
	return out
}
