package manage

import tea "github.com/charmbracelet/bubbletea"

type actionDoneMsg struct {
	text string
	err  error
}

type checkedMsg struct {
	id, state string
	err       error
}

// actionBlocked is why Render now, Remove rendered file or Check now cannot
// run on id, or "". They act on the definition on disk, so an output with
// anything staged -- or one that exists only in this session -- would
// render something other than what the screen shows.
func (m Model) actionBlocked(id string) string {
	v := m.outView(id)
	switch {
	case m.stale:
		return "the last reload failed -- press . then Reload from disk first"
	case v.Added:
		return id + " is not saved yet -- w to save it first"
	case v.Removed:
		return id + " is marked for removal"
	case len(m.staged.OnOutput(id)) > 0:
		return "save or undo " + id + "'s staged changes first -- they are not on disk yet"
	}
	return ""
}

// runAction runs an immediate action, holding input the way a save does.
func (m *Model) runAction(label string, run func() (string, error)) tea.Cmd {
	m.saving = true
	m.msg = message{label + "…", false}
	return func() tea.Msg {
		text, err := run()
		return actionDoneMsg{text, err}
	}
}

// afterAction ends an immediate action. Success reloads, holding input as
// a save does, because the render records and the file's state changed; a
// refusal changed nothing and says why.
func (m Model) afterAction(msg actionDoneMsg) (tea.Model, tea.Cmd) {
	m.saving = false
	if msg.err != nil {
		m.msg = message{errText(msg.err), true}
		return m, nil
	}
	m.msg = message{msg.text, false}
	m.reloading = true
	return m, m.reloadCmd()
}

// afterChecked puts a Check now result on screen until the next reload.
func (m Model) afterChecked(msg checkedMsg) (tea.Model, tea.Cmd) {
	m.saving = false
	if msg.err != nil {
		m.msg = message{"could not check " + msg.id + ": " + errText(msg.err), true}
		return m, nil
	}
	m.outs.checked[msg.id] = msg.state
	m.msg = message{msg.id + " checked: " + msg.state, false}
	return m, nil
}

// actionItems are the . menu's three immediate actions on output id.
func (m *Model) actionItems(id string) []menuItem {
	why := m.actionBlocked(id)
	render := menuItem{label: "Render now", disabled: why, run: func(m *Model) tea.Cmd {
		r := m.deps.Render
		return m.runAction("rendering "+id, func() (string, error) { return r(id) })
	}}
	clean := menuItem{label: "Remove rendered file", disabled: why, run: func(m *Model) tea.Cmd {
		path := m.outView(id).Output.Path
		c := m.deps.Clean
		m.push(&confirmOverlay{
			title: "Remove " + path,
			text:  path + " is deleted from this worktree. The output stays defined; render writes it again.",
			choices: []choice{{"enter", "remove", func(m *Model) tea.Cmd {
				return m.runAction("removing "+path, func() (string, error) { return c(id) })
			}}},
		})
		return nil
	}}
	check := menuItem{label: "Check now (reads secrets)", disabled: why, run: func(m *Model) tea.Cmd {
		c := m.deps.Check
		m.saving = true
		m.msg = message{"checking " + id + "…", false}
		return func() tea.Msg {
			st, err := c(id)
			return checkedMsg{id, st, err}
		}
	}}
	return []menuItem{render, clean, check}
}
