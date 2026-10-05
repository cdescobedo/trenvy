package manage

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/session"
)

// overlay is a panel drawn over the body: an edit field, the . menu, the
// save review, a confirmation, the key list. Only the top one gets keys.
// esc always closes the top overlay and does nothing else (spec §1); the
// Model handles esc before an overlay sees it.
//
// Overlays are pointers, updated in place. A Model value is copied on
// every Update, and those copies share the overlays; nothing reads an old
// Model after Update returns, so the sharing is harmless.
type overlay interface {
	update(m *Model, k tea.KeyMsg) (done bool, cmd tea.Cmd)
	view(m *Model, w, h int) string
	hints(m *Model) []hint
}

// panel is how every overlay draws: a title, a blank line, the body, in a
// focused pane no wider than panelMax columns and no taller than h.
func (m *Model) panel(title string, body []string, w, h int) string {
	return m.panelAt(title, body, 0, w, h)
}

// panelAt is panel scrolled so body line focus stays visible.
func (m *Model) panelAt(title string, body []string, focus, w, h int) string {
	pw := min(w, panelMax)
	lines := append([]string{m.th.Title.Render(title), ""}, body...)
	ph := min(h, len(lines)+2)
	return m.th.Pane(strings.Join(window(lines, focus+2, ph-2), "\n"), pw, ph, true)
}

// choice is one answer a confirmOverlay offers.
type choice struct {
	key, label string
	run        func(m *Model) tea.Cmd
}

// confirmOverlay asks one question. esc is always "no" and needs no choice.
type confirmOverlay struct {
	title, text string
	choices     []choice
}

func (c *confirmOverlay) update(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	for _, ch := range c.choices {
		if keyIs(k, ch.key) {
			return true, ch.run(m)
		}
	}
	return false, nil
}

func (c *confirmOverlay) view(m *Model, w, h int) string {
	body := strings.Split(m.th.Text.Width(min(w, panelMax)-4).Render(c.text), "\n")
	return m.panel(c.title, body, w, h)
}

func (c *confirmOverlay) hints(*Model) []hint {
	var hs []hint
	for _, ch := range c.choices {
		hs = append(hs, hint{ch.key, ch.label})
	}
	return append(hs, hint{"esc", "cancel"})
}

// helpOverlay is the full key list. It wraps rather than truncates (spec
// diagnosis #11: the old list was cut off at 110 columns), and on a short
// terminal it scrolls, saying which way there is more (spec §1: "never
// truncates").
type helpOverlay struct{ off int }

func (o *helpOverlay) update(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	w, _ := m.size()
	_, _, maxOff := o.layout(m, w, m.bodyHeight())
	switch {
	case keyIs(k, "j", "down"):
		o.off = clamp(o.off+1, 0, maxOff)
	case keyIs(k, "k", "up"):
		o.off = clamp(o.off-1, 0, maxOff)
	}
	return keyIs(k, "?"), nil
}

// layout is the help body for a w x h body area, how many of its lines fit
// in the panel, and the furthest it can scroll.
func (o *helpOverlay) layout(m *Model, w, h int) (body []string, rows, maxOff int) {
	descW := min(w, panelMax) - 4 - 10
	for _, v := range verbs {
		desc := strings.Split(m.th.KeyDesc.Width(descW).Render(v.desc), "\n")
		for i, d := range desc {
			k := ""
			if i == 0 {
				k = v.key
			}
			body = append(body, lipgloss.JoinHorizontal(lipgloss.Top, m.th.Key.Width(10).Render(k), d))
		}
	}
	rows, maxOff = scrollRows(len(body), h)
	return body, rows, maxOff
}

// scrollRows is how many body lines fit in a panel h tall, and the furthest
// a body of n lines can scroll. Two borders, the title and the blank line
// under it take four rows. Scrolling needs a row for an indicator and one
// for the body; below that the pane simply cuts.
func scrollRows(n, h int) (rows, maxOff int) {
	rows = max(0, min(h, n+4)-4)
	if n <= rows || rows < 2 {
		return rows, 0
	}
	// Scrolled to the end, the first row is "↑ more" and the rest is body.
	return rows, max(0, n-(rows-1))
}

