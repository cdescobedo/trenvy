package manage

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
)

// outputNameProblem is why name cannot name an output, or "". ignore is
// the identity being renamed, which may keep its own name. A name is taken
// if any output holds it now -- even one being renamed away, because
// commitStaged refuses renames onto held names (swaps included) -- or a
// staged rename will give it to another output.
func (m Model) outputNameProblem(name, ignore string) string {
	if name == "" {
		return "an output needs a name"
	}
	if !manifest.ValidOutputName(name) {
		return fmt.Sprintf("%q: output names may use letters, digits, - and _ only, and may not begin with -", name)
	}
	for _, id := range m.outputIDs() {
		if id == ignore {
			continue
		}
		if id == name || m.outView(id).RenamedTo == name {
			return fmt.Sprintf("output %q already exists", name)
		}
	}
	return ""
}

// outputPathTaken is why path cannot be an output's path, or "": another
// output, not marked for removal, renders there after staged changes.
// `trenvy output add` refuses the same thing, because rendering one would
// overwrite the other's file. ignore is the output whose path is being
// edited. Paths compare cleaned, as commitStaged's check does.
func (m Model) outputPathTaken(path, ignore string) string {
	for _, id := range m.outputIDs() {
		v := m.outView(id)
		if id == ignore || v.Removed {
			continue
		}
		if filepath.Clean(v.Output.Path) == filepath.Clean(path) {
			return fmt.Sprintf("output %q already renders to %s; two outputs on one path overwrite each other", id, path)
		}
	}
	return ""
}

// pathEntered is enter on an output's path field. A path with a problem
// is refused, with the problem on the message line. A path git ignores, or
// one already warned about (warned), is accepted. Otherwise git is asked,
// and its answer comes back as an ignoredMsg for the overlay's afterIgnored.
func (m *Model) pathEntered(path, ignore, warned string) (ok bool, ask tea.Cmd) {
	if err := manifest.CheckOutputPath(path); err != nil || path == "" {
		m.msg = message{fmt.Sprintf("%q: an output's path must be relative to the worktree, inside it", path), true}
		return false, nil
	}
	if p := m.outputPathTaken(path, ignore); p != "" {
		m.msg = message{p, true}
		return false, nil
	}
	if warned == path {
		return true, nil
	}
	ignored := m.deps.Ignored
	return false, func() tea.Msg {
		ok, err := ignored(path)
		return ignoredMsg{path, ok, err}
	}
}

// pathLiveProblem is what is wrong with a path field's value while typing,
// or "". An empty field shows nothing until enter.
func (m Model) pathLiveProblem(path, ignore string) string {
	if path == "" {
		return ""
	}
	if err := manifest.CheckOutputPath(path); err != nil {
		return err.Error()
	}
	return m.outputPathTaken(path, ignore)
}

// notIgnoredWarning words git's answer for a path it does not ignore; again
// is what a second enter does ("add anyway").
func notIgnoredWarning(msg ignoredMsg, again string) message {
	why := "is not ignored by git -- render refuses to write it without --allow-tracked"
	// guard.GitIgnored never returns an error: outside a repository, or
	// with no git, it reports the path ignored (see Deps.Ignored). This
	// branch keeps the Deps contract honest for an Ignored that can fail.
	if msg.err != nil {
		why = "could not be checked against .gitignore (" + errText(msg.err) + ")"
	}
	return message{msg.path + " " + why + " -- enter again to " + again, true}
}

// pathAsker is an overlay with an output path field waiting on git.
type pathAsker interface {
	// askedPath is the path in the field, or "" when the overlay is not on
	// a path step: git's answer for any other path is stale.
	askedPath() string
	// afterIgnored takes git's answer, and reports whether the overlay is
	// done.
	afterIgnored(m *Model, msg ignoredMsg) bool
}

// addOutputOverlay is a on Outputs: name, then format, then path -- staged
// together as one addition (spec §4).
type addOutputOverlay struct {
	step   int // 0 name, 1 format, 2 path
	name   string
	format string
	f      field
	warned string // the path the gitignore warning was shown for
}

