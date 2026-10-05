package manage_test

import (
	"strings"
	"testing"
)

func out(m manageModel, keys ...string) manageModel {
	return press(m, append([]string{"2"}, keys...)...)
}

func TestAddOutputWizardStagesOneAddition(t *testing.T) {
	m := out(newModel(&fakeDeps{ignored: map[string]bool{"api.env": true}}), "a")
	m = typeText(m, "api")
	m = press(m, "enter", "d") // dotenv; path field opens with .env selected
	m = typeText(m, "api.env")
	m = press(m, "enter")
	if got := staged(m); len(got) != 1 || got[0] != "add-output:api::" {
		t.Fatalf("staged = %v", got)
	}
	if c := m.Staged()[0]; c.Format != "dotenv" || c.Path != "api.env" {
		t.Errorf("change = %+v", c)
	}
	if !strings.Contains(lineWith(t, m.View(), "api"), "new") {
		t.Errorf("the list does not show the new output:\n%s", m.View())
	}
}

func TestAddOutputWarnsWhenGitDoesNotIgnoreThePath(t *testing.T) {
	m := out(newModel(&fakeDeps{ignored: map[string]bool{}}), "a")
	m = typeText(m, "api")
	m = press(m, "enter", "d", "enter") // keep .env; git does not ignore it
	if len(m.Staged()) != 0 || !strings.Contains(m.View(), "not ignored by git") {
		t.Fatalf("no warning: staged %v\n%s", staged(m), m.View())
	}
	if m = press(m, "enter"); len(m.Staged()) != 1 {
		t.Errorf("second enter did not add anyway: %v", staged(m))
	}
}

func TestAddOutputRefusesBadNamesAndPaths(t *testing.T) {
	for _, c := range []struct{ name, want string }{
		{"local", "already exists"},
		{"-bad", "letters, digits"},
	} {
		m := out(newModel(&fakeDeps{}), "a")
		m = typeText(m, c.name)
		m = press(m, "enter")
		if !strings.Contains(m.View(), c.want) {
			t.Errorf("%q: view lacks %q:\n%s", c.name, c.want, m.View())
		}
	}
	m := out(newModel(&fakeDeps{}), "a")
	m = typeText(m, "api")
	m = press(m, "enter", "d", "ctrl+u")
	m = typeText(m, "../outside.env")
	m = press(m, "enter")
	if len(m.Staged()) != 0 || !strings.Contains(m.View(), "outside.env") {
		t.Errorf("a path escaping the worktree was accepted: %v", staged(m))
	}
}

func TestAddOutputNeedsAName(t *testing.T) {
	m := out(newModel(&fakeDeps{}), "a", "enter")
	if !strings.Contains(m.View(), "an output needs a name") {
		t.Errorf("view:\n%s", m.View())
	}
}

// Review Focus 2.
func TestRenameOntoAnExistingNameIsRefused(t *testing.T) {
	m := out(newModel(&fakeDeps{}), ".", "j", "enter") // local, menu, Rename
	m = press(m, "ctrl+u")
	m = typeText(m, "web")
	m = press(m, "enter")
	if len(m.Staged()) != 0 || !strings.Contains(m.View(), "already exists") {
		t.Errorf("staged %v\n%s", staged(m), m.View())
	}
}

func TestRenameShowsInTheListAndUndoes(t *testing.T) {
	m := out(newModel(&fakeDeps{}), ".", "j", "enter", "ctrl+u")
	m = typeText(m, "root")
	m = press(m, "enter")
	if !strings.Contains(m.View(), "local -> root") {
		t.Fatalf("view:\n%s", m.View())
	}
	if m = press(m, "u"); len(m.Staged()) != 0 {
		t.Errorf("u left %v", staged(m))
	}
}

func TestMakeDefault(t *testing.T) {
	m := out(newModel(&fakeDeps{}), "j", ".", "enter") // web, menu, Make default
	if got := staged(m); len(got) != 1 || got[0] != "default-output:web::" {
		t.Fatalf("staged = %v", got)
	}
	if !strings.Contains(listRow(t, m.View(), "web"), "default") || strings.Contains(listRow(t, m.View(), "local"), "default") {
		t.Errorf("default tag did not move:\n%s", m.View())
	}
	m = out(newModel(&fakeDeps{}), ".", "enter") // local is already the default
	if len(m.Staged()) != 0 || !strings.Contains(m.View(), "already the default") {
		t.Errorf("staged %v", staged(m))
	}
}

func TestEditFormatAndPathFromTheDetail(t *testing.T) {
	m := out(newModel(&fakeDeps{}), "enter", "enter", "j") // format row -> choice j(son)
	if got := staged(m); len(got) != 1 || got[0] != "set-output-format:local::json" {
		t.Fatalf("staged = %v", got)
	}
	m = press(m, "j", "enter", "ctrl+u")
	m = typeText(m, "config/.env")
	m = press(m, "enter")
	if got := staged(m); len(got) != 2 || got[1] != "set-output-path:local::config/.env" {
		t.Errorf("staged = %v", got)
	}
	// Setting a value back to what is on disk drops the change.
	m = press(m, "k", "enter", "d")
	if got := staged(m); len(got) != 1 {
		t.Errorf("format back to dotenv did not drop its change: %v", got)
	}
}

