package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manage"
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

// A sample with no output left the user with variables and nowhere to put
// them, which is the state both migrated projects landed in.
func TestImportCreatesAnOutputByDefault(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	sample := h.writeSample(t, "src/Demo.Api/.env_example", "A=1\nB=2\n")

	if err := h.run("import", sample, "--no-tui"); err != nil {
		t.Fatal(err)
	}

	m := h.manifest(t, "p")
	o, ok := m.Outputs["api"]
	if !ok {
		t.Fatalf("no output named api; got %v", m.Outputs)
	}
	if o.Path != "src/Demo.Api/.env" {
		t.Errorf("Path = %q, want src/Demo.Api/.env", o.Path)
	}
	if o.Format != manifest.FormatDotenv {
		t.Errorf("Format = %q, want dotenv", o.Format)
	}
	if m.Project.Output != "api" {
		t.Errorf("project.output = %q, want api", m.Project.Output)
	}
}

func TestImportNoOutputCreatesNone(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	sample := h.writeSample(t, "src/Demo.Api/.env_example", "A=1\n")

	if err := h.run("import", sample, "--no-tui", "--no-output"); err != nil {
		t.Fatal(err)
	}

	m := h.manifest(t, "p")
	if len(m.Outputs) != 0 {
		t.Errorf("Outputs = %v, want none", m.Outputs)
	}
	if len(m.Vars) == 0 {
		t.Error("--no-output suppressed the variables too")
	}
}

func TestImportRefusesOutputAndNoOutputTogether(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	sample := h.writeSample(t, "a/.env_example", "A=1\n")

	err := h.run("import", sample, "--no-tui", "--output", "x", "--no-output")

	if err == nil {
		t.Fatal("import accepted both flags")
	}
	if !strings.Contains(err.Error(), "--no-output") {
		t.Errorf("error does not name the conflict: %v", err)
	}
}

// Two outputs on one path clobber each other on render, and the second one's
// record would make the first look MODIFIED.
func TestImportReusesAnExistingOutputAtTheSamePath(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	sample := h.writeSample(t, "src/Demo.Api/.env_example", "A=1\n")
	if err := h.run("import", sample, "--no-tui", "--output", "chosen"); err != nil {
		t.Fatal(err)
	}

	if err := h.run("import", sample, "--no-tui"); err != nil {
		t.Fatal(err)
	}

	m := h.manifest(t, "p")
	if len(m.Outputs) != 1 {
		t.Errorf("Outputs = %v, want exactly one", m.Outputs)
	}
	if _, ok := m.Outputs["chosen"]; !ok {
		t.Error("the second import replaced the user's chosen name")
	}
}

// import --output must refuse to redefine an existing output by NAME, the
// same rule `output add` already enforces at output.go:123. Without this
// guard, `import --output web` against a project that already has a "web"
// output silently replaces web's format and path, orphaning whatever it
// used to render -- `trenvy clean web` then targets the wrong file.
func TestImportRefusesToRedefineAnExistingOutputByName(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	first := h.writeSample(t, "src/Web/.env_example", "A=1\n")
	if err := h.run("import", first, "--no-tui", "--output", "web"); err != nil {
		t.Fatal(err)
	}
	before := h.manifest(t, "p").Outputs["web"]

	second := h.writeSample(t, "src/Other/.env_example", "B=2\n")
	err := h.run("import", second, "--no-tui", "--output", "web")

	if err == nil {
		t.Fatal("import redefined an existing output by name")
	}
	if !strings.Contains(err.Error(), `output "web" is already defined`) {
		t.Errorf("error = %v, want it to match output add's wording", err)
	}
	after := h.manifest(t, "p")
	if got := after.Outputs["web"]; got.Path != before.Path || got.Format != before.Format {
		t.Errorf("output web = %+v, want unchanged from %+v", got, before)
	}
	if _, ok := after.Vars["B"]; ok {
		t.Error("the refused import wrote the second sample's variable anyway")
	}
}

