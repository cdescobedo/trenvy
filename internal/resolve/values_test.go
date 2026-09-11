package resolve_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/resolve"
	"github.com/cdescobedo/envctl/internal/store"
)

func mustParse(t *testing.T, src string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return m
}

// lookupFrom builds a SecretLookup backed by a map, standing in for the store.
func lookupFrom(secrets map[string]string) resolve.SecretLookup {
	return func(name string) (string, error) {
		v, ok := secrets[name]
		if !ok {
			return "", fmt.Errorf("%s: %w", name, store.ErrNotFound)
		}
		return v, nil
	}
}

const resolveTOML = `
[project]
id = "p"
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

[vars.APP_URL]
value = "http://+:8080"

[vars.DB]
value = "Server=localhost"
targets = { docker = "Server=host.docker.internal" }

[vars.PUBLIC_API]
value = "/"

[vars.TOKEN]
secret = true
`

func TestValuesUsesDefaultValue(t *testing.T) {
	m := mustParse(t, resolveTOML)
	got, err := resolve.Values(m, "local", lookupFrom(map[string]string{"TOKEN": "t"}))
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if got.Values["DB"] != "Server=localhost" {
		t.Errorf("DB = %q, want the default value", got.Values["DB"])
	}
}

func TestValuesPrefersPerTargetOverride(t *testing.T) {
	m := mustParse(t, resolveTOML)
	got, err := resolve.Values(m, "docker", lookupFrom(map[string]string{"TOKEN": "t"}))
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if got.Values["DB"] != "Server=host.docker.internal" {
		t.Errorf("DB = %q, want the docker override", got.Values["DB"])
	}
}

func TestValuesReadsSecretsFromTheLookup(t *testing.T) {
	m := mustParse(t, resolveTOML)
	got, err := resolve.Values(m, "local", lookupFrom(map[string]string{"TOKEN": "s3cret"}))
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if got.Values["TOKEN"] != "s3cret" {
		t.Errorf("TOKEN = %q", got.Values["TOKEN"])
	}
}

func TestValuesNamesAreSortedForDeterminism(t *testing.T) {
	m := mustParse(t, resolveTOML)
	got, err := resolve.Values(m, "local", lookupFrom(map[string]string{"TOKEN": "t"}))
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	want := []string{"APP_URL", "DB", "PUBLIC_API", "TOKEN"}
	if !reflect.DeepEqual(got.Names, want) {
		t.Errorf("Names = %v, want %v", got.Names, want)
	}
}

func TestValuesAppliesIncludeGlobs(t *testing.T) {
	m := mustParse(t, resolveTOML)
	got, err := resolve.Values(m, "web", lookupFrom(nil))
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if !reflect.DeepEqual(got.Names, []string{"PUBLIC_API"}) {
		t.Errorf("Names = %v, want [PUBLIC_API]", got.Names)
	}
}

func TestValuesAppliesExcludeAfterInclude(t *testing.T) {
	m := mustParse(t, `
[project]
id = "p"
default_target = "t"
[targets.t]
dest = "process"
include = ["A_*"]
exclude = ["A_SKIP"]
[vars.A_KEEP]
value = "1"
[vars.A_SKIP]
value = "2"
[vars.B_OTHER]
value = "3"
`)
	got, err := resolve.Values(m, "t", lookupFrom(nil))
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if !reflect.DeepEqual(got.Names, []string{"A_KEEP"}) {
		t.Errorf("Names = %v, want [A_KEEP]", got.Names)
	}
}

func TestValuesOmittedIncludeMeansEverything(t *testing.T) {
	m := mustParse(t, "[project]\nid=\"p\"\ndefault_target=\"t\"\n[targets.t]\ndest=\"process\"\n[vars.A]\nvalue=\"1\"\n[vars.B]\nvalue=\"2\"\n")
	got, err := resolve.Values(m, "t", lookupFrom(nil))
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if !reflect.DeepEqual(got.Names, []string{"A", "B"}) {
		t.Errorf("Names = %v, want [A B]", got.Names)
	}
}

