package cli_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/store"
	"github.com/cdescobedo/trenvy/internal/ui"
)

// markUISaved drives a model through w so tests can build a finished, saved
// model without going through a real terminal session -- mirrors edit_test.go's
// markSaved for internal/tui.
func markUISaved(m ui.Model) ui.Model {
	out, _ := m.Update(ui.KeyRune('w'))
	return out
}

// clearAndType opens the cell under the cursor with e, clears whatever
// beginEdit prefilled the buffer with, types newValue, and stages it with
// enter. Mirrors internal/ui/model_test.go's editCell: a plain cell's e
// prefills the buffer with its current value, so typing straight in without
// clearing first would append to it rather than replace it.
func clearAndType(m ui.Model, newValue string) ui.Model {
	m, _ = m.Update(ui.KeyRune('e'))
	for range m.Buffer() {
		m, _ = m.Update(ui.KeyType(tea.KeyBackspace))
	}
	m, _ = m.Update(keyRunes(newValue))
	m, _ = m.Update(keyEnter())
	return m
}

// failOnUIStore fails exactly one keyring operation, identified by the exact
// service name it is called with, whether that call is a Set or a Delete.
// This is what lets a test arrange a partial success -- some ops applied,
// one not -- for a batch that mixes writes and deletes, the way a staged
// delete-var (deletes) alongside a staged set-secret (a write) can.
type failOnUIStore struct {
	*store.Mem
	failOn string
}

func (f failOnUIStore) Set(service, value string) error {
	if service == f.failOn {
		return errors.New("keyring is locked")
	}
	return f.Mem.Set(service, value)
}

func (f failOnUIStore) Delete(service string) error {
	if service == f.failOn {
		return errors.New("keyring is locked")
	}
	return f.Mem.Delete(service)
}

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

// A secret genuinely missing from the keyring -- not merely unread under
// ui's always-quick pass -- is a real, actionable "unknown": classifyOutput's
// secretsMissing branch (unaffected by Fix 2) already inspects the file
// before deciding this, so it must still surface as a problem. The output
// path exists here (a stray file, no render record) specifically so this
// exercises secretsMissing rather than the never-rendered case, which is
// TestUiOverviewHasNoProblemForASecretBearingOutputNeverRendered's job.
func TestUiOverviewReportsUnknownForAMissingSecret(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t)) // docker output includes ALPHA_TOKEN (secret)
	h.writeSample(t, "src/.env", "FOO=bar\n")
	// ALPHA_TOKEN deliberately never seeded -- missing under dev.

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

// Fix 2 (CRITICAL): the case the test above deliberately sidestepped. Before
// the fix, classifyOutput's quick branch returned outputQuickSkipped for
// ANY output selecting a secret before ever calling guard.Inspect, so a
// secret-bearing output that had simply never been rendered -- the ordinary
// state of a fresh clone -- became a permanent, unfixable false problem:
// ui.go's outputStates always passes quick=true, so no user action inside
// `trenvy ui` could ever clear it. Running the Fix's own suggestion
// (`trenvy status`) proved nothing was wrong and did not clear it either.
func TestUiOverviewHasNoProblemForASecretBearingOutputNeverRendered(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t)) // docker output includes ALPHA_TOKEN (secret)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "v")
	// src/.env is never written at all -- not hand-edited, not rendered,
	// just absent, exactly like the plain-variable case above but with a
	// secret in the mix, which is what used to reach the quick-skip branch
	// before ever checking whether the file existed.

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
		t.Errorf("a never-rendered secret-bearing output produced a problem:\n%s", out)
	}
}

// Fix 2's harder case, and the one the live verification is built around: a
// secret-bearing output that HAS been rendered and IS fresh must also read
// as no problem, not just an absent one. classifyOutput's quick branch now
// inspects the file first: once it is neither absent, foreign, nor hand
// -edited, the one thing left unresolved -- a secret's value changing since
// the render, with the file otherwise untouched -- is invisible to any check
// that does not read it, so ui treats it as fine rather than as a permanent
// "unknown" nothing the user can do inside `trenvy ui` would ever clear.
// `trenvy status` (no --quick) remains the command that actually reads the
// value and can tell fresh from stale for real; see the final-fix-report for
// a live run of both side by side.
func TestUiOverviewHasNoProblemForARenderedFreshSecretBearingOutput(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t)) // docker output includes ALPHA_TOKEN (secret)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "s3cretvalue")
	if err := h.run("render", "docker"); err != nil {
		t.Fatalf("render: %v", err)
	}

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
		t.Errorf("a rendered, fresh, secret-bearing output produced a problem:\n%s", out)
	}
	// Fix wave round 2, concern 1: the empty problem list must not be
	// mistaken for "nothing is wrong" -- this is exactly the case ui could
	// not actually verify, so the healthy line must caveat it, naming
	// trenvy status.
	if !strings.Contains(out, "trenvy status") {
		t.Errorf("healthy line does not name trenvy status for an unverified output:\n%s", out)
	}
}

