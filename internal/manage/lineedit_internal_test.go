package manage

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/style"
)

func runes(s string) tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func kt(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func edit(initial string, keys ...tea.KeyMsg) string {
	f := newField(initial, false)
	for _, k := range keys {
		f = f.update(k)
	}
	return f.value()
}

// Spec §1: a field opens with its value selected. Typing replaces it; an
// arrow keeps it. This is the fix for "developmentproduction-ish".
func TestFieldTypingReplacesTheSelectedValue(t *testing.T) {
	if got := edit("development", runes("production")); got != "production" {
		t.Errorf("got %q, want production", got)
	}
}

func TestFieldArrowKeepsTheValueForEditing(t *testing.T) {
	if got := edit("dev", kt(tea.KeyRight), runes("-2")); got != "dev-2" {
		t.Errorf("right then type = %q, want dev-2", got)
	}
	if got := edit("dev", kt(tea.KeyLeft), runes("x")); got != "xdev" {
		t.Errorf("left then type = %q, want xdev", got)
	}
}

func TestFieldEditingKeys(t *testing.T) {
	cases := []struct {
		name string
		keys []tea.KeyMsg
		want string
	}{
		{"backspace on selection clears", []tea.KeyMsg{kt(tea.KeyBackspace)}, ""},
		{"backspace at end", []tea.KeyMsg{kt(tea.KeyEnd), kt(tea.KeyBackspace)}, "hello worl"},
		{"delete at home", []tea.KeyMsg{kt(tea.KeyHome), kt(tea.KeyDelete)}, "ello world"},
		{"ctrl+a then type", []tea.KeyMsg{kt(tea.KeyCtrlA), runes(">")}, ">hello world"},
		{"ctrl+e then type", []tea.KeyMsg{kt(tea.KeyCtrlE), runes("!")}, "hello world!"},
		{"ctrl+u clears to start", []tea.KeyMsg{kt(tea.KeyEnd), kt(tea.KeyLeft), kt(tea.KeyCtrlU)}, "d"},
		{"ctrl+k clears to end", []tea.KeyMsg{kt(tea.KeyHome), kt(tea.KeyRight), kt(tea.KeyCtrlK)}, "h"},
		{"ctrl+w deletes a word", []tea.KeyMsg{kt(tea.KeyEnd), kt(tea.KeyCtrlW)}, "hello "},
		{"ctrl+w skips trailing space", []tea.KeyMsg{kt(tea.KeyEnd), kt(tea.KeySpace), kt(tea.KeyCtrlW)}, "hello "},
		{"space inserts", []tea.KeyMsg{kt(tea.KeyEnd), kt(tea.KeySpace), runes("x")}, "hello world x"},
		{"left clamps at start", []tea.KeyMsg{kt(tea.KeyHome), kt(tea.KeyLeft), runes("<")}, "<hello world"},
	}
	for _, c := range cases {
		if got := edit("hello world", c.keys...); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// Every value this edits is one line: a pasted newline would split it.
func TestFieldPasteIsOneInsertionWithoutNewlines(t *testing.T) {
	paste := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line1\r\nline2\n"), Paste: true}
	if got := edit("", paste); got != "line1line2" {
		t.Errorf("paste = %q, want line1line2", got)
	}
}

func TestFieldMultibyteRunes(t *testing.T) {
	if got := edit("héllo", kt(tea.KeyEnd), kt(tea.KeyBackspace), kt(tea.KeyBackspace)); got != "hél" {
		t.Errorf("got %q, want hél", got)
	}
}

// A masked field never draws what was typed.
func TestMaskedFieldNeverDrawsItsValue(t *testing.T) {
	f := newField("", true).update(runes("s3cret"))
	v := f.view(style.Theme{})
	if strings.Contains(v, "s3cret") || strings.Count(v, "•") != 6 {
		t.Errorf("masked view = %q, want six bullets and no value", v)
	}
	if f.value() != "s3cret" {
		t.Errorf("value = %q", f.value())
	}
}

func TestDisplayEscapesControlCharacters(t *testing.T) {
	cases := map[string]string{
		"plain":       "plain",
		"tab\there":   `tab\there`,
		"esc\x1b[31m": `esc\x1b[31m`,
		"line\nbreak": `line\nbreak`,
		"c1\u009bcsi": `c1\x9bcsi`,
		"wide 日本":     "wide 日本",
	}
	for in, want := range cases {
		if got := display(in); got != want {
			t.Errorf("display(%q) = %q, want %q", in, got, want)
		}
	}
	if plural(1, "change") != "1 change" || plural(2, "change") != "2 changes" {
		t.Error("plural")
	}
}
