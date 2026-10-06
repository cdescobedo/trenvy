package manage

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/sample"
	"github.com/cdescobedo/trenvy/internal/style"
)

// SetupMode picks which steps the setup flow runs.
type SetupMode int

const (
	SetupNew    SetupMode = iota // steps Project, Seed, Review, Create
	SetupImport                  // steps Review, Import (spec §6: import opens steps 3-4)
)

// SetupSample is one sample setup can seed from, with everything the
// summary shows about it worked out by the caller.
type SetupSample struct {
	Path       string         // as shown: relative to the worktree
	Entries    []sample.Entry // parsed, in file order
	OutputName string         // the output it creates or reuses; "" with NoOutput
	OutputPath string
	Reused     bool // an existing output already renders to OutputPath
	NoOutput   bool // import --no-output
	Ignored    bool // git ignores OutputPath (true outside a repository)
}

// SetupInput is every fact the setup flow shows or checks. The caller
// gathers them; Setup itself reads nothing.
type SetupInput struct {
	Mode         SetupMode
	ID, Root     string // prefills (SetupNew); ID alone names the project (SetupImport)
	Dir          string // where trenvy runs; the root must contain it
	TakenIDs     []string
	TakenRoots   []string
	ManifestPath func(id string) string // pure: config.ManifestPath
	Samples      []SetupSample          // SetupNew: detected; SetupImport: exactly one
	// NoSamplesNote is the seed step's word on why no sample was found, shown
	// only when Samples is empty; "" shows none.
	NoSamplesNote string
	Existing      map[string]manifest.Var // SetupImport: the manifest's variables, for conflicts
	Force         bool                    // SetupImport: import --force
}

// SetupResult is what a finished setup asks the caller to write.
type SetupResult struct {
	ID, Root string
	Sample   int            // index into Samples; -1 is Start empty
	Entries  []sample.Entry // rows kept, with the secret flags and edited values
}

type setupStep int

const (
	stepProject setupStep = iota
	stepSeed
	stepReview
	stepSummary
)

type setupRow struct {
	entry sample.Entry
	left  bool // left out of the import
}

// Setup is the setup flow (spec §6). Like Model it performs no I/O: the
// caller hands it every fact in SetupInput and writes the SetupResult.
type Setup struct {
	th            style.Theme
	in            SetupInput
	width, height int
	step          setupStep
	id, root      field
	focus         int // 0 the id, 1 the root
	seed          int // len(in.Samples) is Start empty
	loaded        int // the sample rows were loaded from; -1 none
	rows          []setupRow
	cursor        int
	scroll        int // the summary's first visible line
	editing       bool
	edit          field
	msg           message
	result        *SetupResult
	quit          bool
}

// NewSetup starts the flow. In SetupImport, in.Samples must hold exactly
// one sample, the file being imported; only the first is used, and with
// none the review is empty and the result carries no entries.
func NewSetup(in SetupInput, th style.Theme) Setup {
	s := Setup{th: th, in: in, id: newField(in.ID, false), root: newField(in.Root, false), loaded: -1}
	if in.Mode == SetupImport {
		s.step = stepReview
		if len(in.Samples) > 0 {
			s.load(0)
		}
	}
	return s
}

func (s *Setup) load(i int) {
	if s.loaded == i {
		return
	}
	s.rows, s.cursor, s.loaded = nil, 0, i
	for _, e := range s.in.Samples[i].Entries {
		// A sample value guessed secret is never shown and never stored, so
		// it is dropped here: unticking the row leaves it plain and empty.
		if e.GuessSecret {
			e.Value = ""
		}
		s.rows = append(s.rows, setupRow{entry: e})
	}
}

func (s Setup) Init() tea.Cmd { return nil }

// Result is the finished setup; false when it was cancelled or is not done.
func (s Setup) Result() (SetupResult, bool) {
	if s.result == nil {
		return SetupResult{}, false
	}
	return *s.result, true
}

// Quitting reports that the flow has ended, finished or cancelled.
func (s Setup) Quitting() bool { return s.quit || s.result != nil }

