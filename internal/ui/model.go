package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Tab is one of the panels in the management interface's tab bar.
type Tab int

const (
	TabOverview Tab = iota
	TabVariables
	TabProfiles
	TabOutputs

	tabCount // sentinel: keep last, used to wrap tab arithmetic
)

// String names the tab for the bar and for test failure messages.
func (t Tab) String() string {
	switch t {
	case TabOverview:
		return "Overview"
	case TabVariables:
		return "Variables"
	case TabProfiles:
		return "Profiles"
	case TabOutputs:
		return "Outputs"
	}
	return "unknown"
}

// Mode is the Variables tab's current input mode. Mirrors internal/tui.Mode.
type Mode int

const (
	// ModeBrowse is the resting mode: the cursor moves and e, a, d, and /
	// act on the cell or row under it.
	ModeBrowse Mode = iota
	// ModeEditValue is typing a replacement value for the selected cell.
	ModeEditValue
	// ModeAddName is typing the name of a variable to add.
	ModeAddName
	// ModeFilter is typing the substring that narrows the visible rows.
	ModeFilter
	// ModeEditSecret is typing a replacement value for a secret cell. The
	// buffer is masked in View() (see viewVariables) rather than echoed, so a
	// secret is never printed to the screen -- and to terminal scrollback --
	// while it is being typed.
	ModeEditSecret
)

// String names the mode for test failure messages.
func (md Mode) String() string {
	switch md {
	case ModeBrowse:
		return "browse"
	case ModeEditValue:
		return "edit"
	case ModeAddName:
		return "add"
	case ModeFilter:
		return "filter"
	case ModeEditSecret:
		return "edit-secret"
	}
	return "unknown"
}

// Change kinds. Task 9 switches on Kind to decide where a change lands: the
// manifest for set-value/add-var/delete-var, the keyring for set-secret.
const (
	ChangeSetValue  = "set-value"
	ChangeAddVar    = "add-var"
	ChangeDeleteVar = "delete-var"
	ChangeSetSecret = "set-secret"
)

// Change is one uncommitted edit, held in the model until w writes it to the
// manifest or the keyring (Task 9). Profile says which column a set-value or
// set-secret belongs to; a value written to the wrong profile would be
// silent data corruption, so the field is never left to be inferred from
// cursor position at commit time.
type Change struct {
	Kind, Name, Profile, Value string
}

// RevealRequestMsg asks the host to fetch one secret's value for one
// profile. Unlike internal/tui's flat table, a row here has one cell per
// profile, so Name alone cannot say which keyring entry to fetch -- Profile
// is what keeps a reveal of one column from being ambiguous with another.
type RevealRequestMsg struct{ Name, Profile string }

// RevealedMsg carries a secret value fetched in response to r, for exactly
// one (Name, Profile) cell. Folding it in must never mark more than that one
// cell revealed: not the variable's other profiles, and not another secret
// entirely -- see Model.setRevealed.
type RevealedMsg struct{ Name, Profile, Value string }

// revealedCell is one secret value the user has explicitly revealed with r,
// scoped to exactly the cell that produced it.
type revealedCell struct{ Name, Profile, Value string }

// Model is the management interface's state. Update is a pure function:
// state in, state out, no I/O — mirroring internal/tui, this package's
// sibling flat-table model. Any data the model needs from outside (the
// keyring, the registry) arrives as a message; Update never fetches it.
type Model struct {
	project  string
	mx       *Matrix
	problems []Problem
	tab      Tab
	row      int // selected index into visibleNames()
	col      int // selected index into mx.Profiles

	mode   Mode
	buffer string
	filter string
	staged []Change
	// saved records that the session ended with w. Task 9's cli-side commit
	// checks this, never len(Staged()) > 0: Staged() looks identical whether
	// or not w was pressed, and this is the one bit that tells the two apart
	// -- mirrors internal/tui's own Saved.
	saved bool

	// revealed holds cells the user has explicitly revealed with r this
	// session. A slice, not a map keyed by name: it must scope to exactly
	// one profile per entry, mirroring how staged already scopes Change by
	// (Name, Profile) rather than by Name alone.
	revealed []revealedCell
}

// New builds a model over one project's matrix and its detected problems.
//
// The signature takes exactly what this task needs. It is expected to grow:
// a later task's project switcher needs the list of registered projects,
// which this package cannot read itself since it does no I/O. That addition
// is deliberately left unresolved here rather than guessed.
func New(project string, mx *Matrix, ps []Problem) Model {
	return Model{project: project, mx: mx, problems: ps, tab: TabOverview}
}

// Tab reports the active tab.
func (m Model) Tab() Tab { return m.tab }

// Row reports the selected variable's index into mx.Names.
func (m Model) Row() int { return m.row }

