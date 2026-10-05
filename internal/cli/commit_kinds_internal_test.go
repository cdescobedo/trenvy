package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
	"github.com/cdescobedo/trenvy/internal/store"
)

const kindsManifest = `
[project]
id = "vpb"
profiles = ["dev", "prod"]
default_profile = "dev"
salt = "abc123"

[vars.APP_MODE]
value = "development"
profiles = { prod = "production" }

[vars.LOG_LEVEL]
value = "info"

[vars.TOKEN]
secret = true
`

// newCommitApp registers kindsManifest in a temp config dir and resolves an
// appContext over st, the way commit_phase_internal_test.go's tests do.
func newCommitApp(t *testing.T, st store.Store) (*Options, *appContext) {
	t.Helper()
	return newCommitAppFrom(t, st, kindsManifest)
}

// kindsManifestWithOutput adds one output, for the phases that touch the
// state file.
const kindsManifestWithOutput = kindsManifest + `
[outputs.local]
format = "dotenv"
path = ".env"
`

func newCommitAppFrom(t *testing.T, st store.Store, src string) (*Options, *appContext) {
	t.Helper()
	t.Setenv("TRENVY_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	m, err := manifest.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := config.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(config.Entry{ID: "vpb", Root: root}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(config.ManifestPath("vpb"), m); err != nil {
		t.Fatal(err)
	}
	opts := &Options{Dir: root, Stdout: io.Discard, Stderr: io.Discard,
		NewStore: func(*config.Config, string) (store.Store, error) { return st, nil }}
	app, err := opts.resolveApp()
	if err != nil {
		t.Fatal(err)
	}
	return opts, app
}