// scrollPanel draws body scrolled to off, with "↑ more" and "↓ more" rows
// where there is more in that direction.
func (m *Model) scrollPanel(title string, body []string, off, w, h int) string {
	rows, maxOff := scrollRows(len(body), h)
	if maxOff == 0 {
		return m.panel(title, body, w, h)
	}
	off = clamp(off, 0, maxOff) // a resize can shrink maxOff
	var shown []string
	n := rows
	if off > 0 {
		shown = append(shown, m.th.Muted.Render("↑ more"))
		n--
	}
	more := off+n < len(body)
	if more {
		n--
	}
	if n < 1 && off > 0 { // too short for both: a verb and ↓ more win
		shown, n = nil, n+1
	}
	shown = append(shown, body[off:off+max(0, n)]...)
	if more {
		shown = append(shown, m.th.Muted.Render("↓ more"))
	}
	return m.panel(title, shown, w, h)
}

func (o *helpOverlay) view(m *Model, w, h int) string {
	body, _, _ := o.layout(m, w, h)
	return m.scrollPanel("Keys", body, o.off, w, h)
}

// filesOverlay lists the files a save left on disk, each in full: a path
// is the one thing here that must not be cut. It scrolls like help.
type filesOverlay struct {
	paths []string
	off   int
}

func (o *filesOverlay) layout(m *Model, w int) []string {
	inner := min(w, panelMax) - 4
	var body []string
	for _, p := range o.paths {
		for i, part := range wrap(display(p), inner-2) {
			pre := ""
			if i > 0 {
				pre = "  " // a path's continuation hangs under its start
			}
			body = append(body, pre+m.th.Text.Render(part))
		}
	}
	body = append(body, "")
	for _, part := range wrap("trenvy no longer tracks these. Delete them if you no longer need them.", inner) {
		body = append(body, m.th.Muted.Render(part))
	}
	return body
}

func (o *filesOverlay) update(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	w, _ := m.size()
	_, maxOff := scrollRows(len(o.layout(m, w)), m.bodyHeight())
	switch {
	case keyIs(k, "j", "down"):
		o.off = clamp(o.off+1, 0, maxOff)
	case keyIs(k, "k", "up"):
		o.off = clamp(o.off-1, 0, maxOff)
	}
	return false, nil
}

func (o *filesOverlay) view(m *Model, w, h int) string {
	return m.scrollPanel("Files left on disk", o.layout(m, w), o.off, w, h)
}

func (o *filesOverlay) hints(m *Model) []hint {
	w, _ := m.size()
	if _, maxOff := scrollRows(len(o.layout(m, w)), m.bodyHeight()); maxOff > 0 {
		return []hint{{"↑↓ j k", "scroll"}, {"esc", "close"}}
	}
	return []hint{{"esc", "close"}}
}

func (o *helpOverlay) hints(m *Model) []hint {
	w, _ := m.size()
	if _, _, maxOff := o.layout(m, w, m.bodyHeight()); maxOff > 0 {
		return []hint{{"↑↓ j k", "scroll"}, {"esc", "close"}}
	}
	return []hint{{"esc", "close"}}
}

// filterOverlay narrows the list as it is typed. enter and esc both close
// it and keep the filter; to clear it, open / again and ctrl+u.
type filterOverlay struct{ f field }

func (o *filterOverlay) update(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	if k.Type == tea.KeyEnter {
		return true, nil
	}
	o.f = o.f.update(k)
	m.vars.filter = o.f.value()
	m.resetCursor()
	return false, nil
}

// view returns "" so the Model keeps drawing the list under it: the point
// of a live filter is seeing the list narrow. The list's own first line
// shows the field (see listBody).
func (o *filterOverlay) view(*Model, int, int) string { return "" }

