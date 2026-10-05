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
	cursor int // index into Model.names()
	// at is the detail row under the cursor, by profile ("" the shared
	// row). It is a profile, not an index, because the rows change under
	// it: Make secret drops the shared row and dropping that promotion
	// brings it back. rowIndex turns it into an index that is always in
	// range.
	at           string
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
// A missing value that a staged value will fill is left out: the cell
// already reads staged, and the problem goes on save.
func (m Model) problemsFor(name string) []session.Problem {
	var out []session.Problem
	for _, p := range m.snap.Problems {
		if p.Name == name && p.Kind != "output" && !(p.Kind == "missing" && m.valueStaged(name, p.Profile)) {
			out = append(out, p)
		}
	}
	return out
}

// valueStaged reports whether a staged change gives name a value under
// profile: a secret or an override for that profile, or a shared value.
func (m Model) valueStaged(name, profile string) bool {
	for _, c := range m.staged.On(name) {
		switch c.Kind {
		case session.ChangeSetShared:
			return true
		case session.ChangeSetSecret, session.ChangeSetValue:
			if c.Profile == profile {
				return true
			}
		}
	}
	return false
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

// rowIndex is the detail cursor's index in rows: the row for vars.at, or
// the first row when that row is gone (a secret has no shared row).
func (m Model) rowIndex(rows []detailRow) int {
	for i, r := range rows {
		if r.profile == m.vars.at {
			return i
		}
	}
	return 0
}

// resetCursor returns to the first row of the list, hiding anything
// revealed: the selection has changed under the user.
func (m *Model) resetCursor() {
	m.vars.cursor, m.vars.at, m.vars.pane = 0, "", paneList
	m.vars.revealed = map[cellKey]string{}
}

func (m *Model) moveCursor(d int) {
	n := len(m.names())
	if n == 0 {
		return
	}
	m.vars.cursor = clamp(m.vars.cursor+d, 0, n-1)
	m.vars.at = ""
	m.vars.revealed = map[cellKey]string{}
}

// variablesKey handles the Variables screen's keys, including every
// staging action.
func (m Model) variablesKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyIs(k, "/"):
		m.push(&filterOverlay{f: newField(m.vars.filter, false)})
		return m, nil
	case keyIs(k, "!"):
		m.vars.problemsOnly = !m.vars.problemsOnly
		m.resetCursor()
		return m, nil
	case keyIs(k, "a"):
		if m.canStage() {
			m.push(&addOverlay{f: newField("", false)})
		}
		return m, nil
	case keyIs(k, "."):
		m.push(m.varMenu())
		return m, nil
	}
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
			m.vars.pane, m.vars.at = paneDetail, ""
		case keyIs(k, "d"):
			m.toggleDelete(name)
		case keyIs(k, "u"):
			m.undoVar(name)
		}
		return m, nil
	}
	rows := m.detailRows(m.varView(name))
	if len(rows) == 0 {
		m.vars.pane = paneList
		return m, nil
	}
	i := m.rowIndex(rows)
	at := rows[i].profile
	switch {
	case keyIs(k, "j", "down"):
		m.vars.at = rows[min(i+1, len(rows)-1)].profile
	case keyIs(k, "k", "up"):
		m.vars.at = rows[max(i-1, 0)].profile
	case keyIs(k, "h", "left", "esc"):
		m.vars.pane = paneList
	case keyIs(k, "v"):
		return m.toggleReveal(name, at)
	case keyIs(k, "enter"):
		return m.beginEdit(name, at)
	case keyIs(k, "d"):
		m.unsetRow(name, at)
	case keyIs(k, "u"):
		m.undoCell(name, at)
	}
	return m, nil
}

