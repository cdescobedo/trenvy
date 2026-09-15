package cli_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
)

func profileHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	m.Project.DefaultProfile = "dev"
	h.register("vpb", m)
	return h
}

// reloadManifest reads the manifest back from disk, so a test asserts on what
// was persisted rather than on an in-memory object the command mutated.
func reloadManifest(t *testing.T, id string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Load(config.ManifestPath(id))
	if err != nil {
		t.Fatalf("reload manifest: %v", err)
	}
	return m
}

func TestProfileWithNoSubcommandNamesTheActiveProfileAndItsSource(t *testing.T) {
	h := profileHarness(t)

	if err := h.run("profile"); err != nil {
		t.Fatalf("profile: %v", err)
	}

	out := h.out.String()
	if !strings.Contains(out, "dev") {
		t.Errorf("output does not name the active profile:\n%s", out)
	}
	if !strings.Contains(out, "default_profile") {
		t.Errorf("output does not say where it came from:\n%s", out)
	}
}

func TestProfileNamesTheEnvVarAsItsSource(t *testing.T) {
	h := profileHarness(t)
	t.Setenv("ENVCTL_PROFILE", "prod")

	if err := h.run("profile"); err != nil {
		t.Fatalf("profile: %v", err)
	}

	out := h.out.String()
	if !strings.Contains(out, "prod") || !strings.Contains(out, "ENVCTL_PROFILE") {
		t.Errorf("output does not blame the environment variable:\n%s", out)
	}
}

// The active profile and the persisted default can differ, and a list that
// showed only one of them would hide the difference.
func TestProfileListMarksActiveAndDefaultSeparately(t *testing.T) {
	h := profileHarness(t)
	t.Setenv("ENVCTL_PROFILE", "prod")

	if err := h.run("profile", "list"); err != nil {
		t.Fatalf("profile list: %v", err)
	}

	out := h.out.String()
	var devLine, prodLine string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "dev") {
			devLine = l
		}
		if strings.Contains(l, "prod") {
			prodLine = l
		}
	}
	if !strings.Contains(devLine, "default") {
		t.Errorf("dev is the persisted default but is not marked: %q", devLine)
	}
	if !strings.HasPrefix(strings.TrimSpace(prodLine), "*") {
		t.Errorf("prod is active but is not marked: %q", prodLine)
	}
}

func TestProfileAddDeclaresANewProfile(t *testing.T) {
	h := profileHarness(t)

	if err := h.run("profile", "add", "personal"); err != nil {
		t.Fatalf("profile add: %v", err)
	}

	got := reloadManifest(t, "vpb").Project.Profiles
	found := false
	for _, p := range got {
		if p == "personal" {
			found = true
		}
	}
	if !found {
		t.Errorf("profiles = %v, want personal among them", got)
	}
}

func TestProfileAddRejectsADuplicate(t *testing.T) {
	h := profileHarness(t)

	err := h.run("profile", "add", "dev")

	if err == nil || !strings.Contains(err.Error(), "dev") {
		t.Errorf("error = %v, want a complaint naming dev", err)
	}
}

func TestProfileAddRejectsAnInvalidName(t *testing.T) {
	h := profileHarness(t)

	err := h.run("profile", "add", "dev/local")

	if err == nil {
		t.Fatal("profile add accepted a name with a slash")
	}
	if got := reloadManifest(t, "vpb").Project.Profiles; len(got) != 2 {
		t.Errorf("profiles = %v, want the manifest untouched", got)
	}
}

