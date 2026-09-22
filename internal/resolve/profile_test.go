package resolve_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/resolve"
)

const profileManifestTOML = `
[project]
id = "p"
profiles = ["dev", "prod"]
default_profile = "dev"
`

func TestProfileFallsBackToTheManifestDefault(t *testing.T) {
	m := mustParse(t, profileManifestTOML)

	got, err := resolve.Profile(m, "", "", "")

	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if got.Name != "dev" {
		t.Errorf("Name = %q, want dev", got.Name)
	}
	if got.Source != "default" {
		t.Errorf("Source = %q, want %q", got.Source, "default")
	}
}

func TestProfileEnvVarBeatsTheManifestDefault(t *testing.T) {
	m := mustParse(t, profileManifestTOML)

	got, err := resolve.Profile(m, "", "prod", "")

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

	got, err := resolve.Profile(m, "dev", "prod", "")

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

	_, err := resolve.Profile(m, "staging", "", "")

	if !errors.Is(err, resolve.ErrUnknownProfile) {
		t.Fatalf("error = %v, want ErrUnknownProfile", err)
	}
	if !strings.Contains(err.Error(), "dev") || !strings.Contains(err.Error(), "prod") {
		t.Errorf("error does not list the declared profiles: %v", err)
	}
}

// A stale exported TRENVY_PROFILE is easy to forget about, so the error has
// to say where the bad name came from.
func TestProfileErrorNamesTheSourceOfABadName(t *testing.T) {
	m := mustParse(t, profileManifestTOML)

	_, err := resolve.Profile(m, "", "staging", "")

	if err == nil || !strings.Contains(err.Error(), resolve.ProfileEnvVar) {
		t.Errorf("error = %v, want it to blame the environment variable", err)
	}
}

// A bad TRENVY_PROFILE is a dead end without this: every trenvy command,
// including `profile use` (the one that would fix it), validates the env var
// before running, so a stale export otherwise locks the shell out of its own
// escape hatch.
func TestProfileErrorFromTheEnvVarNamesTheEscape(t *testing.T) {
	m := mustParse(t, profileManifestTOML)

	_, err := resolve.Profile(m, "", "staging", "")

	want := "unset " + resolve.ProfileEnvVar
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to name the escape: %s", err, want)
	}
}

// A bad --profile value needs no escape hint: the next invocation simply
// omits the flag.
func TestProfileErrorFromTheFlagDoesNotNameTheEscape(t *testing.T) {
	m := mustParse(t, profileManifestTOML)

	_, err := resolve.Profile(m, "staging", "", "")

	if err == nil {
		t.Fatal("Profile accepted an undeclared name")
	}
	if strings.Contains(err.Error(), "unset ") {
		t.Errorf("error = %v, want no escape hint for a --profile source", err)
	}
}

// fourSourceManifestTOML declares one profile per precedence source. A table
// that only checked the top (--profile) and bottom (default) source would
// pass against an implementation that ignored active entirely; setting all
// four sources to different, declared names makes every comparison in the
// chain unambiguous, including the two that define this level: active must
// lose to TRENVY_PROFILE and must win over default_profile.
const fourSourceManifestTOML = `
[project]
id = "p"
profiles = ["byflag", "byenv", "byactive", "bydefault"]
default_profile = "bydefault"
`

func TestProfilePrecedenceOrder(t *testing.T) {
	m := mustParse(t, fourSourceManifestTOML)

	tests := []struct {
		name                     string
		flag, env, active        string
		wantName, wantSourceLike string
	}{
		{
			name:           "flag beats env, active and default",
			flag:           "byflag",
			env:            "byenv",
			active:         "byactive",
			wantName:       "byflag",
			wantSourceLike: "--profile",
		},
		{
			// The active level is only real if it can lose. An
			// implementation that skipped the active parameter entirely
			// would still pick byenv here, so this alone doesn't prove
			// active was consulted -- but paired with the next case, it
			// proves active was consulted and ranked correctly.
			name:           "env beats active and default",
			flag:           "",
			env:            "byenv",
			active:         "byactive",
			wantName:       "byenv",
			wantSourceLike: resolve.ProfileEnvVar,
		},
		{
			name:           "active beats the manifest default",
			flag:           "",
			env:            "",
			active:         "byactive",
			wantName:       "byactive",
			wantSourceLike: "active",
		},
		{
			name:           "default applies when nothing else is set",
			flag:           "",
			env:            "",
			active:         "",
			wantName:       "bydefault",
			wantSourceLike: "default",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolve.Profile(m, tc.flag, tc.env, tc.active)

			if err != nil {
				t.Fatalf("Profile: %v", err)
			}
			if got.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
			}
			if !strings.Contains(got.Source, tc.wantSourceLike) {
				t.Errorf("Source = %q, want it to contain %q", got.Source, tc.wantSourceLike)
			}
		})
	}
}

// A profile named in the state file and later removed from the manifest is
// still reachable: `profile rm` only clears the active selection for a state
// file it reads, and a hand edit or an older build can leave one behind.
// Without a named escape here, that leaves every command failing until
// someone hand-edits the state file.
func TestProfileErrorFromActiveNamesTheEscape(t *testing.T) {
	m := mustParse(t, profileManifestTOML)

	_, err := resolve.Profile(m, "", "", "staging")

	if err == nil || !errors.Is(err, resolve.ErrUnknownProfile) {
		t.Fatalf("error = %v, want ErrUnknownProfile", err)
	}
	if !strings.Contains(err.Error(), "(from active)") {
		t.Errorf("error = %v, want it to blame active", err)
	}
	want := "trenvy profile use --clear"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to name the escape: %s", err, want)
	}
}
