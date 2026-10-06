package manage_test

import (
	"regexp"
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
			if len(keys) == 5 && w >= 60 {
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
		{[]string{"enter", "enter"}, "space e d enter esc"},
		{[]string{"enter", "enter", "j"}, "space d enter esc"}, // a secret row: no e
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
