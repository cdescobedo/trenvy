package manage_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/sample"
)

func setupInput() manage.SetupInput {
	return manage.SetupInput{
		Mode: manage.SetupNew, ID: "app", Root: "/code/app", Dir: "/code/app/main",
		TakenIDs: []string{"other"}, TakenRoots: []string{"/code/other"},
		ManifestPath: func(id string) string { return "/cfg/projects/" + id + ".toml" },
		Samples: []manage.SetupSample{{
			Path: ".env.example",
			Entries: []sample.Entry{
				{Name: "APP_MODE", Value: "development"},
				{Name: "API_TOKEN", Value: "changeme", GuessSecret: true},
				{Name: "DATABASE_URL", Value: "postgres://localhost/app"},
			},
			OutputName: "local", OutputPath: ".env", Ignored: true,
		}},
	}
}

func setupKeys(s manage.Setup, keys ...string) manage.Setup {
	for _, k := range keys {
		next, _ := s.Update(key(k))
		s = next.(manage.Setup)
	}
	return s
}

func setupType(s manage.Setup, text string) manage.Setup {
	next, _ := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	return next.(manage.Setup)
}

func newSetupAt(in manage.SetupInput, w, h int) manage.Setup {
	next, _ := manage.NewSetup(in, noTheme()).Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(manage.Setup)
}

func TestSetupHappyPathKeepsTheReviewChoices(t *testing.T) {
	s := newSetupAt(setupInput(), 100, 30)
	s = setupKeys(s, "enter")       // project accepted
	s = setupKeys(s, "enter")       // seed: .env.example
	s = setupKeys(s, "space")       // APP_MODE becomes secret
	s = setupKeys(s, "j", "j", "d") // leave DATABASE_URL out
	s = setupKeys(s, "enter")       // summary
	v := s.View()
	for _, want := range []string{"/cfg/projects/app.toml", "local -> .env", "git ignores it", "2 secret"} {
		if !strings.Contains(v, want) {
			t.Errorf("summary lacks %q:\n%s", want, v)
		}
	}
	s = setupKeys(s, "enter")
	res, ok := s.Result()
	if !ok || res.ID != "app" || res.Root != "/code/app" || res.Sample != 0 {
		t.Fatalf("result = %+v %v", res, ok)
	}
	if len(res.Entries) != 2 || !res.Entries[0].GuessSecret || res.Entries[1].Name != "API_TOKEN" {
		t.Errorf("entries = %+v", res.Entries)
	}
}

func TestSetupRefusesAnInvalidIDUntilEdited(t *testing.T) {
	in := setupInput()
	in.ID = "my app"
	s := newSetupAt(in, 100, 30)
	if !strings.Contains(s.View(), `project id "my app"`) {
		t.Errorf("the prefill's problem should show before enter:\n%s", s.View())
	}
	s = setupKeys(s, "enter")
	if strings.Contains(s.View(), "Start empty") {
		t.Fatalf("an invalid id moved on to the seed step:\n%s", s.View())
	}
	s = setupKeys(setupType(s, "my-app"), "enter")
	if !strings.Contains(s.View(), "Start empty") {
		t.Errorf("a valid id should move on:\n%s", s.View())
	}
}

func TestSetupRootMustBeAbsoluteFreeAndContainThisDirectory(t *testing.T) {
	cases := map[string]string{
		"relative":    "the root must be an absolute path",
		"/code/other": "another project is registered at /code/other",
		"/elsewhere":  "this directory is not under /elsewhere",
	}
	for root, want := range cases {
		s := setupKeys(setupType(setupKeys(newSetupAt(setupInput(), 100, 30), "tab"), root), "enter")
		if !strings.Contains(s.View(), want) || strings.Contains(s.View(), "Start empty") {
			t.Errorf("root %q: want refusal %q:\n%s", root, want, s.View())
		}
	}
	if s := setupKeys(newSetupAt(setupInput(), 100, 30), "enter"); !strings.Contains(s.View(), "Start empty") {
		t.Errorf("the prefilled root should be accepted:\n%s", s.View())
	}
}

