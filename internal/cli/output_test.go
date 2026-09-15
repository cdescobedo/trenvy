package cli_test

import (
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
)

func TestOutputListShowsEachOutputAndMarksTheDefault(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	h.setOutputs(t, "p", map[string]manifest.Output{
		"api":  {Format: "dotenv", Path: "src/.env", Include: []string{"*"}, Exclude: []string{"DEV_CERTS_PATH"}},
		"root": {Format: "dotenv", Path: ".env", Include: []string{"DEV_CERTS_PATH"}},
	}, "api")

	if err := h.run("output", "list"); err != nil {
		t.Fatal(err)
	}

	out := h.out.String()
	for _, want := range []string{"api", "root", "src/.env", ".env", "all except DEV_CERTS_PATH"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "api *") && !strings.Contains(out, "* api") {
		t.Errorf("the default output is not marked:\n%s", out)
	}
}

// A project that only ever feeds exec and shell legitimately has no outputs.
func TestOutputListWithNoOutputsIsNotAnError(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")

	if err := h.run("output", "list"); err != nil {
		t.Fatalf("list errored on a project with no outputs: %v", err)
	}
	if !strings.Contains(h.out.String(), "output add") {
		t.Errorf("list does not say how to make one:\n%s", h.out.String())
	}
}

func TestOutputAddCreatesOne(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")

	err := h.run("output", "add", "web", "--format", "json",
		"--path", "public/env.json", "--include", "PUBLIC_*")

	if err != nil {
		t.Fatal(err)
	}
	o, ok := h.manifest(t, "p").Outputs["web"]
	if !ok {
		t.Fatal("output web was not created")
	}
	if o.Format != "json" || o.Path != "public/env.json" {
		t.Errorf("output = %+v", o)
	}
	if len(o.Include) != 1 || o.Include[0] != "PUBLIC_*" {
		t.Errorf("Include = %v, want [PUBLIC_*]", o.Include)
	}
}

func TestOutputAddDoesNotClaimTheDefault(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")

	if err := h.run("output", "add", "web", "--format", "json", "--path", "e.json"); err != nil {
		t.Fatal(err)
	}

	if got := h.manifest(t, "p").Project.Output; got != "" {
		t.Errorf("project.output = %q; add must leave it to `output default`", got)
	}
}

func TestOutputAddRefusesADuplicatePath(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	if err := h.run("output", "add", "a", "--format", "dotenv", "--path", ".env"); err != nil {
		t.Fatal(err)
	}

	err := h.run("output", "add", "b", "--format", "dotenv", "--path", ".env")

	if err == nil {
		t.Fatal("add accepted a second output on one path")
	}
	if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), ".env") {
		t.Errorf("error names neither the incumbent nor the path: %v", err)
	}
	if _, exists := h.manifest(t, "p").Outputs["b"]; exists {
		t.Error("the refused output was written anyway")
	}
}

func TestOutputAddRefusesABadName(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")

	err := h.run("output", "add", "my.app", "--format", "dotenv", "--path", ".env")

	if err == nil || !strings.Contains(err.Error(), "my.app") {
		t.Errorf("error = %v, want it to reject the name", err)
	}
	if err != nil && strings.Contains(err.Error(), "invalid manifest") {
		t.Errorf("error = %v; a bad argument must not be reported as a corrupt manifest", err)
	}
}

func TestOutputDefaultSetsIt(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	if err := h.run("output", "add", "web", "--format", "json", "--path", "e.json"); err != nil {
		t.Fatal(err)
	}

	if err := h.run("output", "default", "web"); err != nil {
		t.Fatal(err)
	}

	if got := h.manifest(t, "p").Project.Output; got != "web" {
		t.Errorf("project.output = %q, want web", got)
	}
}

func TestOutputDefaultRefusesAnUnknownName(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")

	err := h.run("output", "default", "nope")

	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("error = %v, want it to name the unknown output", err)
	}
}

// The manifest and the state file must move together. A test that checked
// only the manifest would pass against the exact bug this command prevents.
func TestOutputRenameMovesTheManifestAndTheStateRecord(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	h.setOutputs(t, "p", map[string]manifest.Output{
		"app": {Format: "dotenv", Path: ".env", Include: []string{"*"}},
	}, "app")
	h.recordRender(t, "p", h.root, "app", ".env")

	if err := h.run("output", "rename", "app", "docker"); err != nil {
		t.Fatal(err)
	}

	m := h.manifest(t, "p")
	if _, gone := m.Outputs["app"]; gone {
		t.Error("the old name is still defined")
	}
	if _, ok := m.Outputs["docker"]; !ok {
		t.Error("the new name was not created")
	}
	if m.Project.Output != "docker" {
		t.Errorf("project.output = %q, want docker", m.Project.Output)
	}
	st := h.state(t, "p")
	if _, ok := st.Find(h.root, "docker"); !ok {
		t.Error("the render record was not repointed; the file would read FOREIGN")
	}
}

