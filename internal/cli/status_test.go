package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
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
	if !strings.Contains(out, "trenvy -p dev secret set ALPHA_TOKEN") {
		t.Errorf("status does not give the fix with the active profile:\n%s", out)
	}
}

// Copy-pasting the fix must write the credential into the profile status is
// actually reporting on. A bare `trenvy secret set` names no profile, so it
// silently stores under whichever profile happens to be active in the
// pasting shell -- which under -p prod is not the profile status just
// checked, and the paste-and-rerun leaves status still reporting MISSING.
func TestStatusMissingHintNamesTheActiveProfile(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)

	if err := h.run("-p", "prod", "status"); err != nil {
		t.Fatalf("status: %v", err)
	}
	out := h.out.String()
	if !strings.Contains(out, "trenvy -p prod secret set ALPHA_TOKEN") {
		t.Errorf("status does not name the active profile in its fix:\n%s", out)
	}
}

// Freshness is an HMAC over resolved values, so status's default behaviour
// must actually read them -- that is the whole point of the full check.
func TestStatusWithoutQuickReadsSecretValues(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "s3cretvalue")

	if err := h.run("status"); err != nil {
		t.Fatal(err)
	}
	var sawGet bool
	for _, call := range h.mem.Calls() {
		if strings.HasPrefix(call, "Get ") {
			sawGet = true
		}
	}
	if !sawGet {
		t.Error("status without --quick never read a secret value")
	}
}

// --quick must skip every value read while still reporting presence through
// Has alone: on macOS a read can raise an authorization prompt, so a status
// that only wants to see what is present must not pay for reading it too.
func TestStatusQuickSkipsSecretReadsButStillReportsPresence(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "s3cretvalue")

	if err := h.run("status", "--quick"); err != nil {
		t.Fatal(err)
	}
	out := h.out.String()
	if !strings.Contains(out, "present") {
		t.Errorf("--quick did not report the secret present:\n%s", out)
	}
	if !strings.Contains(out, "unknown (--quick)") {
		t.Errorf("--quick did not mark the output's freshness unknown:\n%s", out)
	}
	for _, call := range h.mem.Calls() {
		if strings.HasPrefix(call, "Get ") {
			t.Errorf("status --quick read a secret value: %v", h.mem.Calls())
		}
	}
}

// An output with no secrets at all must still report real freshness under
// --quick: the flag exists to skip reads that are unavoidable, not to make
// every output vaguer than it has to be.
func TestStatusQuickReportsRealFreshnessForAnOutputWithNoSecrets(t *testing.T) {
	h := newHarness(t)
	h.gitInit("public/env.json\n")
	m, err := manifest.Parse([]byte(`
[project]
id = "fe"
profiles = ["dev"]
default_profile = "dev"
output = "web"
salt = "abc123"
[outputs.web]
format = "json"
path = "public/env.json"
include = ["*"]
[vars.PUBLIC_URL]
value = "https://example.com"
`))
	if err != nil {
		t.Fatal(err)
	}
	h.register("fe", m)

	if err := h.run("render", "web"); err != nil {
		t.Fatal(err)
	}
	if err := h.run("status", "--quick"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "fresh") {
		t.Errorf("--quick reported a secret-free output as anything but its real freshness:\n%s", h.out)
	}
}

func TestStatusNeverPrintsASecretValue(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "s3cretvalue")

	if err := h.run("status"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.out.String(), "s3cretvalue") {
		t.Errorf("status printed a secret value:\n%s", h.out)
	}
}

func TestStatusReportsOutputFreshness(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "k")

	if err := h.run("render", "docker"); err != nil {
		t.Fatal(err)
	}
	if err := h.run("status"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "fresh") {
		t.Errorf("a just-rendered output is not reported fresh:\n%s", h.out)
	}

	// Change a plain value; the rendered file is now behind.
	if err := h.run("set", "APP_URL", "http://+:9999"); err != nil {
		t.Fatal(err)
	}
	if err := h.run("status"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "STALE") {
		t.Errorf("an output behind the manifest is not reported stale:\n%s", h.out)
	}
}

func TestStatusReportsAHandEditedFileAsModified(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "k")
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
	for _, want := range []string{"project", "profile", "manifest", "worktree", "vars", "outputs"} {
		t.Run(want, func(t *testing.T) {
			if !strings.Contains(out, want) {
				t.Errorf("status output has no %q section:\n%s", want, out)
			}
		})
	}
}

// Which profile is in force decides every value status reports on, and a
// stale exported TRENVY_PROFILE is invisible until something names it. So the
// header says both the profile and where it came from.
func TestStatusNamesTheActiveProfileAndItsSource(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	t.Setenv("TRENVY_PROFILE", "prod")

	if err := h.run("status"); err != nil {
		t.Fatalf("status: %v", err)
	}
	out := h.out.String()
	if !strings.Contains(out, "profile") || !strings.Contains(out, "prod") {
		t.Errorf("status does not name the active profile:\n%s", out)
	}
	if !strings.Contains(out, "TRENVY_PROFILE") {
		t.Errorf("status does not say where the profile came from:\n%s", out)
	}
}
