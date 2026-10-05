// Package manage is trenvy's interactive interface (spec 2026-09-30, TUI
// redesign): one shell, list-and-detail screens, staged edits reviewed
// before they are saved, and a save that keeps the session open.
//
// Update performs no I/O. Anything touching the keyring or the disk is a
// Deps function, called inside a tea.Cmd, so a test drives the model with
// key messages and feeds each Cmd's message back itself.
package manage

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/session"
	"github.com/cdescobedo/trenvy/internal/style"
)

// Snapshot is what the interface shows, loaded from disk at the start and
// after every save.
type Snapshot struct {
	Project, Profile, Source        string
	ManifestPath, Worktree, Backend string
	Matrix                          *session.Matrix
	Problems                        []session.Problem
	OutputsFor                      map[string][]string // variable -> outputs selecting it
	OutputPaths                     map[string]string   // output -> its path
	OutputStates                    map[string]string   // output -> a session.State* word (quick mode)
	DefaultOutput                   string              // project.output on disk
	Projects                        []string            // every registered project id
}

// CommitResult is what a successful save reports.
type CommitResult struct {
	Saved       int
	LeftInPlace []string
}

// ErrNothingWritten marks a Commit error for a batch refused before
// anything was written -- one the manifest will not accept. Retrying it
// unchanged cannot succeed, so the review says so instead of offering a
// retry. The error's own text is the reason; the review supplies the words
// "nothing was written".
var ErrNothingWritten = errors.New("nothing was written")

// Deps is every piece of I/O the interface asks for.
type Deps struct {
	Reveal func(name, profile string) (string, error)
	Commit func([]session.Change) (CommitResult, error)
	Reload func() (Snapshot, error)
	// Ignored reports whether git ignores path in this worktree, for the
	// add-output warning (spec §4).
	Ignored func(path string) (bool, error)
}

type message struct {
	text  string
	isErr bool
}

// revealedMsg carries gen, the reload generation it was requested in; one
// that arrives after a reload is dropped.
type revealedMsg struct {
	name, profile, value string
	gen                  int
}
type revealFailedMsg struct{ err error }
type committedMsg struct {
	res CommitResult
	err error
}
type reloadedMsg struct {
	snap Snapshot
	err  error
}

// ignoredMsg is git's answer for the path an add-output names.
type ignoredMsg struct {
	path    string
	ignored bool
	err     error
}

// Model is the interface's whole state.
type Model struct {
	th   style.Theme
	deps Deps
	snap Snapshot

	width, height int

	staged   session.Staged
	scr      screen
	vars     variables
	outs     outputs
	overlays []overlay
	msg      message

	// stale is set when a reload fails: what is on screen may not match
	// disk, so staging and saving are refused until a reload succeeds
	// (spec Risks, "Save-and-stay").
	stale  bool
	saving bool
	// reloading holds input from a finished commit until its reload lands,
	// so nothing is staged or committed against the pre-commit snapshot.
	reloading bool
	// gen counts reloads; a reveal from an earlier one is stale.
	gen int

	quit     bool
	switchTo string
}

func New(snap Snapshot, deps Deps, th style.Theme) Model {
	return Model{th: th, deps: deps, snap: snap, vars: newVariables(), outs: newOutputs()}
}

func (m Model) Init() tea.Cmd { return nil }

// Staged is every change staged, for tests and the CLI.
func (m Model) Staged() []session.Change { return m.staged.All() }

// SwitchTo names the project the user chose to switch to, if any.
func (m Model) SwitchTo() (string, bool) { return m.switchTo, m.switchTo != "" }

// Quitting reports that the session has ended.
func (m Model) Quitting() bool { return m.quit }

// FooterKeys lists the keys the footer shows now, for tests.
func (m Model) FooterKeys() []string {
	var out []string
	for _, h := range m.footerHints() {
		out = append(out, h.key)
	}
	return out
}

func (m *Model) push(o overlay) {
	m.overlays = append(m.overlays[:len(m.overlays):len(m.overlays)], o)
}

func (m *Model) top() overlay {
	if len(m.overlays) == 0 {
		return nil
	}
	return m.overlays[len(m.overlays)-1]
}

func (m *Model) removeOverlay(i int) {
	m.overlays = append(m.overlays[:i:i], m.overlays[i+1:]...)
}

// canStage refuses staging while the snapshot may be out of date.
func (m *Model) canStage() bool {
	if m.stale {
		m.msg = message{"can't edit: the last reload failed -- press . then Reload from disk", true}
		return false
	}
	return true
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case revealedMsg:
		if name, ok := m.selected(); ok && name == msg.name && msg.gen == m.gen {
			m.vars.revealed[cellKey{msg.name, msg.profile}] = msg.value
		}
		return m, nil
	case revealFailedMsg:
		m.msg = message{"could not reveal: " + msg.err.Error(), true}
		return m, nil
	case committedMsg:
		return m.afterCommit(msg)
	case reloadedMsg:
		return m.afterReload(msg)
	case ignoredMsg:
		// Only the add-output panel still on its path step, with the same
		// path, takes the answer; one for a path since edited is stale.
		if a, ok := m.top().(*addOutputOverlay); ok && a.step == 2 && strings.TrimSpace(a.f.value()) == msg.path {
			if a.afterIgnored(&m, msg) {
				m.removeOverlay(len(m.overlays) - 1)
			}
		}
		return m, nil
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.Type == tea.KeyCtrlC {
		m.quit = true
		return m, tea.Quit
	}
	if m.saving || m.reloading {
		return m, nil
	}
	m.msg = message{}
	if n := len(m.overlays); n > 0 {
		if k.Type == tea.KeyEsc {
			m.removeOverlay(n - 1)
			return m, nil
		}
		done, cmd := m.overlays[n-1].update(&m, k)
		if done {
			m.removeOverlay(n - 1)
		}
		return m, cmd
	}
	switch {
	case keyIs(k, "q"):
		if m.staged.Len() > 0 {
			m.push(m.quitConfirm())
			return m, nil
		}
		m.quit = true
		return m, tea.Quit
	case keyIs(k, "?"):
		m.push(&helpOverlay{})
		return m, nil
	case keyIs(k, "w"):
		return m.openReview()
	}
	if m.switchScreen(k) {
		return m, nil
	}
	return m.screenKey(k)
}

