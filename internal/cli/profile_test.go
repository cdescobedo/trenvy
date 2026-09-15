package cli_test

import (
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

func TestProfileUseDoesNotTouchTheManifest(t *testing.T) {
	h := profileHarness(t)

	if err := h.run("profile", "use", "prod"); err != nil {
		t.Fatalf("profile use: %v", err)
	}

	if got := reloadManifest(t, "vpb").Project.DefaultProfile; got != "dev" {
		t.Errorf("default_profile = %q, want it unchanged at dev", got)
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
	if err := manifest.Save(config.ManifestPath("vpb"), m); err != nil {
		t.Fatal(err)
	}
	h.mem.Seed("envctl/vpb/prod/ALPHA_TOKEN", "k")

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
	if !strings.Contains(strings.Join(h.mem.Calls(), " "), "Delete envctl/vpb/prod/ALPHA_TOKEN") {
		t.Errorf("the prod secret was not deleted: %v", h.mem.Calls())
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
}

func TestProfileRmRefusesThePersistedDefault(t *testing.T) {
	h := profileHarness(t)

	err := h.run("profile", "rm", "dev", "--yes")

	if err == nil || !strings.Contains(err.Error(), "profile default") {
		t.Errorf("error = %v, want it to point at `profile default`", err)
	}
}

func TestProfileRmRefusesTheActiveProfile(t *testing.T) {
	h := profileHarness(t)
	t.Setenv("ENVCTL_PROFILE", "prod")

	err := h.run("profile", "rm", "prod", "--yes")

	if err == nil || !strings.Contains(err.Error(), "active") {
		t.Errorf("error = %v, want a refusal naming it as active", err)
	}
}
