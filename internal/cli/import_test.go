package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
)

func TestImportSeedsTheManifestFromADotenvSample(t *testing.T) {
	h := newHarness(t)
	if err := h.run("init", "--id", "vpb"); err != nil {
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

func TestImportCreatesAFileTargetForTheSamplesRealPath(t *testing.T) {
	h := newHarness(t)
	if err := h.run("init", "--id", "vpb"); err != nil {
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

	if err := h.run("import", sample, "--no-tui", "--target", "docker"); err != nil {
		t.Fatalf("import: %v", err)
	}

	m, _ := manifest.Load(config.ManifestPath("vpb"))
	tgt, ok := m.Targets["docker"]
	if !ok {
		t.Fatal("import did not create the target")
	}
	// The sample's own path, minus the _example suffix, is the obvious guess.
	if tgt.Path != filepath.Join("src", "DemoApp.Api", ".env") {
		t.Errorf("target path = %q", tgt.Path)
	}
	if tgt.Format != manifest.FormatDotenv {
		t.Errorf("target format = %q", tgt.Format)
	}
}

func TestImportJSONSampleCreatesAJSONTarget(t *testing.T) {
	h := newHarness(t)
	if err := h.run("init", "--id", "fe"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(h.root, "public")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sample := filepath.Join(dir, "env.json.example")
	if err := os.WriteFile(sample, []byte(`{"backendAPI":"/","auth0Domain":"d"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := h.run("import", sample, "--no-tui", "--target", "web"); err != nil {
		t.Fatalf("import: %v", err)
	}

	m, _ := manifest.Load(config.ManifestPath("fe"))
	tgt := m.Targets["web"]
	if tgt.Format != manifest.FormatJSON {
		t.Errorf("format = %q, want json", tgt.Format)
	}
	if tgt.Path != filepath.Join("public", "env.json") {
		t.Errorf("path = %q", tgt.Path)
	}
	if _, ok := m.Vars["backendAPI"]; !ok {
		t.Error("free-form json keys were not imported")
	}
}

func TestImportRefusesToClobberExistingVariables(t *testing.T) {
	h := newHarness(t)
	if err := h.run("init", "--id", "vpb"); err != nil {
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
