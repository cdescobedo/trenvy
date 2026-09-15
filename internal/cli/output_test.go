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
