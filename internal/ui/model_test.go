package ui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/ui"
)

// newTestModel builds a model over a two-profile, three-variable fixture:
// one shared, one overridden, one secret so the matrix has something under
// every Cell kind the later tabs will need to render.
func newTestModel(t *testing.T) ui.Model {
	t.Helper()
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev", "prod"}},
		Vars: map[string]manifest.Var{
			"SHARED":   {Value: s("v")},
			"OVERRIDE": {Value: s("a"), Profiles: map[string]string{"dev": "b"}},
			"TOKEN":    {Secret: true},
		},
	}
	mx := ui.NewMatrix(m, nil)
	ps := ui.Problems(mx, nil)
	return ui.New("demo", mx, ps)
}

func TestTabSwitchingByNumberAndTab(t *testing.T) {
	m := newTestModel(t)
	if m.Tab() != ui.TabOverview {
		t.Fatalf("opens on %v, want Overview", m.Tab())
	}
	m, _ = m.Update(ui.KeyRune('2'))
	if m.Tab() != ui.TabVariables {
		t.Errorf("after '2' = %v, want Variables", m.Tab())
	}
	m, _ = m.Update(ui.KeyType(tea.KeyTab))
	if m.Tab() != ui.TabProfiles {
		t.Errorf("after tab = %v, want Profiles", m.Tab())
	}
}

// tab from the last tab must return to the first rather than run off the end.
func TestTabWrapsAround(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('4'))
	if m.Tab() != ui.TabOutputs {
		t.Fatalf("after '4' = %v, want Outputs", m.Tab())
	}
	m, _ = m.Update(ui.KeyType(tea.KeyTab))
	if m.Tab() != ui.TabOverview {
		t.Errorf("tab from Outputs = %v, want it to wrap to Overview", m.Tab())
	}
}

// Profiles and Outputs are in the bar but inert in phase 1. They must say so
// and name the CLI that does the job, rather than showing an empty panel.
func TestInertTabsNameTheirCLI(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.Update(ui.KeyRune('3'))
	if v := m.View(); !strings.Contains(v, "trenvy profile") {
		t.Errorf("Profiles tab does not name its CLI:\n%s", v)
	}
	m, _ = m.Update(ui.KeyRune('4'))
	if v := m.View(); !strings.Contains(v, "trenvy output") {
		t.Errorf("Outputs tab does not name its CLI:\n%s", v)
	}
}

// The tab bar itself must list every tab name, so a user can see where '2',
// tab, and shift+tab will land before they press anything.
func TestTabBarListsAllTabs(t *testing.T) {
	m := newTestModel(t)
	v := m.View()
	for _, name := range []string{"Overview", "Variables", "Profiles", "Outputs"} {
		if !strings.Contains(v, name) {
			t.Errorf("tab bar missing %q:\n%s", name, v)
		}
	}
}