func (s Setup) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			s.quit = true
			return s, tea.Quit
		}
		s.msg = message{}
		return s.key(msg)
	}
	return s, nil
}

func (s Setup) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if s.editing {
		switch k.Type {
		case tea.KeyEnter:
			s.rows[s.cursor].entry.Value, s.editing = s.edit.value(), false
		case tea.KeyEsc:
			s.editing = false
		default:
			s.edit = s.edit.update(k)
		}
		return s, nil
	}
	if k.Type == tea.KeyEsc {
		return s.back()
	}
	switch s.step {
	case stepProject:
		return s.projectKey(k)
	case stepSeed:
		return s.seedKey(k)
	case stepReview:
		return s.reviewKey(k)
	}
	return s.summaryKey(k)
}

func (s Setup) firstStep() setupStep {
	if s.in.Mode == SetupImport {
		return stepReview
	}
	return stepProject
}

func (s Setup) back() (tea.Model, tea.Cmd) {
	switch {
	case s.step == s.firstStep():
		s.quit = true
		return s, tea.Quit
	case s.step == stepSummary && s.startEmpty():
		s.step = stepSeed
	default:
		s.step--
	}
	return s, nil
}

func (s Setup) startEmpty() bool { return s.in.Mode == SetupNew && s.seed == len(s.in.Samples) }

func (s Setup) projectKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyTab, tea.KeyShiftTab, tea.KeyUp, tea.KeyDown:
		s.focus = 1 - s.focus
	case tea.KeyEnter:
		if p := s.idProblem(); p != "" {
			s.msg = message{p, true}
		} else if p := s.rootProblem(); p != "" {
			s.msg = message{p, true}
		} else {
			s.step = stepSeed
		}
	default:
		if s.focus == 0 {
			s.id = s.id.update(k)
		} else {
			s.root = s.root.update(k)
		}
	}
	return s, nil
}

func (s Setup) chosenID() string   { return strings.TrimSpace(s.id.value()) }
func (s Setup) chosenRoot() string { return filepath.Clean(strings.TrimSpace(s.root.value())) }

func (s Setup) idProblem() string {
	id := s.chosenID()
	if err := manifest.ValidateProjectID(id); err != nil {
		return "project id " + strconv.Quote(id) + ": " + err.Error()
	}
	if slices.Contains(s.in.TakenIDs, id) {
		return id + " is already a registered project; pick another id"
	}
	return ""
}

// rootProblem is ruling 4: an absolute root, nobody else's, holding the
// directory trenvy runs in -- or the new project could not be found here.
func (s Setup) rootProblem() string {
	root := s.chosenRoot()
	switch {
	case !filepath.IsAbs(root):
		return "the root must be an absolute path"
	case slices.ContainsFunc(s.in.TakenRoots, func(r string) bool { return filepath.Clean(r) == root }):
		return "another project is registered at " + root
	case !config.UnderOrEqual(filepath.Clean(s.in.Dir), root):
		// The registry's own rule, so setup never writes a project that
		// Lookup would then not find from here.
		return "this directory is not under " + root + "; trenvy would not find the project from here"
	}
	return ""
}

func (s Setup) seedKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyIs(k, "j", "down"):
		s.seed = min(s.seed+1, len(s.in.Samples))
	case keyIs(k, "k", "up"):
		s.seed = max(s.seed-1, 0)
	case keyIs(k, "enter"):
		if s.startEmpty() {
			s.step, s.scroll = stepSummary, 0
		} else {
			s.load(s.seed)
			s.step = stepReview
		}
	}
	return s, nil
}

func (s Setup) reviewKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if keyIs(k, "enter") {
		s.step, s.scroll = stepSummary, 0
		return s, nil
	}
	if len(s.rows) == 0 {
		return s, nil
	}
	r := &s.rows[s.cursor]
	switch {
	case keyIs(k, "j", "down"):
		s.cursor = min(s.cursor+1, len(s.rows)-1)
	case keyIs(k, "k", "up"):
		s.cursor = max(s.cursor-1, 0)
	case keyIs(k, " "):
		if r.left {
			s.msg = message{r.entry.Name + " is left out; d brings it back", true}
		} else {
			r.entry.GuessSecret = !r.entry.GuessSecret
		}
	case keyIs(k, "d"):
		r.left = !r.left
	case keyIs(k, "e"):
		switch {
		case r.left:
			s.msg = message{r.entry.Name + " is left out; d brings it back", true}
		case r.entry.GuessSecret:
			s.msg = message{"a secret's value is stored after setup, never from the sample", true}
		default:
			s.editing, s.edit = true, newField(r.entry.Value, false)
		}
	}
	return s, nil
}

