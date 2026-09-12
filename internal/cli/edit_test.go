package cli_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/tui"
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

	got, err := h.mem.Get("envctl/vpb/ALPHA_TOKEN")
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

func TestEditWritesOnlyChangedSecrets(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "unchanged")

	h.opts.RunTUI = func(m tui.Model, _ func(string) (string, error)) (tui.Model, error) {
		return markSaved(tui.New("vpb", []tui.Row{
			{Name: "ALPHA_TOKEN", Secret: true, Present: true}, // not dirty
			{Name: "APP_URL", Value: "http://+:8080"},      // not dirty
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
}

func TestEditAppliesDeletions(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "v")

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
	if ok, _ := h.mem.Has("envctl/vpb/ALPHA_TOKEN"); ok {
		t.Error("the deleted variable's keyring item was left behind")
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

// TestEditSavingAFilteredViewKeepsFilteredOutRows guards the failure mode
// that matters most in this task: applyRows must receive AllRows(), never
// Rows(). If the model had an active filter when the user saved, saving from
// the filtered view would delete every variable the filter hid.
func TestEditSavingAFilteredViewKeepsFilteredOutRows(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	h.opts.RunTUI = func(m tui.Model, _ func(string) (string, error)) (tui.Model, error) {
		rows := []tui.Row{
			{Name: "APP_URL", Value: "http://+:8080"},
			{Name: "ALPHA_TOKEN", Secret: true, Present: true},
		}
		out := tui.New("vpb", rows)
		// Simulate an active filter that hides everything but one row: type
		// "/" then a filter string that matches nothing named BALANCE...
		out, _ = out.Update(keyRunes("/"))
		out, _ = out.Update(keyRunes("A"))
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
