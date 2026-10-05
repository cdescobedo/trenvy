package manage

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// typedOverlay asks for each removed profile's name before a save (spec
// §3: a profile removal needs the name typed before enter is accepted).
// esc returns to the review with nothing written.
type typedOverlay struct {
	names []string
	i     int
	f     field
}

func (o *typedOverlay) update(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	if k.Type != tea.KeyEnter {
		o.f = o.f.update(k)
		return false, nil
	}
	want := o.names[o.i]
	if strings.TrimSpace(o.f.value()) != want {
		m.msg = message{"type " + want + " exactly to remove it, or esc to go back", true}
		return false, nil
	}
	o.i, o.f = o.i+1, newField("", false)
	if o.i < len(o.names) {
		return false, nil
	}
	return true, m.startSave()
}

func (o *typedOverlay) view(m *Model, w, h int) string {
	p := o.names[o.i]
	text := fmt.Sprintf("Type %s to remove it. Its keyring items are deleted and cannot be restored.", p)
	if len(o.names) > 1 {
		text = fmt.Sprintf("(%d of %d) ", o.i+1, len(o.names)) + text
	}
	body := strings.Split(m.th.Text.Width(min(w, panelMax)-4).Render(text), "\n")
	body = append(body, "", " "+o.f.view(m.th, min(w, panelMax)-4))
	return m.panel("Remove profile "+p, body, w, h)
}

func (o *typedOverlay) hints(*Model) []hint {
	return []hint{{"enter", "confirm"}, {"esc", "back to review"}}
}
