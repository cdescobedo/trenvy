package manage_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
)

func TestTabRowShowsBothScreensAndKeysSwitch(t *testing.T) {
	m := newModel(&fakeDeps{})
	tabs := strings.Split(m.View(), "\n")[1]
	if !strings.Contains(tabs, "Variables") || !strings.Contains(tabs, "Outputs") || strings.Contains(tabs, "Profiles") {
		t.Errorf("tab row = %q", tabs)
	}
	// "not rendered" is web's state word in the Outputs list; no
	// Variables line carries it.
	for _, k := range []string{"2", "tab"} {
		v := press(newModel(&fakeDeps{}), k).View()
		if !strings.Contains(v, "not rendered") {
			t.Errorf("%s did not open Outputs:\n%s", k, v)
		}
	}
	// DATABASE_URL is in the Outputs grid too, so the state word must go.
	if v := press(newModel(&fakeDeps{}), "2", "1").View(); !strings.Contains(v, "DATABASE_URL") || strings.Contains(v, "not rendered") {
		t.Errorf("1 did not return to Variables:\n%s", v)
	}
	// Without colour the active tab is bracketed, so it still reads.
	if tabs := strings.Split(press(newModel(&fakeDeps{}), "2").View(), "\n")[1]; !strings.Contains(tabs, "[Outputs]") || strings.Contains(tabs, "[Variables]") {
		t.Errorf("active tab not marked without colour: %q", tabs)
	}
}

func TestOutputsListShowsDefaultAndState(t *testing.T) {
	v := press(newModel(&fakeDeps{}), "2").View()
	local := lineWith(t, v, "local")
	if !strings.Contains(local, "default") || !strings.Contains(local, "fresh") {
		t.Errorf("local row = %q", local)
	}
	if !strings.Contains(lineWith(t, v, "web "), "not rendered") {
		t.Errorf("web row lacks its state:\n%s", v)
	}
}

// Spec §4: state keeps status's word plus a gloss; quick mode's skip reads
// "unverified", never "unknown (--quick)".
func TestOutputStateWordsAndGlosses(t *testing.T) {
	snap := fixture()
	snap.OutputStates["local"] = session.StateUnverified
	m := sized(manage.New(snap, (&fakeDeps{}).deps(), noTheme()), 100, 30)
	v := press(m, "2").View()
	if strings.Contains(v, "--quick") || !strings.Contains(v, "unverified") || !strings.Contains(v, "not checked") {
		t.Errorf("view:\n%s", v)
	}
}

// The cursor row and the others share one column grid: the cursor column
// takes the same two cells whether it holds > or not.
func TestOutputsListColumnsAlign(t *testing.T) {
	v := press(newModel(&fakeDeps{}), "2").View()
	local, web := lineWith(t, v, "fresh"), lineWith(t, v, "not rendered")
	if a, b := strings.Index(local, "local"), strings.Index(web, "web"); a != b {
		t.Errorf("names start at %d and %d:\n%s\n%s", a, b, local, web)
	}
	if a, b := strings.Index(local, "fresh"), strings.Index(web, "not rendered"); a != b {
		t.Errorf("states start at %d and %d:\n%s\n%s", a, b, local, web)
	}
}

func TestOutputDetailShowsFactsModeAndMembership(t *testing.T) {
	v := press(newModel(&fakeDeps{}), "2", "j").View() // web
	for _, want := range []string{"format", "json", "path", "web.json", "new variables", "not included", "[x] PUBLIC_URL", "[ ] DATABASE_URL"} {
		if !strings.Contains(v, want) {
			t.Errorf("web detail lacks %q:\n%s", want, v)
		}
	}
	local := press(newModel(&fakeDeps{}), "2").View()
	if !strings.Contains(local, "included automatically") {
		t.Errorf("local detail lacks the everything-except sentence:\n%s", local)
	}
}

func TestOutputsNavigation(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "2", "enter", "j", "j")
	if !strings.Contains(lineWith(t, m.View(), "API_TOKEN"), ">") {
		t.Errorf("the detail cursor is not on the first variable row:\n%s", m.View())
	}
	if v := press(m, "left").View(); !strings.Contains(lineWith(t, v, "local"), ">") {
		t.Errorf("left did not return to the list:\n%s", v)
	}
}

func TestOutputsEmptyState(t *testing.T) {
	mf := fixtureManifest()
	mf.Outputs = map[string]manifest.Output{}
	mf.Project.Output = ""
	snap := snapshotOf(mf, nil)
	snap.DefaultOutput, snap.OutputStates = "", map[string]string{}
	m := sized(manage.New(snap, (&fakeDeps{}).deps(), noTheme()), 100, 30)
	m = press(m, "2", "j", "enter", "left", "d", "u", ".", "esc")
	if !strings.Contains(m.View(), "No outputs yet") {
		t.Errorf("view:\n%s", m.View())
	}
}