func TestValuesFailsOnMissingSecretNamingTheVariable(t *testing.T) {
	m := mustParse(t, resolveTOML)
	_, err := resolve.Values(m, "local", lookupFrom(nil))
	if err == nil {
		t.Fatal("Values succeeded with a missing secret")
	}
	if !strings.Contains(err.Error(), "TOKEN") {
		t.Errorf("error does not name the variable: %v", err)
	}
	if !strings.Contains(err.Error(), "envctl secret set TOKEN") {
		t.Errorf("error does not suggest the fix: %v", err)
	}
}

func TestValuesSkipsMissingOptionalSecret(t *testing.T) {
	m := mustParse(t, "[project]\nid=\"p\"\ndefault_target=\"t\"\n[targets.t]\ndest=\"process\"\n[vars.MAYBE]\nsecret=true\noptional=true\n")
	got, err := resolve.Values(m, "t", lookupFrom(nil))
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if len(got.Names) != 0 {
		t.Errorf("Names = %v, want empty", got.Names)
	}
}

func TestValuesRejectsUnknownTarget(t *testing.T) {
	m := mustParse(t, resolveTOML)
	_, err := resolve.Values(m, "ghost", lookupFrom(nil))
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("err = %v, want one naming the unknown target", err)
	}
}

// Only secrets that survive selection should be fetched. Reading a secret that
// this target excludes would cost an authorization prompt for nothing.
func TestValuesDoesNotFetchExcludedSecrets(t *testing.T) {
	m := mustParse(t, `
[project]
id = "p"
default_target = "t"
[targets.t]
dest = "process"
include = ["KEEP"]
[vars.KEEP]
value = "1"
[vars.SKIPPED]
secret = true
`)
	fetched := map[string]bool{}
	lookup := func(name string) (string, error) {
		fetched[name] = true
		return "v", nil
	}
	if _, err := resolve.Values(m, "t", lookup); err != nil {
		t.Fatalf("Values: %v", err)
	}
	if fetched["SKIPPED"] {
		t.Error("Values fetched a secret excluded by the target's selection")
	}
}

func TestValuesRejectsIncludePatternError(t *testing.T) {
	m := mustParse(t, `
[project]
id = "p"
default_target = "t"
[targets.t]
dest = "process"
include = ["["]
[vars.X]
value = "1"
`)
	_, err := resolve.Values(m, "t", lookupFrom(nil))
	if err == nil {
		t.Fatal("Values succeeded with malformed include pattern")
	}
	if !strings.Contains(err.Error(), "t") {
		t.Errorf("error does not name the target: %v", err)
	}
	if !strings.Contains(err.Error(), "[") {
		t.Errorf("error does not name the pattern: %v", err)
	}
}

func TestValuesRejectsExcludePatternError(t *testing.T) {
	m := mustParse(t, `
[project]
id = "p"
default_target = "t"
[targets.t]
dest = "process"
include = ["*"]
exclude = ["["]
[vars.X]
value = "1"
`)
	_, err := resolve.Values(m, "t", lookupFrom(nil))
	if err == nil {
		t.Fatal("Values succeeded with malformed exclude pattern")
	}
	if !strings.Contains(err.Error(), "t") {
		t.Errorf("error does not name the target: %v", err)
	}
	if !strings.Contains(err.Error(), "[") {
		t.Errorf("error does not name the pattern: %v", err)
	}
}

func TestValuesUsesDefaultValueForSecret(t *testing.T) {
	m := mustParse(t, `
[project]
id = "p"
default_target = "t"
[targets.t]
dest = "process"
[vars.API_KEY]
value = "default-key"
secret = true
`)
	fetched := map[string]bool{}
	lookup := func(name string) (string, error) {
		fetched[name] = true
		return "from-keyring", nil
	}
	got, err := resolve.Values(m, "t", lookup)
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if got.Values["API_KEY"] != "default-key" {
		t.Errorf("API_KEY = %q, want the default value", got.Values["API_KEY"])
	}
	if fetched["API_KEY"] {
		t.Error("Values fetched a secret that had a default value")
	}
}
