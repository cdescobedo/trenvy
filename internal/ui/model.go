package ui

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

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
	// ModeSwitchProject is the panel `p` opens: every registered project,
	// with a cursor that enter resolves to a choice.
	ModeSwitchProject
	// ModeConfirmSwitch is the question asked when the switcher's cursor is
	// on a project other than the current one and staged changes exist.
	// Mirrors internal/tui's ModeConfirmQuit's y/anything-else shape, but
	// names a sharper loss: staged changes live only in this model's memory
	// for the one session (Task 9), so switching does not defer them the way
	// leaving them staged across a quit-and-reopen might read -- it destroys
	// them the instant the new project's session starts.
	ModeConfirmSwitch
	// ModeConfirmQuit is the question asked when q is pressed with staged
	// changes present. Mirrors internal/tui's ModeConfirmQuit: every key but
	// y answers no, and ctrl+c cancels unconditionally even from here (see
	// Update). Unlike ModeConfirmSwitch, there is nothing to remember about
	// which target was asked for -- quitting has exactly one destination.
	ModeConfirmQuit
	// ModeHelp is the panel `?` opens: the full key list, available from any
	// tab like the switcher. It exists because the short, persistent hint
	// line (see keyHints) is deliberately incomplete -- naming every key,
	// including movement and tab-switching, would defeat the point of a
	// one-line reminder -- so `?` is where the rest is discoverable.
	ModeHelp
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
	case ModeSwitchProject:
		return "switch-project"
	case ModeConfirmSwitch:
		return "confirm-switch"
	case ModeConfirmQuit:
		return "confirm-quit"
	case ModeHelp:
		return "help"
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
	project      string
	profile      string // active profile name, e.g. "prod"
	source       string // where profile came from: "--profile", "TRENVY_PROFILE", "default"
	manifestPath string // path to this project's manifest file
	worktree     string // this worktree's root, resolved the same way `trenvy status` does
	backend      string // keyring backend name
	mx           *Matrix
	problems     []Problem
	// outputFreshnessUnverified is true when at least one output's freshness
	// could not be determined here because doing so would require reading a
	// secret's value (see internal/cli's outputStates). Such an output never
	// becomes its own Problem -- that would recreate the permanent,
	// unfixable false alarm Fix 2 removed -- but an empty problem list
	// implicitly claims "here is everything wrong", which is false while
	// this is true. viewOverview adds one sentence to the healthy-state line
	// for it, once, rather than a per-output note that never changes and so
	// never gets read.
	outputFreshnessUnverified bool
	tab                       Tab
	row                       int // selected index into visibleNames()
	col                       int // selected index into mx.Profiles

	// projects lists every registered project id, for the switcher `p`
	// opens. It never changes after New: internal/ui does no I/O, so a
	// registry change made from another session is only ever seen the next
	// time the cli host builds a fresh model.
	projects []string
	// switchRow is the switcher panel's own cursor, scoped to projects the
	// same way overviewRow is scoped to problems -- a separate field so
	// opening the switcher and moving its cursor never disturbs row/col.
	switchRow int
	// switchTo is the project id chosen in the switcher, if the session
	// ended that way. Set only alongside a tea.Quit, mirroring saved -- see
	// SwitchTo's doc for why the cli host must check this before Saved().
	switchTo string
	// pendingSwitch holds the id ModeConfirmSwitch is asking about, so a
	// decline can restore ModeBrowse without losing which project "y" would
	// have meant.
	pendingSwitch string

	// overviewRow is the Overview tab's own cursor, scoped to problems. It is
	// a separate field from row/col rather than reusing them: those are
	// scoped to the Variables matrix, and moving through the problem list
	// would otherwise silently move the matrix cursor too -- so jumping to a
	// problem, or later pressing e, could land on a cell the user never
	// selected. Flagged during Task 5's review and recorded for this task.
	overviewRow int

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

