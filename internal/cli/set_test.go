package cli_test

import (
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
)

func TestSetAddsAPlainVariable(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	if err := h.run("set", "NEW_VAR", "hello"); err != nil {
		t.Fatalf("set: %v", err)
	}
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	v := m.Vars["NEW_VAR"]
	if v.Value == nil || *v.Value != "hello" {
		t.Errorf("NEW_VAR = %v", v.Value)
	}
}

func TestSetUpdatesAnExistingVariable(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	if err := h.run("set", "APP_URL", "http://+:9000"); err != nil {
		t.Fatalf("set: %v", err)
	}
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	if got := *m.Vars["APP_URL"].Value; got != "http://+:9000" {
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
	if v.Value == nil {
		t.Fatal("an empty value was stored as absent")
	}
	if *v.Value != "" {
		t.Errorf("value = %q", *v.Value)
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

	got, err := h.mem.Get("envctl/vpb/ALPHA_TOKEN")
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
	got, _ := h.mem.Get("envctl/vpb/K")
	if got != "  padded value  " {
		t.Errorf("stored = %q; leading and trailing spaces are part of a secret", got)
	}
}

func TestSecretRmDeletesFromTheStore(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "v")

	if err := h.run("secret", "rm", "ALPHA_TOKEN", "--yes"); err != nil {
		t.Fatalf("secret rm: %v", err)
	}
	if ok, _ := h.mem.Has("envctl/vpb/ALPHA_TOKEN"); ok {
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
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "v")

	if err := h.run("unset", "ALPHA_TOKEN", "--yes"); err != nil {
		t.Fatalf("unset: %v", err)
	}
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	if _, ok := m.Vars["ALPHA_TOKEN"]; ok {
		t.Error("the variable is still in the manifest")
	}
	if ok, _ := h.mem.Has("envctl/vpb/ALPHA_TOKEN"); ok {
		t.Error("the keyring item was left behind")
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