func (o *filterOverlay) hints(*Model) []hint {
	return []hint{{"enter", "done"}, {"ctrl+u", "clear"}}
}

// editOverlay edits one value. save returns false to keep the field open,
// having set m.msg to say why.
type editOverlay struct {
	title string
	f     field
	save  func(m *Model, value string) bool
}

func (e *editOverlay) update(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	if k.Type == tea.KeyEnter {
		return e.save(m, e.f.value()), nil
	}
	e.f = e.f.update(k)
	return false, nil
}

func (e *editOverlay) view(m *Model, w, h int) string {
	return m.panel(e.title, []string{" " + e.f.view(m.th)}, w, h)
}

func (e *editOverlay) hints(*Model) []hint {
	return []hint{{"enter", "stage"}, {"ctrl+u", "clear"}, {"esc", "cancel"}}
}

// addOverlay is a: name, then plain or secret, then the first value --
// staged together, as one change the review lists (spec §2).
type addOverlay struct {
	step   int // 0 name, 1 kind, 2 value
	name   string
	secret bool
	f      field
}

func (a *addOverlay) update(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	switch a.step {
	case 0:
		if k.Type != tea.KeyEnter {
			a.f = a.f.update(k)
			return false, nil
		}
		name := strings.TrimSpace(a.f.value())
		switch {
		case name == "":
			m.msg = message{"a variable needs a name", true}
		case strings.ContainsAny(name, " \t="):
			m.msg = message{"a name can't contain spaces or =", true}
		case m.exists(name):
			m.msg = message{name + " already exists", true}
		default:
			a.name, a.step = name, 1
		}
	case 1:
		switch {
		case keyIs(k, "p"):
			a.secret, a.step, a.f = false, 2, newField("", false)
		case keyIs(k, "s"):
			a.secret, a.step, a.f = true, 2, newField("", true)
		}
	case 2:
		if k.Type != tea.KeyEnter {
			a.f = a.f.update(k)
			return false, nil
		}
		val := a.f.value()
		if a.secret && val == "" {
			m.msg = message{"a secret can't be empty -- esc to cancel", true}
			return false, nil
		}
		m.staged.Add(session.Change{Kind: session.ChangeAddVar, Name: a.name})
		if a.secret {
			m.staged.Add(session.Change{Kind: session.ChangePromoteSecret, Name: a.name})
			m.staged.Add(session.Change{Kind: session.ChangeSetSecret, Name: a.name, Profile: m.snap.Profile, Value: val})
		} else {
			m.staged.Add(session.Change{Kind: session.ChangeSetShared, Name: a.name, Value: val})
		}
		m.selectName(a.name)
		m.msg = message{a.name + " added -- u to undo", false}
		return true, nil
	}
	return false, nil
}

func (a *addOverlay) view(m *Model, w, h int) string {
	switch a.step {
	case 0:
		return m.panel("Add variable -- name", []string{" " + a.f.view(m.th)}, w, h)
	case 1:
		return m.panel("Add "+a.name+" -- kind", []string{
			" " + m.th.Key.Render("p") + " " + m.th.Text.Render("plain: one shared value, overridable per profile"),
			" " + m.th.Key.Render("s") + " " + m.th.Text.Render("secret: one keyring item per profile"),
		}, w, h)
	}
	title := "Add " + a.name + " -- shared value"
	if a.secret {
		title = "Add " + a.name + " -- value for " + m.snap.Profile
	}
	return m.panel(title, []string{" " + a.f.view(m.th)}, w, h)
}

func (a *addOverlay) hints(*Model) []hint {
	if a.step == 1 {
		return []hint{{"p", "plain"}, {"s", "secret"}, {"esc", "cancel"}}
	}
	return []hint{{"enter", "next"}, {"esc", "cancel"}}
}

// menuItem is one . entry. A disabled entry stays in the list with its
// reason, so an absent action is never a silent one.
type menuItem struct {
	label, disabled string
	run             func(m *Model) tea.Cmd
}