// Params is what New needs to build a model. Named fields, not positional
// strings: Project, Profile, Source and Backend are four adjacent strings,
// and a positional New(project, profile, source, backend string, ...) lets
// two of them be transposed at a call site and still compile -- silently
// printing, say, "profile dev (keychain)" with backend where source belongs.
// A UI whose job includes telling the user which profile is active is the
// wrong place for that kind of silent swap.
//
// It was also built to grow: Task 11's project switcher needs the list of
// registered projects, which this package cannot read itself since it does
// no I/O. Projects is that growth -- a new field, not a break to any call
// site's argument order, which is exactly what a positional signature would
// have forced the moment it grew a fifth string.
type Params struct {
	Project      string // project id
	Profile      string // active profile name, e.g. "prod"
	Source       string // where profile came from: "--profile", "TRENVY_PROFILE", "default"
	ManifestPath string // path to this project's manifest file, e.g. from config.ManifestPath
	Worktree     string // this worktree's root, e.g. from resolve.WorktreeRoot
	Backend      string // keyring backend name

	Matrix   *Matrix
	Problems []Problem

	// OutputFreshnessUnverified is true when at least one output's
	// freshness could not be determined without reading a secret -- see
	// internal/cli's outputStates, which computes it alongside Problems'
	// own outputs map. Never folded into Problems itself: it is not a
	// per-output fact this package could point a cell or a Fix at, only a
	// caveat on how complete the Overview's own answer is.
	OutputFreshnessUnverified bool

	// Projects lists every registered project id, for the switcher `p`
	// opens. internal/cli loads it via config.LoadRegistry(); this package
	// never reads the registry itself.
	Projects []string
}

