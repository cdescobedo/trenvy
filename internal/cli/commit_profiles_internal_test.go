package cli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/session"
	"github.com/cdescobedo/trenvy/internal/store"
)

func TestCommitAddsAProfileAndMakesItTheDefault(t *testing.T) {
	opts, app := newCommitApp(t, store.NewMem())
	if _, err := commitStaged(opts, app, []session.Change{
		{Kind: session.ChangeAddProfile, Name: "qa"},
		{Kind: session.ChangeDefaultProfile, Name: "qa"},
	}); err != nil {
		t.Fatal(err)
	}
	m := loadVPB(t)
	if !reflect.DeepEqual(m.Project.Profiles, []string{"dev", "prod", "qa"}) || m.Project.DefaultProfile != "qa" {
		t.Errorf("project = %+v", m.Project)
	}
}

func TestCommitRefusesAnInvalidProfileName(t *testing.T) {
	opts, app := newCommitApp(t, store.NewMem())
	_, err := commitStaged(opts, app, []session.Change{{Kind: session.ChangeAddProfile, Name: "a b"}})
	var ib *invalidBatchError
	if !errors.As(err, &ib) {
		t.Fatalf("err = %v, want an *invalidBatchError", err)
	}
}

// Review Focus 1: only items that exist are deleted and counted.
func TestCommitRemoveProfileDeletesOnlyItsStoredSecrets(t *testing.T) {
	mem := store.NewMem()
	for _, svc := range []string{"trenvy/vpb/dev/TOKEN", "trenvy/vpb/prod/TOKEN"} {
		if err := mem.Set(svc, "v"); err != nil {
			t.Fatal(err)
		}
	}
	opts, app := newCommitApp(t, mem)
	if _, err := commitStaged(opts, app, []session.Change{{Kind: session.ChangeRemoveProfile, Name: "prod"}}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := mem.Has("trenvy/vpb/prod/TOKEN"); ok {
		t.Error("prod's TOKEN is still in the keyring")
	}
	if ok, _ := mem.Has("trenvy/vpb/dev/TOKEN"); !ok {
		t.Error("dev's TOKEN was deleted too")
	}
	m := loadVPB(t)
	if !reflect.DeepEqual(m.Project.Profiles, []string{"dev"}) {
		t.Errorf("profiles = %v, want [dev]", m.Project.Profiles)
	}
	if _, ok := m.Vars["APP_MODE"].Profiles["prod"]; ok {
		t.Error("APP_MODE still overrides prod")
	}
}

// Review Focus 1, on the failure path: the delete total counts only the
// items Has finds, and an item it does not find is never deleted.
func TestCommitDeleteTotalCountsOnlyStoredItems(t *testing.T) {
	st := &failDelOnce{Mem: store.NewMem(), failOn: "trenvy/vpb/prod/TOKEN2"}
	st.Seed("trenvy/vpb/prod/TOKEN2", "v")
	opts, app := newCommitAppFrom(t, st, kindsManifest+"\n[vars.TOKEN2]\nsecret = true\n")
	_, err := commitStaged(opts, app, []session.Change{{Kind: session.ChangeRemoveProfile, Name: "prod"}})
	if err == nil {
		t.Fatal("commit succeeded; the store was set to fail")
	}
	if !strings.Contains(err.Error(), "deleted 0 of 1 keyring items") {
		t.Errorf("err = %v, want deleted 0 of 1", err)
	}
	for _, c := range st.Calls() {
		if c == "Delete trenvy/vpb/prod/TOKEN" {
			t.Errorf("an item Has did not find was deleted: %v", st.Calls())
		}
	}
}

// delete-var counts the same way: TOKEN is stored under prod only.
func TestCommitDeleteVarTotalCountsOnlyStoredItems(t *testing.T) {
	st := &failDelOnce{Mem: store.NewMem(), failOn: "trenvy/vpb/prod/TOKEN"}
	st.Seed("trenvy/vpb/prod/TOKEN", "v")
	opts, app := newCommitApp(t, st)
	_, err := commitStaged(opts, app, []session.Change{{Kind: session.ChangeDeleteVar, Name: "TOKEN"}})
	if err == nil || !strings.Contains(err.Error(), "deleted 0 of 1 keyring items") {
		t.Errorf("err = %v, want deleted 0 of 1", err)
	}
}

// hasFails answers every Has with an error.
type hasFails struct{ *store.Mem }

func (hasFails) Has(string) (bool, error) { return false, errors.New("keyring is locked") }

// A Has that errors cannot say the item is absent, so the delete is
// still attempted.
func TestCommitDeletesAnItemWhoseHasErrors(t *testing.T) {
	mem := store.NewMem()
	mem.Seed("trenvy/vpb/prod/TOKEN", "v")
	opts, app := newCommitApp(t, hasFails{mem})
	if _, err := commitStaged(opts, app, []session.Change{{Kind: session.ChangeRemoveProfile, Name: "prod"}}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := mem.Has("trenvy/vpb/prod/TOKEN"); ok {
		t.Error("prod's TOKEN survived; the delete was skipped when Has failed")
	}
}

func TestCommitRemovingTheDefaultIsRefusedUnlessTheDefaultMoves(t *testing.T) {
	opts, app := newCommitApp(t, store.NewMem())
	_, err := commitStaged(opts, app, []session.Change{{Kind: session.ChangeRemoveProfile, Name: "dev"}})
	var ib *invalidBatchError
	if !errors.As(err, &ib) {
		t.Fatalf("removing the default: err = %v, want an *invalidBatchError", err)
	}
	fresh, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commitStaged(opts, fresh, []session.Change{
		{Kind: session.ChangeRemoveProfile, Name: "dev"},
		{Kind: session.ChangeDefaultProfile, Name: "prod"},
	}); err != nil {
		t.Fatalf("moving the default and removing dev: %v", err)
	}
	if m := loadVPB(t); m.Project.DefaultProfile != "prod" || len(m.Project.Profiles) != 1 {
		t.Errorf("project = %+v", m.Project)
	}
}

func TestCommitUseProfileSetsActiveAndRefusesAnUnsavedOne(t *testing.T) {
	opts, app := newCommitApp(t, store.NewMem())
	if _, err := commitStaged(opts, app, []session.Change{{Kind: session.ChangeUseProfile, Name: "prod"}}); err != nil {
		t.Fatal(err)
	}
	st, err := config.LoadState("vpb")
	if err != nil {
		t.Fatal(err)
	}
	if st.Active != "prod" {
		t.Errorf("Active = %q, want prod", st.Active)
	}
	fresh, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	_, err = commitStaged(opts, fresh, []session.Change{
		{Kind: session.ChangeAddProfile, Name: "qa"},
		{Kind: session.ChangeUseProfile, Name: "qa"},
	})
	var ib *invalidBatchError
	if !errors.As(err, &ib) || !strings.Contains(err.Error(), "not in the manifest yet") {
		t.Fatalf("use on an unsaved profile: err = %v", err)
	}
	if st, _ := config.LoadState("vpb"); st.Active != "prod" {
		t.Errorf("Active = %q after a refused batch, want prod", st.Active)
	}
}

func TestCommitRemovingTheActiveProfileClearsTheSelection(t *testing.T) {
	opts, app := newCommitApp(t, store.NewMem())
	app.State.Active = "prod"
	if err := app.State.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := commitStaged(opts, app, []session.Change{{Kind: session.ChangeRemoveProfile, Name: "prod"}}); err != nil {
		t.Fatal(err)
	}
	if st, _ := config.LoadState("vpb"); st.Active != "" {
		t.Errorf("Active = %q, want cleared", st.Active)
	}
}

func TestCommitSkipsASecretWriteOnARemovedProfile(t *testing.T) {
	mem := store.NewMem()
	opts, app := newCommitApp(t, mem)
	if _, err := commitStaged(opts, app, []session.Change{
		{Kind: session.ChangeSetSecret, Name: "TOKEN", Profile: "prod", Value: "s"},
		{Kind: session.ChangeRemoveProfile, Name: "prod"},
	}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := mem.Has("trenvy/vpb/prod/TOKEN"); ok {
		t.Error("a secret was stored under a profile the same batch removes")
	}
}

// Spec §3: a failed phase 3 delete, then a retry, ends where one clean
// commit would.
func TestCommitRetryAfterAKeyringDeleteFailureOnARemoveProfile(t *testing.T) {
	st := &failDelOnce{Mem: store.NewMem(), failOn: "trenvy/vpb/prod/TOKEN"}
	if err := st.Mem.Set("trenvy/vpb/prod/TOKEN", "v"); err != nil {
		t.Fatal(err)
	}
	opts, app := newCommitApp(t, st)
	batch := []session.Change{{Kind: session.ChangeRemoveProfile, Name: "prod"}}
	if _, err := commitStaged(opts, app, batch); err == nil {
		t.Fatal("first commit succeeded; the store was set to fail")
	}
	if m := loadVPB(t); len(m.Project.Profiles) != 2 {
		t.Fatal("manifest changed on a failed commit")
	}
	fresh, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commitStaged(opts, fresh, batch); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if ok, _ := st.Has("trenvy/vpb/prod/TOKEN"); ok {
		t.Error("prod's TOKEN survived the retry")
	}
	if m := loadVPB(t); !reflect.DeepEqual(m.Project.Profiles, []string{"dev"}) {
		t.Errorf("profiles = %v after the retry", m.Project.Profiles)
	}
}

// Review Focus 4: the state save (clearing Active) lands, the manifest save
// fails. The profile is still declared, the selection is already clear,
// and the retry ends clean.
func TestCommitRetryAfterAManifestSaveFailureOnARemoveProfile(t *testing.T) {
	opts, app := newCommitApp(t, store.NewMem())
	app.State.Active = "prod"
	if err := app.State.Save(); err != nil {
		t.Fatal(err)
	}
	batch := []session.Change{
		{Kind: session.ChangeRemoveProfile, Name: "prod"},
		{Kind: session.ChangeUseProfile, Name: "dev"},
	}
	restore := lockDir(t, filepath.Dir(config.ManifestPath("vpb")))
	if _, err := commitStaged(opts, app, batch); err == nil {
		t.Fatal("first commit succeeded; the manifest directory is read-only")
	}
	restore()
	if m := loadVPB(t); len(m.Project.Profiles) != 2 {
		t.Fatal("manifest changed on a failed commit")
	}
	fresh, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatalf("the project no longer loads after the failed commit: %v", err)
	}
	if _, err := commitStaged(opts, fresh, batch); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if st, _ := config.LoadState("vpb"); st.Active != "dev" {
		t.Errorf("Active = %q, want dev", st.Active)
	}
	if m := loadVPB(t); !reflect.DeepEqual(m.Project.Profiles, []string{"dev"}) {
		t.Errorf("profiles = %v", m.Project.Profiles)
	}
}

// A state save that fails before the manifest: nothing in the manifest
// moves, and the retry ends clean.
func TestCommitRetryAfterAStateSaveFailureOnUseProfile(t *testing.T) {
	opts, app := newCommitApp(t, store.NewMem())
	batch := []session.Change{
		{Kind: session.ChangeUseProfile, Name: "prod"},
		{Kind: session.ChangeDefaultProfile, Name: "prod"},
	}
	if err := os.MkdirAll(filepath.Dir(config.StatePath("vpb")), 0o700); err != nil {
		t.Fatal(err)
	}
	restore := lockDir(t, filepath.Dir(config.StatePath("vpb")))
	if _, err := commitStaged(opts, app, batch); err == nil {
		t.Fatal("first commit succeeded; the state directory is read-only")
	}
	restore()
	if m := loadVPB(t); m.Project.DefaultProfile != "dev" {
		t.Fatal("manifest changed on a failed commit")
	}
	fresh, err := opts.resolveAppByID("vpb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commitStaged(opts, fresh, batch); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if st, _ := config.LoadState("vpb"); st.Active != "prod" {
		t.Errorf("Active = %q, want prod", st.Active)
	}
	if m := loadVPB(t); m.Project.DefaultProfile != "prod" {
		t.Errorf("default = %q, want prod", m.Project.DefaultProfile)
	}
}

func TestSnapshotCarriesDefaultAndActiveProfile(t *testing.T) {
	opts, app := newCommitApp(t, store.NewMem())
	app.State.Active = "prod"
	snap, err := buildSnapshot(opts, app, nil)
	if err != nil {
		t.Fatal(err)
	}
	if snap.DefaultProfile != "dev" || snap.Active != "prod" {
		t.Errorf("DefaultProfile = %q, Active = %q; want dev, prod", snap.DefaultProfile, snap.Active)
	}
}