// Review Focus 5.
func TestOutputsLayoutNeverExceedsTheTerminal(t *testing.T) {
	mf := fixtureManifest()
	mf.Outputs["a-very-long-output-name-for-layout"] = manifest.Output{Format: manifest.FormatDotenv, Path: strings.Repeat("deep/", 20) + ".env"}
	for i := 0; i < 60; i++ {
		mf.Vars["VAR_"+strings.Repeat("X", i%17)+string(rune('A'+i%26))] = manifestVar("v")
	}
	for _, w := range []int{40, 79, 80, 120} {
		for _, keys := range [][]string{{"2"}, {"2", "enter"}, {"2", "enter", "j", "j", "j", "j", "j"}} {
			snap := snapshotOf(mf, nil)
			m := press(sized(manage.New(snap, (&fakeDeps{}).deps(), asciiTheme()), w, 24), keys...)
			lines := strings.Split(m.View(), "\n")
			if len(lines) > 24 {
				t.Errorf("w=%d %v: %d lines", w, keys, len(lines))
			}
			for i, l := range lines {
				if lw := lipgloss.Width(l); lw > w {
					t.Errorf("w=%d %v: line %d is %d wide: %q", w, keys, i, lw, l)
				}
			}
		}
	}
}

// TestOutputsLayoutNeverExceedsTheTerminal cannot see a cut: Theme.Pane
// cuts every body line to fit, so a value drawn on one line still passes
// it. Here the whole path must be on screen once wrapping is undone.
func TestOutputValuesWrapRatherThanCut(t *testing.T) {
	const long = "a-very-long-output-name-for-layout"
	path := strings.Repeat("deep/", 20) + ".env"
	mf := fixtureManifest()
	mf.Outputs[long] = manifest.Output{Format: manifest.FormatDotenv, Path: path}
	ids := make([]string, 0, len(mf.Outputs))
	for id := range mf.Outputs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	keys := []string{"2"}
	for _, id := range ids {
		if id == long {
			break
		}
		keys = append(keys, "j")
	}
	keys = append(keys, "enter")
	for _, w := range []int{40, 80} {
		m := press(sized(manage.New(snapshotOf(mf, nil), (&fakeDeps{}).deps(), asciiTheme()), w, 24), keys...)
		// Keep only the detail pane: at 80 columns it is right of the list.
		var detail []string
		for _, l := range strings.Split(m.View(), "\n") {
			if i := strings.LastIndex(l, "││"); i >= 0 {
				l = l[i:]
			}
			detail = append(detail, l)
		}
		flat := strings.NewReplacer("│", "", " ", "", "\n", "").Replace(strings.Join(detail, "\n"))
		if !strings.Contains(flat, path) {
			t.Errorf("w=%d: the full path is not on screen once wrapping is undone:\n%s", w, m.View())
		}
	}
}

// Switching screens keeps each screen's cursor where it was, whichever
// switch key is used.
func TestScreensKeepTheirCursorsAcrossASwitch(t *testing.T) {
	v := press(newModel(&fakeDeps{}), "j", "2", "shift+tab").View() // APP_MODE
	if !strings.Contains(v, "> APP_MODE") {
		t.Errorf("Variables lost its cursor:\n%s", v)
	}
	v = press(newModel(&fakeDeps{}), "2", "j", "1", "tab").View() // web
	if !strings.Contains(v, "> web") {
		t.Errorf("Outputs lost its cursor:\n%s", v)
	}
	if v := press(newModel(&fakeDeps{}), "?").View(); !strings.Contains(v, "shift+tab") {
		t.Errorf("? does not list shift+tab:\n%s", v)
	}
}

// A reload keeps the Outputs cursor on the same output by identity, even
// when an output added on disk sorts ahead of it.
func TestReloadKeepsTheOutputsCursor(t *testing.T) {
	mf := fixtureManifest()
	mf.Outputs["api"] = manifest.Output{Format: manifest.FormatDotenv, Path: "api.env"}
	next := snapshotOf(mf, map[string]map[string]bool{"API_TOKEN": {"dev": true, "prod": true}})
	f := &fakeDeps{next: &next}
	m := press(newModel(f), "d", "2", "j", "w", "enter") // stage, Outputs, web, save
	if f.reloads != 1 {
		t.Fatalf("reloads = %d, want 1", f.reloads)
	}
	if !strings.Contains(lineWith(t, m.View(), "not rendered"), ">") {
		t.Errorf("the cursor left web after the reload:\n%s", m.View())
	}
}
