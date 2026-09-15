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