// toggleReveal shows or hides one secret cell's real value. Only this path
// calls Get, so opening the interface never raises a keyring prompt.
func (m Model) toggleReveal(name, profile string) (tea.Model, tea.Cmd) {
	key := cellKey{name, profile}
	if _, ok := m.vars.revealed[key]; ok {
		delete(m.vars.revealed, key)
		return m, nil
	}
	v := m.varView(name)
	if !v.Var.Secret || !m.snap.Matrix.Stored[name][profile] || v.NewSecret[profile] {
		m.msg = message{"nothing stored to reveal under " + profile, false}
		return m, nil
	}
	reveal, gen := m.deps.Reveal, m.gen
	return m, func() tea.Msg {
		val, err := reveal(name, profile)
		if err != nil {
			return revealFailedMsg{err}
		}
		return revealedMsg{name, profile, val, gen}
	}
}

// variablesHints is the footer for the Variables screen: only keys that do
// something for the selection right now (spec §1).
func (m Model) variablesHints() []hint {
	name, ok := m.selected()
	if !ok {
		return []hint{{"a", "add"}, {"/", "filter"}, {"!", "problems"}}
	}
	v := m.varView(name)
	if m.vars.pane == paneList {
		hs := []hint{{"enter", "open"}}
		if v.Deleted {
			hs = append(hs, hint{"d", "restore"})
		} else {
			hs = append(hs, hint{"d", "delete"})
		}
		if len(m.staged.On(name)) > 0 {
			hs = append(hs, hint{"u", "undo"})
		}
		return append(hs, hint{"a", "add"}, hint{".", "more"}, hint{"/", "filter"})
	}
	rows := m.detailRows(v)
	if len(rows) == 0 {
		return []hint{{"←", "back"}, {".", "more"}}
	}
	r := rows[m.rowIndex(rows)]
	verb := "edit"
	if v.Var.Secret {
		verb = "store"
	}
	hs := []hint{{"enter", verb}}
	if v.Var.Secret && !v.NewSecret[r.profile] && m.snap.Matrix.Stored[name][r.profile] {
		if _, shown := m.vars.revealed[cellKey{name, r.profile}]; shown {
			hs = append(hs, hint{"v", "hide"})
		} else {
			hs = append(hs, hint{"v", "reveal"})
		}
	}
	if m.removable(v, r) {
		hs = append(hs, hint{"d", "remove"})
	}
	if len(m.staged.OnCell(name, r.profile)) > 0 {
		hs = append(hs, hint{"u", "undo"})
	}
	return append(hs, hint{"←", "back"}, hint{".", "more"})
}

// removable reports whether d on this detail row removes something: an
// override, or a shared value, that is not the variable's only value.
func (m Model) removable(v session.VarView, r detailRow) bool {
	if v.Var.Secret || v.Deleted || onlyValue(v, r.profile) {
		return false
	}
	if r.profile != "" {
		_, ok := v.Var.Profiles[r.profile]
		return ok
	}
	return v.Var.Value != nil
}

// onlyValue reports whether the value on row profile ("" the shared value)
// is a required plain variable's last one. Removing it would leave the
// variable with no value, which the manifest refuses.
func onlyValue(v session.VarView, profile string) bool {
	if v.Var.Optional {
		return false
	}
	if profile == "" {
		return v.Var.Value != nil && len(v.Var.Profiles) == 0
	}
	_, ok := v.Var.Profiles[profile]
	return ok && v.Var.Value == nil && len(v.Var.Profiles) == 1
}

