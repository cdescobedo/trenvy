package manage_test

import (
	"errors"
	"strings"
	"testing"
)

func TestRenderNowRunsAndReloads(t *testing.T) {
	f := &fakeDeps{rendered: map[string]string{"local": "wrote .env  (5 vars, 1 from the keyring, 0600)"}}
	m := out(newModel(f), ".")
	m = pickMenu(t, m, "Render now")
	if f.renders != 1 || f.reloads != 1 || !strings.Contains(m.View(), "wrote .env") {
		t.Errorf("renders %d reloads %d\n%s", f.renders, f.reloads, m.View())
	}
}

// Review Focus 3.
func TestRenderRefusalIsShownAndNothingIsWritten(t *testing.T) {
	f := &fakeDeps{renderErr: errors.New(".env is not ignored by git")}
	m := pickMenu(t, out(newModel(f), "."), "Render now")
	if !strings.Contains(m.View(), "not ignored by git") || m.Quitting() {
		t.Errorf("view:\n%s", m.View())
	}
	if f.reloads != 0 {
		t.Errorf("a refused render reloaded (%d)", f.reloads)
	}
}

func TestRemoveRenderedFileAsksFirst(t *testing.T) {
	f := &fakeDeps{cleaned: map[string]string{"local": "removed .env"}}
	m := pickMenu(t, out(newModel(f), "."), "Remove rendered file")
	if f.cleans != 0 || !strings.Contains(m.View(), "deleted") {
		t.Fatalf("removed without asking (%d)\n%s", f.cleans, m.View())
	}
	m = press(m, "enter")
	if f.cleans != 1 || !strings.Contains(m.View(), "removed .env") {
		t.Errorf("cleans %d\n%s", f.cleans, m.View())
	}
}

func TestCheckNowReplacesTheStateUntilReload(t *testing.T) {
	f := &fakeDeps{checks: map[string]string{"local": "STALE"}}
	m := pickMenu(t, out(newModel(f), "."), "Check now")
	if !strings.Contains(listRow(t, m.View(), "local"), "STALE") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestActionsAreDisabledWhileTheOutputHasStagedChanges(t *testing.T) {
	f := &fakeDeps{}
	m := out(newModel(f), "enter", "j", "j", "space", "left", ".") // stage a membership edit on local
	for _, item := range []string{"Render now", "Remove rendered file", "Check now"} {
		mm := pickMenu(t, m, item)
		if f.renders+f.cleans+f.checksRun != 0 || !strings.Contains(mm.View(), "staged changes") {
			t.Errorf("%s ran or gave no reason:\n%s", item, mm.View())
		}
	}
}

// pickMenu moves the open . menu's cursor to the item labelled label and
// presses enter.
func pickMenu(t *testing.T, m manageModel, label string) manageModel {
	t.Helper()
	for i := 0; i < 12; i++ {
		if strings.Contains(lineWith(t, m.View(), label), ">") {
			return press(m, "enter")
		}
		m = press(m, "j")
	}
	t.Fatalf("menu has no %q:\n%s", label, m.View())
	return m
}
