package manage

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/session"
	"github.com/cdescobedo/trenvy/internal/style"
)

// profiles is the Profiles screen's own state (spec §5).
type profiles struct {
	cursor int
	at     string // detail row: "" or "miss:<NAME>"
	pane   pane
}

// profileIDs is every profile the session knows: the manifest's, in its
// order, then any added this session.
func (m Model) profileIDs() []string {
	ids := append([]string(nil), m.snap.Matrix.Profiles...)
	seen := map[string]bool{}
	for _, p := range ids {
		seen[p] = true
	}
	for _, p := range m.staged.AddedProfiles() {
		if !seen[p] {
			ids = append(ids, p)
		}
	}
	return ids
}

func (m Model) selectedProfile() (string, bool) {
	ids := m.profileIDs()
	if len(ids) == 0 {
		return "", false
	}
	return ids[clamp(m.profs.cursor, 0, len(ids)-1)], true
}

func (m *Model) selectProfile(p string) {
	for i, x := range m.profileIDs() {
		if x == p {
			m.profs.cursor, m.profs.at, m.profs.pane = i, "", paneList
			return
		}
	}
}

func (m Model) profView(p string) session.ProfileView {
	return m.staged.ProfileView(p, m.snap.DefaultProfile, m.snap.Active)
}

// missingFor is every variable with no value under p, by the snapshot's
// problems, less those a staged value will fill and those staged for
// deletion.
func (m Model) missingFor(p string) []string {
	var out []string
	for _, pr := range m.snap.Problems {
		if pr.Kind == "missing" && pr.Profile == p && !m.valueStaged(pr.Name, p) && !m.varView(pr.Name).Deleted {
			out = append(out, pr.Name)
		}
	}
	sort.Strings(out)
	return out
}

// profileCounts is p's override count and its secrets stored out of the
// total, as they will read after save.
func (m Model) profileCounts(p string) (overrides, stored, secrets int) {
	for _, n := range m.allNames() {
		v := m.varView(n)
		if v.Deleted {
			continue
		}
		if v.Var.Secret {
			secrets++
			if v.NewSecret[p] || m.snap.Matrix.Stored[n][p] {
				stored++
			}
			continue
		}
		if _, ok := v.Var.Profiles[p]; ok {
			overrides++
		}
	}
	return
}

func profTags(v session.ProfileView) []string {
	var tags []string
	if v.Default {
		tags = append(tags, "default")
	}
	if v.Active {
		tags = append(tags, "active")
	}
	return tags
}

func (m Model) profRows(p string) []string {
	var rows []string
	if m.profView(p).Added {
		return nil
	}
	for _, n := range m.missingFor(p) {
		rows = append(rows, "miss:"+n)
	}
	return rows
}

func (m Model) profRowIndex(rows []string) int {
	for i, r := range rows {
		if r == m.profs.at {
			return i
		}
	}
	return 0
}

func (m *Model) moveProfile(d int) {
	n := len(m.profileIDs())
	if n == 0 {
		return
	}
	m.profs.cursor = clamp(m.profs.cursor+d, 0, n-1)
	m.profs.at = ""
}

// profilesKey is the Profiles screen's keys.
func (m Model) profilesKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyIs(k, "a"):
		m.beginAddProfile()
		return m, nil
	case keyIs(k, "."):
		m.push(m.profMenu())
		return m, nil
	}
	p, ok := m.selectedProfile()
	if !ok {
		return m, nil
	}
	if m.profs.pane == paneList {
		switch {
		case keyIs(k, "j", "down"):
			m.moveProfile(1)
		case keyIs(k, "k", "up"):
			m.moveProfile(-1)
		case keyIs(k, "l", "right", "enter"):
			m.profs.pane, m.profs.at = paneDetail, ""
		case keyIs(k, "d"):
			m.toggleRemoveProfile(p)
		case keyIs(k, "u"):
			m.undoProfile(p)
		}
		return m, nil
	}
	rows := m.profRows(p)
	switch {
	case keyIs(k, "j", "down") && len(rows) > 0:
		m.profs.at = rows[min(m.profRowIndex(rows)+1, len(rows)-1)]
	case keyIs(k, "k", "up") && len(rows) > 0:
		m.profs.at = rows[max(m.profRowIndex(rows)-1, 0)]
	case keyIs(k, "h", "left", "esc"):
		m.profs.pane = paneList
	case keyIs(k, "enter") && len(rows) > 0:
		m.jumpToCell(strings.TrimPrefix(rows[m.profRowIndex(rows)], "miss:"), p)
	}
	return m, nil
}

func (m Model) profilesHints() []hint {
	p, ok := m.selectedProfile()
	if !ok {
		return []hint{{"a", "add"}}
	}
	if m.profs.pane == paneList {
		hs := []hint{{"enter", "open"}}
		if m.profView(p).Removed {
			hs = append(hs, hint{"d", "keep"})
		} else {
			hs = append(hs, hint{"d", "remove"})
		}
		if len(m.staged.OnProfile(p)) > 0 {
			hs = append(hs, hint{"u", "undo"})
		}
		return append(hs, hint{"a", "add"}, hint{".", "more"})
	}
	var hs []hint
	if len(m.profRows(p)) > 0 {
		hs = append(hs, hint{"enter", "jump"})
	}
	return append(hs, hint{"←", "back"}, hint{".", "more"})
}

const (
	profNoteW = 16 // "default · active"
	// profChrome is a list row less its name: two borders, the cursor
	// column and a space, a space, the note, a space and the mark.
	profChrome  = 2 + 2 + 1 + profNoteW + 1 + 1
	profListMin = 28
	profListMax = 48
)