func TestOutputRenameRefusesAnExistingName(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	h.setOutputs(t, "p", map[string]manifest.Output{
		"a": {Format: "dotenv", Path: "a.env"},
		"b": {Format: "dotenv", Path: "b.env"},
	}, "a")

	err := h.run("output", "rename", "a", "b")

	if err == nil || !strings.Contains(err.Error(), "b") {
		t.Errorf("error = %v, want it to name the collision", err)
	}
	if _, ok := h.manifest(t, "p").Outputs["a"]; !ok {
		t.Error("the refused rename removed the original anyway")
	}
}

// Records can exist in worktrees the user is not standing in. Deleting the
// output silently would orphan those files: the name they are keyed under
// would be gone, so trenvy clean could no longer reach them.
//
// This passes --yes precisely to prove that --yes alone does not bypass the
// record refusal: --yes answers the confirmation prompt, --force answers
// this refusal, and the two are independent.
func TestOutputRmRefusesWhileRenderRecordsExist(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	h.setOutputs(t, "p", map[string]manifest.Output{
		"app": {Format: "dotenv", Path: ".env"},
	}, "app")
	h.recordRender(t, "p", h.root, "app", ".env")

	err := h.run("output", "rm", "app", "--yes")

	if err == nil {
		t.Fatal("rm removed an output that had been rendered")
	}
	if !strings.Contains(err.Error(), h.root) {
		t.Errorf("error does not name the worktree: %v", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error does not name the way forward: %v", err)
	}
	if _, ok := h.manifest(t, "p").Outputs["app"]; !ok {
		t.Error("the refused rm removed it anyway")
	}
}

func TestOutputRmForceDropsTheRecordsAndKeepsTheFiles(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	h.setOutputs(t, "p", map[string]manifest.Output{
		"app": {Format: "dotenv", Path: ".env"},
	}, "app")
	h.recordRender(t, "p", h.root, "app", ".env")

	if err := h.run("output", "rm", "app", "--yes", "--force"); err != nil {
		t.Fatal(err)
	}

	if _, ok := h.manifest(t, "p").Outputs["app"]; ok {
		t.Error("the output survived --force")
	}
	if _, ok := h.state(t, "p").Find(h.root, "app"); ok {
		t.Error("the render record survived --force")
	}
}

// --force alone must still prompt: it answers the render-record refusal, not
// the confirmation question. An unattended run that only set --force, never
// intending to answer the prompt, must not silently proceed.
func TestOutputRmForceWithoutYesStillPrompts(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	h.setOutputs(t, "p", map[string]manifest.Output{
		"app": {Format: "dotenv", Path: ".env"},
	}, "app")
	h.recordRender(t, "p", h.root, "app", ".env")
	h.opts.Stdin = strings.NewReader("") // EOF: nothing typed at the prompt

	err := h.run("output", "rm", "app", "--force")

	if err == nil {
		t.Fatal("rm --force with no --yes and no confirmation removed the output")
	}
	if _, ok := h.manifest(t, "p").Outputs["app"]; !ok {
		t.Error("the unconfirmed rm removed it anyway")
	}
	if !strings.Contains(h.errb.String(), "Remove output") {
		t.Errorf("rm --force did not prompt for confirmation:\n%s", h.errb.String())
	}
}

// Removing the default is allowed; it clears project.output and says so,
// because refusing until the user repoints is ceremony for a reversible edit.
func TestOutputRmClearsTheDefaultItHeld(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")
	h.setOutputs(t, "p", map[string]manifest.Output{
		"app": {Format: "dotenv", Path: ".env"},
	}, "app")

	if err := h.run("output", "rm", "app", "--yes"); err != nil {
		t.Fatal(err)
	}

	m := h.manifest(t, "p")
	if m.Project.Output != "" {
		t.Errorf("project.output = %q, want cleared", m.Project.Output)
	}
	if !strings.Contains(h.out.String(), "render") {
		t.Errorf("rm did not warn that bare render now needs an argument:\n%s", h.out.String())
	}
}

func TestOutputRmRefusesAnUnknownName(t *testing.T) {
	h := newHarness(t)
	h.initProject(t, "p")

	err := h.run("output", "rm", "nope", "--yes")

	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("error = %v, want it to name the unknown output", err)
	}
}
