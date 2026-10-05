package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
	"github.com/cdescobedo/trenvy/internal/store"
	"github.com/cdescobedo/trenvy/internal/ui"
)

// The tests in this file are the original interface's commit tests
// (ui_test.go), carried over before that interface is deleted. Each hands
// commitStaged the batch the original's keys staged, and keeps the
// original's assertions. Where the original asserted the old caller's own
// output -- "trenvy ui" in its redo sentence, "left in place" on its stdout
// -- that caller goes with the old interface: the first is dropped, and the
// second is read from commitStaged's result instead.

// portedFixture is cli_test's fixtureManifest: one profile, the default
// output docker selecting everything, a plain APP_URL and a secret
// ALPHA_TOKEN.
const portedFixture = `
[project]
id = "vpb"
profiles = ["dev"]
default_profile = "dev"
output = "docker"
salt = "abc123"

[outputs.docker]
format = "dotenv"
path = "src/.env"
include = ["*"]

[vars.APP_URL]
value = "http://+:8080"

[vars.ALPHA_TOKEN]
secret = true
`

// portedFixtureThreeProfiles is portedFixture declaring three profiles,
// the shape the originals built by setting m.Project.Profiles.
func portedFixtureThreeProfiles() string {
	return strings.Replace(portedFixture, `profiles = ["dev"]`, `profiles = ["dev", "staging", "prod"]`, 1)
}

// skipIfNotRestrictable is cli_test's helper of the same name: a read-only
// directory does not stop a write on windows or as root.
func skipIfNotRestrictable(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a read-only directory does not stop a write on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("a read-only directory does not stop a write when running as root")
	}
}

// recordRenders records each entry in the state file and returns a freshly
// resolved app that sees them, standing in for cli_test's recordRender.
func recordRenders(t *testing.T, opts *Options, app *appContext, entries ...config.RenderEntry) *appContext {
	t.Helper()
	for _, e := range entries {
		app.State.Record(e)
	}
	if err := app.State.Save(); err != nil {
		t.Fatal(err)
	}
	fresh, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	return fresh
}

func loadState(t *testing.T) *config.State {
	t.Helper()
	s, err := config.LoadState("vpb")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustCommit(t *testing.T, opts *Options, app *appContext, batch []ui.Change) commitResult {
	t.Helper()
	res, err := commitStaged(opts, app, batch)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return res
}

// dbURLSource is one variable, DB_URL, with a value under dev only -- the
// shape of the Fix 1 repro (dev set, prod missing).
const dbURLSource = `
[project]
id = "vpb"
profiles = ["dev", "prod"]
default_profile = "dev"
salt = "abc123"

[vars.DB_URL]
profiles = { dev = "postgres://dev" }
`

func assertDBURLKeptWithBothProfiles(t *testing.T, deletedMsg string) {
	t.Helper()
	v, ok := loadVPB(t).Vars["DB_URL"]
	if !ok {
		t.Fatal(deletedMsg)
	}
	if got := v.Profiles["dev"]; got != "postgres://dev" {
		t.Errorf("DB_URL profiles[dev] = %q, want the untouched value %q to survive", got, "postgres://dev")
	}
	if got := v.Profiles["prod"]; got != "postgres://prod" {
		t.Errorf("DB_URL profiles[prod] = %q, want the staged edit %q", got, "postgres://prod")
	}
}

// The Fix 1 regression, order 1: d then an edit of the prod cell. The edit
// cancels the pending delete (internal/ui's beginEdit; session.Staged.Add
// holds the same rule), so the batch is the edit alone, and dev survives.
func TestCommitDeleteThenEditPreservesTheUntouchedProfile(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), dbURLSource)
	var s session.Staged
	s.Add(ui.Change{Kind: ui.ChangeDeleteVar, Name: "DB_URL"})
	s.Add(ui.Change{Kind: ui.ChangeSetValue, Name: "DB_URL", Profile: "prod", Value: "postgres://prod"})
	mustCommit(t, opts, app, s.All())
	assertDBURLKeptWithBothProfiles(t, "DB_URL was deleted -- want it kept, with the edit cancelling the delete")
}

// The Fix 1 regression, order 2: an edit of the prod cell, then d. The
// original's d refused to stage a delete over a row with a staged edit, so
// the batch it committed was the edit alone. (The new interface resolves
// this order the other way -- a delete drops the row's other changes, see
// session.Staged.Add -- which is its own rule, tested there.)
func TestCommitEditThenDeletePreservesTheUntouchedProfile(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), dbURLSource)
	mustCommit(t, opts, app, []ui.Change{
		{Kind: ui.ChangeSetValue, Name: "DB_URL", Profile: "prod", Value: "postgres://prod"},
	})
	assertDBURLKeptWithBothProfiles(t, "DB_URL was deleted -- want d refused over the staged edit")
}

