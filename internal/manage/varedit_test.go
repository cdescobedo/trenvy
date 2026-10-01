package manage_test

import (
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/session"
)

func staged(m interface{ Staged() []session.Change }) []string {
	var out []string
	for _, c := range m.Staged() {
		out = append(out, c.Kind+":"+c.Name+":"+c.Profile+":"+c.Value)
	}
	return out
}

// Spec §2: the row decides the scope, and the overlay says it.
func TestEnterOnSharedEditsTheSharedValueAndNamesWhoInherits(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", "enter", "enter") // APP_MODE, detail, shared row
	if v := m.View(); !strings.Contains(v, "used by dev, staging") {
		t.Errorf("overlay does not name the inheritors:\n%s", v)
	}
	m = typeText(m, "dev2")
	m = press(m, "enter")
	if got := staged(m); len(got) != 1 || got[0] != "set-shared:APP_MODE::dev2" {
		t.Errorf("staged = %v", got)
	}
}

func TestEnterOnAProfileRowStagesAnOverride(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", "enter", "j", "j", "enter") // staging row
	if v := m.View(); !strings.Contains(v, "Override for staging only") {
		t.Errorf("overlay title:\n%s", v)
	}
	m = typeText(m, "stg")
	m = press(m, "enter")
	if got := staged(m); len(got) != 1 || got[0] != "set-value:APP_MODE:staging:stg" {
		t.Errorf("staged = %v", got)
	}
	if !strings.Contains(m.View(), "1 unsaved") {
		t.Error("header does not count the staged change")
	}
}

func TestSecretEntryIsMaskedAndRefusesEmpty(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "enter", "j", "enter") // API_TOKEN staging
	m = typeText(m, "s3cret")
	if strings.Contains(m.View(), "s3cret") {
		t.Fatal("secret drawn while typing")
	}
	m = press(m, "enter")
	if got := staged(m); len(got) != 1 || got[0] != "set-secret:API_TOKEN:staging:s3cret" {
		t.Errorf("staged = %v", got)
	}
	m = press(newModel(&fakeDeps{}), "enter", "j", "enter", "enter")
	if len(m.Staged()) != 0 || !strings.Contains(m.View(), "can't be empty") {
		t.Errorf("empty secret: staged %v, view:\n%s", staged(m), m.View())
	}
}

func TestDOnAnOverrideRevertsToShared(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", "enter", "j", "j", "j", "d") // APP_MODE prod
	if got := staged(m); len(got) != 1 || got[0] != "unset-value:APP_MODE:prod:" {
		t.Errorf("staged = %v", got)
	}
	if !strings.Contains(lineWith(t, m.View(), "prod"), "from shared") {
		t.Errorf("prod row did not revert: %q", lineWith(t, m.View(), "prod"))
	}
}

func TestDOnTheOnlyValueRefusesWithAReason(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", "j", "enter", "d") // DATABASE_URL shared, no overrides
	if len(m.Staged()) != 0 || !strings.Contains(m.View(), "only value") {
		t.Errorf("staged %v, view:\n%s", staged(m), m.View())
	}
}

func TestDInTheListTogglesDeletionAndUUndoes(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", "d")
	if got := staged(m); len(got) != 1 || got[0] != "delete-var:APP_MODE::" {
		t.Fatalf("staged = %v", got)
	}
	if !strings.Contains(lineWith(t, m.View(), "APP_MODE"), "x") {
		t.Error("row lacks the delete mark")
	}
	if m = press(m, "d"); len(m.Staged()) != 0 {
		t.Errorf("second d did not restore: %v", staged(m))
	}
	if m = press(m, "d", "u"); len(m.Staged()) != 0 {
		t.Errorf("u did not undo: %v", staged(m))
	}
}

func TestUOnADetailRowUndoesOnlyThatCell(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", "enter", "enter")
	m = typeText(m, "a")
	m = press(m, "enter", "j", "enter")
	m = typeText(m, "b")
	m = press(m, "enter", "u") // cursor still on dev
	if got := staged(m); len(got) != 1 || got[0] != "set-shared:APP_MODE::a" {
		t.Errorf("staged = %v, want only the shared edit left", got)
	}
}