// kept is the review's rows that will be imported.
func (s Setup) kept() []sample.Entry {
	var out []sample.Entry
	for _, r := range s.rows {
		if !r.left {
			out = append(out, r.entry)
		}
	}
	return out
}

// conflicts is ruling 5: variables the import would change in the manifest.
func (s Setup) conflicts() []string {
	var out []string
	for _, e := range s.kept() {
		v := manifest.Var{Secret: e.GuessSecret}
		if !e.GuessSecret {
			val := e.Value
			v.Value = &val
		}
		if old, ok := s.in.Existing[e.Name]; ok && !manifest.SameVar(old, v) {
			out = append(out, e.Name)
		}
	}
	sort.Strings(out)
	return out
}

// blocked is ruling 5's refusal: conflicts and no --force.
func (s Setup) blocked() bool { return len(s.conflicts()) > 0 && !s.in.Force }

func (s Setup) summaryKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyIs(k, "j", "down"):
		s.scroll = min(s.scroll+1, s.summaryOverflow())
		return s, nil
	case keyIs(k, "k", "up"):
		// Clamped first: a resize may have shrunk the overflow.
		s.scroll = max(min(s.scroll, s.summaryOverflow())-1, 0)
		return s, nil
	case !keyIs(k, "enter"):
		return s, nil
	}
	if s.blocked() {
		s.msg = message{strings.Join(s.conflicts(), ", ") + " already in the manifest; re-run with --force to overwrite", true}
		return s, nil
	}
	res := SetupResult{ID: s.chosenID(), Root: s.chosenRoot(), Sample: -1}
	if s.in.Mode == SetupImport {
		// Import names an existing project; it has no root field to read.
		res.Root = s.in.Root
	}
	if !s.startEmpty() {
		res.Sample, res.Entries = s.loaded, s.kept()
	}
	s.result = &res
	return s, tea.Quit
}

// --- view ---

func (s Setup) View() string {
	if s.Quitting() {
		return ""
	}
	w, h := termSize(s.width, s.height)
	if small := tooSmall(w, h); small != "" {
		return small
	}
	msg := messageLines(s.th, s.msg, w)
	bodyH := s.bodyHeight(w, h)
	lines, first, last := s.body(contentWidth(w))
	if s.step == stepSummary {
		first, last = s.scroll, s.scroll+bodyH-3
	}
	body := s.th.Pane(strings.Join(windowRow(lines, first, last, bodyH-2), "\n"), w, bodyH, true)
	head := append(s.headerLines(w), s.stepRow(w), body)
	return strings.Join(append(append(head, msg...), footerLine(s.th, s.hints(), w)), "\n")
}

// contentWidth is the body's content column: the pane's inner width less
// the two-cell cursor lead and a cell of margin, as outDetailBody leaves.
func contentWidth(w int) int { return w - 5 }

// bodyHeight is the pane's height, borders included: what the header, the
// step row, the message and the footer leave.
func (s Setup) bodyHeight(w, h int) int {
	return h - chromeLines + 1 - len(messageLines(s.th, s.msg, w)) - (len(s.headerLines(w)) - 1)
}

// summaryOverflow is how many summary lines do not fit the pane at once.
func (s Setup) summaryOverflow() int {
	w, h := termSize(s.width, s.height)
	return max(0, len(s.summaryBody(contentWidth(w)))-(s.bodyHeight(w, h)-2))
}

// headerMax is the most lines the header takes, so a long import path
// never pushes the view past a short terminal.
const headerMax = 2