// A staged set-value lands as a per-profile override, not as the shared
// Value: editing one cell must not change what every other profile
// resolves to.
func TestCommitSetValueToManifest(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), portedFixture)
	mustCommit(t, opts, app, []ui.Change{
		{Kind: ui.ChangeSetValue, Name: "APP_URL", Profile: "dev", Value: "http://+:9999"},
	})
	v := loadVPB(t).Vars["APP_URL"]
	if got, ok := v.Profiles["dev"]; !ok || got != "http://+:9999" {
		t.Errorf("APP_URL profiles[dev] = %q, ok=%v, want the staged value", got, ok)
	}
}

// A staged set-secret reaches the keyring under the right profile's service
// name, and never the manifest.
func TestCommitSetSecretToKeyring(t *testing.T) {
	mem := store.NewMem()
	opts, app := newCommitAppFrom(t, mem, portedFixture)
	mustCommit(t, opts, app, []ui.Change{
		{Kind: ui.ChangeSetSecret, Name: "ALPHA_TOKEN", Profile: "dev", Value: "brand-new-secret"},
	})
	got, err := mem.Get("trenvy/vpb/dev/ALPHA_TOKEN")
	if err != nil {
		t.Fatalf("store Get: %v", err)
	}
	if got != "brand-new-secret" {
		t.Errorf("stored secret = %q, want the staged value", got)
	}
	body, err := os.ReadFile(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "brand-new-secret") {
		t.Error("a secret value reached the manifest")
	}
}

// promotionBatch is what s then y on APP_URL staged: one set-secret per
// profile holding the shared value, then the promote-secret.
func promotionBatch(app *appContext, name string) []ui.Change {
	return session.PromotionChanges(name, app.Manifest.Vars[name], app.Manifest.Project.Profiles)
}

// A promotion moves the shared value into every profile's keyring item and
// flips the manifest entry to secret with no Value and no Profiles --
// Validate refuses a secret carrying either. get re-resolves APP_URL the way
// a secret created directly would.
func TestCommitPromotionMovesTheSharedValue(t *testing.T) {
	mem := store.NewMem()
	opts, app := newCommitAppFrom(t, mem, portedFixtureThreeProfiles())
	mustCommit(t, opts, app, promotionBatch(app, "APP_URL"))

	for _, p := range []string{"dev", "staging", "prod"} {
		svc := "trenvy/vpb/" + p + "/APP_URL"
		got, err := mem.Get(svc)
		if err != nil {
			t.Fatalf("store Get(%s): %v", svc, err)
		}
		if got != "http://+:8080" {
			t.Errorf("%s = %q, want the manifest's shared value", svc, got)
		}
	}

	m2 := loadVPB(t)
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

	var out bytes.Buffer
	get := *opts
	get.Stdout = &out
	if err := Execute(get, []string{"get", "APP_URL"}); err != nil {
		t.Fatalf("get APP_URL after promotion: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "http://+:8080" {
		t.Errorf("get APP_URL = %q, want the promoted value resolved back out of the keyring", got)
	}
}

// The keyring is written before the manifest is saved, so a manifest save
// that fails leaves the values stored and the manifest still declaring the
// variable plain -- recoverable.
func TestCommitPromotionStoresEveryProfileBeforeTheManifestSave(t *testing.T) {
	skipIfNotRestrictable(t)
	mem := store.NewMem()
	opts, app := newCommitAppFrom(t, mem, portedFixtureThreeProfiles())
	batch := promotionBatch(app, "APP_URL")

	restore := lockDir(t, filepath.Join(config.Dir(), "projects"))
	if _, err := commitStaged(opts, app, batch); err == nil {
		t.Fatal("commit succeeded despite a read-only manifest directory")
	}

	for _, p := range []string{"dev", "staging", "prod"} {
		svc := "trenvy/vpb/" + p + "/APP_URL"
		got, getErr := mem.Get(svc)
		if getErr != nil {
			t.Fatalf("store Get(%s): %v -- the keyring write must land before the failed manifest save", svc, getErr)
		}
		if got != "http://+:8080" {
			t.Errorf("%s = %q, want the value already stored despite the manifest save failing", svc, got)
		}
	}

	restore()
	v := loadVPB(t).Vars["APP_URL"]
	if v.Secret {
		t.Error("the on-disk manifest was flipped to secret despite the manifest save failing -- the value is now stranded, declared nowhere")
	}
	if v.Value == nil || *v.Value != "http://+:8080" {
		t.Errorf("APP_URL's plain value = %v, want it intact -- that is the state a user can retry the promotion from", v.Value)
	}
}

// promote-secret flips the manifest on its own: a variable added and
// promoted before it has a value stages no set-secret to ride along.
func TestCommitPromotionOfAFreshVariable(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), `
[project]
id = "vpb"
profiles = ["dev", "staging", "prod"]
default_profile = "dev"
salt = "abc123"
`)
	batch := []ui.Change{{Kind: ui.ChangeAddVar, Name: "FRESH_SECRET"}}
	batch = append(batch, session.PromotionChanges("FRESH_SECRET", manifest.Var{}, app.Manifest.Project.Profiles)...)
	if len(batch) != 2 {
		t.Fatalf("setup: batch = %+v, want add-var and promote-secret only", batch)
	}
	mustCommit(t, opts, app, batch)

	m2 := loadVPB(t)
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