// quitConfirm is q with unsaved changes (spec §3).
func (m *Model) quitConfirm() overlay {
	return &confirmOverlay{
		title: "Unsaved changes",
		text:  plural(m.staged.Len(), "unsaved change") + " -- nothing is written if you quit now.",
		choices: []choice{
			{"w", "review", func(m *Model) tea.Cmd { _, cmd := m.openReviewPtr(); return cmd }},
			{"q", "discard and quit", func(m *Model) tea.Cmd { m.quit = true; return tea.Quit }},
		},
	}
}

func (m Model) size() (int, int) {
	w, h := m.width, m.height
	if w == 0 {
		w = 100
	}
	if h == 0 {
		h = 30
	}
	return w, h
}

func (m Model) View() string {
	if m.quit {
		return ""
	}
	w, h := m.size()
	if w < minWidth || h < minHeight {
		return fmt.Sprintf("trenvy needs at least %d×%d -- this terminal is %d×%d", minWidth, minHeight, w, h)
	}
	msg := m.messageLines(w)
	bodyH := m.bodyHeight()
	body := m.screenView(w, bodyH)
	if top := m.top(); top != nil {
		// An overlay that draws nothing (the filter) leaves the body visible.
		if o := top.view(&m, w, bodyH); o != "" {
			body = lipgloss.Place(w, bodyH, lipgloss.Center, lipgloss.Center, o)
		}
	}
	return strings.Join(append(append([]string{m.viewHeader(w), m.viewTabs(w), body}, msg...), m.viewFooter(w)), "\n")
}

// bodyHeight is the rows left for the body under the current message,
// which may take more than the one line chromeLines counts.
func (m Model) bodyHeight() int {
	w, h := m.size()
	return h - chromeLines + 1 - len(m.messageLines(w))
}

// viewHeader draws the header. The right side -- the problem pill, the
// unsaved count and its key -- is what needs acting on, so when the line is
// too narrow the left side gives way: the project is cut first, then the
// profile, then the profile's source.
func (m Model) viewHeader(w int) string {
	t := m.th
	on := t.OnHeader
	var right []string
	if n := len(m.snap.Problems); n > 0 {
		right = append(right, t.PillDanger.Render("! "+plural(n, "problem")))
	}
	if n := m.staged.Len(); n > 0 {
		right = append(right, t.PillStaged.Render(fmt.Sprintf("%d unsaved", n))+
			on(t.Muted).Render(" ")+on(t.Key).Render("w")+on(t.KeyDesc).Render(" review"))
	}
	r := strings.Join(right, on(t.Muted).Render("  ")) + on(t.Muted).Render(" ")
	left := m.headerLeft(w - lipgloss.Width(r) - 1)
	gap := max(1, w-lipgloss.Width(left)-lipgloss.Width(r))
	line := left + on(t.Text).Render(strings.Repeat(" ", gap)) + r
	return lipgloss.NewStyle().MaxWidth(w).Render(line)
}

// headerLeft is " project · profile (source)" in at most avail cells.
func (m Model) headerLeft(avail int) string {
	if avail <= 0 {
		// MaxWidth(0) would mean no limit at all.
		return ""
	}
	t := m.th
	on := t.OnHeader
	proj, prof, src := display(m.snap.Project), display(m.snap.Profile), " ("+m.snap.Source+")"
	over := func() int { return lipgloss.Width(" "+proj+" · "+prof+src) - avail }
	if n := over(); n > 0 {
		proj = cutTo(proj, max(1, lipgloss.Width(proj)-n))
	}
	if n := over(); n > 0 {
		prof = cutTo(prof, max(1, lipgloss.Width(prof)-n))
	}
	if over() > 0 {
		src = ""
	}
	left := on(t.Title).Render(" "+proj) + on(t.Muted).Render(" · ") + on(t.Title).Render(prof) + on(t.Muted).Render(src)
	if over() > 0 {
		left = lipgloss.NewStyle().MaxWidth(avail).Render(left)
	}
	return left
}

// messageLines is the message, wrapped rather than cut so its end -- often
// the way to undo or recover -- survives. Past maxMessageLines the last line
// is cut, ending in ….
func (m Model) messageLines(w int) []string {
	st := m.th.Text
	if m.msg.isErr {
		st = m.th.Danger
	}
	lines := wrap(display(m.msg.text), w-2)
	if len(lines) > maxMessageLines {
		rest := strings.Join(lines[maxMessageLines-1:], " ")
		lines = append(lines[:maxMessageLines-1], cutTo(rest, w-2))
	}
	for i, l := range lines {
		lines[i] = " " + st.Render(l)
	}
	return lines
}

func (m Model) footerHints() []hint {
	if top := m.top(); top != nil {
		return top.hints(&m)
	}
	return footer(m.screenHints())
}

func (m Model) viewFooter(w int) string {
	var parts []string
	for _, h := range m.footerHints() {
		parts = append(parts, m.th.Key.Render(h.key)+" "+m.th.KeyDesc.Render(h.desc))
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(" " + strings.Join(parts, "   "))
}
