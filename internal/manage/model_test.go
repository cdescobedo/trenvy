package manage_test

import (
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/style"
)

func TestHeaderNamesProjectProfileAndSource(t *testing.T) {
	v := newModel(&fakeDeps{}).View()
	first := strings.SplitN(v, "\n", 2)[0]
	for _, want := range []string{"app", "dev", "(default)", "1 problem"} {
		if !strings.Contains(first, want) {
			t.Errorf("header %q lacks %q", first, want)
		}
	}
	if strings.Contains(first, "unsaved") {
		t.Errorf("header %q shows an unsaved count with nothing staged", first)
	}
}

// Spec §1: Overview is gone; phase 1 has one screen, and the tab row
// claims no screen that does not exist yet.
func TestTabRowShowsOnlyBuiltScreens(t *testing.T) {
	second := strings.Split(newModel(&fakeDeps{}).View(), "\n")[1]
	if !strings.Contains(second, "Variables") || strings.Contains(second, "Overview") || strings.Contains(second, "Outputs") {
		t.Errorf("tab row = %q", second)
	}
}

func TestListShowsEveryVariableWithMarks(t *testing.T) {
	v := newModel(&fakeDeps{}).View()
	for _, want := range []string{"API_TOKEN", "APP_MODE", "DATABASE_URL", "LOG_LEVEL", "secret"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if !strings.Contains(lineWith(t, v, "API_TOKEN"), "!") {
		t.Errorf("API_TOKEN's row has no problem mark: %q", lineWith(t, v, "API_TOKEN"))
	}
}

// Spec §2: the detail pane shows every profile's effective value, not a
// state word, with provenance beside it.
func TestDetailShowsValuesAndProvenance(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j") // APP_MODE
	v := m.View()
	for _, want := range []string{"shared", "development", "from shared", "production", "override", "written to", "local"} {
		if !strings.Contains(v, want) {
			t.Errorf("detail lacks %q:\n%s", want, v)
		}
	}
	if !strings.Contains(lineWith(t, v, "staging"), "development") {
		t.Errorf("staging row does not show the inherited value: %q", lineWith(t, v, "staging"))
	}
}

func TestSecretDetailMasksAndFlagsMissing(t *testing.T) {
	v := newModel(&fakeDeps{}).View() // API_TOKEN selected
	if !strings.Contains(lineWith(t, v, "staging"), "MISSING") {
		t.Errorf("staging row = %q, want MISSING", lineWith(t, v, "staging"))
	}
	if !strings.Contains(lineWith(t, v, "prod"), "••••••••") || !strings.Contains(lineWith(t, v, "prod"), "stored") {
		t.Errorf("prod row = %q, want masked and stored", lineWith(t, v, "prod"))
	}
	if !strings.Contains(v, "no value under staging") {
		t.Errorf("detail lacks the in-app problem line:\n%s", v)
	}
}

func TestOptionalVariableReadsOptional(t *testing.T) {
	v := press(newModel(&fakeDeps{}), "j", "j", "j").View() // LOG_LEVEL
	if !strings.Contains(v, "optional") {
		t.Errorf("LOG_LEVEL detail does not say optional:\n%s", v)
	}
}

// Review Focus 2.
func TestControlCharactersInValuesAreDrawnVisibly(t *testing.T) {
	mf := fixtureManifest()
	mf.Vars["APP_MODE"] = manifestVar("red\x1b[31mtext")
	m := sized(manage.New(snapshotOf(mf, nil), (&fakeDeps{}).deps(), style.Theme{}), 100, 30)
	v := press(m, "j").View()
	if strings.Contains(v, "\x1b") {
		t.Fatalf("a raw ESC reached the screen: %q", v)
	}
	if !strings.Contains(v, `red\x1b[31mtext`) {
		t.Errorf("the escape is not drawn visibly:\n%s", v)
	}
}

// Review Focus 1.
func TestTinyTerminalSaysSoInsteadOfDrawing(t *testing.T) {
	for _, size := range [][2]int{{39, 30}, {100, 9}, {1, 1}} {
		v := sized(newModel(&fakeDeps{}), size[0], size[1]).View()
		if !strings.Contains(v, "at least 40×10") || strings.Contains(v, "╭") {
			t.Errorf("%dx%d view = %q", size[0], size[1], v)
		}
	}
}

func TestQuitWithNothingStagedQuits(t *testing.T) {
	if m := press(newModel(&fakeDeps{}), "q"); !m.Quitting() {
		t.Error("q with nothing staged did not quit")
	}
}

func TestCtrlCQuitsFromAnywhere(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "?", "ctrl+c")
	if !m.Quitting() {
		t.Error("ctrl+c inside an overlay did not quit")
	}
}

func TestHelpListsTheVerbsAndEscCloses(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "?")
	if v := m.View(); !strings.Contains(v, "undo the staged change") || !strings.Contains(v, "review and save") {
		t.Errorf("help lacks the verbs:\n%s", v)
	}
	if v := press(m, "esc").View(); strings.Contains(v, "review and save") {
		t.Error("esc did not close help")
	}
}

func lineWith(t *testing.T, v, sub string) string {
	t.Helper()
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, sub) {
			return l
		}
	}
	t.Fatalf("no line contains %q:\n%s", sub, v)
	return ""
}

// cursorLines is every screen line holding the cursor glyph.
func cursorLines(v string) []string {
	var out []string
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, ">") {
			out = append(out, l)
		}
	}
	return out
}

