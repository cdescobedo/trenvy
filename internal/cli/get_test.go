package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/store"
)

// readbackFixture covers every case get and show have to tell apart: a plain
// var, a per-profile override, a stored secret, a secret that is not stored,
// an optional secret with no value, and an output that selects only some.
func readbackFixture(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
profiles = ["dev", "prod"]
default_profile = "dev"
output = "local"
salt = "abc123"

[outputs.local]
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
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "alpha-value")
	return h
}

// showHarness stores both required secrets, so a whole-output resolve works.
func showHarness(t *testing.T) *harness {
	t.Helper()
	h := getHarness(t)
	h.mem.Seed("trenvy/vpb/dev/BETA_TOKEN", "beta-value")
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

func TestGetPrintsTheBaseValueWhenNoProfileOverrideApplies(t *testing.T) {
	h := getHarness(t)

	if err := h.run("get", "PUBLIC_API"); err != nil {
		t.Fatalf("get: %v", err)
	}

	if got := h.out.String(); got != "/\n" {
		t.Errorf("stdout = %q, want the var's base value", got)
	}
}

// An output decides a file's audience, not a value. Naming a variable
// explicitly makes any subset irrelevant, so get takes no output at all.
//
// "web" is a real, declared output in readbackFixture -- not a made-up name.
// Using a name that does not exist would let this pass because the old code
// rejected an unknown output, never reaching the arity question at all; using
// a valid one means only arity is left to reject the call.
func TestGetTakesNoOutputArgument(t *testing.T) {
	h := getHarness(t)

	err := h.run("get", "APP_MODE", "web")

	if err == nil {
		t.Fatal("get accepted a second positional argument")
	}
	if !strings.Contains(err.Error(), "received 2") {
		t.Errorf("error does not say get was given too many arguments: %v", err)
	}
}

// init creates a project with no outputs at all, so routing get through
// outputOrDefault would break the most basic loop -- init, set, get -- from
// the moment a fresh project exists. get must resolve a name from the profile
// alone, with no output configuration of any kind.
func TestGetWorksWithNoOutputsConfigured(t *testing.T) {
	h := newHarness(t)
	h.register("proc", noOutputFixture(t))

	if err := h.run("get", "APP_MODE"); err != nil {
		t.Fatalf("get on a project with no outputs: %v", err)
	}
	if got := h.out.String(); got != "development\n" {
		t.Errorf("stdout = %q, want the plain value", got)
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

	want := []string{"Get trenvy/vpb/dev/ALPHA_TOKEN"}
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

func TestGetSaysHowToStoreAMissingSecret(t *testing.T) {
	h := getHarness(t)

	err := h.run("get", "BETA_TOKEN")

	if err == nil {
		t.Fatal("get succeeded for a secret that is not in the keyring")
	}
	if !strings.Contains(err.Error(), "trenvy -p dev secret set BETA_TOKEN") {
		t.Errorf("error does not say how to store it under the right profile: %v", err)
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

// Resolving whether an optional variable has a value is purely a function of
// the active profile -- an output only ever decides selection. The message
// must say so, or a reader debugging an unset optional goes looking at the
// wrong axis.
func TestGetOptionalMissingValueNamesTheProfileNotTheOutput(t *testing.T) {
	h := getHarness(t)

	err := h.run("get", "SPARE_TOKEN")

	if err == nil {
		t.Fatal("get succeeded for an optional secret with no value")
	}
	if !strings.Contains(err.Error(), "SPARE_TOKEN") {
		t.Errorf("error does not name the variable: %v", err)
	}
	if !strings.Contains(err.Error(), "profile") {
		t.Errorf("error does not name the profile: %v", err)
	}
	if strings.Contains(err.Error(), "output") {
		t.Errorf("error still names the output, which never decides the value: %v", err)
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
// On a prompting backend a bare `trenvy show` would otherwise cost one
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

// An output narrows which variables are listed and nothing else. This used to
// lean on a per-target value override as its evidence; with values owned by
// the profile, the include list is the whole of what an output decides, so
// that is what the test pins: what is in the subset, and what is left out.
func TestShowRestrictsToTheNamedOutput(t *testing.T) {
	h := showHarness(t)

	if err := h.run("show", "web", "--reveal"); err != nil {
		t.Fatalf("show web: %v", err)
	}

	out := h.out.String()
	if !strings.Contains(out, "PUBLIC_API") {
		t.Errorf("show omitted a var the web output includes:\n%s", out)
	}
	if strings.Contains(out, "APP_MODE") {
		t.Errorf("show listed a var the web output excludes:\n%s", out)
	}
	if strings.Contains(out, "ALPHA_TOKEN") {
		t.Errorf("show listed a secret the web output excludes:\n%s", out)
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
	if !strings.Contains(out, "Generated by trenvy") {
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

// noOutputFixture is the shape `init` now creates: one profile, no outputs at
// all, because exec and shell are the verb.
func noOutputFixture(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(`
[project]
id = "proc"
profiles = ["dev"]
default_profile = "dev"
salt = "abc123"

[vars.APP_MODE]
value = "development"

[vars.API_KEY]
secret = true
`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// show answers "what are my values", which is a question about the profile.
// An output is a filter on that answer, not a precondition for asking it -- so
// a project with no outputs, which is exactly what `init` produces, must still
// be able to see its own variables. Otherwise the one command you reach for
// when confused is the one that cannot run.
func TestShowWithNoOutputsListsTheWholeProfile(t *testing.T) {
	h := newHarness(t)
	h.register("proc", noOutputFixture(t))
	h.mem.Seed("trenvy/proc/dev/API_KEY", "k3y")

	if err := h.run("show"); err != nil {
		t.Fatalf("show on a project with no outputs: %v", err)
	}

	out := h.out.String()
	if !strings.Contains(out, "APP_MODE") || !strings.Contains(out, "development") {
		t.Errorf("show omitted the plain variable:\n%s", out)
	}
	if !strings.Contains(out, "API_KEY") {
		t.Errorf("show omitted the secret's row:\n%s", out)
	}
	if !strings.Contains(out, "•") {
		t.Errorf("show did not mask the secret:\n%s", out)
	}
	if strings.Contains(out, "k3y") {
		t.Errorf("show printed a secret without --reveal:\n%s", out)
	}
}

// The masked path must stay free on a no-output project too: listing every
// variable is still a table of bullets, and on a prompting backend opening the
// store just to print them would make `trenvy show` cost more than a render.
func TestShowWithNoOutputsStillNeverTouchesTheStore(t *testing.T) {
	h := newHarness(t)
	h.register("proc", noOutputFixture(t))
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
}

// --reveal and --format have to reach the same whole-profile set: formatting
// everything to stdout is render-to-stdout of the profile, and a project with
// no outputs is exactly the one that has no other way to get it.
func TestShowWithNoOutputsRevealsAndFormatsTheWholeProfile(t *testing.T) {
	h := newHarness(t)
	h.register("proc", noOutputFixture(t))
	h.mem.Seed("trenvy/proc/dev/API_KEY", "k3y")

	if err := h.run("show", "--format", "json"); err != nil {
		t.Fatalf("show --format json: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(h.out.Bytes(), &got); err != nil {
		t.Fatalf("output is not a json object: %v\n%s", err, h.out)
	}
	if got["APP_MODE"] != "development" || got["API_KEY"] != "k3y" {
		t.Errorf("json output = %v, want the whole profile", got)
	}
}

// project.output says where render's bytes land. show writes nothing, so it
// must not inherit that narrowing: a bare `show` on a project whose default
// output is a narrow one still answers for the whole profile.
func TestShowWithNoArgumentIgnoresTheDefaultOutput(t *testing.T) {
	h := newHarness(t)
	m, err := manifest.Parse([]byte(`
[project]
id = "fe"
profiles = ["dev"]
default_profile = "dev"
output = "web"
salt = "abc123"

[outputs.web]
format = "json"
path = "public/env.json"
include = ["PUBLIC_*"]

[vars.APP_MODE]
value = "development"

[vars.PUBLIC_API]
value = "/"
`))
	if err != nil {
		t.Fatal(err)
	}
	h.register("fe", m)

	if err := h.run("show"); err != nil {
		t.Fatalf("show: %v", err)
	}

	out := h.out.String()
	if !strings.Contains(out, "PUBLIC_API") {
		t.Errorf("show omitted a variable:\n%s", out)
	}
	if !strings.Contains(out, "APP_MODE") {
		t.Errorf("a bare show was narrowed by project.output:\n%s", out)
	}
}
