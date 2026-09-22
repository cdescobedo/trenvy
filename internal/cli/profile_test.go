package cli_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/store"
)

func profileHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	m.Project.DefaultProfile = "dev"
	h.register("vpb", m)
	return h
}

// reloadManifest reads the manifest back from disk, so a test asserts on what
// was persisted rather than on an in-memory object the command mutated.
func reloadManifest(t *testing.T, id string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Load(config.ManifestPath(id))
	if err != nil {
		t.Fatalf("reload manifest: %v", err)
	}
	return m
}

func TestProfileWithNoSubcommandNamesTheActiveProfileAndItsSource(t *testing.T) {
	h := profileHarness(t)

	if err := h.run("profile"); err != nil {
		t.Fatalf("profile: %v", err)
	}

	out := h.out.String()
	if !strings.Contains(out, "dev") {
		t.Errorf("output does not name the active profile:\n%s", out)
	}
	if !strings.Contains(out, "(default)") {
		t.Errorf("output does not say where it came from:\n%s", out)
	}
}

func TestProfileNamesTheEnvVarAsItsSource(t *testing.T) {
	h := profileHarness(t)
	t.Setenv("TRENVY_PROFILE", "prod")

	if err := h.run("profile"); err != nil {
		t.Fatalf("profile: %v", err)
	}

	out := h.out.String()
	if !strings.Contains(out, "prod") || !strings.Contains(out, "TRENVY_PROFILE") {
		t.Errorf("output does not blame the environment variable:\n%s", out)
	}
}

// The other two sources (--profile, TRENVY_PROFILE) are transient: they only
// hold for the invocation or the shell that set them. `profile use` is the
// one source that persists to the state file, so it is the case a user
// actually hits on their next ordinary command in a fresh shell -- and it is
// exercised nowhere else in this file, which only drives the env var.
func TestProfileNamesTheActiveStateAsItsSource(t *testing.T) {
	h := profileHarness(t)
	if err := h.run("profile", "use", "prod"); err != nil {
		t.Fatalf("profile use: %v", err)
	}

	if err := h.run("profile"); err != nil {
		t.Fatalf("profile: %v", err)
	}

	out := h.out.String()
	if !strings.Contains(out, "prod") || !strings.Contains(out, "(active)") {
		t.Errorf("output does not name the persisted active profile:\n%s", out)
	}
}

// The active profile and the persisted default can differ, and a list that
// showed only one of them would hide the difference.
func TestProfileListMarksActiveAndDefaultSeparately(t *testing.T) {
	h := profileHarness(t)
	t.Setenv("TRENVY_PROFILE", "prod")

	if err := h.run("profile", "list"); err != nil {
		t.Fatalf("profile list: %v", err)
	}

	out := h.out.String()
	var devLine, prodLine string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "dev") {
			devLine = l
		}
		if strings.Contains(l, "prod") {
			prodLine = l
		}
	}
	if !strings.Contains(devLine, "default") {
		t.Errorf("dev is the persisted default but is not marked: %q", devLine)
	}
	if !strings.HasPrefix(strings.TrimSpace(prodLine), "*") {
		t.Errorf("prod is active but is not marked: %q", prodLine)
	}
}

// A row with no note (the common case: neither active nor default) must not
// leave trailing whitespace behind -- tabwriter pads a tab-terminated empty
// cell to the column width, and an earlier version of this command asked for
// exactly that cell.
func TestProfileListRendersNoTrailingWhitespace(t *testing.T) {
	h := profileHarness(t)
	t.Setenv("TRENVY_PROFILE", "prod")

	if err := h.run("profile", "list"); err != nil {
		t.Fatalf("profile list: %v", err)
	}

	want := "  dev  (default)\n* prod\n"
	if got := h.out.String(); got != want {
		t.Errorf("profile list =\n%q\nwant\n%q", got, want)
	}

	for _, line := range strings.Split(strings.TrimRight(h.out.String(), "\n"), "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Errorf("line has trailing whitespace: %q", line)
		}
	}
}