// delete-var removes the keyring item under every profile, not the active
// one three times: asserting each profile is what catches app.service.
func TestCommitDeleteVarAcrossEveryProfile(t *testing.T) {
	mem := store.NewMem()
	mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "d")
	mem.Seed("trenvy/vpb/staging/ALPHA_TOKEN", "s")
	mem.Seed("trenvy/vpb/prod/ALPHA_TOKEN", "p")
	opts, app := newCommitAppFrom(t, mem, portedFixtureThreeProfiles())
	mustCommit(t, opts, app, []ui.Change{{Kind: ui.ChangeDeleteVar, Name: "ALPHA_TOKEN"}})

	if _, ok := loadVPB(t).Vars["ALPHA_TOKEN"]; ok {
		t.Error("the deleted variable is still in the manifest")
	}
	for _, p := range []string{"dev", "staging", "prod"} {
		svc := "trenvy/vpb/" + p + "/ALPHA_TOKEN"
		if ok, _ := mem.Has(svc); ok {
			t.Errorf("profile %q still has the deleted secret in the keyring: %s", p, svc)
		}
	}
}

func TestCommitAddOutput(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), portedFixture)
	mustCommit(t, opts, app, []ui.Change{
		{Kind: ui.ChangeAddOutput, Name: "public", Format: "json", Path: "public/env.json"},
	})
	m := loadVPB(t)
	o, ok := m.Outputs["public"]
	if !ok {
		t.Fatal("the staged output was not added to the manifest")
	}
	if o.Format != "json" || o.Path != "public/env.json" {
		t.Errorf("output public = %+v, want format json, path public/env.json", o)
	}
	if _, ok := m.Outputs["docker"]; !ok {
		t.Error("the pre-existing output was lost")
	}
}

// uncheckGlobs is the membership editor's save after unchecking off on an
// output whose include is include: the globs session.WriteGlobs writes for
// that checkbox state, over the fixture's two variables.
func uncheckGlobs(t *testing.T, include []string, off string) (in, ex []string) {
	t.Helper()
	all := []string{"ALPHA_TOKEN", "APP_URL"}
	checked := session.Checked(include, nil, all)
	checked[off] = false
	return session.WriteGlobs(session.InferGlobMode(include), all, checked)
}

// set-globs lands on the existing output's Include/Exclude and leaves its
// format and path alone. The original unchecked the second variable,
// APP_URL.
func TestCommitSetGlobs(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), portedFixture)
	in, ex := uncheckGlobs(t, []string{"*"}, "APP_URL")
	mustCommit(t, opts, app, []ui.Change{{Kind: ui.ChangeSetGlobs, Name: "docker", Include: in, Exclude: ex}})

	o, ok := loadVPB(t).Outputs["docker"]
	if !ok {
		t.Fatal("docker was removed from the manifest")
	}
	if o.Format != "dotenv" || o.Path != "src/.env" {
		t.Errorf("set-globs touched format/path: %+v", o)
	}
	if len(o.Exclude) != 1 {
		t.Fatalf("docker.Exclude = %#v, want exactly one excluded name", o.Exclude)
	}
	if len(o.Include) != 0 {
		t.Errorf("docker.Include = %#v, want empty (everything-except mode)", o.Include)
	}
}

