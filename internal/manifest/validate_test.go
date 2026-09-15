package manifest_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/manifest"
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

func TestValidateRejectsSecretWithPerTargetValues(t *testing.T) {
	m := parseOrFatal(t, `
[project]
id = "p"
default_target = "local"
[targets.local]
dest = "process"
[vars.TOKEN]
secret = true
targets = { local = "nope" }
`)
	err := m.Validate()
	if err == nil {
		t.Fatal("Validate accepted a secret with per-target values")
	}
	if !strings.Contains(err.Error(), "TOKEN") {
		t.Errorf("error does not name the variable: %v", err)
	}
	if strings.Contains(err.Error(), "nope") {
		t.Errorf("error leaked a value: %v", err)
	}
}

// The manifest must never contain a secret value: a hand edit that sets both
// secret = true and a value leaves resolve.Values preferring the plaintext
// while status reports the variable MISSING (it only checks the keyring).
func TestValidateRejectsSecretWithAPlainValue(t *testing.T) {
	m := parseOrFatal(t, `
[project]
id = "p"
default_target = "local"
[targets.local]
dest = "process"
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
	cases := map[string]struct {
		src  string
		want string
	}{
		"missing id": {
			src:  "[project]\ndefault_target = \"local\"\n[targets.local]\ndest = \"process\"\n",
			want: "project.id",
		},
		"unknown default target": {
			src:  "[project]\nid = \"p\"\ndefault_target = \"nope\"\n[targets.local]\ndest = \"process\"\n",
			want: "default_target",
		},
		"bad dest": {
			src:  "[project]\nid = \"p\"\ndefault_target = \"t\"\n[targets.t]\ndest = \"sideways\"\n",
			want: "dest",
		},
		"file target without path": {
			src:  "[project]\nid = \"p\"\ndefault_target = \"t\"\n[targets.t]\ndest = \"file\"\nformat = \"dotenv\"\n",
			want: "path",
		},
		"file target without format": {
			src:  "[project]\nid = \"p\"\ndefault_target = \"t\"\n[targets.t]\ndest = \"file\"\npath = \".env\"\n",
			want: "format",
		},
		"absolute path": {
			src:  "[project]\nid = \"p\"\ndefault_target = \"t\"\n[targets.t]\ndest = \"file\"\nformat = \"dotenv\"\npath = \"/etc/passwd\"\n",
			want: "relative",
		},
		"path escapes worktree": {
			src:  "[project]\nid = \"p\"\ndefault_target = \"t\"\n[targets.t]\ndest = \"file\"\nformat = \"dotenv\"\npath = \"../../.env\"\n",
			want: "escape",
		},
		"override names unknown target": {
			src:  "[project]\nid = \"p\"\ndefault_target = \"t\"\n[targets.t]\ndest = \"process\"\n[vars.A]\nvalue = \"x\"\ntargets = { ghost = \"y\" }\n",
			want: "ghost",
		},
		"no value and not secret": {
			src:  "[project]\nid = \"p\"\ndefault_target = \"t\"\n[targets.t]\ndest = \"process\"\n[vars.A]\n",
			want: "A",
		},
		"empty default target": {
			src:  "[project]\nid = \"p\"\ndefault_target = \"\"\n[targets.t]\ndest = \"process\"\n",
			want: "required",
		},
		"no targets": {
			src:  "[project]\nid = \"p\"\ndefault_target = \"t\"\n",
			want: "at least one",
		},
		"process target with path": {
			src:  "[project]\nid = \"p\"\ndefault_target = \"t\"\n[targets.t]\ndest = \"process\"\npath = \".env\"\n",
			want: "takes no path",
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
	m := parseOrFatal(t, "[project]\ndefault_target = \"ghost\"\n[targets.t]\ndest = \"sideways\"\n")
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
default_target = "local"

[targets.local]
dest = "process"
include = ["*"]

[vars.A]
value = "a"
`

func TestValidateAcceptsDeclaredProfiles(t *testing.T) {
	m := mustParse(t, profileTOML)
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRequiresAtLeastOneProfile(t *testing.T) {
	m := mustParse(t, profileTOML)
	m.Project.Profiles = nil

	err := m.Validate()

	if err == nil || !strings.Contains(err.Error(), "profiles") {
		t.Errorf("error = %v, want a complaint about profiles", err)
	}
}

func TestValidateRejectsADefaultProfileThatIsNotDeclared(t *testing.T) {
	m := mustParse(t, profileTOML)
	m.Project.DefaultProfile = "staging"

	err := m.Validate()

	if err == nil || !strings.Contains(err.Error(), "staging") {
		t.Errorf("error = %v, want it to name the undeclared profile", err)
	}
}

func TestValidateRejectsADuplicateProfile(t *testing.T) {
	m := mustParse(t, profileTOML)
	m.Project.Profiles = []string{"dev", "dev"}

	err := m.Validate()

	if err == nil || !strings.Contains(err.Error(), "dev") {
		t.Errorf("error = %v, want it to name the duplicate", err)
	}
}

// A profile name becomes a segment of the keyring service name, so a slash
// would silently reshape the namespace rather than fail.
func TestValidateRejectsAProfileNameWithASlash(t *testing.T) {
	m := mustParse(t, profileTOML)
	m.Project.Profiles = []string{"dev/local"}
	m.Project.DefaultProfile = "dev/local"

	err := m.Validate()

	if err == nil || !strings.Contains(err.Error(), "dev/local") {
		t.Errorf("error = %v, want it to reject the name", err)
	}
}

func mustParse(t *testing.T, src string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return m
}