// New builds a model over one project's matrix and its detected problems.
// Profile, Source, ManifestPath, Worktree and Backend are the same facts
// `trenvy status` reports -- project id, active profile and where it came
// from, the manifest's path, the worktree root, and the keyring backend --
// passed in rather than re-derived, since this package does no I/O and
// cannot read them itself.
func New(p Params) Model {
	return Model{
		project:                   p.Project,
		profile:                   p.Profile,
		source:                    p.Source,
		manifestPath:              p.ManifestPath,
		worktree:                  p.Worktree,
		backend:                   p.Backend,
		mx:                        p.Matrix,
		problems:                  p.Problems,
		outputFreshnessUnverified: p.OutputFreshnessUnverified,
		projects:                  p.Projects,
		tab:                       TabOverview,
	}
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

// SwitchTo reports the project id chosen in the switcher, if the session
// ended that way. The cli host checks this before Saved(): a switch quits
// the session exactly like w and q do, but is neither of them -- it must
// never be read as "write" (saved stays false) or as "discard and stop"
// (the host loads the chosen project and keeps going instead of returning).
func (m Model) SwitchTo() (string, bool) {
	return m.switchTo, m.switchTo != ""
}

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
// write is staged without saying what it will write; r itself refuses this
// cell outright while the write is pending (see revealRequest), rather than
// fetching the keyring's current value only to show "pending" over it anyway
// -- so there is no read left that could ever disagree with this text.
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

// moveCursorDown and moveCursorUp route j/k and the down/up arrows to
// whichever cursor the active tab owns: the Overview tab's own overviewRow,
// or the Variables matrix's row. Routing by tab here, rather than in every
// caller, is what keeps the two cursors from ever being moved by the wrong
// tab's keypress.
func (m *Model) moveCursorDown() {
	if m.tab == TabOverview {
		m.moveOverviewRow(1)
		return
	}
	m.moveRow(1)
}

func (m *Model) moveCursorUp() {
	if m.tab == TabOverview {
		m.moveOverviewRow(-1)
		return
	}
	m.moveRow(-1)
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

// moveOverviewRow shifts the Overview tab's own cursor by delta, clamping
// rather than wrapping for the same reason moveRow does. It never touches
// row/col: see overviewRow's field comment.
func (m *Model) moveOverviewRow(delta int) {
	if len(m.problems) == 0 {
		return
	}
	m.overviewRow = clamp(m.overviewRow+delta, 0, len(m.problems)-1)
}

// jumpToProblem moves the cursor to the cell the problem under the Overview
// cursor names. A "missing" problem always has a Profile and a matrix cell:
// enter switches to Variables with row/col on exactly that (name, profile).
//
// An "output" problem has no cell -- Profile is empty, since freshness is a
// property of the whole rendered file, not one variable under one profile.
// Guessing a cell for it would be a silent wrong answer, so enter instead
// switches to the Outputs tab: inert in phase 1, but the closest the model
// can move today without landing somewhere the problem never named.
func (m *Model) jumpToProblem() {
	if m.overviewRow < 0 || m.overviewRow >= len(m.problems) {
		return
	}
	p := m.problems[m.overviewRow]
	if p.Kind != "missing" {
		m.tab = TabOutputs
		return
	}

	m.tab = TabVariables
	// Clear the filter: the jump must land exactly, and a filter that
	// happens to hide p.Name would otherwise make the row impossible to
	// find, or worse, silently select the wrong row.
	m.filter = ""
	for i, n := range m.visibleNames() {
		if n == p.Name {
			m.row = i
			break
		}
	}
	for i, profile := range m.mx.Profiles {
		if profile == p.Profile {
			m.col = i
			break
		}
	}
}

// beginSwitch opens the project switcher. The cursor starts on the current
// project when it appears in the list, so glancing at what else is
// registered and backing out with esc does not require a keypress first just
// to find where you already are.
func (m *Model) beginSwitch() {
	m.mode = ModeSwitchProject
	m.switchRow = 0
	for i, id := range m.projects {
		if id == m.project {
			m.switchRow = i
			break
		}
	}
}

// moveSwitchRow shifts the switcher's own cursor by delta, clamping rather
// than wrapping for the same reason moveRow does.
func (m *Model) moveSwitchRow(delta int) {
	if len(m.projects) == 0 {
		return
	}
	m.switchRow = clamp(m.switchRow+delta, 0, len(m.projects)-1)
}

// updateSwitch handles keys while the switcher panel is open: j/k and the
// arrows move its cursor, enter resolves a choice, esc backs out without
// choosing anything.
func (m Model) updateSwitch(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = ModeBrowse
		return m, nil
	case tea.KeyUp:
		m.moveSwitchRow(-1)
		return m, nil
	case tea.KeyDown:
		m.moveSwitchRow(1)
		return m, nil
	case tea.KeyEnter:
		return m.selectProject()
	}
	switch msg.String() {
	case "j":
		m.moveSwitchRow(1)
	case "k":
		m.moveSwitchRow(-1)
	}
	return m, nil
}

// selectProject resolves the switcher's cursor to a choice. Picking the
// project already open closes the panel with nothing to rebuild. Picking a
// different one with nothing staged ends the session immediately with
// switchTo set, for the cli host to act on; with staged changes it detours
// through ModeConfirmSwitch first, since those changes exist only in this
// model's memory for this one session (Task 9) and switching away loses them
// for good, with no session left afterward to recover them from.
func (m Model) selectProject() (Model, tea.Cmd) {
	if m.switchRow < 0 || m.switchRow >= len(m.projects) {
		m.mode = ModeBrowse
		return m, nil
	}
	id := m.projects[m.switchRow]
	if id == m.project {
		m.mode = ModeBrowse
		return m, nil
	}
	if len(m.staged) > 0 {
		m.pendingSwitch = id
		m.mode = ModeConfirmSwitch
		return m, nil
	}
	m.switchTo = id
	return m, tea.Quit
}

// updateConfirmSwitch answers the "discard staged changes and switch?"
// question ModeConfirmSwitch asks, mirroring internal/tui's updateConfirm's
// y/anything-else shape. Declining must leave staged and project exactly as
// they were: that is the entire point of asking first, and any branch that
// cleared staged, changed project, or set switchTo on a decline would make
// the confirmation a formality rather than the one thing standing between a
// user and silently losing work they thought was still pending.
func (m Model) updateConfirmSwitch(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		m.switchTo = m.pendingSwitch
		m.pendingSwitch = ""
		m.staged = nil
		return m, tea.Quit
	default:
		m.pendingSwitch = ""
		m.mode = ModeBrowse
		return m, nil
	}
}