// A membership edit for an output added in the same batch still lands:
// phase 1 inserts it before phase 5 edits it. The original unchecked the
// first variable, ALPHA_TOKEN.
func TestCommitSetGlobsForAnAddedOutput(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), portedFixture)
	in, ex := uncheckGlobs(t, nil, "ALPHA_TOKEN")
	mustCommit(t, opts, app, []ui.Change{
		{Kind: ui.ChangeAddOutput, Name: "public", Format: "dotenv", Path: "public/.env"},
		{Kind: ui.ChangeSetGlobs, Name: "public", Include: in, Exclude: ex},
	})

	o, ok := loadVPB(t).Outputs["public"]
	if !ok {
		t.Fatal("the staged output was not added to the manifest")
	}
	if o.Format != "dotenv" || o.Path != "public/.env" {
		t.Errorf("public = %+v, want format dotenv, path public/.env", o)
	}
	if len(o.Exclude) != 1 {
		t.Errorf("public.Exclude = %#v, want exactly one excluded name (the membership edit did not land)", o.Exclude)
	}
}

// renameAndMembership is a rename of docker to prod-env and a membership
// edit unchecking APP_URL, both keyed by docker's identity.
func renameAndMembership(t *testing.T) (rename, globs ui.Change) {
	in, ex := uncheckGlobs(t, []string{"*"}, "APP_URL")
	return ui.Change{Kind: ui.ChangeRenameOutput, Name: "docker", Value: "prod-env"},
		ui.Change{Kind: ui.ChangeSetGlobs, Name: "docker", Include: in, Exclude: ex}
}

// Fix 1 (whole-branch review): a rename and a membership edit on one row
// both land whichever was staged first, because every rename runs in its
// own pass (5b) after every other output edit (5a).
func TestCommitRenameThenMembershipBothLand(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), portedFixture)
	rename, globs := renameAndMembership(t)
	mustCommit(t, opts, app, []ui.Change{rename, globs})
	assertRenameAndMembershipEditBothLanded(t)
}

// The order that already worked before the fix, pinned so a change to
// commitStaged cannot flip which order works.
func TestCommitMembershipThenRenameBothLand(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), portedFixture)
	rename, globs := renameAndMembership(t)
	mustCommit(t, opts, app, []ui.Change{globs, rename})
	assertRenameAndMembershipEditBothLanded(t)
}

func assertRenameAndMembershipEditBothLanded(t *testing.T) {
	t.Helper()
	m := loadVPB(t)
	if _, gone := m.Outputs["docker"]; gone {
		t.Error("the old name is still defined")
	}
	o, ok := m.Outputs["prod-env"]
	if !ok {
		t.Fatal("the rename did not land")
	}
	if o.Format != "dotenv" || o.Path != "src/.env" {
		t.Errorf("renamed output lost its facts: %+v", o)
	}
	if len(o.Exclude) != 1 || o.Exclude[0] != "APP_URL" {
		t.Errorf("prod-env.Exclude = %#v, want exactly [\"APP_URL\"] -- the membership edit did not land", o.Exclude)
	}
	if len(o.Include) != 0 {
		t.Errorf("prod-env.Include = %#v, want empty (everything-except mode)", o.Include)
	}
}

// twoOutputsPublicDefault has public as the default, so renaming docker and
// making it the default in one batch is a real, order-sensitive pair.
const twoOutputsPublicDefault = `
[project]
id = "vpb"
profiles = ["dev"]
default_profile = "dev"
output = "public"
salt = "abc123"

[outputs.docker]
format = "dotenv"
path = "src/.env"
include = ["*"]

[outputs.public]
format = "json"
path = "public/env.json"
include = ["*"]

[vars.APP_URL]
value = "http://+:8080"
`

var (
	defaultDocker = ui.Change{Kind: ui.ChangeDefaultOutput, Name: "docker"}
	renameDocker  = ui.Change{Kind: ui.ChangeRenameOutput, Name: "docker", Value: "prod-env"}
)

// The order that already worked before the fix, pinned so a change to
// commitStaged cannot flip which order works.
func TestCommitDefaultThenRenameBothLand(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), twoOutputsPublicDefault)
	mustCommit(t, opts, app, []ui.Change{defaultDocker, renameDocker})
	assertDockerRenamedAndDefault(t)
}

// default-output reads and writes project.output by the row's pre-rename
// identity, so D after r on one row must still land: applied in staged
// order, it would point project.output at a name the rename just moved.
func TestCommitRenameThenDefaultBothLand(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), twoOutputsPublicDefault)
	mustCommit(t, opts, app, []ui.Change{renameDocker, defaultDocker})
	assertDockerRenamedAndDefault(t)
}

func assertDockerRenamedAndDefault(t *testing.T) {
	t.Helper()
	m := loadVPB(t)
	if _, gone := m.Outputs["docker"]; gone {
		t.Error("the old name is still defined")
	}
	if _, ok := m.Outputs["prod-env"]; !ok {
		t.Fatal("the rename did not land")
	}
	if m.Project.Output != "prod-env" {
		t.Errorf("project.output = %q, want prod-env -- the default did not follow the rename", m.Project.Output)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("the committed manifest fails Validate: %v", err)
	}
}