// The other half of concern 1: a project whose outputs carry no secrets at
// all must see the plain healthy line, no caveat -- nothing here is
// actually unverified, so naming trenvy status would be noise.
func TestUiOverviewHealthyLineStaysPlainWithNoSecretOutputs(t *testing.T) {
	h := newHarness(t)
	h.gitInit("public/env.json\n")
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
	if err := h.run("render", "web"); err != nil {
		t.Fatalf("render: %v", err)
	}

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
		t.Errorf("a rendered, fresh, secret-free output produced a problem:\n%s", out)
	}
	if strings.Contains(out, "trenvy status") {
		t.Errorf("healthy line names trenvy status despite no secret-bearing outputs:\n%s", out)
	}
}

// dbURLManifest builds a project with one variable, DB_URL, that has a value
// under "dev" only -- the exact shape of the live Fix 1 repro (dev set,
// prod missing) so pressing d then e on its prod cell either resurrects it
// with dev silently wiped, or correctly keeps dev intact.
func dbURLManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
profiles = ["dev", "prod"]
default_profile = "dev"
salt = "abc123"

[vars.DB_URL]
profiles = { dev = "postgres://dev" }
`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// The Fix 1 regression, order 1: d (stage delete) then e (edit the prod
// cell). Before the fix, this silently discarded the delete AND wiped dev --
// commitStaged's set-value branch read Vars["DB_URL"] after the delete had
// already removed it, reconstructing a Var with only prod's value. After the
// fix, e cancels the pending delete before staging the edit, so dev survives
// and DB_URL is never deleted at all.
func TestUiDeleteThenEditPreservesTheUntouchedProfile(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", dbURLManifest(t))

	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		mo, _ = mo.Update(ui.KeyRune('2')) // Variables tab; only row: DB_URL
		mo, _ = mo.Update(ui.KeyRune('d')) // stage delete
		mo, _ = mo.Update(ui.KeyRune('l')) // move to the prod column
		mo = clearAndType(mo, "postgres://prod")
		return markUISaved(mo), nil
	}

	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}

	m, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	v, ok := m.Vars["DB_URL"]
	if !ok {
		t.Fatalf("DB_URL was deleted -- want it kept, with the edit cancelling the delete")
	}
	if got := v.Profiles["dev"]; got != "postgres://dev" {
		t.Errorf("DB_URL profiles[dev] = %q, want the untouched value %q to survive", got, "postgres://dev")
	}
	if got := v.Profiles["prod"]; got != "postgres://prod" {
		t.Errorf("DB_URL profiles[prod] = %q, want the staged edit %q", got, "postgres://prod")
	}
}

// The Fix 1 regression, order 2: e (edit the prod cell) then d. Before the
// fix this already deleted DB_URL cleanly (dev included) -- the bug report's
// own "reverse order deletes correctly" -- but that still destroys dev, a
// value the user never edited, only because d happened to be pressed last.
// After the fix, d refuses to stage a delete over a row that already has a
// staged edit, so both orders converge on the same outcome: the edit lands,
// dev survives, and DB_URL is never deleted.
func TestUiEditThenDeletePreservesTheUntouchedProfile(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", dbURLManifest(t))

	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		mo, _ = mo.Update(ui.KeyRune('2')) // Variables tab; only row: DB_URL
		mo, _ = mo.Update(ui.KeyRune('l')) // move to the prod column first
		mo = clearAndType(mo, "postgres://prod")
		mo, _ = mo.Update(ui.KeyRune('d')) // must refuse: a staged edit exists
		return markUISaved(mo), nil
	}

	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}

	m, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	v, ok := m.Vars["DB_URL"]
	if !ok {
		t.Fatalf("DB_URL was deleted -- want d refused over the staged edit")
	}
	if got := v.Profiles["dev"]; got != "postgres://dev" {
		t.Errorf("DB_URL profiles[dev] = %q, want the untouched value %q to survive", got, "postgres://dev")
	}
	if got := v.Profiles["prod"]; got != "postgres://prod" {
		t.Errorf("DB_URL profiles[prod] = %q, want the staged edit %q", got, "postgres://prod")
	}
}

// A staged set-value must land in the manifest as a per-profile override,
// not as the shared Value -- editing one cell must not change what every
// other profile resolves to.
func TestUiCommitsStagedSetValueToManifest(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t)) // sorted rows: ALPHA_TOKEN, APP_URL

	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		mo, _ = mo.Update(ui.KeyRune('2')) // Variables tab
		mo, _ = mo.Update(ui.KeyRune('j')) // row 1: APP_URL (row 0 is the secret)
		mo = clearAndType(mo, "http://+:9999")
		return markUISaved(mo), nil
	}

	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}

	m, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	v := m.Vars["APP_URL"]
	if got, ok := v.Profiles["dev"]; !ok || got != "http://+:9999" {
		t.Errorf("APP_URL profiles[dev] = %q, ok=%v, want the staged value", got, ok)
	}
}

// A staged set-secret must reach the keyring under the right profile's
// service name, and must never reach the manifest -- the manifest never
// carries a secret's value.
func TestUiCommitsStagedSetSecretToKeyring(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t)) // row 0: ALPHA_TOKEN, secret, profile dev

	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		mo, _ = mo.Update(ui.KeyRune('2'))
		mo, _ = mo.Update(ui.KeyRune('e')) // Secret == true -> ModeEditSecret
		mo, _ = mo.Update(keyRunes("brand-new-secret"))
		mo, _ = mo.Update(keyEnter())
		return markUISaved(mo), nil
	}

	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}

	got, err := h.mem.Get("trenvy/vpb/dev/ALPHA_TOKEN")
	if err != nil {
		t.Fatalf("store Get: %v", err)
	}
	if got != "brand-new-secret" {
		t.Errorf("stored secret = %q, want the staged value", got)
	}
	if body := mustReadFile(t, config.ManifestPath("vpb")); strings.Contains(body, "brand-new-secret") {
		t.Error("a secret value reached the manifest")
	}
}

// s's promotion is only staged intent until w commits it (Tasks 1-2); this is
// the test that makes it real. APP_URL carries a shared Value with no
// per-profile override, so s opens the confirmation and y answers it, staging
// one set-secret per declared profile plus the row's promote-secret (see
// stagePromotion). Every profile's keyring item must hold the manifest's
// value, and the manifest entry must come out with secret = true and BOTH
// Value and Profiles cleared -- Validate (validate.go:94, :98) refuses a
// secret carrying either, so asserting only Secret would pass against a
// manifest that fails to load back. get is the cheapest way to catch a
// promotion that validates but silently resolves to nothing: it re-resolves
// APP_URL exactly as a secret created directly would.
func TestUiCommitsPromotionMovesTheSharedValueIntoTheKeyringAndFlipsTheManifest(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "staging", "prod"}
	h.register("vpb", m)

	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		mo, _ = mo.Update(ui.KeyRune('2')) // Variables tab
		mo, _ = mo.Update(ui.KeyRune('j')) // row 1: APP_URL (row 0 is the secret)
		mo, _ = mo.Update(ui.KeyRune('s')) // shared value -> confirm first
		mo, _ = mo.Update(ui.KeyRune('y')) // confirm the promotion
		return markUISaved(mo), nil
	}

	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}

	for _, p := range []string{"dev", "staging", "prod"} {
		svc := "trenvy/vpb/" + p + "/APP_URL"
		got, err := h.mem.Get(svc)
		if err != nil {
			t.Fatalf("store Get(%s): %v", svc, err)
		}
		if got != "http://+:8080" {
			t.Errorf("%s = %q, want the manifest's shared value", svc, got)
		}
	}

	m2, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	v := m2.Vars["APP_URL"]
	if !v.Secret {
		t.Error("APP_URL was not flipped to a secret")
	}
	if v.Value != nil {
		t.Errorf("APP_URL still carries a plain Value %q after promotion", *v.Value)
	}
	if len(v.Profiles) != 0 {
		t.Errorf("APP_URL still carries Profiles %v after promotion", v.Profiles)
	}
	if err := m2.Validate(); err != nil {
		t.Errorf("the promoted manifest fails Validate: %v", err)
	}

	if err := h.run("get", "APP_URL"); err != nil {
		t.Fatalf("get APP_URL after promotion: %v", err)
	}
	if got := strings.TrimSpace(h.out.String()); got != "http://+:8080" {
		t.Errorf("get APP_URL = %q, want the promoted value resolved back out of the keyring", got)
	}
}

// The ordering is the safety property: commitStaged writes the keyring
// before it saves the manifest, so a manifest save that fails afterward
// leaves the values already stored and the manifest still declaring the
// variable plain -- recoverable. A read-only manifest directory (phase 1's
// output-rename/rm tests use the same technique) forces exactly that save to
// fail without touching the keyring at all.
func TestUiPromotionStoresEveryProfileBeforeTheManifestSaveEvenWhenItFails(t *testing.T) {
	skipIfNotRestrictable(t)
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "staging", "prod"}
	h.register("vpb", m)

	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		mo, _ = mo.Update(ui.KeyRune('2'))
		mo, _ = mo.Update(ui.KeyRune('j')) // row 1: APP_URL
		mo, _ = mo.Update(ui.KeyRune('s'))
		mo, _ = mo.Update(ui.KeyRune('y'))
		return markUISaved(mo), nil
	}

	dir := filepath.Join(config.Dir(), "projects")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	defer restore()

	err := h.run("ui")
	if err == nil {
		t.Fatal("ui succeeded despite a read-only manifest directory")
	}

	for _, p := range []string{"dev", "staging", "prod"} {
		svc := "trenvy/vpb/" + p + "/APP_URL"
		got, getErr := h.mem.Get(svc)
		if getErr != nil {
			t.Fatalf("store Get(%s): %v -- the keyring write must land before the failed manifest save", svc, getErr)
		}
		if got != "http://+:8080" {
			t.Errorf("%s = %q, want the value already stored despite the manifest save failing", svc, got)
		}
	}

	restore()

	m2, err2 := manifest.Load(config.ManifestPath("vpb"))
	if err2 != nil {
		t.Fatal(err2)
	}
	v := m2.Vars["APP_URL"]
	if v.Secret {
		t.Error("the on-disk manifest was flipped to secret despite the manifest save failing -- the value is now stranded, declared nowhere")
	}
	if v.Value == nil || *v.Value != "http://+:8080" {
		t.Errorf("APP_URL's plain value = %v, want it intact -- that is the state a user can retry the promotion from", v.Value)
	}
}

// promote-secret must flip the manifest on its own -- it cannot ride along
// on a set-secret that happened to be staged in the same batch. A variable
// added and promoted before it is ever given a value (v.Value and
// v.Profiles both nil under every profile) stages zero set-secret changes
// (see stagePromotion) alongside its one promote-secret, so this is the case
// that only passes once commitStaged switches on ChangePromoteSecret itself.
func TestUiCommitsPromotionOfAFreshVariableWithNoValueAnywhere(t *testing.T) {
	h := newHarness(t)
	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
profiles = ["dev", "staging", "prod"]
default_profile = "dev"
salt = "abc123"
`))
	if err != nil {
		t.Fatal(err)
	}
	h.register("vpb", m)

	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		mo, _ = mo.Update(ui.KeyRune('2')) // Variables tab
		mo, _ = mo.Update(ui.KeyRune('a'))
		mo, _ = mo.Update(keyRunes("FRESH_SECRET"))
		mo, _ = mo.Update(keyEnter())      // stage add-var, the only row
		mo, _ = mo.Update(ui.KeyRune('s')) // promote before it has a value: no set-secret rides along
		return markUISaved(mo), nil
	}

	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}

	m2, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	v, ok := m2.Vars["FRESH_SECRET"]
	if !ok {
		t.Fatal("FRESH_SECRET is missing from the manifest")
	}
	if !v.Secret {
		t.Error("FRESH_SECRET was not flipped to a secret: the flip must come from promote-secret itself, not from a set-secret that was never staged")
	}
	if v.Value != nil {
		t.Errorf("FRESH_SECRET carries a plain Value %q", *v.Value)
	}
	if len(v.Profiles) != 0 {
		t.Errorf("FRESH_SECRET carries Profiles %v", v.Profiles)
	}
	if err := m2.Validate(); err != nil {
		t.Errorf("the promoted manifest fails Validate: %v", err)
	}
}

