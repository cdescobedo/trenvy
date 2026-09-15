package cli_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/tui"
)

func keyRunes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func keyEnter() tea.KeyMsg         { return tea.KeyMsg{Type: tea.KeyEnter} }

// The TUI itself is exercised in internal/tui. Here the interest is the save
// path: which rows become manifest entries and which become keyring writes.
func TestEditSavesPlainValuesToTheManifestAndSecretsToTheStore(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	h.opts.RunTUI = func(m tui.Model, reveal func(string) (string, error)) (tui.Model, error) {
		rows := []tui.Row{
			{Name: "APP_URL", Value: "http://+:9999", Dirty: true},
			{Name: "ALPHA_TOKEN", Secret: true, Value: "brand-new", Dirty: true},
			{Name: "NEW_PLAIN", Value: "added", Dirty: true},
		}
		out := tui.New("vpb", rows)
		return markSaved(out), nil
	}

	if err := h.run("edit"); err != nil {
		t.Fatalf("edit: %v", err)
	}

	m, _ := manifest.Load(config.ManifestPath("vpb"))
	if got := *m.Vars["APP_URL"].Value; got != "http://+:9999" {
		t.Errorf("plain value = %q", got)
	}
	if got := *m.Vars["NEW_PLAIN"].Value; got != "added" {
		t.Errorf("new plain value = %q", got)
	}

	got, err := h.mem.Get("trenvy/vpb/dev/ALPHA_TOKEN")
	if err != nil {
		t.Fatalf("store Get: %v", err)
	}
	if got != "brand-new" {
		t.Errorf("stored secret = %q", got)
	}
	if body := mustReadFile(t, config.ManifestPath("vpb")); strings.Contains(body, "brand-new") {
		t.Error("a secret value reached the manifest")
	}
}

// TestEditWritesOnlyChangedSecrets covers two ways a secret row can carry a
// non-dirty Value: never having been touched at all, and having been
// revealed (which populates Value without setting Dirty — see
// internal/tui/model.go's RevealedMsg handler). Both must skip the store
// write; the second case is the one the write-only-when-dirty guard exists
// for, since a reveal-then-save session is exactly where a naive
// "Value != empty" check would misfire and rewrite an unchanged Keychain
// item.
func TestEditWritesOnlyChangedSecrets(t *testing.T) {
	cases := []struct {
		name string
		row  tui.Row
	}{
		{
			name: "never touched",
			row:  tui.Row{Name: "ALPHA_TOKEN", Secret: true, Present: true},
		},
		{
			name: "revealed but not edited",
			row: tui.Row{
				Name: "ALPHA_TOKEN", Secret: true, Present: true,
				Value: "unchanged", Revealed: true, Dirty: false,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.register("vpb", fixtureManifest(t))
			h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "unchanged")

			h.opts.RunTUI = func(m tui.Model, _ func(string) (string, error)) (tui.Model, error) {
				return markSaved(tui.New("vpb", []tui.Row{
					tc.row,
					{Name: "APP_URL", Value: "http://+:8080"}, // not dirty
				})), nil
			}
			if err := h.run("edit"); err != nil {
				t.Fatal(err)
			}

			for _, call := range h.mem.Calls() {
				if strings.HasPrefix(call, "Set ") {
					t.Errorf("edit wrote a secret that did not change: %v", h.mem.Calls())
				}
			}
		})
	}
}

func TestEditAppliesDeletions(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "v")

	h.opts.RunTUI = func(m tui.Model, _ func(string) (string, error)) (tui.Model, error) {
		return markSaved(tui.New("vpb", []tui.Row{
			{Name: "APP_URL", Value: "http://+:8080"},
			{Name: "ALPHA_TOKEN", Secret: true, Present: true, Deleted: true},
		})), nil
	}
	if err := h.run("edit"); err != nil {
		t.Fatal(err)
	}

	m, _ := manifest.Load(config.ManifestPath("vpb"))
	if _, ok := m.Vars["ALPHA_TOKEN"]; ok {
		t.Error("the deleted variable is still in the manifest")
	}
	if ok, _ := h.mem.Has("trenvy/vpb/dev/ALPHA_TOKEN"); ok {
		t.Error("the deleted variable's keyring item was left behind")
	}
}

// TestEditTogglingSecretToPlainDeletesTheOldKeyringItem guards against an
// orphaned keyring item: ALPHA_TOKEN starts secret with a value in the
// store; the session toggles it to plain with a new value. The manifest
// entry is no longer secret, but if the old keyring item survives,
// re-toggling it back to secret later without setting a fresh value would
// (per the write-only-when-dirty guard) leave that stale, unrelated value in
// place with nothing telling the user it is not current.
func TestEditTogglingSecretToPlainDeletesTheOldKeyringItem(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "old-secret-value")

	h.opts.RunTUI = func(m tui.Model, _ func(string) (string, error)) (tui.Model, error) {
		return markSaved(tui.New("vpb", []tui.Row{
			{Name: "APP_URL", Value: "http://+:8080"},
			{Name: "ALPHA_TOKEN", Secret: false, Value: "now-plain", Dirty: true},
		})), nil
	}
	if err := h.run("edit"); err != nil {
		t.Fatal(err)
	}

	m, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	if v := m.Vars["ALPHA_TOKEN"]; v.Secret || v.Value == nil || *v.Value != "now-plain" {
		t.Errorf("ALPHA_TOKEN = %+v, want a plain var with value %q", v, "now-plain")
	}
	if ok, _ := h.mem.Has("trenvy/vpb/dev/ALPHA_TOKEN"); ok {
		t.Error("the old secret's keyring item survived the secret-to-plain toggle")
	}
}

func TestEditCancelledChangesNothing(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	before := mustReadFile(t, config.ManifestPath("vpb"))

	h.opts.RunTUI = func(m tui.Model, _ func(string) (string, error)) (tui.Model, error) {
		return tui.New("vpb", []tui.Row{{Name: "X", Value: "y", Dirty: true}}), nil // not saved
	}
	if err := h.run("edit"); err != nil {
		t.Fatal(err)
	}
	if after := mustReadFile(t, config.ManifestPath("vpb")); after != before {
		t.Error("a cancelled edit changed the manifest")
	}
}

// applyRows must receive AllRows(), never Rows(). If the model had an active
// filter when the user saved, saving from the filtered view would delete
// every variable the filter hid -- silent, total data loss from a keystroke
// the user made to see better.
func TestEditSavingAFilteredViewKeepsFilteredOutRows(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	h.opts.RunTUI = func(m tui.Model, _ func(string) (string, error)) (tui.Model, error) {
		rows := []tui.Row{
			{Name: "APP_URL", Value: "http://+:8080"},
			{Name: "ALPHA_TOKEN", Secret: true, Present: true},
		}
		out := tui.New("vpb", rows)
		// Filter to "U", which matches APP_URL and hides ALPHA_TOKEN, so the
		// saved view really is narrower than the model.
		out, _ = out.Update(keyRunes("/"))
		out, _ = out.Update(keyRunes("U"))
		out, _ = out.Update(keyEnter())
		return markSaved(out), nil
	}
	if err := h.run("edit"); err != nil {
		t.Fatal(err)
	}

	m, _ := manifest.Load(config.ManifestPath("vpb"))
	if _, ok := m.Vars["ALPHA_TOKEN"]; !ok {
		t.Error("a row hidden by an active filter was deleted on save")
	}
}

// markSaved drives a model through w so tests can build a finished model.
func markSaved(m tui.Model) tui.Model {
	out, _ := m.Update(tui.KeyW())
	return out
}
