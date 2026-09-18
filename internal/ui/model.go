package ui

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/resolve"
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
	// ModeConfirmPromote is the question asked when s is pressed on a
	// variable whose value is shared across profiles (see promoteSecret): the
	// promotion is about to turn that one value into several keyring items,
	// which a per-profile override never does since it already has one value
	// per profile. Mirrors ModeConfirmSwitch and ModeConfirmQuit's
	// y/anything-else shape; declining must leave staged untouched, the same
	// rule those two already hold to (see updateConfirmPromote).
	ModeConfirmPromote
	// ModeHelp is the panel `?` opens: the full key list, available from any
	// tab like the switcher. It exists because the short, persistent hint
	// line (see keyHints) is deliberately incomplete -- naming every key,
	// including movement and tab-switching, would defeat the point of a
	// one-line reminder -- so `?` is where the rest is discoverable.
	ModeHelp
	// ModeOutputAddName is typing the name for a new output -- step 1 of 3
	// in the Outputs tab's `a`. Nothing stages until all three steps
	// complete (see confirmOutputAddPath): there is no output worth having
	// with only a name, or only a name and a format.
	ModeOutputAddName
	// ModeOutputAddFormat is step 2 of 3: typing the new output's format.
	ModeOutputAddFormat
	// ModeOutputAddPath is step 3 of 3: typing the new output's path.
	// Enter here stages add-output with all three facts at once.
	ModeOutputAddPath
	// ModeOutputRename is typing the new name for the Outputs tab row under
	// the cursor (`r`).
	ModeOutputRename
	// ModeOutputGlobs is the include/exclude editor `enter` opens on the
	// Outputs tab row under the cursor (Task 8): a checkbox per variable
	// this package already knows about, plus the mode toggle (`m`) that
	// says which of the two ModeEverythingExcept/ModeOnlyThese futures a
	// variable that does not exist yet falls into. Nothing stages until
	// enter commits it as set-globs; esc abandons the whole edit, mirroring
	// every other input mode's own esc-abandons convention.
	ModeOutputGlobs

	// ModeEverythingExcept and ModeOnlyThese are not FSM states the way the
	// modes above are; they answer a different question -- for one output's
	// membership editor (Task 8), which of two futures a variable that does
	// not exist yet falls into. WriteGlobs (globs.go) takes one of these two
	// as input and is the only place that reads them. They live on this
	// type, rather than a type of their own, because Task 8's editor holds
	// the current choice as ordinary Model state alongside every other mode.
	ModeEverythingExcept
	ModeOnlyThese
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
	case ModeConfirmPromote:
		return "confirm-promote"
	case ModeHelp:
		return "help"
	case ModeOutputAddName:
		return "output-add-name"
	case ModeOutputAddFormat:
		return "output-add-format"
	case ModeOutputAddPath:
		return "output-add-path"
	case ModeOutputRename:
		return "output-rename"
	case ModeOutputGlobs:
		return "output-globs"
	case ModeEverythingExcept:
		return "everything-except"
	case ModeOnlyThese:
		return "only-these"
	}
	return "unknown"
}

// Change kinds. Task 9 switches on Kind to decide where a change lands: the
// manifest for set-value/add-var/delete-var, the keyring for set-secret.
//
// ChangePromoteSecret is `s`'s row-level record: it stands apart from the
// set-secret changes `s` stages alongside it (see promoteSecret) because a
// set-secret can also come from editing a cell that was already secret,
// which must not flip anything in the manifest. Only ChangePromoteSecret
// tells the phased commit (2a's Task 3) "this variable becomes secret ->
// flip the manifest entry to secret = true with no Value and no Profiles".
//
// The four Change*Output kinds are the Outputs tab's own staged edits (Task
// 5). They key by the output's identity at the start of the session (or the
// name it was added under this session), never by a pending rename's
// target -- see outputIdentityNames -- so a row that is both renamed and,
// say, made default in the same session stages two Changes that agree about
// which row they mean.
//
// ChangeSetGlobs is Task 8's own addition, keyed by identity the same way:
// the include/exclude editor's save. It did not exist when Task 6 built
// commitStaged's phases, so wiring it in means adding a case to whichever
// phase it belongs in, never restructuring the phases themselves (see
// commitStaged's own doc in internal/cli/ui.go).
const (
	ChangeSetValue      = "set-value"
	ChangeAddVar        = "add-var"
	ChangeDeleteVar     = "delete-var"
	ChangeSetSecret     = "set-secret"
	ChangePromoteSecret = "promote-secret"
	ChangeAddOutput     = "add-output"
	ChangeRenameOutput  = "rename-output"
	ChangeRemoveOutput  = "remove-output"
	ChangeDefaultOutput = "default-output"
	ChangeSetGlobs      = "set-globs"
)