// The path-reuse branch is different from the name-collision guard above and
// must keep working: importing the same sample again resolves to the same
// output path, so it reuses the existing output silently even though the
// same name is given explicitly.
func TestImportSamePathReusesSilentlyEvenWithTheSameNameGiven(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	sample := h.writeSample(t, "src/Web/.env_example", "A=1\n")
	if err := h.run("import", sample, "--no-tui", "--output", "web"); err != nil {
		t.Fatal(err)
	}

	if err := h.run("import", sample, "--no-tui", "--output", "web"); err != nil {
		t.Fatalf("re-importing the same sample at the same path was refused: %v", err)
	}

	m := h.manifest(t, "p")
	if len(m.Outputs) != 1 {
		t.Errorf("Outputs = %v, want exactly one", m.Outputs)
	}
}

// import writes Include: ["*"], a literal glob that goes through
// path.Match, whose "*" does not cross "/". A variable named GROUP/KEY is
// silently dropped from the rendered file instead of tripping the
// identifier check render/dotenv.go already has. output add leaves Include
// empty, which resolve.selected short-circuits to "everything" before
// path.Match ever runs, so it reports the dotenv error correctly. The two
// commands must agree on the same variable.
func TestImportAndOutputAddAgreeOnASlashedVariableName(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")

	m := h.manifest(t, "p")
	v := "1"
	m.Vars["GROUP/KEY"] = manifest.Var{Value: &v}
	if err := manifest.Save(config.ManifestPath("p"), m); err != nil {
		t.Fatal(err)
	}

	if err := h.run("output", "add", "added", "--format", "dotenv", "--path", "added.env"); err != nil {
		t.Fatal(err)
	}
	sample := h.writeSample(t, "src/Demo.Api/.env_example", "OTHER=1\n")
	if err := h.run("import", sample, "--no-tui", "--output", "imported"); err != nil {
		t.Fatal(err)
	}

	addErr := h.run("render", "added")
	impErr := h.run("render", "imported")

	if addErr == nil || !strings.Contains(addErr.Error(), "is not a valid shell identifier") {
		t.Fatalf("output add's render = %v, want the dotenv identifier error", addErr)
	}
	if impErr == nil {
		t.Fatal("import's output silently dropped GROUP/KEY instead of erroring like output add")
	}
	if !strings.Contains(impErr.Error(), "is not a valid shell identifier") {
		t.Errorf("import's output error = %v, want the same dotenv identifier error as output add", impErr)
	}
}

// requireValidOutputName is wired into output add and output rename but not
// import, so a bad --output name reached Manifest.Validate and was reported
// as "invalid manifest: output ...", misattributing a bad argument to a
// corrupt config file.
func TestImportRefusesABadOutputName(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	sample := h.writeSample(t, ".env_example", "A=1\n")

	err := h.run("import", sample, "--no-tui", "--output", "my.app")

	if err == nil || !strings.Contains(err.Error(), "my.app") {
		t.Errorf("error = %v, want it to reject the name", err)
	}
	if err != nil && strings.Contains(err.Error(), "invalid manifest") {
		t.Errorf("error = %v; a bad argument must not be reported as a corrupt manifest", err)
	}
}