func TestSetupStartEmptySkipsTheReview(t *testing.T) {
	s := setupKeys(newSetupAt(setupInput(), 100, 30), "enter", "j", "enter")
	if v := s.View(); !strings.Contains(v, "none") || strings.Contains(v, "APP_MODE") {
		t.Errorf("start empty should land on the summary with no output:\n%s", v)
	}
	s = setupKeys(s, "esc")
	if !strings.Contains(s.View(), "Start empty") {
		t.Errorf("esc from an empty summary should return to the seed step:\n%s", s.View())
	}
	s = setupKeys(s, "enter", "enter")
	if res, ok := s.Result(); !ok || res.Sample != -1 || len(res.Entries) != 0 {
		t.Errorf("result = %+v %v", res, ok)
	}
}

func TestSetupNeverShowsASecretsSampleValue(t *testing.T) {
	v := setupKeys(newSetupAt(setupInput(), 100, 30), "enter", "enter").View()
	if strings.Contains(v, "changeme") || !strings.Contains(v, "you'll store this next") {
		t.Errorf("review:\n%s", v)
	}
}

func TestSetupEditsAPlainValueAndRefusesASecrets(t *testing.T) {
	s := setupKeys(newSetupAt(setupInput(), 100, 30), "enter", "enter", "e")
	s = setupKeys(setupType(s, "prod-mode"), "enter")
	s = setupKeys(s, "j", "e")
	if !strings.Contains(s.View(), "stored after setup") {
		t.Errorf("e on a secret should be refused:\n%s", s.View())
	}
	s = setupKeys(s, "enter", "enter")
	res, _ := s.Result()
	if len(res.Entries) == 0 || res.Entries[0].Value != "prod-mode" {
		t.Errorf("entries = %+v", res.Entries)
	}
}

func TestSetupCancelsFromAnyStep(t *testing.T) {
	for _, keys := range [][]string{{"esc"}, {"enter", "ctrl+c"}, {"enter", "enter", "ctrl+c"}, {"enter", "enter", "enter", "ctrl+c"}} {
		s := setupKeys(newSetupAt(setupInput(), 100, 30), keys...)
		if _, ok := s.Result(); ok || !s.Quitting() {
			t.Errorf("%v: result ok=%v quitting=%v, want cancelled", keys, ok, s.Quitting())
		}
	}
}

func importInput() manage.SetupInput {
	in := setupInput()
	in.Mode = manage.SetupImport
	old := "x"
	in.Existing = map[string]manifest.Var{"APP_MODE": {Value: &old}}
	return in
}

func TestImportStartsAtTheReviewAndEscCancels(t *testing.T) {
	s := newSetupAt(importInput(), 100, 30)
	if !strings.Contains(s.View(), "APP_MODE") || strings.Contains(s.View(), "Start empty") {
		t.Errorf("import should open on the review:\n%s", s.View())
	}
	if s = setupKeys(s, "esc"); !s.Quitting() {
		t.Error("esc on import's first step should cancel")
	}
}

// Review Focus 5.
func TestImportSummaryRefusesConflictsWithoutForce(t *testing.T) {
	s := setupKeys(newSetupAt(importInput(), 100, 30), "enter", "enter")
	if _, ok := s.Result(); ok || !strings.Contains(s.View(), "re-run with --force") {
		t.Errorf("a conflict without --force must be refused:\n%s", s.View())
	}
	in := importInput()
	in.Force = true
	if _, ok := setupKeys(newSetupAt(in, 100, 30), "enter", "enter").Result(); !ok {
		t.Error("with --force the import should finish")
	}
}

