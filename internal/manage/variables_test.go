package manage_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
	"github.com/cdescobedo/trenvy/internal/style"
)

func TestRevealFetchesOneCellAndHidesOnLeave(t *testing.T) {
	f := &fakeDeps{secrets: map[string]string{"API_TOKEN/prod": "tok-prod"}}
	m := press(newModel(f), "enter", "j", "j", "v") // API_TOKEN, detail, prod row
	if v := m.View(); !strings.Contains(v, "tok-prod") {
		t.Fatalf("v did not reveal prod:\n%s", v)
	}
	if strings.Contains(m.View(), "tok-dev") {
		t.Error("revealed a cell that was not asked for")
	}
	m = press(m, "left", "j", "k") // leave API_TOKEN and come back
	if strings.Contains(m.View(), "tok-prod") {
		t.Error("the revealed value survived leaving the variable")
	}
}

func TestVHidesARevealedCell(t *testing.T) {
	f := &fakeDeps{secrets: map[string]string{"API_TOKEN/dev": "tok-dev"}}
	m := press(newModel(f), "enter", "v", "v")
	if strings.Contains(m.View(), "tok-dev") {
		t.Error("second v did not hide the value")
	}
}

func TestFilterNarrowsLiveAndEscKeepsIt(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "/")
	m = typeText(m, "app")
	if v := m.View(); strings.Contains(v, "DATABASE_URL") || !strings.Contains(v, "APP_MODE") {
		t.Errorf("filter did not narrow live:\n%s", v)
	}
	if v := press(m, "esc").View(); strings.Contains(v, "DATABASE_URL") {
		t.Error("esc dropped the filter; it should only close the field")
	}
}

func TestBangShowsOnlyProblemRows(t *testing.T) {
	v := press(newModel(&fakeDeps{}), "!").View()
	if !strings.Contains(v, "API_TOKEN") || strings.Contains(v, "DATABASE_URL") {
		t.Errorf("! did not limit the list to problem rows:\n%s", v)
	}
}

// Review Focus 3.
func TestEmptyListNeverPanics(t *testing.T) {
	empty := snapshotOf(&manifest.Manifest{Project: manifest.Project{Profiles: []string{"dev"}}}, nil)
	m := sized(manage.New(empty, (&fakeDeps{}).deps(), style.Theme{}), 100, 30)
	for _, k := range []string{"j", "k", "enter", "left", "d", "u", "v", ".", "esc", "/", "esc", "!", "w", "esc"} {
		m = press(m, k)
	}
	if !strings.Contains(m.View(), "No variables yet") && !strings.Contains(m.View(), "Nothing matches") {
		t.Errorf("no empty state:\n%s", m.View())
	}
	m = press(newModel(&fakeDeps{}), "/")
	m = typeText(m, "zzz")
	m = press(m, "esc", "j", "enter", "d", "v")
	if !strings.Contains(m.View(), "Nothing matches") {
		t.Errorf("filter matching nothing has no empty state:\n%s", m.View())
	}
}

// Review Focus 5.
func TestListScrollsToKeepTheCursorVisible(t *testing.T) {
	mf := &manifest.Manifest{Project: manifest.Project{Profiles: []string{"dev"}}, Vars: map[string]manifest.Var{}}
	for i := 0; i < 200; i++ {
		mf.Vars[fmt.Sprintf("VAR_%03d", i)] = manifestVar("v")
	}
	m := sized(manage.New(snapshotOf(mf, nil), (&fakeDeps{}).deps(), style.Theme{}), 100, 24)
	for i := 0; i < 150; i++ {
		m = press(m, "j")
	}
	if !strings.Contains(m.View(), "VAR_150") {
		t.Errorf("cursor row VAR_150 is off screen:\n%s", m.View())
	}
}

// colourTheme is a theme that renders colour, for tests that must see
// styled output.
func colourTheme() style.Theme {
	r := lipgloss.NewRenderer(nil)
	r.SetColorProfile(termenv.TrueColor)
	r.SetHasDarkBackground(true)
	return style.NewTheme(r)
}

// The conditional hints appear exactly when they apply.
func TestConditionalHints(t *testing.T) {
	has := func(m manage.Model, k string) bool {
		for _, x := range m.FooterKeys() {
			if x == k {
				return true
			}
		}
		return false
	}
	m := newModel(&fakeDeps{})
	if has(m, "u") {
		t.Error("u shown with nothing staged")
	}
	if has(press(m, "enter", "j"), "v") { // staging row: nothing stored
		t.Error("v shown on a row with nothing stored")
	}
	if !has(press(m, "enter"), "v") { // dev row: stored
		t.Error("v missing on a stored row")
	}
}

// The cursor jumps to the first row when the filter changes; a revealed
// value must not follow it.
func TestFilteringOrTogglingProblemsHidesRevealedValues(t *testing.T) {
	f := &fakeDeps{secrets: map[string]string{"API_TOKEN/dev": "tok-dev"}}
	m := press(newModel(f), "enter", "v")
	if !strings.Contains(m.View(), "tok-dev") {
		t.Fatal("setup: not revealed")
	}
	// Filtering to a different variable and back lands on API_TOKEN again.
	m = typeText(press(m, "/"), "api")
	if strings.Contains(m.View(), "tok-dev") {
		t.Error("typing a filter left the value revealed")
	}
	m = press(newModel(f), "enter", "v", "!")
	if strings.Contains(m.View(), "tok-dev") {
		t.Error("! left the value revealed")
	}
}

