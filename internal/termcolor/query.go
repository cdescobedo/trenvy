package termcolor

import (
	"bytes"
	"io"
	"os"
	"time"

	"github.com/muesli/cancelreader"
	"golang.org/x/term"
)

// Query asks the terminal once, before the interface starts (spec
// 2026-10-06 §1). It never leaves a reader behind: a reader still blocked on
// stdin after the round would swallow the user's first keystroke.
func Query(in *os.File, out io.Writer, timeout time.Duration) (Reply, error) {
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return Reply{}, err
	}
	defer term.Restore(int(in.Fd()), state)
	restoreVT := enableVT(out)
	defer restoreVT()
	cr, err := cancelreader.NewReader(in)
	if err != nil {
		return Reply{}, err
	}
	defer cr.Close()
	return query(cr, cr.Cancel, out, timeout)
}

func query(r io.Reader, cancel func() bool, w io.Writer, timeout time.Duration) (Reply, error) {
	if _, err := w.Write(QueryBytes()); err != nil {
		return Reply{}, err
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
	finish := func() (Reply, error) {
		cancel()
		// Drain until the reader goroutine exits, so nothing reads stdin after us.
		for {
			select {
			case c := <-ch:
				got.Write(c.b)
			case <-done:
				rep, _ := Parse(got.Bytes())
				return rep, nil
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