// rename-output moves the manifest key and repoints project.output when the
// renamed row held it, as `trenvy output rename` does.
func TestCommitRenameOutputMovesKeyAndDefault(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), portedFixture)
	mustCommit(t, opts, app, []ui.Change{renameDocker})

	m := loadVPB(t)
	if _, gone := m.Outputs["docker"]; gone {
		t.Error("the old name is still defined")
	}
	o, ok := m.Outputs["prod-env"]
	if !ok {
		t.Fatal("the new name was not created")
	}
	if o.Format != "dotenv" || o.Path != "src/.env" {
		t.Errorf("renamed output lost its facts: %+v", o)
	}
	if m.Project.Output != "prod-env" {
		t.Errorf("project.output = %q, want prod-env", m.Project.Output)
	}
}

// remove-output clears project.output when the removed row held it, as
// `trenvy output rm` does.
func TestCommitRemoveOutputClearsTheDefault(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), portedFixture)
	mustCommit(t, opts, app, []ui.Change{{Kind: ui.ChangeRemoveOutput, Name: "docker"}})

	m := loadVPB(t)
	if _, ok := m.Outputs["docker"]; ok {
		t.Error("the removed output is still defined")
	}
	if m.Project.Output != "" {
		t.Errorf("project.output = %q, want cleared", m.Project.Output)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("the committed manifest fails Validate: %v", err)
	}
}

// remove-output forgets every render record under the removed name, in
// every worktree, and names each file left on disk. The original read
// those names from the old caller's stdout; they are res.leftInPlace.
func TestCommitRemoveOutputForgetsRecordsAndNamesFiles(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), portedFixture)
	worktreeA := app.Worktree
	worktreeB := filepath.Join(t.TempDir(), "other-worktree")
	app = recordRenders(t, opts, app,
		config.RenderEntry{Worktree: worktreeA, Output: "docker", Path: "src/.env"},
		config.RenderEntry{Worktree: worktreeB, Output: "docker", Path: "src/.env"})

	res := mustCommit(t, opts, app, []ui.Change{{Kind: ui.ChangeRemoveOutput, Name: "docker"}})

	st := loadState(t)
	if _, ok := st.Find(worktreeA, "docker"); ok {
		t.Error("the render record in worktree A survived the removal")
	}
	if _, ok := st.Find(worktreeB, "docker"); ok {
		t.Error("the render record in worktree B survived the removal")
	}

	left := strings.Join(res.leftInPlace, "\n")
	if !strings.Contains(left, worktreeA+"/src/.env") {
		t.Errorf("leftInPlace does not name worktree A's file left on disk: %q", res.leftInPlace)
	}
	if !strings.Contains(left, worktreeB+"/src/.env") {
		t.Errorf("leftInPlace does not name worktree B's file left on disk: %q", res.leftInPlace)
	}
}

func TestCommitDefaultOutputFlipsProjectOutput(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), `
[project]
id = "vpb"
profiles = ["dev"]
default_profile = "dev"
output = "docker"
salt = "abc123"

[outputs.docker]
format = "dotenv"
path = "src/.env"

[outputs.public]
format = "json"
path = "public/env.json"
`)
	mustCommit(t, opts, app, []ui.Change{{Kind: ui.ChangeDefaultOutput, Name: "public"}})
	if got := loadVPB(t).Project.Output; got != "public" {
		t.Errorf("project.output = %q, want public", got)
	}
}

// twoSecrets is two secrets under dev, for batches that store one and
// delete the other.
const twoSecrets = `
[project]
id = "vpb"
profiles = ["dev"]
default_profile = "dev"
salt = "abc123"

[vars.ALPHA_TOKEN]
secret = true

[vars.SECOND_TOKEN]
secret = true
`

// storeOneDeleteOther is what d on ALPHA_TOKEN then an edit of
// SECOND_TOKEN staged.
var storeOneDeleteOther = []ui.Change{
	{Kind: ui.ChangeDeleteVar, Name: "ALPHA_TOKEN"},
	{Kind: ui.ChangeSetSecret, Name: "SECOND_TOKEN", Profile: "dev", Value: "new-value"},
}