// The env-var test above already checks that active and default get
// distinct marks; this checks the same split for the source a user actually
// leaves behind after `profile use` -- the persisted state file, not a
// variable that vanishes when the shell exits.
func TestProfileListDistinguishesPersistedActiveFromDefault(t *testing.T) {
	h := profileHarness(t)
	if err := h.run("profile", "use", "prod"); err != nil {
		t.Fatalf("profile use: %v", err)
	}

	if err := h.run("profile", "list"); err != nil {
		t.Fatalf("profile list: %v", err)
	}

	want := "  dev  (default)\n* prod\n"
	if got := h.out.String(); got != want {
		t.Errorf("profile list =\n%q\nwant\n%q", got, want)
	}
}

func TestProfileAddDeclaresANewProfile(t *testing.T) {
	h := profileHarness(t)

	if err := h.run("profile", "add", "personal"); err != nil {
		t.Fatalf("profile add: %v", err)
	}

	got := reloadManifest(t, "vpb").Project.Profiles
	found := false
	for _, p := range got {
		if p == "personal" {
			found = true
		}
	}
	if !found {
		t.Errorf("profiles = %v, want personal among them", got)
	}
}

func TestProfileAddRejectsADuplicate(t *testing.T) {
	h := profileHarness(t)

	err := h.run("profile", "add", "dev")

	if err == nil || !strings.Contains(err.Error(), "dev") {
		t.Errorf("error = %v, want a complaint naming dev", err)
	}
}

func TestProfileAddRejectsAnInvalidName(t *testing.T) {
	h := profileHarness(t)

	err := h.run("profile", "add", "dev/local")

	if err == nil {
		t.Fatal("profile add accepted a name with a slash")
	}
	if got := reloadManifest(t, "vpb").Project.Profiles; len(got) != 2 {
		t.Errorf("profiles = %v, want the manifest untouched", got)
	}
}

// `profile use` used to emit `export TRENVY_PROFILE=<name>` on stdout for
// `eval` to consume. It now persists the selection to the state file and
// emits nothing on stdout at all: a bare stdout, not a replacement export,
// is what makes `eval "$(trenvy profile use x)"` -- if anyone still has it in
// a script from before this command persisted anything -- a harmless no-op
// rather than executing arbitrary confirmation text as shell commands.
func TestProfileUseWritesTheActiveProfileToStateAndEmitsNothingOnStdout(t *testing.T) {
	h := profileHarness(t)

	if err := h.run("profile", "use", "prod"); err != nil {
		t.Fatalf("profile use: %v", err)
	}

	if got := h.state(t, "vpb").Active; got != "prod" {
		t.Errorf("state Active = %q, want prod", got)
	}
	if h.out.Len() != 0 {
		t.Errorf("stdout = %q, want nothing emitted", h.out.String())
	}
}

// Confirmation goes to stderr, not stdout -- see the doc comment on
// newProfileUseCmd for why: stdout has to stay empty for the eval-safety
// property above, so anything a human needs to see goes to the one stream
// `$(...)` never captures.
func TestProfileUseReportsWhatItDidOnStderr(t *testing.T) {
	h := profileHarness(t)

	if err := h.run("profile", "use", "prod"); err != nil {
		t.Fatalf("profile use: %v", err)
	}

	if got := h.errb.String(); !strings.Contains(got, "prod") {
		t.Errorf("stderr = %q, want it to name the profile now active", got)
	}
}

// The migration hazard: TRENVY_PROFILE outranks the active selection `use`
// just persisted (resolve.Profile's precedence), so a user who still has it
// exported would otherwise see `use` appear to do nothing. This must survive
// even `eval "$(trenvy profile use x)"`, which is exactly why it goes to
// stderr -- `$(...)` never captures that stream, eval or not.
func TestProfileUseWarnsWhenTRENVY_PROFILEStillOverridesIt(t *testing.T) {
	h := profileHarness(t)
	t.Setenv("TRENVY_PROFILE", "dev")

	if err := h.run("profile", "use", "prod"); err != nil {
		t.Fatalf("profile use: %v", err)
	}

	if got := h.state(t, "vpb").Active; got != "prod" {
		t.Errorf("state Active = %q, want prod", got)
	}
	errOut := h.errb.String()
	if !strings.Contains(errOut, "TRENVY_PROFILE") {
		t.Errorf("stderr = %q, want it to name TRENVY_PROFILE", errOut)
	}
	if !strings.Contains(errOut, "unset TRENVY_PROFILE") {
		t.Errorf("stderr = %q, want it to name the fix: unset TRENVY_PROFILE", errOut)
	}
}

