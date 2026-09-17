package cli_test

import (
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/store"
	"github.com/cdescobedo/trenvy/internal/ui"
)

// trenvy ui must refuse outside a project, exactly like its siblings, rather
// than inventing its own message.
func TestUiOutsideAProjectFailsWithRegisterHint(t *testing.T) {
	h := newHarness(t)
	err := h.run("ui")
	if err == nil {
		t.Fatal("ui succeeded outside a project")
	}
	if !strings.Contains(err.Error(), "trenvy project init") {
		t.Errorf("error does not suggest project init: %v", err)
	}
}

// This is the test that catches the app.service(name) trap: it binds the
// ACTIVE profile, so calling it inside the presence loop would report dev's
// presence for prod too. ALPHA_TOKEN is seeded under dev only, so the two
// columns must read differently -- if they read the same, the loop used the
// active profile instead of its own.
func TestUiMatrixHasARowPerVariableAndAColumnPerProfile(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "v")

	var got *ui.Model
	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		got = &mo
		return mo, nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}
	if got == nil {
		t.Fatal("RunUI was never called")
	}

	v, _ := got.Update(ui.KeyRune('2')) // Variables tab
	out := v.View()

	if !strings.Contains(out, "dev") || !strings.Contains(out, "prod") {
		t.Errorf("matrix does not show both profile columns:\n%s", out)
	}

	var row string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "ALPHA_TOKEN") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("ALPHA_TOKEN row not found:\n%s", out)
	}
	if !strings.Contains(row, "stored") {
		t.Errorf("dev column does not show stored presence: %q", row)
	}
	if !strings.Contains(row, "MISSING") {
		t.Errorf("prod column does not show MISSING -- looks like every profile"+
			" reported dev's presence (the app.service trap): %q", row)
	}
}

// noGetStore fails the test the instant Get is called, rather than merely
// recording the call for a later check: presence must come from Has, which
// on a real macOS Keychain cannot raise an authorization prompt, while Get
// can. A load path that used Get would prompt once per secret per profile --
// exactly the "nobody opens this tool" failure mode this guards against.
type noGetStore struct {
	*store.Mem
	t *testing.T
}

func (s noGetStore) Get(service string) (string, error) {
	s.t.Fatalf("Get called during ui load for %q; presence must come from Has", service)
	return "", nil
}

func TestUiLoadsPresenceViaHasNeverGet(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "v")
	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		return noGetStore{Mem: h.mem, t: t}, nil
	}
	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		return mo, nil
	}

	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}
}

// The Overview's output problems must come from the same guard.Inspect path
// status.go already uses, not a second implementation: a hand-written file
// with no render record is FOREIGN by that path, and the ui command must
// report it identically.
//
// The output here references only a plain variable, deliberately: an output
// that also selects a secret short-circuits to "unknown" before guard.Inspect
// ever runs (see TestUiOverviewReportsUnknownForAnOutputWithASecret) -- ui
// always calls describeOutput with quick=true so that loading never calls
// Get, and this test's job is to prove the guard path itself is intact for
// the outputs it *does* reach.
func TestUiOverviewReportsOutputStateFromTheExistingGuardPath(t *testing.T) {
	h := newHarness(t)
	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
profiles = ["dev"]
default_profile = "dev"
output = "web"
salt = "abc123"

[outputs.web]
format = "json"
path = "public/env.json"
include = ["APP_URL"]

[vars.APP_URL]
value = "http://+:8080"
`))
	if err != nil {
		t.Fatal(err)
	}
	h.register("vpb", m)
	h.writeSample(t, "public/env.json", "{}\n") // no render record -> FOREIGN

	var got *ui.Model
	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		got = &mo
		return mo, nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}

	out := got.View() // default tab is Overview
	if !strings.Contains(out, "web is FOREIGN") {
		t.Errorf("overview does not flag the foreign output via guard.Inspect:\n%s", out)
	}
}

// An output that selects a secret must not cost a keyring Get just because
// the ui command opened: the presence loop's Has-only guarantee would be
// pointless if the output-freshness pass turned around and read the value
// anyway. describeOutput's own --quick branch is what ui relies on for this,
// and its "unknown (--quick)" must reach the Overview as the plain token
// "unknown" -- "--quick" is a trenvy status flag the ui user never typed and
// cannot type, and problem.go's Fix for it must name trenvy status, not
// trenvy render (rendering does not resolve not-knowing, and can fail
// outright on a hand-edited file).
func TestUiOverviewReportsUnknownForAnOutputWithASecret(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t)) // docker output includes ALPHA_TOKEN (secret)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "v")

	var got *ui.Model
	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		got = &mo
		return mo, nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}

	out := got.View()
	if !strings.Contains(out, "docker: freshness needs reading its secrets") {
		t.Errorf("overview does not report the secret-bearing output's Detail:\n%s", out)
	}
	if strings.Contains(out, "--quick") {
		t.Errorf("overview leaks the --quick flag name into the ui:\n%s", out)
	}
	if !strings.Contains(out, "-- trenvy status") {
		t.Errorf("overview does not fix the unknown output with trenvy status:\n%s", out)
	}
}

// The regression fix-round-2 exists for: describeOutput's "not rendered" text
// (guard.StatusAbsent, via the plain-output path) and ui.Problems' "absent"
// check never matched as plain strings, so any output that had simply never
// been rendered in this worktree -- the ordinary state of a fresh clone --
// became a permanent false alarm the first time anyone opened `trenvy ui`.
// classifyOutput's typed outputAbsent must reach ui.Problems as
// ui.StateAbsent, which Problems already knows to skip.
//
// The output here selects only a plain variable, deliberately: one that also
// selects a secret takes the quick-skip branch regardless of whether the
// file exists (TestUiOverviewReportsUnknownForAnOutputWithASecret covers
// that path), so it would not exercise the guard.StatusAbsent branch this
// test is pinned to.
func TestUiOverviewHasNoProblemForAnOutputNeverRendered(t *testing.T) {
	h := newHarness(t)
	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
profiles = ["dev"]
default_profile = "dev"
output = "web"
salt = "abc123"

[outputs.web]
format = "json"
path = "public/env.json"
include = ["APP_URL"]

[vars.APP_URL]
value = "http://+:8080"
`))
	if err != nil {
		t.Fatal(err)
	}
	h.register("vpb", m)
	// public/env.json is never written at all -- not hand-edited, not
	// rendered, just absent: the ordinary state of a fresh clone.

	var got *ui.Model
	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		got = &mo
		return mo, nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}

	out := got.View()
	if !strings.Contains(out, "No problems found") {
		t.Errorf("a never-rendered output produced a problem:\n%s", out)
	}
}