// updateConfirmQuit answers the "discard staged changes and quit?" question
// ModeConfirmQuit asks, mirroring internal/tui's updateConfirm's
// y/anything-else shape (and updateConfirmSwitch's, this package's own copy
// of the same rule). Declining must leave staged, mode, and the cursor
// exactly as they were -- restoring ModeBrowse without touching row, col or
// tab is what lets the user resume exactly where q interrupted them, rather
// than landing back in browse mode having lost their place.
func (m Model) updateConfirmQuit(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		// The session is ending regardless of what Staged() holds -- the
		// cli host commits only when Saved() is true (see TestQQuitsWithoutSaving) --
		// but clearing it here mirrors updateConfirmSwitch's own "destroy on
		// confirm" and keeps the model's own state honest about what it is
		// discarding, rather than leaving stale Changes behind on a Model
		// that happens to be reused after Run returns.
		m.staged = nil
		return m, tea.Quit
	default:
		m.mode = ModeBrowse
		return m, nil
	}
}

// updateHelp handles keys while the help panel is open: esc backs out,
// mirroring the switcher's own esc-cancels convention. Any other key is
// ignored rather than falling through to whatever it would have meant in
// ModeBrowse -- a key pressed while reading the key list must not also act
// on the cursor underneath it.
func (m Model) updateHelp(msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		m.mode = ModeBrowse
	}
	return m, nil
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
		// Ctrl+C is an immediate, unconditional cancel in every mode,
		// mirroring internal/tui: bubbletea's raw mode delivers it as an
		// ordinary KeyMsg, not SIGINT, so without this a user mid-edit, or
		// facing the quit confirmation itself, has no way out except
		// answering a prompt they may not understand -- and from
		// ModeConfirmQuit it would otherwise fall through to "no" like any
		// other key and refuse to quit at all.
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		switch m.mode {
		case ModeEditValue, ModeEditSecret, ModeAddName, ModeFilter:
			return m.updateInput(msg)
		case ModeSwitchProject:
			return m.updateSwitch(msg)
		case ModeConfirmSwitch:
			return m.updateConfirmSwitch(msg)
		case ModeConfirmQuit:
			return m.updateConfirmQuit(msg)
		case ModeHelp:
			return m.updateHelp(msg)
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
		m.moveCursorUp()
		return m, nil
	case tea.KeyDown:
		m.moveCursorDown()
		return m, nil
	case tea.KeyLeft:
		if m.tab == TabVariables {
			m.moveCol(-1)
		}
		return m, nil
	case tea.KeyRight:
		if m.tab == TabVariables {
			m.moveCol(1)
		}
		return m, nil
	case tea.KeyEnter:
		if m.tab == TabOverview {
			m.jumpToProblem()
		}
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
		m.moveCursorDown()
	case "k":
		m.moveCursorUp()
	case "h":
		if m.tab == TabVariables {
			m.moveCol(-1)
		}
	case "l":
		if m.tab == TabVariables {
			m.moveCol(1)
		}
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
	case "p":
		// Available from any tab, unlike e/a/d: picking a different
		// registered project is not a Variables-tab concept.
		m.beginSwitch()
	case "?":
		// Available from any tab, like p: the full key list is not scoped
		// to whichever tab happens to be open when someone wonders what
		// they can press.
		m.mode = ModeHelp
	case "w":
		m.saved = true
		return m, tea.Quit
	case "q":
		// Nothing staged means nothing to lose, so q leaves immediately --
		// asking first here would be a confirmation with nothing behind it.
		// With staged changes present, it asks first (updateConfirmQuit),
		// mirroring internal/tui's own ModeConfirmQuit gate.
		if len(m.staged) > 0 {
			m.mode = ModeConfirmQuit
			return m, nil
		}
		return m, tea.Quit
	}
	return m, nil
}