func TestSetupLayoutNeverExceedsTheTerminal(t *testing.T) {
	in := setupInput()
	in.Root = "/code/" + strings.Repeat("deep/", 30) + "app"
	in.Dir = in.Root + "/main"
	long := strings.Repeat("v", 200)
	in.Samples[0].Entries = append(in.Samples[0].Entries, sample.Entry{Name: "LONG_VALUE", Value: long})
	for _, w := range []int{40, 60, 80, 100, 120} {
		for _, keys := range [][]string{{}, {"enter"}, {"enter", "enter"}, {"enter", "enter", "j", "j", "j"}, {"enter", "enter", "enter"}} {
			s := setupKeys(newSetupAt(in, w, 24), keys...)
			v := s.View()
			for i, l := range strings.Split(v, "\n") {
				if lw := lipgloss.Width(l); lw > w {
					t.Errorf("w=%d %v: line %d is %d wide: %q", w, keys, i, lw, l)
				}
			}
			if len(keys) == 5 {
				flat := strings.NewReplacer("│", "", " ", "", "\n", "").Replace(v)
				if !strings.Contains(flat, long) {
					t.Errorf("w=%d: the long value is cut:\n%s", w, v)
				}
			}
		}
	}
}

func TestSetupFooterListsOnlyKeysThatAct(t *testing.T) {
	cases := []struct {
		keys []string
		want string
	}{
		{nil, "enter tab esc"},
		{[]string{"enter"}, "enter j/k esc"},
		{[]string{"enter", "enter"}, "enter space e d esc"},
		{[]string{"enter", "enter", "j"}, "enter space d esc"}, // a secret row: no e
		{[]string{"enter", "enter", "enter"}, "enter esc"},
	}
	for _, c := range cases {
		got := strings.Join(setupKeys(newSetupAt(setupInput(), 100, 30), c.keys...).FooterKeys(), " ")
		if got != c.want {
			t.Errorf("%v: footer = %q, want %q", c.keys, got, c.want)
		}
	}
}

// At 40 columns the review's secret note wraps rather than being cut, and
// a seed path that wraps keeps its gap before the variable count.
func TestSetupWrapsNotesAndPathsAtFortyColumns(t *testing.T) {
	in := setupInput()
	in.Samples = append(in.Samples, manage.SetupSample{Path: "config/web.json.example", Entries: in.Samples[0].Entries[:1]})
	seed := setupKeys(newSetupAt(in, 40, 24), "enter").View()
	gap := regexp.MustCompile(`\S  +\d+ variables?`)
	for _, l := range strings.Split(seed, "\n") {
		if regexp.MustCompile(`\d+ variables?`).MatchString(l) && !gap.MatchString(l) {
			t.Errorf("a seed row runs into its count: %q\n%s", l, seed)
		}
	}
	review := setupKeys(newSetupAt(in, 40, 24), "enter", "enter").View()
	if flat := strings.NewReplacer("│", "", " ", "", "\n", "").Replace(review); !strings.Contains(flat, "you'llstorethisnext") {
		t.Errorf("the secret's note is cut:\n%s", review)
	}
}

func lastLine(v string) string {
	lines := strings.Split(v, "\n")
	return lines[len(lines)-1]
}

// A footer too wide for the terminal drops whole hints from the right,
// keeping the first and the last; it never cuts one mid-word. Each narrow
// part must be a whole part of the same screen's footer at 120 columns.
func TestNarrowFootersDropWholeHints(t *testing.T) {
	model := func(w int) string {
		return lastLine(sized(manage.New(fixture(), (&fakeDeps{}).deps(), noTheme()), w, 24).View())
	}
	cases := []struct {
		name        string
		at          func(w int) string
		first, last string
	}{
		{"setup project", func(w int) string { return lastLine(newSetupAt(setupInput(), w, 24).View()) }, "enter next", "esc cancel"},
		{"setup review", func(w int) string {
			return lastLine(setupKeys(newSetupAt(setupInput(), w, 24), "enter", "enter").View())
		}, "enter continue", "esc back"},
		{"model variables", model, "enter open", "? keys"},
	}
	for _, c := range cases {
		narrow, wide := c.at(40), c.at(120)
		if !strings.HasPrefix(strings.TrimSpace(narrow), c.first) || !strings.HasSuffix(narrow, c.last) {
			t.Errorf("%s: footer %q should start with %q and end with %q", c.name, narrow, c.first, c.last)
		}
		whole := strings.Split(strings.TrimSpace(wide), "   ")
		for _, part := range strings.Split(strings.TrimSpace(narrow), "   ") {
			if !slices.Contains(whole, part) {
				t.Errorf("%s: footer part %q is not a whole hint of %q", c.name, part, wide)
			}
		}
	}
}