// headerLines is the header. In import mode it names the file, a value, so
// it wraps rather than being cut, up to headerMax lines; a path that cannot
// fit them is cut at its front with …, keeping the file's name.
func (s Setup) headerLines(w int) []string {
	title := "Set up a project"
	if s.in.Mode == SetupImport {
		title = "Import"
		if len(s.in.Samples) > 0 {
			title += " " + display(s.in.Samples[0].Path)
		}
	}
	lines := wrap(title, w-2)
	if len(lines) > headerMax && s.in.Mode == SetupImport && len(s.in.Samples) > 0 {
		// Packed rather than wrapped at the space after "Import", which
		// would leave its first line nearly empty, and the path cut a cell
		// at a time until it fits: a two-cell rune may not fill a line.
		path := display(s.in.Samples[0].Path)
		for budget := min(lipgloss.Width(path), headerMax*(w-2)); budget > 0; budget-- {
			if packed := packLines("Import "+cutFront(path, budget), w-2); len(packed) <= headerMax {
				lines = packed
				break
			}
		}
	}
	// Whatever is left over wraps past headerMax only below the interface's
	// minimum width, where View shows the too-small sentence instead.
	lines = lines[:min(len(lines), headerMax)]
	for i, l := range lines {
		lines[i] = " " + s.th.Title.Render(l)
	}
	return lines
}

// packLines splits s into lines of at most w cells, filling each.
func packLines(s string, w int) []string {
	var lines []string
	for s != "" {
		var l string
		l, s = splitCells(s, w)
		lines = append(lines, l)
	}
	return lines
}

// cutFront shortens s to w cells by dropping its front, starting with …
// when it had to cut, so a path keeps its file name.
func cutFront(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	rs := []rune(s)
	used, i := 1, len(rs) // the … takes one cell
	for i > 0 && used+lipgloss.Width(string(rs[i-1])) <= w {
		i--
		used += lipgloss.Width(string(rs[i]))
	}
	return "…" + string(rs[i:])
}

