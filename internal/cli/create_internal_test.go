package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/sample"
)

func TestResolveAppOutsideAProjectIsErrNotRegistered(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", t.TempDir())
	opts := &Options{Dir: t.TempDir()}
	_, err := opts.resolveApp()
	if !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("err = %v, want ErrNotRegistered", err)
	}
	if !strings.Contains(err.Error(), "register one with: trenvy project init") {
		t.Errorf("message changed: %v", err)
	}
}

func TestRegisterProjectWritesTheManifestThenTheRegistry(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	m, err := newProjectManifest("app")
	if err != nil {
		t.Fatal(err)
	}
	if err := registerProject("app", root, m); err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Load(config.ManifestPath("app")); err != nil {
		t.Errorf("manifest not written: %v", err)
	}
	reg, _ := config.LoadRegistry()
	if e, ok := reg.Lookup(root); !ok || e.ID != "app" {
		t.Errorf("registry lookup = %+v %v", e, ok)
	}
	// A taken id writes nothing more.
	other := t.TempDir()
	m2, _ := newProjectManifest("app")
	if err := registerProject("app", other, m2); err == nil {
		t.Error("a taken id was registered again")
	}
}

// Write order is the project rule: the manifest is saved before the
// registry, so a manifest that cannot be saved leaves no registry entry
// pointing at it.
func TestRegisterProjectSavesNoRegistryWhenTheManifestSaveFails(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("TRENVY_CONFIG_DIR", cfg)
	// A file where the projects directory belongs: the manifest save fails.
	if err := os.WriteFile(filepath.Join(cfg, "projects"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m, _ := newProjectManifest("app")
	if err := registerProject("app", t.TempDir(), m); err == nil {
		t.Fatal("the manifest save was blocked, yet registerProject succeeded")
	}
	if reg, _ := config.LoadRegistry(); len(reg.Projects) != 0 {
		t.Errorf("registry = %+v, want empty: it was saved before the manifest", reg.Projects)
	}
}

func TestRegisterProjectWritesNothingForAnInvalidManifest(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", t.TempDir())
	m, _ := newProjectManifest("app")
	m.Vars["EMPTY"] = manifest.Var{} // no value, not secret, not optional
	if err := registerProject("app", t.TempDir(), m); err == nil {
		t.Fatal("an invalid manifest was registered")
	}
	if _, err := os.Stat(config.ManifestPath("app")); !os.IsNotExist(err) {
		t.Errorf("a manifest was written: %v", err)
	}
	if reg, _ := config.LoadRegistry(); len(reg.Projects) != 0 {
		t.Errorf("registry = %+v, want empty", reg.Projects)
	}
}

func TestImportIntoAppliesEntriesAndAnOutput(t *testing.T) {
	// Resolved: outputFor resolves the sample's symlinks (macOS /var is
	// /private/var), and the worktree it falls back to must match, as
	// resolve.WorktreeRoot's own fallback already does in real use.
	wt, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	samplePath := filepath.Join(wt, ".env.example")
	m, _ := newProjectManifest("app")
	rep, err := importInto(m, wt, samplePath, []sample.Entry{
		{Name: "APP_MODE", Value: "development"},
		{Name: "API_TOKEN", Value: "changeme", GuessSecret: true},
	}, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Secrets != 1 || rep.Created == "" {
		t.Errorf("report = %+v", rep)
	}
	if v := m.Vars["API_TOKEN"]; !v.Secret || v.Value != nil {
		t.Errorf("API_TOKEN = %+v: a secret's sample value must not be kept", v)
	}
	if o := m.Outputs[rep.Created]; o.Path != ".env" || m.Project.Output != rep.Created || rep.Output != rep.Created {
		t.Errorf("output %q = %+v, default %q", rep.Created, o, m.Project.Output)
	}
}

func TestImportIntoRefusesAConflictWithoutForce(t *testing.T) {
	m, _ := newProjectManifest("app")
	old := "x"
	m.Vars["APP_MODE"] = manifest.Var{Value: &old}
	_, err := importInto(m, t.TempDir(), "s.env.example", []sample.Entry{{Name: "APP_MODE", Value: "y"}}, "", true, false)
	if err == nil || !strings.Contains(err.Error(), "re-run with --force") {
		t.Fatalf("err = %v", err)
	}
	if _, err := importInto(m, t.TempDir(), "s.env.example", []sample.Entry{{Name: "APP_MODE", Value: "y"}}, "", true, true); err != nil {
		t.Fatalf("with force: %v", err)
	}
}
