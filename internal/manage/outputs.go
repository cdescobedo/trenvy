package manage

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/session"
)

// outputs is the Outputs screen's own state.
type outputs struct {
	cursor  int
	at      string // detail row: "format", "path", or "var:<NAME>"
	pane    pane
	checked map[string]string // Check now results by identity, until the next reload
}

func newOutputs() outputs { return outputs{checked: map[string]string{}} }

// outputIDs is every output the session knows, by identity: its name at
// the start of the session, or the name it was added under. A staged
// rename changes what is drawn, never the identity (session.Staged).
func (m Model) outputIDs() []string {
	seen := map[string]bool{}
	var ids []string
	for id := range m.snap.Matrix.Outputs {
		seen[id] = true
		ids = append(ids, id)
	}
	for _, id := range m.staged.AddedOutputs() {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (m Model) selectedOutput() (string, bool) {
	ids := m.outputIDs()
	if len(ids) == 0 {
		return "", false
	}
	return ids[clamp(m.outs.cursor, 0, len(ids)-1)], true
}

func (m Model) outView(id string) session.OutputView {
	return m.staged.OutputView(id, m.snap.Matrix.Outputs[id], m.snap.DefaultOutput)
}

// selectOutput moves the cursor to id.
func (m *Model) selectOutput(id string) {
	for i, x := range m.outputIDs() {
		if x == id {
			m.outs.cursor, m.outs.at, m.outs.pane = i, "", paneList
			return
		}
	}
}

// displayName is id, or "id -> new" while a rename is staged.
func displayName(id string, v session.OutputView) string {
	if v.RenamedTo != "" {
		return id + " -> " + v.RenamedTo
	}
	return id
}

// outputState is the state word, its gloss and its style (spec §4). The
// word is `trenvy status`'s own; only quick mode's skip is renamed, to
// "unverified". A Check now result replaces the snapshot's until a reload.
func (m Model) outputState(id string, v session.OutputView) (word, gloss string, st lipgloss.Style) {
	t := m.th
	if v.Added {
		return "new", "not saved yet", t.Staged
	}
	s := m.snap.OutputStates[id]
	if c, ok := m.outs.checked[id]; ok {
		s = c
	}
	switch s {
	case session.StateFresh:
		return s, "up to date with the manifest", t.OK
	case session.StateAbsent:
		return s, "not rendered in this worktree", t.Muted
	case session.StateStale:
		return s, "out of date -- render to update it", t.Warn
	case session.StateModified:
		return s, "edited by hand since the last render", t.Warn
	case session.StateForeign:
		return s, "a file trenvy did not write is at this path", t.Danger
	case session.StateUnverified:
		return "unverified", "not checked, holds secrets -- . then Check now", t.Muted
	}
	return session.StateUnknown, "can't tell -- a secret is missing or the file can't be read", t.Danger
}

func (m Model) outputProblem(id string) bool {
	for _, p := range m.snap.Problems {
		if p.Kind == "output" && p.Name == id {
			return true
		}
	}
	return false
}

// outRow is a detail-pane row the cursor can stop on.
type outRow struct {
	key   string // "format", "path", or "var:<NAME>"
	label string
}

func (m Model) outRows() []outRow {
	rows := []outRow{{"format", "format"}, {"path", "path"}}
	for _, n := range m.allNames() {
		rows = append(rows, outRow{"var:" + n, n})
	}
	return rows
}

func (m Model) outRowIndex(rows []outRow) int {
	for i, r := range rows {
		if r.key == m.outs.at {
			return i
		}
	}
	return 0
}

func (m *Model) moveOutput(d int) {
	n := len(m.outputIDs())
	if n == 0 {
		return
	}
	m.outs.cursor = clamp(m.outs.cursor+d, 0, n-1)
	m.outs.at = ""
}

// outputsKey handles the Outputs screen's keys.
func (m Model) outputsKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyIs(k, "a"):
		m.beginAddOutput()
		return m, nil
	case keyIs(k, "."):
		m.push(m.outMenu())
		return m, nil
	}
	id, ok := m.selectedOutput()
	if !ok {
		return m, nil
	}
	if m.outs.pane == paneList {
		switch {
		case keyIs(k, "j", "down"):
			m.moveOutput(1)
		case keyIs(k, "k", "up"):
			m.moveOutput(-1)
		case keyIs(k, "l", "right", "enter"):
			m.outs.pane, m.outs.at = paneDetail, ""
		case keyIs(k, "d"):
			m.toggleRemoveOutput(id)
		case keyIs(k, "u"):
			m.undoOutput(id)
		}
		return m, nil
	}
	rows := m.outRows()
	i := m.outRowIndex(rows)
	switch {
	case keyIs(k, "j", "down"):
		m.outs.at = rows[min(i+1, len(rows)-1)].key
	case keyIs(k, "k", "up"):
		m.outs.at = rows[max(i-1, 0)].key
	case keyIs(k, "h", "left", "esc"):
		m.outs.pane = paneList
	case keyIs(k, "enter"):
		return m.beginOutputEdit(id, rows[i].key)
	case keyIs(k, " ") && strings.HasPrefix(rows[i].key, "var:"):
		m.toggleMember(id, strings.TrimPrefix(rows[i].key, "var:"))
	case keyIs(k, "u"):
		m.undoOutputRow(id, rows[i].key)
	}
	return m, nil
}

