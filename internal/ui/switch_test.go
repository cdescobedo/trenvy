package ui_test

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/ui"
)

// switchFixture builds a minimal one-profile, one-variable matrix. The
// switcher tests below exercise `p` and the panel it opens, not the matrix
// itself, so the fixture only needs to be valid, not varied.
func switchFixture(t *testing.T) (*ui.Matrix, []ui.Problem) {
	t.Helper()
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev"}},
		Vars: map[string]manifest.Var{
			"ONE": {Value: s("v")},
		},
	}
	mx := ui.NewMatrix(m, nil)
	return mx, ui.Problems(mx, nil)
}

// switchParams builds Params directly, rather than through testParams,
// because every test in this file needs to control Project and Projects --
// exactly the two fields testParams hard-codes.
func switchParams(project string, projects []string, mx *ui.Matrix, ps []ui.Problem) ui.Params {
	return ui.Params{
		Project: project, Profile: "dev", Source: "default", Backend: "keychain",
		Matrix: mx, Problems: ps, Projects: projects,
	}
}

// p must list every registered project, mark which one is current, and not
// touch the underlying tab/browse state -- pressing it must open a panel,
// not silently do nothing.
func TestSwitcherListsRegisteredProjectsAndMarksCurrent(t *testing.T) {
	mx, ps := switchFixture(t)
	m := ui.New(switchParams("beta", []string{"alpha", "beta", "gamma"}, mx, ps))

	m, _ = m.Update(ui.KeyRune('p'))

	if m.Mode() != ui.ModeSwitchProject {
		t.Fatalf("mode after p = %v, want ModeSwitchProject", m.Mode())
	}
	v := m.View()
	for _, id := range []string{"alpha", "beta", "gamma"} {
		if !strings.Contains(v, id) {
			t.Errorf("switcher does not list project %q:\n%s", id, v)
		}
	}
	if !strings.Contains(v, "beta") || !strings.Contains(v, "current") {
		t.Errorf("switcher does not mark beta as the current project:\n%s", v)
	}
}

// Selecting a different registered project with nothing staged must end the
// session immediately -- SwitchTo reports the choice, and the cli host reads
// that, not Saved(), to decide to reload rather than commit.
func TestSwitcherSelectingADifferentProjectQuitsWithSwitchTo(t *testing.T) {
	mx, ps := switchFixture(t)
	m := ui.New(switchParams("alpha", []string{"alpha", "beta", "gamma"}, mx, ps))

	m, _ = m.Update(ui.KeyRune('p'))
	m, _ = m.Update(ui.KeyRune('j')) // alpha -> beta

	m, cmd := m.Update(ui.KeyType(tea.KeyEnter))

	id, ok := m.SwitchTo()
	if !ok || id != "beta" {
		t.Fatalf("SwitchTo() = %q, %v, want (%q, true)", id, ok, "beta")
	}
	if m.Saved() {
		t.Errorf("Saved() = true after switching, want false -- a switch must never be read as a write")
	}
	if cmd == nil {
		t.Fatal("selecting a project did not return a command to end the session")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("selecting a project's command = %T, want tea.Quit", cmd())
	}
}

// Selecting the project that is already open must not report a switch at
// all: there is nothing to rebuild, and a spurious SwitchTo would send the
// cli host through a pointless reload.
func TestSwitcherSelectingTheCurrentProjectIsANoOp(t *testing.T) {
	mx, ps := switchFixture(t)
	m := ui.New(switchParams("alpha", []string{"alpha", "beta"}, mx, ps))

	m, _ = m.Update(ui.KeyRune('p'))
	m, cmd := m.Update(ui.KeyType(tea.KeyEnter)) // cursor starts on "alpha", the current one

	if id, ok := m.SwitchTo(); ok {
		t.Errorf("SwitchTo() = %q, true; want no switch for selecting the current project", id)
	}
	if m.Mode() != ui.ModeBrowse {
		t.Errorf("mode after selecting the current project = %v, want ModeBrowse", m.Mode())
	}
	if cmd != nil {
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Error("selecting the current project quit the session; it should be a no-op")
		}
	}
}

// esc must back out of the switcher without choosing anything.
func TestSwitcherEscCancelsWithoutSwitching(t *testing.T) {
	mx, ps := switchFixture(t)
	m := ui.New(switchParams("alpha", []string{"alpha", "beta"}, mx, ps))

	m, _ = m.Update(ui.KeyRune('p'))
	m, _ = m.Update(ui.KeyType(tea.KeyEsc))

	if m.Mode() != ui.ModeBrowse {
		t.Errorf("mode after esc = %v, want ModeBrowse", m.Mode())
	}
	if _, ok := m.SwitchTo(); ok {
		t.Error("esc must not report a switch")
	}
}