// Keyring writes before keyring deletes, checked on the store's own call
// log: every other assertion would stay green if the loops swapped.
func TestCommitKeyringWritesBeforeDeletes(t *testing.T) {
	mem := store.NewMem()
	mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "old")
	opts, app := newCommitAppFrom(t, mem, twoSecrets)
	mustCommit(t, opts, app, storeOneDeleteOther)

	calls := mem.Calls()
	setIdx, delIdx := -1, -1
	for i, c := range calls {
		if c == "Set trenvy/vpb/dev/SECOND_TOKEN" {
			setIdx = i
		}
		if c == "Delete trenvy/vpb/dev/ALPHA_TOKEN" {
			delIdx = i
		}
	}
	if setIdx == -1 || delIdx == -1 {
		t.Fatalf("expected both a Set and a Delete call, got %v", calls)
	}
	if setIdx > delIdx {
		t.Errorf("the keyring write happened after the delete, not before: %v", calls)
	}
}

// State file before manifest: a rename with a render record repoints the
// record before the manifest save is attempted, so a failed save leaves
// the old name defined for a retry and the record not FOREIGN.
func TestCommitRenameOutputWritesStateBeforeManifest(t *testing.T) {
	skipIfNotRestrictable(t)
	opts, app := newCommitAppFrom(t, store.NewMem(), portedFixture)
	app = recordRenders(t, opts, app, config.RenderEntry{Worktree: app.Worktree, Output: "docker", Path: "src/.env"})

	restore := lockDir(t, filepath.Join(config.Dir(), "projects"))
	if _, err := commitStaged(opts, app, []ui.Change{renameDocker}); err == nil {
		t.Fatal("commit succeeded despite a read-only manifest directory")
	}

	if _, ok := loadState(t).Find(app.Worktree, "prod-env"); !ok {
		t.Fatal("state was not updated before the manifest save failed; " +
			"the rendered file would now read FOREIGN")
	}

	restore()
	if _, ok := loadVPB(t).Outputs["docker"]; !ok {
		t.Error("the old name should still be defined so a retry can find it")
	}
}

// Additions before removals: an output added and removed in one session
// never reaches the manifest. The original's keys staged both changes, and
// commitStaged must net them by applying the removal after the addition.
// session.Staged nets them before commit ever sees them; both are checked,
// because the netted batch alone is empty and would pass against any
// commitStaged at all.
func TestCommitAddThenRemoveSameOutputNeverLands(t *testing.T) {
	const noOutputs = `
[project]
id = "vpb"
profiles = ["dev"]
default_profile = "dev"
salt = "abc123"
`
	add := ui.Change{Kind: ui.ChangeAddOutput, Name: "temp", Format: "dotenv", Path: "temp/.env"}
	remove := ui.Change{Kind: ui.ChangeRemoveOutput, Name: "temp"}

	t.Run("as the original staged it", func(t *testing.T) {
		opts, app := newCommitAppFrom(t, store.NewMem(), noOutputs)
		mustCommit(t, opts, app, []ui.Change{add, remove})
		if _, ok := loadVPB(t).Outputs["temp"]; ok {
			t.Error("an output added and removed in the same session was resurrected in the manifest")
		}
	})

	t.Run("through session.Staged", func(t *testing.T) {
		opts, app := newCommitAppFrom(t, store.NewMem(), noOutputs)
		var s session.Staged
		s.Add(add)
		s.Add(remove)
		if n := s.Len(); n != 0 {
			t.Errorf("staged %d changes for an output added and removed in one session, want none: %+v", n, s.All())
		}
		if _, err := commitStaged(opts, app, s.All()); err != nil {
			t.Fatalf("commit: %v", err)
		}
		if _, ok := loadVPB(t).Outputs["temp"]; ok {
			t.Error("an output added and removed in the same session was resurrected in the manifest")
		}
	})
}

// secretAndRename is an edit of ALPHA_TOKEN (a keyring write that lands
// before any later failure) and a rename of docker (a legal batch, so
// Validate passes).
var secretAndRename = []ui.Change{
	{Kind: ui.ChangeSetSecret, Name: "ALPHA_TOKEN", Profile: "dev", Value: "new-secret-value"},
	renameDocker,
}

// assertSecretLandedAndDockerKept is the shared tail of the two
// save-failure tests: the keyring write stays, the manifest is untouched.
func assertSecretLandedAndDockerKept(t *testing.T, mem *store.Mem, manifestMsg string) {
	t.Helper()
	got, err := mem.Get("trenvy/vpb/dev/ALPHA_TOKEN")
	if err != nil {
		t.Fatalf("the keyring write should have already landed: %v", err)
	}
	if got != "new-secret-value" {
		t.Errorf("stored secret = %q, want the write that already landed", got)
	}
	if _, ok := loadVPB(t).Outputs["docker"]; !ok {
		t.Error(manifestMsg)
	}
}

