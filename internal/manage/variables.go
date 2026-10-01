package manage

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
)

type pane int

const (
	paneList pane = iota
	paneDetail
)

type cellKey struct{ name, profile string }

// variables is the Variables screen's own state.
type variables struct {
	cursor       int // index into Model.names()
	row          int // detail row under the cursor
	pane         pane
	filter       string
	problemsOnly bool
	revealed     map[cellKey]string // cleared whenever the cursor leaves its variable
}

func newVariables() variables { return variables{revealed: map[cellKey]string{}} }

// allNames is every variable the session knows: the snapshot's plus any
// added this session, sorted.
func (m Model) allNames() []string {
	seen := map[string]bool{}
	var all []string
	for _, n := range append(append([]string(nil), m.snap.Matrix.Names...), m.staged.Added()...) {
		if !seen[n] {
			seen[n] = true
			all = append(all, n)
		}
	}
	sort.Strings(all)
	return all
}

// names is allNames narrowed by the filter and the problems-only toggle.
func (m Model) names() []string {
	var out []string
	f := strings.ToLower(m.vars.filter)
	for _, n := range m.allNames() {
		if f != "" && !strings.Contains(strings.ToLower(n), f) {
			continue
		}
		if m.vars.problemsOnly && len(m.problemsFor(n)) == 0 {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (m Model) selected() (string, bool) {
	ns := m.names()
	if len(ns) == 0 {
		return "", false
	}
	return ns[clamp(m.vars.cursor, 0, len(ns)-1)], true
}

func (m Model) varView(name string) session.VarView {
	return m.staged.View(name, m.snap.Matrix.Vars[name])
}

// problemsFor is every snapshot problem about the variable name. An
// "output" problem names an output, not a variable, so it never matches.
func (m Model) problemsFor(name string) []session.Problem {
	var out []session.Problem
	for _, p := range m.snap.Problems {
		if p.Name == name && p.Kind != "output" {
			out = append(out, p)
		}
	}
	return out
}

type detailRow struct {
	profile string // "" is the shared row
	label   string
}

func (m Model) detailRows(v session.VarView) []detailRow {
	var rows []detailRow
	if !v.Var.Secret {
		rows = append(rows, detailRow{"", "shared"})
	}
	for _, p := range m.snap.Matrix.Profiles {
		rows = append(rows, detailRow{p, p})
	}
	return rows
}

func (m *Model) moveCursor(d int) {
	n := len(m.names())
	if n == 0 {
		return
	}
	m.vars.cursor = clamp(m.vars.cursor+d, 0, n-1)
	m.vars.row = 0
	m.vars.revealed = map[cellKey]string{}
}

// variablesKey handles the Variables screen's keys. Task 7 wires movement
// and the panes; Tasks 8 and 9 add the rest to the switch below.
func (m Model) variablesKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	name, ok := m.selected()
	if !ok {
		return m, nil
	}
	if m.vars.pane == paneList {
		switch {
		case keyIs(k, "j", "down"):
			m.moveCursor(1)
		case keyIs(k, "k", "up"):
			m.moveCursor(-1)
		case keyIs(k, "l", "right", "enter"):
			m.vars.pane, m.vars.row = paneDetail, 0
		}
		return m, nil
	}
	rows := m.detailRows(m.varView(name))
	switch {
	case keyIs(k, "j", "down"):
		m.vars.row = min(m.vars.row+1, len(rows)-1)
	case keyIs(k, "k", "up"):
		m.vars.row = max(m.vars.row-1, 0)
	case keyIs(k, "h", "left", "esc"):
		m.vars.pane = paneList
	}
	return m, nil
}

// variablesHints is the footer for the Variables screen. Task 8 extends it.
func (m Model) variablesHints() []hint {
	if m.vars.pane == paneList {
		return []hint{{"enter", "open"}}
	}
	return []hint{{"←", "back"}}
}

func (m Model) viewVariables(w, h int) string {
	if w < stackBelow {
		if m.vars.pane == paneDetail {
			return m.th.Pane(m.detailBody(w-4, h-2), w, h, true)
		}
		return m.th.Pane(m.listBody(w-2, h-2, false), w, h, true)
	}
	lw := m.listWidth(w)
	list := m.th.Pane(m.listBody(lw-2, h-2, w >= wideFrom), lw, h, m.vars.pane == paneList)
	detail := m.th.Pane(m.detailBody(w-lw-4, h-2), w-lw, h, m.vars.pane == paneDetail)
	return lipgloss.JoinHorizontal(lipgloss.Top, list, detail)
}

func pad(s string, w int) string {
	if n := lipgloss.Width(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// cutTo shortens s to w cells, ending in … when it had to cut. Used only
// for list names and the wide preview column; the detail pane never cuts.
func cutTo(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	return lipgloss.NewStyle().MaxWidth(w-1).Render(s) + "…"
}

func (m Model) listBody(inner, height int, wide bool) string {
	t := m.th
	var head []string
	if m.vars.filter != "" {
		head = append(head, " "+t.Muted.Render("filter: ")+t.Text.Render(display(m.vars.filter)))
	}
	if m.vars.problemsOnly {
		head = append(head, " "+t.Muted.Render("problems only -- ! to show all"))
	}
	body := m.listRows(inner, height-len(head), wide)
	return strings.Join(append(head, body), "\n")
}

func (m Model) listRows(inner, height int, wide bool) string {
	t := m.th
	names := m.names()
	if len(names) == 0 {
		msg := "No variables yet -- a to add one"
		if m.vars.filter != "" || m.vars.problemsOnly {
			msg = "Nothing matches -- / to change the filter, ! to show all"
		}
		return " " + t.Muted.Render(msg)
	}
	nameW := inner - (listChrome - 2)
	if wide {
		nameW -= previewWidth + 1
	}
	var lines []string
	cursor := clamp(m.vars.cursor, 0, len(names)-1)
	for i, n := range names {
		v := m.varView(n)
		tag := ""
		if v.Var.Secret {
			tag = "secret"
		}
		mark, markSt := " ", t.Text
		switch {
		case v.Deleted:
			mark, markSt = "x", t.Staged
		case len(m.staged.On(n)) > 0:
			mark, markSt = "~", t.Staged
		case len(m.problemsFor(n)) > 0:
			mark, markSt = "!", t.Danger
		}
		preview := ""
		if wide {
			preview = " " + pad(cutTo(m.previewValue(n, v), previewWidth), previewWidth)
		}
		row := " " + pad(cutTo(n, nameW), nameW) + " " + pad(tag, 6) + " "
		if i == cursor {
			lines = append(lines, t.Selected.Render(row)+markSt.Render(mark)+t.Muted.Render(preview))
			continue
		}
		lines = append(lines, " "+t.Text.Render(pad(cutTo(n, nameW), nameW))+" "+t.Muted.Render(pad(tag, 6))+" "+markSt.Render(mark)+t.Muted.Render(preview))
	}
	return strings.Join(window(lines, cursor, height), "\n")
}

// previewValue is the active profile's value for the wide list column,
// masked for a secret.
func (m Model) previewValue(name string, v session.VarView) string {
	p := m.snap.Profile
	if v.Var.Secret {
		if m.snap.Matrix.Stored[name][p] {
			return "••••••••"
		}
		return "-"
	}
	if val, ok := v.Var.Profiles[p]; ok {
		return display(val)
	}
	if v.Var.Value != nil {
		return display(*v.Var.Value)
	}
	return "-"
}

const (
	labelW = 12
	annotW = 14
)

// cellText is one detail row's value and annotation, already styled.
func (m Model) cellText(name string, v session.VarView, r detailRow) (value, annot string) {
	t := m.th
	if r.profile == "" {
		if v.Var.Value == nil {
			return t.Muted.Render("(none)"), ""
		}
		return t.Title.Render(display(*v.Var.Value)), ""
	}
	if v.Var.Secret {
		key := cellKey{name, r.profile}
		switch {
		case v.NewSecret[r.profile]:
			return t.Muted.Render("(new value)"), t.Staged.Render("~ staged")
		case m.vars.revealed[key] != "":
			return t.Text.Render(display(m.vars.revealed[key])), t.OK.Render("stored")
		case m.snap.Matrix.Stored[name][r.profile]:
			return t.Text.Render("••••••••"), t.OK.Render("stored")
		case v.Var.Optional:
			return t.Muted.Render("unset"), t.Muted.Render("optional")
		}
		return t.Muted.Render("-"), t.Danger.Render("MISSING !")
	}
	if val, ok := v.Var.Profiles[r.profile]; ok {
		return t.Title.Render(display(val)), t.Text.Render("override")
	}
	if v.Var.Value != nil {
		return t.Text.Render(display(*v.Var.Value)), t.Muted.Render("from shared")
	}
	if v.Var.Optional {
		return t.Muted.Render("unset"), t.Muted.Render("optional")
	}
	return t.Muted.Render("-"), t.Danger.Render("MISSING !")
}

func (m Model) detailBody(inner, height int) string {
	t := m.th
	name, ok := m.selected()
	if !ok {
		return " " + t.Muted.Render("Nothing selected")
	}
	v := m.varView(name)
	kind := "plain"
	if v.Var.Secret {
		kind = "secret"
	}
	req := "required"
	if v.Var.Optional {
		req = "optional"
	}
	meta := kind + " · " + req
	switch {
	case v.Deleted:
		meta = "x marked for deletion · " + meta
	case v.Added:
		meta = "~ new · " + meta
	}
	head := t.Title.Render(cutTo(name, max(1, inner-lipgloss.Width(meta)-1)))
	lines := []string{head + strings.Repeat(" ", max(1, inner-lipgloss.Width(head)-lipgloss.Width(meta))) + t.Muted.Render(meta), ""}

	valueW := max(8, inner-labelW-annotW-1)
	focusLine := 0
	for i, r := range m.detailRows(v) {
		label := r.label
		if r.profile == m.snap.Profile {
			label += " " + t.Accent.Render("*")
		}
		value, annot := m.cellText(name, v, r)
		if len(m.staged.OnCell(name, r.profile)) > 0 && !v.NewSecret[r.profile] {
			annot += " " + t.Staged.Render("~")
		}
		wrapped := strings.Split(lipgloss.NewStyle().Width(valueW).Render(value), "\n")
		if i == m.vars.row && m.vars.pane == paneDetail {
			focusLine = len(lines)
			lines = append(lines, t.Selected.Render(pad(r.label, labelW)+pad(stripStyles(wrapped[0]), valueW))+" "+annot)
		} else {
			lines = append(lines, pad(t.Muted.Render(pad(label, labelW)), labelW)+pad(wrapped[0], valueW)+" "+annot)
		}
		for _, cont := range wrapped[1:] {
			lines = append(lines, strings.Repeat(" ", labelW)+cont)
		}
		if r.profile == "" {
			lines = append(lines, "")
		}
	}
	lines = append(lines, "")
	wrapTo := lipgloss.NewStyle().Width(max(8, inner-2))
	for _, p := range m.problemsFor(name) {
		for i, l := range strings.Split(wrapTo.Render(t.Text.Render(problemLine(p, v.Var))), "\n") {
			lead := "  "
			if i == 0 {
				lead = t.Danger.Render("! ")
			}
			lines = append(lines, lead+l)
		}
	}
	written := t.Muted.Render("no output -- exec and shell still see it")
	if outs := m.snap.OutputsFor[name]; len(outs) > 0 {
		var parts []string
		for _, o := range outs {
			parts = append(parts, t.Text.Render(o)+t.Muted.Render(" ("+m.snap.OutputPaths[o]+")"))
		}
		written = strings.Join(parts, t.Muted.Render(", "))
	}
	for i, l := range strings.Split(lipgloss.NewStyle().Width(max(8, inner-labelW)).Render(written), "\n") {
		lead := strings.Repeat(" ", labelW)
		if i == 0 {
			lead = t.Muted.Render(pad("written to", labelW))
		}
		lines = append(lines, lead+l)
	}
	for i := range lines {
		lines[i] = " " + lines[i]
	}
	return strings.Join(window(lines, focusLine, height), "\n")
}

// problemLine words a problem for the interface, with an in-app fix (spec
// §2) -- never session.Problem.Fix, which names the old interface's keys.
func problemLine(p session.Problem, v manifest.Var) string {
	switch p.Kind {
	case "missing":
		verb := "set it"
		if v.Secret {
			verb = "store it"
		}
		return "no value under " + p.Profile + " -- enter on " + p.Profile + " to " + verb
	case "membership":
		return "no output writes it"
	}
	return p.Detail
}

// stripStyles removes lipgloss styling so a selected row can be redrawn on
// the selection surface in one style; semantic marks stay outside it (spec
// §0 rule).
func stripStyles(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			inEsc = false
		case !inEsc:
			b.WriteRune(r)
		}
	}
	return b.String()
}