// Switching with staged changes must confirm first: the changes exist only
// in this session's memory, and switching away loses them for good.
func TestSwitcherWithStagedChangesAsksForConfirmation(t *testing.T) {
	mx, ps := switchFixture(t)
	m := ui.New(switchParams("alpha", []string{"alpha", "beta"}, mx, ps))
	m, _ = m.Update(ui.KeyRune('2')) // Variables tab, so e actually opens
	m = editCell(m, "NEWVAL")        // stage a set-value on ONE

	m, _ = m.Update(ui.KeyRune('p'))
	m, _ = m.Update(ui.KeyRune('j')) // alpha -> beta
	m, cmd := m.Update(ui.KeyType(tea.KeyEnter))

	if m.Mode() != ui.ModeConfirmSwitch {
		t.Fatalf("mode after enter with staged changes = %v, want ModeConfirmSwitch", m.Mode())
	}
	if cmd != nil {
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Error("selecting a project with staged changes must not quit before confirming")
		}
	}
	if len(m.Staged()) != 1 {
		t.Errorf("Staged() = %+v, want the change to still be staged while awaiting confirmation", m.Staged())
	}
}

// This is the test the brief calls out by name: declining must leave BOTH
// the staged changes AND the current project untouched. The confirmation
// exists entirely to make that possible -- a decline that lost either would
// defeat the point of asking.
func TestSwitcherDecliningConfirmationLeavesStagedAndProjectUntouched(t *testing.T) {
	mx, ps := switchFixture(t)
	m := ui.New(switchParams("alpha", []string{"alpha", "beta"}, mx, ps))
	m, _ = m.Update(ui.KeyRune('2')) // Variables tab, so e actually opens
	m = editCell(m, "NEWVAL")
	staged := m.Staged()

	m, _ = m.Update(ui.KeyRune('p'))
	m, _ = m.Update(ui.KeyRune('j'))          // alpha -> beta
	m, _ = m.Update(ui.KeyType(tea.KeyEnter)) // now ModeConfirmSwitch

	m, cmd := m.Update(ui.KeyRune('n')) // decline

	if m.Mode() != ui.ModeBrowse {
		t.Errorf("mode after declining = %v, want ModeBrowse", m.Mode())
	}
	if id, ok := m.SwitchTo(); ok {
		t.Errorf("SwitchTo() = %q, true after declining; want no switch", id)
	}
	if got := m.Staged(); len(got) != len(staged) || !reflect.DeepEqual(got[0], staged[0]) {
		t.Errorf("Staged() = %+v after declining, want untouched %+v", got, staged)
	}
	if cmd != nil {
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Error("declining must not quit the session")
		}
	}
}

// Agreeing must destroy the staged changes -- not carry them into the new
// project's session, which would be silent data corruption if the new
// project happens to share a variable name -- and report the switch.
func TestSwitcherConfirmingSwitchDestroysStagedChangesAndReportsTheChoice(t *testing.T) {
	mx, ps := switchFixture(t)
	m := ui.New(switchParams("alpha", []string{"alpha", "beta"}, mx, ps))
	m, _ = m.Update(ui.KeyRune('2')) // Variables tab, so e actually opens
	m = editCell(m, "NEWVAL")

	m, _ = m.Update(ui.KeyRune('p'))
	m, _ = m.Update(ui.KeyRune('j'))          // alpha -> beta
	m, _ = m.Update(ui.KeyType(tea.KeyEnter)) // ModeConfirmSwitch

	m, cmd := m.Update(ui.KeyRune('y'))

	id, ok := m.SwitchTo()
	if !ok || id != "beta" {
		t.Fatalf("SwitchTo() = %q, %v, want (%q, true)", id, ok, "beta")
	}
	if len(m.Staged()) != 0 {
		t.Errorf("Staged() = %+v after confirming the switch, want empty", m.Staged())
	}
	if cmd == nil || func() bool { _, ok := cmd().(tea.QuitMsg); return !ok }() {
		t.Error("confirming the switch must quit the session")
	}
}

// A registry of exactly one project -- the current one -- is the common
// case for a new user. p must not panic and must not render an empty panel;
// it says plainly that there is nothing to switch to.
func TestSwitcherWithOneProjectSaysThereIsNothingToSwitchTo(t *testing.T) {
	mx, ps := switchFixture(t)
	m := ui.New(switchParams("alpha", []string{"alpha"}, mx, ps))

	m, _ = m.Update(ui.KeyRune('p'))
	v := m.View()

	if !strings.Contains(strings.ToLower(v), "nothing to switch") {
		t.Errorf("switcher with one project does not say there is nothing to switch to:\n%s", v)
	}

	// Selecting anyway (enter with the only row) must not panic and must not
	// report a switch, since the only project in the list is the current one.
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	if id, ok := m.SwitchTo(); ok {
		t.Errorf("SwitchTo() = %q, true with only one project registered; want no switch", id)
	}
}

// Neither the switcher panel nor its help line may offer register or
// remove: those stay on `trenvy project` as the user's own decision.
func TestSwitcherOffersNeitherRegisterNorRemove(t *testing.T) {
	mx, ps := switchFixture(t)
	m := ui.New(switchParams("alpha", []string{"alpha", "beta"}, mx, ps))

	m, _ = m.Update(ui.KeyRune('p'))
	v := strings.ToLower(m.View())

	if strings.Contains(v, "regist") {
		t.Errorf("switcher panel mentions registering a project:\n%s", v)
	}
	if strings.Contains(v, "remov") || strings.Contains(v, " rm") {
		t.Errorf("switcher panel mentions removing a project:\n%s", v)
	}
}
