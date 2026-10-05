package cli_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/store"
)

// set now writes the active profile's override, not the shared Value -- see
// TestSetWritesTheActiveProfilesOverride below for the multi-profile case that
// motivates it. The fixture's only declared profile is "dev", which is also
// the default, so these tests assert on Profiles["dev"].
func TestSetAddsAPlainVariable(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	if err := h.run("set", "NEW_VAR", "hello"); err != nil {
		t.Fatalf("set: %v", err)
	}
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	v := m.Vars["NEW_VAR"]
	if got, ok := v.Profiles["dev"]; !ok || got != "hello" {
		t.Errorf("NEW_VAR.Profiles[dev] = %q, ok=%v", got, ok)
	}
}

func TestSetUpdatesAnExistingVariable(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	if err := h.run("set", "APP_URL", "http://+:9000"); err != nil {
		t.Fatalf("set: %v", err)
	}
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	if got := m.Vars["APP_URL"].Profiles["dev"]; got != "http://+:9000" {
		t.Errorf("value = %q", got)
	}
}

func TestSetAcceptsAnEmptyValue(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	if err := h.run("set", "BLANK", ""); err != nil {
		t.Fatalf("set: %v", err)
	}
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	v := m.Vars["BLANK"]
	got, ok := v.Profiles["dev"]
	if !ok {
		t.Fatal("an empty value was stored as absent")
	}
	if got != "" {
		t.Errorf("value = %q", got)
	}
}

func TestSetRefusesToOverwriteASecretWithAPlainValue(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	err := h.run("set", "ALPHA_TOKEN", "oops")
	if err == nil {
		t.Fatal("set overwrote a secret with a plain value")
	}
	if !strings.Contains(err.Error(), "trenvy -p dev secret set ALPHA_TOKEN") {
		t.Errorf("error does not point at the right command: %v", err)
	}
	// The rejected value must not appear anywhere.
	if strings.Contains(err.Error(), "oops") {
		t.Errorf("error echoed the value: %v", err)
	}
}

func TestSecretSetStoresInTheStoreNotTheManifest(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.opts.Stdin = strings.NewReader("s3cretvalue\n")

	if err := h.run("secret", "set", "ALPHA_TOKEN", "--stdin"); err != nil {
		t.Fatalf("secret set: %v", err)
	}

	got, err := h.mem.Get("trenvy/vpb/dev/ALPHA_TOKEN")
	if err != nil {
		t.Fatalf("store Get: %v", err)
	}
	if got != "s3cretvalue" {
		t.Errorf("stored = %q", got)
	}

	data := mustReadFile(t, config.ManifestPath("vpb"))
	if strings.Contains(data, "s3cretvalue") {
		t.Error("the secret value was written into the manifest")
	}
}

func TestSecretSetMarksANewVariableAsSecret(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.opts.Stdin = strings.NewReader("v\n")

	if err := h.run("secret", "set", "BRAND_NEW", "--stdin"); err != nil {
		t.Fatalf("secret set: %v", err)
	}
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	if !m.Vars["BRAND_NEW"].Secret {
		t.Error("the new variable was not marked secret")
	}
}

func TestSecretSetTrimsOnlyTheTrailingNewline(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.opts.Stdin = strings.NewReader("  padded value  \n")

	if err := h.run("secret", "set", "K", "--stdin"); err != nil {
		t.Fatal(err)
	}
	got, _ := h.mem.Get("trenvy/vpb/dev/K")
	if got != "  padded value  " {
		t.Errorf("stored = %q; leading and trailing spaces are part of a secret", got)
	}
}

func TestSecretRmDeletesFromTheStore(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "v")

	if err := h.run("secret", "rm", "ALPHA_TOKEN", "--yes"); err != nil {
		t.Fatalf("secret rm: %v", err)
	}
	if ok, _ := h.mem.Has("trenvy/vpb/dev/ALPHA_TOKEN"); ok {
		t.Error("the item is still in the store")
	}
	// The variable stays in the manifest; only its value is gone.
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	if _, ok := m.Vars["ALPHA_TOKEN"]; !ok {
		t.Error("secret rm removed the variable from the manifest")
	}
	if !strings.Contains(h.out.String(), "deleted ALPHA_TOKEN from the keyring") {
		t.Errorf("stdout = %q, want confirmation of a real deletion", h.out.String())
	}
}

