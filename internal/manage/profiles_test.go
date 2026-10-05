package manage_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/manifest"
)

func TestThreeOpensProfilesAndTabCyclesAllThree(t *testing.T) {
	v := press(newModel(&fakeDeps{}), "3").View()
	if tabs := strings.Split(v, "\n")[1]; !strings.Contains(tabs, "[Profiles]") {
		t.Errorf("3 did not open Profiles: %q", tabs)
	}
	for _, keys := range [][]string{{"tab", "tab"}, {"shift+tab"}} {
		if tabs := strings.Split(press(newModel(&fakeDeps{}), keys...).View(), "\n")[1]; !strings.Contains(tabs, "[Profiles]") {
			t.Errorf("%v did not reach Profiles: %q", keys, tabs)
		}
	}
	if tabs := strings.Split(press(newModel(&fakeDeps{}), "3", "tab").View(), "\n")[1]; !strings.Contains(tabs, "[Variables]") {
		t.Errorf("tab from Profiles did not wrap to Variables: %q", tabs)
	}
}

// fixture: dev is the default; API_TOKEN (secret) is stored under dev and
// prod, so staging is missing it. APP_MODE overrides prod.
func TestProfilesListShowsTagsMissingAndMarks(t *testing.T) {
	v := press(newModel(&fakeDeps{}), "3").View()
	if row := lineWith(t, v, "> dev"); !strings.Contains(row, "default") {
		t.Errorf("dev row = %q, want the default tag", row)
	}
	if row := lineWith(t, v, "staging"); !strings.Contains(row, "1 missing") || !strings.Contains(row, "!") {
		t.Errorf("staging row = %q, want 1 missing and !", row)
	}
}

func TestProfileDetailCountsOverridesSecretsAndMissing(t *testing.T) {
	v := press(newModel(&fakeDeps{}), "3", "j", "j").View() // prod
	for line, want := range map[string]string{
		"overrides": `overrides +1 +│`,
		"secrets":   `secrets +1 of 1 stored +│`,
		"none":      `missing +none +│`,
	} {
		if row := lineWith(t, v, line); !regexp.MustCompile(want).MatchString(row) {
			t.Errorf("prod detail line = %q, want %s:\n%s", row, want, v)
		}
	}
	v = press(newModel(&fakeDeps{}), "3", "j", "enter").View() // staging, detail focused
	// Ruling R2: the cursor, then the "missing" label, then the name.
	if !strings.Contains(v, "0 of 1 stored") || !strings.Contains(lineWith(t, v, "API_TOKEN"), ">") {
		t.Errorf("staging detail should list API_TOKEN as missing under the cursor:\n%s", v)
	}
}

// A variable staged for deletion is not missing anywhere: it will not
// exist after save.
func TestADeletedVariableIsNotMissing(t *testing.T) {
	v := press(newModel(&fakeDeps{}), "d", "3", "j").View() // delete API_TOKEN, then staging
	if strings.Contains(v, "1 missing") {
		t.Errorf("the list still counts API_TOKEN as missing:\n%s", v)
	}
	if row := lineWith(t, v, "missing"); !strings.Contains(row, "missing     none") {
		t.Errorf("staging's missing line = %q, want none:\n%s", row, v)
	}
}

func TestActiveTagFollowsTheSnapshot(t *testing.T) {
	snap := fixture()
	snap.Active = "prod"
	m := press(sized(manage.New(snap, (&fakeDeps{}).deps(), noTheme()), 100, 30), "3")
	if row := lineWith(t, m.View(), "prod"); !strings.Contains(row, "active") {
		t.Errorf("prod row = %q, want the active tag", row)
	}
}

func TestProfilesLayoutNeverExceedsTheTerminal(t *testing.T) {
	mf := fixtureManifest()
	mf.Project.Profiles = append(mf.Project.Profiles, "a-very-long-profile-name-for-layout-checks")
	for i := 0; i < 30; i++ {
		mf.Vars["REQUIRED_"+strings.Repeat("Z", i%19)+string(rune('A'+i%26))] = manifest.Var{Secret: true}
	}
	for _, w := range []int{40, 60, 79, 80, 100, 120} {
		for _, keys := range [][]string{{"3"}, {"3", "enter"}, {"3", "G"}, {"3", "j", "j", "j", "enter", "j", "j"}} {
			m := press(sized(manage.New(snapshotOf(mf, nil), (&fakeDeps{}).deps(), asciiTheme()), w, 24), keys...)
			v := m.View()
			lines := strings.Split(v, "\n")
			if len(lines) > 24 {
				t.Errorf("w=%d %v: %d lines", w, keys, len(lines))
			}
			for i, l := range lines {
				if lw := lipgloss.Width(l); lw > w {
					t.Errorf("w=%d %v: line %d is %d wide: %q", w, keys, i, lw, l)
				}
			}
			// The pane clips what overflows it, so a width check alone
			// cannot see a lost mark: the long name is whole or cut with
			// …, and its note and ! survive inside the border.
			if len(keys) == 1 {
				row := lineWith(t, v, "a-very-long-prof")
				whole := strings.Contains(row, "a-very-long-profile-name-for-layout-checks")
				if !whole && !strings.Contains(row, "…") || !regexp.MustCompile(`31 missing +!│`).MatchString(row) {
					t.Errorf("w=%d: long row = %q, want the name whole or cut with …, then 31 missing and !", w, row)
				}
			}
		}
	}
}

func TestReloadKeepsTheProfilesCursor(t *testing.T) {
	f := &fakeDeps{}
	m := press(newModel(f), "d", "3", "j", "j", "w", "enter") // stage on Variables, then prod, save
	if f.reloads != 1 {
		t.Fatalf("reloads = %d, want 1", f.reloads)
	}
	if !strings.Contains(m.View(), "> prod") {
		t.Errorf("the cursor left prod after the reload:\n%s", m.View())
	}
}