func (m Model) viewVariables(w, h int) string {
	if w < stackBelow {
		if m.vars.pane == paneDetail {
			return m.th.Pane(m.detailBody(w-5, h-2), w, h, true)
		}
		return m.th.Pane(m.listBody(w-2, h-2, false), w, h, true)
	}
	lw := m.listWidth(w)
	list := m.th.Pane(m.listBody(lw-2, h-2, w >= wideFrom), lw, h, m.vars.pane == paneList)
	detail := m.th.Pane(m.detailBody(w-lw-5, h-2), w-lw, h, m.vars.pane == paneDetail)
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
	if f, ok := m.top().(*filterOverlay); ok {
		head = append(head, " "+t.Accent.Render("/ ")+f.f.view(t, inner-4))
	} else if m.vars.filter != "" {
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
	// The cursor column shows the selection without colour: > where the
	// keys go, a muted · when the detail pane has them.
	cur := t.Accent.Render(">")
	if m.vars.pane != paneList {
		cur = t.Muted.Render("·")
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
			lines = append(lines, cur+t.Selected.Render(row)+markSt.Render(mark)+t.Muted.Render(preview))
			continue
		}
		lines = append(lines, "  "+t.Text.Render(pad(cutTo(n, nameW), nameW))+" "+t.Muted.Render(pad(tag, 6))+" "+markSt.Render(mark)+t.Muted.Render(preview))
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

// The detail pane's label column is sized to the labels it holds, within
// labelMin..labelMax; annotW is the room kept for an annotation.
const (
	labelMin = 12
	labelMax = 24
	annotW   = 14
)

// labelWidth is the detail pane's label column. Profile names have no
// length limit, so it fits the longest label -- a profile with room for
// " *", or "written to" -- plus one separating space. It is clamped, and
// never so wide that the value and annotation would be pushed past inner,
// where the pane would cut them; a label that still does not fit is cut
// instead (detailLabel).
func (m Model) labelWidth(inner int) int {
	need := lipgloss.Width("written to") + 1
	for _, p := range m.snap.Matrix.Profiles {
		need = max(need, lipgloss.Width(p)+3)
	}
	return clamp(need, labelMin, min(labelMax, inner-annotW-1-8))
}

// detailLabel is r's label cut to leave a space before the value in a
// column labelW wide, and whether it carries the active profile's *.
func (m Model) detailLabel(r detailRow, labelW int) (string, bool) {
	star := r.profile != "" && r.profile == m.snap.Profile
	room := labelW - 1
	if star {
		room -= 2
	}
	return cutTo(r.label, max(1, room)), star
}

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

// detailBody draws the selected variable. inner is the width its text may
// use: the pane's inside less the two-cell cursor lead and a one-cell gap
// before the right border.
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

	labelW := m.labelWidth(inner)
	valueW := max(8, inner-labelW-annotW-1)
	focused := m.vars.pane == paneDetail
	cursorAt := -1
	rows := m.detailRows(v)
	at := m.rowIndex(rows)
	for i, r := range rows {
		short, star := m.detailLabel(r, labelW)
		label := short
		if star {
			label += " " + t.Accent.Render("*")
		}
		value, annot := m.cellText(name, v, r)
		if len(m.staged.OnCell(name, r.profile)) > 0 && !v.NewSecret[r.profile] {
			annot += " " + t.Staged.Render("~")
		}
		wrapped := strings.Split(lipgloss.NewStyle().Width(valueW).Render(value), "\n")
		if i == at && focused {
			cursorAt = len(lines)
			// The * stays: accent clears 4.5:1 on the selection surface
			// (spec §0), so it is drawn on it rather than dropped.
			sel := t.Selected
			lab := sel.Render(short)
			if star {
				lab += sel.Render(" ") + t.Accent.Inherit(sel).Render("*")
			}
			fill := strings.Repeat(" ", max(0, labelW-lipgloss.Width(lab)))
			lines = append(lines, lab+sel.Render(fill+pad(stripStyles(wrapped[0]), valueW))+" "+annot)
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
	// A two-cell lead: the cursor column and a space, as in the list.
	for i := range lines {
		lead := "  "
		if i == cursorAt {
			lead = t.Accent.Render(">") + " "
		}
		lines[i] = lead + lines[i]
	}
	return strings.Join(window(lines, max(cursorAt, 0), height), "\n")
}

// problemLine words a problem for the interface, with an in-app fix (spec
// §2) -- never session.Problem.Fix, which is worded for outside it: a
// command, or how to reach the screen.
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