// A state save that fails reports how far the run got in partialCommitError's
// shape, including the keyring write that already landed. The original also
// checked for "trenvy ui", the old caller's redo sentence, which goes with it.
func TestCommitPartialFailureWhenStateSaveFails(t *testing.T) {
	skipIfNotRestrictable(t)
	mem := store.NewMem()
	opts, app := newCommitAppFrom(t, mem, portedFixture)
	dir := filepath.Join(config.Dir(), "state")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	restore := lockDir(t, dir)

	_, err := commitStaged(opts, app, secretAndRename)
	if err == nil {
		t.Fatal("commit succeeded despite a read-only state directory")
	}
	if !strings.Contains(err.Error(), "stored 1 of 1 secrets") {
		t.Errorf("error does not report the keyring write that already landed: %v", err)
	}
	if !strings.Contains(err.Error(), "manifest was not saved") {
		t.Errorf("error does not say the manifest is unsaved: %v", err)
	}

	restore()
	assertSecretLandedAndDockerKept(t, mem, "the manifest was changed despite the state save failing")
}

// Fix 3: a manifest save that fails after real progress reports that
// progress, not a bare "permission denied".
func TestCommitPartialFailureWhenManifestSaveFails(t *testing.T) {
	skipIfNotRestrictable(t)
	mem := store.NewMem()
	opts, app := newCommitAppFrom(t, mem, portedFixture)
	restore := lockDir(t, filepath.Join(config.Dir(), "projects"))

	_, err := commitStaged(opts, app, secretAndRename)
	if err == nil {
		t.Fatal("commit succeeded despite a read-only manifest directory")
	}
	if !strings.Contains(err.Error(), "stored 1 of 1 secrets") {
		t.Errorf("error does not report the keyring write that already landed: %v", err)
	}
	if !strings.Contains(err.Error(), "manifest was not saved") {
		t.Errorf("error does not say the manifest is unsaved: %v", err)
	}

	restore()
	assertSecretLandedAndDockerKept(t, mem, "the manifest was changed despite the save failing")
}

// The whole batch is validated first (spec 2026-09-30 §3): a rename's
// render-record repoint is not saved for a batch the manifest refuses (an
// illegal output path here), and the error says nothing was written.
func TestCommitValidationFailureLeavesStateUntouched(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), portedFixture)
	app = recordRenders(t, opts, app, config.RenderEntry{Worktree: app.Worktree, Output: "docker", Path: "src/.env"})

	_, err := commitStaged(opts, app, []ui.Change{
		renameDocker,
		{Kind: ui.ChangeAddOutput, Name: "bad", Format: "dotenv", Path: "../escape"},
	})
	if err == nil {
		t.Fatal("commit succeeded with an illegal output path")
	}
	if !strings.Contains(err.Error(), "nothing was written") {
		t.Errorf("error does not say nothing was written: %v", err)
	}

	st := loadState(t)
	if _, ok := st.Find(app.Worktree, "docker"); !ok {
		t.Error("the render record was repointed for a batch the manifest refuses")
	}
	if _, ok := st.Find(app.Worktree, "prod-env"); ok {
		t.Error("the state file was saved for a batch the manifest refuses")
	}
	if _, ok := loadVPB(t).Outputs["docker"]; !ok {
		t.Error("the manifest should still declare the old name; Validate refused the whole batch")
	}
}

// A keyring delete that fails partway says exactly how far it got: dev's
// delete lands, staging's fails, prod's is never tried -- "1 of 3". The
// original's "trenvy ui" check was the old caller's redo sentence; its
// "press w" check still holds of commitStaged's own words.
func TestCommitPartialFailureReportsHowFarItGot(t *testing.T) {
	st := &failDelOnce{Mem: store.NewMem(), failOn: "trenvy/vpb/staging/ALPHA_TOKEN"}
	st.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "d")
	st.Seed("trenvy/vpb/staging/ALPHA_TOKEN", "s")
	st.Seed("trenvy/vpb/prod/ALPHA_TOKEN", "p")
	opts, app := newCommitAppFrom(t, st, portedFixtureThreeProfiles())

	_, err := commitStaged(opts, app, []ui.Change{{Kind: ui.ChangeDeleteVar, Name: "ALPHA_TOKEN"}})
	if err == nil {
		t.Fatal("commit succeeded despite a failing keyring delete")
	}
	if !strings.Contains(err.Error(), "deleted 1 of 3 keyring items") {
		t.Errorf("error does not say how far the deletes got: %v", err)
	}
	// No secret was written, so a "stored" clause would be "0 of 0" noise.
	if strings.Contains(err.Error(), "stored") {
		t.Errorf("error mentions a store count for a batch with no stores: %v", err)
	}
	if !strings.Contains(err.Error(), "manifest was not saved") {
		t.Errorf("error does not say the manifest is unsaved: %v", err)
	}
	if strings.Contains(err.Error(), "press w") {
		t.Errorf("error implies the ended session can be resumed with w: %v", err)
	}

	if _, ok := loadVPB(t).Vars["ALPHA_TOKEN"]; !ok {
		t.Error("the manifest was saved despite the keyring delete failing")
	}
	if ok, _ := st.Has("trenvy/vpb/dev/ALPHA_TOKEN"); ok {
		t.Error("dev's item should already be gone -- the run got at least that far")
	}
	if ok, _ := st.Has("trenvy/vpb/prod/ALPHA_TOKEN"); !ok {
		t.Error("prod's item must not have been touched once staging's delete failed")
	}
}