// This is the test that would catch the app.service(name) trap in the
// delete-var path: ALPHA_TOKEN is stored under all three profiles, and a
// buggy loop using app.service (which binds only the active profile, "dev")
// would delete dev's item three times while leaving staging's and prod's
// items behind -- orphaned secrets with no manifest entry, unfindable by
// anything. Asserting all three profiles individually is what makes that
// bug visible; asserting only "dev" would still pass.
func TestUiCommitsStagedDeleteVarAcrossEveryProfilesKeyringAndTheManifest(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "staging", "prod"}
	h.register("vpb", m)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "d")
	h.mem.Seed("trenvy/vpb/staging/ALPHA_TOKEN", "s")
	h.mem.Seed("trenvy/vpb/prod/ALPHA_TOKEN", "p")

	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		mo, _ = mo.Update(ui.KeyRune('2')) // row 0: ALPHA_TOKEN
		mo, _ = mo.Update(ui.KeyRune('d')) // stage delete-var
		return markUISaved(mo), nil
	}

	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}

	m2, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m2.Vars["ALPHA_TOKEN"]; ok {
		t.Error("the deleted variable is still in the manifest")
	}
	for _, p := range []string{"dev", "staging", "prod"} {
		svc := "trenvy/vpb/" + p + "/ALPHA_TOKEN"
		if ok, _ := h.mem.Has(svc); ok {
			t.Errorf("profile %q still has the deleted secret in the keyring: %s", p, svc)
		}
	}
}

