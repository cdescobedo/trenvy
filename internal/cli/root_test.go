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
default_target = "local"
salt = "abc123"

[targets.local]
dest = "process"
include = ["*"]

[targets.docker]
dest = "file"
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

func TestProjectsListsRegisteredProjects(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	if err := h.run("projects"); err != nil {
		t.Fatalf("projects: %v", err)
	}
	out := h.out.String()
	if !strings.Contains(out, "vpb") {
		t.Errorf("output does not list the project:\n%s", out)
	}
	if !strings.Contains(out, h.root) {
		t.Errorf("output does not show the root:\n%s", out)
	}
}

func TestProjectsOnAnEmptyRegistry(t *testing.T) {
	h := newHarness(t)
	if err := h.run("projects"); err != nil {
		t.Fatalf("projects: %v", err)
	}
	if !strings.Contains(h.out.String(), "envctl init") {
		t.Errorf("output does not suggest how to register one:\n%s", h.out)
	}
}

// Every command that needs a project must fail clearly outside one, rather
// than with a confusing missing-file error.
func TestCommandsOutsideAProjectExplainThemselves(t *testing.T) {
	h := newHarness(t)
	err := h.run("status")
	if err == nil {
		t.Fatal("status succeeded outside a project")
	}
	if !strings.Contains(err.Error(), "envctl init") {
		t.Errorf("error does not suggest init: %v", err)
	}
}

func TestResolutionWorksFromASubdirectory(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "k")

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
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "k")

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

func mkdirAll(p string) error { return os.MkdirAll(p, 0o755) }

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