// Against a profile with nothing stored, secret rm swallowed ErrNotFound and
// printed the same "deleted" line as a real deletion -- in a tool whose
// headline hazard is acting on the wrong profile, that tells a user who ran
// it against the wrong one that the secret is gone when it never existed
// there. The two cases must read differently.
func TestSecretRmDistinguishesNothingStoredFromARealDeletion(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	// ALPHA_TOKEN is declared but never seeded in the store.

	if err := h.run("secret", "rm", "ALPHA_TOKEN", "--yes"); err != nil {
		t.Fatalf("secret rm: %v", err)
	}
	out := h.out.String()
	if strings.Contains(out, "deleted ALPHA_TOKEN from the keyring") {
		t.Errorf("stdout claims a deletion that never happened: %q", out)
	}
	if !strings.Contains(out, "nothing to delete") {
		t.Errorf("stdout does not say nothing was stored: %q", out)
	}
}

func TestUnsetRemovesTheVariableAndItsSecret(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "v")

	if err := h.run("unset", "ALPHA_TOKEN", "--yes"); err != nil {
		t.Fatalf("unset: %v", err)
	}
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	if _, ok := m.Vars["ALPHA_TOKEN"]; ok {
		t.Error("the variable is still in the manifest")
	}
	if ok, _ := h.mem.Has("trenvy/vpb/dev/ALPHA_TOKEN"); ok {
		t.Error("the keyring item was left behind")
	}
}

// A value written while standing on a profile belongs to that profile. Writing
// the shared default instead would silently change every other profile too.
func TestSetWritesTheActiveProfilesOverride(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)

	if err := h.run("-p", "prod", "set", "APP_URL", "http://+:9090"); err != nil {
		t.Fatalf("set: %v", err)
	}

	got := reloadManifest(t, "vpb").Vars["APP_URL"]
	if got.Profiles["prod"] != "http://+:9090" {
		t.Errorf("prod override = %q, want the new value", got.Profiles["prod"])
	}
	if got.Value == nil || *got.Value != "http://+:8080" {
		t.Errorf("the shared default was modified: %v", got.Value)
	}
}

func TestSetSharedWritesTheDefaultValue(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)

	if err := h.run("-p", "prod", "set", "APP_URL", "http://+:7070", "--shared"); err != nil {
		t.Fatalf("set --shared: %v", err)
	}

	got := reloadManifest(t, "vpb").Vars["APP_URL"]
	if got.Value == nil || *got.Value != "http://+:7070" {
		t.Errorf("shared value = %v, want the new value", got.Value)
	}
	if _, ok := got.Profiles["prod"]; ok {
		t.Error("--shared wrote a profile override")
	}
}

// The success line must match what --shared's own flag help says it does:
// write the default used by every profile without its own value. "set %s
// for every profile" overclaims -- resolve.Value consults a profile's own
// override first, so any profile holding one is unaffected.
func TestSetSharedMessageMatchesItsOwnSemantics(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	if err := h.run("set", "APP_URL", "http://shared", "--shared"); err != nil {
		t.Fatalf("set --shared: %v", err)
	}
	out := h.out.String()
	if !strings.Contains(out, "without its own value") {
		t.Errorf("success message does not match --shared's help text: %q", out)
	}
}

