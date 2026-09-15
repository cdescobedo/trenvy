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