// Col reports the selected profile's index into mx.Profiles.
func (m Model) Col() int { return m.col }

// Mode reports the current input mode.
func (m Model) Mode() Mode { return m.mode }

// Buffer is the text typed so far in whichever input mode is active: a
// value, a new variable name, or a filter.
func (m Model) Buffer() string { return m.buffer }

// Saved reports that the session ended with w rather than q or ctrl+c. The
// cli must not write anything back to the manifest or the keyring until
// this is true -- quitting any other way must leave it at its zero value.
func (m Model) Saved() bool { return m.saved }

// Staged returns the uncommitted changes, in the order they were made. A
// copy, so a caller cannot mutate the model's internal slice through it.
func (m Model) Staged() []Change {
	out := make([]Change, len(m.staged))
	copy(out, m.staged)
	return out
}

// stage records c, replacing any earlier staged change with the same Kind,
// Name and Profile: re-editing a cell before w must not leave stale
// duplicates for Task 9 to write more than once.
func (m *Model) stage(c Change) {
	for i, existing := range m.staged {
		if existing.Kind == c.Kind && existing.Name == c.Name && existing.Profile == c.Profile {
			m.staged[i] = c
			return
		}
	}
	m.staged = append(m.staged, c)
}

// setRevealed records that (name, profile) was revealed with value v,
// replacing any earlier reveal of the same cell rather than appending a
// duplicate.
func (m *Model) setRevealed(name, profile, v string) {
	for i, r := range m.revealed {
		if r.Name == name && r.Profile == profile {
			m.revealed[i].Value = v
			return
		}
	}
	m.revealed = append(m.revealed, revealedCell{Name: name, Profile: profile, Value: v})
}

// revealedValue reports the value revealed for exactly (name, profile), if
// any. Looking it up by both fields, not by name alone, is what stops
// revealing one profile's secret from displaying on another's.
func (m Model) revealedValue(name, profile string) (string, bool) {
	for _, r := range m.revealed {
		if r.Name == name && r.Profile == profile {
			return r.Value, true
		}
	}
	return "", false
}

// allNames returns every variable name the matrix knows about plus any not
// yet on disk that the user has staged with a, sorted so a freshly added
// name takes its place in the list rather than always trailing at the end.
func (m Model) allNames() []string {
	names := append([]string{}, m.mx.Names...)
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		seen[n] = true
	}
	for _, c := range m.staged {
		if c.Kind == ChangeAddVar && !seen[c.Name] {
			names = append(names, c.Name)
			seen[c.Name] = true
		}
	}
	sort.Strings(names)
	return names
}

// visibleNames is allNames narrowed by the active filter. Deleted rows stay
// in this list on purpose: filtering hides rows the user asked to hide,
// which is not what a staged delete means -- that row must stay visible and
// marked until w or q resolves it.
func (m Model) visibleNames() []string {
	all := m.allNames()
	if m.filter == "" {
		return all
	}
	out := make([]string, 0, len(all))
	for _, n := range all {
		if strings.Contains(strings.ToUpper(n), strings.ToUpper(m.filter)) {
			out = append(out, n)
		}
	}
	return out
}

// isDeleted reports whether name has a staged delete-var change.
func (m Model) isDeleted(name string) bool {
	for _, c := range m.staged {
		if c.Kind == ChangeDeleteVar && c.Name == name {
			return true
		}
	}
	return false
}

// cellText is what a cell renders: a staged set-value's literal text takes
// precedence over the matrix's on-disk classification, so an edit is visible
// the instant it is staged rather than only after w writes it out.
//
// A staged set-secret is deliberately handled differently: it must NOT show
// its Value the way set-value does. Echoing a just-typed secret into the grid
// the instant it is staged would defeat the whole point of masking the input
// for it -- the plaintext would simply reappear one line up. "pending" says a
// write is staged without saying what it will write; r still reveals what is
// actually in the keyring today.
func (m Model) cellText(name, profile string) string {
	for i := len(m.staged) - 1; i >= 0; i-- {
		c := m.staged[i]
		if c.Name != name || c.Profile != profile {
			continue
		}
		switch c.Kind {
		case ChangeSetValue:
			return c.Value
		case ChangeSetSecret:
			return "secret (pending)"
		}
	}
	if v, ok := m.revealedValue(name, profile); ok {
		return v
	}
	return string(m.mx.Cell(name, profile))
}

// currentValue is what e prefills its buffer with: a pending edit if one is
// already staged for this cell (so re-opening it does not discard unsaved
// typing back to the on-disk value), otherwise the profile override or
// shared value the matrix would resolve today.
func (m Model) currentValue(name, profile string) string {
	for i := len(m.staged) - 1; i >= 0; i-- {
		c := m.staged[i]
		if c.Kind == ChangeSetValue && c.Name == name && c.Profile == profile {
			return c.Value
		}
	}
	v := m.mx.Vars[name]
	if val, ok := v.Profiles[profile]; ok {
		return val
	}
	if v.Value != nil {
		return *v.Value
	}
	return ""
}

