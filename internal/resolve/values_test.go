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
profiles = ["dev", "docker"]
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

[vars.APP_URL]
value = "http://+:8080"

[vars.DB]
value = "Server=localhost"
[vars.DB.profiles]
docker = "Server=host.docker.internal"

[vars.PUBLIC_API]
value = "/"

[vars.TOKEN]
secret = true
`

func TestValuesUsesDefaultValue(t *testing.T) {
	m := mustParse(t, resolveTOML)
	got, err := resolve.Values(m, "dev", "all", lookupFrom(map[string]string{"TOKEN": "t"}))
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if got.Values["DB"] != "Server=localhost" {
		t.Errorf("DB = %q, want the default value", got.Values["DB"])
	}
}

// A variable's value varies by profile, never by output: the same output
// resolved under a different profile is a different set of values.
func TestValuesPrefersThePerProfileOverride(t *testing.T) {
	m := mustParse(t, resolveTOML)
	got, err := resolve.Values(m, "docker", "all", lookupFrom(map[string]string{"TOKEN": "t"}))
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if got.Values["DB"] != "Server=host.docker.internal" {
		t.Errorf("DB = %q, want the docker profile override", got.Values["DB"])
	}
}

func TestValuesReadsSecretsFromTheLookup(t *testing.T) {
	m := mustParse(t, resolveTOML)
	got, err := resolve.Values(m, "dev", "all", lookupFrom(map[string]string{"TOKEN": "s3cret"}))
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if got.Values["TOKEN"] != "s3cret" {
		t.Errorf("TOKEN = %q", got.Values["TOKEN"])
	}
}

func TestValuesNamesAreSortedForDeterminism(t *testing.T) {
	m := mustParse(t, resolveTOML)
	got, err := resolve.Values(m, "dev", "all", lookupFrom(map[string]string{"TOKEN": "t"}))
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
	got, err := resolve.Values(m, "dev", "web", lookupFrom(nil))
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
profiles = ["dev"]
default_profile = "dev"
output = "t"
[outputs.t]
format = "dotenv"
path = ".env"
include = ["A_*"]
exclude = ["A_SKIP"]
[vars.A_KEEP]
value = "1"
[vars.A_SKIP]
value = "2"
[vars.B_OTHER]
value = "3"
`)
	got, err := resolve.Values(m, "dev", "t", lookupFrom(nil))
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if !reflect.DeepEqual(got.Names, []string{"A_KEEP"}) {
		t.Errorf("Names = %v, want [A_KEEP]", got.Names)
	}
}

func TestValuesOmittedIncludeMeansEverything(t *testing.T) {
	m := mustParse(t, "[project]\nid=\"p\"\nprofiles=[\"dev\"]\ndefault_profile=\"dev\"\noutput=\"t\"\n[outputs.t]\nformat=\"dotenv\"\npath=\".env\"\n[vars.A]\nvalue=\"1\"\n[vars.B]\nvalue=\"2\"\n")
	got, err := resolve.Values(m, "dev", "t", lookupFrom(nil))
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if !reflect.DeepEqual(got.Names, []string{"A", "B"}) {
		t.Errorf("Names = %v, want [A B]", got.Names)
	}
}

func TestValuesFailsOnMissingSecretNamingTheVariable(t *testing.T) {
	m := mustParse(t, resolveTOML)
	_, err := resolve.Values(m, "dev", "all", lookupFrom(nil))
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
	m := mustParse(t, "[project]\nid=\"p\"\nprofiles=[\"dev\"]\ndefault_profile=\"dev\"\noutput=\"t\"\n[outputs.t]\nformat=\"dotenv\"\npath=\".env\"\n[vars.MAYBE]\nsecret=true\noptional=true\n")
	got, err := resolve.Values(m, "dev", "t", lookupFrom(nil))
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if len(got.Names) != 0 {
		t.Errorf("Names = %v, want empty", got.Names)
	}
}

func TestValuesRejectsUnknownOutput(t *testing.T) {
	m := mustParse(t, resolveTOML)
	_, err := resolve.Values(m, "dev", "ghost", lookupFrom(nil))
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("err = %v, want one naming the unknown output", err)
	}
}

