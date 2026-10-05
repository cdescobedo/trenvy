package cli_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/manifest"
)

func twoProfileManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
profiles = ["dev", "prod"]
default_profile = "dev"
salt = "abc123"

[vars.APP_URL]
value = "http://+:8080"
profiles = { prod = "https://app.example" }

[vars.ALPHA_TOKEN]
secret = true
`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestUiRemovesAProfileOnceItsNameIsTyped(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", twoProfileManifest(t))
	for _, p := range []string{"dev", "prod"} {
		if err := h.mem.Set("trenvy/vpb/"+p+"/ALPHA_TOKEN", "v-"+p); err != nil {
			t.Fatal(err)
		}
	}
	var view string
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		m = drive(m, r("3"), r("j"), r("d"), r("w"), enter, r("prod"), enter)
		view = m.View()
		return drive(m, r("q")), nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view, "saved 1 change") {
		t.Errorf("view after save:\n%s", view)
	}
	m, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.Project.Profiles, []string{"dev"}) || len(m.Vars["APP_URL"].Profiles) != 0 {
		t.Errorf("manifest after removal: profiles %v, APP_URL %+v", m.Project.Profiles, m.Vars["APP_URL"])
	}
	if ok, _ := h.mem.Has("trenvy/vpb/prod/ALPHA_TOKEN"); ok {
		t.Error("prod's keyring item survived")
	}
	if ok, _ := h.mem.Has("trenvy/vpb/dev/ALPHA_TOKEN"); !ok {
		t.Error("dev's keyring item was deleted")
	}
}

func TestUiUseOnThisMachineMovesTheHeaderAfterSave(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", twoProfileManifest(t))
	var view string
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		m = drive(m, r("3"), r("j"), r("."), enter, r("w"), enter)
		view = m.View()
		return drive(m, r("q")), nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatal(err)
	}
	if st, _ := config.LoadState("vpb"); st.Active != "prod" {
		t.Errorf("Active = %q, want prod", st.Active)
	}
	if head, _, _ := strings.Cut(view, "\n"); !strings.Contains(head, "prod (active)") {
		t.Errorf("header after save = %q, want prod (active)", head)
	}
}

// Review Focus 2, end to end: --profile outranks the saved selection.
func TestUiUseUnderAProfileFlagStaysOutranked(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", twoProfileManifest(t))
	var view string
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		m = drive(m, r("3"), r("j"), r("."), enter, r("w"), enter)
		view = m.View()
		return drive(m, r("q")), nil
	}
	if err := h.run("--profile", "dev", "ui"); err != nil {
		t.Fatal(err)
	}
	if st, _ := config.LoadState("vpb"); st.Active != "prod" {
		t.Errorf("Active = %q, want prod", st.Active)
	}
	if head, _, _ := strings.Cut(view, "\n"); !strings.Contains(head, "dev (--profile)") {
		t.Errorf("header = %q, want dev (--profile)", head)
	}
}