// Reproduces the bug directly: a profile with its own override is unaffected
// by --shared, so the success line must name it as such rather than
// implying every profile just changed.
func TestSetSharedNamesProfilesWithTheirOwnOverrideAsUnaffected(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	if err := h.run("-p", "prod", "set", "APP_URL", "http://prod-only"); err != nil {
		t.Fatalf("set prod override: %v", err)
	}

	if err := h.run("set", "APP_URL", "http://shared-default", "--shared"); err != nil {
		t.Fatalf("set --shared: %v", err)
	}
	out := h.out.String()
	if !strings.Contains(out, "prod") {
		t.Errorf("success message does not name the unaffected profile: %q", out)
	}
	if strings.Contains(out, "set APP_URL for every profile\n") {
		t.Errorf("success message still claims every profile changed: %q", out)
	}
}

// set is the one writer that takes a value in argv, so it is the one writer
// that must never touch a secret: the value is already in the process table
// and the shell history by the time the command runs.
func TestSetRefusesASecretVariable(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	err := h.run("set", "ALPHA_TOKEN", "hunter2")

	if err == nil {
		t.Fatal("set wrote to a secret variable")
	}
	if !strings.Contains(err.Error(), "trenvy -p dev secret set ALPHA_TOKEN") {
		t.Errorf("error does not point at the safe command: %v", err)
	}
}

// The suggested command must reproduce the invocation that would actually
// work: under -p prod, a bare `trenvy secret set` would store the value
// under whichever profile happens to be active, not the one this refusal is
// actually about.
func TestSetSecretRefusalNamesTheActiveProfile(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)

	err := h.run("-p", "prod", "set", "ALPHA_TOKEN", "hunter2")

	if err == nil {
		t.Fatal("set wrote to a secret variable")
	}
	if !strings.Contains(err.Error(), "trenvy -p prod secret set ALPHA_TOKEN") {
		t.Errorf("error does not name the active profile: %v", err)
	}
}

// manifest.Validate() would also reject this -- a secret may not carry a
// Value -- but its message describes the manifest, not what the user should
// do about it. The explicit guard in set is what points at "trenvy secret
// set", so the assertion checks for that message specifically: deleting the
// guard and falling back to Validate's rejection alone must fail this test.
func TestSetSharedAlsoRefusesASecretVariable(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	err := h.run("set", "ALPHA_TOKEN", "hunter2", "--shared")

	if err == nil {
		t.Fatal("set --shared wrote to a secret variable")
	}
	if !strings.Contains(err.Error(), "trenvy -p dev secret set ALPHA_TOKEN") {
		t.Errorf("error does not point at the safe command: %v", err)
	}
}

// A sample .env file describes the project, not one environment, so an import
// seeds the shared default rather than whichever profile happens to be active.
func TestImportWritesTheSharedDefault(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	sample := filepath.Join(h.root, "sample.env")
	if err := os.WriteFile(sample, []byte("NEW_VAR=hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := h.run("-p", "prod", "import", sample, "--no-tui"); err != nil {
		t.Fatalf("import: %v", err)
	}

	got := reloadManifest(t, "vpb").Vars["NEW_VAR"]
	if got.Value == nil || *got.Value != "hello" {
		t.Errorf("value = %v, want the shared default", got.Value)
	}
	if len(got.Profiles) != 0 {
		t.Errorf("import wrote profile overrides: %v", got.Profiles)
	}
}

// secret rm drops one profile's value; unset drops the variable and every
// profile's value. A prompt that does not say which is a prompt that gets
// approved without being read.
func TestSecretRmPromptNamesOnlyTheActiveProfile(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	h.opts.Stdin = strings.NewReader("n\n")

	_ = h.run("secret", "rm", "ALPHA_TOKEN")

	if !strings.Contains(h.errb.String(), `profile "dev"`) {
		t.Errorf("prompt does not name the profile:\n%s", h.errb)
	}
}

func TestUnsetPromptNamesEveryProfileItWillClear(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	h.opts.Stdin = strings.NewReader("n\n")

	_ = h.run("unset", "ALPHA_TOKEN")

	if !strings.Contains(h.errb.String(), "2 profiles") {
		t.Errorf("prompt does not state the blast radius:\n%s", h.errb)
	}
}

func TestUnsetDeletesTheSecretInEveryProfile(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "d")
	h.mem.Seed("trenvy/vpb/prod/ALPHA_TOKEN", "p")

	if err := h.run("unset", "ALPHA_TOKEN", "--yes"); err != nil {
		t.Fatalf("unset: %v", err)
	}

	calls := strings.Join(h.mem.Calls(), " ")
	for _, want := range []string{
		"Delete trenvy/vpb/dev/ALPHA_TOKEN",
		"Delete trenvy/vpb/prod/ALPHA_TOKEN",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %q in %v", want, h.mem.Calls())
		}
	}
	// unset's own prompt promised "delete its secret in 2 profiles"; the
	// success line must say how many it actually cleared, the same way
	// profile rm and project rm report their counts.
	if !strings.Contains(h.out.String(), "removed ALPHA_TOKEN (2 secrets deleted)") {
		t.Errorf("stdout = %q, want it to report how many secrets were cleared", h.out.String())
	}
}