func TestAddPlainVariable(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "a")
	m = typeText(m, "NEW_VAR")
	m = press(m, "enter", "p")
	m = typeText(m, "hello")
	m = press(m, "enter")
	want := []string{"add-var:NEW_VAR::", "set-shared:NEW_VAR::hello"}
	if got := staged(m); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("staged = %v, want %v", got, want)
	}
	if !strings.Contains(m.View(), "NEW_VAR") {
		t.Error("new variable not selected in the list")
	}
}

func TestAddSecretVariableStoresTheActiveProfile(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "a")
	m = typeText(m, "NEW_KEY")
	m = press(m, "enter", "s")
	m = typeText(m, "k")
	m = press(m, "enter")
	want := []string{"add-var:NEW_KEY::", "promote-secret:NEW_KEY::", "set-secret:NEW_KEY:dev:k"}
	if got := staged(m); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("staged = %v, want %v", got, want)
	}
}

func TestAddRefusesAnExistingName(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "a")
	m = typeText(m, "APP_MODE")
	m = press(m, "enter")
	if !strings.Contains(m.View(), "already exists") {
		t.Errorf("view:\n%s", m.View())
	}
}

func TestMakeSecretConfirmsWhenASharedValueExists(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", ".", "enter") // APP_MODE, menu, Make secret
	if v := m.View(); !strings.Contains(v, "3 separate keyring items") {
		t.Fatalf("no drift confirmation:\n%s", v)
	}
	m = press(m, "esc")
	if len(m.Staged()) != 0 {
		t.Errorf("esc staged something: %v", staged(m))
	}
	m = press(m, ".", "enter", "enter")
	want := "set-secret:APP_MODE:dev:development,set-secret:APP_MODE:staging:development,set-secret:APP_MODE:prod:production,promote-secret:APP_MODE::"
	if got := strings.Join(staged(m), ","); got != want {
		t.Errorf("staged = %s", got)
	}
}

func TestMakeSecretIsDisabledOnASecret(t *testing.T) {
	m := press(newModel(&fakeDeps{}), ".", "enter") // API_TOKEN
	if len(m.Staged()) != 0 || !strings.Contains(m.View(), "already a secret") {
		t.Errorf("staged %v, view:\n%s", staged(m), m.View())
	}
}

func TestMarkRequiredStagesOptionalFalse(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", "j", "j", ".") // LOG_LEVEL
	if !strings.Contains(m.View(), "Mark required") {
		t.Fatalf("menu:\n%s", m.View())
	}
	m = press(m, "j", "enter")
	if got := staged(m); len(got) != 1 || got[0] != "set-optional:LOG_LEVEL::false" {
		t.Errorf("staged = %v", got)
	}
}

func TestRenameIsShownDisabledWithItsReason(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", ".", "j", "j", "enter")
	if !strings.Contains(m.View(), "not supported") {
		t.Errorf("view:\n%s", m.View())
	}
}

func TestProjectInfoShowsThePaths(t *testing.T) {
	m := press(newModel(&fakeDeps{}), ".")
	for i := 0; i < 3; i++ {
		m = press(m, "j")
	}
	m = press(m, "enter")
	if v := m.View(); !strings.Contains(v, "/cfg/projects/app.toml") || !strings.Contains(v, "/code/app") {
		t.Errorf("project info:\n%s", v)
	}
}

func TestSwitchProjectAsksWhenUnsaved(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", "d", ".")
	for i := 0; i < 4; i++ {
		m = press(m, "j")
	}
	m = press(m, "enter", "enter") // Switch project, pick "other"
	if !strings.Contains(m.View(), "discards") {
		t.Fatalf("no confirmation:\n%s", m.View())
	}
	m = press(m, "enter")
	if id, ok := m.SwitchTo(); !ok || id != "other" || !m.Quitting() {
		t.Errorf("SwitchTo = %q, %v; quitting %v", id, ok, m.Quitting())
	}
}

func TestQWithUnsavedAsksAndQAgainDiscards(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", "d", "q")
	if m.Quitting() || !strings.Contains(m.View(), "1 unsaved change") {
		t.Fatalf("q did not ask:\n%s", m.View())
	}
	if m = press(m, "q"); !m.Quitting() {
		t.Error("second q did not quit")
	}
}