func loadVPB(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCommitAppliesSharedUnsetAndOptional(t *testing.T) {
	opts, app := newCommitApp(t, store.NewMem())
	res, err := commitStaged(opts, app, []session.Change{
		{Kind: session.ChangeSetShared, Name: "APP_MODE", Value: "dev-mode"},
		{Kind: session.ChangeUnsetValue, Name: "APP_MODE", Profile: "prod"},
		{Kind: session.ChangeUnsetValue, Name: "LOG_LEVEL"},
		{Kind: session.ChangeSetOptional, Name: "LOG_LEVEL", Value: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.saved != 4 {
		t.Errorf("saved = %d, want 4", res.saved)
	}
	m := loadVPB(t)
	if v := m.Vars["APP_MODE"]; v.Value == nil || *v.Value != "dev-mode" || len(v.Profiles) != 0 {
		t.Errorf("APP_MODE = %+v, want shared dev-mode and no overrides", v)
	}
	if v := m.Vars["LOG_LEVEL"]; v.Value != nil || !v.Optional {
		t.Errorf("LOG_LEVEL = %+v, want no value and optional", v)
	}
}

// Spec §3: a batch the manifest refuses must write nothing, including the
// keyring -- today Validate runs after the keyring phases, so the secret
// below lands before the refusal.
func TestCommitRefusesAnInvalidBatchBeforeTheKeyring(t *testing.T) {
	mem := store.NewMem()
	opts, app := newCommitApp(t, mem)
	_, err := commitStaged(opts, app, []session.Change{
		{Kind: session.ChangeSetSecret, Name: "TOKEN", Profile: "dev", Value: "s3cret"},
		{Kind: session.ChangeAddVar, Name: "EMPTY"}, // no value, not secret, not optional: invalid
	})
	var ib *invalidBatchError
	if !errors.As(err, &ib) {
		t.Fatalf("err = %v, want an *invalidBatchError", err)
	}
	if ok, _ := mem.Has("trenvy/vpb/dev/TOKEN"); ok {
		t.Error("the keyring write landed for a batch the manifest refuses")
	}
}

// failOnce fails the first Set to failOn, then behaves.
type failOnce struct {
	*store.Mem
	failOn string
	failed bool
}

func (f *failOnce) Set(service, value string) error {
	if service == f.failOn && !f.failed {
		f.failed = true
		return errors.New("keyring is locked")
	}
	return f.Mem.Set(service, value)
}

// Spec §3: after a partial failure the whole batch stays staged, and
// re-running it must end where one clean commit would.
func TestCommitRetryAfterAPartialFailureMatchesOneCleanCommit(t *testing.T) {
	st := &failOnce{Mem: store.NewMem(), failOn: "trenvy/vpb/prod/TOKEN"}
	opts, app := newCommitApp(t, st)
	batch := []session.Change{
		{Kind: session.ChangeSetSecret, Name: "TOKEN", Profile: "dev", Value: "d"},
		{Kind: session.ChangeSetSecret, Name: "TOKEN", Profile: "prod", Value: "p"},
		{Kind: session.ChangeSetShared, Name: "APP_MODE", Value: "x"},
	}
	if _, err := commitStaged(opts, app, batch); err == nil {
		t.Fatal("first commit succeeded; the store was set to fail")
	}
	if v := loadVPB(t).Vars["APP_MODE"]; *v.Value != "development" {
		t.Fatalf("manifest changed on a failed commit: %+v", v)
	}

	fresh, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commitStaged(opts, fresh, batch); err != nil {
		t.Fatalf("retry: %v", err)
	}
	for svc, want := range map[string]string{"trenvy/vpb/dev/TOKEN": "d", "trenvy/vpb/prod/TOKEN": "p"} {
		if got, err := st.Get(svc); err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", svc, got, err, want)
		}
	}
	if v := loadVPB(t).Vars["APP_MODE"]; *v.Value != "x" {
		t.Errorf("APP_MODE = %+v, want x", v)
	}
}

// failDelOnce fails the first Delete of failOn, then behaves.
type failDelOnce struct {
	*store.Mem
	failOn string
	failed bool
}

func (f *failDelOnce) Delete(service string) error {
	if service == f.failOn && !f.failed {
		f.failed = true
		return errors.New("keyring is locked")
	}
	return f.Mem.Delete(service)
}

// lockDir makes dir unwritable until the returned restore runs (also on
// cleanup), the way the existing read-only-directory tests do.
func lockDir(t *testing.T, dir string) (restore func()) {
	t.Helper()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	restore = func() { _ = os.Chmod(dir, 0o700) }
	t.Cleanup(restore)
	return restore
}

// recordLocalRender records a render of "local" in the app's worktree and
// returns a freshly resolved app that sees it.
func recordLocalRender(t *testing.T, opts *Options, app *appContext) *appContext {
	t.Helper()
	app.State.Record(config.RenderEntry{Worktree: app.Entry.Root, Output: "local", Path: ".env"})
	if err := app.State.Save(); err != nil {
		t.Fatal(err)
	}
	fresh, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	return fresh
}

// Spec §3, phase 3: a keyring delete that fails partway leaves the manifest
// alone, and re-running the batch finishes the job.
func TestCommitRetryAfterAKeyringDeleteFailureMatchesOneCleanCommit(t *testing.T) {
	st := &failDelOnce{Mem: store.NewMem(), failOn: "trenvy/vpb/prod/TOKEN"}
	for _, svc := range []string{"trenvy/vpb/dev/TOKEN", "trenvy/vpb/prod/TOKEN"} {
		if err := st.Mem.Set(svc, "v"); err != nil {
			t.Fatal(err)
		}
	}
	opts, app := newCommitApp(t, st)
	batch := []session.Change{
		{Kind: session.ChangeDeleteVar, Name: "TOKEN"},
		{Kind: session.ChangeSetShared, Name: "APP_MODE", Value: "x"},
	}
	if _, err := commitStaged(opts, app, batch); err == nil {
		t.Fatal("first commit succeeded; the store was set to fail")
	}
	if _, ok := loadVPB(t).Vars["TOKEN"]; !ok {
		t.Fatal("manifest changed on a failed commit: TOKEN is gone")
	}

	fresh, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commitStaged(opts, fresh, batch); err != nil {
		t.Fatalf("retry: %v", err)
	}
	for _, svc := range []string{"trenvy/vpb/dev/TOKEN", "trenvy/vpb/prod/TOKEN"} {
		if ok, _ := st.Has(svc); ok {
			t.Errorf("%s still in the keyring after the retry", svc)
		}
	}
	m := loadVPB(t)
	if _, ok := m.Vars["TOKEN"]; ok {
		t.Error("TOKEN still in the manifest after the retry")
	}
	if v := m.Vars["APP_MODE"]; *v.Value != "x" {
		t.Errorf("APP_MODE = %+v, want x", v)
	}
}

// Spec §3, phase 4: a state-file save that fails leaves the old name in the
// state and the manifest; the retry repoints the record and renames.
func TestCommitRetryAfterAStateSaveFailureMatchesOneCleanCommit(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), kindsManifestWithOutput)
	app = recordLocalRender(t, opts, app)
	batch := []session.Change{
		{Kind: session.ChangeRenameOutput, Name: "local", Value: "main"},
		{Kind: session.ChangeSetShared, Name: "APP_MODE", Value: "x"},
	}

	restore := lockDir(t, filepath.Dir(config.StatePath("vpb")))
	if _, err := commitStaged(opts, app, batch); err == nil {
		t.Fatal("first commit succeeded; the state directory is read-only")
	}
	restore()
	if _, ok := loadVPB(t).Outputs["local"]; !ok {
		t.Fatal("manifest changed on a failed commit: local is gone")
	}

	fresh, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commitStaged(opts, fresh, batch); err != nil {
		t.Fatalf("retry: %v", err)
	}
	m := loadVPB(t)
	if _, ok := m.Outputs["main"]; !ok {
		t.Error("output main missing after the retry")
	}
	if _, ok := m.Outputs["local"]; ok {
		t.Error("output local still present after the retry")
	}
	after, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.State.Find(app.Entry.Root, "main"); !ok {
		t.Error("render record not repointed to main")
	}
	if _, ok := after.State.Find(app.Entry.Root, "local"); ok {
		t.Error("render record still under local")
	}
}

// Spec §3, phase 6: a manifest save that fails after the state save landed.
// The error must name the file left on disk (phase 4's invariant), and the
// retry must end where one clean commit would.
func TestCommitRetryAfterAManifestSaveFailureMatchesOneCleanCommit(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), kindsManifestWithOutput)
	app = recordLocalRender(t, opts, app)
	batch := []session.Change{
		{Kind: session.ChangeRemoveOutput, Name: "local"},
		{Kind: session.ChangeSetShared, Name: "APP_MODE", Value: "x"},
	}

	restore := lockDir(t, filepath.Dir(config.ManifestPath("vpb")))
	_, err := commitStaged(opts, app, batch)
	if err == nil {
		t.Fatal("first commit succeeded; the manifest directory is read-only")
	}
	want := "left in place: " + filepath.Join(app.Entry.Root, ".env")
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error does not name the file left on disk (%q): %v", want, err)
	}
	restore()
	if _, ok := loadVPB(t).Outputs["local"]; !ok {
		t.Fatal("manifest changed on a failed commit: local is gone")
	}

	fresh, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commitStaged(opts, fresh, batch); err != nil {
		t.Fatalf("retry: %v", err)
	}
	m := loadVPB(t)
	if _, ok := m.Outputs["local"]; ok {
		t.Error("output local still present after the retry")
	}
	if v := m.Vars["APP_MODE"]; *v.Value != "x" {
		t.Errorf("APP_MODE = %+v, want x", v)
	}
	after, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.State.Find(app.Entry.Root, "local"); ok {
		t.Error("render record for local survived the removal")
	}
}