// Global constraint "every state reads in words with no colour" and the
// spec §1 mock: the selection and the focused pane read without colour.
func TestCursorAndPaneFocusShowWithoutColour(t *testing.T) {
	m := newModel(&fakeDeps{})
	if v := m.View(); !strings.Contains(v, "> API_TOKEN") {
		t.Errorf("list cursor not drawn before the selected name:\n%s", v)
	}
	if v := press(m, "j").View(); !strings.Contains(v, "> APP_MODE") || strings.Contains(v, "> API_TOKEN") {
		t.Errorf("list cursor did not move with j:\n%s", v)
	}

	m = press(m, "enter") // detail focused, on dev
	v := m.View()
	if strings.Contains(v, "> API_TOKEN") || !strings.Contains(v, "· API_TOKEN") {
		t.Errorf("with the detail focused the list row should show · rather than >:\n%s", v)
	}
	if cl := cursorLines(v); len(cl) != 1 || !strings.Contains(cl[0], "dev") {
		t.Errorf("want one detail cursor line, on dev; got %q", cl)
	}
	if cl := cursorLines(press(m, "j").View()); len(cl) != 1 || !strings.Contains(cl[0], "staging") {
		t.Errorf("want one detail cursor line, on staging; got %q", cl)
	}
}

// Spec §2: * marks the active profile -- including while it is selected.
func TestActiveProfileStarSurvivesSelection(t *testing.T) {
	v := press(newModel(&fakeDeps{}), "enter").View() // detail focused on dev
	cl := cursorLines(v)
	if len(cl) != 1 || !strings.Contains(cl[0], "dev") || !strings.Contains(cl[0], "*") {
		t.Errorf("selected active-profile row = %q, want > and *", cl)
	}
}

// Spec §1: the ? key list wraps and never truncates, so when it is taller
// than the terminal it scrolls and says there is more.
func TestHelpScrollsWhenTallerThanTheTerminal(t *testing.T) {
	m := press(sized(newModel(&fakeDeps{}), 100, 16), "?")
	v := m.View()
	if strings.Contains(v, "this list") || !strings.Contains(v, "↓ more") {
		t.Fatalf("a cut help list should end in ↓ more:\n%s", v)
	}
	if !strings.Contains(strings.Join(m.FooterKeys(), " "), "j") {
		t.Errorf("help footer %q does not offer scrolling", m.FooterKeys())
	}
	for range 20 {
		m = press(m, "j")
	}
	v = m.View()
	if !strings.Contains(v, "this list") || !strings.Contains(v, "↑ more") || strings.Contains(v, "↓ more") {
		t.Errorf("scrolled to the end, help should show its last verb and ↑ more:\n%s", v)
	}
	if v = press(m, "k").View(); !strings.Contains(v, "↓ more") {
		t.Errorf("k did not scroll back up:\n%s", v)
	}
}

// At the minimum size the help panel has room for two rows; every scroll
// position must still show a verb, not only the two indicators.
func TestHelpAtTheMinimumSizeAlwaysShowsAVerb(t *testing.T) {
	m := press(sized(newModel(&fakeDeps{}), 40, 10), "?")
	for i := 0; i < 20; i++ {
		v := m.View()
		if strings.Contains(v, "↑ more") && strings.Contains(v, "↓ more") {
			t.Fatalf("after %d j: only indicators, no verb:\n%s", i, v)
		}
		m = press(m, "j")
	}
}

// The cursor column must not push the detail pane's text against its
// right border: the meta keeps a one-cell gap.
func TestDetailKeepsAGapBeforeTheRightBorder(t *testing.T) {
	for _, w := range []int{60, 100} {
		v := press(sized(newModel(&fakeDeps{}), w, 30), "enter").View()
		if l := lineWith(t, v, "secret · required"); !strings.Contains(l, "required │") {
			t.Errorf("w=%d: detail header %q has no gap before the border", w, l)
		}
	}
}

// Without colour the . menu's cursor row must still be told apart.
func TestMenuCursorShowsWithoutColour(t *testing.T) {
	m := press(newModel(&fakeDeps{}), ".")
	v := m.View()
	if !strings.Contains(lineWith(t, v, "Make secret"), "> Make secret") || strings.Contains(lineWith(t, v, "Mark optional"), ">") {
		t.Errorf("menu cursor not on Make secret:\n%s", v)
	}
	v = press(m, "j").View()
	if !strings.Contains(lineWith(t, v, "Mark optional"), "> Mark optional") || strings.Contains(lineWith(t, v, "Make secret"), ">") {
		t.Errorf("menu cursor did not move to Mark optional:\n%s", v)
	}
}

func TestSwitchProjectCursorShowsWithoutColour(t *testing.T) {
	snap := fixture()
	snap.Projects = []string{"app", "other", "third"}
	m := press(sized(manage.New(snap, (&fakeDeps{}).deps(), style.Theme{}), 100, 30), ".", "j", "j", "j", "j", "enter")
	v := m.View()
	if !strings.Contains(lineWith(t, v, "other"), "> other") || strings.Contains(lineWith(t, v, "third"), ">") {
		t.Errorf("switch cursor not on other:\n%s", v)
	}
	v = press(m, "j").View()
	if !strings.Contains(lineWith(t, v, "third"), "> third") || strings.Contains(lineWith(t, v, "other"), ">") {
		t.Errorf("switch cursor did not move to third:\n%s", v)
	}
}