// reclampRow keeps row inside the visible list's bounds after the set of
// visible rows changes size (filtering, mainly). Without this, a row index
// left pointing past the end of a narrowed list is not "on" any row, and the
// next e would edit whatever View() happens to render for an out-of-range
// cursor instead of a row the user can see.
func (m *Model) reclampRow() {
	n := len(m.visibleNames())
	if n == 0 {
		m.row = 0
		return
	}
	if m.row >= n {
		m.row = n - 1
	}
	if m.row < 0 {
		m.row = 0
	}
}

// moveRow shifts the row cursor by delta, clamping at both edges rather than
// wrapping. Unlike the tab ring, a variable list can be long, and wrapping
// from the last row to the first would put a later delete key on whatever
// variable the cursor lands on instead of refusing to move.
func (m *Model) moveRow(delta int) {
	if m.mx == nil {
		return
	}
	names := m.visibleNames()
	if len(names) == 0 {
		return
	}
	m.row = clamp(m.row+delta, 0, len(names)-1)
}

// moveCol shifts the column cursor by delta, clamping for the same reason
// moveRow does.
func (m *Model) moveCol(delta int) {
	if m.mx == nil || len(m.mx.Profiles) == 0 {
		return
	}
	m.col = clamp(m.col+delta, 0, len(m.mx.Profiles)-1)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update advances the state machine. Purity holds here as it does in
// internal/tui: every branch below only reads and writes m and msg, and
// nothing here performs I/O.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case RevealedMsg:
		m.setRevealed(msg.Name, msg.Profile, msg.Value)
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case ModeEditValue, ModeEditSecret, ModeAddName, ModeFilter:
			return m.updateInput(msg)
		default:
			return m.updateBrowse(msg)
		}
	}
	return m, nil
}

func (m Model) updateBrowse(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyTab:
		m.tab = (m.tab + 1) % tabCount
		return m, nil
	case tea.KeyShiftTab:
		m.tab = (m.tab - 1 + tabCount) % tabCount
		return m, nil
	case tea.KeyUp:
		m.moveRow(-1)
		return m, nil
	case tea.KeyDown:
		m.moveRow(1)
		return m, nil
	case tea.KeyLeft:
		m.moveCol(-1)
		return m, nil
	case tea.KeyRight:
		m.moveCol(1)
		return m, nil
	}
	switch msg.String() {
	case "1":
		m.tab = TabOverview
	case "2":
		m.tab = TabVariables
	case "3":
		m.tab = TabProfiles
	case "4":
		m.tab = TabOutputs
	case "j":
		m.moveRow(1)
	case "k":
		m.moveRow(-1)
	case "h":
		m.moveCol(-1)
	case "l":
		m.moveCol(1)
	case "e":
		m.beginEdit()
	case "a":
		if m.tab == TabVariables {
			m.mode = ModeAddName
			m.buffer = ""
		}
	case "d":
		m.toggleDelete()
	case "/":
		if m.tab == TabVariables {
			m.mode = ModeFilter
			m.buffer = ""
		}
	case "r":
		if req, ok := m.revealRequest(); ok {
			return m, func() tea.Msg { return req }
		}
	case "w":
		m.saved = true
		return m, tea.Quit
	case "q":
		// No confirmation yet: a later task adds the "are you sure" gate
		// this codebase's other quit-with-unsaved-changes prompts use.
		// Until then q simply ends the session with saved left false, which
		// is already enough for the cli to discard whatever is staged.
		return m, tea.Quit
	}
	return m, nil
}

// revealRequest names the (Name, Profile) cell r would fetch, if the cursor
// is on a secret cell. It only reads state; it never fetches anything --
// that is the whole reason reveal is a message: a stray r must never risk
// the authorization prompt a real keyring Get can raise on macOS.
func (m Model) revealRequest() (RevealRequestMsg, bool) {
	if m.tab != TabVariables {
		return RevealRequestMsg{}, false
	}
	names := m.visibleNames()
	if len(names) == 0 || m.col >= len(m.mx.Profiles) {
		return RevealRequestMsg{}, false
	}
	name := names[m.row]
	if !m.mx.Vars[name].Secret {
		return RevealRequestMsg{}, false
	}
	return RevealRequestMsg{Name: name, Profile: m.mx.Profiles[m.col]}, true
}

