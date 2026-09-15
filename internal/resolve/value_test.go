package resolve_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/resolve"
)

const oneValueTOML = `
[project]
id = "p"
profiles = ["dev"]
default_profile = "dev"
default_target = "local"

[targets.local]
dest = "process"
include = ["*"]

[targets.docker]
dest = "file"
format = "dotenv"
path = ".env"
include = ["*"]

[targets.web]
dest = "file"
format = "json"
path = "public/env.json"
include = ["PUBLIC_*"]

[vars.PUBLIC_API]
value = "/"

[vars.DB]
value = "Server=localhost"
targets = { docker = "Server=host.docker.internal" }

[vars.TOKEN]
secret = true

[vars.SPARE]
secret = true
optional = true
`

func TestValueUsesTheDefaultValue(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	got, ok, err := resolve.Value(m, "local", "DB", lookupFrom(nil))

	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if !ok || got != "Server=localhost" {
		t.Errorf("Value = %q, %v; want the default value", got, ok)
	}
}

func TestValuePrefersAPerTargetOverride(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	got, _, err := resolve.Value(m, "docker", "DB", lookupFrom(nil))

	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if got != "Server=host.docker.internal" {
		t.Errorf("Value = %q, want the docker override", got)
	}
}

func TestValueReadsASecretFromTheLookup(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	got, ok, err := resolve.Value(m, "local", "TOKEN", lookupFrom(map[string]string{"TOKEN": "t0p"}))

	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if !ok || got != "t0p" {
		t.Errorf("Value = %q, %v; want the stored secret", got, ok)
	}
}

// One variable means one read. Resolving a whole target to answer for a single
// name would cost an authorization prompt per secret on a prompting backend.
func TestValueReadsOnlyTheNamedSecret(t *testing.T) {
	m := mustParse(t, oneValueTOML)
	var asked []string
	lookup := func(name string) (string, error) {
		asked = append(asked, name)
		return "v", nil
	}

	if _, _, err := resolve.Value(m, "local", "TOKEN", lookup); err != nil {
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

	if _, _, err := resolve.Value(m, "local", "DB", lookup); err != nil {
		t.Fatalf("Value: %v", err)
	}
}

func TestValueReportsAnOptionalSecretWithNoValueAsAbsent(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	got, ok, err := resolve.Value(m, "local", "SPARE", lookupFrom(nil))

	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if ok {
		t.Errorf("Value = %q, true; want absent for an optional unset secret", got)
	}
}

func TestValueSaysHowToStoreAMissingSecret(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	_, _, err := resolve.Value(m, "local", "TOKEN", lookupFrom(nil))

	if err == nil {
		t.Fatal("Value succeeded with the secret absent from the keyring")
	}
	if !strings.Contains(err.Error(), "envctl secret set TOKEN") {
		t.Errorf("error does not say how to store it: %v", err)
	}
}

func TestValueRejectsAnUnknownTarget(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	_, _, err := resolve.Value(m, "nope", "DB", lookupFrom(nil))

	if !errors.Is(err, resolve.ErrUnknownTarget) {
		t.Errorf("error = %v, want ErrUnknownTarget", err)
	}
}

func TestValueRejectsAnUnknownVariable(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	_, _, err := resolve.Value(m, "local", "NOPE", lookupFrom(nil))

	if !errors.Is(err, resolve.ErrUnknownVar) {
		t.Errorf("error = %v, want ErrUnknownVar", err)
	}
}

func TestSelectsAppliesTheTargetGlobs(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	for _, tc := range []struct {
		target, name string
		want         bool
	}{
		{"web", "PUBLIC_API", true},
		{"web", "DB", false},
		{"local", "DB", true},
	} {
		got, err := resolve.Selects(m, tc.target, tc.name)
		if err != nil {
			t.Fatalf("Selects(%s, %s): %v", tc.target, tc.name, err)
		}
		if got != tc.want {
			t.Errorf("Selects(%s, %s) = %v, want %v", tc.target, tc.name, got, tc.want)
		}
	}
}

func TestSelectsRejectsAnUnknownTarget(t *testing.T) {
	m := mustParse(t, oneValueTOML)

	_, err := resolve.Selects(m, "nope", "DB")

	if !errors.Is(err, resolve.ErrUnknownTarget) {
		t.Errorf("error = %v, want ErrUnknownTarget", err)
	}
}