// A declared secret with no value stored under some profile is the ordinary
// case, not a failure -- most projects only fill in a secret under one
// profile at a time. unset's per-profile loop must tolerate store.ErrNotFound
// on an individual item rather than abort, the same property already pinned
// for profile rm's loop (TestProfileRmDeletesTheProfilesSecretsAndOverrides
// in profile_test.go).
func TestUnsetToleratesASecretNeverStoredInSomeProfiles(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	// Only dev has ever had a value stored; prod is declared but unseeded --
	// its delete must return store.ErrNotFound rather than block the rest.
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "d")

	if err := h.run("unset", "ALPHA_TOKEN", "--yes"); err != nil {
		t.Fatalf("unset: %v", err)
	}

	calls := strings.Join(h.mem.Calls(), " ")
	for _, want := range []string{
		"Delete trenvy/vpb/dev/ALPHA_TOKEN",
		"Delete trenvy/vpb/prod/ALPHA_TOKEN",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %q in %v", want, h.mem.Calls())
		}
	}
	// prod's delete hit ErrNotFound and must not inflate the count: only
	// dev's item was ever actually removed.
	if !strings.Contains(h.out.String(), "removed ALPHA_TOKEN (1 secrets deleted)") {
		t.Errorf("stdout = %q, want it to report exactly 1 secret actually deleted", h.out.String())
	}
}

// The manifest is not touched until every profile's keyring item is gone, so
// a failure partway through the loop must leave it intact and say so -- the
// same property already pinned for project rm's keyring loop
// (TestProjectRmKeepsTheManifestWhenAKeyringDeleteFails in project_test.go).
func TestUnsetKeepsTheManifestWhenAKeyringDeleteFails(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	attempts := 0
	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		return failingDeleteStore{Store: h.mem, attempts: &attempts}, nil
	}

	err := h.run("unset", "ALPHA_TOKEN", "--yes")
	if err == nil {
		t.Fatal("unset succeeded despite a failing keyring delete")
	}
	if attempts == 0 {
		t.Error("no keyring delete was attempted")
	}
	// The failure hits the very first profile in the loop, so truthfully
	// zero profiles' items were deleted by the time it happened -- and the
	// error must not overstate that.
	if !strings.Contains(err.Error(), "0 profiles") {
		t.Errorf("error does not truthfully report zero profiles cleared: %v", err)
	}

	got := reloadManifest(t, "vpb")
	if _, ok := got.Vars["ALPHA_TOKEN"]; !ok {
		t.Error("the variable was removed from the manifest despite the failure")
	}
}