// Spec §1: the footer lists only keys that do something for the selection
// right now. Each listed key must change the screen or return a command.
func TestEveryFooterKeyDoesSomething(t *testing.T) {
	states := map[string][]string{
		"list, secret":          nil,
		"list, plain":           {"j"},
		"detail, secret dev":    {"enter"},
		"detail, plain shared":  {"j", "enter"},
		"detail, plain prod":    {"j", "enter", "j", "j", "j"},
		"outputs list":          {"2"},
		"outputs detail format": {"2", "enter"},
		"outputs detail var":    {"2", "enter", "j", "j"},
	}
	for name, setup := range states {
		f := &fakeDeps{secrets: map[string]string{"API_TOKEN/dev": "d"}}
		base := press(sized(manage.New(fixture(), f.deps(), colourTheme()), 100, 30), setup...)
		for _, k := range base.FooterKeys() {
			if k == "?" {
				continue
			}
			next, cmd := base.Update(key(k))
			if cmd == nil && next.(manage.Model).View() == base.View() {
				t.Errorf("%s: footer key %q did nothing", name, k)
			}
		}
	}
}

// M5: a value staged for a missing cell answers its problem, so the
// detail stops showing it -- for a secret (set-secret), a plain override
// (set-value) and a shared value (set-shared). Others stay.
func TestAStagedValueAnswersItsMissingProblem(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "enter", "j", "enter") // API_TOKEN staging
	m = typeText(m, "s3cret")
	m = press(m, "enter")
	if v := m.View(); strings.Contains(v, "no value under staging") {
		t.Errorf("a stored-on-save secret still shows its problem:\n%s", v)
	}

	mf := fixtureManifest()
	mf.Vars["REQ"] = manifest.Var{Profiles: map[string]string{"dev": "x"}} // missing under staging and prod
	snap := snapshotOf(mf, map[string]map[string]bool{"API_TOKEN": {"dev": true, "prod": true}})
	m = sized(manage.New(snap, (&fakeDeps{}).deps(), noTheme()), 100, 30)
	for i := 0; i < 8 && !strings.Contains(lineWith(t, m.View(), "REQ"), ">"); i++ {
		m = press(m, "j")
	}
	m = press(m, "enter")
	for i := 0; i < 6 && !strings.Contains(lineWith(t, m.View(), "staging"), ">"); i++ {
		m = press(m, "j")
	}
	m = press(m, "enter")
	m = typeText(m, "y")
	m = press(m, "enter")
	v := m.View()
	if strings.Contains(v, "no value under staging") || !strings.Contains(v, "no value under prod") {
		t.Errorf("after a staging override, want only prod's problem:\n%s", v)
	}
	for i := 0; i < 6 && !strings.Contains(lineWith(t, m.View(), "shared"), ">"); i++ {
		m = press(m, "k")
	}
	m = press(m, "enter")
	m = typeText(m, "z")
	m = press(m, "enter")
	if v := m.View(); strings.Contains(v, "no value under") {
		t.Errorf("after a shared value, want no missing problem:\n%s", v)
	}
	if got := strings.Join(staged(m), ","); got != "set-value:REQ:staging:y,set-shared:REQ::z" {
		t.Errorf("staged = %s", got)
	}
}

func TestEditingACellOnARemovedProfileIsRefused(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "3", "j", "d", "1") // staging removed; back to Variables
	m = press(m, "j", "enter", "j", "j")                  // APP_MODE, staging row
	if row := lineWith(t, m.View(), "staging"); !strings.Contains(row, "x removed") {
		t.Errorf("staging row = %q, want x removed", row)
	}
	for _, k := range []string{"enter", "d"} {
		after := press(m, k)
		if len(after.Staged()) != 1 || !strings.Contains(after.View(), "staging is marked for removal") {
			t.Errorf("%s on a removed profile's cell: staged %d, view:\n%s", k, len(after.Staged()), after.View())
		}
	}
}

// Spec §1: enter and d are refused on a removed profile's row, so the
// footer does not offer them.
func TestFooterOnARemovedProfilesRowOffersNoEdit(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "3", "j", "d", "1", "j", "enter", "j", "j") // APP_MODE, staging row
	if got := m.FooterKeys(); strings.Join(got, " ") != "← . ?" {
		t.Errorf("footer = %v, want [← . ?]", got)
	}
}

func TestPromotionSkipsARemovedProfile(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "3", "j", "j", "d", "1") // prod removed
	m = press(m, "j", "j", ".", "enter", "enter")              // DATABASE_URL, Make secret, confirm
	var secrets []string
	for _, c := range m.Staged() {
		if c.Kind == session.ChangeSetSecret {
			secrets = append(secrets, c.Profile)
		}
	}
	if strings.Join(secrets, ",") != "dev,staging" {
		t.Errorf("set-secret profiles = %v, want dev,staging (prod is being removed)", secrets)
	}
}
