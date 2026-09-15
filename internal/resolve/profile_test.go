package resolve_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/resolve"
)

const profileManifestTOML = `
[project]
id = "p"
profiles = ["dev", "prod"]
default_profile = "dev"
default_target = "local"

[targets.local]
dest = "process"
include = ["*"]
`

func TestProfileFallsBackToTheManifestDefault(t *testing.T) {
	m := mustParse(t, profileManifestTOML)

	got, err := resolve.Profile(m, "", "")

	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if got.Name != "dev" {
		t.Errorf("Name = %q, want dev", got.Name)
	}
	if !strings.Contains(got.Source, "default_profile") {
		t.Errorf("Source = %q, want it to name the manifest field", got.Source)
	}
}

func TestProfileEnvVarBeatsTheManifestDefault(t *testing.T) {
	m := mustParse(t, profileManifestTOML)

	got, err := resolve.Profile(m, "", "prod")

	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if got.Name != "prod" {
		t.Errorf("Name = %q, want prod", got.Name)
	}
	if !strings.Contains(got.Source, resolve.ProfileEnvVar) {
		t.Errorf("Source = %q, want it to name the environment variable", got.Source)
	}
}

func TestProfileFlagBeatsTheEnvVar(t *testing.T) {
	m := mustParse(t, profileManifestTOML)

	got, err := resolve.Profile(m, "dev", "prod")

	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if got.Name != "dev" {
		t.Errorf("Name = %q, want the flag to win", got.Name)
	}
	if !strings.Contains(got.Source, "--profile") {
		t.Errorf("Source = %q, want it to name the flag", got.Source)
	}
}

func TestProfileRejectsAnUndeclaredName(t *testing.T) {
	m := mustParse(t, profileManifestTOML)

	_, err := resolve.Profile(m, "staging", "")

	if !errors.Is(err, resolve.ErrUnknownProfile) {
		t.Fatalf("error = %v, want ErrUnknownProfile", err)
	}
	if !strings.Contains(err.Error(), "dev") || !strings.Contains(err.Error(), "prod") {
		t.Errorf("error does not list the declared profiles: %v", err)
	}
}

// A stale exported ENVCTL_PROFILE is easy to forget about, so the error has
// to say where the bad name came from.
func TestProfileErrorNamesTheSourceOfABadName(t *testing.T) {
	m := mustParse(t, profileManifestTOML)

	_, err := resolve.Profile(m, "", "staging")

	if err == nil || !strings.Contains(err.Error(), resolve.ProfileEnvVar) {
		t.Errorf("error = %v, want it to blame the environment variable", err)
	}
}

// A bad ENVCTL_PROFILE is a dead end without this: every envctl command,
// including `profile use` (the one that would fix it), validates the env var
// before running, so a stale export otherwise locks the shell out of its own
// escape hatch.
func TestProfileErrorFromTheEnvVarNamesTheEscape(t *testing.T) {
	m := mustParse(t, profileManifestTOML)

	_, err := resolve.Profile(m, "", "staging")

	want := "unset " + resolve.ProfileEnvVar
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to name the escape: %s", err, want)
	}
}

// A bad --profile value needs no escape hint: the next invocation simply
// omits the flag.
func TestProfileErrorFromTheFlagDoesNotNameTheEscape(t *testing.T) {
	m := mustParse(t, profileManifestTOML)

	_, err := resolve.Profile(m, "staging", "")

	if err == nil {
		t.Fatal("Profile accepted an undeclared name")
	}
	if strings.Contains(err.Error(), "unset ") {
		t.Errorf("error = %v, want no escape hint for a --profile source", err)
	}
}