// A delete that fails partway through has already destroyed the items before
// it, permanently -- telling the user nothing was removed is the kind of
// false reassurance that stops them from re-running and losing the rest, the
// same risk already fixed for project rm
// (TestProjectRmSaysWhatWasAlreadyDeletedWhenADeleteFails in project_test.go).
func TestUnsetSaysHowManyProfilesWereAlreadyClearedWhenADeleteFails(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod", "staging"}
	h.register("vpb", m)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "d")
	h.mem.Seed("trenvy/vpb/prod/ALPHA_TOKEN", "p")
	// dev and prod succeed; staging fails, so exactly 2 profiles' items are
	// already gone by the time the error is written.
	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		return failOnProfileStore{Store: h.mem, failOn: "staging"}, nil
	}

	err := h.run("unset", "ALPHA_TOKEN", "--yes")
	if err == nil {
		t.Fatal("unset succeeded despite a failing keyring delete")
	}
	if !strings.Contains(err.Error(), "2 profiles") {
		t.Errorf("error does not say how many profiles are already cleared: %v", err)
	}
	if !strings.Contains(err.Error(), "ALPHA_TOKEN") {
		t.Errorf("error does not name the variable still in the manifest: %v", err)
	}

	got := reloadManifest(t, "vpb")
	if _, ok := got.Vars["ALPHA_TOKEN"]; !ok {
		t.Error("the variable was removed from the manifest despite the failure")
	}
}

// failOnProfileStore fails a delete only for one profile's item, so a test
// can arrange a partial success (some profiles cleared, one not) rather than
// an all-or-nothing failure.
type failOnProfileStore struct {
	store.Store
	failOn string
}

func (f failOnProfileStore) Delete(service string) error {
	if strings.Contains(service, "/"+f.failOn+"/") {
		return errors.New("keyring is locked")
	}
	return f.Store.Delete(service)
}

func TestUnsetWithoutConfirmationRefuses(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.opts.Stdin = strings.NewReader("n\n")

	if err := h.run("unset", "APP_URL"); err == nil {
		t.Fatal("unset proceeded without confirmation")
	}
	m, _ := manifest.Load(config.ManifestPath("vpb"))
	if _, ok := m.Vars["APP_URL"]; !ok {
		t.Error("the variable was removed despite the refusal")
	}
}

// A name that is not a shell identifier is perfectly legal in a json output
// and cannot work in a dotenv one. The warning fires only when some dotenv
// output actually selects it, so a json-only project is never nagged.
func TestSetWarnsWhenTheNameCannotReachADotenvOutput(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	h.setOutputs(t, "p", map[string]manifest.Output{
		"docker": {Format: manifest.FormatDotenv, Path: ".env"},
	}, "docker")

	if err := h.run("set", "feature.flag", "yes"); err != nil {
		t.Fatalf("set must not refuse the name: %v", err)
	}

	if _, ok := h.manifest(t, "p").Vars["feature.flag"]; !ok {
		t.Error("the warning blocked the write; it must only warn")
	}
	warn := h.errb.String()
	if !strings.Contains(warn, "feature.flag") || !strings.Contains(warn, "docker") {
		t.Errorf("warning must name the variable and the output it cannot reach:\n%s", warn)
	}
}

func TestSetDoesNotWarnForAJSONOnlyProject(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	h.setOutputs(t, "p", map[string]manifest.Output{
		"cfg": {Format: manifest.FormatJSON, Path: "cfg.json"},
	}, "cfg")

	if err := h.run("set", "feature.flag", "yes"); err != nil {
		t.Fatal(err)
	}

	if w := h.errb.String(); w != "" {
		t.Errorf("a json output takes any key; nothing to warn about:\n%s", w)
	}
}

func TestSetDoesNotWarnForAShellLegalName(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	h.setOutputs(t, "p", map[string]manifest.Output{
		"docker": {Format: manifest.FormatDotenv, Path: ".env"},
	}, "docker")

	if err := h.run("set", "auth0ClientID", "x"); err != nil {
		t.Fatal(err)
	}

	if w := h.errb.String(); w != "" {
		t.Errorf("auth0ClientID is a valid shell identifier:\n%s", w)
	}
}

// An output that excludes the variable cannot be broken by it.
func TestSetDoesNotWarnWhenTheDotenvOutputExcludesTheName(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	h.setOutputs(t, "p", map[string]manifest.Output{
		"docker": {Format: manifest.FormatDotenv, Path: ".env", Exclude: []string{"feature.*"}},
	}, "docker")

	if err := h.run("set", "feature.flag", "yes"); err != nil {
		t.Fatal(err)
	}

	if w := h.errb.String(); w != "" {
		t.Errorf("the output excludes it, so it cannot reach the file:\n%s", w)
	}
}

func twoProfileSecretManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
profiles = ["dev", "prod"]
default_profile = "dev"
output = "docker"
salt = "abc123"

[outputs.docker]
format = "dotenv"
path = "src/.env"
include = ["*"]

[vars.TOKEN]
secret = true

[vars.PLAIN]
value = "plain"
`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// failOnDeleteStore fails the Delete of exactly one service name and leaves
// every other operation to the embedded store.
type failOnDeleteStore struct {
	*store.Mem
	failOn string
}

func (f failOnDeleteStore) Delete(service string) error {
	if service == f.failOn {
		return errors.New("keyring is locked")
	}
	return f.Mem.Delete(service)
}

func TestSecretDemoteWritesEqualValuesAsShared(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", twoProfileSecretManifest(t))
	h.mem.Set("trenvy/vpb/dev/TOKEN", "same")
	h.mem.Set("trenvy/vpb/prod/TOKEN", "same")
	if err := h.run("secret", "demote", "TOKEN", "--yes"); err != nil {
		t.Fatal(err)
	}
	v := h.manifest(t, "vpb").Vars["TOKEN"]
	if v.Secret || v.Value == nil || *v.Value != "same" || len(v.Profiles) != 0 {
		t.Errorf("TOKEN = %+v", v)
	}
	for _, p := range []string{"dev", "prod"} {
		if ok, _ := h.mem.Has("trenvy/vpb/" + p + "/TOKEN"); ok {
			t.Errorf("%s keyring item survived", p)
		}
	}
	if strings.Contains(h.out.String(), "same") {
		t.Errorf("printed a value: %q", h.out.String())
	}
}

func TestSecretDemoteKeepsDifferingValuesPerProfile(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", twoProfileSecretManifest(t))
	h.mem.Set("trenvy/vpb/dev/TOKEN", "d")
	h.mem.Set("trenvy/vpb/prod/TOKEN", "p")
	if err := h.run("secret", "demote", "TOKEN", "--yes"); err != nil {
		t.Fatal(err)
	}
	v := h.manifest(t, "vpb").Vars["TOKEN"]
	if v.Value != nil || v.Profiles["dev"] != "d" || v.Profiles["prod"] != "p" {
		t.Errorf("TOKEN = %+v", v)
	}
}

func TestSecretDemoteRefusals(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", twoProfileSecretManifest(t))
	if err := h.run("secret", "demote", "PLAIN", "--yes"); err == nil || !strings.Contains(err.Error(), "not a secret") {
		t.Errorf("plain: %v", err)
	}
	if err := h.run("secret", "demote", "TOKEN", "--yes"); err == nil || !strings.Contains(err.Error(), "nothing stored") {
		t.Errorf("empty: %v", err)
	}
	if !h.manifest(t, "vpb").Vars["TOKEN"].Secret {
		t.Error("a refused demote changed the manifest")
	}
}

func TestSecretDemoteSavesTheManifestBeforeDeleting(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", twoProfileSecretManifest(t))
	h.mem.Set("trenvy/vpb/dev/TOKEN", "d")
	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		return failOnDeleteStore{Mem: h.mem, failOn: "trenvy/vpb/dev/TOKEN"}, nil
	}
	err := h.run("secret", "demote", "TOKEN", "--yes")
	if err == nil {
		t.Fatal("demote succeeded with a failing delete")
	}
	if v := h.manifest(t, "vpb").Vars["TOKEN"]; v.Secret || v.Profiles["dev"] != "d" && (v.Value == nil || *v.Value != "d") {
		t.Errorf("the value was not saved before the failing delete: %+v", v)
	}
	if !strings.Contains(err.Error(), "deleted 0 of 1") {
		t.Errorf("error does not say how far it got: %v", err)
	}
}
