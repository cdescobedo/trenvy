package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusReportsSecretPresence(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	// ALPHA_TOKEN deliberately absent from the store.

	if err := h.run("status"); err != nil {
		t.Fatalf("status: %v", err)
	}
	out := h.out.String()
	if !strings.Contains(out, "MISSING") || !strings.Contains(out, "ALPHA_TOKEN") {
		t.Errorf("missing secret not reported:\n%s", out)
	}
	if !strings.Contains(out, "envctl secret set ALPHA_TOKEN") {
		t.Errorf("status does not give the fix:\n%s", out)
	}
}

// Presence must be established without reading values: on macOS a read can
// raise an authorization prompt, so status would cost one prompt per secret.
func TestStatusProbesPresenceWithoutReadingValues(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	// The secret is deliberately absent. With nothing to resolve, status must
	// still be able to report presence — and must do so with Has alone.
	if err := h.run("status"); err != nil {
		t.Fatal(err)
	}
	calls := h.mem.Calls()
	if len(calls) == 0 {
		t.Fatal("status probed nothing")
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "Get ") {
			t.Errorf("status read a secret value: %v", calls)
		}
	}
}

func TestStatusNeverPrintsASecretValue(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "s3cretvalue")

	if err := h.run("status"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.out.String(), "s3cretvalue") {
		t.Errorf("status printed a secret value:\n%s", h.out)
	}
}

func TestStatusReportsTargetFreshness(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "k")

	if err := h.run("render", "docker"); err != nil {
		t.Fatal(err)
	}
	if err := h.run("status"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "fresh") {
		t.Errorf("a just-rendered target is not reported fresh:\n%s", h.out)
	}

	// Change a plain value; the rendered file is now behind.
	if err := h.run("set", "APP_URL", "http://+:9999"); err != nil {
		t.Fatal(err)
	}
	if err := h.run("status"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "STALE") {
		t.Errorf("a target behind the manifest is not reported stale:\n%s", h.out)
	}
}

func TestStatusReportsAHandEditedFileAsModified(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "k")
	if err := h.run("render", "docker"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(h.root, "src", ".env")
	if err := os.WriteFile(path, []byte("A=edited by hand\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := h.run("status"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "MODIFIED") {
		t.Errorf("a hand-edited file is not reported modified:\n%s", h.out)
	}
}

func TestStatusShowsTheBackendAndWorktree(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	if err := h.run("status"); err != nil {
		t.Fatal(err)
	}
	out := h.out.String()
	for _, want := range []string{"project", "manifest", "worktree", "vars", "targets"} {
		t.Run(want, func(t *testing.T) {
			if !strings.Contains(out, want) {
				t.Errorf("status output has no %q section:\n%s", want, out)
			}
		})
	}
}
