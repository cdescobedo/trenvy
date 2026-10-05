package manage_test

import (
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
)

func stagedKinds(m manage.Model) []string {
	var out []string
	for _, c := range m.Staged() {
		out = append(out, c.Kind+":"+c.Name)
	}
	return out
}

func TestAddProfileStagesANameAndShowsItAsNew(t *testing.T) {
	m := typeText(press(newModel(&fakeDeps{}), "3", "a"), "qa")
	m = press(m, "enter")
	if got := stagedKinds(m); len(got) != 1 || got[0] != "add-profile:qa" {
		t.Fatalf("staged = %v", got)
	}
	v := m.View()
	if row := lineWith(t, v, "qa"); !strings.Contains(row, "~") {
		t.Errorf("qa row = %q, want the ~ mark", row)
	}
	if !strings.Contains(v, "new -- save, then give it values in Variables") {
		t.Errorf("an added profile's detail should say values come after save:\n%s", v)
	}
}

func TestAddProfileRefusesBadAndDuplicateNames(t *testing.T) {
	for name, want := range map[string]string{"a b": "letters, digits", "prod": "already exists", "": "needs a name"} {
		m := press(typeText(press(newModel(&fakeDeps{}), "3", "a"), name), "enter")
		if len(m.Staged()) != 0 || !strings.Contains(m.View(), want) {
			t.Errorf("name %q: staged %v, want refusal %q:\n%s", name, stagedKinds(m), want, m.View())
		}
	}
}

func TestRemoveProfileRefusals(t *testing.T) {
	// dev is the default and the session's profile: the default refusal
	// names the fix.
	if v := press(newModel(&fakeDeps{}), "3", "d").View(); !strings.Contains(v, "dev is the default") {
		t.Errorf("removing the default:\n%s", v)
	}
	// The session is on prod, which is not the default.
	snap := fixture()
	snap.Profile, snap.Source = "prod", "--profile"
	m := press(sized(manage.New(snap, (&fakeDeps{}).deps(), noTheme()), 100, 30), "3", "j", "j", "d")
	if v := m.View(); len(m.Staged()) != 0 || !strings.Contains(v, "this session is on prod") {
		t.Errorf("removing the session's profile:\n%s", v)
	}
	// The last profile.
	one := fixtureManifest()
	one.Project.Profiles = []string{"dev"}
	one.Vars["APP_MODE"] = manifestVar("development")
	m = press(sized(manage.New(snapshotOf(one, nil), (&fakeDeps{}).deps(), noTheme()), 100, 30), "3", "d")
	if !strings.Contains(m.View(), "last profile") {
		t.Errorf("removing the last profile:\n%s", m.View())
	}
}

func TestRemovingAVariablesOnlyValueIsRefused(t *testing.T) {
	mf := fixtureManifest()
	mf.Vars["ONLY_PROD"] = manifest.Var{Profiles: map[string]string{"prod": "x"}}
	m := press(sized(manage.New(snapshotOf(mf, map[string]map[string]bool{"API_TOKEN": {"dev": true, "prod": true}}), (&fakeDeps{}).deps(), noTheme()), 100, 30), "3", "j", "j", "d")
	if v := m.View(); len(m.Staged()) != 0 || !strings.Contains(v, "would leave ONLY_PROD with no value") {
		t.Errorf("removing the only value's profile should be refused:\n%s", v)
	}
}

// Two removals together: REQ's only values sit on staging and prod, so
// once staging is marked, marking prod would leave REQ with none.
func TestRemovingTheLastProfileHoldingAValueIsRefusedAcrossRemovals(t *testing.T) {
	mf := fixtureManifest()
	mf.Vars["REQ"] = manifest.Var{Profiles: map[string]string{"staging": "a", "prod": "b"}}
	m := press(sized(manage.New(snapshotOf(mf, map[string]map[string]bool{"API_TOKEN": {"dev": true, "prod": true}}), (&fakeDeps{}).deps(), noTheme()), 100, 30), "3", "j", "d", "j", "d")
	if got := stagedKinds(m); len(got) != 1 || got[0] != "remove-profile:staging" {
		t.Errorf("staged = %v, want only staging's removal", got)
	}
	if v := m.View(); !strings.Contains(v, "would leave REQ with no value") {
		t.Errorf("the refusal should name REQ:\n%s", v)
	}
}

func TestRemoveProfileStagesAndDTogglesItBack(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "3", "j", "d") // staging
	if got := stagedKinds(m); len(got) != 1 || got[0] != "remove-profile:staging" {
		t.Fatalf("staged = %v", got)
	}
	if row := lineWith(t, m.View(), "staging"); !strings.Contains(row, "x") {
		t.Errorf("staging row = %q, want x", row)
	}
	if m = press(m, "d"); len(m.Staged()) != 0 {
		t.Errorf("d again left %v staged", stagedKinds(m))
	}
}

