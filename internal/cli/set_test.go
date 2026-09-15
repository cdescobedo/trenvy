package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
)

// set now writes the active profile's override, not the shared Value -- see
// TestSetWritesTheActiveProfilesOverride below for the multi-profile case that
// motivates it. The fixture's only declared profile is "dev", which is also
// the default, so these tests assert on Profiles["dev"].
func TestSetAddsAPlainVariable(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	if err := h.run("set", "NEW_VAR", "hello"); err != nil {
		t.Fatalf("set: %v", err)
	}
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	v := m.Vars["NEW_VAR"]
	if got, ok := v.Profiles["dev"]; !ok || got != "hello" {
		t.Errorf("NEW_VAR.Profiles[dev] = %q, ok=%v", got, ok)
	}
}

func TestSetUpdatesAnExistingVariable(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	if err := h.run("set", "APP_URL", "http://+:9000"); err != nil {
		t.Fatalf("set: %v", err)
	}
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	if got := m.Vars["APP_URL"].Profiles["dev"]; got != "http://+:9000" {
		t.Errorf("value = %q", got)
	}
}

func TestSetAcceptsAnEmptyValue(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	if err := h.run("set", "BLANK", ""); err != nil {
		t.Fatalf("set: %v", err)
	}
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	v := m.Vars["BLANK"]
	got, ok := v.Profiles["dev"]
	if !ok {
		t.Fatal("an empty value was stored as absent")
	}
	if got != "" {
		t.Errorf("value = %q", got)
	}
}

func TestSetRefusesToOverwriteASecretWithAPlainValue(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	err := h.run("set", "ALPHA_TOKEN", "oops")
	if err == nil {
		t.Fatal("set overwrote a secret with a plain value")
	}
	if !strings.Contains(err.Error(), "envctl secret set") {
		t.Errorf("error does not point at the right command: %v", err)
	}
	// The rejected value must not appear anywhere.
	if strings.Contains(err.Error(), "oops") {
		t.Errorf("error echoed the value: %v", err)
	}
}

func TestSecretSetStoresInTheStoreNotTheManifest(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.opts.Stdin = strings.NewReader("s3cretvalue\n")

	if err := h.run("secret", "set", "ALPHA_TOKEN", "--stdin"); err != nil {
		t.Fatalf("secret set: %v", err)
	}

	got, err := h.mem.Get("envctl/vpb/dev/ALPHA_TOKEN")
	if err != nil {
		t.Fatalf("store Get: %v", err)
	}
	if got != "s3cretvalue" {
		t.Errorf("stored = %q", got)
	}

	data := mustReadFile(t, config.ManifestPath("vpb"))
	if strings.Contains(data, "s3cretvalue") {
		t.Error("the secret value was written into the manifest")
	}
}

func TestSecretSetMarksANewVariableAsSecret(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.opts.Stdin = strings.NewReader("v\n")

	if err := h.run("secret", "set", "BRAND_NEW", "--stdin"); err != nil {
		t.Fatalf("secret set: %v", err)
	}
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	if !m.Vars["BRAND_NEW"].Secret {
		t.Error("the new variable was not marked secret")
	}
}

func TestSecretSetTrimsOnlyTheTrailingNewline(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.opts.Stdin = strings.NewReader("  padded value  \n")

	if err := h.run("secret", "set", "K", "--stdin"); err != nil {
		t.Fatal(err)
	}
	got, _ := h.mem.Get("envctl/vpb/dev/K")
	if got != "  padded value  " {
		t.Errorf("stored = %q; leading and trailing spaces are part of a secret", got)
	}
}

func TestSecretRmDeletesFromTheStore(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "v")

	if err := h.run("secret", "rm", "ALPHA_TOKEN", "--yes"); err != nil {
		t.Fatalf("secret rm: %v", err)
	}
	if ok, _ := h.mem.Has("envctl/vpb/dev/ALPHA_TOKEN"); ok {
		t.Error("the item is still in the store")
	}
	// The variable stays in the manifest; only its value is gone.
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	if _, ok := m.Vars["ALPHA_TOKEN"]; !ok {
		t.Error("secret rm removed the variable from the manifest")
	}
}

func TestUnsetRemovesTheVariableAndItsSecret(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "v")

	if err := h.run("unset", "ALPHA_TOKEN", "--yes"); err != nil {
		t.Fatalf("unset: %v", err)
	}
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	if _, ok := m.Vars["ALPHA_TOKEN"]; ok {
		t.Error("the variable is still in the manifest")
	}
	if ok, _ := h.mem.Has("envctl/vpb/dev/ALPHA_TOKEN"); ok {
		t.Error("the keyring item was left behind")
	}
}

