package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/cli"
	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/store"
)

// harness wires a hermetic envctl: a temp config dir, a temp project root, and
// an in-memory secret store. Every CLI test uses it, which is what keeps the
// suite from touching a real keyring or a real config directory.
type harness struct {
	t    *testing.T
	root string
	mem  *store.Mem
	out  *bytes.Buffer
	errb *bytes.Buffer
	opts cli.Options
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	t.Setenv("ENVCTL_CONFIG_DIR", t.TempDir())

	h := &harness{
		t:    t,
		root: t.TempDir(),
		mem:  store.NewMem(),
		out:  &bytes.Buffer{},
		errb: &bytes.Buffer{},
	}
	h.opts = cli.Options{
		Stdin:    strings.NewReader(""),
		Stdout:   h.out,
		Stderr:   h.errb,
		Dir:      h.root,
		NewStore: func(*config.Config, string) (store.Store, error) { return h.mem, nil },
	}
	return h
}

func (h *harness) run(args ...string) error {
	h.t.Helper()
	h.out.Reset()
	h.errb.Reset()
	return cli.Execute(h.opts, args)
}

// register writes a registry entry and a manifest, standing in for `init`.
func (h *harness) register(id string, m *manifest.Manifest) {
	h.t.Helper()
	r, err := config.LoadRegistry()
	if err != nil {
		h.t.Fatal(err)
	}
	if err := r.Add(config.Entry{ID: id, Root: h.root}); err != nil {
		h.t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		h.t.Fatal(err)
	}
	if err := manifest.Save(config.ManifestPath(id), m); err != nil {
		h.t.Fatal(err)
	}
}

func fixtureManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(`
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
`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Every command that needs a project must fail clearly outside one, rather
// than with a confusing missing-file error.
func TestCommandsOutsideAProjectExplainThemselves(t *testing.T) {
	h := newHarness(t)
	err := h.run("status")
	if err == nil {
		t.Fatal("status succeeded outside a project")
	}
	// Pinned to the full "project init" spelling, not just "envctl init" --
	// that shorter substring is also present in stale text pointing at the
	// now-deleted top-level init command, so it cannot tell the two apart.
	if !strings.Contains(err.Error(), "envctl project init") {
		t.Errorf("error does not suggest project init: %v", err)
	}
}

func TestResolutionWorksFromASubdirectory(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k")

	sub := filepath.Join(h.root, "main", "src", "deep")
	if err := mkdirAll(sub); err != nil {
		t.Fatal(err)
	}
	h.opts.Dir = sub

	if err := h.run("status"); err != nil {
		t.Fatalf("status from a subdirectory: %v", err)
	}
	if !strings.Contains(h.out.String(), "vpb") {
		t.Errorf("status did not resolve the project:\n%s", h.out)
	}
}

// The --backend flag is parsed before resolveApp runs, but the value only
// reaches the store if it is threaded through NewStore explicitly: it is
// bound into a copy of opts.Backend at Execute time, not observable via a
// closure captured earlier.
func TestBackendFlagReachesTheStoreFactory(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k")

	var gotBackend string
	h.opts.NewStore = func(cfg *config.Config, backend string) (store.Store, error) {
		gotBackend = backend
		return h.mem, nil
	}

	if err := h.run("status", "--backend", "mem"); err != nil {
		t.Fatalf("status: %v", err)
	}
	if gotBackend != "mem" {
		t.Errorf("NewStore received backend %q, want %q", gotBackend, "mem")
	}
}

// A project that never touches a secret must work even when the configured
// backend cannot be opened at all -- the file backend errors outright with no
// passphrase set. resolveApp must not open a store it will never use.
func TestZeroSecretProjectWorksWithTheFileBackendAndNoPassphrase(t *testing.T) {
	h := newHarness(t)
	h.gitInit("public/env.json\n")
	m, err := manifest.Parse([]byte(`
[project]
id = "fe"
profiles = ["dev"]
default_profile = "dev"
output = "web"
salt = "abc123"

[outputs.web]
format = "json"
path = "public/env.json"
include = ["*"]

[vars.PUBLIC_URL]
value = "https://example.com"
`))
	if err != nil {
		t.Fatal(err)
	}
	h.register("fe", m)

	t.Setenv("ENVCTL_BACKEND", "file")
	t.Setenv("ENVCTL_PASSPHRASE", "")
	h.opts.NewStore = func(cfg *config.Config, backend string) (store.Store, error) {
		return store.Open(backend, "someone")
	}

	if err := h.run("status"); err != nil {
		t.Fatalf("status on a zero-secret project with an unopenable backend: %v", err)
	}
	if err := h.run("render", "web"); err != nil {
		t.Fatalf("render on a zero-secret project with an unopenable backend: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.root, "public", "env.json")); err != nil {
		t.Errorf("render did not write the file: %v", err)
	}
	if err := h.run("set", "PUBLIC_URL", "https://example.org"); err != nil {
		t.Fatalf("set on a zero-secret project with an unopenable backend: %v", err)
	}
}

// The profile decides which keyring item a secret reads, so a flag that does
// not reach the service name is a flag that silently reads the wrong secret.
func TestProfileFlagSelectsTheKeyringItem(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	h.mem.Seed("envctl/vpb/prod/ALPHA_TOKEN", "prod-key")

	if err := h.run("--profile", "prod", "get", "ALPHA_TOKEN"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := h.out.String(); got != "prod-key\n" {
		t.Errorf("stdout = %q, want the prod secret", got)
	}
}

func TestProfileEnvVarSelectsTheKeyringItem(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	h.mem.Seed("envctl/vpb/prod/ALPHA_TOKEN", "prod-key")
	t.Setenv("ENVCTL_PROFILE", "prod")

	if err := h.run("get", "ALPHA_TOKEN"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := h.out.String(); got != "prod-key\n" {
		t.Errorf("stdout = %q, want the prod secret", got)
	}
}

func TestAnUndeclaredProfileFailsBeforeAnythingIsRead(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	err := h.run("--profile", "staging", "get", "ALPHA_TOKEN")

	if err == nil {
		t.Fatal("get succeeded with an undeclared profile")
	}
	if !strings.Contains(err.Error(), "from --profile") {
		t.Errorf("error does not name --profile as the source of the bad name: %v", err)
	}
	if calls := h.mem.Calls(); len(calls) != 0 {
		t.Errorf("the store was touched before the profile was validated: %v", calls)
	}
}

func mkdirAll(p string) error { return os.MkdirAll(p, 0o755) }

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
