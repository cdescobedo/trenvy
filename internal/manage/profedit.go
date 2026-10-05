package manage

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
)

func (m Model) profileNameProblem(name string) string {
	if name == "" {
		return "a profile needs a name"
	}
	if err := manifest.CheckProfileName(name); err != nil {
		return name + ": " + err.Error()
	}
	for _, p := range m.profileIDs() {
		if p == name {
			return name + " already exists"
		}
	}
	return ""
}

func (m *Model) beginAddProfile() {
	if !m.canStage() {
		return
	}
	m.push(&editOverlay{title: "New profile -- its name", f: newField("", false), save: func(m *Model, name string) bool {
		name = strings.TrimSpace(name)
		if p := m.profileNameProblem(name); p != "" {
			m.msg = message{p, true}
			return false
		}
		m.staged.Add(session.Change{Kind: session.ChangeAddProfile, Name: name})
		m.selectProfile(name)
		m.msg = message{name + " will be added -- save, then give it values in Variables", false}
		return true
	}})
}

// removeRefusal is why p cannot be marked for removal, or "" (ruling 4,
// and the save's own rule that a required variable keeps a value). A
// profile added this session can always go: removing it stages nothing.
func (m Model) removeRefusal(p string) string {
	v := m.profView(p)
	if v.Added {
		return ""
	}
	kept := 0
	for _, q := range m.profileIDs() {
		if !m.profView(q).Removed {
			kept++
		}
	}
	switch {
	case kept <= 1:
		return p + " is the last profile -- a project needs at least one"
	case v.Default:
		return p + " is the default -- make another profile the default first (. then Make default)"
	case p == m.snap.Profile:
		return "this session is on " + p + " -- remove it from a session on another profile: trenvy -p <other> ui"
	}
	// A required plain variable with no shared value keeps a value only
	// through its overrides. Those on profiles already marked for removal
	// do not count, so two removals together cannot orphan it either.
	var orphans []string
	for _, n := range m.allNames() {
		vv := m.varView(n)
		if vv.Deleted || vv.Var.Secret || vv.Var.Optional || vv.Var.Value != nil {
			continue
		}
		if _, ok := vv.Var.Profiles[p]; !ok {
			continue
		}
		left := 0
		for q := range vv.Var.Profiles {
			if q != p && !m.profView(q).Removed {
				left++
			}
		}
		if left == 0 {
			orphans = append(orphans, n)
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		return "removing " + p + " would leave " + strings.Join(orphans, ", ") + " with no value -- give them a shared value first"
	}
	return ""
}

func (m *Model) toggleRemoveProfile(p string) {
	if !m.canStage() {
		return
	}
	v := m.profView(p)
	if v.Removed {
		m.staged.DropKind(session.ChangeRemoveProfile, p)
		m.msg = message{p + " kept", false}
		return
	}
	if r := m.removeRefusal(p); r != "" {
		m.msg = message{r, true}
		return
	}
	cells := len(m.staged.CellsOn(p))
	selections := droppedSelections(m.staged.OnProfile(p))
	m.staged.Add(session.Change{Kind: session.ChangeRemoveProfile, Name: p})
	if v.Added {
		m.profs.cursor = clamp(m.profs.cursor, 0, len(m.profileIDs())-1)
		m.msg = message{p + " will not be added after all" + selections, false}
		return
	}
	text := p + " marked for removal -- d again to keep it" + selections
	if cells > 0 {
		text += "; " + plural(cells, "staged value") + " on it dropped, and keeping it will not bring them back"
	}
	m.msg = message{text, false}
}

// droppedSelections words the use or default staged on a profile that a
// removal drops with it, as a clause for the removal's message.
func droppedSelections(cs []session.Change) string {
	text := ""
	for _, c := range cs {
		switch c.Kind {
		case session.ChangeUseProfile:
			text += "; its staged use on this machine dropped"
		case session.ChangeDefaultProfile:
			text += "; its staged default dropped"
		}
	}
	return text
}

// undoProfile drops the profile's own staged changes. Values staged on its
// cells belong to their variables and stay.
func (m *Model) undoProfile(p string) {
	cs := m.staged.OnProfile(p)
	if len(cs) == 0 {
		m.msg = message{"nothing staged on " + p, false}
		return
	}
	for _, c := range cs {
		m.staged.DropKind(c.Kind, p)
	}
	m.profs.cursor = clamp(m.profs.cursor, 0, len(m.profileIDs())-1)
	m.msg = message{"undid " + plural(len(cs), "change") + " on " + p, false}
}

// outranked names what decides this session's profile over the machine's
// selection, or "" when the selection decides it.
func (m Model) outranked() string {
	switch m.snap.Source {
	case "active", "default", "":
		return ""
	}
	return m.snap.Source
}

func (m *Model) useProfile(p string) {
	if !m.canStage() {
		return
	}
	if p == m.snap.Active {
		if u, ok := m.staged.UseProfile(); ok {
			m.staged.DropKind(session.ChangeUseProfile, u)
		}
		m.msg = message{p + " is this machine's active profile again, as on disk", false}
		return
	}
	m.staged.Add(session.Change{Kind: session.ChangeUseProfile, Name: p})
	text := p + " will be this machine's active profile -- u on it to undo"
	if o := m.outranked(); o != "" {
		text = p + " will be this machine's active profile, but " + o + " still outranks it here"
	}
	m.msg = message{text, false}
}

func (m *Model) makeDefaultProfile(p string) {
	if !m.canStage() {
		return
	}
	if p == m.snap.DefaultProfile {
		if d, ok := m.staged.DefaultProfile(); ok {
			m.staged.DropKind(session.ChangeDefaultProfile, d)
		}
		m.msg = message{p + " is the default again, as on disk", false}
		return
	}
	m.staged.Add(session.Change{Kind: session.ChangeDefaultProfile, Name: p})
	m.msg = message{p + " will be the default -- u on it to undo", false}
}

// profMenu is . on the Profiles screen (spec §5).
func (m *Model) profMenu() overlay {
	var items []menuItem
	if p, ok := m.selectedProfile(); ok {
		v := m.profView(p)
		use := menuItem{label: "Use on this machine", run: func(m *Model) tea.Cmd { m.useProfile(p); return nil }}
		switch {
		case v.Removed:
			use.disabled = p + " is marked for removal"
		case v.Added:
			use.disabled = "save first -- " + p + " is not in the manifest yet"
		case v.Active:
			use.disabled = p + " is already this machine's active profile"
		}
		def := menuItem{label: "Make default", run: func(m *Model) tea.Cmd { m.makeDefaultProfile(p); return nil }}
		switch {
		case v.Removed:
			def.disabled = p + " is marked for removal"
		case v.Default:
			def.disabled = p + " is already the default"
		}
		items = append(items, use, def, menuItem{
			label:    "Rename",
			disabled: "a profile's name is part of every keyring item stored under it -- renaming would mean moving each secret, which is not supported",
		})
	}
	return &menuOverlay{title: "More", items: append(items, m.commonMenuItems()...)}
}

// jumpToCell opens Variables on name with the detail cursor on profile's
// row (spec §5: enter on a missing name).
func (m *Model) jumpToCell(name, profile string) {
	m.selectName(name)
	m.vars.at, m.vars.pane = profile, paneDetail
	m.scr = scrVariables
}