func flatten(v string) string { return strings.NewReplacer("│", "", " ", "", "\n", "").Replace(v) }

// The summary is where enter writes, so every line of it is reachable on a
// short terminal: j scrolls it, and the footer offers j/k only when it
// overflows.
func TestSetupSummaryScrollsToEveryLine(t *testing.T) {
	fresh := setupInput()
	fresh.Root = "/home/user/workspace/personal/a-long-project"
	fresh.Dir = fresh.Root + "/main"
	fresh.Samples[0].Ignored = false
	imp := importInput()
	imp.Samples[0].Ignored = false
	cases := []struct {
		name string
		in   manage.SetupInput
		h    int
		keys []string
		want []string
	}{
		{"new", fresh, 14, []string{"enter", "enter", "enter"},
			[]string{"project", "app", "root", fresh.Root, "manifest", "/cfg/projects/app.toml", "output", "local -> .env",
				"render will refuse until it does", "variables", "you'll store the secrets next"}},
		{"import", imp, 16, []string{"enter"},
			[]string{"manifest", "output", "render will refuse until it does", "variables", "conflicts", "re-run with --force to overwrite"}},
	}
	for _, c := range cases {
		s := setupKeys(newSetupAt(c.in, 40, c.h), c.keys...)
		if !slices.Contains(s.FooterKeys(), "j/k") {
			t.Errorf("%s: an overflowing summary should offer j/k: %v", c.name, s.FooterKeys())
		}
		seen := ""
		for range 30 {
			seen += s.View() + "\n"
			s = setupKeys(s, "j")
		}
		for _, w := range c.want {
			if !strings.Contains(flatten(seen), flatten(w)) {
				t.Errorf("%s at 40x%d: %q is never on screen:\n%s", c.name, c.h, w, s.View())
			}
		}
	}
}

// Wrapping breaks at spaces, so "--" and "--force" are never split across
// lines.
func TestSetupNeverSplitsADashAcrossLines(t *testing.T) {
	fresh := setupInput()
	fresh.Samples[0].Ignored = false
	imp := importInput()
	old := "y"
	imp.Existing["DATABASE_URL"] = manifest.Var{Value: &old}
	views := map[string]func(w int) string{
		"summary": func(w int) string { return setupKeys(newSetupAt(fresh, w, 40), "enter", "enter", "enter").View() },
		"import":  func(w int) string { return setupKeys(newSetupAt(imp, w, 40), "enter", "enter").View() },
		"root": func(w int) string {
			return setupKeys(setupType(setupKeys(newSetupAt(fresh, w, 40), "tab"), "/elsewhere"), "enter").View()
		},
		"taken": func(w int) string { return setupType(newSetupAt(fresh, w, 40), "other").View() },
	}
	for name, view := range views {
		for w := 40; w <= 120; w++ {
			v := view(w)
			for _, l := range strings.Split(v, "\n") {
				l = strings.TrimSpace(strings.Trim(strings.TrimSpace(l), "│"))
				if strings.HasSuffix(l, "-") || strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "--force") {
					t.Errorf("%s at w=%d: a dash is split at a line break: %q\n%s", name, w, l, v)
				}
			}
		}
	}
}

// Ruling 4 uses the registry's own containment rule, so setup accepts only
// a root the project can then be found under: a sibling that shares a
// prefix ("/code/app-old") is not under "/code/app", and nothing below "/"
// is under "/", since Registry.Lookup would not find it there.
func TestSetupRootContainmentComparesPaths(t *testing.T) {
	in := setupInput()
	in.Root = "/"
	if s := setupKeys(newSetupAt(in, 100, 30), "enter"); !strings.Contains(s.View(), "this directory is not under /;") {
		t.Errorf("the root / would orphan the project and should be refused:\n%s", s.View())
	}
	in.Root, in.Dir = "/code/app", "/code/app-old/main"
	if s := setupKeys(newSetupAt(in, 100, 30), "enter"); !strings.Contains(s.View(), "this directory is not under /code/app") {
		t.Errorf("a prefix-sharing sibling is not under the root:\n%s", s.View())
	}
}

