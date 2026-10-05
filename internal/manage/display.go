package manage

import (
	"fmt"
	"strings"
	"unicode"
)

// display makes a value safe to draw. A control character in a value would
// otherwise reach the terminal as a command -- an ESC starts an escape
// sequence -- so each is drawn in Go's escape notation instead. Only the
// drawing changes; the value itself is never rewritten.
func display(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case unicode.IsControl(r):
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// errText is an error as one message line. A command's own refusal can
// run over several lines (render's gitignore refusal is four), which the
// message line would draw as \n escapes: the lines are joined with " -- "
// instead, and a line ending in ":" runs on into the one it introduces.
func errText(err error) string {
	var b strings.Builder
	prev := ""
	for _, l := range strings.Split(err.Error(), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		switch {
		case prev == "":
		case strings.HasSuffix(prev, ":"):
			b.WriteString(" ")
		default:
			b.WriteString(" -- ")
		}
		b.WriteString(l)
		prev = l
	}
	return b.String()
}

// plural is "1 problem" / "2 problems".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