// The --output/--no-output conflict must be caught before the sample file is
// even read, let alone before the interactive review runs: catching it only
// after would discard a whole review session over a flag typo. A sample path
// that does not exist on disk proves the ordering -- if the conflict were
// checked after the read, the error would be a read failure instead.
func TestImportChecksTheFlagConflictBeforeReadingTheSample(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")

	err := h.run("import", "/does/not/exist/.env_example", "--output", "x", "--no-output")

	if err == nil {
		t.Fatal("import accepted both flags")
	}
	if !strings.Contains(err.Error(), "--no-output") {
		t.Errorf("error = %v, want the flag conflict, checked before the sample is read", err)
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

// sameVar treats {Secret:false, Value:nil} as equal to any other nil-Value
// var only when Secret also matches. An optional plain variable with no
// default has exactly that shape, and a sample placeholder for the same name
// parses to {Secret:true, Value:nil} -- same Value, different Secret. Without
// the Secret comparison, sameVar would call that "unchanged" and import would
// silently flip the user's optional plain variable into a secret.
func TestImportRefusesToFlipAnOptionalVariableToSecret(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")

	m := h.manifest(t, "p")
	m.Vars["A"] = manifest.Var{Optional: true}
	if err := manifest.Save(config.ManifestPath("p"), m); err != nil {
		t.Fatal(err)
	}

	sample := h.writeSample(t, ".env_example", "A=changeme\n")
	err := h.run("import", sample, "--no-tui")
	if err == nil {
		t.Fatal("import silently flipped an optional variable to secret")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error does not name --force: %v", err)
	}

	after := h.manifest(t, "p")
	if after.Vars["A"].Secret {
		t.Error("A was flipped to secret despite the refusal")
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

func TestInteractiveImportAppliesTheReview(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "vpb")
	sample := filepath.Join(h.root, ".env.example")
	writeFile(t, sample, "APP_MODE=development\nAPP_URL=http://x\n")
	h.opts.RunSetup = func(s manage.Setup) (manage.Setup, error) {
		return driveSetup(s, r(" "), r("j"), r("d"), enter, enter), nil // APP_MODE secret, APP_URL left out, summary, import
	}
	if err := h.run("import", sample); err != nil {
		t.Fatal(err)
	}
	m := h.manifest(t, "vpb")
	if v := m.Vars["APP_MODE"]; !v.Secret {
		t.Errorf("APP_MODE = %+v, want secret", v)
	}
	if _, ok := m.Vars["APP_URL"]; ok {
		t.Error("APP_URL was imported though it was left out")
	}
}

func TestInteractiveImportCancelWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "vpb")
	sample := filepath.Join(h.root, ".env.example")
	writeFile(t, sample, "APP_MODE=development\n")
	h.opts.RunSetup = func(s manage.Setup) (manage.Setup, error) { return driveSetup(s, esc), nil }
	if err := h.run("import", sample); err == nil || !strings.Contains(err.Error(), "import cancelled") {
		t.Fatalf("err = %v", err)
	}
	if _, ok := h.manifest(t, "vpb").Vars["APP_MODE"]; ok {
		t.Error("a cancelled import wrote APP_MODE")
	}
}

// Review Focus 5, end to end.
func TestInteractiveImportConflictWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "vpb")
	if err := h.run("set", "APP_MODE", "x"); err != nil {
		t.Fatal(err)
	}
	sample := filepath.Join(h.root, ".env.example")
	writeFile(t, sample, "APP_MODE=development\n")
	h.opts.RunSetup = func(s manage.Setup) (manage.Setup, error) {
		s = driveSetup(s, enter, enter) // the summary refuses
		if _, ok := s.Result(); ok {
			t.Error("the summary accepted a conflict without --force")
		}
		return driveSetup(s, tea.KeyMsg{Type: tea.KeyCtrlC}), nil
	}
	if err := h.run("import", sample); err == nil {
		t.Fatal("a refused import returned nil")
	}
	if v := h.manifest(t, "vpb").Vars["APP_MODE"]; v.Value != nil || v.Profiles["dev"] != "x" {
		t.Errorf("APP_MODE = %+v, want the dev value x untouched", v)
	}
	h.opts.RunSetup = func(s manage.Setup) (manage.Setup, error) { return driveSetup(s, enter, enter), nil }
	if err := h.run("import", sample, "--force"); err != nil {
		t.Fatalf("with --force: %v", err)
	}
	if v := h.manifest(t, "vpb").Vars["APP_MODE"]; v.Value == nil || *v.Value != "development" {
		t.Errorf("with --force APP_MODE = %+v", v)
	}
}
