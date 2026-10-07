package termcolor

import (
	"bytes"
	"io"
	"os"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// maxCarry bounds an unfinished sequence. A colour reply is about 30
	// bytes, so anything longer is not one of ours.
	maxCarry = 128
	// moreWait is how long a read that ends inside a sequence waits for the
	// rest (spec 2026-10-06 §3). A terminal writes a reply all at once; a
	// person does not type the next key within 25 ms of the esc key.
	moreWait = 25 * time.Millisecond
)

// Filter sits between the terminal and bubbletea while the interface runs
// (spec 2026-10-06 §3). It removes the terminal's colour replies, the
// DECRQM reply and theme-change notifications from the input, hands them to
// its handlers, and passes every other byte through unchanged and in order.
// It reports itself as the terminal file so bubbletea still sets raw mode
// and cancels reads; Close never closes the terminal, because one Filter
// serves every program a process runs.
//
// No Read returns a cut sequence or rune at its end: bubbletea takes the end
// of a short read as a key boundary. A read that ends inside one waits up to
// moreWait for the rest; if nothing comes, the held bytes pass through as
// keys.
type Filter struct {
	src  io.Reader
	file *os.File
	// more reports whether more input is ready within the wait.
	more func(time.Duration) bool

	mu       sync.Mutex
	onReply  func(Reply)
	onNotify func(dark bool)
	// carry is what the next Read takes before src: the seed (Seed), or
	// what did not fit in the last Read's buffer while more input was
	// already waiting. Guarded by mu: Pending reads it from another
	// goroutine.
	carry []byte

	acc Reply // colour replies seen since the round's OSC 11
}

// NewFilter wraps src. Production must pass the terminal's *os.File
// (os.Stdin): only then does the Filter have a real Fd for bubbletea's raw
// mode and cancel reader, and a way to wait for the rest of a cut sequence.
// Any other reader gets an invalid Fd and no wait.
func NewFilter(src io.Reader) *Filter {
	f := &Filter{src: src, more: func(time.Duration) bool { return false }}
	if file, ok := src.(*os.File); ok {
		f.file = file
		f.more = func(d time.Duration) bool { return readable(file.Fd(), d) }
	}
	return f
}

// SetHandlers sets what receives the removed sequences. onReply gets the
// colour replies gathered up to each DA1; onNotify gets each 997
// notification. It is safe to call while reads happen, and a nil handler
// drops what it would have received.
func (f *Filter) SetHandlers(onReply func(Reply), onNotify func(dark bool)) {
	f.mu.Lock()
	f.onReply, f.onNotify = onReply, onNotify
	f.mu.Unlock()
}

// Seed puts b ahead of everything src has: the keys the startup round read
// before the Filter existed (Round.Keys). Call it before the first Read.
// b is scanned like any input, however long, so an unfinished reply at its
// end is joined with the rest when that follows within moreWait.
//
// A reader that waits for the terminal's fd before calling Read (bubbletea
// wraps its input in a cancelreader) never reads a seed on its own: the
// caller asks the terminal for something (see Pending) so its answer wakes
// the reader.
func (f *Filter) Seed(b []byte) {
	f.mu.Lock()
	f.carry = append(append([]byte(nil), b...), f.carry...)
	f.mu.Unlock()
}

// Pending reports whether the Filter holds bytes the next Read returns
// before reading src.
func (f *Filter) Pending() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.carry) > 0
}

func (f *Filter) takeCarry() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	in := f.carry
	f.carry = nil
	return in
}

func (f *Filter) keepCarry(b []byte) {
	f.mu.Lock()
	f.carry = append([]byte(nil), b...)
	f.mu.Unlock()
}

func (f *Filter) Fd() uintptr {
	if f.file != nil {
		return f.file.Fd()
	}
	return ^uintptr(0)
}

func (f *Filter) Name() string {
	if f.file != nil {
		return f.file.Name()
	}
	return "termcolor-filter"
}

func (f *Filter) Write(p []byte) (int, error) {
	if f.file != nil {
		return f.file.Write(p)
	}
	return len(p), nil
}

// Close does nothing: the terminal outlives each program that reads it.
func (f *Filter) Close() error { return nil }

// Read never returns more than len(p) bytes. What it reads from src fits
// in p before replies are removed; only a seed can be longer, and then
// what does not fit waits in carry, cut between tokens, never inside one.
// A read that held only replies returns 0, nil.
func (f *Filter) Read(p []byte) (int, error) {
	in := f.takeCarry()
	var err error
	if len(in) == 0 {
		in = make([]byte, len(p))
		var n int
		n, err = f.src.Read(in)
		in = in[:n]
	}
	w, i := 0, 0
	for {
		for i < len(in) {
			k, n := next(in[i:])
			if k == partial {
				break
			}
			if k == reply {
				f.handle(in[i : i+n])
				i += n
				continue
			}
			if w+n > len(p) {
				if w == 0 {
					// One token longer than p: not with bubbletea's
					// 256-byte reads.
					n = copy(p, in[i:])
					f.keepCarry(in[i+n:])
					return n, nil
				}
				// The rest of a seed. The input its wake asked for is
				// still unread, so the next Read comes at once.
				f.keepCarry(in[i:])
				return w, nil
			}
			w += copy(p[w:], in[i:i+n])
			i += n
		}
		tail := in[i:]
		if len(tail) == 0 {
			return w, err
		}
		if w+len(tail) > len(p) {
			// Only after a seed, as above: the cut sequence waits whole.
			f.keepCarry(tail)
			return w, nil
		}
		if err != nil || len(tail) > maxCarry || !f.more(moreWait) {
			return w + copy(p[w:], tail), err
		}
		room := len(p) - w - len(tail)
		if room == 0 {
			if w == 0 {
				return copy(p, tail), nil
			}
			// The rest is already waiting, so the next Read comes at once.
			f.keepCarry(tail)
			return w, nil
		}
		var n int
		in = append(in, make([]byte, room)...)
		n, err = f.src.Read(in[len(in)-room:])
		in = in[:len(in)-room+n]
		if n == 0 && err == nil {
			return w + copy(p[w:], tail), nil
		}
	}
}