type menuOverlay struct {
	title  string
	items  []menuItem
	cursor int
}

func (o *menuOverlay) update(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	switch {
	case keyIs(k, "j", "down"):
		o.cursor = min(o.cursor+1, len(o.items)-1)
	case keyIs(k, "k", "up"):
		o.cursor = max(o.cursor-1, 0)
	case keyIs(k, "enter"):
		it := o.items[o.cursor]
		if it.disabled != "" {
			m.msg = message{it.disabled, true}
			return false, nil
		}
		return true, it.run(m)
	}
	return false, nil
}

func (o *menuOverlay) view(m *Model, w, h int) string {
	var body []string
	for i, it := range o.items {
		text, st := it.label, m.th.Text
		if it.disabled != "" {
			text, st = it.label+" (unavailable)", m.th.Muted
		}
		body = append(body, m.cursorRow(i == o.cursor, text, st))
	}
	return m.panel(o.title, body, w, h)
}

// cursorRow is one row of a pick list. The cursor row leads with > as well
// as the selection surface, so it reads without colour too.
func (m *Model) cursorRow(on bool, text string, st lipgloss.Style) string {
	if on {
		return m.th.Accent.Render(">") + " " + m.th.Selected.Render(text)
	}
	return "  " + st.Render(text)
}

func (o *menuOverlay) hints(*Model) []hint { return []hint{{"enter", "choose"}, {"esc", "close"}} }

// infoOverlay is Project info: the facts Overview used to show, as
// reference rather than a screen (spec §1).
type infoOverlay struct{}

func (infoOverlay) update(*Model, tea.KeyMsg) (bool, tea.Cmd) { return false, nil }

func (infoOverlay) view(m *Model, w, h int) string {
	const labelW = 10
	valW := max(8, min(w, panelMax)-4-labelW)
	row := func(label, value string) []string {
		lines := strings.Split(m.th.Text.Width(valW).Render(display(value)), "\n")
		out := []string{m.th.Muted.Render(pad(label, labelW)) + lines[0]}
		for _, l := range lines[1:] {
			out = append(out, strings.Repeat(" ", labelW)+l)
		}
		return out
	}
	var body []string
	body = append(body, row("project", m.snap.Project)...)
	body = append(body, row("profile", m.snap.Profile+" ("+m.snap.Source+")")...)
	body = append(body, row("manifest", m.snap.ManifestPath)...)
	body = append(body, row("worktree", m.snap.Worktree)...)
	body = append(body, row("backend", m.snap.Backend)...)
	return m.panel("Project info", body, w, h)
}

func (infoOverlay) hints(*Model) []hint { return []hint{{"esc", "close"}} }

// switchOverlay picks another registered project.
type switchOverlay struct {
	ids    []string
	cursor int
}

func (o *switchOverlay) update(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	switch {
	case keyIs(k, "j", "down"):
		o.cursor = min(o.cursor+1, len(o.ids)-1)
	case keyIs(k, "k", "up"):
		o.cursor = max(o.cursor-1, 0)
	case keyIs(k, "enter"):
		id := o.ids[o.cursor]
		leave := func(m *Model) tea.Cmd { m.switchTo, m.quit = id, true; return tea.Quit }
		if m.staged.Len() == 0 {
			return true, leave(m)
		}
		m.push(&confirmOverlay{
			title:   "Switch to " + id,
			text:    "Switching discards " + plural(m.staged.Len(), "unsaved change") + ".",
			choices: []choice{{"enter", "switch", leave}},
		})
		return true, nil
	}
	return false, nil
}

func (o *switchOverlay) view(m *Model, w, h int) string {
	var body []string
	for i, id := range o.ids {
		body = append(body, m.cursorRow(i == o.cursor, display(id), m.th.Text))
	}
	return m.panel("Switch project", body, w, h)
}

func (o *switchOverlay) hints(*Model) []hint { return []hint{{"enter", "switch"}, {"esc", "stay"}} }
