package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/store"
)

// readbackFixture covers every case get and show have to tell apart: a plain
// var, a per-target override, a stored secret, a secret that is not stored,
// an optional secret with no value, and a target that selects only some.
func readbackFixture(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
profiles = ["dev", "prod"]
default_profile = "dev"
default_target = "local"
salt = "abc123"

[targets.local]
dest = "process"
include = ["*"]

[targets.web]
dest = "file"
format = "json"
path = "public/env.json"
include = ["PUBLIC_*"]

[vars.APP_MODE]
value = "development"

[vars.PUBLIC_API]
value = "/"
targets = { web = "https://api.example.test" }
[vars.PUBLIC_API.profiles]
prod = "https://api.example.test"

[vars.ALPHA_TOKEN]
secret = true

[vars.BETA_TOKEN]
secret = true

[vars.SPARE_TOKEN]
secret = true
optional = true
`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// getHarness leaves BETA_TOKEN out of the keyring on purpose: reading one
// name must not depend on every other secret being present.
func getHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.register("vpb", readbackFixture(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "alpha-value")
	return h
}

// showHarness stores both required secrets, so a whole-target resolve works.
func showHarness(t *testing.T) *harness {
	t.Helper()
	h := getHarness(t)
	h.mem.Seed("envctl/vpb/dev/BETA_TOKEN", "beta-value")
	return h
}

func TestGetPrintsAPlainValueAndNothingElse(t *testing.T) {
	h := getHarness(t)

	if err := h.run("get", "APP_MODE"); err != nil {
		t.Fatalf("get: %v", err)
	}

	if got := h.out.String(); got != "development\n" {
		t.Errorf("stdout = %q, want the bare value and one newline", got)
	}
}

func TestGetPrintsASecretValue(t *testing.T) {
	h := getHarness(t)

	if err := h.run("get", "ALPHA_TOKEN"); err != nil {
		t.Fatalf("get: %v", err)
	}

	if got := h.out.String(); got != "alpha-value\n" {
		t.Errorf("stdout = %q, want the stored secret", got)
	}
}

func TestGetHonoursAProfileOverride(t *testing.T) {
	h := getHarness(t)

	if err := h.run("-p", "prod", "get", "PUBLIC_API"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := h.out.String(); got != "https://api.example.test\n" {
		t.Errorf("stdout = %q, want the prod override", got)
	}
}

func TestGetFallsBackToTheDefaultTarget(t *testing.T) {
	h := getHarness(t)

	if err := h.run("get", "PUBLIC_API"); err != nil {
		t.Fatalf("get: %v", err)
	}

	if got := h.out.String(); got != "/\n" {
		t.Errorf("stdout = %q, want the default-target value", got)
	}
}

// The whole point of resolving one name: an unrelated secret missing from the
// keyring must not stop you reading the one you asked for.
func TestGetWorksWhileAnUnrelatedSecretIsMissing(t *testing.T) {
	h := getHarness(t)

	if err := h.run("get", "APP_MODE"); err != nil {
		t.Fatalf("get failed because another secret was missing: %v", err)
	}
	if got := h.out.String(); got != "development\n" {
		t.Errorf("stdout = %q", got)
	}
}

func TestGetReadsOnlyTheSecretItWasAskedFor(t *testing.T) {
	h := showHarness(t)

	if err := h.run("get", "ALPHA_TOKEN"); err != nil {
		t.Fatalf("get: %v", err)
	}

	want := []string{"Get envctl/vpb/dev/ALPHA_TOKEN"}
	if got := h.mem.Calls(); len(got) != 1 || got[0] != want[0] {
		t.Errorf("store calls = %v, want exactly %v", got, want)
	}
}

func TestGetOnAPlainVarNeverOpensTheStore(t *testing.T) {
	h := getHarness(t)
	opened := false
	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		opened = true
		return h.mem, nil
	}

	if err := h.run("get", "APP_MODE"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if opened {
		t.Error("reading a plain value opened the secret store")
	}
}

func TestGetRejectsAnUnknownName(t *testing.T) {
	h := getHarness(t)

	err := h.run("get", "NOPE")

	if err == nil {
		t.Fatal("get succeeded for a name that is not in the manifest")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Errorf("error does not say the name is not in the manifest: %v", err)
	}
}

// PUBLIC_* is all the web target selects, so APP_MODE has no value there and
// the error has to say which target refused it rather than claim it is unset.
func TestGetSaysWhenATargetDoesNotSelectTheName(t *testing.T) {
	h := getHarness(t)

	err := h.run("get", "APP_MODE", "web")

	if err == nil {
		t.Fatal("get succeeded for a name the target excludes")
	}
	if !strings.Contains(err.Error(), "web") {
		t.Errorf("error does not name the target: %v", err)
	}
}

func TestGetSaysHowToStoreAMissingSecret(t *testing.T) {
	h := getHarness(t)

	err := h.run("get", "BETA_TOKEN")

	if err == nil {
		t.Fatal("get succeeded for a secret that is not in the keyring")
	}
	if !strings.Contains(err.Error(), "envctl secret set BETA_TOKEN") {
		t.Errorf("error does not say how to store it: %v", err)
	}
}

func TestGetReportsAnOptionalSecretWithNoValue(t *testing.T) {
	h := getHarness(t)

	err := h.run("get", "SPARE_TOKEN")

	if err == nil {
		t.Fatal("get succeeded for an optional secret with no value")
	}
	if !strings.Contains(err.Error(), "SPARE_TOKEN") {
		t.Errorf("error does not name the variable: %v", err)
	}
}

func TestGetRejectsAnUnknownTarget(t *testing.T) {
	h := getHarness(t)

	err := h.run("get", "APP_MODE", "nope")

	if err == nil {
		t.Fatal("get succeeded for an unknown target")
	}
}

func TestShowMasksSecretsByDefault(t *testing.T) {
	h := showHarness(t)

	if err := h.run("show"); err != nil {
		t.Fatalf("show: %v", err)
	}

	out := h.out.String()
	if !strings.Contains(out, "development") {
		t.Errorf("show hid a plain value:\n%s", out)
	}
	if strings.Contains(out, "alpha-value") {
		t.Errorf("show printed a secret without --reveal:\n%s", out)
	}
	if !strings.Contains(out, "•") {
		t.Errorf("show does not mask the secret:\n%s", out)
	}
}

// A masked table has no use for a secret's value, so it must not pay for one.
// On a prompting backend a bare `envctl show` would otherwise cost one
// authorization per secret to print bullets.
func TestShowMaskedNeverTouchesTheStore(t *testing.T) {
	h := showHarness(t)
	opened := false
	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		opened = true
		return h.mem, nil
	}

	if err := h.run("show"); err != nil {
		t.Fatalf("show: %v", err)
	}

	if opened {
		t.Error("a masked show opened the secret store")
	}
	if calls := h.mem.Calls(); len(calls) != 0 {
		t.Errorf("a masked show made store calls: %v", calls)
	}
}

func TestShowRevealPrintsSecretValues(t *testing.T) {
	h := showHarness(t)

	if err := h.run("show", "--reveal"); err != nil {
		t.Fatalf("show --reveal: %v", err)
	}

	out := h.out.String()
	for _, want := range []string{"alpha-value", "beta-value", "development"} {
		if !strings.Contains(out, want) {
			t.Errorf("show --reveal omitted %q:\n%s", want, out)
		}
	}
}

func TestShowRestrictsToTheNamedTarget(t *testing.T) {
	h := showHarness(t)

	if err := h.run("show", "web", "--reveal"); err != nil {
		t.Fatalf("show web: %v", err)
	}

	out := h.out.String()
	if !strings.Contains(out, "https://api.example.test") {
		t.Errorf("show did not apply the web override:\n%s", out)
	}
	if strings.Contains(out, "APP_MODE") {
		t.Errorf("show listed a var the web target excludes:\n%s", out)
	}
}

// A masked dotenv file would be a file full of bullets, so the format flag
// carries the reveal with it rather than producing something unusable.
func TestShowFormatDotenvImpliesReveal(t *testing.T) {
	h := showHarness(t)

	if err := h.run("show", "--format", "dotenv"); err != nil {
		t.Fatalf("show --format dotenv: %v", err)
	}

	out := h.out.String()
	if !strings.Contains(out, `ALPHA_TOKEN="alpha-value"`) {
		t.Errorf("dotenv output is not revealed:\n%s", out)
	}
	if !strings.Contains(out, "Generated by envctl") {
		t.Errorf("dotenv output has no header:\n%s", out)
	}
}

func TestShowFormatJSONEmitsAnObjectOfValues(t *testing.T) {
	h := showHarness(t)

	if err := h.run("show", "--format", "json"); err != nil {
		t.Fatalf("show --format json: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(h.out.Bytes(), &got); err != nil {
		t.Fatalf("output is not a json object: %v\n%s", err, h.out)
	}
	if got["ALPHA_TOKEN"] != "alpha-value" || got["APP_MODE"] != "development" {
		t.Errorf("json output = %v", got)
	}
}

func TestShowRejectsAnUnknownFormat(t *testing.T) {
	h := showHarness(t)

	err := h.run("show", "--format", "yaml")

	if err == nil {
		t.Fatal("show accepted an unknown format")
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Errorf("error does not name the format: %v", err)
	}
}
