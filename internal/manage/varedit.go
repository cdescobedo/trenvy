package manage

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
)

// exists reports whether name is a variable already, on disk or added.
func (m Model) exists(name string) bool {
	_, onDisk := m.snap.Matrix.Vars[name]
	return onDisk || m.staged.Has(session.ChangeAddVar, name)
}

// selectName moves the cursor to name, clearing a filter that would hide it.
func (m *Model) selectName(name string) {
	for pass := 0; pass < 2; pass++ {
		for i, n := range m.names() {
			if n == name {
				m.vars.cursor, m.vars.at, m.vars.pane = i, "", paneList
				return
			}
		}
		m.vars.filter, m.vars.problemsOnly = "", false
	}
}

// inheritors lists the profiles that resolve to v's shared value.
func (m Model) inheritors(v manifest.Var) []string {
	var out []string
	for _, p := range m.snap.Matrix.Profiles {
		if _, ok := v.Profiles[p]; !ok {
			out = append(out, p)
		}
	}
	return out
}

// beginEdit opens the field for one detail row. The row decides the scope
// and the title names it (spec §2).
func (m Model) beginEdit(name, profile string) (tea.Model, tea.Cmd) {
	if !m.canStage() {
		return m, nil
	}
	v := m.varView(name)
	if v.Deleted {
		m.msg = message{name + " is marked for deletion -- u to restore it first", true}
		return m, nil
	}
	if v.Var.Secret {
		m.push(&editOverlay{
			title: "Store " + name + " for " + profile,
			f:     newField("", true),
			save: func(m *Model, val string) bool {
				if val == "" {
					m.msg = message{"a secret can't be empty -- esc to cancel", true}
					return false
				}
				m.staged.Add(session.Change{Kind: session.ChangeSetSecret, Name: name, Profile: profile, Value: val})
				m.msg = message{name + " for " + profile + " staged -- u to undo", false}
				return true
			},
		})
		return m, nil
	}
	if profile == "" {
		title := "Shared value -- every profile overrides it"
		if who := m.inheritors(v.Var); len(who) > 0 {
			title = "Shared value -- used by " + strings.Join(who, ", ")
		}
		initial := ""
		if v.Var.Value != nil {
			initial = *v.Var.Value
		}
		m.push(&editOverlay{title: title, f: newField(initial, false), save: func(m *Model, val string) bool {
			m.staged.Add(session.Change{Kind: session.ChangeSetShared, Name: name, Value: val})
			m.msg = message{name + " shared value staged -- u to undo", false}
			return true
		}})
		return m, nil
	}
	initial, ok := v.Var.Profiles[profile]
	if !ok && v.Var.Value != nil {
		initial = *v.Var.Value
	}
	m.push(&editOverlay{title: "Override for " + profile + " only", f: newField(initial, false), save: func(m *Model, val string) bool {
		m.staged.Add(session.Change{Kind: session.ChangeSetValue, Name: name, Profile: profile, Value: val})
		m.msg = message{name + " override for " + profile + " staged -- u to undo", false}
		return true
	}})
	return m, nil
}

// unsetRow is d on a detail row.
func (m *Model) unsetRow(name, profile string) {
	if !m.canStage() {
		return
	}
	v := m.varView(name)
	switch {
	case v.Deleted:
		m.msg = message{name + " is marked for deletion -- u to restore it first", true}
	case v.Var.Secret:
		m.msg = message{fmt.Sprintf("a stored secret is removed with: trenvy -p %s secret rm %s", profile, name), false}
	case onlyValue(v, profile):
		m.msg = message{"can't remove " + name + "'s only value -- mark it optional (.) or delete the variable (d in the list)", true}
	case profile != "":
		if _, ok := v.Var.Profiles[profile]; !ok {
			m.msg = message{name + " has no override under " + profile + " -- it already uses the shared value", false}
			return
		}
		m.staged.Add(session.Change{Kind: session.ChangeUnsetValue, Name: name, Profile: profile})
		m.msg = message{name + " override for " + profile + " removed -- u to undo", false}
	case v.Var.Value == nil:
		m.msg = message{name + " has no shared value to remove", false}
	default:
		m.staged.Add(session.Change{Kind: session.ChangeUnsetValue, Name: name})
		if left := m.inheritors(v.Var); len(left) > 0 && !v.Var.Optional {
			m.msg = message{"shared value removed -- " + name + " is now MISSING under " + strings.Join(left, ", ") + " -- u to undo", false}
		} else {
			m.msg = message{"shared value removed -- u to undo", false}
		}
	}
}