// revealRequest names the (Name, Profile) cell r would fetch, if the cursor
// is on a secret cell. It only reads state; it never fetches anything --
// that is the whole reason reveal is a message: a stray r must never risk
// the authorization prompt a real keyring Get can raise on macOS.
//
// It also refuses when this exact cell already has a staged set-secret
// (Fix 6): cellText shows "secret (pending)" for that cell no matter what
// the keyring currently holds, so fetching the current value would cost a
// real Get -- a real authorization prompt on macOS -- for an answer that is
// never displayed and immediately thrown away. That was the discarded
// prompt this fix removes: r looked like a silent no-op on such a cell, but
// still ran the fetch underneath it.
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
	profile := m.mx.Profiles[m.col]
	for _, c := range m.staged {
		if c.Kind == ChangeSetSecret && c.Name == name && c.Profile == profile {
			return RevealRequestMsg{}, false
		}
	}
	return RevealRequestMsg{Name: name, Profile: profile}, true
}

// beginEdit opens an input mode on the cell under the cursor: ModeEditValue
// for a plain variable, ModeEditSecret for a secret. One key, one meaning,
// whatever the row -- secret-ness toggling is not a key this package has (see
// toggleDelete's own doc for why d and e must never both apply to one row).
//
// A staged delete for this row is cancelled first, unconditionally. Leaving
// it staged underneath a new edit is exactly the Fix 1 bug: commitStaged
// applies changes in staged order, so a delete followed by a set-value read
// Vars[name] AFTER the delete had already removed it, silently reconstructing
// the variable from a zero Var with every profile but the one just edited
// wiped -- the delete itself discarded in the process, with no warning
// either way. Cancelling here means editing a deleted row is now a clear,
// visible decision to keep it: the "(deleted)" marker disappears from the
// row the instant this runs, so nothing about undoing the delete is silent.
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
	m.removeDelete(name)
	if m.mx.Vars[name].Secret {
		m.mode = ModeEditSecret
		m.buffer = ""
		return
	}
	profile := m.mx.Profiles[m.col]
	m.mode = ModeEditValue
	m.buffer = m.currentValue(name, profile)
}

// removeDelete un-stages a delete-var change for name, if one is staged.
// Shared by toggleDelete's own toggle-off and beginEdit's cancel-on-edit, so
// there is exactly one place that knows how to find and drop a staged
// delete-var change rather than two loops that could drift apart.
func (m *Model) removeDelete(name string) {
	for i, c := range m.staged {
		if c.Kind == ChangeDeleteVar && c.Name == name {
			m.staged = append(m.staged[:i], m.staged[i+1:]...)
			return
		}
	}
}

