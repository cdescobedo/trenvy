package manage

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/session"
)

// reviewOverlay is w: every staged change, grouped by what it changes,
// before -> after, with irreversible lines marked (spec §3). It is also
// the confirmation step: nothing is written until enter here.
type reviewOverlay struct {
	cursor  int
	failure string // the last commit's error, shown until the next attempt
}

type reviewLine struct {
	index        int // into staged.All()
	group        string
	mark, what   string
	detail       string
	irreversible bool
}

func (m *Model) reviewLines() []reviewLine {
	all := m.staged.All()
	idx := make([]int, len(all))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return all[idx[a]].Name < all[idx[b]].Name })
	out := make([]reviewLine, 0, len(all))
	for _, i := range idx {
		out = append(out, m.describe(i, all[i]))
	}
	return out
}

func valueOr(p *string, none string) string {
	if p == nil {
		return none
	}
	return display(*p)
}

func (m *Model) describe(i int, c session.Change) reviewLine {
	l := reviewLine{index: i, group: c.Name, mark: "~"}
	base := m.snap.Matrix.Vars[c.Name]
	switch c.Kind {
	case session.ChangeAddVar:
		l.what = "add variable"
	case session.ChangeDeleteVar:
		l.mark, l.what, l.irreversible = "x", "delete variable", true
		if base.Secret {
			var stored []string
			for _, p := range m.snap.Matrix.Profiles {
				if m.snap.Matrix.Stored[c.Name][p] {
					stored = append(stored, p)
				}
			}
			l.detail = "removes " + plural(len(stored), "keyring item")
			if len(stored) > 0 {
				l.detail += ": " + strings.Join(stored, ", ")
			}
		} else {
			l.detail = "its values leave the manifest"
		}
	case session.ChangeSetShared:
		l.what = "shared value"
		l.detail = valueOr(base.Value, "(none)") + "  ->  " + display(c.Value)
	case session.ChangeSetValue:
		before, ok := base.Profiles[c.Profile]
		if !ok {
			before = valueOr(base.Value, "(none)")
		}
		l.what = c.Profile + " override"
		l.detail = display(before) + "  ->  " + display(c.Value)
	case session.ChangeUnsetValue:
		if c.Profile == "" {
			l.what, l.detail = "remove shared value", "was "+valueOr(base.Value, "(none)")
		} else {
			l.what, l.detail = "remove "+c.Profile+" override", "was "+display(base.Profiles[c.Profile])
		}
	case session.ChangeSetSecret:
		l.what, l.detail = c.Profile, "store new value (keyring)"
	case session.ChangePromoteSecret:
		l.what, l.detail, l.irreversible = "make secret", "its values move into the keyring; there is no way back to plain", true
	case session.ChangeSetOptional:
		l.what = "mark required"
		if c.Value == "true" {
			l.what = "mark optional"
		}
	default:
		l.what = c.Kind
	}
	return l
}

func (m Model) openReview() (tea.Model, tea.Cmd) {
	_, cmd := m.openReviewPtr()
	return m, cmd
}

func (m *Model) openReviewPtr() (bool, tea.Cmd) {
	if m.staged.Len() == 0 {
		m.msg = message{"nothing to save", false}
		return false, nil
	}
	m.push(&reviewOverlay{})
	return true, nil
}

func (r *reviewOverlay) update(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	lines := m.reviewLines()
	switch {
	case keyIs(k, "j", "down"):
		r.cursor = min(r.cursor+1, len(lines)-1)
	case keyIs(k, "k", "up"):
		r.cursor = max(r.cursor-1, 0)
	case keyIs(k, "u"):
		l := lines[clamp(r.cursor, 0, len(lines)-1)]
		dropped := m.staged.Drop(l.index)
		what := l.group + " " + l.what
		if n := len(dropped) - 1; n > 0 {
			m.msg = message{fmt.Sprintf("dropped %s and %s that depended on it", what, plural(n, "change")), false}
		} else {
			m.msg = message{"dropped " + what, false}
		}
		if m.staged.Len() == 0 {
			return true, nil
		}
		r.cursor = clamp(r.cursor, 0, m.staged.Len()-1)
	case keyIs(k, "enter"):
		if m.stale {
			m.msg = message{"can't save: the last reload failed -- esc, then . and Reload from disk", true}
			return false, nil
		}
		m.saving, r.failure = true, ""
		return false, m.commitCmd()
	}
	return false, nil
}

