package cli_test

import (
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/manifest"
)

// The tests in this file are the original interface's Overview, Outputs tab
// and switcher tests (ui_test.go, ui_switch_test.go), carried over to the
// interface that replaced it.
// The Overview is gone: its problem list is the header's "! N problems",
// and its output facts are the Outputs screen's list and detail.

// webOnlyManifest is one plain variable and one output, web, selecting it.
func webOnlyManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
profiles = ["dev"]
default_profile = "dev"
output = "web"
salt = "abc123"

[outputs.web]
format = "json"
path = "public/env.json"
include = ["APP_URL"]

[vars.APP_URL]
value = "http://+:8080"
`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// screenAt opens `trenvy ui` and returns the view after keys.
func screenAt(t *testing.T, h *harness, keys ...string) string {
	t.Helper()
	var view string
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		for _, k := range keys {
			m = drive(m, r(k))
		}
		if len(keys) == 0 {
			m = drive(m)
		}
		view = m.View()
		return m, nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}
	return view
}

// listRow is the Outputs list row naming name. Only the list pane's part
// of each line is searched, so the detail pane's title, drawn beside
// another output's row, is never mistaken for it.
func listRow(t *testing.T, view, name string) string {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		list, _, _ := strings.Cut(line, "││")
		if strings.Contains(list, "> "+name+" ") || strings.Contains(list, "  "+name+" ") {
			return list
		}
	}
	t.Fatalf("%q row not found:\n%s", name, view)
	return ""
}

// headerLine is the view's first line, where the problem pill sits.
func headerLine(view string) string {
	line, _, _ := strings.Cut(view, "\n")
	return line
}

// The state word comes from the guard.Inspect path `trenvy status` uses: a
// hand-written file with no render record is FOREIGN, and is a problem. The
// output selects only a plain variable, so quick mode reaches the guard.
func TestOutputsScreenReportsStateFromTheGuardPath(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", webOnlyManifest(t))
	h.writeSample(t, "public/env.json", "{}\n") // no render record -> FOREIGN

	view := screenAt(t, h, "2")
	if row := listRow(t, view, "web"); !strings.Contains(row, "FOREIGN") {
		t.Errorf("web row = %q, want FOREIGN via guard.Inspect:\n%s", row, view)
	}
	if !strings.Contains(headerLine(view), "! 1 problem") {
		t.Errorf("header does not count the foreign output as a problem:\n%s", view)
	}
}

// A secret genuinely missing from the keyring -- not merely unread in quick
// mode -- is a real "unknown": the file exists (a stray one, no record), so
// this is the secrets-missing branch, not the never-rendered one.
func TestOutputsScreenReportsUnknownForAMissingSecret(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t)) // docker includes ALPHA_TOKEN (secret)
	h.writeSample(t, "src/.env", "FOO=bar\n")
	// ALPHA_TOKEN deliberately never seeded -- missing under dev.

	view := screenAt(t, h, "2")
	if row := listRow(t, view, "docker"); !strings.Contains(row, "unknown") {
		t.Errorf("docker row = %q, want unknown:\n%s", row, view)
	}
	if !strings.Contains(view, "a secret is missing") {
		t.Errorf("the detail does not say why the state is unknown:\n%s", view)
	}
	if strings.Contains(view, "--quick") {
		t.Errorf("the screen leaks the --quick flag name:\n%s", view)
	}
}

// An output never rendered -- the ordinary state of a fresh clone -- is not
// a problem. It reads "not rendered", trenvy status's own words.
func TestHeaderHasNoProblemForAnOutputNeverRendered(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", webOnlyManifest(t))
	// public/env.json is never written at all.

	view := screenAt(t, h, "2")
	if strings.Contains(headerLine(view), "problem") {
		t.Errorf("a never-rendered output produced a problem:\n%s", view)
	}
	if row := listRow(t, view, "web"); !strings.Contains(row, "not rendered") {
		t.Errorf("web row = %q, want not rendered:\n%s", row, view)
	}
}

// The same with a secret in the output: quick mode must still see that the
// file is absent before it skips reading the secret.
func TestHeaderHasNoProblemForASecretOutputNeverRendered(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t)) // docker includes ALPHA_TOKEN (secret)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "v")
	// src/.env is never written at all.

	view := screenAt(t, h, "2")
	if strings.Contains(headerLine(view), "problem") {
		t.Errorf("a never-rendered secret-bearing output produced a problem:\n%s", view)
	}
	if row := listRow(t, view, "docker"); !strings.Contains(row, "not rendered") {
		t.Errorf("docker row = %q, want not rendered:\n%s", row, view)
	}
}

// A rendered secret-bearing output cannot be verified without reading its
// secret, so it is neither a problem nor "fresh": it reads "unverified",
// the new interface's word for status --quick's "unknown (--quick)".
func TestHeaderHasNoProblemForAFreshSecretOutput(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t)) // docker includes ALPHA_TOKEN (secret)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "s3cretvalue")
	if err := h.run("render", "docker"); err != nil {
		t.Fatalf("render: %v", err)
	}

	view := screenAt(t, h, "2")
	if strings.Contains(headerLine(view), "problem") {
		t.Errorf("a rendered, fresh, secret-bearing output produced a problem:\n%s", view)
	}
	row := listRow(t, view, "docker")
	if strings.Contains(row, "fresh") {
		t.Errorf("docker row claims fresh, which this view cannot verify: %q", row)
	}
	if !strings.Contains(row, "unverified") {
		t.Errorf("docker row = %q, want the honest unverified", row)
	}
}

// A secret-bearing output that IS stale -- a plain value it selects changed
// since the render -- is indistinguishable in quick mode from the fresh one
// above, so it reads "unverified" too, never "fresh".
func TestOutputsScreenShowsUnverifiedNeverFresh(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t)) // docker includes ALPHA_TOKEN (secret) + APP_URL (plain)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "s3cretvalue")
	if err := h.run("render", "docker"); err != nil {
		t.Fatalf("render: %v", err)
	}
	if err := h.run("set", "APP_URL", "http://+:9090"); err != nil {
		t.Fatalf("set: %v", err)
	}

	view := screenAt(t, h, "2")
	if strings.Contains(headerLine(view), "problem") {
		t.Errorf("a STALE secret-bearing output this view cannot verify produced a problem:\n%s", view)
	}
	row := listRow(t, view, "docker")
	if strings.Contains(row, "fresh") {
		t.Errorf("a genuinely STALE output's row falsely claims fresh: %q", row)
	}
	if !strings.Contains(row, "unverified") {
		t.Errorf("docker row = %q, want the honest unverified", row)
	}
}

// twoOutputsManifest has docker (the default, everything except
// INTERNAL_ONLY) and public (only APP_URL), and no secrets.
func twoOutputsManifest(t *testing.T) *manifest.Manifest {
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
exclude = ["INTERNAL_ONLY"]

[outputs.public]
format = "json"
path = "public/env.json"
include = ["APP_URL"]

[vars.APP_URL]
value = "http://+:8080"

[vars.INTERNAL_ONLY]
value = "x"
`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// The Outputs screen shows the facts `trenvy output list` prints -- name,
// format, path, what it selects -- with the default marked and the state
// word from the same states map. Selection is a checkbox per variable and a
// sentence for variables added later, where the old tab printed prose.
func TestOutputsScreenShowsEachOutputsFacts(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", twoOutputsManifest(t))

	docker := screenAt(t, h, "2")
	public := screenAt(t, h, "2", "j")

	if row := listRow(t, docker, "docker"); !strings.Contains(row, "default") || !strings.Contains(row, "not rendered") {
		t.Errorf("docker row = %q, want default and not rendered:\n%s", row, docker)
	}
	if row := listRow(t, docker, "public"); strings.Contains(row, "default") || !strings.Contains(row, "not rendered") {
		t.Errorf("public row = %q, want not rendered and not default:\n%s", row, docker)
	}
	for _, want := range []string{"dotenv", "src/.env", "[x] APP_URL", "[ ] INTERNAL_ONLY", "included automatically"} {
		if !strings.Contains(docker, want) {
			t.Errorf("docker's detail missing %q:\n%s", want, docker)
		}
	}
	for _, want := range []string{"json", "public/env.json", "[x] APP_URL", "[ ] INTERNAL_ONLY", "only the checked ones"} {
		if !strings.Contains(public, want) {
			t.Errorf("public's detail missing %q:\n%s", want, public)
		}
	}
}