// toggleDelete stages delete-var for the row under the cursor, or un-stages
// it if it is already staged -- the user has not saved anything yet and can
// still change their mind.
//
// A row with a staged cell edit (set-value or set-secret) refuses a new
// delete instead of adding one: committing both for the same variable is the
// other half of the Fix 1 bug (see beginEdit's doc) -- delete-then-edit is
// prevented there by cancelling the delete, but edit-then-delete reaches
// commitStaged with both still staged unless d itself declines here. The two
// keys now settle on one outcome regardless of order: whichever of d/e is
// pressed second, the row ends up edited, never resurrected with a gap.
func (m *Model) toggleDelete() {
	if m.tab != TabVariables {
		return
	}
	names := m.visibleNames()
	if len(names) == 0 {
		return
	}
	name := names[m.row]
	if m.isDeleted(name) {
		m.removeDelete(name)
		return
	}
	for _, c := range m.staged {
		if (c.Kind == ChangeSetValue || c.Kind == ChangeSetSecret) && c.Name == name {
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

// View renders the tab bar and the active tab's body, or the project
// switcher's or help panel's own view while one of those is open.
func (m Model) View() string {
	if m.mode == ModeSwitchProject || m.mode == ModeConfirmSwitch {
		return m.viewSwitcher()
	}
	if m.mode == ModeHelp {
		return m.viewHelp()
	}

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

	// The status line lives outside the per-tab switch above, not inside
	// viewVariables: q and its confirmation apply from every tab, so what is
	// pending must be visible no matter which one is on screen. keyHints
	// follows it for the same reason -- discoverability is one of this
	// feature's four stated purposes, so the reminder of what the keys do
	// must not depend on which tab happens to be open, or on anything being
	// staged the way statusLine's own w/q mention does.
	fmt.Fprintf(&b, "\n%s\n", m.statusLine())
	fmt.Fprintln(&b, m.keyHints())
	if m.mode == ModeConfirmQuit {
		b.WriteString("quit anyway and discard them? [y/N]\n")
	}

	return b.String()
}

// keyHints is the persistent, short reminder of what the keys do -- shown on
// every tab regardless of staged count, unlike statusLine's own w/q mention.
// It is deliberately incomplete (no movement keys, no tab switching, no w/q,
// already covered by statusLine): naming everything here would defeat the
// point of a one-line reminder, so `?` opens viewHelp for the rest. `s` is
// never listed -- Ruling 8 descoped the secret-toggle key, and it does
// nothing today, so naming it here would be worse than no hint at all.
func (m Model) keyHints() string {
	return "e edit · r reveal · a add · d delete · / filter · p switch · ? help"
}

// viewHelp renders the full key list `?` opens, replacing the tab bar and
// body the same way viewSwitcher does. Every key named here is one this
// package actually implements -- checked by running each in a built binary,
// not transcribed from the spec -- and `s` is never among them.
func (m Model) viewHelp() string {
	var b strings.Builder
	b.WriteString("Keys\n\n")
	rows := []struct{ key, action string }{
		{"1-4, tab, shift+tab", "switch tabs"},
		{"j/k, h/l, up/down/left/right", "move the cursor"},
		{"e", "edit the cell under the cursor (masked while typing if it is a secret)"},
		{"r", "reveal the cell under the cursor's real value (one keyring read)"},
		{"a", "add a variable"},
		{"d", "mark the selected variable deleted (press again to un-mark it)"},
		{"/", "filter rows by name substring"},
		{"p", "switch to a different registered project"},
		{"w", "commit everything staged"},
		{"q", "quit, asking first if anything is staged"},
		{"ctrl+c", "quit immediately, without asking, without writing anything"},
		{"?", "show this list"},
	}
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		fmt.Fprintf(w, "  %s\t%s\n", r.key, r.action)
	}
	_ = w.Flush()
	b.WriteString("\npress esc to go back\n")
	return b.String()
}

// viewOverview renders the landing screen: the same project, profile (and
// its source), manifest path, worktree and backend facts `trenvy status`
// reports (same words, same order, so a user does not learn a second
// vocabulary for them here), then either the problem list or a plain
// statement that there is nothing to fix.
//
// This is the screen a healthy project sees most of the time, so "no
// problems" must read as success, not as an empty panel that looks broken.
func (m Model) viewOverview() string {
	var b strings.Builder
	fmt.Fprintf(&b, "project   %s\n", m.project)
	fmt.Fprintf(&b, "profile   %s  (%s)\n", m.profile, m.source)
	fmt.Fprintf(&b, "manifest  %s\n", m.manifestPath)
	fmt.Fprintf(&b, "worktree  %s\n", m.worktree)
	fmt.Fprintf(&b, "backend   %s\n\n", m.backend)

	if len(m.problems) == 0 {
		b.WriteString("No problems found. This project is healthy -- nothing needs attention.\n")
		if m.outputFreshnessUnverified {
			// An empty list otherwise implicitly claims "here is everything
			// wrong with this project", which is false the instant an
			// output's freshness went unchecked -- the reader has no way to
			// tell "nothing is wrong" from "nothing I can see is wrong".
			// Said once, here, never per-output: a note on every
			// secret-bearing output would be permanent noise that never
			// changes and so never gets read.
			b.WriteString("Freshness of outputs with secrets is not checked here -- trenvy status checks it.\n")
		}
		return b.String()
	}

	for i, p := range m.problems {
		cursor := " "
		if i == m.overviewRow {
			cursor = ">"
		}
		fmt.Fprintf(&b, "%s %s -- %s\n", cursor, p.Detail, p.Fix)
	}
	fmt.Fprint(&b, "\npress enter to jump to the cell that fixes the selected problem\n")
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