// setRecorder records every Set it passes to Mem.
type setRecorder struct {
	*store.Mem
	sets []string
}

func (r *setRecorder) Set(service, value string) error {
	r.sets = append(r.sets, service)
	return r.Mem.Set(service, value)
}

// A hand-built batch that deletes a variable it also promotes must not store
// the promotion's keyring items: the delete leaves no manifest entry naming
// them, so they would be orphaned. session.Staged never builds this batch;
// commitStaged must not trust that every caller used it.
func TestCommitSkipsSecretWritesForADeletedVariable(t *testing.T) {
	st := &setRecorder{Mem: store.NewMem()}
	opts, app := newCommitApp(t, st)
	_, err := commitStaged(opts, app, []session.Change{
		{Kind: session.ChangeSetSecret, Name: "APP_MODE", Profile: "dev", Value: "development"},
		{Kind: session.ChangeSetSecret, Name: "APP_MODE", Profile: "prod", Value: "production"},
		{Kind: session.ChangePromoteSecret, Name: "APP_MODE"},
		{Kind: session.ChangeDeleteVar, Name: "APP_MODE"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.sets) != 0 {
		t.Errorf("stored %v for a variable the same batch deletes", st.sets)
	}
	if _, ok := loadVPB(t).Vars["APP_MODE"]; ok {
		t.Error("APP_MODE survived its delete")
	}
}

// Spec §3, phases 4 and 6 together: the rename reaches the state file, then
// the manifest save fails. The retry starts from records already under the
// new name, so it only ends where one clean commit would if repointing
// nothing is not a failure ("State.RenameOutput on records already moved
// moves none"). The state-save test above cannot see this: its failed save
// writes nothing, so its retry is a first run.
func TestCommitRetryAfterARenameReachedTheStateFileMatchesOneCleanCommit(t *testing.T) {
	opts, app := newCommitAppFrom(t, store.NewMem(), kindsManifestWithOutput)
	app = recordLocalRender(t, opts, app)
	batch := []session.Change{
		{Kind: session.ChangeRenameOutput, Name: "local", Value: "main"},
		{Kind: session.ChangeSetShared, Name: "APP_MODE", Value: "x"},
	}

	restore := lockDir(t, filepath.Dir(config.ManifestPath("vpb")))
	if _, err := commitStaged(opts, app, batch); err == nil {
		t.Fatal("first commit succeeded; the manifest directory is read-only")
	}
	restore()
	moved, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := moved.State.Find(app.Entry.Root, "main"); !ok {
		t.Fatal("setup: the state save did not land before the manifest failure")
	}
	if _, ok := moved.Manifest.Outputs["local"]; !ok {
		t.Fatal("manifest changed on a failed commit: local is gone")
	}

	if _, err := commitStaged(opts, moved, batch); err != nil {
		t.Fatalf("retry: %v", err)
	}
	m := loadVPB(t)
	if _, ok := m.Outputs["main"]; !ok {
		t.Error("output main missing after the retry")
	}
	if _, ok := m.Outputs["local"]; ok {
		t.Error("output local still present after the retry")
	}
	if v := m.Vars["APP_MODE"]; *v.Value != "x" {
		t.Errorf("APP_MODE = %+v, want x", v)
	}
	after, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.State.Find(app.Entry.Root, "main"); !ok {
		t.Error("render record not under main after the retry")
	}
}

// The new interface keeps a refused batch staged, and retrying it unchanged
// cannot succeed, so its commit error must say nothing was written in a way
// the review can test for -- and leave the words to the review.
func TestManageCommitMarksARefusedBatchAsNothingWritten(t *testing.T) {
	opts, app := newCommitApp(t, store.NewMem())
	_, err := manageCommit(opts, app, []session.Change{{Kind: session.ChangeAddVar, Name: "EMPTY"}})
	if !errors.Is(err, manage.ErrNothingWritten) {
		t.Errorf("err = %v, want it to match manage.ErrNothingWritten", err)
	}
	if err != nil && (!strings.Contains(err.Error(), "needs value") || strings.Contains(err.Error(), "nothing was written")) {
		t.Errorf("err text = %q, want the manifest's reason and not the review's words", err)
	}

	// A keyring write that fails partway is not a refusal: a retry can land.
	st := &failOnce{Mem: store.NewMem(), failOn: "trenvy/vpb/dev/TOKEN"}
	opts, app = newCommitApp(t, st)
	_, err = manageCommit(opts, app, []session.Change{{Kind: session.ChangeSetSecret, Name: "TOKEN", Profile: "dev", Value: "v"}})
	if err == nil || errors.Is(err, manage.ErrNothingWritten) {
		t.Errorf("err = %v, want a failure that is not ErrNothingWritten", err)
	}
}

const outputsManifest = `
[project]
id = "vpb"
profiles = ["dev"]
default_profile = "dev"
output = "local"
salt = "abc123"

[outputs.local]
format = "dotenv"
path = ".env"

[outputs.web]
format = "json"
path = "web.json"

[vars.APP_URL]
value = "x"
`

func newOutputsApp(t *testing.T) (*Options, *appContext) {
	t.Helper()
	t.Setenv("TRENVY_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	m, err := manifest.Parse([]byte(outputsManifest))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := config.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(config.Entry{ID: "vpb", Root: root}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(config.ManifestPath("vpb"), m); err != nil {
		t.Fatal(err)
	}
	opts := &Options{Dir: root, Stdout: io.Discard, Stderr: io.Discard,
		NewStore: func(*config.Config, string) (store.Store, error) { return store.NewMem(), nil }}
	app, err := opts.resolveApp()
	if err != nil {
		t.Fatal(err)
	}
	return opts, app
}

func TestCommitAppliesOutputFormatAndPath(t *testing.T) {
	opts, app := newOutputsApp(t)
	_, err := commitStaged(opts, app, []session.Change{
		{Kind: session.ChangeSetOutputFormat, Name: "local", Value: "json"},
		{Kind: session.ChangeSetOutputPath, Name: "local", Value: "config/env.json"},
		{Kind: session.ChangeRenameOutput, Name: "local", Value: "root"},
	})
	if err != nil {
		t.Fatal(err)
	}
	o, ok := loadVPB(t).Outputs["root"]
	if !ok || o.Format != "json" || o.Path != "config/env.json" {
		t.Errorf("outputs = %+v, want root json config/env.json (edits applied under the identity, then renamed)", loadVPB(t).Outputs)
	}
}

// Review Focus 2: a rename onto an existing output, or two onto one name,
// is refused before anything is written -- phase 5b would otherwise
// overwrite the other output silently.
func TestCommitRefusesARenameOntoAnExistingOutput(t *testing.T) {
	for name, batch := range map[string][]session.Change{
		"onto existing": {{Kind: session.ChangeRenameOutput, Name: "local", Value: "web"}},
		"two onto one": {
			{Kind: session.ChangeRenameOutput, Name: "local", Value: "shared"},
			{Kind: session.ChangeRenameOutput, Name: "web", Value: "shared"},
		},
		"onto an added": {
			{Kind: session.ChangeAddOutput, Name: "extra", Format: "dotenv", Path: "x.env"},
			{Kind: session.ChangeRenameOutput, Name: "local", Value: "extra"},
		},
	} {
		opts, app := newOutputsApp(t)
		_, err := commitStaged(opts, app, batch)
		var ib *invalidBatchError
		if !errors.As(err, &ib) {
			t.Errorf("%s: err = %v, want an *invalidBatchError", name, err)
			continue
		}
		if len(loadVPB(t).Outputs) != 2 {
			t.Errorf("%s: the manifest changed", name)
		}
	}
	// A swap is refused too: phase 5b renames one row at a time, so
	// local->web would overwrite web before web->local ran, and one output
	// would be lost (traced 2026-10-05). Rename through a third name.
	opts, app := newOutputsApp(t)
	_, err := commitStaged(opts, app, []session.Change{
		{Kind: session.ChangeRenameOutput, Name: "local", Value: "web"},
		{Kind: session.ChangeRenameOutput, Name: "web", Value: "local"},
	})
	var ib *invalidBatchError
	if !errors.As(err, &ib) {
		t.Errorf("swap: err = %v, want an *invalidBatchError", err)
	}
}

// Phase 1 ledger (Task 4, PHASE 2 MUST ADDRESS): a failure after the state
// save forgets render records; the retry finds none to forget. The failed
// attempt must hand back the left-in-place paths with its error, so the
// caller can carry them to the retry's result.
func TestCommitReturnsLeftInPlaceWithALateError(t *testing.T) {
	opts, app := newOutputsApp(t)
	app.State.Record(config.RenderEntry{Worktree: app.Worktree, Output: "web", Path: "web.json"})
	if err := app.State.Save(); err != nil {
		t.Fatal(err)
	}
	restore := lockDir(t, filepath.Dir(config.ManifestPath("vpb")))
	defer restore()
	res, err := commitStaged(opts, app, []session.Change{{Kind: session.ChangeRemoveOutput, Name: "web"}})
	if err == nil {
		t.Fatal("commit succeeded with the manifest directory locked")
	}
	if want := app.Worktree + "/web.json"; len(res.leftInPlace) != 1 || res.leftInPlace[0] != want {
		t.Errorf("leftInPlace = %v, want [%s]", res.leftInPlace, want)
	}
}

func TestLeftInPlaceSurvivesARetry(t *testing.T) {
	opts, app := newOutputsApp(t)
	app.State.Record(config.RenderEntry{Worktree: app.Worktree, Output: "web", Path: "web.json"})
	if err := app.State.Save(); err != nil {
		t.Fatal(err)
	}
	batch := []session.Change{{Kind: session.ChangeRemoveOutput, Name: "web"}}
	var carry leftCarry
	restore := lockDir(t, filepath.Dir(config.ManifestPath("vpb")))
	if _, err := carry.commit(opts, app, batch); err == nil {
		t.Fatal("first attempt succeeded with the manifest directory locked")
	}
	restore()
	fresh, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	res, err := carry.commit(opts, fresh, batch)
	if err != nil {
		t.Fatal(err)
	}
	if want := app.Worktree + "/web.json"; len(res.LeftInPlace) != 1 || res.LeftInPlace[0] != want {
		t.Errorf("retry LeftInPlace = %v, want [%s]", res.LeftInPlace, want)
	}
}

// The old interface stages a rename back to the row's own name; phase 5b
// treats it as a no-op, so the collision check must let it through.
func TestCommitAllowsASelfRenameAndRenamingAnAddedOutput(t *testing.T) {
	opts, app := newOutputsApp(t)
	if _, err := commitStaged(opts, app, []session.Change{
		{Kind: session.ChangeRenameOutput, Name: "local", Value: "local"},
	}); err != nil {
		t.Fatalf("self-rename: %v", err)
	}
	if o, ok := loadVPB(t).Outputs["local"]; !ok || o.Format != "dotenv" || o.Path != ".env" {
		t.Errorf("outputs = %+v, want local dotenv .env intact", loadVPB(t).Outputs)
	}

	opts, app = newOutputsApp(t)
	if _, err := commitStaged(opts, app, []session.Change{
		{Kind: session.ChangeAddOutput, Name: "extra", Format: "dotenv", Path: "x.env"},
		{Kind: session.ChangeRenameOutput, Name: "extra", Value: "free"},
	}); err != nil {
		t.Fatalf("add then rename: %v", err)
	}
	outs := loadVPB(t).Outputs
	if _, ok := outs["free"]; !ok {
		t.Errorf("outputs = %+v, want free", outs)
	}
	if _, ok := outs["extra"]; ok {
		t.Errorf("outputs = %+v, extra should be gone", outs)
	}
}