func (r *reviewOverlay) view(m *Model, w, h int) string {
	t := m.th
	var body []string
	group, focus := "", 0
	for i, l := range m.reviewLines() {
		if l.group != group {
			group = l.group
			body = append(body, t.Title.Render(group))
		}
		cur := "  "
		if i == r.cursor {
			cur, focus = t.Accent.Render("> "), len(body)
		}
		detail := t.Text.Render(l.detail)
		if l.irreversible {
			detail = t.Danger.Render(l.detail + " !")
		}
		body = append(body, cur+t.Staged.Render(l.mark)+" "+t.Text.Render(pad(l.what, 22))+detail)
		if l.irreversible {
			body = append(body, "    "+strings.Repeat(" ", 22)+t.Danger.Render("cannot be undone after saving"))
		}
	}
	switch {
	case m.saving:
		body = append(body, "", t.Muted.Render("saving…"))
	case r.failure != "":
		body = append(body, "")
		for _, l := range strings.Split(r.failure, "\n") {
			body = append(body, t.Danger.Render(display(l)))
		}
		body = append(body, t.Text.Render("every change is still staged -- enter to retry"))
	}
	return m.panelAt("Review "+t.PillStaged.Render(plural(m.staged.Len(), "change")), body, focus, w, h)
}

func (r *reviewOverlay) hints(m *Model) []hint {
	if m.saving {
		return []hint{{"ctrl+c", "quit without waiting"}}
	}
	return []hint{{"enter", "save all"}, {"u", "drop this change"}, {"esc", "keep editing"}}
}

func (m *Model) commitCmd() tea.Cmd {
	changes, commit := m.staged.All(), m.deps.Commit
	return func() tea.Msg {
		res, err := commit(changes)
		return committedMsg{res, err}
	}
}

func (m *Model) reloadCmd() tea.Cmd {
	reload := m.deps.Reload
	return func() tea.Msg {
		snap, err := reload()
		return reloadedMsg{snap, err}
	}
}

func (m *Model) review() *reviewOverlay {
	for i := len(m.overlays) - 1; i >= 0; i-- {
		if r, ok := m.overlays[i].(*reviewOverlay); ok {
			return r
		}
	}
	return nil
}

// afterCommit follows a save. Success clears the staged set and closes the
// review; failure keeps both, shows the report, and still reloads, because
// part of the batch (keyring items, the state file) may already have landed
// and the screen must say so. Either way the snapshot is reloaded before
// anything else can be staged or saved.
func (m Model) afterCommit(msg committedMsg) (tea.Model, tea.Cmd) {
	m.saving = false
	if msg.err != nil {
		if r := m.review(); r != nil {
			r.failure = msg.err.Error()
		}
		return m, m.reloadCmd()
	}
	for i := len(m.overlays) - 1; i >= 0; i-- {
		if _, ok := m.overlays[i].(*reviewOverlay); ok {
			m.removeOverlay(i)
		}
	}
	m.staged.Clear()
	text := "saved " + plural(msg.res.Saved, "change")
	if len(msg.res.LeftInPlace) > 0 {
		text += " -- left in place: " + strings.Join(msg.res.LeftInPlace, ", ")
	}
	m.msg = message{text, false}
	return m, m.reloadCmd()
}

// afterReload swaps in a fresh snapshot, keeping the cursor on the same
// variable. Revealed values are dropped: a reload is a new view of disk,
// and a secret shown before it must be asked for again.
func (m Model) afterReload(msg reloadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.stale = true
		m.msg = message{"reload failed: " + msg.err.Error() + " -- press . then Reload from disk", true}
		return m, nil
	}
	keep, _ := m.selected()
	m.stale, m.snap = false, msg.snap
	m.vars.revealed = map[cellKey]string{}
	if keep != "" {
		m.selectName(keep)
	}
	return m, nil
}