// The whole batch model rests on this: staging every kind of change and
// quitting any way other than w must leave the manifest byte-identical and
// must never touch the keyring, including the Has calls the presence load
// itself makes -- only Set and Delete are checked, since Has is expected.
func TestUiQuitWithoutWWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	before := mustReadFile(t, config.ManifestPath("vpb"))

	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		mo, _ = mo.Update(ui.KeyRune('2'))
		mo, _ = mo.Update(ui.KeyRune('j')) // APP_URL
		mo, _ = mo.Update(ui.KeyRune('e'))
		mo, _ = mo.Update(keyRunes("should-not-land"))
		mo, _ = mo.Update(keyEnter()) // stage set-value

		mo, _ = mo.Update(ui.KeyRune('k')) // back to ALPHA_TOKEN
		mo, _ = mo.Update(ui.KeyRune('e'))
		mo, _ = mo.Update(keyRunes("should-not-store"))
		mo, _ = mo.Update(keyEnter()) // stage set-secret

		mo, _ = mo.Update(ui.KeyRune('a'))
		mo, _ = mo.Update(keyRunes("NEWVAR"))
		mo, _ = mo.Update(keyEnter()) // stage add-var

		mo, _ = mo.Update(ui.KeyRune('j'))
		mo, _ = mo.Update(ui.KeyRune('j'))
		mo, _ = mo.Update(ui.KeyRune('d')) // stage delete-var, on NEWVAR

		// w is deliberately never pressed.
		return mo, nil
	}

	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}

	if got := mustReadFile(t, config.ManifestPath("vpb")); got != before {
		t.Error("quitting without w changed the manifest")
	}
	for _, call := range h.mem.Calls() {
		if strings.HasPrefix(call, "Set ") || strings.HasPrefix(call, "Delete ") {
			t.Errorf("quitting without w touched the keyring: %v", h.mem.Calls())
		}
	}
}

