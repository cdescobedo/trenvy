package manifest_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
)

func parseOrFatal(t *testing.T, src string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return m
}

func TestValidateAcceptsAGoodManifest(t *testing.T) {
	m := parseOrFatal(t, backendTOML)
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// The manifest must never contain a secret value: a hand edit that sets both
// secret = true and a value leaves resolve.Values preferring the plaintext
// while status reports the variable MISSING (it only checks the keyring).
func TestValidateRejectsSecretWithAPlainValue(t *testing.T) {
	m := parseOrFatal(t, `
[project]
id = "p"
profiles = ["dev"]
default_profile = "dev"
[vars.TOKEN]
secret = true
value = "should-not-be-here"
`)
	err := m.Validate()
	if err == nil {
		t.Fatal("Validate accepted a secret with a plain value")
	}
	if !strings.Contains(err.Error(), "TOKEN") {
		t.Errorf("error does not name the variable: %v", err)
	}
	if strings.Contains(err.Error(), "should-not-be-here") {
		t.Errorf("error leaked the value: %v", err)
	}
}

func TestValidateRejectsBadShape(t *testing.T) {
	// head is a project that is valid on every axis but the one each case
	// breaks, so a failure names the rule under test and nothing else.
	const head = "[project]\nid = \"p\"\nprofiles = [\"dev\"]\ndefault_profile = \"dev\"\n"

	cases := map[string]struct {
		src  string
		want string
	}{
		"missing id": {
			src:  "[project]\nprofiles = [\"dev\"]\ndefault_profile = \"dev\"\n",
			want: "project.id",
		},
		"output without path": {
			src:  head + "[outputs.t]\nformat = \"dotenv\"\n",
			want: "path",
		},
		"output without format": {
			src:  head + "[outputs.t]\npath = \".env\"\n",
			want: "format",
		},
		"output with an unknown format": {
			src:  head + "[outputs.t]\nformat = \"yaml\"\npath = \".env\"\n",
			want: "format",
		},
		"absolute path": {
			src:  head + "[outputs.t]\nformat = \"dotenv\"\npath = \"/etc/passwd\"\n",
			want: "relative",
		},
		"path escapes worktree": {
			src:  head + "[outputs.t]\nformat = \"dotenv\"\npath = \"../../.env\"\n",
			want: "escape",
		},
		"no value and not secret": {
			src:  head + "[vars.A]\n",
			want: "A",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := parseOrFatal(t, tc.src)
			err := m.Validate()
			if err == nil {
				t.Fatalf("Validate accepted %s", name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	m := parseOrFatal(t, "[project]\noutput = \"ghost\"\n[outputs.t]\nformat = \"sideways\"\n")
	err := m.Validate()
	var verr *manifest.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Validate returned %T, want *ValidationError", err)
	}
	if len(verr.Problems) < 3 {
		t.Errorf("Problems = %v, want at least 3", verr.Problems)
	}
}

// profileTOML is a minimal valid manifest with profiles declared. Tests below
// mutate one thing about it at a time.
const profileTOML = `
[project]
id = "p"
profiles = ["dev", "prod"]
default_profile = "dev"

[vars.A]
value = "a"
`

func TestValidateAcceptsDeclaredProfiles(t *testing.T) {
	m := parseOrFatal(t, profileTOML)
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRequiresAtLeastOneProfile(t *testing.T) {
	m := parseOrFatal(t, profileTOML)
	m.Project.Profiles = nil

	err := m.Validate()

	if err == nil || !strings.Contains(err.Error(), "profiles") {
		t.Errorf("error = %v, want a complaint about profiles", err)
	}
}

func TestValidateRejectsADefaultProfileThatIsNotDeclared(t *testing.T) {
	m := parseOrFatal(t, profileTOML)
	m.Project.DefaultProfile = "staging"

	err := m.Validate()

	if err == nil || !strings.Contains(err.Error(), "staging") {
		t.Errorf("error = %v, want it to name the undeclared profile", err)
	}
}

func TestValidateRejectsADuplicateProfile(t *testing.T) {
	m := parseOrFatal(t, profileTOML)
	m.Project.Profiles = []string{"dev", "dev"}

	err := m.Validate()

	if err == nil || !strings.Contains(err.Error(), "dev") {
		t.Errorf("error = %v, want it to name the duplicate", err)
	}
}

// A profile name becomes a segment of the keyring service name, so a slash
// would silently reshape the namespace rather than fail.
func TestValidateRejectsAProfileNameWithASlash(t *testing.T) {
	m := parseOrFatal(t, profileTOML)
	m.Project.Profiles = []string{"dev/local"}
	m.Project.DefaultProfile = "dev/local"

	err := m.Validate()

	if err == nil || !strings.Contains(err.Error(), "dev/local") {
		t.Errorf("error = %v, want it to reject the name", err)
	}
}

func TestValidateRejectsAnOverrideForAnUndeclaredProfile(t *testing.T) {
	m := parseOrFatal(t, profileTOML)
	v := m.Vars["A"]
	v.Profiles = map[string]string{"staging": "x"}
	m.Vars["A"] = v

	err := m.Validate()

	if err == nil || !strings.Contains(err.Error(), "staging") {
		t.Errorf("error = %v, want it to name the undeclared profile", err)
	}
}

// A secret's value lives in the keyring and nowhere else. A per-profile plain
// value would put it in the manifest, which is the leak `set` refusing a
// secret exists to prevent, only slower.
func TestValidateRejectsAPlainProfileValueOnASecret(t *testing.T) {
	m := parseOrFatal(t, profileTOML)
	m.Vars["S"] = manifest.Var{Secret: true, Profiles: map[string]string{"dev": "leaked-value"}}

	err := m.Validate()

	if err == nil || !strings.Contains(err.Error(), "S") {
		t.Errorf("error = %v, want it to reject the plain value on a secret", err)
	}
	// The complaint is that a value is here at all, so the complaint itself
	// must not repeat it.
	if err != nil && strings.Contains(err.Error(), "leaked-value") {
		t.Errorf("error leaked the value it is rejecting: %v", err)
	}
}

// outputTOML is a minimal valid manifest on the output axis: a project whose
// render destination is a file, with no notion of a process anywhere in it.
const outputTOML = `
[project]
id = "p"
profiles = ["dev"]
default_profile = "dev"
output = "env"

[outputs.env]
format = "dotenv"
path = "src/.env"

[vars.A]
value = "a"
`

func TestValidateAcceptsOutputs(t *testing.T) {
	m := parseOrFatal(t, outputTOML)
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// A project that only ever feeds exec and shell needs no outputs, where the
// old model forced a dummy process target.
func TestValidateAcceptsAProjectWithNoOutputs(t *testing.T) {
	m := parseOrFatal(t, `
[project]
id = "p"
profiles = ["dev"]
default_profile = "dev"

[vars.A]
value = "a"
`)
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsADefaultOutputThatIsNotDefined(t *testing.T) {
	m := parseOrFatal(t, outputTOML)
	m.Project.Output = "nope"

	err := m.Validate()

	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("error = %v, want it to name the undefined output", err)
	}
}

// A pre-profile manifest must be refused by name rather than silently
// half-read: toml ignores unknown keys, so without this a [targets.*] block
// would vanish and the project would look empty -- which is now a legal shape,
// so nothing downstream would complain either.
//
// The two halves are tested apart as well as together. A manifest can carry
// either one alone -- a project with targets but no default, or a default
// naming a target block someone already deleted -- and a single combined
// fixture would let one half of the check rot away unnoticed.
func TestValidateRejectsAPreProfileManifest(t *testing.T) {
	const head = `
[project]
id = "p"
profiles = ["dev"]
default_profile = "dev"
`
	cases := map[string]string{
		"a targets block alone": head + "\n[targets.local]\ndest = \"process\"\n",
		"default_target alone":  head + "default_target = \"local\"\n",
		"both, as a real old manifest has them": head +
			"default_target = \"local\"\n\n[targets.local]\ndest = \"process\"\n",
	}

	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			m := parseOrFatal(t, src)

			err := m.Validate()

			if err == nil {
				t.Fatal("Validate accepted a pre-profile manifest")
			}
			if !strings.Contains(err.Error(), "trenvy project init") {
				t.Errorf("error does not say how to recover: %v", err)
			}
		})
	}
}

func TestValidateProjectID(t *testing.T) {
	ok := []string{"trenvy", "verity-portal-backend", "my.app", "site.v2", "a_b", "x1"}
	for _, s := range ok {
		if err := manifest.ValidateProjectID(s); err != nil {
			t.Errorf("ValidateProjectID(%q) = %v, want nil", s, err)
		}
	}
	bad := []string{"", ".", "..", "../../etc", "team/app", `team\app`, ".hidden", "-lead", "a b", "a:b"}
	for _, s := range bad {
		if err := manifest.ValidateProjectID(s); err == nil {
			t.Errorf("ValidateProjectID(%q) = nil, want a rejection", s)
		}
	}
}

// Win32 resolves CON, PRN, AUX, NUL, COM1-9 and LPT1-9 as devices even when
// the name carries an extension, and trenvy writes the manifest as <id>.toml.
// An id of "con" therefore looks fine on macOS and is unusable on Windows, so
// it is refused everywhere: a config directory should survive being carried
// between machines.
func TestValidateProjectIDRejectsWindowsDeviceNames(t *testing.T) {
	reserved := []string{"con", "CON", "Con", "prn", "aux", "nul",
		"com1", "COM9", "lpt1", "LPT9", "con.app"}
	for _, s := range reserved {
		err := manifest.ValidateProjectID(s)
		if err == nil {
			t.Errorf("ValidateProjectID(%q) = nil, want a rejection", s)
			continue
		}
		if !strings.Contains(err.Error(), "Windows") {
			t.Errorf("ValidateProjectID(%q) = %v, want it to say why", s, err)
		}
	}
	// Merely containing a device name is not reserved, and the numbered
	// devices stop at 9.
	fine := []string{"console", "conf", "com0", "com10", "lpt", "my.con", "auxiliary"}
	for _, s := range fine {
		if err := manifest.ValidateProjectID(s); err != nil {
			t.Errorf("ValidateProjectID(%q) = %v, want nil", s, err)
		}
	}
}

// A project id is a segment of the keyring service name, exactly as a profile
// name is, so a slash reshapes the namespace instead of failing.
func TestValidateRejectsAProjectIDWithASlash(t *testing.T) {
	m := parseOrFatal(t, profileTOML)
	m.Project.ID = "team/app"

	err := m.Validate()

	if err == nil || !strings.Contains(err.Error(), "team/app") {
		t.Errorf("error = %v, want it to reject the id", err)
	}
}

// The id is also a raw path segment in ManifestPath and StatePath, so ".."
// escapes the config directory rather than naming a project.
func TestValidateRejectsAProjectIDThatEscapesTheConfigDir(t *testing.T) {
	m := parseOrFatal(t, profileTOML)
	m.Project.ID = "../../escape"

	err := m.Validate()

	if err == nil || !strings.Contains(err.Error(), "../../escape") {
		t.Errorf("error = %v, want it to reject the traversal", err)
	}
}

func TestValidOutputName(t *testing.T) {
	ok := []string{"api", "web", "docker", "root", "env-2", "a_b", "X1"}
	for _, s := range ok {
		if !manifest.ValidOutputName(s) {
			t.Errorf("ValidOutputName(%q) = false, want true", s)
		}
	}
	// Dots are refused: an output name is a TOML key and a state-file value,
	// never a path segment, so a dot buys nothing. A leading "-" would make
	// `trenvy render <name>` unparseable as anything but a flag.
	bad := []string{"", "my.app", "-lead", "a/b", "a b", "a:b"}
	for _, s := range bad {
		if manifest.ValidOutputName(s) {
			t.Errorf("ValidOutputName(%q) = true, want false", s)
		}
	}
}

func TestValidateRejectsAnOutputNameWithADot(t *testing.T) {
	m := parseOrFatal(t, profileTOML)
	m.Outputs["my.app"] = manifest.Output{Format: "dotenv", Path: ".env"}

	err := m.Validate()

	if err == nil || !strings.Contains(err.Error(), "my.app") {
		t.Errorf("error = %v, want it to name the bad output", err)
	}
}

func TestCheckOutputPath(t *testing.T) {
	if err := manifest.CheckOutputPath(".env"); err != nil {
		t.Errorf(".env: %v", err)
	}
	for _, bad := range []string{"/etc/env", "../outside.env"} {
		if err := manifest.CheckOutputPath(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