// Review Focus 3.
func TestRemoveMessageCountsDroppedValues(t *testing.T) {
	// APP_MODE, detail, rows shared/dev/staging/prod: stage a staging value.
	m := press(newModel(&fakeDeps{}), "j", "enter", "j", "j", "enter")
	m = press(typeText(m, "stg"), "enter")
	if len(m.Staged()) != 1 {
		t.Fatalf("setup staged %v", stagedKinds(m))
	}
	m = press(m, "3", "j", "d")
	if v := m.View(); !strings.Contains(v, "1 staged value") || !strings.Contains(v, "will not bring") {
		t.Errorf("the message should count the dropped value and say undo won't restore it:\n%s", v)
	}
	if got := stagedKinds(m); len(got) != 1 || got[0] != "remove-profile:staging" {
		t.Errorf("staged = %v, want only the removal", got)
	}
}

func TestUndoOnAProfileKeepsValuesOnItsCells(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", "enter", "j", "j", "j", "enter") // APP_MODE prod
	m = press(typeText(m, "p2"), "enter")
	m = press(m, "3", "j", "j", ".", "j", "enter") // prod: Make default
	m = press(m, "u")
	if got := stagedKinds(m); len(got) != 1 || got[0] != session.ChangeSetValue+":APP_MODE" {
		t.Errorf("u on prod should undo only its default: staged = %v", got)
	}
}

func TestProfileMenuUseAndDefault(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "3", "j", "j", ".", "enter") // prod: Use on this machine
	if u, ok := useOf(m); !ok || u != "prod" {
		t.Errorf("use-profile = %q %v", u, ok)
	}
	if row := lineWith(t, m.View(), "prod"); !strings.Contains(row, "active") {
		t.Errorf("prod row = %q, want active (as after save)", row)
	}
	m = press(m, ".", "j", "enter") // Make default
	if row := lineWith(t, m.View(), "prod"); !strings.Contains(row, "default") {
		t.Errorf("prod row = %q, want default", row)
	}
	// Rename is disabled with its reason.
	m = press(newModel(&fakeDeps{}), "3", ".", "j", "j", "enter")
	if !strings.Contains(m.View(), "keyring") || len(m.Staged()) != 0 {
		t.Errorf("Rename should be refused with the keyring reason:\n%s", m.View())
	}
}

func useOf(m manage.Model) (string, bool) {
	for _, c := range m.Staged() {
		if c.Kind == session.ChangeUseProfile {
			return c.Name, true
		}
	}
	return "", false
}

// Review Focus 2.
func TestUseOnThisMachineSaysWhenOutranked(t *testing.T) {
	snap := fixture()
	snap.Source = "--profile"
	m := press(sized(manage.New(snap, (&fakeDeps{}).deps(), noTheme()), 100, 30), "3", "j", "j", ".", "enter")
	if !strings.Contains(m.View(), "--profile still outranks it") {
		t.Errorf("the message should say the flag outranks the selection here:\n%s", m.View())
	}
}

func TestUseIsDisabledOnAnUnsavedProfile(t *testing.T) {
	m := press(typeText(press(newModel(&fakeDeps{}), "3", "a"), "qa"), "enter")
	m = press(m, ".", "enter")
	if _, ok := useOf(m); ok || !strings.Contains(m.View(), "save first") {
		t.Errorf("Use on an unsaved profile should be refused:\n%s", m.View())
	}
}

// Review Focus 5.
func TestEnterOnAMissingNameJumpsToTheCell(t *testing.T) {
	for _, w := range []int{40, 100} {
		m := press(sized(manage.New(fixture(), (&fakeDeps{}).deps(), noTheme()), w, 24), "3", "j", "enter", "enter")
		v := m.View()
		if tabs := strings.Split(v, "\n")[1]; !strings.Contains(tabs, "[Variables]") {
			t.Fatalf("w=%d: enter did not jump to Variables:\n%s", w, v)
		}
		if !strings.Contains(v, "API_TOKEN") || !strings.Contains(lineWith(t, v, "staging"), ">") {
			t.Errorf("w=%d: the cursor should be on API_TOKEN's staging row:\n%s", w, v)
		}
	}
}

func TestProfilesFooterListsOnlyKeysThatAct(t *testing.T) {
	cases := []struct {
		keys []string
		want []string
	}{
		{[]string{"3"}, []string{"enter", "d", "a", ".", "?"}},
		{[]string{"3", "j", "d"}, []string{"enter", "d", "u", "a", ".", "?"}},
		{[]string{"3", "j", "enter"}, []string{"enter", "←", ".", "?"}},
		{[]string{"3", "j", "j", "enter"}, []string{"←", ".", "?"}}, // prod: nothing missing
	}
	for _, c := range cases {
		got := press(newModel(&fakeDeps{}), c.keys...).FooterKeys()
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%v: footer = %v, want %v", c.keys, got, c.want)
		}
	}
}