// A keyring op that fails partway through a commit cannot be unwound, so the
// error must say exactly how far the run got. The delete-var here targets
// three profiles in manifest order (dev, staging, prod); dev's delete
// succeeds and staging's fails, so the count must read "1 of 3", never "0 of
// 3" or "2 of 3" -- and dev's item must actually be gone while prod's must
// not have been touched.
func TestUiPartialFailureReportsHowFarItGotAndLeavesTheManifestAlone(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "staging", "prod"}
	h.register("vpb", m)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "d")
	h.mem.Seed("trenvy/vpb/staging/ALPHA_TOKEN", "s")
	h.mem.Seed("trenvy/vpb/prod/ALPHA_TOKEN", "p")

	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		return failOnUIStore{Mem: h.mem, failOn: "trenvy/vpb/staging/ALPHA_TOKEN"}, nil
	}

	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		mo, _ = mo.Update(ui.KeyRune('2'))
		mo, _ = mo.Update(ui.KeyRune('d')) // stage delete-var on ALPHA_TOKEN
		return markUISaved(mo), nil
	}

	err := h.run("ui")
	if err == nil {
		t.Fatal("ui succeeded despite a failing keyring delete")
	}
	if !strings.Contains(err.Error(), "deleted 1 of 3 keyring items") {
		t.Errorf("error does not say how far the deletes got: %v", err)
	}
	// This batch never wrote a secret at all, so a "stored" clause here
	// would be exactly the "0 of 0" noise a reader has to filter out at the
	// worst possible moment.
	if strings.Contains(err.Error(), "stored") {
		t.Errorf("error mentions a store count for a batch with no stores: %v", err)
	}
	if !strings.Contains(err.Error(), "manifest was not saved") {
		t.Errorf("error does not say the manifest is unsaved: %v", err)
	}
	if !strings.Contains(err.Error(), "trenvy ui") {
		t.Errorf("error does not name the command that lets the user retry: %v", err)
	}
	// The session that staged this has already ended (RunUI already
	// returned by the time commitStaged runs), so nothing here can be a
	// resume -- "open ... again" must not read as "press w again".
	if strings.Contains(err.Error(), "press w") {
		t.Errorf("error implies the ended session can be resumed with w: %v", err)
	}

	m2, err2 := manifest.Load(config.ManifestPath("vpb"))
	if err2 != nil {
		t.Fatal(err2)
	}
	if _, ok := m2.Vars["ALPHA_TOKEN"]; !ok {
		t.Error("the manifest was saved despite the keyring delete failing")
	}
	if ok, _ := h.mem.Has("trenvy/vpb/dev/ALPHA_TOKEN"); ok {
		t.Error("dev's item should already be gone -- the run got at least that far")
	}
	if ok, _ := h.mem.Has("trenvy/vpb/prod/ALPHA_TOKEN"); !ok {
		t.Error("prod's item must not have been touched once staging's delete failed")
	}
}