// outputsHints is the Outputs footer, most important first.
func (m Model) outputsHints() []hint {
	id, ok := m.selectedOutput()
	if !ok {
		return []hint{{"a", "add"}}
	}
	v := m.outView(id)
	if m.outs.pane == paneList {
		hs := []hint{{"enter", "open"}}
		if v.Removed {
			hs = append(hs, hint{"d", "restore"})
		} else {
			hs = append(hs, hint{"d", "remove"})
		}
		if len(m.staged.OnOutput(id)) > 0 {
			hs = append(hs, hint{"u", "undo"})
		}
		return append(hs, hint{"a", "add"}, hint{".", "more"})
	}
	rows := m.outRows()
	r := rows[m.outRowIndex(rows)]
	var hs []hint
	if strings.HasPrefix(r.key, "var:") {
		hs = append(hs, hint{"space", "toggle"})
		if v.Changed["globs"] {
			hs = append(hs, hint{"u", "undo"})
		}
	} else {
		hs = append(hs, hint{"enter", "edit"})
		if v.Changed[r.key] {
			hs = append(hs, hint{"u", "undo"})
		}
	}
	return append(hs, hint{"←", "back"}, hint{".", "more"})
}

const (
	outTagW   = 7  // "default"
	outStateW = 12 // "not rendered"
	// outChrome is a list row less its name: two borders, the cursor
	// column and a space, then a space, the tag, a space, the state, a
	// space and the mark.
	outChrome  = 2 + 2 + 1 + outTagW + 1 + outStateW + 1 + 1
	outListMin = 32
	outListMax = 52
)

func (m Model) outListWidth() int {
	longest := 0
	for _, id := range m.outputIDs() {
		longest = max(longest, lipgloss.Width(displayName(id, m.outView(id))))
	}
	return clamp(longest+outChrome, outListMin, outListMax)
}

func (m Model) viewOutputs(w, h int) string {
	if w < stackBelow {
		if m.outs.pane == paneDetail {
			return m.th.Pane(m.outDetailBody(w-5, h-2), w, h, true)
		}
		return m.th.Pane(m.outListBody(w-2, h-2), w, h, true)
	}
	lw := min(m.outListWidth(), w/2)
	list := m.th.Pane(m.outListBody(lw-2, h-2), lw, h, m.outs.pane == paneList)
	detail := m.th.Pane(m.outDetailBody(w-lw-5, h-2), w-lw, h, m.outs.pane == paneDetail)
	return lipgloss.JoinHorizontal(lipgloss.Top, list, detail)
}

func (m Model) outListBody(inner, height int) string {
	t := m.th
	ids := m.outputIDs()
	if len(ids) == 0 {
		return " " + t.Muted.Render("No outputs yet -- a to add one")
	}
	nameW := max(4, inner-(outChrome-2))
	cur := t.Accent.Render(">")
	if m.outs.pane != paneList {
		cur = t.Muted.Render("·")
	}
	cursor := clamp(m.outs.cursor, 0, len(ids)-1)
	var lines []string
	for i, id := range ids {
		v := m.outView(id)
		tag := ""
		if v.Default {
			tag = "default"
		}
		word, _, wst := m.outputState(id, v)
		mark, markSt := " ", t.Text
		switch {
		case v.Removed:
			mark, markSt = "x", t.Staged
		case len(m.staged.OnOutput(id)) > 0:
			mark, markSt = "~", t.Staged
		case m.outputProblem(id):
			mark, markSt = "!", t.Danger
		}
		name := pad(cutTo(display(displayName(id, v)), nameW), nameW)
		if i == cursor {
			lines = append(lines, cur+t.Selected.Render(" "+name+" "+pad(tag, outTagW)+" ")+wst.Render(pad(word, outStateW))+" "+markSt.Render(mark))
			continue
		}
		lines = append(lines, "  "+t.Text.Render(name)+" "+t.Muted.Render(pad(tag, outTagW))+" "+wst.Render(pad(word, outStateW))+" "+markSt.Render(mark))
	}
	return strings.Join(window(lines, cursor, height), "\n")
}

