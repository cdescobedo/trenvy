package termcolor

import (
	"bytes"
	"io"
	"os"
	"sync"
)

// maxCarry bounds an unfinished sequence held over to the next read. A
// colour reply is about 30 bytes, so anything longer is not one of ours.
const maxCarry = 128

// Filter sits between the terminal and bubbletea while the interface runs
// (spec 2026-10-06 §3). It removes the terminal's colour replies, the
// DECRQM reply and theme-change notifications from the input, hands them to
// its handlers, and passes every other byte through unchanged and in order.
// It reports itself as the terminal file so bubbletea still sets raw mode
// and cancels reads; Close never closes the terminal, because one Filter
// serves every program a process runs.
type Filter struct {
	src  io.Reader
	file *os.File

	mu       sync.Mutex
	onReply  func(Reply)
	onNotify func(dark bool)

	// carry is an unfinished sequence from the last read. Every carry has
	// already waited one read, so if it is still unfinished at the start
	// of the next one it passes through.
	carry []byte
	acc   Reply // colour replies seen since the last DA1
	buf   [512]byte
}

// NewFilter wraps src, normally os.Stdin. Fd, Name and Write reach the
// underlying file only when src is an *os.File.
func NewFilter(src io.Reader) *Filter {
	f := &Filter{src: src}
	if file, ok := src.(*os.File); ok {
		f.file = file
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

// Read reads at most len(p)-len(carry) bytes from src, so what it returns
// never exceeds len(p) and nothing is buffered for later. A read that held
// only replies returns 0, nil.
func (f *Filter) Read(p []byte) (int, error) {
	if len(p) <= len(f.carry) {
		// Not with bubbletea's 256-byte buffer and maxCarry 128. Release
		// the carry rather than return more than asked.
		n := copy(p, f.carry)
		f.carry = f.carry[n:]
		return n, nil
	}
	room := min(len(p)-len(f.carry), len(f.buf))
	n, err := f.src.Read(f.buf[:room])
	data := append(f.carry, f.buf[:n]...)
	held := len(f.carry) > 0
	f.carry = nil
	// A read that filled its room may have more waiting right behind it,
	// as when a round longer than one read arrives at once.
	full := n == room
	out := f.strip(data, held, full, err != nil)
	return copy(p, out), err
}

// strip removes complete replies from data and returns the rest. An
// unfinished sequence at the end is carried into the next read, unless it
// is the carry that already waited (held, starting at 0), it is too long,
// or this is the last read (final).
func (f *Filter) strip(data []byte, held, full, final bool) []byte {
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		if data[i] != 0x1b {
			out = append(out, data[i])
			i++
			continue
		}
		end, complete, ours := scanSeq(data[i:])
		// A lone ESC at the end of a short read is the esc key, and passes
		// at once. At the end of a full read it may start a reply.
		if i == len(data)-1 && !full {
			ours = false
		}
		switch {
		case !ours:
			out = append(out, data[i])
			i++
		case !complete:
			rest := data[i:]
			if final || (held && i == 0) || len(rest) > maxCarry {
				return append(out, rest...)
			}
			f.carry = append([]byte(nil), rest...)
			return out
		default:
			f.handle(data[i : i+end])
			i += end
		}
	}
	return out
}

// scanSeq reports, for data starting with ESC, whether it starts a sequence
// this filter owns, whether that sequence is complete, and its length. An
// unfinished sequence that could still become one of ours counts as ours.
func scanSeq(data []byte) (end int, complete, ours bool) {
	if len(data) < 2 {
		return 0, false, true
	}
	switch data[1] {
	case ']':
		return scanOSC(data)
	case '[':
		return scanCSI(data)
	}
	return 0, false, false
}

var oscOurs = [][]byte{[]byte("10;"), []byte("11;"), []byte("4;")}

// scanOSC owns OSC 10, 11 and 4 replies, ended by BEL or ST. The body of a
// colour reply is printable, so any other control byte, an ESC that is not
// ST included, means this is not one: the keys after a cut reply are never
// swallowed up to some later reply's terminator.
func scanOSC(data []byte) (end int, complete, ours bool) {
	body := data[2:]
	prefixed := false
	for _, p := range oscOurs {
		if bytes.HasPrefix(body, p) {
			prefixed = true
		} else if len(body) < len(p) && bytes.HasPrefix(p, body) {
			return 0, false, true // too short to tell yet
		}
	}
	if !prefixed {
		return 0, false, false
	}
	for j := 2; j < len(data); j++ {
		switch c := data[j]; {
		case c == 0x07:
			return j + 1, true, true
		case c == 0x1b && j+1 == len(data):
			return 0, false, true
		case c == 0x1b && data[j+1] == '\\':
			return j + 2, true, true
		case c < 0x20 || c == 0x7f:
			return 0, false, false
		}
	}
	return 0, false, true
}

// scanCSI owns CSI ? Ps c (DA1), CSI ? 997 ; 1|2 n and CSI ? 2031 ; Ps $ y.
func scanCSI(data []byte) (end int, complete, ours bool) {
	if len(data) < 3 {
		return 0, false, true
	}
	if data[2] != '?' {
		return 0, false, false
	}
	j := 3
	for j < len(data) && (data[j] >= '0' && data[j] <= '9' || data[j] == ';') {
		j++
	}
	if j == len(data) {
		return 0, false, true
	}
	params := string(data[3:j])
	switch data[j] {
	case 'c':
		return j + 1, true, true
	case 'n':
		if params == "997;1" || params == "997;2" {
			return j + 1, true, true
		}
	case '$':
		if j+1 == len(data) {
			return 0, false, true
		}
		if data[j+1] == 'y' && len(params) == 6 && params[:5] == "2031;" && params[5] >= '0' && params[5] <= '9' {
			return j + 2, true, true
		}
	}
	return 0, false, false
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
	rep, da1 := Parse(seq)
	f.acc = f.acc.Merge(rep)
	if da1 {
		if f.acc != (Reply{}) && onReply != nil {
			onReply(f.acc)
		}
		f.acc = Reply{}
	}
}
