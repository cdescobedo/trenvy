// Package tui is the interactive table for init, import and edit.
//
// Update is a pure function: state in, state out, no I/O. Keyring reads are
// issued as tea.Cmd values and arrive back as messages, so the whole state
// machine is testable without a terminal or a secret store.
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Row is one variable in the table.
type Row struct {
	Name      string
	Value     string
	Secret    bool
	Present   bool // the secret exists in the keyring (from Has, not Get)
	Overrides int
	Source    string // where an imported row came from, shown during import
	Dirty     bool
	Deleted   bool
}

// RevealedMsg carries a secret value fetched in response to r.
type RevealedMsg struct {
	Name  string
	Value string
}

// SaveMsg is emitted when the user presses w.
type SaveMsg struct{}

// RevealRequestMsg asks the host to fetch one secret.
type RevealRequestMsg struct{ Name string }

// Model is the table's state.
type Model struct {
	title     string
	rows      []Row
	cursor    int
	mode      Mode
	buffer    string
	filter    string
	saved     bool
	cancelled bool
}

// New builds a model over rows.
func New(title string, rows []Row) Model {
	return Model{title: title, rows: rows, mode: ModeBrowse}
}

func (m Model) Cursor() int     { return m.cursor }
func (m Model) Mode() Mode      { return m.mode }
func (m Model) Saved() bool     { return m.saved }
func (m Model) Cancelled() bool { return m.cancelled }
func (m Model) Title() string   { return m.title }
func (m Model) Buffer() string  { return m.buffer }

// matchesFilter reports whether r passes the active filter. Rows and
// realIndex must share this predicate: realIndex resolves the cursor by
// counting matches in row order, so any divergence between the two would
// mean an edit lands on a row the user cannot see selected.
func (m Model) matchesFilter(r Row) bool {
	return m.filter == "" || strings.Contains(strings.ToUpper(r.Name), strings.ToUpper(m.filter))
}

// Rows returns the visible rows, honouring the active filter.
func (m Model) Rows() []Row {
	if m.filter == "" {
		return m.rows
	}
	var out []Row
	for _, r := range m.rows {
		if m.matchesFilter(r) {
			out = append(out, r)
		}
	}
	return out
}

// AllRows returns every row including those hidden by the filter, for saving.
func (m Model) AllRows() []Row { return m.rows }

// nameExists reports whether name is already used by a row. Two rows sharing
// a name would collapse into one on save, since the manifest stores
// variables in a map, so add must never be allowed to create one.
func (m Model) nameExists(name string) bool {
	for _, r := range m.rows {
		if r.Name == name {
			return true
		}
	}
	return false
}

func (m Model) dirty() bool {
	for _, r := range m.rows {
		if r.Dirty || r.Deleted {
			return true
		}
	}
	return false
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update advances the state machine.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case RevealedMsg:
		for i := range m.rows {
			if m.rows[i].Name == msg.Name {
				// Revealing changes nothing, so the row must not become dirty.
				m.rows[i].Value = msg.Value
			}
		}
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case ModeEditValue, ModeAddName, ModeFilter:
			return m.updateInput(msg)
		case ModeConfirmQuit:
			return m.updateConfirm(msg)
		default:
			return m.updateBrowse(msg)
		}
	}
	return m, nil
}

func (m Model) updateBrowse(msg tea.KeyMsg) (Model, tea.Cmd) {
	visible := m.Rows()

	switch msg.String() {
	case "j", "down":
		if m.cursor < len(visible)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "e":
		if len(visible) > 0 {
			m.mode = ModeEditValue
			m.buffer = ""
		}
	case "a":
		m.mode = ModeAddName
		m.buffer = ""
	case "/":
		m.mode = ModeFilter
		m.buffer = ""
	case "s":
		if i, ok := m.realIndex(); ok {
			becomingSecret := !m.rows[i].Secret
			m.rows[i].Secret = becomingSecret
			if becomingSecret {
				// A value that has just become secret must not stay in the
				// manifest, so it is dropped here rather than at save time.
				// Going the other way must not clear it: a revealed value
				// (or a plain value restored from secret) has to survive.
				m.rows[i].Value = ""
			}
			m.rows[i].Dirty = true
		}
	case "d":
		if i, ok := m.realIndex(); ok {
			// Deferred: nothing is destroyed until w.
			m.rows[i].Deleted = !m.rows[i].Deleted
		}
	case "r":
		if i, ok := m.realIndex(); ok && m.rows[i].Secret {
			name := m.rows[i].Name
			return m, func() tea.Msg { return RevealRequestMsg{Name: name} }
		}
	case "w":
		m.saved = true
		return m, func() tea.Msg { return SaveMsg{} }
	case "esc":
		// An active filter is the one thing browse mode itself can clear;
		// nothing stays permanently hidden.
		if m.filter != "" {
			m.filter = ""
			m.cursor = 0
		}
	case "q", "ctrl+c":
		if m.dirty() {
			m.mode = ModeConfirmQuit
			return m, nil
		}
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateInput(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		if m.mode == ModeFilter {
			m.filter = ""
			m.cursor = 0
		}
		m.mode = ModeBrowse
		m.buffer = ""
		return m, nil

	case tea.KeyEnter:
		switch m.mode {
		case ModeEditValue:
			if i, ok := m.realIndex(); ok {
				m.rows[i].Value = m.buffer
				m.rows[i].Dirty = true
			}
		case ModeAddName:
			if m.buffer != "" && !m.nameExists(m.buffer) {
				m.rows = append(m.rows, Row{Name: m.buffer, Dirty: true})
			}
		case ModeFilter:
			m.filter = m.buffer
			m.cursor = 0
		}
		m.mode = ModeBrowse
		m.buffer = ""
		return m, nil

	case tea.KeyBackspace:
		if m.buffer != "" {
			m.buffer = m.buffer[:len(m.buffer)-1]
		}
		return m, nil

	case tea.KeyRunes:
		m.buffer += string(msg.Runes)
		return m, nil

	case tea.KeySpace:
		m.buffer += " "
		return m, nil
	}
	return m, nil
}

func (m Model) updateConfirm(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		m.cancelled = true
		return m, tea.Quit
	default:
		m.mode = ModeBrowse
		return m, nil
	}
}

// realIndex maps the cursor over the filtered view back to m.rows by
// counting matches in row order rather than resolving by name, so two rows
// sharing a name never resolve to the wrong one.
func (m Model) realIndex() (int, bool) {
	if m.cursor < 0 {
		return 0, false
	}
	n := m.cursor
	for i, r := range m.rows {
		if !m.matchesFilter(r) {
			continue
		}
		if n == 0 {
			return i, true
		}
		n--
	}
	return 0, false
}