// The warning is conditional: printing it unconditionally would train users
// to ignore it.
func TestProfileUseSaysNothingAboutTRENVY_PROFILEWhenItIsUnset(t *testing.T) {
	h := profileHarness(t)

	if err := h.run("profile", "use", "prod"); err != nil {
		t.Fatalf("profile use: %v", err)
	}

	if strings.Contains(h.errb.String(), "TRENVY_PROFILE") {
		t.Errorf("stderr = %q, want no mention of TRENVY_PROFILE when it is not set", h.errb.String())
	}
}

func TestProfileUseClearEmptiesTheActiveSelection(t *testing.T) {
	h := profileHarness(t)
	if err := h.run("profile", "use", "prod"); err != nil {
		t.Fatalf("profile use: %v", err)
	}

	if err := h.run("profile", "use", "--clear"); err != nil {
		t.Fatalf("profile use --clear: %v", err)
	}

	if got := h.state(t, "vpb").Active; got != "" {
		t.Errorf("state Active = %q, want empty after --clear", got)
	}
}

func TestProfileUseClearWarnsWhenTRENVY_PROFILEIsSet(t *testing.T) {
	h := profileHarness(t)
	if err := h.run("profile", "use", "prod"); err != nil {
		t.Fatalf("profile use: %v", err)
	}
	t.Setenv("TRENVY_PROFILE", "dev")

	if err := h.run("profile", "use", "--clear"); err != nil {
		t.Fatalf("profile use --clear: %v", err)
	}

	if !strings.Contains(h.errb.String(), "unset TRENVY_PROFILE") {
		t.Errorf("stderr = %q, want it to name the fix: unset TRENVY_PROFILE", h.errb.String())
	}
}

// This is the whole reason --clear exists: a state file naming a profile the
// manifest no longer declares makes resolveApp's ordinary profile resolution
// fail for every command. If --clear routed through resolveApp the way the
// other profile subcommands do, the escape hatch would be unreachable in
// exactly the situation it exists for.
func TestProfileUseClearWorksWhenTheActiveProfileIsNoLongerDeclared(t *testing.T) {
	h := profileHarness(t)
	if err := h.run("profile", "use", "prod"); err != nil {
		t.Fatalf("profile use: %v", err)
	}
	// Simulate a state file left behind by a hand edit, or by an older
	// binary's `profile rm` (fixed below): prod is still recorded active but
	// no longer declared.
	m := reloadManifest(t, "vpb")
	m.Project.Profiles = []string{"dev"}
	if err := manifest.Save(config.ManifestPath("vpb"), m); err != nil {
		t.Fatal(err)
	}

	if err := h.run("profile", "use", "--clear"); err != nil {
		t.Fatalf("profile use --clear: %v", err)
	}

	if got := h.state(t, "vpb").Active; got != "" {
		t.Errorf("state Active = %q, want empty after --clear", got)
	}
}

func TestProfileUseClearRejectsAProfileNameArgument(t *testing.T) {
	h := profileHarness(t)

	err := h.run("profile", "use", "--clear", "prod")

	if err == nil {
		t.Fatal("profile use --clear accepted a profile name argument")
	}
}

func TestProfileUseRequiresANameOrClear(t *testing.T) {
	h := profileHarness(t)

	err := h.run("profile", "use")

	if err == nil {
		t.Fatal("profile use with no name and no --clear succeeded")
	}
}