// The import header's path is a value: one that fits the header's two
// lines wraps whole, and the view keeps to the terminal's height.
func TestImportHeaderWrapsItsPath(t *testing.T) {
	in := importInput()
	in.Samples[0].Path = "config/" + strings.Repeat("nested/", 6) + "service.env.example"
	v := newSetupAt(in, 40, 24).View()
	if !strings.Contains(flatten(v), flatten("Import "+in.Samples[0].Path)) {
		t.Errorf("the header cuts the path:\n%s", v)
	}
	lines := strings.Split(v, "\n")
	if len(lines) != 24 {
		t.Errorf("view is %d lines, want 24:\n%s", len(lines), v)
	}
	for i, l := range lines {
		if lipgloss.Width(l) > 40 {
			t.Errorf("line %d is %d wide: %q", i, lipgloss.Width(l), l)
		}
	}
}

// NewSetup's import precondition is exactly one sample. Without one it
// must not panic: the review is empty and the result carries no entries.
func TestImportWithNoSampleDoesNotPanic(t *testing.T) {
	in := importInput()
	in.Samples = nil
	s := newSetupAt(in, 80, 24)
	_ = s.View()
	s = setupKeys(s, "space", "d", "e", "enter")
	_ = s.View()
	s = setupKeys(s, "enter")
	if res, ok := s.Result(); !ok || len(res.Entries) != 0 || res.Sample != -1 {
		t.Errorf("result = %+v %v", res, ok)
	}
}

func TestSetupFooterPerSelectionKind(t *testing.T) {
	forced := importInput()
	forced.Force = true
	cases := []struct {
		name    string
		in      manage.SetupInput
		keys    []string
		want    string
		escDesc string
	}{
		{"left-out row", setupInput(), []string{"enter", "enter", "d"}, "enter d esc", "esc back"},
		{"editing", setupInput(), []string{"enter", "enter", "e"}, "enter esc", "esc cancel edit"},
		{"import review", importInput(), nil, "enter space e d esc", "esc cancel"},
		{"import summary blocked", importInput(), []string{"enter"}, "esc", "esc back"},
		{"import summary forced", forced, []string{"enter"}, "enter esc", "esc back"},
	}
	for _, c := range cases {
		s := setupKeys(newSetupAt(c.in, 100, 30), c.keys...)
		if got := strings.Join(s.FooterKeys(), " "); got != c.want {
			t.Errorf("%s: footer = %q, want %q", c.name, got, c.want)
		}
		if !strings.HasSuffix(lastLine(s.View()), c.escDesc) {
			t.Errorf("%s: footer %q should end with %q", c.name, lastLine(s.View()), c.escDesc)
		}
	}
}

// Import names an existing project; its result's root is the caller's, not
// a cleaned empty field (".").
func TestImportResultKeepsTheCallersRoot(t *testing.T) {
	in := importInput()
	in.Force, in.Root = true, ""
	res, ok := setupKeys(newSetupAt(in, 100, 30), "enter", "enter").Result()
	if !ok || res.Root != "" || res.ID != "app" {
		t.Errorf("result = %+v %v", res, ok)
	}
}

func TestSetupProjectFocusMovesWithTabShiftTabAndArrows(t *testing.T) {
	for _, k := range []string{"tab", "shift+tab", "down", "up"} {
		s := setupKeys(setupType(setupKeys(newSetupAt(setupInput(), 100, 30), k), "/code/app2"), "enter")
		if !strings.Contains(s.View(), "this directory is not under /code/app2") {
			t.Errorf("%s should move focus to the root field:\n%s", k, s.View())
		}
	}
}