// stepRow draws the steps as viewTabs draws the tabs: the current one
// bracketed, so it reads without colour.
func (s Setup) stepRow(w int) string {
	labels, at := []string{"Project", "Seed", "Review", "Create"}, int(s.step)
	if s.in.Mode == SetupImport {
		labels, at = []string{"Review", "Import"}, int(s.step-stepReview)
	}
	var parts []string
	for i, l := range labels {
		if i == at {
			parts = append(parts, "["+s.th.TabActive.Render(l)+"]")
		} else {
			parts = append(parts, s.th.TabInactive.Render(l))
		}
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(" " + strings.Join(parts, " "))
}

// FooterKeys lists the keys the footer shows now, for tests.
func (s Setup) FooterKeys() []string { return hintKeys(s.hints()) }

// hints is the footer: only keys that act at this moment, ordered by
// importance as Model's are -- the step's enter first, esc last -- so a
// narrow footer, which drops hints from the right, keeps both.
func (s Setup) hints() []hint {
	if s.editing {
		return []hint{{"enter", "keep"}, {"esc", "cancel edit"}}
	}
	esc := hint{"esc", "back"}
	if s.step == s.firstStep() {
		esc = hint{"esc", "cancel"}
	}
	switch s.step {
	case stepProject:
		return []hint{{"enter", "next"}, {"tab", "switch field"}, esc}
	case stepSeed:
		return []hint{{"enter", "choose"}, {"j/k", "move"}, esc}
	case stepReview:
		hs := []hint{{"enter", "continue"}}
		if len(s.rows) > 0 {
			r := s.rows[s.cursor]
			if !r.left {
				hs = append(hs, hint{"space", "secret"})
			}
			if !r.left && !r.entry.GuessSecret {
				hs = append(hs, hint{"e", "edit"})
			}
			if r.left {
				hs = append(hs, hint{"d", "bring back"})
			} else {
				hs = append(hs, hint{"d", "leave out"})
			}
		}
		return append(hs, esc)
	}
	var hs []hint
	if !s.blocked() {
		verb := "create"
		if s.in.Mode == SetupImport {
			verb = "import"
		}
		hs = append(hs, hint{"enter", verb})
	}
	if s.summaryOverflow() > 0 {
		hs = append(hs, hint{"j/k", "scroll"})
	}
	return append(hs, esc)
}

// body is the current step's lines, each led by "> " or two spaces, with
// the first and last line of the row the cursor is on.
func (s Setup) body(inner int) (lines []string, first, last int) {
	switch s.step {
	case stepProject:
		return s.projectBody(inner)
	case stepSeed:
		return s.seedBody(inner)
	case stepReview:
		return s.reviewBody(inner)
	}
	return s.summaryBody(inner), 0, 0
}

// columns sets left and right side by side, each wrapped in its own
// column, so neither is cut. leftW includes a two-cell gap.
func columns(left string, leftSt lipgloss.Style, leftW int, right string, rightSt lipgloss.Style, rightW int) []string {
	l, r := wrap(display(left), max(1, leftW-2)), wrap(display(right), rightW)
	out := make([]string, max(len(l), len(r)))
	for i := range out {
		var a, b string
		if i < len(l) {
			a = l[i]
		}
		if i < len(r) {
			b = r[i]
		}
		out[i] = leftSt.Render(pad(a, leftW)) + rightSt.Render(b)
	}
	return out
}

// lead puts the cursor on the focused row's first line and indents the rest.
func (s Setup) lead(lines []string, focused bool) []string {
	for i := range lines {
		if i == 0 && focused {
			lines[i] = s.th.Accent.Render(">") + " " + lines[i]
		} else {
			lines[i] = "  " + lines[i]
		}
	}
	return lines
}

func (s Setup) projectBody(inner int) ([]string, int, int) {
	const labelW = 12 // "project id" and a gap
	valueW := max(8, inner-labelW)
	var lines []string
	first, last := 0, 0
	row := func(label string, f field, problem string, focused bool) {
		block := []string{s.th.Muted.Render(pad(label, labelW)) + f.view(s.th, valueW)}
		if problem != "" {
			for _, p := range wrap(display(problem), valueW) {
				block = append(block, strings.Repeat(" ", labelW)+s.th.Danger.Render(p))
			}
		}
		if focused {
			first, last = len(lines), len(lines)+len(block)-1
		}
		lines = append(lines, s.lead(block, focused)...)
	}
	row("project id", s.id, s.idProblem(), s.focus == 0)
	lines = append(lines, "")
	row("root", s.root, s.rootProblem(), s.focus == 1)
	return lines, first, last
}

func (s Setup) seedBody(inner int) ([]string, int, int) {
	const empty = "Start empty"
	pathW := lipgloss.Width(empty)
	for _, smp := range s.in.Samples {
		pathW = max(pathW, lipgloss.Width(display(smp.Path)))
	}
	pathW = min(pathW, inner/2) + 2
	descW := max(8, inner-pathW)
	var lines []string
	first, last := 0, 0
	row := func(i int, label, desc string, st lipgloss.Style) {
		block := columns(label, st, pathW, desc, s.th.Muted, descW)
		if i == s.seed {
			first, last = len(lines), len(lines)+len(block)-1
		}
		lines = append(lines, s.lead(block, i == s.seed)...)
	}
	if len(s.in.Samples) == 0 && s.in.NoSamplesNote != "" {
		for _, l := range wrap(display(s.in.NoSamplesNote), inner) {
			lines = append(lines, "  "+s.th.Muted.Render(l))
		}
		lines = append(lines, "")
	}
	for i, smp := range s.in.Samples {
		row(i, smp.Path, plural(len(smp.Entries), "variable"), s.th.Text)
	}
	row(len(s.in.Samples), empty, "add variables yourself afterwards", s.th.Text)
	return lines, first, last
}

func (s Setup) reviewBody(inner int) ([]string, int, int) {
	if len(s.rows) == 0 {
		return []string{"  " + s.th.Muted.Render("No variables in this sample")}, 0, 0
	}
	const boxW = 4 // "[x] "
	longest := 0
	for _, r := range s.rows {
		longest = max(longest, lipgloss.Width(display(r.entry.Name)))
	}
	nameW := min(longest, max(4, (inner-boxW)/2)) + 2
	valueW := max(8, inner-boxW-nameW)
	var lines []string
	first, last := 0, 0
	for i, r := range s.rows {
		box := "[ ]"
		if r.entry.GuessSecret {
			box = "[x]"
		}
		nameSt, text, textSt := s.th.Text, display(r.entry.Value), s.th.Text
		switch {
		case r.left:
			nameSt, text, textSt = s.th.Muted, "x left out", s.th.Muted
		case r.entry.GuessSecret:
			text, textSt = "you'll store this next", s.th.Muted
		case r.entry.Value == "":
			text, textSt = "empty", s.th.Muted
		}
		var value []string
		if i == s.cursor && s.editing {
			value = []string{s.edit.view(s.th, valueW)}
		} else {
			for _, p := range wrap(text, valueW) {
				value = append(value, textSt.Render(p))
			}
		}
		name := pad(cutTo(display(r.entry.Name), nameW-2), nameW)
		block := []string{s.th.Text.Render(box+" ") + nameSt.Render(name) + value[0]}
		for _, v := range value[1:] {
			block = append(block, strings.Repeat(" ", boxW+nameW)+v)
		}
		if i == s.cursor {
			first, last = len(lines), len(lines)+len(block)-1
		}
		lines = append(lines, s.lead(block, i == s.cursor)...)
	}
	return lines, first, last
}

func (s Setup) summaryBody(inner int) []string {
	const labelW = 11 // "variables" and a gap
	valueW := max(8, inner-labelW)
	var lines []string
	add := func(label string, values ...string) {
		for i, v := range values {
			l := ""
			if i == 0 {
				l = label
			}
			lines = append(lines, columns(l, s.th.Muted, labelW, v, s.th.Text, valueW)...)
		}
	}
	id := s.chosenID()
	if s.in.Mode == SetupNew {
		add("project", id)
		add("root", s.chosenRoot())
	}
	add("manifest", s.in.ManifestPath(id))
	add("output", s.outputLines()...)

	plain, secret := 0, 0
	for _, e := range s.kept() {
		if e.GuessSecret {
			secret++
		} else {
			plain++
		}
	}
	switch {
	case s.in.Mode == SetupImport && plain+secret == 0:
		// Import exits after saving; it never opens Variables.
		add("variables", "none; nothing will be imported")
	case s.startEmpty() || plain+secret == 0:
		add("variables", "none; add them in Variables afterwards")
	case secret > 0:
		add("variables", fmt.Sprintf("%d plain, %d secret; you'll store the secrets next", plain, secret))
	default:
		add("variables", fmt.Sprintf("%d plain, %d secret", plain, secret))
	}

	if s.in.Mode == SetupImport {
		c := s.conflicts()
		switch {
		case len(c) == 0:
			add("conflicts", "none")
		case s.in.Force:
			add("conflicts", strings.Join(c, ", ")+": already in the manifest with a different definition; --force overwrites them")
		default:
			add("conflicts", strings.Join(c, ", ")+": already in the manifest with a different definition; re-run with --force to overwrite")
		}
	}
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return lines
}

// outputLines is the summary's output: what it renders to, and whether
// render's gitignore guard will let it.
func (s Setup) outputLines() []string {
	if s.startEmpty() || s.loaded < 0 {
		return []string{"none"}
	}
	smp := s.in.Samples[s.loaded]
	switch {
	case smp.NoOutput:
		return []string{"none"}
	case smp.Reused:
		return []string{smp.OutputName + " already renders to " + smp.OutputPath}
	case smp.Ignored:
		return []string{smp.OutputName + " -> " + smp.OutputPath, "git ignores it"}
	}
	return []string{smp.OutputName + " -> " + smp.OutputPath, "git does not ignore it; render will refuse until it does"}
}

// RunSetup drives s in the terminal's alternate screen until it quits.
func RunSetup(s Setup) (Setup, error) {
	final, err := tea.NewProgram(s, tea.WithAltScreen()).Run()
	if err != nil {
		return s, fmt.Errorf("run setup: %w", err)
	}
	fs, ok := final.(Setup)
	if !ok {
		return s, fmt.Errorf("run setup: unexpected final model %T", final)
	}
	return fs, nil
}