// toggleDelete is d in the list.
func (m *Model) toggleDelete(name string) {
	if !m.canStage() {
		return
	}
	if m.staged.Has(session.ChangeDeleteVar, name) {
		m.staged.DropKind(session.ChangeDeleteVar, name)
		m.msg = message{name + " restored", false}
		return
	}
	added := m.staged.Has(session.ChangeAddVar, name)
	// Add drops every other change on name; say so rather than lose them
	// silently.
	others := len(m.staged.On(name))
	m.staged.Add(session.Change{Kind: session.ChangeDeleteVar, Name: name})
	if added {
		m.msg = message{name + " removed -- it was only added this session", false}
		m.moveCursor(0)
		return
	}
	text := name + " marked for deletion"
	if others > 0 {
		text += ", dropped " + plural(others, "staged change") + " on it"
	}
	m.msg = message{text + " -- u to undo", false}
}

func (m *Model) undoVar(name string) {
	if n := len(m.staged.DropOn(name)); n > 0 {
		m.msg = message{"undid " + plural(n, "change") + " on " + name, false}
		m.moveCursor(0)
		return
	}
	m.msg = message{"nothing staged on " + name, false}
}

func (m *Model) undoCell(name, profile string) {
	if n := len(m.staged.DropCell(name, profile)); n > 0 {
		m.msg = message{"undid " + plural(n, "change"), false}
		return
	}
	m.msg = message{"nothing staged on this row", false}
}

// beginPromote is Make secret. A shared value becomes one keyring item per
// profile that can drift apart, so that case confirms first, in the old
// interface's words; overrides only stage straight away.
func (m *Model) beginPromote(name string) {
	if !m.canStage() {
		return
	}
	v := m.varView(name)
	stage := func(m *Model) tea.Cmd {
		for _, c := range session.PromotionChanges(name, v.Var, m.snap.Matrix.Profiles) {
			m.staged.Add(c)
		}
		m.msg = message{name + " will become a secret -- u to undo", false}
		return nil
	}
	if v.Var.Value == nil {
		stage(m)
		return
	}
	m.push(&confirmOverlay{
		title: "Make " + name + " a secret",
		text: fmt.Sprintf("%s becomes %s, one per declared profile -- changing one "+
			"afterward will not change the others. There is no way back to plain.",
			name, plural(len(m.snap.Matrix.Profiles), "separate keyring item")),
		choices: []choice{{"enter", "promote", stage}},
	})
}

// varMenu is . on the Variables screen.
func (m *Model) varMenu() overlay {
	var items []menuItem
	if name, ok := m.selected(); ok {
		v := m.varView(name)
		secret := menuItem{label: "Make secret", run: func(m *Model) tea.Cmd { m.beginPromote(name); return nil }}
		switch {
		case v.Var.Secret:
			secret.disabled = name + " is already a secret -- there is no way back to plain"
		case v.Deleted:
			secret.disabled = name + " is marked for deletion"
		}
		label, want := "Mark optional", "true"
		if v.Var.Optional {
			label, want = "Mark required", "false"
		}
		opt := menuItem{label: label, run: func(m *Model) tea.Cmd {
			if m.canStage() {
				m.staged.Add(session.Change{Kind: session.ChangeSetOptional, Name: name, Value: want})
				m.msg = message{name + " marked " + label[len("Mark "):] + " -- u to undo", false}
			}
			return nil
		}}
		switch {
		case v.Deleted:
			opt.disabled = name + " is marked for deletion"
		case want == "false" && !v.Var.Secret && v.Var.Value == nil && len(v.Var.Profiles) == 0:
			opt.disabled = "give " + name + " a value before making it required"
		}
		items = append(items, secret, opt, menuItem{
			label:    "Rename",
			disabled: "renaming a variable is not supported yet -- a secret's keyring items and every output glob naming it would have to move with it",
		})
	}
	if m.stale {
		items = append(items, menuItem{label: "Reload from disk", run: func(m *Model) tea.Cmd { return m.reloadCmd() }})
	}
	items = append(items, menuItem{label: "Project info", run: func(m *Model) tea.Cmd { m.push(infoOverlay{}); return nil }})
	var others []string
	for _, id := range m.snap.Projects {
		if id != m.snap.Project {
			others = append(others, id)
		}
	}
	sw := menuItem{label: "Switch project", run: func(m *Model) tea.Cmd { m.push(&switchOverlay{ids: others}); return nil }}
	if len(others) == 0 {
		sw.disabled = "no other project is registered"
	}
	return &menuOverlay{title: "More", items: append(items, sw)}
}