func TestSetupDBringsALeftOutRowBack(t *testing.T) {
	s := setupKeys(newSetupAt(setupInput(), 100, 30), "enter", "enter", "d")
	if !strings.Contains(s.View(), "x left out") {
		t.Fatalf("d should leave the row out:\n%s", s.View())
	}
	s = setupKeys(s, "d")
	if strings.Contains(s.View(), "x left out") {
		t.Errorf("d again should bring the row back:\n%s", s.View())
	}
	res, _ := setupKeys(s, "enter", "enter").Result()
	if len(res.Entries) != 3 || res.Entries[0].Name != "APP_MODE" {
		t.Errorf("entries = %+v", res.Entries)
	}
}

func TestSetupEscDropsAnEditAndStaysInTheReview(t *testing.T) {
	s := setupKeys(setupType(setupKeys(newSetupAt(setupInput(), 100, 30), "enter", "enter", "e"), "prod"), "esc")
	if s.Quitting() || !strings.Contains(s.View(), "development") || strings.Contains(s.View(), "prod") {
		t.Errorf("esc should drop the edit and keep the review:\n%s", s.View())
	}
	res, _ := setupKeys(s, "enter", "enter").Result()
	if res.Entries[0].Value != "development" {
		t.Errorf("entries = %+v", res.Entries)
	}
}

func TestSetupReenteringTheSameSampleKeepsTheReview(t *testing.T) {
	s := setupKeys(newSetupAt(setupInput(), 100, 30), "enter", "enter", "space", "esc")
	if !strings.Contains(s.View(), "Start empty") {
		t.Fatalf("esc from the review should return to the seed step:\n%s", s.View())
	}
	res, _ := setupKeys(s, "enter", "enter", "enter").Result()
	if len(res.Entries) == 0 || !res.Entries[0].GuessSecret {
		t.Errorf("the review's choices were lost on re-entering the sample: %+v", res.Entries)
	}
}

func TestSetupSummaryOutputLine(t *testing.T) {
	cases := map[string]struct {
		edit func(*manage.SetupSample)
		want string
	}{
		"reused":      {func(s *manage.SetupSample) { s.Reused = true }, "local already renders to .env"},
		"no output":   {func(s *manage.SetupSample) { s.NoOutput, s.OutputName = true, "" }, "output     none"},
		"not ignored": {func(s *manage.SetupSample) { s.Ignored = false }, "git does not ignore it; render will refuse until it does"},
	}
	for name, c := range cases {
		in := setupInput()
		c.edit(&in.Samples[0])
		v := setupKeys(newSetupAt(in, 100, 30), "enter", "enter", "enter").View()
		if !strings.Contains(v, c.want) {
			t.Errorf("%s: summary lacks %q:\n%s", name, c.want, v)
		}
		if name != "not ignored" && strings.Contains(v, "git ") {
			t.Errorf("%s: no git note expected:\n%s", name, v)
		}
	}
}

// Setup orders its hints by importance, enter first and esc last, so a
// narrow footer drops the others and keeps the way forward and back.
func TestSetupReviewFooterKeepsEnterAndEscAtFortyColumns(t *testing.T) {
	for _, keys := range [][]string{{"enter", "enter"}, {"enter", "enter", "j"}, {"enter", "enter", "d"}} {
		f := lastLine(setupKeys(newSetupAt(setupInput(), 40, 24), keys...).View())
		if !strings.HasPrefix(strings.TrimSpace(f), "enter continue") || !strings.HasSuffix(f, "esc back") {
			t.Errorf("%v: footer %q should keep enter continue and esc back", keys, f)
		}
	}
}