// beginEdit opens an input mode on the cell under the cursor: ModeEditValue
// for a plain variable, ModeEditSecret for a secret. One key, one meaning,
// whatever the row -- `s` is the separate, row-level toggle of secret-ness.
//
// A secret's buffer starts empty rather than prefilled with its current
// value, unlike a plain cell's. Prefilling it would mean reading the secret
// just to open the prompt, which is exactly the keyring Get this package's
// design (Has-only, no Get) exists to avoid triggering unasked.
func (m *Model) beginEdit() {
	if m.tab != TabVariables {
		return
	}
	names := m.visibleNames()
	if len(names) == 0 || len(m.mx.Profiles) == 0 {
		return
	}
	name := names[m.row]
	if m.mx.Vars[name].Secret {
		m.mode = ModeEditSecret
		m.buffer = ""
		return
	}
	profile := m.mx.Profiles[m.col]
	m.mode = ModeEditValue
	m.buffer = m.currentValue(name, profile)
}

// toggleDelete stages delete-var for the row under the cursor, or un-stages
// it if it is already staged -- the user has not saved anything yet and can
// still change their mind.
func (m *Model) toggleDelete() {
	if m.tab != TabVariables {
		return
	}
	names := m.visibleNames()
	if len(names) == 0 {
		return
	}
	name := names[m.row]
	for i, c := range m.staged {
		if c.Kind == ChangeDeleteVar && c.Name == name {
			m.staged = append(m.staged[:i], m.staged[i+1:]...)
			return
		}
	}
	m.staged = append(m.staged, Change{Kind: ChangeDeleteVar, Name: name})
}

// updateInput handles typing in whichever of the three text-entry modes is
// active, mirroring internal/tui's updateInput: backspace, printable runes
// and space edit the buffer; esc abandons it; enter commits it per mode.
func (m Model) updateInput(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = ModeBrowse
		m.buffer = ""
		return m, nil

	case tea.KeyEnter:
		switch m.mode {
		case ModeEditValue:
			names := m.visibleNames()
			if m.row < len(names) && m.col < len(m.mx.Profiles) {
				m.stage(Change{
					Kind:    ChangeSetValue,
					Name:    names[m.row],
					Profile: m.mx.Profiles[m.col],
					Value:   m.buffer,
				})
			}
		case ModeEditSecret:
			names := m.visibleNames()
			if m.row < len(names) && m.col < len(m.mx.Profiles) {
				m.stage(Change{
					Kind:    ChangeSetSecret,
					Name:    names[m.row],
					Profile: m.mx.Profiles[m.col],
					Value:   m.buffer,
				})
			}
		case ModeAddName:
			if m.buffer != "" {
				exists := false
				for _, n := range m.allNames() {
					if n == m.buffer {
						exists = true
						break
					}
				}
				if !exists {
					m.stage(Change{Kind: ChangeAddVar, Name: m.buffer})
				}
			}
		case ModeFilter:
			m.filter = m.buffer
			m.reclampRow()
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

// View renders the tab bar and the active tab's body.
func (m Model) View() string {
	var b strings.Builder

	for t := Tab(0); t < tabCount; t++ {
		marker := " "
		if t == m.tab {
			marker = ">"
		}
		fmt.Fprintf(&b, "%s%d:%s  ", marker, int(t)+1, t)
	}
	b.WriteString("\n\n")

	switch m.tab {
	case TabOverview:
		b.WriteString(m.viewOverview())
	case TabVariables:
		b.WriteString(m.viewVariables())
	case TabProfiles:
		b.WriteString(m.viewProfiles())
	case TabOutputs:
		b.WriteString(m.viewOutputs())
	}

	return b.String()
}

func (m Model) viewOverview() string {
	if len(m.problems) == 0 {
		return "No problems found.\n"
	}
	var b strings.Builder
	for _, p := range m.problems {
		fmt.Fprintf(&b, "%s -- %s\n", p.Detail, p.Fix)
	}
	return b.String()
}

// viewProfiles is a placeholder: profile management is `trenvy profile`'s job
// today (add, default, list, rm, use), not this tab's, until a later task
// wires it up. Naming that command here rather than leaving the panel empty
// tells the user where to go instead of looking broken.
func (m Model) viewProfiles() string {
	return "Profile management is not built into this view yet.\n" +
		"Use `trenvy profile` (add, default, list, rm, use) from the command line.\n"
}

// viewOutputs mirrors viewProfiles: `trenvy output` (add, default, list,
// rename, rm) already does this job.
func (m Model) viewOutputs() string {
	return "Output management is not built into this view yet.\n" +
		"Use `trenvy output` (add, default, list, rename, rm) from the command line.\n"
}

// KeyRune returns the message for a single rune key press, so callers can
// drive a model without depending on bubbletea's key types. Mirrors
// internal/tui.KeyW.
func KeyRune(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// KeyType returns the message for a non-rune key, e.g. tea.KeyTab.
func KeyType(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