// A value written while standing on a profile belongs to that profile. Writing
// the shared default instead would silently change every other profile too.
func TestSetWritesTheActiveProfilesOverride(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)

	if err := h.run("-p", "prod", "set", "APP_URL", "http://+:9090"); err != nil {
		t.Fatalf("set: %v", err)
	}

	got := reloadManifest(t, "vpb").Vars["APP_URL"]
	if got.Profiles["prod"] != "http://+:9090" {
		t.Errorf("prod override = %q, want the new value", got.Profiles["prod"])
	}
	if got.Value == nil || *got.Value != "http://+:8080" {
		t.Errorf("the shared default was modified: %v", got.Value)
	}
}

func TestSetSharedWritesTheDefaultValue(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)

	if err := h.run("-p", "prod", "set", "APP_URL", "http://+:7070", "--shared"); err != nil {
		t.Fatalf("set --shared: %v", err)
	}

	got := reloadManifest(t, "vpb").Vars["APP_URL"]
	if got.Value == nil || *got.Value != "http://+:7070" {
		t.Errorf("shared value = %v, want the new value", got.Value)
	}
	if _, ok := got.Profiles["prod"]; ok {
		t.Error("--shared wrote a profile override")
	}
}

// set is the one writer that takes a value in argv, so it is the one writer
// that must never touch a secret: the value is already in the process table
// and the shell history by the time the command runs.
func TestSetRefusesASecretVariable(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	err := h.run("set", "ALPHA_TOKEN", "hunter2")

	if err == nil {
		t.Fatal("set wrote to a secret variable")
	}
	if !strings.Contains(err.Error(), "envctl secret set") {
		t.Errorf("error does not point at the safe command: %v", err)
	}
}

func TestSetSharedAlsoRefusesASecretVariable(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	if err := h.run("set", "ALPHA_TOKEN", "hunter2", "--shared"); err == nil {
		t.Fatal("set --shared wrote to a secret variable")
	}
}

// A sample .env file describes the project, not one environment, so an import
// seeds the shared default rather than whichever profile happens to be active.
func TestImportWritesTheSharedDefault(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	sample := filepath.Join(h.root, "sample.env")
	if err := os.WriteFile(sample, []byte("NEW_VAR=hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := h.run("-p", "prod", "import", sample, "--no-tui"); err != nil {
		t.Fatalf("import: %v", err)
	}

	got := reloadManifest(t, "vpb").Vars["NEW_VAR"]
	if got.Value == nil || *got.Value != "hello" {
		t.Errorf("value = %v, want the shared default", got.Value)
	}
	if len(got.Profiles) != 0 {
		t.Errorf("import wrote profile overrides: %v", got.Profiles)
	}
}

// secret rm drops one profile's value; unset drops the variable and every
// profile's value. A prompt that does not say which is a prompt that gets
// approved without being read.
func TestSecretRmPromptNamesOnlyTheActiveProfile(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	h.opts.Stdin = strings.NewReader("n\n")

	_ = h.run("secret", "rm", "ALPHA_TOKEN")

	if !strings.Contains(h.errb.String(), `profile "dev"`) {
		t.Errorf("prompt does not name the profile:\n%s", h.errb)
	}
}

func TestUnsetPromptNamesEveryProfileItWillClear(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	h.opts.Stdin = strings.NewReader("n\n")

	_ = h.run("unset", "ALPHA_TOKEN")

	if !strings.Contains(h.errb.String(), "2 profiles") {
		t.Errorf("prompt does not state the blast radius:\n%s", h.errb)
	}
}

func TestUnsetDeletesTheSecretInEveryProfile(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "d")
	h.mem.Seed("envctl/vpb/prod/ALPHA_TOKEN", "p")

	if err := h.run("unset", "ALPHA_TOKEN", "--yes"); err != nil {
		t.Fatalf("unset: %v", err)
	}

	calls := strings.Join(h.mem.Calls(), " ")
	for _, want := range []string{
		"Delete envctl/vpb/dev/ALPHA_TOKEN",
		"Delete envctl/vpb/prod/ALPHA_TOKEN",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %q in %v", want, h.mem.Calls())
		}
	}
}

func TestUnsetWithoutConfirmationRefuses(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.opts.Stdin = strings.NewReader("n\n")

	if err := h.run("unset", "APP_URL"); err == nil {
		t.Fatal("unset proceeded without confirmation")
	}
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	if _, ok := m.Vars["APP_URL"]; !ok {
		t.Error("the variable was removed despite the refusal")
	}
}