// outDetailBody draws the selected output: its facts, its state with a
// gloss, the future-variables sentence, and one checkbox per variable.
// Every value wraps under its own column; nothing is cut.
func (m Model) outDetailBody(inner, height int) string {
	t := m.th
	id, ok := m.selectedOutput()
	if !ok {
		return " " + t.Muted.Render("Nothing selected")
	}
	v := m.outView(id)
	meta := v.Output.Format
	if v.Default {
		meta += " · default"
	}
	switch {
	case v.Removed:
		meta = "x marked for removal · " + meta
	case v.Added:
		meta = "~ new · " + meta
	}
	// The meta sits right of the name when both fit. Otherwise it wraps
	// on its own lines under the name: "x marked for removal · dotenv ·
	// default" alone is wider than the detail pane at 40 columns, and the
	// staged state is the part that must not be lost.
	title := display(displayName(id, v))
	var lines []string
	if lipgloss.Width(title)+1+lipgloss.Width(meta) <= inner {
		gap := strings.Repeat(" ", inner-lipgloss.Width(title)-lipgloss.Width(meta))
		lines = append(lines, t.Title.Render(title)+gap+t.Muted.Render(meta))
	} else {
		lines = append(lines, t.Title.Render(cutTo(title, inner)))
		for _, p := range wrap(meta, inner) {
			lines = append(lines, t.Muted.Render(p))
		}
	}
	lines = append(lines, "")

	const labelW = 14 // "new variables" + a space
	valueW := max(8, inner-labelW-2)
	focused := m.outs.pane == paneDetail
	rows := m.outRows()
	at := m.outRowIndex(rows)
	cursorAt := -1
	field := func(key, label, value string, changed bool) {
		parts := wrap(display(value), valueW)
		mark := ""
		if changed {
			mark = " " + t.Staged.Render("~")
		}
		if focused && rows[at].key == key {
			cursorAt = len(lines)
		}
		lines = append(lines, t.Muted.Render(pad(label, labelW))+t.Text.Render(parts[0])+mark)
		for _, p := range parts[1:] {
			lines = append(lines, strings.Repeat(" ", labelW)+t.Text.Render(p))
		}
	}
	field("format", "format", v.Output.Format, v.Changed["format"])
	field("path", "path", v.Output.Path, v.Changed["path"])

	// The gloss wraps beside the word, or under it when the pane is too
	// narrow to leave it a useful column.
	word, gloss, wst := m.outputState(id, v)
	lead := labelW + lipgloss.Width(word) + 2
	if valueW-lipgloss.Width(word)-2 < 8 {
		lines = append(lines, t.Muted.Render(pad("state", labelW))+wst.Render(word))
		lead = labelW
	}
	for i, p := range wrap(gloss, valueW-(lead-labelW)) {
		if i == 0 && lead > labelW {
			lines = append(lines, t.Muted.Render(pad("state", labelW))+wst.Render(word)+"  "+t.Muted.Render(p))
			continue
		}
		lines = append(lines, strings.Repeat(" ", lead)+t.Muted.Render(p))
	}

	future := "included automatically"
	if session.InferGlobMode(v.Output.Include) == session.GlobsOnlyThese {
		future = "not included -- only the checked ones"
	}
	lines = append(lines, "")
	for i, p := range wrap(future, valueW) {
		lead := strings.Repeat(" ", labelW)
		if i == 0 {
			lead = t.Muted.Render(pad("new variables", labelW))
		}
		lines = append(lines, lead+t.Text.Render(p))
	}
	lines = append(lines, "")

	names := m.allNames()
	checked := session.Checked(v.Output.Include, v.Output.Exclude, names)
	base := m.snap.Matrix.Outputs[id]
	onDisk := session.Checked(base.Include, base.Exclude, names)
	for _, n := range names {
		box := "[ ]"
		if checked[n] {
			box = "[x]"
		}
		mark := ""
		if !v.Added && checked[n] != onDisk[n] {
			mark = " " + t.Staged.Render("~")
		}
		if focused && rows[at].key == "var:"+n {
			cursorAt = len(lines)
		}
		lines = append(lines, t.Text.Render(box+" "+cutTo(display(n), max(4, inner-6)))+mark)
	}

	for i := range lines {
		lead := "  "
		if i == cursorAt {
			lead = t.Accent.Render(">") + " "
		}
		lines[i] = lead + lines[i]
	}
	return strings.Join(window(lines, max(cursorAt, 0), height), "\n")
}