func (m *Model) beginAddOutput() {
	if m.canStage() {
		m.push(&addOutputOverlay{f: newField("", false)})
	}
}

func (a *addOutputOverlay) update(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	switch a.step {
	case 0:
		if k.Type != tea.KeyEnter {
			a.f = a.f.update(k)
			return false, nil
		}
		name := strings.TrimSpace(a.f.value())
		if p := m.outputNameProblem(name, ""); p != "" {
			m.msg = message{p, true}
			return false, nil
		}
		a.name, a.step = name, 1
	case 1:
		switch {
		case keyIs(k, "d"):
			// .env is where import and most tools look, but only one output
			// can render there.
			path := ".env"
			if m.outputPathTaken(path, "") != "" {
				path = a.name + ".env"
			}
			a.format, a.step, a.f = manifest.FormatDotenv, 2, newField(path, false)
		case keyIs(k, "j"):
			a.format, a.step, a.f = manifest.FormatJSON, 2, newField(a.name+".json", false)
		}
	case 2:
		if k.Type != tea.KeyEnter {
			a.f = a.f.update(k)
			return false, nil
		}
		path := strings.TrimSpace(a.f.value())
		ok, ask := m.pathEntered(path, "", a.warned)
		if ok {
			a.stage(m, path)
		}
		return ok, ask
	}
	return false, nil
}

func (a *addOutputOverlay) askedPath() string {
	if a.step != 2 {
		return ""
	}
	return strings.TrimSpace(a.f.value())
}

// afterIgnored finishes the path step once git has answered.
func (a *addOutputOverlay) afterIgnored(m *Model, msg ignoredMsg) bool {
	if msg.err == nil && msg.ignored {
		a.stage(m, msg.path)
		return true
	}
	a.warned = msg.path
	m.msg = notIgnoredWarning(msg, "add anyway")
	return false
}

func (a *addOutputOverlay) stage(m *Model, path string) {
	m.staged.Add(session.Change{Kind: session.ChangeAddOutput, Name: a.name, Format: a.format, Path: path})
	m.selectOutput(a.name)
	m.msg = message{"output " + a.name + " added -- u to undo", false}
}

// problem is what is wrong with the field's value right now, or "". The
// panel shows it while typing (spec §4, "validated live"); enter refuses
// the same values. An empty field shows nothing until enter.
func (a *addOutputOverlay) problem(m *Model) string {
	v := strings.TrimSpace(a.f.value())
	if v == "" {
		return ""
	}
	switch a.step {
	case 0:
		return m.outputNameProblem(v, "")
	case 2:
		return m.pathLiveProblem(v, "")
	}
	return ""
}

// fieldBody is the field and, under it, the current problem.
func (a *addOutputOverlay) fieldBody(m *Model, w int) []string {
	return fieldWithProblem(m, a.f, a.problem(m), w)
}

// fieldWithProblem is a panel body: f and, under it, problem if any.
func fieldWithProblem(m *Model, f field, problem string, w int) []string {
	body := []string{" " + f.view(m.th, min(w, panelMax)-4)}
	if problem != "" {
		for _, l := range wrap(display(problem), min(w, panelMax)-5) {
			body = append(body, " "+m.th.Danger.Render(l))
		}
	}
	return body
}

func (a *addOutputOverlay) view(m *Model, w, h int) string {
	switch a.step {
	case 0:
		return m.panel("Add output -- name", a.fieldBody(m, w), w, h)
	case 1:
		return m.panel("Add "+a.name+" -- format", []string{
			" " + m.th.Key.Render("d") + " " + m.th.Text.Render("dotenv: KEY=value lines"),
			" " + m.th.Key.Render("j") + " " + m.th.Text.Render("json: one object"),
		}, w, h)
	}
	return m.panel("Add "+a.name+" -- path in this worktree", a.fieldBody(m, w), w, h)
}