func TestMembershipToggleStagesGlobsAndUndoes(t *testing.T) {
	m := out(newModel(&fakeDeps{}), "enter", "j", "j", "space") // local, first variable row (API_TOKEN)
	got := m.Staged()
	if len(got) != 1 || got[0].Kind != "set-globs" || len(got[0].Exclude) != 1 || got[0].Exclude[0] != "API_TOKEN" {
		t.Fatalf("staged = %+v", got)
	}
	if !strings.Contains(lineWith(t, m.View(), "API_TOKEN"), "[ ]") {
		t.Errorf("box not unchecked:\n%s", m.View())
	}
	if m = press(m, "space"); len(m.Staged()) != 0 {
		t.Errorf("toggling back did not drop the change: %+v", m.Staged())
	}
}

// Review Focus 4.
func TestMembershipGridIncludesAddedVariablesAndUncheckingAllSelectsNone(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "a")
	m = typeText(m, "NEW_VAR")
	m = press(m, "enter", "p")
	m = typeText(m, "x")
	m = press(m, "enter", "2", "j", "enter") // web (only these: PUBLIC_*)
	if !strings.Contains(m.View(), "NEW_VAR") {
		t.Fatalf("grid lacks the added variable:\n%s", m.View())
	}
	// Uncheck the one checked box (PUBLIC_URL) under only-these.
	for i := 0; i < 8; i++ {
		if strings.Contains(lineWith(t, m.View(), "PUBLIC_URL"), ">") {
			break
		}
		m = press(m, "j")
	}
	m = press(m, "space")
	var globs []string
	for _, c := range m.Staged() {
		if c.Kind == "set-globs" {
			globs = append(append(globs, c.Include...), c.Exclude...)
		}
	}
	if len(globs) == 0 {
		t.Fatal("no set-globs staged")
	}
	for _, c := range m.Staged() {
		if c.Kind == "set-globs" && len(c.Include) == 0 {
			t.Errorf("an empty include was staged (it would mean everything): %+v", c)
		}
	}
}

func TestSwitchingTheFutureModeKeepsTheBoxes(t *testing.T) {
	m := out(newModel(&fakeDeps{}), ".") // local menu
	for i := 0; i < 2; i++ {
		m = press(m, "j")
	}
	m = press(m, "enter") // "New variables: not included"
	if !strings.Contains(m.View(), "not included") {
		t.Fatalf("mode did not switch:\n%s", m.View())
	}
	if strings.Contains(m.View(), "[ ]") {
		t.Errorf("switching the mode unchecked a box:\n%s", m.View())
	}
}

func TestRemoveOutputTogglesAndSaysWhatItDropped(t *testing.T) {
	m := out(newModel(&fakeDeps{}), ".", "j", "enter", "ctrl+u")
	m = typeText(m, "root")
	m = press(m, "enter", "d")
	if got := staged(m); len(got) != 1 || got[0] != "remove-output:local::" {
		t.Fatalf("staged = %v", got)
	}
	if !strings.Contains(m.View(), "dropped 1 staged change") {
		t.Errorf("message:\n%s", m.View())
	}
	if m = press(m, "d"); len(m.Staged()) != 0 {
		t.Errorf("second d did not restore: %v", staged(m))
	}
}

// Review Focus 1.
func TestOutputAndVariableWithTheSameNameStageIndependently(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "a")
	m = typeText(m, "local")
	m = press(m, "enter", "p")
	m = typeText(m, "v")
	m = press(m, "enter", "2", "d") // remove output local
	if !hasStaged(m, "add-var:local::") || !hasStaged(m, "remove-output:local::") {
		t.Fatalf("staged = %v", staged(m))
	}
	m = press(m, "u") // undo on the output
	if !hasStaged(m, "add-var:local::") || hasStaged(m, "remove-output:local::") {
		t.Errorf("undo on the output touched the variable: %v", staged(m))
	}
}

// listRow is the list pane's part of the first line whose list part holds
// name. The detail pane's title shares a screen line with a list row, and
// its meta ("json · default") must not be read as that row's tag.
func listRow(t *testing.T, v, name string) string {
	t.Helper()
	for _, l := range strings.Split(v, "\n") {
		if i := strings.Index(l, "││"); i >= 0 {
			l = l[:i]
		}
		if strings.Contains(l, name) {
			return l
		}
	}
	t.Fatalf("no list row holds %q:\n%s", name, v)
	return ""
}

func hasStaged(m manageModel, want string) bool {
	for _, s := range staged(m) {
		if s == want {
			return true
		}
	}
	return false
}
