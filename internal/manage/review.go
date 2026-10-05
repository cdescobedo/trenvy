package manage

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
)

// reviewOverlay is w: every staged change, grouped by what it changes,
// before -> after, with irreversible lines marked (spec §3). It is also
// the confirmation step: nothing is written until enter here.
type reviewOverlay struct {
	cursor int
	// off scrolls a failure report into view when the panel is too short to
	// show it above the cursor: 0 follows the cursor, n > 0 puts the
	// report's line n-1 on the panel's first row.
	off     int
	failure string // the last commit's error, shown until the next attempt
	refused bool   // that error was ErrNothingWritten: retrying cannot succeed
}

type reviewLine struct {
	index        int    // into staged.All()
	kind         string // "var" or "output": variables list first
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
	// Variables, then outputs, each by name. A variable and an output may
	// share a name, so the kind is the first key.
	sort.SliceStable(idx, func(a, b int) bool {
		ca, cb := all[idx[a]], all[idx[b]]
		if oa, ob := isOutputChange(ca), isOutputChange(cb); oa != ob {
			return ob
		}
		return ca.Name < cb.Name
	})
	out := make([]reviewLine, 0, len(all))
	for _, i := range idx {
		out = append(out, m.describe(i, all[i]))
	}
	return out
}

func isOutputChange(c session.Change) bool {
	switch c.Kind {
	case session.ChangeAddOutput, session.ChangeRemoveOutput, session.ChangeRenameOutput,
		session.ChangeDefaultOutput, session.ChangeSetGlobs, session.ChangeSetOutputFormat, session.ChangeSetOutputPath:
		return true
	}
	return false
}

func valueOr(p *string, none string) string {
	if p == nil {
		return none
	}
	return display(*p)
}

func (m *Model) describe(i int, c session.Change) reviewLine {
	if isOutputChange(c) {
		return m.describeOutput(i, c)
	}
	l := reviewLine{index: i, kind: "var", group: c.Name, mark: "~"}
	base := m.snap.Matrix.Vars[c.Name]
	switch c.Kind {
	case session.ChangeAddVar:
		kind := "plain"
		if m.staged.Has(session.ChangePromoteSecret, c.Name) {
			kind = "secret"
		}
		l.what, l.detail = "add variable", "(none)  ->  "+kind
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
		l.what, l.detail, l.irreversible = m.describePromotion(c.Name, base)
	case session.ChangeSetOptional:
		l.what, l.detail = "mark required", "optional  ->  required"
		if c.Value == "true" {
			l.what, l.detail = "mark optional", "required  ->  optional"
		}
	default:
		l.what = c.Kind
	}
	return l
}

// describeOutput words an output change before -> after. A default-output
// change sits in its target's group, since the target is what it names.
func (m *Model) describeOutput(i int, c session.Change) reviewLine {
	l := reviewLine{index: i, kind: "output", group: "output " + displayName(c.Name, m.outView(c.Name)), mark: "~"}
	base := m.snap.Matrix.Outputs[c.Name]
	switch c.Kind {
	case session.ChangeAddOutput:
		l.what, l.detail = "add output", "(none)  ->  "+c.Format+" "+display(c.Path)
	case session.ChangeRemoveOutput:
		l.mark, l.what, l.irreversible = "x", "remove output", true
		l.detail = "its definition leaves the manifest; " + display(base.Path) + " stays on disk"
	case session.ChangeRenameOutput:
		l.what, l.detail = "rename", c.Name+"  ->  "+c.Value
	case session.ChangeDefaultOutput:
		before := m.snap.DefaultOutput
		if before == "" {
			before = "(none)"
		}
		l.what, l.detail = "make default", before+"  ->  "+c.Name
	case session.ChangeSetGlobs:
		l.what = "membership"
		l.detail = session.SelectsSummary(base) + "  ->  " + session.SelectsSummary(manifest.Output{Include: c.Include, Exclude: c.Exclude})
	case session.ChangeSetOutputFormat:
		l.what, l.detail = "format", base.Format+"  ->  "+c.Value
	case session.ChangeSetOutputPath:
		l.what, l.detail = "path", display(base.Path)+"  ->  "+display(c.Value)
	}
	return l
}