func (a *addOutputOverlay) hints(*Model) []hint {
	switch {
	case a.step == 1:
		return []hint{{"d", "dotenv"}, {"j", "json"}, {"esc", "cancel"}}
	case a.step == 2 && a.warned != "" && strings.TrimSpace(a.f.value()) == a.warned:
		return []hint{{"enter", "add anyway"}, {"esc", "cancel"}}
	case a.step == 2:
		return []hint{{"enter", "add"}, {"esc", "cancel"}}
	}
	return []hint{{"enter", "next"}, {"esc", "cancel"}}
}

// toggleRemoveOutput is d in the Outputs list.
func (m *Model) toggleRemoveOutput(id string) {
	if !m.canStage() {
		return
	}
	if m.staged.Has(session.ChangeRemoveOutput, id) {
		m.staged.DropKind(session.ChangeRemoveOutput, id)
		m.msg = message{id + " restored", false}
		return
	}
	added := m.staged.Has(session.ChangeAddOutput, id)
	others := len(m.staged.OnOutput(id))
	m.staged.Add(session.Change{Kind: session.ChangeRemoveOutput, Name: id})
	switch {
	case added:
		m.msg = message{id + " removed -- it was only added this session", false}
		m.moveOutput(0)
	case others > 0:
		m.msg = message{fmt.Sprintf("%s marked for removal, dropped %s on it -- u restores the output (the dropped changes are gone)", id, plural(others, "staged change")), false}
	default:
		m.msg = message{id + " marked for removal -- u to undo", false}
	}
}

func (m *Model) undoOutput(id string) {
	if n := len(m.staged.DropOnOutput(id)); n > 0 {
		m.msg = message{"undid " + plural(n, "change") + " on " + id, false}
		m.moveOutput(0)
		return
	}
	m.msg = message{"nothing staged on " + id, false}
}

// undoOutputRow is u on a detail row. A variable row undoes the whole
// membership edit -- the grid is one staged change -- and says so.
func (m *Model) undoOutputRow(id, rowKey string) {
	kind := map[string]string{"format": session.ChangeSetOutputFormat, "path": session.ChangeSetOutputPath}[rowKey]
	if kind == "" {
		kind = session.ChangeSetGlobs
	}
	if len(m.staged.DropKind(kind, id)) == 0 {
		m.msg = message{"nothing staged on this row", false}
		return
	}
	if kind == session.ChangeSetGlobs {
		m.msg = message{"undid " + id + "'s membership edit", false}
		return
	}
	m.msg = message{"undid " + id + "'s " + rowKey, false}
}

// fieldOutcome is what a format or path edit did, so the message never
// claims a change that is not staged.
type fieldOutcome int

const (
	fieldStaged fieldOutcome = iota // a row-level change, undone by u on the row
	fieldOnDisk                     // back to the disk value: the change is dropped
	fieldFolded                     // folded into the output's add-output
)

// stageOutputField stages a format or path edit, dropping it instead when
// the value is what is already on disk.
func (m *Model) stageOutputField(id, kind, value, onDisk string) fieldOutcome {
	if !m.staged.Has(session.ChangeAddOutput, id) && value == onDisk {
		m.staged.DropKind(kind, id)
		return fieldOnDisk
	}
	if m.staged.Has(session.ChangeAddOutput, id) {
		// An addition carries its own format and path: re-stage it.
		v := m.outView(id)
		f, p := v.Output.Format, v.Output.Path
		if kind == session.ChangeSetOutputFormat {
			f = value
		} else {
			p = value
		}
		m.staged.Add(session.Change{Kind: session.ChangeAddOutput, Name: id, Format: f, Path: p})
		return fieldFolded
	}
	m.staged.Add(session.Change{Kind: kind, Name: id, Value: value})
	return fieldStaged
}

// fieldStagedMsg is the message after a format or path edit.
func fieldStagedMsg(id, field string, o fieldOutcome) message {
	switch o {
	case fieldOnDisk:
		return message{id + " " + field + " is back to what is on disk", false}
	case fieldFolded:
		return message{id + "'s " + field + " changed -- it is part of adding " + id + "; u on " + id + " in the list undoes the whole addition", false}
	}
	return message{id + " " + field + " staged -- u to undo", false}
}

