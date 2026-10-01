package cli

import (
	"errors"
	"io"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/store"
	"github.com/cdescobedo/trenvy/internal/ui"
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
	t.Setenv("TRENVY_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	m, err := manifest.Parse([]byte(kindsManifest))
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
	res, err := commitStaged(opts, app, []ui.Change{
		{Kind: ui.ChangeSetShared, Name: "APP_MODE", Value: "dev-mode"},
		{Kind: ui.ChangeUnsetValue, Name: "APP_MODE", Profile: "prod"},
		{Kind: ui.ChangeUnsetValue, Name: "LOG_LEVEL"},
		{Kind: ui.ChangeSetOptional, Name: "LOG_LEVEL", Value: "true"},
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
	_, err := commitStaged(opts, app, []ui.Change{
		{Kind: ui.ChangeSetSecret, Name: "TOKEN", Profile: "dev", Value: "s3cret"},
		{Kind: ui.ChangeAddVar, Name: "EMPTY"}, // no value, not secret, not optional: invalid
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
	batch := []ui.Change{
		{Kind: ui.ChangeSetSecret, Name: "TOKEN", Profile: "dev", Value: "d"},
		{Kind: ui.ChangeSetSecret, Name: "TOKEN", Profile: "prod", Value: "p"},
		{Kind: ui.ChangeSetShared, Name: "APP_MODE", Value: "x"},
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