// A project with no outputs -- the normal shape for one that only feeds
// exec and shell -- reads as a state that names how to add one. The list's
// own "No outputs yet -- a to add one" is cut to "a to add on" by the
// list pane's minimum width, so the key is read from the footer.
func TestOutputsScreenEmptyState(t *testing.T) {
	h := newHarness(t)
	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
profiles = ["dev"]
default_profile = "dev"
salt = "abc123"

[vars.APP_URL]
value = "http://+:8080"
`))
	if err != nil {
		t.Fatal(err)
	}
	h.register("vpb", m)

	view := screenAt(t, h, "2")
	if !strings.Contains(view, "No outputs yet") {
		t.Errorf("empty outputs screen does not read as a state:\n%s", view)
	}
	if !strings.Contains(view, "a add") {
		t.Errorf("empty outputs screen does not name how to add one:\n%s", view)
	}
}

// registerAnotherProject registers id at its own root: Registry.Add refuses
// two entries sharing h.root.
func registerAnotherProject(t *testing.T, id string, m *manifest.Manifest) {
	t.Helper()
	reg, err := config.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(config.Entry{ID: id, Root: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(config.ManifestPath(id), m); err != nil {
		t.Fatal(err)
	}
}

// toSwitchProject opens the . menu and moves to its last item, Switch
// project; the menu cursor stops at the end.
var toSwitchProject = []string{".", "j", "j", "j", "j", "j", "j"}

// Choosing another project reloads the interface against it, in the same
// invocation, rather than requiring a quit and a re-run from its directory.
func TestUiNewSwitchesProjectsAndReloadsAgainstTheChosenOne(t *testing.T) {
	h := newHarness(t)
	h.register("alpha", fixtureManifest(t))
	registerAnotherProject(t, "beta", fixtureManifest(t))

	calls := 0
	var second string
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		calls++
		if calls == 1 {
			for _, k := range toSwitchProject {
				m = drive(m, r(k))
			}
			return drive(m, enter, enter), nil // Switch project, beta
		}
		second = drive(m).View()
		return m, nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}
	if calls != 2 {
		t.Fatalf("RunManage was called %d times, want 2 (once per project)", calls)
	}
	if head := headerLine(second); !strings.Contains(head, "beta") || strings.Contains(head, "alpha") {
		t.Errorf("the second session is not beta's: %q", head)
	}
}

// Switching never commits the project being left: its staged change is
// discarded by the confirmation, not written to its manifest.
func TestUiNewSwitchesProjectsWithStagedChangesConfirmsAndDoesNotCommit(t *testing.T) {
	h := newHarness(t)
	h.register("alpha", fixtureManifest(t)) // ALPHA_TOKEN, APP_URL
	registerAnotherProject(t, "beta", fixtureManifest(t))
	before := h.manifest(t, "alpha").Vars["APP_URL"]

	calls := 0
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		calls++
		if calls > 1 {
			return m, nil
		}
		// APP_URL, detail, shared row, type, stage.
		m = drive(m, r("j"), enter, enter, r("http://+:9999"), enter)
		if n := len(m.Staged()); n != 1 {
			t.Fatalf("setup: %d changes staged, want the APP_URL edit", n)
		}
		for _, k := range toSwitchProject {
			m = drive(m, r(k))
		}
		m = drive(m, enter, enter) // Switch project, beta
		if v := m.View(); !strings.Contains(v, "discards 1 unsaved change") {
			t.Fatalf("switching with a staged change did not ask:\n%s", v)
		}
		return drive(m, enter), nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatalf("ui: %v", err)
	}
	if calls != 2 {
		t.Fatalf("RunManage was called %d times, want 2", calls)
	}

	after := h.manifest(t, "alpha").Vars["APP_URL"]
	if after.Value == nil || *after.Value != *before.Value {
		got := "<none>"
		if after.Value != nil {
			got = *after.Value
		}
		t.Errorf("alpha's APP_URL = %q, want %q: the unsaved edit was committed instead of discarded", got, *before.Value)
	}
	if after.Profiles["dev"] != before.Profiles["dev"] {
		t.Error("alpha's manifest changed even though the edit was never saved")
	}
}