func (m Model) profListWidth() int {
	longest := 0
	for _, p := range m.profileIDs() {
		longest = max(longest, lipgloss.Width(p))
	}
	return clamp(longest+profChrome, profListMin, profListMax)
}

func (m Model) viewProfiles(w, h int) string {
	if w < stackBelow {
		if m.profs.pane == paneDetail {
			return m.th.Pane(m.profDetailBody(w-5, h-2), w, h, true)
		}
		return m.th.Pane(m.profListBody(w-2, h-2), w, h, true)
	}
	lw := min(m.profListWidth(), w/2)
	list := m.th.Pane(m.profListBody(lw-2, h-2), lw, h, m.profs.pane == paneList)
	detail := m.th.Pane(m.profDetailBody(w-lw-5, h-2), w-lw, h, m.profs.pane == paneDetail)
	return lipgloss.JoinHorizontal(lipgloss.Top, list, detail)
}

func (m Model) profListBody(inner, height int) string {
	t := m.th
	ids := m.profileIDs()
	nameW := max(4, inner-(profChrome-2))
	cur := t.Accent.Render(">")
	if m.profs.pane != paneList {
		cur = t.Muted.Render("·")
	}
	cursor := clamp(m.profs.cursor, 0, len(ids)-1)
	var lines []string
	for i, p := range ids {
		v := m.profView(p)
		missing := len(m.missingFor(p))
		note, noteSt := strings.Join(profTags(v), " · "), t.Muted
		if note == "" && missing > 0 && !v.Added {
			note, noteSt = fmt.Sprintf("%d missing", missing), t.Danger
		}
		mark, markSt := " ", t.Text
		switch {
		case v.Removed:
			mark, markSt = "x", t.Staged
		case len(m.staged.OnProfile(p)) > 0:
			mark, markSt = "~", t.Staged
		case missing > 0:
			mark, markSt = "!", t.Danger
		}
		name := pad(cutTo(display(p), nameW), nameW)
		if i == cursor {
			lines = append(lines, cur+t.Selected.Render(" "+name+" ")+noteSt.Render(pad(note, profNoteW))+" "+markSt.Render(mark))
			continue
		}
		lines = append(lines, "  "+t.Text.Render(name)+" "+noteSt.Render(pad(note, profNoteW))+" "+markSt.Render(mark))
	}
	return strings.Join(window(lines, cursor, height), "\n")
}

// profDetailBody draws the selected profile: its title and tags, the
// override and secret counts, and every missing name as a cursor row.
func (m Model) profDetailBody(inner, height int) string {
	t := m.th
	p, ok := m.selectedProfile()
	if !ok {
		return " " + t.Muted.Render("Nothing selected")
	}
	v := m.profView(p)
	meta := strings.Join(profTags(v), " · ")
	prefix := ""
	switch {
	case v.Removed:
		prefix = "x marked for removal"
	case v.Added:
		prefix = "~ new"
	}
	if prefix != "" && meta != "" {
		meta = prefix + " · " + meta
	} else if prefix != "" {
		meta = prefix
	}
	title := display(p)
	var lines []string
	if lipgloss.Width(title)+1+lipgloss.Width(meta) <= inner {
		gap := strings.Repeat(" ", max(0, inner-lipgloss.Width(title)-lipgloss.Width(meta)))
		lines = append(lines, t.Title.Render(title)+gap+t.Muted.Render(meta))
	} else {
		lines = append(lines, t.Title.Render(cutTo(title, inner)))
		for _, l := range wrap(meta, inner) {
			lines = append(lines, t.Muted.Render(l))
		}
	}
	lines = append(lines, "")
	const labelW = 12 // "overrides" + room
	valueW := max(8, inner-labelW-2)
	if v.Added {
		for _, l := range wrap("new -- save, then give it values in Variables", inner) {
			lines = append(lines, t.Muted.Render(l))
		}
		return strings.Join(window(prefixRows(lines, -1, t), 0, height), "\n")
	}
	overrides, stored, secrets := m.profileCounts(p)
	field := func(label, value string) {
		for i, l := range wrap(value, valueW) {
			lead := strings.Repeat(" ", labelW)
			if i == 0 {
				lead = t.Muted.Render(pad(label, labelW))
			}
			lines = append(lines, lead+t.Text.Render(l))
		}
	}
	field("overrides", fmt.Sprint(overrides))
	field("secrets", fmt.Sprintf("%d of %d stored", stored, secrets))
	missing := m.missingFor(p)
	cursorAt := -1
	if len(missing) == 0 {
		field("missing", "none")
	}
	rows := m.profRows(p)
	at := m.profRowIndex(rows)
	for i, n := range missing {
		lead := strings.Repeat(" ", labelW)
		if i == 0 {
			lead = t.Muted.Render(pad("missing", labelW))
		}
		if m.profs.pane == paneDetail && i == at {
			cursorAt = len(lines)
		}
		lines = append(lines, lead+t.Danger.Render(cutTo(display(n), valueW)))
	}
	return strings.Join(window(prefixRows(lines, cursorAt, t), max(cursorAt, 0), height), "\n")
}

// prefixRows gives every line the two-cell cursor lead, ">" on cursorAt.
func prefixRows(lines []string, cursorAt int, t style.Theme) []string {
	for i := range lines {
		lead := "  "
		if i == cursorAt {
			lead = t.Accent.Render(">") + " "
		}
		lines[i] = lead + lines[i]
	}
	return lines
}