// describePromotion words a promote-secret from what is staged, not from
// disk: the set-secrets staged on name are what the keyring will hold. A
// variable added this session has no plain value to lose, so it is "new
// secret" and reversible until saved like any addition. A promoted one
// with a value on disk under a profile that has no staged set-secret
// (dropped with u) loses that value, and the line says the profile is left
// without one.
func (m *Model) describePromotion(name string, base manifest.Var) (what, detail string, irreversible bool) {
	if m.staged.Has(session.ChangeAddVar, name) {
		return "new secret", "values live in the keyring, never the manifest", false
	}
	writes := map[string]bool{}
	for _, c := range m.staged.On(name) {
		if c.Kind == session.ChangeSetSecret {
			writes[c.Profile] = true
		}
	}
	var into, lost []string
	for _, p := range m.snap.Matrix.Profiles {
		if writes[p] {
			into = append(into, p)
		}
	}
	for _, pc := range session.PromotionChanges(name, base, m.snap.Matrix.Profiles) {
		if pc.Kind == session.ChangeSetSecret && !writes[pc.Profile] {
			lost = append(lost, pc.Profile)
		}
	}
	detail = "moves " + strconv.Itoa(len(into)) + " into keyring"
	if len(into) > 0 {
		detail += ": " + strings.Join(into, ", ")
	}
	if len(lost) > 0 {
		state := "MISSING"
		if m.varView(name).Var.Optional {
			state = "unset"
		}
		verb := " becomes "
		if len(lost) > 1 {
			verb = " become "
		}
		detail += "; " + strings.Join(lost, ", ") + verb + state
	}
	return "make secret", detail, true
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
		// Past the last change j scrolls instead, down the failure report,
		// so a panel too short to show it above the changes can still
		// reach its end. k at the first change scrolls back.
		if r.cursor >= len(lines)-1 {
			w, _ := m.size()
			_, _, report := r.layout(m, w)
			r.off = min(r.off+1, report)
		} else {
			r.cursor, r.off = r.cursor+1, 0
		}
	case keyIs(k, "k", "up"):
		if r.cursor == 0 {
			r.off = max(r.off-1, 0)
		} else {
			r.cursor, r.off = r.cursor-1, 0
		}
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
		r.cursor, r.off = clamp(r.cursor, 0, m.staged.Len()-1), 0
	case keyIs(k, "enter"):
		if m.stale {
			m.msg = message{"can't save: the last reload failed -- esc, then . and Reload from disk", true}
			return false, nil
		}
		m.saving, r.failure, r.refused = true, "", false
		return false, m.commitCmd()
	}
	return false, nil
}

// view draws the review. Nothing in it is cut: a detail wraps under its
// own column, a "what" too wide for its column takes a line of its own, and
// the panel scrolls to keep the cursor in view. The save's status and a
// failure's report come first, above the changes, so they are on screen
// when the review reopens with them.
func (r *reviewOverlay) view(m *Model, w, h int) string {
	body, focus, _ := r.layout(m, w)
	if r.off > 0 {
		// panelAt keeps focus on the last row it shows, so the line to show
		// first is rows-1 above it. Two borders, the title and its blank
		// line take four of h.
		rows := max(1, min(h, len(body)+4)-4)
		focus = r.off - 1 + rows - 1
	}
	return m.panelAt("Review "+m.th.PillStaged.Render(plural(m.staged.Len(), "change")), body, focus, w, h)
}

// layout is the review's body, the body line the cursor is on, and how many
// of the body's leading lines are the failure report.
func (r *reviewOverlay) layout(m *Model, w int) (body []string, focus, report int) {
	t := m.th
	inner := min(w, panelMax) - 2
	// "> ~ " leads each line; the what column and the details follow, with
	// a one-cell gap before the border.
	const lead = 4
	whatW := clamp((inner-lead)/2, 8, 22)
	detailW := inner - lead - whatW - 1
	switch {
	case m.saving:
		body = append(body, t.Muted.Render("saving…"), "")
	case m.reloading:
		body = append(body, t.Muted.Render("reloading…"), "")
	case r.failure != "":
		for _, l := range strings.Split(r.failure, "\n") {
			// A report line keeps its own indent; its continuation hangs
			// two cells further in.
			l = display(l)
			text := strings.TrimLeft(l, " ")
			ind := len(l) - len(text)
			for j, part := range wrap(text, inner-1-ind-2) {
				pre := strings.Repeat(" ", ind)
				if j > 0 {
					pre += "  "
				}
				body = append(body, pre+t.Danger.Render(part))
			}
		}
		report = len(body)
		for _, part := range wrap(r.nextStep(m), inner-1) {
			body = append(body, t.Text.Render(part))
		}
		body = append(body, "")
	}
	group := ""
	indent := strings.Repeat(" ", lead+whatW)
	for i, l := range m.reviewLines() {
		if l.group != group {
			group = l.group
			for _, part := range wrap(display(group), inner-1) {
				body = append(body, t.Title.Render(part))
			}
		}
		cur := "  "
		if i == r.cursor {
			cur, focus = t.Accent.Render("> "), len(body)
		}
		st, text := t.Text, l.detail
		if l.irreversible {
			st = t.Danger
		}
		// A detail that would wrap beside its label drops under it instead,
		// where it has the panel's full width: a path or a sentence stays
		// in one piece more often than in a narrow column.
		dw, hang, own := detailW, indent, lipgloss.Width(l.what) > whatW
		details := wrap(text, dw)
		if len(details) > 1 {
			own = true
		}
		if own {
			dw, hang = inner-lead-1, strings.Repeat(" ", lead)
			details = wrap(text, dw)
		}
		if text == "" {
			details = nil
		}
		if l.irreversible {
			// The "!" leads its own line rather than trailing the detail, so
			// it never decides where the detail wraps.
			details = append(details, wrap("! cannot be undone after saving", dw)...)
		}
		head := cur + t.Staged.Render(l.mark) + " "
		if lipgloss.Width(l.what) > whatW {
			for j, part := range wrap(l.what, inner-lead-1) {
				if j > 0 {
					head = strings.Repeat(" ", lead)
				}
				body = append(body, head+t.Text.Render(part))
			}
		} else if own {
			body = append(body, head+t.Text.Render(l.what))
		} else if len(details) > 0 {
			body = append(body, head+t.Text.Render(pad(l.what, whatW))+st.Render(details[0]))
			details = details[1:]
		} else {
			body = append(body, head+t.Text.Render(l.what))
		}
		for _, d := range details {
			body = append(body, hang+st.Render(d))
		}
	}
	return body, focus, report
}

