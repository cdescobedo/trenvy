package cli_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/ui"
)

// registerSecondProject registers id at its own root, distinct from
// h.root -- h.register always points at h.root, and Registry.Add refuses
// two entries sharing a root, so a second project for the switcher to list
// needs its own directory the way a real second project would have one.
func registerSecondProject(t *testing.T, h *harness, id string, m *manifest.Manifest) string {
	t.Helper()
	root := t.TempDir()
	r, err := config.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Add(config.Entry{ID: id, Root: root}); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(config.ManifestPath(id), m); err != nil {
		t.Fatal(err)
	}
	return root
}

// Choosing a different registered project in the switcher must reload the
// ui command against it -- in the same process, in the same command
// invocation -- rather than requiring the user to quit and re-run `trenvy
// ui` from the other project's directory.
func TestUiSwitcherReloadsAgainstTheChosenProject(t *testing.T) {
	h := newHarness(t)
	h.register("alpha", fixtureManifest(t))
	registerSecondProject(t, h, "beta", fixtureManifest(t))

	calls := 0
	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		calls++
		if calls == 1 {
			mo, _ = mo.Update(ui.KeyRune('p'))
			mo, _ = mo.Update(ui.KeyRune('j')) // alpha -> beta
			mo, _ = mo.Update(ui.KeyType(tea.KeyEnter))
			return mo, nil
		}
		// Second call: this must be a fresh model for beta, proven by the
		// Overview header, which repeats the project id verbatim.
		out := mo.View()
		if !strings.Contains(out, "beta") {
			t.Errorf("second RunUI call's model is not beta's:\n%s", out)
		}
		return mo, nil
	}

	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}
	if calls != 2 {
		t.Fatalf("RunUI was called %d times, want 2 (once per project)", calls)
	}
	if !strings.Contains(h.out.String(), "no changes written") {
		t.Errorf("stdout = %q, want the usual no-changes acknowledgement after the second session", h.out.String())
	}
}

// Switching must never commit the project being left: staged changes on
// alpha are destroyed by the confirmation, not written to alpha's manifest.
func TestUiSwitcherWithStagedChangesConfirmsAndDoesNotCommitTheOldProject(t *testing.T) {
	h := newHarness(t)
	h.register("alpha", fixtureManifest(t)) // sorted rows: ALPHA_TOKEN, APP_URL
	registerSecondProject(t, h, "beta", fixtureManifest(t))
	before := h.manifest(t, "alpha")

	calls := 0
	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		calls++
		if calls == 1 {
			mo, _ = mo.Update(ui.KeyRune('2')) // Variables tab
			mo, _ = mo.Update(ui.KeyRune('j')) // APP_URL
			mo = clearAndType(mo, "http://+:9999")

			mo, _ = mo.Update(ui.KeyRune('p'))
			mo, _ = mo.Update(ui.KeyRune('j')) // alpha -> beta
			mo, _ = mo.Update(ui.KeyType(tea.KeyEnter))
			if mo.Mode() != ui.ModeConfirmSwitch {
				t.Fatalf("mode = %v, want ModeConfirmSwitch", mo.Mode())
			}
			mo, _ = mo.Update(ui.KeyRune('y'))
			return mo, nil
		}
		return mo, nil
	}

	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}
	if calls != 2 {
		t.Fatalf("RunUI was called %d times, want 2", calls)
	}

	after := h.manifest(t, "alpha")
	if got, ok := after.Vars["APP_URL"].Profiles["dev"]; ok && got == "http://+:9999" {
		t.Error("alpha's staged edit was committed despite switching away instead of writing")
	}
	if after.Vars["APP_URL"].Profiles["dev"] != before.Vars["APP_URL"].Profiles["dev"] {
		t.Error("alpha's manifest changed even though the edit was never saved")
	}
}