// beginOutputEdit is enter on a detail row.
func (m Model) beginOutputEdit(id, rowKey string) (tea.Model, tea.Cmd) {
	if !m.canStage() {
		return m, nil
	}
	v := m.outView(id)
	if v.Removed {
		m.msg = message{id + " is marked for removal -- u to restore it first", true}
		return m, nil
	}
	base := m.snap.Matrix.Outputs[id]
	switch {
	case rowKey == "format":
		pick := func(f string) func(*Model) tea.Cmd {
			return func(m *Model) tea.Cmd {
				m.msg = fieldStagedMsg(id, "format", m.stageOutputField(id, session.ChangeSetOutputFormat, f, base.Format))
				return nil
			}
		}
		m.push(&confirmOverlay{
			title:   "Format for " + id,
			text:    "currently " + v.Output.Format,
			choices: []choice{{"d", "dotenv", pick(manifest.FormatDotenv)}, {"j", "json", pick(manifest.FormatJSON)}},
		})
	case rowKey == "path":
		m.push(&pathOverlay{id: id, onDisk: base.Path, f: newField(v.Output.Path, false)})
	case strings.HasPrefix(rowKey, "var:"):
		m.toggleMember(id, strings.TrimPrefix(rowKey, "var:"))
	}
	return m, nil
}

// pathOverlay is enter on an output's path row: the add panel's path step
// for an output that exists, with the same live problems and the same
// warning for a path git does not ignore.
type pathOverlay struct {
	id     string
	onDisk string // the path in the manifest; going back to it drops the edit
	f      field
	warned string // the path the gitignore warning was shown for
}

func (p *pathOverlay) update(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	if k.Type != tea.KeyEnter {
		p.f = p.f.update(k)
		return false, nil
	}
	path := strings.TrimSpace(p.f.value())
	if path == p.onDisk && !m.staged.Has(session.ChangeAddOutput, p.id) {
		// Back to what is on disk: nothing new to check.
		p.stage(m, path)
		return true, nil
	}
	ok, ask := m.pathEntered(path, p.id, p.warned)
	if ok {
		p.stage(m, path)
	}
	return ok, ask
}

func (p *pathOverlay) askedPath() string { return strings.TrimSpace(p.f.value()) }

func (p *pathOverlay) afterIgnored(m *Model, msg ignoredMsg) bool {
	if msg.err == nil && msg.ignored {
		p.stage(m, msg.path)
		return true
	}
	p.warned = msg.path
	m.msg = notIgnoredWarning(msg, "use it anyway")
	return false
}

func (p *pathOverlay) stage(m *Model, path string) {
	m.msg = fieldStagedMsg(p.id, "path", m.stageOutputField(p.id, session.ChangeSetOutputPath, path, p.onDisk))
}

func (p *pathOverlay) view(m *Model, w, h int) string {
	problem := m.pathLiveProblem(strings.TrimSpace(p.f.value()), p.id)
	return m.panel("Path for "+p.id+" in this worktree", fieldWithProblem(m, p.f, problem, w), w, h)
}

func (p *pathOverlay) hints(*Model) []hint {
	if p.warned != "" && strings.TrimSpace(p.f.value()) == p.warned {
		return []hint{{"enter", "use anyway"}, {"esc", "cancel"}}
	}
	return []hint{{"enter", "stage"}, {"ctrl+u", "clear"}, {"esc", "cancel"}}
}

// stageGlobs stages the globs for checked under mode, or drops the
// membership edit when the result selects what disk already selects in
// the same mode.
func (m *Model) stageGlobs(id string, mode session.GlobMode, checked map[string]bool) {
	names := m.allNames()
	inc, exc := session.WriteGlobs(mode, names, checked)
	base := m.snap.Matrix.Outputs[id]
	same := session.InferGlobMode(base.Include) == mode &&
		equalSelection(session.Checked(base.Include, base.Exclude, names), session.Checked(inc, exc, names))
	if same && !m.staged.Has(session.ChangeAddOutput, id) {
		m.staged.DropKind(session.ChangeSetGlobs, id)
		return
	}
	m.staged.Add(session.Change{Kind: session.ChangeSetGlobs, Name: id, Include: inc, Exclude: exc})
}

