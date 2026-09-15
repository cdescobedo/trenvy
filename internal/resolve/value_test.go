package resolve_test

import (
	"errors"
	"testing"

	"github.com/cdescobedo/trenvy/internal/resolve"
)

const oneValueTOML = `
[project]
id = "p"
profiles = ["dev"]
default_profile = "dev"
output = "all"

[outputs.all]
format = "dotenv"
path = ".env"
include = ["*"]

[outputs.web]
format = "json"
path = "public/env.json"
include = ["PUBLIC_*"]

[vars.PUBLIC_API]
value = "/"

[vars.DB]
value = "Server=localhost"

[vars.TOKEN]
secret = true

[vars.SPARE]
secret = true
optional = true
`

func TestValueUsesTheDefaultValue(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	got, ok, err := resolve.Value(m, "dev", "DB", lookupFrom(nil))

	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if !ok || got != "Server=localhost" {
		t.Errorf("Value = %q, %v; want the default value", got, ok)
	}
}

func TestValueReadsASecretFromTheLookup(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	got, ok, err := resolve.Value(m, "dev", "TOKEN", lookupFrom(map[string]string{"TOKEN": "t0p"}))

	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if !ok || got != "t0p" {
		t.Errorf("Value = %q, %v; want the stored secret", got, ok)
	}
}

// One variable means one read. Resolving a whole output to answer for a single
// name would cost an authorization prompt per secret on a prompting backend.
func TestValueReadsOnlyTheNamedSecret(t *testing.T) {
	m := mustParse(t, oneValueTOML)
	var asked []string
	lookup := func(name string) (string, error) {
		asked = append(asked, name)
		return "v", nil
	}

	if _, _, err := resolve.Value(m, "dev", "TOKEN", lookup); err != nil {
		t.Fatalf("Value: %v", err)
	}

	if len(asked) != 1 || asked[0] != "TOKEN" {
		t.Errorf("lookup asked for %v, want only TOKEN", asked)
	}
}

func TestValueOnAPlainVarNeverReadsASecret(t *testing.T) {
	m := mustParse(t, oneValueTOML)
	lookup := func(name string) (string, error) {
		t.Errorf("resolving a plain variable read the secret store for %s", name)
		return "", nil
	}

	if _, _, err := resolve.Value(m, "dev", "DB", lookup); err != nil {
		t.Fatalf("Value: %v", err)
	}
}

func TestValueReportsAnOptionalSecretWithNoValueAsAbsent(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	got, ok, err := resolve.Value(m, "dev", "SPARE", lookupFrom(nil))

	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if ok {
		t.Errorf("Value = %q, true; want absent for an optional unset secret", got)
	}
}

// The hint must carry -p <profile> so that pasting it verbatim stores the
// secret under the profile the error is actually about -- not whatever
// profile happens to be active in the caller's shell. resolve.Value has no
// notion of an "active" profile (that lives in the CLI layer), so it always
// includes the flag; asserting only that the variable name appears would
// pass against the old, buggy message just as well as the new one, so this
// pins the exact text.
func TestValueSaysHowToStoreAMissingSecret(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	_, _, err := resolve.Value(m, "dev", "TOKEN", lookupFrom(nil))

	if err == nil {
		t.Fatal("Value succeeded with the secret absent from the keyring")
	}
	want := `TOKEN is not in the keyring for profile "dev"; run: trenvy -p dev secret set TOKEN`
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// Same shape, but with a profile other than the manifest's default -- the
// case the bug report actually reproduced: `trenvy -p prod render local`
// must not suggest a bare `trenvy secret set TOKEN`, which would store the
// value under whatever profile happens to be active instead of prod.
func TestValueSaysHowToStoreAMissingSecretUnderANonDefaultProfile(t *testing.T) {
	m := mustParse(t, profileValueTOML)

	_, _, err := resolve.Value(m, "prod", "TOKEN", lookupFrom(nil))

	if err == nil {
		t.Fatal("Value succeeded with the secret absent from the keyring")
	}
	want := `TOKEN is not in the keyring for profile "prod"; run: trenvy -p prod secret set TOKEN`
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestValueRejectsAnUnknownVariable(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	_, _, err := resolve.Value(m, "dev", "NOPE", lookupFrom(nil))

	if !errors.Is(err, resolve.ErrUnknownVar) {
		t.Errorf("error = %v, want ErrUnknownVar", err)
	}
}

func TestSelectsAppliesTheOutputGlobs(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	for _, tc := range []struct {
		output, name string
		want         bool
	}{
		{"web", "PUBLIC_API", true},
		{"web", "DB", false},
		{"all", "DB", true},
	} {
		got, err := resolve.Selects(m, tc.output, tc.name)
		if err != nil {
			t.Fatalf("Selects(%s, %s): %v", tc.output, tc.name, err)
		}
		if got != tc.want {
			t.Errorf("Selects(%s, %s) = %v, want %v", tc.output, tc.name, got, tc.want)
		}
	}
}

func TestSelectsRejectsAnUnknownOutput(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	_, err := resolve.Selects(m, "nope", "DB")

	if !errors.Is(err, resolve.ErrUnknownOutput) {
		t.Errorf("error = %v, want ErrUnknownOutput", err)
	}
}

const profileValueTOML = `
[project]
id = "p"
profiles = ["dev", "prod"]
default_profile = "dev"

[vars.DB]
value = "Server=localhost"
[vars.DB.profiles]
prod = "Server=db.prod.internal"

[vars.TOKEN]
secret = true
`

func TestValuePrefersTheProfileOverride(t *testing.T) {
	m := mustParse(t, profileValueTOML)

	got, ok, err := resolve.Value(m, "prod", "DB", lookupFrom(nil))

	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if !ok || got != "Server=db.prod.internal" {
		t.Errorf("Value = %q, %v; want the prod override", got, ok)
	}
}

func TestValueFallsBackToTheSharedDefault(t *testing.T) {
	m := mustParse(t, profileValueTOML)

	got, _, err := resolve.Value(m, "dev", "DB", lookupFrom(nil))

	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if got != "Server=localhost" {
		t.Errorf("Value = %q, want the shared default", got)
	}
}