// nextStep is the line under a failure's report. A partial failure is
// retried whole (spec §3); a refused batch wrote nothing and would be
// refused again, so it points at the change to drop or edit instead. When
// the reload after the failure failed too, enter is refused until a reload
// lands, so retrying is not on offer.
func (r *reviewOverlay) nextStep(m *Model) string {
	if m.stale {
		return "every change is still staged -- press esc, then . and Reload from disk, then w to retry"
	}
	if r.refused {
		return "nothing was written -- u to drop the change the manifest refuses, or esc to edit"
	}
	return "every change is still staged -- enter to retry"
}

func (r *reviewOverlay) hints(m *Model) []hint {
	if m.saving {
		return []hint{{"ctrl+c", "quit now -- the save may be left partly applied"}}
	}
	if m.reloading {
		return []hint{{"ctrl+c", "quit now"}}
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
// anything else can be staged or saved: input stays blocked (reloading)
// from this message until the reload lands, on success or failure.
func (m Model) afterCommit(msg committedMsg) (tea.Model, tea.Cmd) {
	m.saving, m.reloading = false, true
	if msg.err != nil {
		if r := m.review(); r != nil {
			r.failure, r.refused = msg.err.Error(), errors.Is(msg.err, ErrNothingWritten)
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
	if n := len(msg.res.LeftInPlace); n > 0 {
		// The paths go in a panel of their own, where they wrap in full; on
		// the message line they would push out what follows a failed reload.
		text += " -- " + plural(n, "file") + " left on disk, listed"
		m.push(&filesOverlay{paths: msg.res.LeftInPlace})
	}
	m.msg = message{text, false}
	return m, m.reloadCmd()
}

// afterReload swaps in a fresh snapshot, keeping each screen's cursor on
// the same variable and output. Revealed values are dropped: a reload is a
// new view of disk, and a secret shown before it must be asked for again.
// Check now results go with them, since they described the old snapshot.
func (m Model) afterReload(msg reloadedMsg) (tea.Model, tea.Cmd) {
	m.reloading = false
	m.gen++
	m.vars.revealed = map[cellKey]string{}
	if msg.err != nil {
		m.stale = true
		// The recovery comes before the error, whose length is not ours to
		// choose, so the message line never cuts it off.
		text := "reload failed, press . then Reload from disk: " + msg.err.Error()
		// A save that landed set a plain message just before this reload;
		// keep its first clause, or the failure reads as if nothing was
		// saved. Not the file list: that has its own panel.
		if m.msg.text != "" && !m.msg.isErr {
			text += " -- " + strings.SplitN(m.msg.text, " -- ", 2)[0]
		}
		m.msg = message{text, true}
		return m, nil
	}
	keep, _ := m.selected()
	keepOut, _ := m.selectedOutput()
	m.stale, m.snap = false, msg.snap
	if keep != "" {
		m.selectName(keep)
	}
	if keepOut != "" {
		m.selectOutput(keepOut)
	}
	m.outs.checked = map[string]string{}
	return m, nil
}
