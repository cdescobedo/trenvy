package manifest_test

import (
	"testing"

	"github.com/cdescobedo/envctl/internal/manifest"
)

const backendTOML = `
[project]
id = "demo-backend"
profiles = ["dev"]
default_profile = "dev"
default_target = "local"
salt = "b7c1de"

[targets.local]
dest = "process"
include = ["*"]

[targets.docker]
dest = "file"
format = "dotenv"
path = "src/DemoApp.Api/.env"
include = ["*"]

[vars.APP_URL]
value = "http://+:8080"

[vars.APP_DB_CONNECTION]
value = "Server=localhost;Port=8675;"
targets = { docker = "Server=host.docker.internal;Port=8675;" }

[vars.ALPHA_TOKEN]
secret = true
`

func TestParseBackendManifest(t *testing.T) {
	m, err := manifest.Parse([]byte(backendTOML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if m.Project.ID != "demo-backend" {
		t.Errorf("Project.ID = %q, want %q", m.Project.ID, "demo-backend")
	}
	if m.Project.DefaultTarget != "local" {
		t.Errorf("Project.DefaultTarget = %q, want %q", m.Project.DefaultTarget, "local")
	}
	if got := m.Targets["docker"].Path; got != "src/DemoApp.Api/.env" {
		t.Errorf("docker target Path = %q", got)
	}
	if got := m.Targets["local"].Dest; got != "process" {
		t.Errorf("local target Dest = %q, want %q", got, "process")
	}

	urls := m.Vars["APP_URL"]
	if urls.Value == nil || *urls.Value != "http://+:8080" {
		t.Errorf("APP_URL Value = %v", urls.Value)
	}
	if urls.Secret {
		t.Error("APP_URL should not be secret")
	}

	db := m.Vars["APP_DB_CONNECTION"]
	if got := db.Targets["docker"]; got != "Server=host.docker.internal;Port=8675;" {
		t.Errorf("docker override = %q", got)
	}

	key := m.Vars["ALPHA_TOKEN"]
	if !key.Secret {
		t.Error("ALPHA_TOKEN should be secret")
	}
	if key.Value != nil {
		t.Errorf("ALPHA_TOKEN Value = %v, want nil", key.Value)
	}
}

// An empty string is a legal value and must survive parsing distinctly from
// "no value at all" — which is why Var.Value is a pointer.
func TestParseDistinguishesEmptyValueFromAbsent(t *testing.T) {
	m, err := manifest.Parse([]byte("[vars.EMPTY]\nvalue = \"\"\n\n[vars.ABSENT]\nsecret = true\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Vars["EMPTY"].Value == nil {
		t.Error("EMPTY.Value is nil, want pointer to empty string")
	} else if *m.Vars["EMPTY"].Value != "" {
		t.Errorf("EMPTY.Value = %q, want empty", *m.Vars["EMPTY"].Value)
	}
	if m.Vars["ABSENT"].Value != nil {
		t.Error("ABSENT.Value should be nil")
	}
}

// Maps must be non-nil even when sections are absent, so downstream code
// can write into them without panicking (e.g., envctl set writes to
// m.Vars[name] and later tasks write to m.Targets[targetName]).
func TestParseNormalizesNilMapsToEmpty(t *testing.T) {
	m, err := manifest.Parse([]byte("[project]\nid = \"test\"\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Targets == nil {
		t.Error("Targets is nil, want empty map")
	}
	if len(m.Targets) != 0 {
		t.Errorf("Targets has %d entries, want 0", len(m.Targets))
	}
	if m.Vars == nil {
		t.Error("Vars is nil, want empty map")
	}
	if len(m.Vars) != 0 {
		t.Errorf("Vars has %d entries, want 0", len(m.Vars))
	}
}
