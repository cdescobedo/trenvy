package manage_test

import (
	"errors"
	"strings"
	"testing"
)

func stageTwo(f *fakeDeps) manageModel {
	m := press(newModel(f), "j", "enter", "j", "j", "j", "d") // APP_MODE prod override removed
	m = press(m, "left", "j", "d")                            // DATABASE_URL marked deleted
	return m
}

func TestWOpensAGroupedReviewWithBeforeAndAfter(t *testing.T) {
	m := press(stageTwo(&fakeDeps{}), "w")
	v := m.View()
	for _, want := range []string{"Review", "2", "APP_MODE", "remove prod override", "was production", "DATABASE_URL", "delete variable"} {
		if !strings.Contains(v, want) {
			t.Errorf("review lacks %q:\n%s", want, v)
		}
	}
}

func TestReviewNeverShowsASecretValue(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "enter", "j", "enter")
	m = typeText(m, "s3cret")
	m = press(m, "enter", "w")
	if v := m.View(); strings.Contains(v, "s3cret") || !strings.Contains(v, "store new value (keyring)") {
		t.Errorf("review:\n%s", v)
	}
}

func TestReviewMarksIrreversibleLines(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "d", "w") // delete API_TOKEN (secret, 2 stored)
	v := m.View()
	if !strings.Contains(v, "removes 2 keyring items: dev, prod") || !strings.Contains(v, "cannot be undone") {
		t.Errorf("review:\n%s", v)
	}
}

func TestUInReviewDropsTheLineAndClosesWhenEmpty(t *testing.T) {
	m := press(stageTwo(&fakeDeps{}), "w", "u")
	if len(m.Staged()) != 1 {
		t.Fatalf("staged after one drop = %v", staged(m))
	}
	m = press(m, "u")
	if len(m.Staged()) != 0 || strings.Contains(m.View(), "Review") {
		t.Errorf("review still open with nothing staged:\n%s", m.View())
	}
}

func TestUInReviewSaysWhatDependentsWentWithIt(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "a")
	m = typeText(m, "NEW")
	m = press(m, "enter", "p")
	m = typeText(m, "v")
	m = press(m, "enter", "w", "u") // first line: add variable NEW
	if len(m.Staged()) != 0 || !strings.Contains(m.View(), "1 change that depended on it") {
		t.Errorf("staged %v, view:\n%s", staged(m), m.View())
	}
}

// Spec §3: saving keeps the session, reloads, and keeps the cursor.
func TestEnterSavesStaysAndReloads(t *testing.T) {
	f := &fakeDeps{}
	m := press(stageTwo(f), "w", "enter")
	if len(f.commits) != 1 || len(f.commits[0]) != 2 {
		t.Fatalf("commits = %v", f.commits)
	}
	if f.reloads != 1 {
		t.Errorf("reloads = %d, want 1", f.reloads)
	}
	if m.Quitting() || len(m.Staged()) != 0 {
		t.Errorf("quitting %v, staged %v", m.Quitting(), staged(m))
	}
	v := m.View()
	if !strings.Contains(v, "saved 2 changes") || strings.Contains(v, "Review") {
		t.Errorf("after save:\n%s", v)
	}
	if !strings.Contains(v, "DATABASE_URL") || !strings.Contains(v, "postgres://") {
		t.Errorf("cursor did not stay on DATABASE_URL:\n%s", v)
	}
}

// Spec §3: a failed commit keeps the whole batch staged and the review
// open with the report, and enter retries all of it.
func TestFailedCommitKeepsEverythingAndRetries(t *testing.T) {
	f := &fakeDeps{commitErr: errors.New("keyring is locked\n  stored 1 of 2 secrets")}
	m := press(stageTwo(f), "w", "enter")
	v := m.View()
	if len(m.Staged()) != 2 || !strings.Contains(v, "keyring is locked") || !strings.Contains(v, "still staged") {
		t.Fatalf("staged %v, view:\n%s", staged(m), v)
	}
	if f.reloads != 1 {
		t.Errorf("reloads = %d, want 1 after a failure too", f.reloads)
	}
	f.commitErr = nil
	m = press(m, "enter")
	if len(f.commits) != 2 || len(f.commits[1]) != 2 || len(m.Staged()) != 0 {
		t.Errorf("retry: commits %v, staged %v", f.commits, staged(m))
	}
}

func TestKeysAreIgnoredWhileSaving(t *testing.T) {
	f := &fakeDeps{}
	m := press(stageTwo(f), "w")
	next, cmd := m.Update(key("enter")) // start the save but do not run it
	m = next.(manageModel)
	m = press(m, "enter", "u", "q")
	if len(m.Staged()) != 2 || m.Quitting() {
		t.Errorf("keys acted while saving: staged %v, quitting %v", staged(m), m.Quitting())
	}
	m = send(m, cmd())
	if len(f.commits) != 1 {
		t.Errorf("commits = %d, want exactly 1", len(f.commits))
	}
}

// Spec Risks: a failed reload blocks staging until one succeeds.
func TestFailedReloadBlocksStagingUntilReloaded(t *testing.T) {
	f := &fakeDeps{reloadErr: errors.New("manifest unreadable")}
	m := press(stageTwo(f), "w", "enter")
	if !strings.Contains(m.View(), "reload failed") {
		t.Fatalf("view:\n%s", m.View())
	}
	if m = press(m, "d"); len(m.Staged()) != 0 || !strings.Contains(m.View(), "can't edit") {
		t.Errorf("staged while stale: %v", staged(m))
	}
	f.reloadErr = nil
	m = press(m, ".", "j", "j", "j", "enter") // Reload from disk comes after Rename
	if m = press(m, "d"); len(m.Staged()) != 1 {
		t.Errorf("still blocked after a good reload: %v", staged(m))
	}
}

// Review Focus 4.
func TestRevealDoesNotSurviveAReload(t *testing.T) {
	f := &fakeDeps{secrets: map[string]string{"API_TOKEN/dev": "tok-dev"}}
	m := press(newModel(f), "enter", "v")
	if !strings.Contains(m.View(), "tok-dev") {
		t.Fatal("setup: reveal failed")
	}
	// Stay on API_TOKEN throughout -- leaving it would hide the value for a
	// different reason. Store staging, save, and the reload must hide dev.
	m = press(m, "j", "enter")
	m = typeText(m, "x")
	m = press(m, "enter", "w", "enter")
	if f.reloads != 1 {
		t.Fatalf("reloads = %d, want 1", f.reloads)
	}
	if strings.Contains(m.View(), "tok-dev") {
		t.Error("a revealed value survived a reload")
	}
}

func TestWWithNothingStagedSaysSo(t *testing.T) {
	if v := press(newModel(&fakeDeps{}), "w").View(); !strings.Contains(v, "nothing to save") {
		t.Errorf("view:\n%s", v)
	}
}