type token int

const (
	plain   token = iota // pass through
	reply                // ours and complete: remove and handle
	partial              // cut by the end of the data
)

// next classifies the token at the start of data and returns its length.
func next(data []byte) (token, int) {
	switch {
	case data[0] == 0x1b:
		if len(data) < 2 {
			return partial, 0
		}
		switch data[1] {
		case '[':
			return scanCSI(data)
		case ']':
			return scanOSC(data)
		case 'O': // SS3: F1-F4 and arrows in application mode
			if len(data) < 3 {
				return partial, 0
			}
			if data[2] >= 0x40 && data[2] <= 0x7e {
				return plain, 3
			}
			// Not SS3 but alt+O: the ESC passes alone, and what follows
			// it, maybe a reply's ESC, is scanned on its own.
			return plain, 1
		}
		return plain, 1 // alt+key: the key follows as its own token
	case data[0] < utf8.RuneSelf:
		return plain, 1
	case !utf8.FullRune(data):
		return partial, 0
	}
	_, n := utf8.DecodeRune(data)
	return plain, n
}

// scanCSI reads ESC [ params intermediates final. Ours are CSI ? Ps c (DA1),
// CSI ? 997 ; 1|2 n and CSI ? 2031 ; Ps $ y; any other CSI is a key, mouse
// or focus event and passes whole.
func scanCSI(data []byte) (token, int) {
	j := 2
	for j < len(data) && data[j] >= 0x30 && data[j] <= 0x3f {
		j++
	}
	params := string(data[2:j])
	for j < len(data) && data[j] >= 0x20 && data[j] <= 0x2f {
		j++
	}
	if j == len(data) {
		return partial, 0
	}
	final := data[j]
	if final < 0x40 || final > 0x7e {
		return plain, 1 // not a CSI: the ESC passes and the rest follows
	}
	inter := string(data[2+len(params) : j])
	if final == 'M' && j == 2 { // X10 mouse: three raw bytes follow
		if len(data) < 6 {
			return partial, 0
		}
		return plain, 6
	}
	ours := false
	switch {
	case final == 'c' && inter == "":
		ours = len(params) > 0 && params[0] == '?' && onlyDigits(params[1:], true)
	case final == 'n' && inter == "":
		ours = params == "?997;1" || params == "?997;2"
	case final == 'y' && inter == "$":
		ours = len(params) == 7 && params[:6] == "?2031;" && onlyDigits(params[6:], false)
	}
	if ours {
		return reply, j + 1
	}
	return plain, j + 1
}

func onlyDigits(s string, semicolons bool) bool {
	for i := 0; i < len(s); i++ {
		if !(s[i] >= '0' && s[i] <= '9' || semicolons && s[i] == ';') {
			return false
		}
	}
	return true
}

var oscOurs = [][]byte{[]byte("10;"), []byte("11;"), []byte("4;")}

// scanOSC owns OSC 10, 11 and 4 replies, ended by BEL or ST. The body of a
// colour reply is printable, so any other control byte, an ESC that is not
// ST included, means this is not one: the keys after a cut reply are never
// swallowed up to some later reply's terminator. An OSC that is not ours
// passes byte by byte, as it did without the filter.
func scanOSC(data []byte) (token, int) {
	body := data[2:]
	prefixed := false
	for _, p := range oscOurs {
		if bytes.HasPrefix(body, p) {
			prefixed = true
		} else if len(body) < len(p) && bytes.HasPrefix(p, body) {
			return partial, 0 // too short to tell yet
		}
	}
	if !prefixed {
		return plain, 1
	}
	for j := 2; j < len(data); j++ {
		switch c := data[j]; {
		case c == 0x07:
			return reply, j + 1
		case c == 0x1b && j+1 == len(data):
			return partial, 0
		case c == 0x1b && data[j+1] == '\\':
			return reply, j + 2
		case c < 0x20 || c == 0x7f:
			return plain, 1
		}
	}
	return partial, 0
}

func (f *Filter) handle(seq []byte) {
	f.mu.Lock()
	onReply, onNotify := f.onReply, f.onNotify
	f.mu.Unlock()
	if dark, ok := ParseNotify(seq); ok {
		if onNotify != nil {
			onNotify(dark)
		}
		return
	}
	if bytes.HasPrefix(seq, []byte("\x1b]11;")) {
		// OSC 11 opens every round: drop what a round whose DA1 never
		// arrived left behind.
		f.acc = Reply{}
	}
	rep, da1 := Parse(seq)
	f.acc = f.acc.Merge(rep)
	if da1 {
		if f.acc != (Reply{}) && onReply != nil {
			onReply(f.acc)
		}
		f.acc = Reply{}
	}
}