// One store that succeeds and one delete that fails read as two figures,
// "stored 1 of 1" and "deleted 0 of 1", never one blurred "1 of 2".
func TestCommitPartialFailureDistinguishesStoredFromDeleted(t *testing.T) {
	st := &failDelOnce{Mem: store.NewMem(), failOn: "trenvy/vpb/dev/ALPHA_TOKEN"}
	st.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "old")
	opts, app := newCommitAppFrom(t, st, twoSecrets)

	_, err := commitStaged(opts, app, storeOneDeleteOther)
	if err == nil {
		t.Fatal("commit succeeded despite a failing keyring delete")
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

	got, getErr := st.Get("trenvy/vpb/dev/SECOND_TOKEN")
	if getErr != nil {
		t.Fatalf("the successful store should still be there: %v", getErr)
	}
	if got != "new-value" {
		t.Errorf("stored secret = %q, want the value from the batch's store", got)
	}
	if ok, _ := st.Has("trenvy/vpb/dev/ALPHA_TOKEN"); !ok {
		t.Error("the failed delete should have left ALPHA_TOKEN in place")
	}
}

// An add-var that cannot validate (what a bare a leaves behind) is refused
// with nothing written, including the set-secret staged beside it.
func TestCommitAddVarFailingValidationWritesNothing(t *testing.T) {
	mem := store.NewMem()
	opts, app := newCommitAppFrom(t, mem, portedFixture)

	_, err := commitStaged(opts, app, []ui.Change{
		{Kind: ui.ChangeSetSecret, Name: "ALPHA_TOKEN", Profile: "dev", Value: "new-secret-value"},
		{Kind: ui.ChangeAddVar, Name: "EMPTYVAR"},
	})
	if err == nil {
		t.Fatal("commit succeeded with an add-var that cannot validate")
	}
	if !strings.Contains(err.Error(), "EMPTYVAR") {
		t.Errorf("error does not name the invalid variable: %v", err)
	}
	if _, ok := loadVPB(t).Vars["EMPTYVAR"]; ok {
		t.Error("the invalid manifest was saved anyway")
	}
	if ok, _ := mem.Has("trenvy/vpb/dev/ALPHA_TOKEN"); ok {
		t.Error("the keyring write landed for a batch the manifest refuses")
	}
	if !strings.Contains(err.Error(), "nothing was written") {
		t.Errorf("error does not say nothing was written: %v", err)
	}
}

// A keyring write that fails reports how far phase 2 got -- "stored 0 of
// 1", never a count that credits the failed write -- and leaves the
// manifest unsaved. The original,
// TestUiCommitErrorEndsWithTheRedoSentenceExactlyOnce, also counted the
// old caller's redo sentence, which goes with it; this carries over the
// part that is commitStaged's own.
func TestCommitKeyringWriteFailureReportsStoredNoneOfOne(t *testing.T) {
	st := &failOnce{Mem: store.NewMem(), failOn: "trenvy/vpb/dev/ALPHA_TOKEN"}
	opts, app := newCommitAppFrom(t, st, portedFixture)

	_, err := commitStaged(opts, app, []ui.Change{
		{Kind: ui.ChangeSetSecret, Name: "ALPHA_TOKEN", Profile: "dev", Value: "new-secret-value"},
	})
	if err == nil {
		t.Fatal("commit succeeded despite a failing keyring write")
	}
	if !strings.Contains(err.Error(), "stored 0 of 1 secrets") {
		t.Errorf("error lacks the progress line: %v", err)
	}
	if !strings.Contains(err.Error(), "manifest was not saved") {
		t.Errorf("error does not say the manifest is unsaved: %v", err)
	}
	if ok, _ := st.Has("trenvy/vpb/dev/ALPHA_TOKEN"); ok {
		t.Error("the failed keyring write left an item in the store")
	}
}