// A sample value guessed secret is never shown and never stored: unticking
// the row makes it plain with an empty value, for e to fill.
func TestUntickingAGuessedSecretDropsItsSampleValue(t *testing.T) {
	s := setupKeys(newSetupAt(setupInput(), 100, 30), "enter", "enter", "j", "space")
	v := s.View()
	if strings.Contains(v, "changeme") || !strings.Contains(v, "empty") {
		t.Errorf("an unticked secret should read empty and never show its sample value:\n%s", v)
	}
	if got := strings.Join(s.FooterKeys(), " "); got != "enter space e d esc" {
		t.Errorf("footer = %q; the unticked row should offer e", got)
	}
	s = setupKeys(s, "enter")
	if strings.Contains(s.View(), "changeme") {
		t.Errorf("the summary shows the sample value:\n%s", s.View())
	}
	res, ok := setupKeys(s, "enter").Result()
	if !ok || len(res.Entries) != 3 {
		t.Fatalf("result = %+v %v", res, ok)
	}
	if e := res.Entries[1]; e.Name != "API_TOKEN" || e.GuessSecret || e.Value != "" {
		t.Errorf("API_TOKEN = %+v, want plain with an empty value", e)
	}
}

// With no samples, the seed step shows the caller's note on why, wrapped
// rather than cut, above Start empty.
func TestSetupSeedShowsTheNoSamplesNote(t *testing.T) {
	note := "no samples here -- run trenvy from a worktree to seed from its samples"
	in := setupInput()
	in.Samples, in.NoSamplesNote = nil, note
	for _, w := range []int{40, 100} {
		v := setupKeys(newSetupAt(in, w, 24), "enter").View()
		if !strings.Contains(flatten(v), flatten(note)) || !strings.Contains(v, "Start empty") {
			t.Errorf("w=%d: the seed step lacks the note:\n%s", w, v)
		}
	}
	in.NoSamplesNote = ""
	if v := setupKeys(newSetupAt(in, 100, 24), "enter").View(); strings.Contains(v, "no samples here") {
		t.Errorf("no note was given, none should show:\n%s", v)
	}
}

// Import exits after it saves, so an import of nothing does not point at
// Variables, which it never opens.
func TestImportOfNothingSaysNothingWillBeImported(t *testing.T) {
	in := importInput()
	in.Force = true
	v := setupKeys(newSetupAt(in, 100, 30), "d", "j", "d", "j", "d", "enter").View()
	if !strings.Contains(v, "none; nothing will be imported") || strings.Contains(v, "Variables afterwards") {
		t.Errorf("summary:\n%s", v)
	}
}

// At the minimum size a very long import path must not push the view past
// the terminal: the header takes at most two lines, and a path that cannot
// fit them is cut with … at its front, keeping the file's name.
func TestImportViewFitsTheMinimumTerminalWithALongPath(t *testing.T) {
	in := importInput()
	in.Samples[0].Path = strings.Repeat("deep/", 60) + "service.env.example"
	// Two-cell runes at an odd width, where a line cannot be filled exactly.
	wide := importInput()
	wide.Samples[0].Path = strings.Repeat("設定", 90) + "/service.env.example"
	for _, c := range []struct {
		in   manage.SetupInput
		w    int
		keys []string
	}{{in, 40, nil}, {in, 40, []string{"enter"}}, {wide, 40, nil}, {wide, 41, nil}} {
		v := setupKeys(newSetupAt(c.in, c.w, 10), c.keys...).View()
		lines := strings.Split(v, "\n")
		if len(lines) > 10 {
			t.Errorf("w=%d %v: the view is %d lines on a 10-line terminal:\n%s", c.w, c.keys, len(lines), v)
		}
		for i, l := range lines {
			if lipgloss.Width(l) > c.w {
				t.Errorf("w=%d %v: line %d is %d wide: %q", c.w, c.keys, i, lipgloss.Width(l), l)
			}
		}
		if !strings.Contains(lines[2], "Review") {
			t.Errorf("w=%d %v: the header takes more than two lines:\n%s", c.w, c.keys, v)
		}
		if head := strings.Join(lines[:2], ""); !strings.Contains(head, "…") || !strings.Contains(flatten(head), "service.env.example") {
			t.Errorf("w=%d %v: the header should cut the path's front and keep its name:\n%s", c.w, c.keys, v)
		}
	}
}
