package manage

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/style"
)

// field is a one-line text editor, hand-rolled because the redesign takes
// no new dependency (spec, "Agreed"). It opens with its initial value
// selected: the first printable key replaces the value, and an arrow key
// keeps it for editing. The old interface appended to a prefilled buffer
// with no way to clear it, which is how "development" became
// "developmentproduction-ish".
//
// Enter and esc belong to whoever owns the field; update ignores them.
type field struct {
	runes    []rune
	cursor   int
	selected bool
	masked   bool
}

func newField(initial string, masked bool) field {
	r := []rune(initial)
	return field{runes: r, cursor: len(r), selected: len(r) > 0, masked: masked}
}

func (f field) value() string { return string(f.runes) }

func (f field) cleared() field { return field{masked: f.masked} }

// oneLine drops line breaks: every value edited here is a single line, and
// a pasted newline would otherwise split one value into two lines of a
// dotenv file.
func oneLine(rs []rune) []rune {
	out := make([]rune, 0, len(rs))
	for _, r := range rs {
		if r != '\n' && r != '\r' {
			out = append(out, r)
		}
	}
	return out
}

func (f field) update(k tea.KeyMsg) field {
	switch k.Type {
	case tea.KeyRunes, tea.KeySpace:
		ins := k.Runes
		if k.Type == tea.KeySpace {
			ins = []rune{' '}
		}
		ins = oneLine(ins)
		if f.selected {
			f = f.cleared()
		}
		rest := append([]rune(nil), f.runes[f.cursor:]...)
		f.runes = append(append(f.runes[:f.cursor:f.cursor], ins...), rest...)
		f.cursor += len(ins)
	case tea.KeyLeft:
		if f.selected {
			f.selected, f.cursor = false, 0
		} else if f.cursor > 0 {
			f.cursor--
		}
	case tea.KeyRight:
		if f.selected {
			f.selected, f.cursor = false, len(f.runes)
		} else if f.cursor < len(f.runes) {
			f.cursor++
		}
	case tea.KeyHome, tea.KeyCtrlA:
		f.selected, f.cursor = false, 0
	case tea.KeyEnd, tea.KeyCtrlE:
		f.selected, f.cursor = false, len(f.runes)
	case tea.KeyBackspace:
		if f.selected {
			return f.cleared()
		}
		if f.cursor > 0 {
			f.runes = append(f.runes[:f.cursor-1:f.cursor-1], f.runes[f.cursor:]...)
			f.cursor--
		}
	case tea.KeyDelete:
		if f.selected {
			return f.cleared()
		}
		if f.cursor < len(f.runes) {
			f.runes = append(f.runes[:f.cursor:f.cursor], f.runes[f.cursor+1:]...)
		}
	case tea.KeyCtrlU:
		if f.selected {
			return f.cleared()
		}
		f.runes, f.cursor = append([]rune(nil), f.runes[f.cursor:]...), 0
	case tea.KeyCtrlK:
		if f.selected {
			return f.cleared()
		}
		f.runes = append([]rune(nil), f.runes[:f.cursor]...)
	case tea.KeyCtrlW:
		if f.selected {
			return f.cleared()
		}
		i := f.cursor
		for i > 0 && unicode.IsSpace(f.runes[i-1]) {
			i--
		}
		for i > 0 && !unicode.IsSpace(f.runes[i-1]) {
			i--
		}
		f.runes = append(f.runes[:i:i], f.runes[f.cursor:]...)
		f.cursor = i
	}
	return f
}

// view draws the field. Both states read without colour: a selected value
// is bracketed as well as highlighted, since the next key replaces it, and
// otherwise a ▏ marks the cursor. A masked field draws a bullet per rune and
// never the rune itself.
func (f field) view(th style.Theme) string {
	shown := f.runes
	if f.masked {
		shown = []rune(strings.Repeat("•", len(f.runes)))
	}
	if f.selected {
		return th.Muted.Render("[") + th.Selected.Render(display(string(shown))) + th.Muted.Render("]")
	}
	return th.Text.Render(display(string(shown[:f.cursor]))) + th.Accent.Render("▏") + th.Text.Render(display(string(shown[f.cursor:])))
}
