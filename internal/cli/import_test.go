package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
)

func TestImportSeedsTheManifestFromADotenvSample(t *testing.T) {
	h := newHarness(t)
	if err := h.run("project", "init", "--id", "vpb"); err != nil {
		t.Fatal(err)
	}

	samplePath := filepath.Join(h.root, ".env_example")
	body := "APP_URL=http://+:8080\nALPHA_TOKEN=your-secret-key-here-replace-with-actual-secret\n"
	if err := os.WriteFile(samplePath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := h.run("import", samplePath, "--no-tui"); err != nil {
		t.Fatalf("import: %v", err)
	}

	m, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	urls := m.Vars["APP_URL"]
	if urls.Value == nil || *urls.Value != "http://+:8080" {
		t.Errorf("APP_URL = %v, want the sample's value", urls.Value)
	}
	if urls.Secret {
		t.Error("a real-looking value was flagged secret")
	}

	key := m.Vars["ALPHA_TOKEN"]
	if !key.Secret {
		t.Error("a placeholder value was not flagged secret")
	}
	// A placeholder must never be stored as the value.
	if key.Value != nil {
		t.Errorf("ALPHA_TOKEN.Value = %v, want nil", key.Value)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("imported manifest does not validate: %v", err)
	}
}

// The post-import hint suggests a command to run next. Under -p prod, a bare
// `trenvy secret set <NAME>` would store the value under whichever profile
// happens to be active in the shell that copy-pastes it, not the profile
// this import actually ran under.
func TestImportSecretHintNamesTheActiveProfile(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	sample := filepath.Join(h.root, "sample.env")
	body := "NEW_SECRET=your-secret-key-here-replace-with-actual-secret\n"
	if err := os.WriteFile(sample, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := h.run("-p", "prod", "import", sample, "--no-tui"); err != nil {
		t.Fatalf("import: %v", err)
	}

	out := h.out.String()
	if !strings.Contains(out, "trenvy -p prod secret set <NAME>") {
		t.Errorf("import hint does not name the active profile:\n%s", out)
	}
}

func TestImportCreatesAnOutputForTheSamplesRealPath(t *testing.T) {
	h := newHarness(t)
	if err := h.run("project", "init", "--id", "vpb"); err != nil {
		t.Fatal(err)
	}
	// outputFor resolves the sample's path against the git worktree it is
	// actually inside; real samples always live inside one.
	h.gitInit("")
	dir := filepath.Join(h.root, "src", "DemoApp.Api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sample := filepath.Join(dir, ".env_example")
	if err := os.WriteFile(sample, []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := h.run("import", sample, "--no-tui", "--output", "docker"); err != nil {
		t.Fatalf("import: %v", err)
	}

	m, _ := manifest.Load(config.ManifestPath("vpb"))
	out, ok := m.Outputs["docker"]
	if !ok {
		t.Fatal("import did not create the output")
	}
	// The sample's own path, minus the _example suffix, is the obvious guess.
	if out.Path != filepath.Join("src", "DemoApp.Api", ".env") {
		t.Errorf("output path = %q", out.Path)
	}
	if out.Format != manifest.FormatDotenv {
		t.Errorf("output format = %q", out.Format)
	}
}

// A project need not be version controlled: resolve.WorktreeRoot says so
// explicitly. When no repository contains the sample, the output path must
// still be relative to the project root, not collapse to the sample's own
// basename -- which is what happens if the sample's own directory is mistaken
// for a worktree root.
func TestImportOutputPathInANonGitProject(t *testing.T) {
	h := newHarness(t)
	if err := h.run("project", "init", "--id", "vpb"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(h.root, "src", "DemoApp.Api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sample := filepath.Join(dir, ".env_example")
	if err := os.WriteFile(sample, []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := h.run("import", sample, "--no-tui", "--output", "docker"); err != nil {
		t.Fatalf("import: %v", err)
	}

	m, _ := manifest.Load(config.ManifestPath("vpb"))
	out, ok := m.Outputs["docker"]
	if !ok {
		t.Fatal("import did not create the output")
	}
	want := filepath.Join("src", "DemoApp.Api", ".env")
	if out.Path != want {
		t.Errorf("output path = %q, want %q", out.Path, want)
	}
}

// A directory whose name happens to contain one of the example suffixes must
// survive intact: only the sample's own filename is stripped.
func TestImportOutputPathLeavesDirectoryNamesIntact(t *testing.T) {
	h := newHarness(t)
	if err := h.run("project", "init", "--id", "vpb"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(h.root, "test_example_project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sample := filepath.Join(dir, ".env_example")
	if err := os.WriteFile(sample, []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := h.run("import", sample, "--no-tui", "--output", "docker"); err != nil {
		t.Fatalf("import: %v", err)
	}

	m, _ := manifest.Load(config.ManifestPath("vpb"))
	out, ok := m.Outputs["docker"]
	if !ok {
		t.Fatal("import did not create the output")
	}
	want := filepath.Join("test_example_project", ".env")
	if out.Path != want {
		t.Errorf("output path = %q, want %q (directory name must survive intact)", out.Path, want)
	}
}

// The documented workflow: import is run from the project root (the
// directory holding every worktree, itself not a git repository), while the
// sample lives inside "main", a real worktree of its own. The stored output
// path must be relative to "main", not to the project root import was run
// from -- otherwise a later render against "main" doubles the segment into
// main/main/src/Api/.env.
func TestImportOutputIsRelativeToTheSamplesOwnWorktreeNotTheCurrentDirectory(t *testing.T) {
	h := newHarness(t)
	if err := h.run("project", "init", "--id", "vpb"); err != nil {
		t.Fatal(err)
	}

	mainDir := filepath.Join(h.root, "main")
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = mainDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(mainDir, ".gitignore"), []byte("src/Api/.env\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(mainDir, "src", "Api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sample := filepath.Join(dir, ".env_example")
	if err := os.WriteFile(sample, []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// h.opts.Dir is still the project root: import is run from there, per the
	// documented workflow.
	if err := h.run("import", sample, "--no-tui", "--output", "docker"); err != nil {
		t.Fatalf("import: %v", err)
	}

	m, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	out, ok := m.Outputs["docker"]
	if !ok {
		t.Fatal("import did not create the output")
	}
	want := filepath.Join("src", "Api", ".env")
	if out.Path != want {
		t.Errorf("output path = %q, want %q (relative to main/, not the project root)", out.Path, want)
	}

	// Rendering from inside main/ must then land the file exactly where the
	// sample itself lives.
	h.opts.Dir = mainDir
	if err := h.run("render", "docker"); err != nil {
		t.Fatalf("render: %v", err)
	}
	rendered := filepath.Join(mainDir, "src", "Api", ".env")
	if _, err := os.Stat(rendered); err != nil {
		t.Errorf("file was not rendered at %s: %v", rendered, err)
	}
}

func TestImportJSONSampleCreatesAJSONOutput(t *testing.T) {
	h := newHarness(t)
	if err := h.run("project", "init", "--id", "fe"); err != nil {
		t.Fatal(err)
	}
	h.gitInit("")
	dir := filepath.Join(h.root, "public")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sample := filepath.Join(dir, "env.json.example")
	if err := os.WriteFile(sample, []byte(`{"backendAPI":"/","auth0Domain":"d"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := h.run("import", sample, "--no-tui", "--output", "web"); err != nil {
		t.Fatalf("import: %v", err)
	}

	m, _ := manifest.Load(config.ManifestPath("fe"))
	out := m.Outputs["web"]
	if out.Format != manifest.FormatJSON {
		t.Errorf("format = %q, want json", out.Format)
	}
	if out.Path != filepath.Join("public", "env.json") {
		t.Errorf("path = %q", out.Path)
	}
	if _, ok := m.Vars["backendAPI"]; !ok {
		t.Error("free-form json keys were not imported")
	}
}

func TestImportRefusesToClobberExistingVariables(t *testing.T) {
	h := newHarness(t)
	if err := h.run("project", "init", "--id", "vpb"); err != nil {
		t.Fatal(err)
	}
	sample := filepath.Join(h.root, ".env_example")
	if err := os.WriteFile(sample, []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.run("import", sample, "--no-tui"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(sample, []byte("A=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := h.run("import", sample, "--no-tui")
	if err == nil {
		t.Fatal("import overwrote an existing variable without --force")
	}
	if !strings.Contains(err.Error(), "A") {
		t.Errorf("error does not name the variable: %v", err)
	}
}

// project init sets no project.output, and until import claims one, a bare
// `trenvy render` has nowhere to write -- the optional [output] argument to
// render/clean/show is optional in name only. import --output is the moment
// an output first exists, so it is the natural place to claim it as the
// project's default when nothing already holds that role.
func TestImportSetsProjectOutputWhenNoneIsSet(t *testing.T) {
	h := newHarness(t)
	if err := h.run("project", "init", "--id", "vpb"); err != nil {
		t.Fatal(err)
	}
	sample := filepath.Join(h.root, ".env_example")
	if err := os.WriteFile(sample, []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := h.run("import", sample, "--no-tui", "--output", "local"); err != nil {
		t.Fatalf("import: %v", err)
	}

	m, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Project.Output != "local" {
		t.Errorf("project.output = %q, want %q", m.Project.Output, "local")
	}
}

// A user who has already chosen a default output must keep it: a second
// import --output must not silently move project.output out from under a
// render/clean/show invocation that relies on it.
func TestImportDoesNotOverwriteAnExistingProjectOutput(t *testing.T) {
	h := newHarness(t)
	if err := h.run("project", "init", "--id", "vpb"); err != nil {
		t.Fatal(err)
	}
	sample := filepath.Join(h.root, ".env_example")
	if err := os.WriteFile(sample, []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.run("import", sample, "--no-tui", "--output", "local"); err != nil {
		t.Fatalf("import: %v", err)
	}

	other := filepath.Join(h.root, "other_example")
	if err := os.WriteFile(other, []byte("B=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.run("import", other, "--no-tui", "--output", "docker"); err != nil {
		t.Fatalf("second import: %v", err)
	}

	m, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Project.Output != "local" {
		t.Errorf("project.output = %q, want the first output %q to survive", m.Project.Output, "local")
	}
}

// This rejection was unreachable before: resolve.WorktreeRoot never fails, and
// for an unversioned project it answers "the sample's own directory", so
// filepath.Rel always succeeded and the guard below could not trip. With a
// real base it can -- a sample outside the project has no sensible rendered
// path, and guessing one would write a credentials file somewhere unrelated.
func TestImportOutputRejectsASampleOutsideTheProject(t *testing.T) {
	h := newHarness(t)
	if err := h.run("project", "init", "--id", "vpb"); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	sample := filepath.Join(outside, ".env_example")
	if err := os.WriteFile(sample, []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := h.run("import", sample, "--no-tui", "--output", "docker")
	if err == nil {
		t.Fatal("import accepted a sample outside the project")
	}
	if !strings.Contains(err.Error(), "not inside a") {
		t.Errorf("error = %v, want it to name the containment problem", err)
	}
}