// Checking one field survives is not enough: it cannot tell "never wrote"
// from "rewrote identically". Comparing the file's bytes can.
func TestProfileUseDoesNotTouchTheManifest(t *testing.T) {
	h := profileHarness(t)
	before, err := os.ReadFile(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}

	if err := h.run("profile", "use", "prod"); err != nil {
		t.Fatalf("profile use: %v", err)
	}

	after, err := os.ReadFile(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("manifest bytes changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// A bad name must fail before emitting anything, or an eval would export a
// profile that does not exist and every later command would fail confusingly.
func TestProfileUseRejectsAnUndeclaredProfileWithoutEmitting(t *testing.T) {
	h := profileHarness(t)

	err := h.run("profile", "use", "staging")

	if err == nil {
		t.Fatal("profile use accepted an undeclared profile")
	}
	if h.out.Len() != 0 {
		t.Errorf("stdout = %q, want nothing emitted", h.out)
	}
	if got := h.state(t, "vpb").Active; got != "" {
		t.Errorf("state Active = %q, want untouched", got)
	}
}

func TestProfileDefaultPersistsToTheManifest(t *testing.T) {
	h := profileHarness(t)

	if err := h.run("profile", "default", "prod"); err != nil {
		t.Fatalf("profile default: %v", err)
	}

	if got := reloadManifest(t, "vpb").Project.DefaultProfile; got != "prod" {
		t.Errorf("default_profile = %q, want prod", got)
	}
}

func TestProfileRmDeletesTheProfilesSecretsAndOverrides(t *testing.T) {
	h := profileHarness(t)
	m := reloadManifest(t, "vpb")
	v := m.Vars["APP_URL"]
	v.Profiles = map[string]string{"prod": "http://+:9090"}
	m.Vars["APP_URL"] = v
	// A second declared secret that is deliberately left unseeded under
	// prod: a declared secret with no value stored in a given profile is
	// the ordinary case, not the exception, and the rm path must tolerate
	// it (store.ErrNotFound) rather than abort on it.
	m.Vars["OTHER_API_KEY"] = manifest.Var{Secret: true}
	if err := manifest.Save(config.ManifestPath("vpb"), m); err != nil {
		t.Fatal(err)
	}
	h.mem.Seed("trenvy/vpb/prod/ALPHA_TOKEN", "k")
	// The same secret name under a different profile must survive rm: that
	// is the property the explicit per-name ServiceName call (rather than
	// app.service, which is bound to the active profile) exists to protect.
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "kept")

	if err := h.run("profile", "rm", "prod", "--yes"); err != nil {
		t.Fatalf("profile rm: %v", err)
	}

	got := reloadManifest(t, "vpb")
	for _, p := range got.Project.Profiles {
		if p == "prod" {
			t.Error("prod is still declared")
		}
	}
	if _, ok := got.Vars["APP_URL"].Profiles["prod"]; ok {
		t.Error("the prod override survived")
	}
	calls := strings.Join(h.mem.Calls(), " ")
	if !strings.Contains(calls, "Delete trenvy/vpb/prod/ALPHA_TOKEN") {
		t.Errorf("the prod secret was not deleted: %v", h.mem.Calls())
	}
	if !strings.Contains(calls, "Delete trenvy/vpb/prod/OTHER_API_KEY") {
		t.Errorf("the never-stored prod secret was not attempted: %v", h.mem.Calls())
	}
	if v, err := h.mem.Get("trenvy/vpb/dev/ALPHA_TOKEN"); err != nil || v != "kept" {
		t.Errorf("a different profile's same-named secret did not survive: %q, %v", v, err)
	}
	// Two secrets are declared but only one was ever stored under prod, so
	// the success line -- which counts actual deletions, not declarations --
	// must say 1, not 2.
	if out := h.out.String(); !strings.Contains(out, "(1 secrets deleted)") {
		t.Errorf("stdout = %q, want it to report 1 secret actually deleted", out)
	}
}

// A delete that fails partway through has already destroyed the items
// before it, permanently. The other three partial-failure paths (unset,
// project rm) say how many items are already gone and name the full
// service, not the bare variable name; profile rm's error dropped both.
func TestProfileRmSaysHowManyWereAlreadyDeletedWhenADeleteFails(t *testing.T) {
	h := profileHarness(t)
	m := reloadManifest(t, "vpb")
	m.Vars["OTHER_API_KEY"] = manifest.Var{Secret: true}
	if err := manifest.Save(config.ManifestPath("vpb"), m); err != nil {
		t.Fatal(err)
	}
	h.mem.Seed("trenvy/vpb/prod/ALPHA_TOKEN", "a")
	h.mem.Seed("trenvy/vpb/prod/OTHER_API_KEY", "b")
	// secretNames sorts, so ALPHA_TOKEN is deleted before OTHER_API_KEY
	// fails: one item really is gone by the time the error is written.
	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		return failOnStore{Store: h.mem, failOn: "OTHER_API_KEY"}, nil
	}

	err := h.run("profile", "rm", "prod", "--yes")
	if err == nil {
		t.Fatal("profile rm succeeded despite a failing keyring delete")
	}
	if !strings.Contains(err.Error(), "1 secrets were already deleted") {
		t.Errorf("error does not say how many secrets are already gone: %v", err)
	}
	if !strings.Contains(err.Error(), "trenvy/vpb/prod/OTHER_API_KEY") {
		t.Errorf("error does not name the failing service (svc, not the bare var name): %v", err)
	}
}

func TestProfileRmRefusesTheLastProfile(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev"}
	h.register("vpb", m)

	err := h.run("profile", "rm", "dev", "--yes")

	if err == nil || !strings.Contains(err.Error(), "last") {
		t.Errorf("error = %v, want a refusal naming it as the last profile", err)
	}
	// A refusal on a destructive command must be a hard no: nothing in the
	// keyring may be touched, even a read, before the refusal is returned.
	if calls := h.mem.Calls(); len(calls) != 0 {
		t.Errorf("keyring calls = %v, want none", calls)
	}
}

func TestProfileRmRefusesThePersistedDefault(t *testing.T) {
	h := profileHarness(t)

	err := h.run("profile", "rm", "dev", "--yes")

	if err == nil || !strings.Contains(err.Error(), "profile default") {
		t.Errorf("error = %v, want it to point at `profile default`", err)
	}
	if calls := h.mem.Calls(); len(calls) != 0 {
		t.Errorf("keyring calls = %v, want none", calls)
	}
}

func TestProfileRmRefusesTheActiveProfile(t *testing.T) {
	h := profileHarness(t)
	t.Setenv("TRENVY_PROFILE", "prod")

	err := h.run("profile", "rm", "prod", "--yes")

	if err == nil || !strings.Contains(err.Error(), "active") {
		t.Errorf("error = %v, want a refusal naming it as active", err)
	}
	if calls := h.mem.Calls(); len(calls) != 0 {
		t.Errorf("keyring calls = %v, want none", calls)
	}
}

// Amendment: `profile rm` used to leave Active untouched, so removing the
// profile it named left the state file pointing at something the manifest no
// longer declares -- the exact situation --clear exists to escape, reached
// with no hand edit at all. The ordinary "you can't remove your own active
// profile" guard above only catches the resolved profile; overriding it with
// --profile lets rm proceed on prod while state still names it active, which
// is exactly the gap this closes.
func TestProfileRmClearsActiveWhenItRemovesTheActiveProfile(t *testing.T) {
	h := profileHarness(t)
	if err := h.run("profile", "use", "prod"); err != nil {
		t.Fatalf("profile use: %v", err)
	}

	if err := h.run("--profile", "dev", "profile", "rm", "prod", "--yes"); err != nil {
		t.Fatalf("profile rm: %v", err)
	}

	if got := h.state(t, "vpb").Active; got != "" {
		t.Errorf("state Active = %q, want cleared after removing the profile it named", got)
	}
}

// Regression guard: rm must not clear Active when it removes some other
// profile.
func TestProfileRmLeavesActiveAloneWhenRemovingADifferentProfile(t *testing.T) {
	h := profileHarness(t)
	m := reloadManifest(t, "vpb")
	m.Project.Profiles = []string{"dev", "prod", "staging"}
	if err := manifest.Save(config.ManifestPath("vpb"), m); err != nil {
		t.Fatal(err)
	}
	if err := h.run("profile", "use", "prod"); err != nil {
		t.Fatalf("profile use: %v", err)
	}

	if err := h.run("--profile", "dev", "profile", "rm", "staging", "--yes"); err != nil {
		t.Fatalf("profile rm: %v", err)
	}

	if got := h.state(t, "vpb").Active; got != "prod" {
		t.Errorf("state Active = %q, want prod untouched", got)
	}
}