// The failure above never wrote a secret, so it alone cannot tell a
// combined "N of M" count apart from two separate ones. This batch stages
// one store (SECOND_TOKEN) and one delete (ALPHA_TOKEN); the store succeeds
// and the delete fails, so a combined count would read "applied 1 of 2" --
// which reads as "one secret is safely stored" just as easily as "one
// secret was removed", and a reader cannot tell which from that number
// alone. The message must instead say "stored 1 of 1" and "deleted 0 of 1"
// as two figures, never folded into one.
func TestUiPartialFailureDistinguishesStoredFromDeletedCounts(t *testing.T) {
	h := newHarness(t)
	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
profiles = ["dev"]
default_profile = "dev"
salt = "abc123"

[vars.ALPHA_TOKEN]
secret = true

[vars.SECOND_TOKEN]
secret = true
`))
	if err != nil {
		t.Fatal(err)
	}
	h.register("vpb", m)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "old")

	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		return failOnUIStore{Mem: h.mem, failOn: "trenvy/vpb/dev/ALPHA_TOKEN"}, nil
	}

	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		mo, _ = mo.Update(ui.KeyRune('2'))
		mo, _ = mo.Update(ui.KeyRune('d')) // row 0: ALPHA_TOKEN -- stage delete-var
		mo, _ = mo.Update(ui.KeyRune('j')) // row 1: SECOND_TOKEN
		mo, _ = mo.Update(ui.KeyRune('e'))
		mo, _ = mo.Update(keyRunes("new-value"))
		mo, _ = mo.Update(keyEnter()) // stage set-secret
		return markUISaved(mo), nil
	}

	err = h.run("ui")
	if err == nil {
		t.Fatal("ui succeeded despite a failing keyring delete")
	}
	if !strings.Contains(err.Error(), "stored 1 of 1 secrets") {
		t.Errorf("error does not report the store count: %v", err)
	}
	if !strings.Contains(err.Error(), "deleted 0 of 1 keyring items") {
		t.Errorf("error does not report the delete count: %v", err)
	}
	if strings.Contains(err.Error(), "1 of 2") {
		t.Errorf("error blurs the two kinds into one combined count: %v", err)
	}

	got, getErr := h.mem.Get("trenvy/vpb/dev/SECOND_TOKEN")
	if getErr != nil {
		t.Fatalf("the successful store should still be there: %v", getErr)
	}
	if got != "new-value" {
		t.Errorf("stored secret = %q, want the value from the batch's store", got)
	}
	if ok, _ := h.mem.Has("trenvy/vpb/dev/ALPHA_TOKEN"); !ok {
		t.Error("the failed delete should have left ALPHA_TOKEN in place")
	}
}

// An add-var of a name that cannot pass Validate (no value, no profile
// value, not secret, not optional -- exactly what a bare "a" leaves behind)
// must be refused without saving the manifest. The set-secret staged
// alongside it proves the point in Five Things #4: that keyring write has
// already landed by the time Validate runs, and it is left in place rather
// than rolled back, because it is the benign half of a refused commit.
func TestUiAddVarFailingValidationRefusesTheManifestButLeavesTheBenignKeyringWrite(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		mo, _ = mo.Update(ui.KeyRune('2'))
		mo, _ = mo.Update(ui.KeyRune('e')) // ALPHA_TOKEN, secret
		mo, _ = mo.Update(keyRunes("new-secret-value"))
		mo, _ = mo.Update(keyEnter()) // stage set-secret

		mo, _ = mo.Update(ui.KeyRune('a'))
		mo, _ = mo.Update(keyRunes("EMPTYVAR"))
		mo, _ = mo.Update(keyEnter()) // stage add-var, with nothing else set on it
		return markUISaved(mo), nil
	}

	err := h.run("ui")
	if err == nil {
		t.Fatal("ui succeeded with an add-var that cannot validate")
	}
	if !strings.Contains(err.Error(), "EMPTYVAR") {
		t.Errorf("error does not name the invalid variable: %v", err)
	}

	m, loadErr := manifest.Load(config.ManifestPath("vpb"))
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if _, ok := m.Vars["EMPTYVAR"]; ok {
		t.Error("the invalid manifest was saved anyway")
	}

	got, getErr := h.mem.Get("trenvy/vpb/dev/ALPHA_TOKEN")
	if getErr != nil {
		t.Fatalf("the benign keyring write should still be there: %v", getErr)
	}
	if got != "new-secret-value" {
		t.Errorf("stored secret = %q, want the write that already landed", got)
	}
}

// Fix 3: a validation failure must say the rest of the batch is lost even
// when the batch touched no keyring op at all. Before the fix, the warning
// only printed on the branch guarded by commitProgress() != "" --
// TestUiAddVarFailingValidationRefusesTheManifestButLeavesTheBenignKeyringWrite
// above stages a keyring write alongside its invalid add-var, so it always
// took that branch and never exercised the bare `return err` beneath it.
// This one stages only a plain manifest edit (no keyring op at all)
// alongside the invalid add-var, so storedTotal and deletedTotal are both
// zero and progress is "" -- exactly the batch shape that used to fail
// silently about everything else it was carrying.
func TestUiCommitFailureWarnsEvenWithNoKeyringOps(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t)) // sorted rows: ALPHA_TOKEN, APP_URL

	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		mo, _ = mo.Update(ui.KeyRune('2'))
		mo, _ = mo.Update(ui.KeyRune('j')) // row 1: APP_URL, a plain variable
		mo = clearAndType(mo, "http://+:9999")

		mo, _ = mo.Update(ui.KeyRune('a'))
		mo, _ = mo.Update(keyRunes("EMPTYVAR"))
		mo, _ = mo.Update(keyEnter()) // stage add-var, with nothing else set on it
		return markUISaved(mo), nil
	}

	err := h.run("ui")
	if err == nil {
		t.Fatal("ui succeeded with an add-var that cannot validate")
	}
	if !strings.Contains(err.Error(), "EMPTYVAR") {
		t.Errorf("error does not name the invalid variable: %v", err)
	}
	if !strings.Contains(err.Error(), "everything else staged this session is gone") {
		t.Errorf("error does not warn that the rest of the batch (the APP_URL edit) is lost: %v", err)
	}

	m, loadErr := manifest.Load(config.ManifestPath("vpb"))
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if v := m.Vars["APP_URL"]; v.Profiles["dev"] == "http://+:9999" {
		t.Error("the APP_URL edit landed despite the whole manifest failing to validate")
	}
}

// Quitting with nothing staged at all must print the same "no changes
// written" acknowledgement edit.go uses for a cancelled session, rather than
// silently exiting.
func TestUiQuitWithNothingStagedSaysSo(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	h.opts.RunUI = func(mo ui.Model, _ func(string, string) (string, error)) (ui.Model, error) {
		return mo, nil
	}

	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}
	if !strings.Contains(h.out.String(), "no changes written") {
		t.Errorf("stdout = %q, want an acknowledgement that nothing was written", h.out.String())
	}
}