// Change is one uncommitted edit, held in the model until w writes it to the
// manifest, the state file or the keyring (Task 6). Profile says which
// column a set-value or set-secret belongs to; a value written to the wrong
// profile would be silent data corruption, so the field is never left to be
// inferred from cursor position at commit time.
//
// Value also carries rename-output's target name: the name the output
// becomes, reusing the "what this change establishes" meaning it already
// has for set-value and set-secret rather than growing a field that would
// mean the same thing under a different name for one kind.
//
// Format and Path carry add-output's definition; every other kind leaves
// them empty.
//
// Include and Exclude carry set-globs' own definition: exactly what
// WriteGlobs produced from the editor's checkbox state and mode at the
// moment enter saved it. Every other kind leaves them nil.
type Change struct {
	Kind, Name, Profile, Value string
	Format, Path               string
	Include, Exclude           []string
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
	// outputs is the Outputs tab's own data: one row per manifest output,
	// built once by the cli host from the same states map that feeds
	// Problems above -- never a second read of app.Manifest.Outputs that
	// could classify freshness differently than the Overview does.
	outputs []OutputRow
	tab     Tab
	row     int // selected index into visibleNames()
	col     int // selected index into mx.Profiles

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
	// pendingPromote holds the variable name ModeConfirmPromote is asking
	// about, mirroring pendingSwitch for the same reason: a decline must
	// restore ModeBrowse without losing which row "y" would have promoted.
	pendingPromote string

	// overviewRow is the Overview tab's own cursor, scoped to problems. It is
	// a separate field from row/col rather than reusing them: those are
	// scoped to the Variables matrix, and moving through the problem list
	// would otherwise silently move the matrix cursor too -- so jumping to a
	// problem, or later pressing e, could land on a cell the user never
	// selected. Flagged during Task 5's review and recorded for this task.
	overviewRow int

	// outputRow is the Outputs tab's own cursor, scoped to outputIdentityNames
	// the same way overviewRow is scoped to problems and switchRow to
	// projects: a separate field so moving it never disturbs row/col, which
	// stay scoped to the Variables matrix.
	outputRow int
	// draftOutputName and draftOutputFormat hold the first two steps of the
	// Outputs tab's `a` wizard while the third is being typed -- buffer only
	// ever holds the CURRENT step, so the earlier steps need somewhere to
	// live until confirmOutputAddPath stages all three together. Both are
	// cleared the instant the flow ends, staged or abandoned, so a later `a`
	// never starts from a stale draft.
	draftOutputName, draftOutputFormat string

	// globsTarget is the output identity ModeOutputGlobs is currently
	// editing -- captured once at beginOutputGlobsEdit so enter's save
	// stages set-globs against the same row the editor opened on, even
	// though outputRow (the Outputs tab's own cursor) is a live field this
	// mode never touches while it is open.
	globsTarget string
	// globsFuture holds the editor's own mode choice -- ModeEverythingExcept
	// or ModeOnlyThese, never any other Mode value -- separately from
	// globsChecked so that toggling it with `m` (toggleGlobsFuture) can
	// never accidentally touch which boxes are checked. See Five Things #2
	// on the task brief: switching mode must not move a single checkbox.
	globsFuture Mode
	// globsChecked is the editor's own checkbox state, one entry per name in
	// allNames() -- derived once at open time via deriveOutputGlobsChecked
	// (resolve.Selects against the row's CURRENT globs, never a hand-rolled
	// parse of Include/Exclude; see Five Things #3), then mutated only by
	// space (toggleGlobsChecked). Cleared when the editor closes, saved or
	// cancelled, so a later open always starts from a fresh derivation
	// rather than a stale map left over from editing a different row.
	globsChecked map[string]bool
	// globsRow is the editor's own cursor, scoped to allNames() the same
	// way overviewRow is scoped to problems and outputRow to
	// outputIdentityNames -- a separate field so opening the editor never
	// disturbs row/col or outputRow.
	globsRow int

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

	// Outputs is the Outputs tab's rows, one per manifest output, built by
	// the cli host from app.Manifest.Outputs and the same states map it
	// passes to Problems (see internal/cli's outputStates and
	// buildOutputRows). This package never reads app.Manifest.Outputs
	// itself: doing so here would be the second implementation of "what are
	// this project's outputs" that this task was explicitly warned against.
	Outputs []OutputRow
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
		outputs:                   p.Outputs,
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

// outputIdentityNames lists the identity every Outputs tab row is keyed by:
// one per Params-provided output, plus one per staged add-output not yet on
// disk -- mirrors allNames()'s inclusion of a freshly staged variable.
// Sorted for the same reason allNames() sorts: a freshly added output takes
// its place in the list rather than always trailing at the end.
//
// This is the identity every other Change* Output kind's Name field keys
// against, including a row with a staged rename -- see the Change kinds'
// own doc for why a rename never changes what later Changes about that row
// key by.
func (m Model) outputIdentityNames() []string {
	names := make([]string, 0, len(m.outputs)+len(m.staged))
	for _, o := range m.outputs {
		names = append(names, o.Name)
	}
	for _, c := range m.staged {
		if c.Kind == ChangeAddOutput {
			names = append(names, c.Name)
		}
	}
	sort.Strings(names)
	return names
}

// outputByName finds the Params-provided facts for identity, if it names an
// existing output rather than one only staged this session.
func (m Model) outputByName(identity string) (OutputRow, bool) {
	for _, o := range m.outputs {
		if o.Name == identity {
			return o, true
		}
	}
	return OutputRow{}, false
}

// outputDisplayName is what the Outputs tab's NAME column shows for
// identity: the pending rename target if `r` staged one, in "old -> new"
// form so the wording matches `trenvy output rename`'s own "renamed output
// %s to %s" -- a reader who knows that command recognizes this immediately
// -- otherwise just identity.
func (m Model) outputDisplayName(identity string) string {
	for _, c := range m.staged {
		if c.Kind == ChangeRenameOutput && c.Name == identity {
			return identity + " -> " + c.Value
		}
	}
	return identity
}

// outputIsDefault reports whether identity currently resolves as
// project.output: a staged default-output always wins over the on-disk fact
// for every row, since stageOutputDefault keeps at most one such Change
// staged at a time (see its own doc) -- so once ANY row has a staged
// default, every other row's on-disk "*" is stale and must stop showing.
func (m Model) outputIsDefault(identity string) bool {
	for _, c := range m.staged {
		if c.Kind == ChangeDefaultOutput {
			return c.Name == identity
		}
	}
	if o, ok := m.outputByName(identity); ok {
		return o.IsDefault
	}
	return false
}

// isOutputDeleted reports whether identity has a staged remove-output
// change, mirroring isDeleted for the Variables tab.
func (m Model) isOutputDeleted(identity string) bool {
	for _, c := range m.staged {
		if c.Kind == ChangeRemoveOutput && c.Name == identity {
			return true
		}
	}
	return false
}

// outputFacts resolves what the FORMAT/PATH/SELECTS/STATE columns show for
// identity: the Params-provided facts for an existing output, or the
// in-progress add-output draft's Format and Path for one this session added
// but has not saved. Such a row shows "all" (add-output carries no globs of
// its own -- Task 8's editor is how those are set) and "pending" for STATE,
// deliberately not one of the State* constants: there is no rendered file
// yet to have a freshness state, and borrowing a State* word would claim a
// check ran that did not.
func (m Model) outputFacts(identity string) (format, path, selects, state string) {
	if o, ok := m.outputByName(identity); ok {
		selects = o.Selects
		if staged, ok := m.stagedGlobsSelects(identity); ok {
			selects = staged
		}
		return o.Format, o.Path, selects, o.State
	}
	for _, c := range m.staged {
		if c.Kind == ChangeAddOutput && c.Name == identity {
			selects = "all"
			if staged, ok := m.stagedGlobsSelects(identity); ok {
				selects = staged
			}
			return c.Format, c.Path, selects, "pending"
		}
	}
	return "", "", "", ""
}

// stagedGlobsSelects reports the SELECTS prose a staged set-globs implies
// for identity, if one is staged -- shared by both of outputFacts' branches
// (an existing output and one only staged as add-output this session) so
// there is exactly one place that re-derives this prose from a staged edit,
// mirroring cellText's own staged-takes-precedence rule for the Variables
// tab: a membership edit must be visible in the table the instant it is
// staged, not only after w writes it to disk.
func (m Model) stagedGlobsSelects(identity string) (string, bool) {
	for _, c := range m.staged {
		if c.Kind == ChangeSetGlobs && c.Name == identity {
			return SelectsSummary(manifest.Output{Include: c.Include, Exclude: c.Exclude}), true
		}
	}
	return "", false
}

// outputNameProblem explains why name would be refused as an output's name
// or new name, or "" when it would not. Shared by add-output's name step and
// rename-output so the two paths into the same manifest key space enforce
// exactly one rule (manifest.ValidOutputName) rather than two that could
// disagree -- the class of defect this task's brief calls out by name.
// Wording matches requireValidOutputName and newOutputAddCmd's own messages
// in internal/cli/output.go verbatim, so a user does not learn a second
// vocabulary for the same refusal between the CLI and this tab.
//
// ignore excludes one identity from the "already taken" scan: rename-output
// passes the row's own current identity, so renaming a row to the name it
// already has is not flagged as a collision with itself.
func (m Model) outputNameProblem(name, ignore string) string {
	if !manifest.ValidOutputName(name) {
		return fmt.Sprintf("%q: output names may use letters, digits, - and _ only, "+
			"and may not begin with -", name)
	}
	for _, existing := range m.outputIdentityNames() {
		if existing == ignore {
			continue
		}
		if existing == name {
			return fmt.Sprintf("output %q is already defined", name)
		}
	}
	return ""
}

// outputAddNameWarning surfaces outputNameProblem live while typing the name
// step of `a`, mirroring internal/tui's addNameWarning and this package's
// own deleteBlockedReason: a refusal must be visible before enter is
// pressed, not discovered by pressing it and watching nothing happen.
func (m Model) outputAddNameWarning() string {
	if m.mode != ModeOutputAddName || m.buffer == "" {
		return ""
	}
	return m.outputNameProblem(m.buffer, "")
}

// outputRenameWarning is outputAddNameWarning's counterpart for `r`,
// excluding the row's own current identity from the collision scan (see
// outputNameProblem's ignore parameter).
func (m Model) outputRenameWarning() string {
	if m.mode != ModeOutputRename || m.buffer == "" {
		return ""
	}
	names := m.outputIdentityNames()
	if m.outputRow >= len(names) {
		return ""
	}
	return m.outputNameProblem(m.buffer, names[m.outputRow])
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
// the Outputs tab's own outputRow, or the Variables matrix's row. Routing by
// tab here, rather than in every caller, is what keeps the three cursors
// from ever being moved by the wrong tab's keypress.
func (m *Model) moveCursorDown() {
	switch m.tab {
	case TabOverview:
		m.moveOverviewRow(1)
	case TabOutputs:
		m.moveOutputRow(1)
	default:
		m.moveRow(1)
	}
}

func (m *Model) moveCursorUp() {
	switch m.tab {
	case TabOverview:
		m.moveOverviewRow(-1)
	case TabOutputs:
		m.moveOutputRow(-1)
	default:
		m.moveRow(-1)
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

// moveOverviewRow shifts the Overview tab's own cursor by delta, clamping
// rather than wrapping for the same reason moveRow does. It never touches
// row/col: see overviewRow's field comment.
func (m *Model) moveOverviewRow(delta int) {
	if len(m.problems) == 0 {
		return
	}
	m.overviewRow = clamp(m.overviewRow+delta, 0, len(m.problems)-1)
}

// moveOutputRow shifts the Outputs tab's own cursor by delta, clamping
// rather than wrapping for the same reason moveRow does. It never touches
// row/col: see outputRow's field comment. The list it clamps against
// includes staged add-output rows (see outputIdentityNames), so a freshly
// added output is a row j/k can already reach before w ever runs.
func (m *Model) moveOutputRow(delta int) {
	n := len(m.outputIdentityNames())
	if n == 0 {
		return
	}
	m.outputRow = clamp(m.outputRow+delta, 0, n-1)
}

// jumpToProblem moves the cursor to the cell the problem under the Overview
// cursor names. A "missing" problem always has a Profile and a matrix cell:
// enter switches to Variables with row/col on exactly that (name, profile).
//
// An "output" problem has no cell -- Profile is empty, since freshness is a
// property of the whole rendered file, not one variable under one profile.
// Guessing a cell for it would be a silent wrong answer, so enter instead
// switches to the Outputs tab -- the closest the model can move without
// landing somewhere the problem never named. It stops there rather than
// also moving outputRow onto the row the problem is about: jumpToProblem's
// own contract is "the Outputs tab is where this is answered", not "here is
// the exact row", and guessing the latter risks the same wrong-cell mistake
// this function exists to avoid for "missing" problems.
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

// updateConfirmPromote answers "promote anyway?" (see promoteConfirmText),
// mirroring updateConfirmSwitch and updateConfirmQuit's y/anything-else
// shape. Declining must leave staged exactly as it was, not merely shorter:
// promoteSecret's confirmed path (stagePromotion) stages several Changes at
// once -- one set-secret per profile plus the promotion itself -- and a
// decline that cleared only some of them would leave a half-promotion,
// values stored as secrets while the manifest still calls the variable
// plain, or the reverse. Resetting mode and pendingPromote unconditionally
// before the switch, and only ever calling stagePromotion on "y", is what
// keeps every other key a true no-op here.
func (m Model) updateConfirmPromote(msg tea.KeyMsg) (Model, tea.Cmd) {
	name := m.pendingPromote
	m.pendingPromote = ""
	m.mode = ModeBrowse
	if msg.String() == "y" {
		m.stagePromotion(name)
	}
	return m, nil
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
		case ModeEditValue, ModeEditSecret, ModeAddName, ModeFilter,
			ModeOutputAddName, ModeOutputAddFormat, ModeOutputAddPath, ModeOutputRename:
			return m.updateInput(msg)
		case ModeSwitchProject:
			return m.updateSwitch(msg)
		case ModeConfirmSwitch:
			return m.updateConfirmSwitch(msg)
		case ModeConfirmQuit:
			return m.updateConfirmQuit(msg)
		case ModeConfirmPromote:
			return m.updateConfirmPromote(msg)
		case ModeHelp:
			return m.updateHelp(msg)
		case ModeOutputGlobs:
			return m.updateOutputGlobs(msg)
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
		if m.tab == TabOutputs {
			m.beginOutputGlobsEdit()
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
		switch m.tab {
		case TabVariables:
			m.mode = ModeAddName
			m.buffer = ""
		case TabOutputs:
			m.beginOutputAdd()
		}
	case "d":
		switch m.tab {
		case TabVariables:
			m.toggleDelete()
		case TabOutputs:
			m.toggleOutputDelete()
		}
	case "D":
		// Capital, deliberately: d already means delete everywhere in this
		// UI, and one key meaning delete on this tab and default on that one
		// is how someone removes an output they meant to promote instead.
		// Outputs-only -- there is nothing else in this UI for it to mean.
		m.stageOutputDefault()
	case "s":
		m.promoteSecret()
	case "/":
		if m.tab == TabVariables {
			m.mode = ModeFilter
			m.buffer = ""
		}
	case "r":
		if m.tab == TabOutputs {
			m.beginOutputRename()
			return m, nil
		}
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
//
// The refusal is silent here on purpose -- see deleteBlockedReason, which
// View renders it through, rather than this function reaching for a message
// field. A key that quietly does nothing is its own defect (see that
// function's doc), so the silence must not reach the user.
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

// deleteBlockedReason explains why d would currently refuse on the row
// under the cursor, or "" when it would not -- derived purely from state
// Update already keeps (mirroring internal/tui's addNameWarning), rather
// than a new mutable field that would need its own clearing logic. It is
// deliberately true whenever the condition holds, not only right after d is
// pressed: a user resting the cursor on an already-edited row learns d will
// not work there before ever trying it, the same way addNameWarning shows
// "already exists" while still typing rather than only after enter.
//
// A silent refusal here is the same class of defect this project has
// already paid for twice: e on a secret cell was a no-op until Ruling 3
// split its meaning from s, and s itself was documented everywhere and
// implemented nowhere until Task 13 pressed it and found out. Both times
// the cost was someone concluding the tool was broken. There is no key that
// reverts a single staged edit, so the only honest advice is q, which
// discards the whole session's staged work -- inventing a narrower one here
// would be reporting a fix that does not exist.
func (m Model) deleteBlockedReason() string {
	if m.tab != TabVariables {
		return ""
	}
	names := m.visibleNames()
	if len(names) == 0 || m.row >= len(names) {
		return ""
	}
	name := names[m.row]
	if m.isDeleted(name) {
		return ""
	}
	for _, c := range m.staged {
		if (c.Kind == ChangeSetValue || c.Kind == ChangeSetSecret) && c.Name == name {
			return fmt.Sprintf(
				"d is disabled here: %s has a staged edit -- q discards everything staged; there is no key that reverts just this one",
				name)
		}
	}
	return ""
}

// promoteSecret is `s`'s entry point in ModeBrowse. A variable already secret
// stages nothing at all: there is no key that demotes a secret back to
// plain, and doing this again would either re-fetch nothing useful or
// silently no-op. Silence alone is the wart this project has removed three
// times already, so promoteBlockedReason (rendered by View) says why instead
// of leaving `s` looking broken.
//
// A variable whose value is shared across profiles (v.Value != nil) does not
// stage immediately: stagePromotion reads that same Value for every profile
// with no override of its own, so promoting it turns one manifest field into
// several keyring items that can drift apart the moment only one of them is
// edited later -- surprising enough that ModeConfirmPromote asks first (see
// promoteConfirmText). A variable with only per-profile overrides has no
// such surprise: it already has one value per profile, so promoting it only
// changes where each value lives, and it stages immediately below, exactly
// as it did before this task.
func (m *Model) promoteSecret() {
	if m.tab != TabVariables {
		return
	}
	names := m.visibleNames()
	if len(names) == 0 {
		return
	}
	name := names[m.row]
	v := m.mx.Vars[name]
	if v.Secret {
		return
	}
	if v.Value != nil {
		m.pendingPromote = name
		m.mode = ModeConfirmPromote
		return
	}
	m.stagePromotion(name)
}

// stagePromotion does `s`'s actual work: moving name's value into the
// keyring instead of merely flipping a flag. Marking the variable secret
// without moving the value would silently destroy it at commit --
// manifest.Validate forbids a secret carrying a Value or a Profiles entry,
// so commitStaged (Task 3) must strip whatever is left there, and a
// promotion that stages nothing but the flag has left nothing to strip
// FROM. That is the failure this design exists to avoid, so every path
// below stages the actual value, never an empty string standing in for it.
//
// Values are read through the same precedence Classify already mirrors --
// v.Profiles[profile] first, then v.Value -- rather than re-deriving an
// order here: a second precedence living in this function is exactly the
// class of bug Classify's own doc warns about, and it would let the
// promoted keyring entry disagree with what the cell claims a profile is
// worth (the cell says "override", the keyring silently gets the shared
// value instead).
//
// A shared Value with no per-profile override becomes one set-secret per
// profile the manifest declares (mx.Profiles), all carrying that same
// value -- not one shared keyring item, which cannot exist: a keyring item
// is addressed per profile by construction. That is the honest translation
// of "make this a secret", not a bug to work around; promoteSecret's
// ModeConfirmPromote gate is what says so before it happens.
//
// Called from two places: promoteSecret itself, for the unsurprising
// override-only case that needs no confirmation, and updateConfirmPromote,
// only on "y" -- so a decline never reaches this function and stages
// nothing.
func (m *Model) stagePromotion(name string) {
	v := m.mx.Vars[name]
	for _, profile := range m.mx.Profiles {
		value, ok := v.Profiles[profile]
		if !ok {
			if v.Value == nil {
				// No override and no shared default: this profile has
				// nothing to move, so it gets no set-secret. Staging one
				// with an empty Value would be exactly the silent
				// value-destruction this function exists to avoid.
				continue
			}
			value = *v.Value
		}
		m.stage(Change{Kind: ChangeSetSecret, Name: name, Profile: profile, Value: value})
	}
	m.stage(Change{Kind: ChangePromoteSecret, Name: name})
}

// promoteConfirmText is what ModeConfirmPromote shows: it names the
// consequence of promoting m.pendingPromote's shared value rather than
// merely asking permission. A prompt reading "promote X to a secret? [y/N]"
// would teach people to press y without reading it; this instead says what
// happens -- the one thing that is surprising here exactly once, a shared
// value becomes n separate keyring items, one per declared profile, and
// changing one afterward will not change the others -- so declining is a
// choice made because of what the prompt says, not a guess at what "sure"
// means.
func (m Model) promoteConfirmText() string {
	return fmt.Sprintf(
		"promoting %s turns its one shared value into %d separate keyring items, one per declared profile -- "+
			"changing one afterward will not change the others. promote anyway? [y/N]",
		m.pendingPromote, len(m.mx.Profiles))
}

// promoteBlockedReason explains why s would currently do nothing on the row
// under the cursor, mirroring deleteBlockedReason's derivation from state
// Update already keeps rather than a new mutable message field. It is
// non-empty exactly when that row is already a secret: s does not demote,
// so a user resting on a secret row must be told before pressing s and
// wondering whether it worked -- the fourth instance of a wart this project
// has already removed three times (see deleteBlockedReason's doc for the
// first three).
func (m Model) promoteBlockedReason() string {
	if m.tab != TabVariables {
		return ""
	}
	names := m.visibleNames()
	if len(names) == 0 || m.row >= len(names) {
		return ""
	}
	name := names[m.row]
	if !m.mx.Vars[name].Secret {
		return ""
	}
	return fmt.Sprintf(
		"s does nothing here: %s is already a secret -- there is no key that turns a secret back into a plain variable",
		name)
}

// beginOutputAdd opens the Outputs tab's `a` wizard: name, then format, then
// path (see confirmOutputAddPath), staged together as one add-output only
// once all three are known.
func (m *Model) beginOutputAdd() {
	if m.tab != TabOutputs {
		return
	}
	m.mode = ModeOutputAddName
	m.buffer = ""
	m.draftOutputName = ""
	m.draftOutputFormat = ""
}

// confirmOutputAddName handles enter on the wizard's name step. An empty
// buffer abandons the whole flow, mirroring ModeAddName's own
// empty-enter-abandons convention on the Variables tab -- there is no "back
// one step" key, so backing out of an add already in progress starts over
// with a, rather than losing only the current field. An invalid or already-
// taken name stays put without clearing the buffer: outputAddNameWarning
// (View) already explains why, live, and closing the prompt here would
// silently discard a name the user is still correcting.
func (m Model) confirmOutputAddName() (Model, tea.Cmd) {
	name := m.buffer
	if name == "" {
		m.mode = ModeBrowse
		return m, nil
	}
	if reason := m.outputNameProblem(name, ""); reason != "" {
		return m, nil
	}
	m.draftOutputName = name
	m.mode = ModeOutputAddFormat
	m.buffer = ""
	return m, nil
}

// confirmOutputAddFormat handles enter on the wizard's format step, mirroring
// confirmOutputAddName's empty-enter-abandons rule. The format itself is not
// validated against FormatDotenv/FormatJSON here: manifest.Validate already
// enforces that at commit time (Task 6), and duplicating the check here
// would be a second rule that could disagree with the one in
// internal/manifest, the exact class of defect ValidOutputName's reuse
// above is already guarding against for names.
func (m Model) confirmOutputAddFormat() (Model, tea.Cmd) {
	if m.buffer == "" {
		m.mode = ModeBrowse
		m.draftOutputName = ""
		return m, nil
	}
	m.draftOutputFormat = m.buffer
	m.mode = ModeOutputAddPath
	m.buffer = ""
	return m, nil
}

// confirmOutputAddPath handles enter on the wizard's final step, staging
// add-output with all three facts at once -- there is no output worth
// having with only some of them, so nothing stages until this point.
func (m Model) confirmOutputAddPath() (Model, tea.Cmd) {
	if m.buffer == "" {
		m.mode = ModeBrowse
		m.draftOutputName = ""
		m.draftOutputFormat = ""
		return m, nil
	}
	m.stage(Change{
		Kind:   ChangeAddOutput,
		Name:   m.draftOutputName,
		Format: m.draftOutputFormat,
		Path:   m.buffer,
	})
	m.mode = ModeBrowse
	m.buffer = ""
	m.draftOutputName = ""
	m.draftOutputFormat = ""
	return m, nil
}

// beginOutputRename opens the Outputs tab's `r` prompt: the new name for the
// row under the cursor. A staged delete for this row is cancelled first,
// unconditionally -- mirrors beginEdit's own cancel-on-edit (see its doc):
// renaming a row the user marked deleted is a clear, visible decision to
// keep it, not a silent resurrection.
func (m *Model) beginOutputRename() {
	if m.tab != TabOutputs {
		return
	}
	names := m.outputIdentityNames()
	if len(names) == 0 || m.outputRow >= len(names) {
		return
	}
	m.removeOutputDelete(names[m.outputRow])
	m.mode = ModeOutputRename
	m.buffer = ""
}

// confirmOutputRename handles enter on the rename prompt. An empty buffer
// abandons the rename, mirroring the add wizard's own convention. An
// invalid or already-taken name stays put, exactly like
// confirmOutputAddName: outputRenameWarning already explains why, live.
//
// The row cannot have moved since beginOutputRename opened this mode --
// updateInput never touches outputRow -- so re-deriving identity from
// outputRow here, rather than remembering it in a field, cannot land on the
// wrong row.
func (m Model) confirmOutputRename() (Model, tea.Cmd) {
	names := m.outputIdentityNames()
	if m.outputRow >= len(names) {
		m.mode = ModeBrowse
		m.buffer = ""
		return m, nil
	}
	identity := names[m.outputRow]
	newName := m.buffer
	if newName == "" {
		m.mode = ModeBrowse
		return m, nil
	}
	if reason := m.outputNameProblem(newName, identity); reason != "" {
		return m, nil
	}
	m.stage(Change{Kind: ChangeRenameOutput, Name: identity, Value: newName})
	m.mode = ModeBrowse
	m.buffer = ""
	return m, nil
}

// stageOutputDefault stages default-output for the row under the cursor
// (`D` -- capital, since `d` already means delete everywhere else in this
// UI; see the task brief). Any earlier default-output staged for a
// DIFFERENT row is replaced: only one output can be project.output, so
// leaving both staged would leave Task 6's commit to guess which one w
// actually applies. A staged delete for this row is cancelled first,
// mirroring beginOutputRename's own cancel-on-edit: making a row the
// default is a decision to keep it.
//
// A row that already resolves as the default -- on disk, or because this
// session already staged it -- stages nothing: defaultBlockedReason (View)
// says why, mirroring promoteBlockedReason's refusal on an already-secret
// row.
func (m *Model) stageOutputDefault() {
	if m.tab != TabOutputs {
		return
	}
	names := m.outputIdentityNames()
	if len(names) == 0 || m.outputRow >= len(names) {
		return
	}
	identity := names[m.outputRow]
	if m.outputIsDefault(identity) {
		return
	}
	m.removeOutputDelete(identity)
	kept := m.staged[:0]
	for _, c := range m.staged {
		if c.Kind == ChangeDefaultOutput {
			continue
		}
		kept = append(kept, c)
	}
	m.staged = kept
	m.staged = append(m.staged, Change{Kind: ChangeDefaultOutput, Name: identity})
}

// defaultBlockedReason explains why D would currently do nothing on the row
// under the cursor, mirroring promoteBlockedReason's derivation from state
// Update already keeps rather than a stored message. Non-empty exactly when
// outputIsDefault already holds for that row -- D only sets the default, it
// never clears one, so there is no narrower fix than "pick a different row"
// to offer here.
func (m Model) defaultBlockedReason() string {
	if m.tab != TabOutputs {
		return ""
	}
	names := m.outputIdentityNames()
	if len(names) == 0 || m.outputRow >= len(names) {
		return ""
	}
	identity := names[m.outputRow]
	if !m.outputIsDefault(identity) {
		return ""
	}
	return fmt.Sprintf(
		"D does nothing here: %s is already the default -- there is no key that clears the default without setting a new one",
		identity)
}

// toggleOutputDelete stages remove-output for the row under the cursor, or
// un-stages it if already staged -- mirrors toggleDelete's own toggle: the
// user has not saved anything yet and can still change their mind.
//
// Refuses over a staged rename-output, default-output or set-globs for the
// same row, the Outputs-tab counterpart of toggleDelete's own refusal over a
// staged set-value/set-secret (see its doc): committing a rename, a new
// default or a membership edit for a row the same batch also removes is
// contradictory, not merely redundant, and letting both stage would leave
// Task 6's commit to guess which one the user actually meant. set-globs is
// refused here rather than cancelled the way beginOutputRename and
// stageOutputDefault cancel a pending delete: the opposite order --
// beginOutputGlobsEdit opening on a row THIS session already marked deleted
// -- refuses outright (see its own doc), and letting d win when pressed
// second here while the editor's own save loses when pressed second there
// would make the outcome depend on keypress order, exactly the phase-1
// Critical this whole family of refusals exists to prevent.
func (m *Model) toggleOutputDelete() {
	if m.tab != TabOutputs {
		return
	}
	names := m.outputIdentityNames()
	if len(names) == 0 || m.outputRow >= len(names) {
		return
	}
	identity := names[m.outputRow]
	if m.isOutputDeleted(identity) {
		m.removeOutputDelete(identity)
		return
	}
	for _, c := range m.staged {
		if (c.Kind == ChangeRenameOutput || c.Kind == ChangeDefaultOutput || c.Kind == ChangeSetGlobs) && c.Name == identity {
			return
		}
	}
	m.staged = append(m.staged, Change{Kind: ChangeRemoveOutput, Name: identity})
}

// removeOutputDelete un-stages a remove-output change for identity, if one
// is staged. Shared by toggleOutputDelete's own toggle-off and
// beginOutputRename/stageOutputDefault's cancel-on-edit, mirroring
// removeDelete's own role for the Variables tab.
func (m *Model) removeOutputDelete(identity string) {
	for i, c := range m.staged {
		if c.Kind == ChangeRemoveOutput && c.Name == identity {
			m.staged = append(m.staged[:i], m.staged[i+1:]...)
			return
		}
	}
}

// outputDeleteBlockedReason mirrors deleteBlockedReason for the Outputs tab:
// derived at view time from state Update already keeps, so a row with a
// staged rename or a staged default explains why d does nothing there
// before the key is ever pressed, not only after.
func (m Model) outputDeleteBlockedReason() string {
	if m.tab != TabOutputs {
		return ""
	}
	names := m.outputIdentityNames()
	if len(names) == 0 || m.outputRow >= len(names) {
		return ""
	}
	identity := names[m.outputRow]
	if m.isOutputDeleted(identity) {
		return ""
	}
	for _, c := range m.staged {
		word := ""
		switch {
		case c.Kind == ChangeRenameOutput && c.Name == identity:
			word = "rename"
		case c.Kind == ChangeDefaultOutput && c.Name == identity:
			word = "default"
		case c.Kind == ChangeSetGlobs && c.Name == identity:
			word = "membership edit"
		}
		if word != "" {
			return fmt.Sprintf(
				"d is disabled here: %s has a staged %s -- q discards everything staged; there is no key that reverts just this one",
				identity, word)
		}
	}
	return ""
}

// beginOutputGlobsEdit opens ModeOutputGlobs on the Outputs tab row under the
// cursor (`enter`), Task 8's include/exclude editor.
//
// Refuses outright on a row this session already staged removing, rather
// than cancelling the delete the way beginOutputRename and stageOutputDefault
// cancel one -- see Five Things #4 on the task brief: editing the membership
// of an output the same batch deletes is a contradiction the commit would
// have to resolve arbitrarily, the exact shape of the phase-1 Critical where
// d then e on one row diverged by keypress order. outputGlobsBlockedReason
// (View) explains the refusal live, mirroring every other blocked-key idiom
// in this file.
func (m *Model) beginOutputGlobsEdit() {
	if m.tab != TabOutputs || m.mx == nil {
		return
	}
	names := m.outputIdentityNames()
	if len(names) == 0 || m.outputRow >= len(names) {
		return
	}
	identity := names[m.outputRow]
	if m.isOutputDeleted(identity) {
		return
	}
	include, exclude := m.currentOutputGlobs(identity)
	m.globsTarget = identity
	m.globsFuture = inferGlobsMode(include)
	m.globsChecked = m.deriveOutputGlobsChecked(identity, include, exclude)
	m.globsRow = 0
	m.mode = ModeOutputGlobs
}

// currentOutputGlobs reports the include/exclude currently in effect for
// identity: a staged set-globs takes precedence over the on-disk fact,
// mirroring currentValue's own precedence for a Variables-tab cell --
// re-opening the editor before w must show the edit just staged, not
// silently discard it back to what was on disk before this session touched
// it. An identity with neither -- a fresh add-output, never yet saved or
// edited -- reports nil, nil, meaning "all" (see outputFacts' own doc for
// why that is add-output's honest default).
func (m Model) currentOutputGlobs(identity string) (include, exclude []string) {
	for i := len(m.staged) - 1; i >= 0; i-- {
		c := m.staged[i]
		if c.Kind == ChangeSetGlobs && c.Name == identity {
			return c.Include, c.Exclude
		}
	}
	if o, ok := m.outputByName(identity); ok {
		return o.Include, o.Exclude
	}
	return nil, nil
}

// inferGlobsMode says which of the editor's two futures include, taken
// alone, currently implies: empty (or the literal ["*"] SelectsSummary
// already treats the same way) means a variable that does not exist yet is
// selected by default -- ModeEverythingExcept -- while any other include
// list means one is not -- ModeOnlyThese. This is the mode the editor OPENS
// in for an existing output; nothing here reads exclude, because exclude
// never changes which future an unseen name falls into (see WriteGlobs' own
// doc). Getting this wrong would make Five Things #1's whole point false the
// instant the editor opened: showing "everything except" over an output
// whose real Include list actually excludes every future variable would be
// lying about the current state before the user ever touches a box.
func inferGlobsMode(include []string) Mode {
	if len(include) == 0 {
		return ModeEverythingExcept
	}
	if len(include) == 1 && include[0] == "*" {
		return ModeEverythingExcept
	}
	return ModeOnlyThese
}

// deriveOutputGlobsChecked builds the editor's opening checkbox state by
// running every name in allNames() through resolve.Selects against a
// throwaway manifest holding exactly include/exclude under identity -- never
// by inspecting Include/Exclude's strings directly. See Five Things #3 on
// the task brief: an output's globs may be arbitrary patterns (PUBLIC_*, say)
// with both lists populated, and only resolve.Selects -- the same function
// render.Dotenv and render.JSON depend on -- knows what they actually mean
// for a given name today. Mirrors globs_test.go's own selectsAll helper,
// which exists for exactly this reason on the WriteGlobs side.
//
// A resolve.Selects error here would mean include or exclude already holds
// an invalid glob pattern -- data already on disk (or already staged) before
// this function ever ran, not a mistake this function is making. Rather than
// panic over a manifest fact this package did not write, the name is left
// unchecked: the editor still opens, and saving it overwrites the broken
// pattern with something WriteGlobs guarantees is well-formed.
func (m Model) deriveOutputGlobsChecked(identity string, include, exclude []string) map[string]bool {
	scratch := &manifest.Manifest{Outputs: map[string]manifest.Output{
		identity: {Include: include, Exclude: exclude},
	}}
	checked := make(map[string]bool, len(m.allNames()))
	for _, name := range m.allNames() {
		sel, err := resolve.Selects(scratch, identity, name)
		if err != nil {
			continue
		}
		checked[name] = sel
	}
	return checked
}

// moveGlobsRow shifts the editor's own cursor by delta, clamping rather than
// wrapping for the same reason moveRow does.
func (m *Model) moveGlobsRow(delta int) {
	n := len(m.allNames())
	if n == 0 {
		return
	}
	m.globsRow = clamp(m.globsRow+delta, 0, n-1)
}

// toggleGlobsChecked flips the box for the variable under the editor's
// cursor (`space`). It never touches globsFuture: checking or unchecking a
// box only ever answers "what is selected today", and mixing the two
// concerns in one function is exactly how switching mode could start moving
// boxes by accident (see Five Things #2).
func (m *Model) toggleGlobsChecked() {
	names := m.allNames()
	if len(names) == 0 || m.globsRow >= len(names) {
		return
	}
	name := names[m.globsRow]
	if m.globsChecked == nil {
		m.globsChecked = map[string]bool{}
	}
	m.globsChecked[name] = !m.globsChecked[name]
}

// toggleGlobsFuture flips which of the two futures a variable that does not
// exist yet falls into (`m`). It touches globsFuture ONLY -- see
// toggleGlobsChecked's own doc, and TestGlobsEditorSwitchingModeDoesNotMoveCheckboxes,
// which is what would catch a regression here.
func (m *Model) toggleGlobsFuture() {
	if m.globsFuture == ModeOnlyThese {
		m.globsFuture = ModeEverythingExcept
		return
	}
	m.globsFuture = ModeOnlyThese
}

// globsFutureLine states, in plain words, which future the editor's current
// mode choice implies for a variable that does not exist yet -- the one
// sentence Task 8 exists to make visible (see Five Things #1 and WriteGlobs'
// own doc). Checking boxes only ever answers what is selected TODAY; this is
// the only place that answers what happens to the NEXT variable added.
func (m Model) globsFutureLine() string {
	if m.globsFuture == ModeOnlyThese {
		return "only these: a variable added later WILL NOT appear in this file"
	}
	return "everything except: a variable added later WILL appear in this file"
}

// outputGlobsBlockedReason explains why `enter` would currently refuse to
// open the membership editor for the row under the cursor, mirroring
// outputDeleteBlockedReason's derivation from state Update already keeps
// rather than a stored message. Non-empty exactly when that row has a staged
// remove-output (see beginOutputGlobsEdit's own doc for why that refuses
// rather than cancelling the delete).
func (m Model) outputGlobsBlockedReason() string {
	if m.tab != TabOutputs || m.mode != ModeBrowse {
		return ""
	}
	names := m.outputIdentityNames()
	if len(names) == 0 || m.outputRow >= len(names) {
		return ""
	}
	identity := names[m.outputRow]
	if !m.isOutputDeleted(identity) {
		return ""
	}
	return fmt.Sprintf(
		"enter is disabled here: %s has a staged delete -- q discards everything staged; there is no key that reverts just this one",
		identity)
}

// updateOutputGlobs handles keys while the membership editor is open: j/k
// and the arrows move its own cursor, space toggles the box under it, m
// switches which future applies, enter saves (staging set-globs) and esc
// abandons the whole edit without staging anything -- mirroring every other
// input mode's own esc-abandons convention.
func (m Model) updateOutputGlobs(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = ModeBrowse
		m.globsTarget = ""
		m.globsChecked = nil
		return m, nil
	case tea.KeyEnter:
		include, exclude := WriteGlobs(m.globsFuture, m.allNames(), m.globsChecked)
		m.stage(Change{Kind: ChangeSetGlobs, Name: m.globsTarget, Include: include, Exclude: exclude})
		m.mode = ModeBrowse
		m.globsTarget = ""
		m.globsChecked = nil
		return m, nil
	case tea.KeyUp:
		m.moveGlobsRow(-1)
		return m, nil
	case tea.KeyDown:
		m.moveGlobsRow(1)
		return m, nil
	case tea.KeySpace:
		m.toggleGlobsChecked()
		return m, nil
	}
	switch msg.String() {
	case "j":
		m.moveGlobsRow(1)
	case "k":
		m.moveGlobsRow(-1)
	case "m":
		m.toggleGlobsFuture()
	}
	return m, nil
}

// updateInput handles typing in whichever text-entry mode is active,
// mirroring internal/tui's updateInput: backspace, printable runes and space
// edit the buffer; esc abandons it; enter commits it per mode. The four
// Output* modes route here too, but each confirms through its own method
// (confirmOutputAddName and friends) rather than inline in the switch below,
// since advancing to the wizard's next step -- or staying put on a refused
// name -- needs to skip this function's own trailing "mode = ModeBrowse"
// reset that every other mode falls through to.
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
		case ModeOutputAddName:
			return m.confirmOutputAddName()
		case ModeOutputAddFormat:
			return m.confirmOutputAddFormat()
		case ModeOutputAddPath:
			return m.confirmOutputAddPath()
		case ModeOutputRename:
			return m.confirmOutputRename()
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
	if m.mode == ModeConfirmPromote {
		b.WriteString(m.promoteConfirmText() + "\n")
	}

	return b.String()
}

// keyHints is the persistent, short reminder of what the keys do -- shown on
// every tab regardless of staged count, unlike statusLine's own w/q mention.
// It is deliberately incomplete (no movement keys, no tab switching, no w/q,
// already covered by statusLine): naming everything here would defeat the
// point of a one-line reminder, so `?` opens viewHelp for the rest.
//
// `p switch` is the one dropped to make room for `s promote`: every other
// key here acts on the row or cell under the cursor, while p acts on the
// whole session -- picking a different registered project -- and is the
// rarest of the seven to reach for while actually editing variables. `?`
// still names it.
//
// The Outputs tab gets its own line rather than this one: e/reveal/promote/
// filter mean nothing there, and naming a/r/d as if they still meant
// add-a-variable/reveal/delete-a-variable would be actively wrong now that
// Task 5 has given them different meanings on that tab. D exists only here.
func (m Model) keyHints() string {
	if m.tab == TabOutputs {
		return "a add · r rename · D default · d delete · enter edit membership · ? help"
	}
	return "e edit · r reveal · a add · d delete · s promote · / filter · ? help"
}

// viewHelp renders the full key list `?` opens, replacing the tab bar and
// body the same way viewSwitcher does. Every key named here is one this
// package actually implements -- checked by running each in a built binary,
// not transcribed from the spec. `s`'s entry says its promotion is one-way:
// a bare "toggle secret" would be a lie in the direction that matters, since
// there is no key that turns a secret back into a plain variable.
//
// a, r and d each mean something different depending on the tab (Task 5),
// so their rows say both meanings rather than describing only the one on
// the Variables tab -- and D, which exists only on the Outputs tab, gets its
// own row rather than being folded into d's, since the two are deliberately
// distinct keys with distinct consequences (see the task brief).
func (m Model) viewHelp() string {
	var b strings.Builder
	b.WriteString("Keys\n\n")
	rows := []struct{ key, action string }{
		{"1-4, tab, shift+tab", "switch tabs"},
		{"j/k, h/l, up/down/left/right", "move the cursor"},
		{"e", "edit the cell under the cursor (masked while typing if it is a secret)"},
		{"r", "reveal the cell under the cursor's real value (Variables tab); rename the selected output (Outputs tab)"},
		{"a", "add a variable (Variables tab); add an output -- name, then format, then path (Outputs tab)"},
		{"d", "mark the selected variable or output deleted (press again to un-mark it)"},
		{"D", "make the selected output the default (Outputs tab only)"},
		{"enter", "jump to the cell for the selected problem (Overview tab); edit the selected output's included/excluded variables (Outputs tab)"},
		{"space", "toggle the variable under the cursor, inside the membership editor"},
		{"m", "switch the membership editor's mode between everything-except and only-these, inside the editor"},
		{"s", "promote the selected variable to a secret -- one-way, there is no key that turns it back"},
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
			b.WriteString(outputFreshnessCaveat)
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

// viewOutputs renders one row per output -- a cursor column, NAME, FORMAT,
// PATH, SELECTS and STATE, the last of these reusing the exact ui.State*
// value internal/cli's outputStates already computed for the Overview's
// Problems (see Model.outputs' own doc). It never reclassifies freshness
// itself: doing so here, even by accident, is how two places computing one
// fact about outputs went out of sync into a phase-1 Critical.
//
// A state such as StateFresh renders exactly as "fresh" whether or not
// outputStates could actually verify it without reading a secret -- the
// Overview already treats that case as no problem (Fix 2), and a distinct,
// alarming label for it here would quietly re-open that decision. The one
// place this fact surfaces is the same caveat sentence the Overview's
// healthy line uses, appended once below, never per row.
//
// Rows come from outputIdentityNames, not m.outputs directly: a staged
// add-output must appear before w ever runs (mirrors the Variables tab
// showing a staged add-var), a staged rename shows as "old -> new" in the
// NAME column (outputDisplayName), a staged default moves "*" onto its row
// and off the previous one (outputIsDefault), and a staged delete marks the
// row "(deleted)" without removing it -- the user is staging an intention
// they can still abandon with q, and a row that vanished would give them
// nothing to change their mind about, and shift every row below it under a
// stationary cursor.
//
// A project with no outputs at all -- none on disk, and none staged this
// session -- is normal, not broken: exec and shell need none. This says so
// in the same words `trenvy output list` already uses for the CLI
// equivalent, but names `a` first: that key is now wired (Task 5), and it
// is the more direct fix for whoever is looking at this exact screen. That
// placeholder only shows in ModeBrowse: mid-wizard (see beginOutputAdd),
// there IS something to render -- the prompt for whichever step is active
// -- even before the first output exists to put a row on screen for.
func (m Model) viewOutputs() string {
	if m.mode == ModeOutputGlobs {
		// Full replacement of this tab's body, not the table plus a prompt
		// appended below the way the add/rename wizards render: "every
		// variable" can be a long list, and stacking it under the full
		// outputs table would bury the one thing this mode is for.
		return m.viewOutputGlobsEditor()
	}

	names := m.outputIdentityNames()
	if len(names) == 0 && m.mode == ModeBrowse {
		return "no outputs; exec and shell need none.\n" +
			"  press a to add one -- name, then format, then path -- or from the command line: " +
			"trenvy output add <name> --format dotenv --path <path>\n"
	}

	var b strings.Builder
	if len(names) > 0 {
		w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "\tNAME\tFORMAT\tPATH\tSELECTS\tSTATE")
		for i, identity := range names {
			cursor := " "
			if i == m.outputRow {
				cursor = ">"
			}
			format, path, selects, state := m.outputFacts(identity)
			name := m.outputDisplayName(identity)
			if m.outputIsDefault(identity) {
				// Mirrors `trenvy output list`'s own marker for
				// project.Output, so a user does not learn a second
				// convention for the same fact between the CLI and this tab.
				name += " *"
			}
			if m.isOutputDeleted(identity) {
				name += " (deleted)"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", cursor, name, format, path, selects, state)
		}
		_ = w.Flush()
	}

	if m.outputFreshnessUnverified {
		b.WriteString("\n" + outputFreshnessCaveat)
	}

	switch m.mode {
	case ModeOutputAddName:
		fmt.Fprintf(&b, "\n new output name: %s_", m.buffer)
		if reason := m.outputAddNameWarning(); reason != "" {
			fmt.Fprintf(&b, "  (%s)", reason)
		}
		b.WriteString("\n")
	case ModeOutputAddFormat:
		fmt.Fprintf(&b, "\n format for %s (dotenv or json): %s_\n", m.draftOutputName, m.buffer)
	case ModeOutputAddPath:
		fmt.Fprintf(&b, "\n path for %s: %s_\n", m.draftOutputName, m.buffer)
	case ModeOutputRename:
		fmt.Fprintf(&b, "\n new name: %s_", m.buffer)
		if reason := m.outputRenameWarning(); reason != "" {
			fmt.Fprintf(&b, "  (%s)", reason)
		}
		b.WriteString("\n")
	case ModeBrowse:
		if reason := m.outputDeleteBlockedReason(); reason != "" {
			fmt.Fprintf(&b, "\n%s\n", reason)
		}
		if reason := m.defaultBlockedReason(); reason != "" {
			fmt.Fprintf(&b, "\n%s\n", reason)
		}
		if reason := m.outputGlobsBlockedReason(); reason != "" {
			fmt.Fprintf(&b, "\n%s\n", reason)
		}
	}

	return b.String()
}

// viewOutputGlobsEditor renders Task 8's include/exclude editor: one line
// per variable allNames() knows about, a checkbox and a cursor, then the one
// sentence Five Things #1 exists for -- which future the current mode
// implies for a variable that does not exist yet -- and finally the keys
// that act here. It replaces viewOutputs' table entirely while open (see
// that function's own doc for why), rather than rendering alongside it.
func (m Model) viewOutputGlobsEditor() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Editing membership for %s\n\n", m.globsTarget)

	names := m.allNames()
	if len(names) == 0 {
		b.WriteString("no variables defined yet -- add one on the Variables tab first.\n")
	}
	for i, name := range names {
		cursor := " "
		if i == m.globsRow {
			cursor = ">"
		}
		box := "[ ]"
		if m.globsChecked[name] {
			box = "[x]"
		}
		fmt.Fprintf(&b, "%s %s %s\n", cursor, box, name)
	}

	fmt.Fprintf(&b, "\n%s\n", m.globsFutureLine())
	b.WriteString("\nspace toggle · m switch mode · enter save · esc cancel\n")
	return b.String()
}

// KeyRune returns the message for a single rune key press, so callers can
// drive a model without depending on bubbletea's key types. Mirrors
// internal/tui.KeyW.
func KeyRune(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// KeyType returns the message for a non-rune key, e.g. tea.KeyTab.
func KeyType(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