func equalSelection(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// toggleMember is space (or enter) on a variable row of the grid.
func (m *Model) toggleMember(id, name string) {
	if !m.canStage() {
		return
	}
	v := m.outView(id)
	if v.Removed {
		m.msg = message{id + " is marked for removal -- u to restore it first", true}
		return
	}
	checked := session.Checked(v.Output.Include, v.Output.Exclude, m.allNames())
	checked[name] = !checked[name]
	m.stageGlobs(id, session.InferGlobMode(v.Output.Include), checked)
}

// switchGlobMode flips which future a new variable falls into. Today's
// boxes stay exactly as they are (spec §4).
func (m *Model) switchGlobMode(id string) {
	if !m.canStage() {
		return
	}
	v := m.outView(id)
	checked := session.Checked(v.Output.Include, v.Output.Exclude, m.allNames())
	mode := session.GlobsOnlyThese
	if session.InferGlobMode(v.Output.Include) == session.GlobsOnlyThese {
		mode = session.GlobsEverythingExcept
	}
	m.stageGlobs(id, mode, checked)
}

// outMenu is . on the Outputs screen. Render now, Remove rendered file and
// Check now sit between "New variables" and "Project info".
func (m *Model) outMenu() overlay {
	var items []menuItem
	if id, ok := m.selectedOutput(); ok {
		v := m.outView(id)
		def := menuItem{label: "Make default", run: func(m *Model) tea.Cmd {
			if !m.canStage() {
				return nil
			}
			// The item is disabled while id is the default after staged
			// changes, so when id is the disk default another default is
			// staged: dropping it is how the disk default comes back.
			staged, _ := m.staged.DefaultOutput()
			if id == m.snap.DefaultOutput {
				m.staged.DropKind(session.ChangeDefaultOutput, staged)
				m.msg = message{id + " is the default again, as on disk", false}
				return nil
			}
			m.staged.Add(session.Change{Kind: session.ChangeDefaultOutput, Name: id})
			m.msg = message{id + " will be the default -- u on it to undo", false}
			return nil
		}}
		switch {
		case v.Default:
			def.disabled = id + " is already the default -- make another output the default instead"
		case v.Removed:
			def.disabled = id + " is marked for removal"
		}
		rename := menuItem{label: "Rename", run: func(m *Model) tea.Cmd {
			if !m.canStage() {
				return nil
			}
			start := id
			if v.RenamedTo != "" {
				start = v.RenamedTo
			}
			m.push(&editOverlay{title: "Rename " + id, f: newField(start, false), save: func(m *Model, name string) bool {
				name = strings.TrimSpace(name)
				if name == id {
					m.staged.DropKind(session.ChangeRenameOutput, id)
					m.msg = message{"rename of " + id + " undone", false}
					return true
				}
				if p := m.outputNameProblem(name, id); p != "" {
					m.msg = message{p, true}
					return false
				}
				m.staged.Add(session.Change{Kind: session.ChangeRenameOutput, Name: id, Value: name})
				m.msg = message{id + " will be renamed " + name + " -- u to undo", false}
				return true
			}})
			return nil
		}}
		if v.Removed {
			rename.disabled = id + " is marked for removal"
		}
		future := "New variables: not included"
		if session.InferGlobMode(v.Output.Include) == session.GlobsOnlyThese {
			future = "New variables: included automatically"
		}
		mode := menuItem{label: future, run: func(m *Model) tea.Cmd { m.switchGlobMode(id); return nil }}
		if v.Removed {
			mode.disabled = id + " is marked for removal"
		}
		items = append(items, def, rename, mode)
		items = append(items, m.actionItems(id)...)
	}
	return &menuOverlay{title: "More", items: append(items, m.commonMenuItems()...)}
}