// The output of `profile use` is eval'd by the shell, so stdout must carry the
// export and nothing else. A stray character here breaks the user's shell.
func TestProfileUseEmitsOnlyAnExportOnStdout(t *testing.T) {
	h := profileHarness(t)

	if err := h.run("profile", "use", "prod"); err != nil {
		t.Fatalf("profile use: %v", err)
	}

	if got, want := h.out.String(), "export ENVCTL_PROFILE=prod\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

// Checking one field survives is not enough: it cannot tell "never wrote"
// from "rewrote identically". Comparing the file's bytes can.
func TestProfileUseDoesNotTouchTheManifest(t *testing.T) {
	h := profileHarness(t)
	before, err := os.ReadFile(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}

	if err := h.run("profile", "use", "prod"); err != nil {
		t.Fatalf("profile use: %v", err)
	}

	after, err := os.ReadFile(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("manifest bytes changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// A bad name must fail before emitting anything, or an eval would export a
// profile that does not exist and every later command would fail confusingly.
func TestProfileUseRejectsAnUndeclaredProfileWithoutEmitting(t *testing.T) {
	h := profileHarness(t)

	err := h.run("profile", "use", "staging")

	if err == nil {
		t.Fatal("profile use accepted an undeclared profile")
	}
	if h.out.Len() != 0 {
		t.Errorf("stdout = %q, want nothing emitted", h.out)
	}
}

func TestProfileDefaultPersistsToTheManifest(t *testing.T) {
	h := profileHarness(t)

	if err := h.run("profile", "default", "prod"); err != nil {
		t.Fatalf("profile default: %v", err)
	}

	if got := reloadManifest(t, "vpb").Project.DefaultProfile; got != "prod" {
		t.Errorf("default_profile = %q, want prod", got)
	}
}

func TestProfileRmDeletesTheProfilesSecretsAndOverrides(t *testing.T) {
	h := profileHarness(t)
	m := reloadManifest(t, "vpb")
	v := m.Vars["APP_URL"]
	v.Profiles = map[string]string{"prod": "http://+:9090"}
	m.Vars["APP_URL"] = v
	// A second declared secret that is deliberately left unseeded under
	// prod: a declared secret with no value stored in a given profile is
	// the ordinary case, not the exception, and the rm path must tolerate
	// it (store.ErrNotFound) rather than abort on it.
	m.Vars["OTHER_API_KEY"] = manifest.Var{Secret: true}
	if err := manifest.Save(config.ManifestPath("vpb"), m); err != nil {
		t.Fatal(err)
	}
	h.mem.Seed("envctl/vpb/prod/ALPHA_TOKEN", "k")
	// The same secret name under a different profile must survive rm: that
	// is the property the explicit per-name ServiceName call (rather than
	// app.service, which is bound to the active profile) exists to protect.
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "kept")

	if err := h.run("profile", "rm", "prod", "--yes"); err != nil {
		t.Fatalf("profile rm: %v", err)
	}

	got := reloadManifest(t, "vpb")
	for _, p := range got.Project.Profiles {
		if p == "prod" {
			t.Error("prod is still declared")
		}
	}
	if _, ok := got.Vars["APP_URL"].Profiles["prod"]; ok {
		t.Error("the prod override survived")
	}
	calls := strings.Join(h.mem.Calls(), " ")
	if !strings.Contains(calls, "Delete envctl/vpb/prod/ALPHA_TOKEN") {
		t.Errorf("the prod secret was not deleted: %v", h.mem.Calls())
	}
	if !strings.Contains(calls, "Delete envctl/vpb/prod/OTHER_API_KEY") {
		t.Errorf("the never-stored prod secret was not attempted: %v", h.mem.Calls())
	}
	if v, err := h.mem.Get("envctl/vpb/dev/ALPHA_TOKEN"); err != nil || v != "kept" {
		t.Errorf("a different profile's same-named secret did not survive: %q, %v", v, err)
	}
	// Two secrets are declared but only one was ever stored under prod, so
	// the success line -- which counts actual deletions, not declarations --
	// must say 1, not 2.
	if out := h.out.String(); !strings.Contains(out, "(1 secrets deleted)") {
		t.Errorf("stdout = %q, want it to report 1 secret actually deleted", out)
	}
}

func TestProfileRmRefusesTheLastProfile(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev"}
	h.register("vpb", m)

	err := h.run("profile", "rm", "dev", "--yes")

	if err == nil || !strings.Contains(err.Error(), "last") {
		t.Errorf("error = %v, want a refusal naming it as the last profile", err)
	}
	// A refusal on a destructive command must be a hard no: nothing in the
	// keyring may be touched, even a read, before the refusal is returned.
	if calls := h.mem.Calls(); len(calls) != 0 {
		t.Errorf("keyring calls = %v, want none", calls)
	}
}

func TestProfileRmRefusesThePersistedDefault(t *testing.T) {
	h := profileHarness(t)

	err := h.run("profile", "rm", "dev", "--yes")

	if err == nil || !strings.Contains(err.Error(), "profile default") {
		t.Errorf("error = %v, want it to point at `profile default`", err)
	}
	if calls := h.mem.Calls(); len(calls) != 0 {
		t.Errorf("keyring calls = %v, want none", calls)
	}
}

func TestProfileRmRefusesTheActiveProfile(t *testing.T) {
	h := profileHarness(t)
	t.Setenv("ENVCTL_PROFILE", "prod")

	err := h.run("profile", "rm", "prod", "--yes")

	if err == nil || !strings.Contains(err.Error(), "active") {
		t.Errorf("error = %v, want a refusal naming it as active", err)
	}
	if calls := h.mem.Calls(); len(calls) != 0 {
		t.Errorf("keyring calls = %v, want none", calls)
	}
}