// Only secrets that survive selection should be fetched. Reading a secret that
// this output excludes would cost an authorization prompt for nothing.
func TestValuesDoesNotFetchExcludedSecrets(t *testing.T) {
	m := mustParse(t, `
[project]
id = "p"
profiles = ["dev"]
default_profile = "dev"
output = "t"
[outputs.t]
format = "dotenv"
path = ".env"
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
	if _, err := resolve.Values(m, "dev", "t", lookup); err != nil {
		t.Fatalf("Values: %v", err)
	}
	if fetched["SKIPPED"] {
		t.Error("Values fetched a secret excluded by the output's selection")
	}
}

func TestValuesRejectsIncludePatternError(t *testing.T) {
	m := mustParse(t, `
[project]
id = "p"
profiles = ["dev"]
default_profile = "dev"
output = "t"
[outputs.t]
format = "dotenv"
path = ".env"
include = ["["]
[vars.X]
value = "1"
`)
	_, err := resolve.Values(m, "dev", "t", lookupFrom(nil))
	if err == nil {
		t.Fatal("Values succeeded with malformed include pattern")
	}
	if !strings.Contains(err.Error(), "t") {
		t.Errorf("error does not name the output: %v", err)
	}
	if !strings.Contains(err.Error(), "[") {
		t.Errorf("error does not name the pattern: %v", err)
	}
}

func TestValuesRejectsExcludePatternError(t *testing.T) {
	m := mustParse(t, `
[project]
id = "p"
profiles = ["dev"]
default_profile = "dev"
output = "t"
[outputs.t]
format = "dotenv"
path = ".env"
include = ["*"]
exclude = ["["]
[vars.X]
value = "1"
`)
	_, err := resolve.Values(m, "dev", "t", lookupFrom(nil))
	if err == nil {
		t.Fatal("Values succeeded with malformed exclude pattern")
	}
	if !strings.Contains(err.Error(), "t") {
		t.Errorf("error does not name the output: %v", err)
	}
	if !strings.Contains(err.Error(), "[") {
		t.Errorf("error does not name the pattern: %v", err)
	}
}

func TestValuesUsesDefaultValueForSecret(t *testing.T) {
	m := mustParse(t, `
[project]
id = "p"
profiles = ["dev"]
default_profile = "dev"
output = "t"
[outputs.t]
format = "dotenv"
path = ".env"
[vars.API_KEY]
value = "default-key"
secret = true
`)
	fetched := map[string]bool{}
	lookup := func(name string) (string, error) {
		fetched[name] = true
		return "from-keyring", nil
	}
	got, err := resolve.Values(m, "dev", "t", lookup)
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

// outputTOML is the post-Task-8 shape: profiles own the values, outputs own
// the subset. PUBLIC_API varies by profile so a test can tell which profile a
// call actually resolved under.
const outputTOML = `
[project]
id = "p"
profiles = ["dev", "prod"]
default_profile = "dev"
output = "env"

[outputs.env]
format = "dotenv"
path = ".env"
include = ["*"]

[outputs.web]
format = "json"
path = "public/env.json"
include = ["PUBLIC_*"]

[vars.APP_MODE]
value = "development"

[vars.PUBLIC_API]
value = "/"
[vars.PUBLIC_API.profiles]
prod = "https://api.example.test"
`

// Values takes the profile explicitly rather than reaching for the manifest's
// default. Both parameters are strings, so an accidental swap compiles
// cleanly; this pins that the profile argument is the one that decides values.
func TestValuesResolvesUnderTheProfileItIsGiven(t *testing.T) {
	m := mustParse(t, outputTOML)

	got, err := resolve.Values(m, "prod", "env", lookupFrom(nil))

	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if got.Profile != "prod" {
		t.Errorf("Profile = %q, want prod", got.Profile)
	}
	if got.Output != "env" {
		t.Errorf("Output = %q, want env", got.Output)
	}
	if got.Values["PUBLIC_API"] != "https://api.example.test" {
		t.Errorf("PUBLIC_API = %q, want the prod value", got.Values["PUBLIC_API"])
	}
}

// All is what exec and shell use: an output's include list is about a file's
// audience, and a process wants its whole environment, so All applies no
// subset at all.
func TestAllResolvesEveryVariableWithNoOutputSubset(t *testing.T) {
	m := mustParse(t, outputTOML)

	got, err := resolve.All(m, "dev", lookupFrom(nil))

	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if !reflect.DeepEqual(got.Names, []string{"APP_MODE", "PUBLIC_API"}) {
		t.Errorf("Names = %v, want every variable in sorted order", got.Names)
	}
	if got.Output != "" {
		t.Errorf("Output = %q, want empty: All belongs to no output", got.Output)
	}
}

func TestAllResolvesUnderTheGivenProfile(t *testing.T) {
	m := mustParse(t, outputTOML)

	got, err := resolve.All(m, "prod", lookupFrom(nil))

	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if got.Profile != "prod" {
		t.Errorf("Profile = %q, want prod", got.Profile)
	}
	if got.Values["PUBLIC_API"] != "https://api.example.test" {
		t.Errorf("PUBLIC_API = %q, want the prod value", got.Values["PUBLIC_API"])
	}
}
