package ui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/ui"
)

// Moved from internal/cli/derive_internal_test.go: SelectsSummary now lives
// here so the Outputs tab and `trenvy output list` share one implementation
// of this prose rather than each computing it -- the same mistake (two
// places computing one fact about outputs) that produced a phase-1 Critical.
func TestSelectsSummary(t *testing.T) {
	cases := []struct {
		o    manifest.Output
		want string
	}{
		{manifest.Output{}, "all"},
		{manifest.Output{Include: []string{"*"}}, "all"},
		{manifest.Output{Include: []string{"*"}, Exclude: []string{"A", "B"}}, "all except A, B"},
		{manifest.Output{Include: []string{"A", "B"}}, "A, B"},
		{manifest.Output{Include: []string{"PUBLIC_*"}, Exclude: []string{"PUBLIC_SECRET"}}, "PUBLIC_* except PUBLIC_SECRET"},
	}
	for _, c := range cases {
		if got := ui.SelectsSummary(c.o); got != c.want {
			t.Errorf("SelectsSummary(%+v) = %q, want %q", c.o, got, c.want)
		}
	}
}

// outputsParams is testParams' counterpart for the Outputs tab: the header
// facts most of these tests never look at, plus whichever rows and unverified
// flag the test actually varies.
func outputsParams(rows []ui.OutputRow, unverified bool) ui.Params {
	return ui.Params{
		Project: "demo", Profile: "dev", Source: "default", Backend: "keychain",
		Outputs:                   rows,
		OutputFreshnessUnverified: unverified,
	}
}

// The tab must render the same facts `trenvy output list` does -- name,
// format, path, the globs as prose -- plus the STATE column, which is Task
// 4's own addition. Two rows, one marked default, so the default marker and
// an un-marked row are both exercised in the same render.
func TestOutputsTabRendersEachOutputsFacts(t *testing.T) {
	rows := []ui.OutputRow{
		{Name: "docker", Format: "dotenv", Path: "src/.env", Selects: "all except ALPHA_TOKEN", State: ui.StateFresh, IsDefault: true},
		{Name: "public", Format: "json", Path: "public/env.json", Selects: "APP_URL", State: ui.StateAbsent},
	}
	m := ui.New(outputsParams(rows, false))
	m, _ = m.Update(ui.KeyRune('4')) // Outputs tab

	v := m.View()
	for _, want := range []string{
		"NAME", "FORMAT", "PATH", "SELECTS", "STATE",
		"docker *", "dotenv", "src/.env", "all except ALPHA_TOKEN", ui.StateFresh,
		"public", "json", "public/env.json", "APP_URL", ui.StateAbsent,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("outputs tab missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "public *") {
		t.Errorf("non-default output marked as default:\n%s", v)
	}
}

// A project with no outputs is normal -- exec and shell need none -- so this
// must read as a state, not a blank, broken panel. Task 4 named only the CLI
// here because `a` was not wired yet; now that it is, the line must name the
// key first -- the real fix for someone staring at this screen -- and keep
// naming the CLI equivalent for a reader working from the command line.
func TestOutputsTabEmptyStateNamesTheAddKeyAndCommand(t *testing.T) {
	m := ui.New(outputsParams(nil, false))
	m, _ = m.Update(ui.KeyRune('4'))

	v := m.View()
	if !strings.Contains(v, "no outputs") {
		t.Errorf("empty outputs tab does not say plainly there are none:\n%s", v)
	}
	if !strings.Contains(v, "press a") {
		t.Errorf("empty outputs tab does not name the key that adds one:\n%s", v)
	}
	if !strings.Contains(v, "trenvy output add") {
		t.Errorf("empty outputs tab does not name the command that adds one:\n%s", v)
	}
}

// Mirrors TestOverviewHealthyLineNamesStatusWhenAnOutputIsUnverified: the
// caveat is the same fact, so it must be the same sentence naming the same
// command, said here too since this tab is where a reader is actually
// looking at output state.
func TestOutputsTabNamesStatusWhenAnOutputIsUnverified(t *testing.T) {
	rows := []ui.OutputRow{
		{Name: "docker", Format: "dotenv", Path: "src/.env", Selects: "all", State: ui.StateFresh, IsDefault: true},
	}
	m := ui.New(outputsParams(rows, true))
	m, _ = m.Update(ui.KeyRune('4'))

	v := m.View()
	if !strings.Contains(v, "trenvy status") {
		t.Errorf("outputs tab does not caveat unverified freshness:\n%s", v)
	}
}

// The other half: nothing unverified means no caveat, matching the
// Overview's own plain healthy line in the same situation.
func TestOutputsTabStaysPlainWhenEveryOutputIsVerified(t *testing.T) {
	rows := []ui.OutputRow{
		{Name: "docker", Format: "dotenv", Path: "src/.env", Selects: "all", State: ui.StateFresh, IsDefault: true},
	}
	m := ui.New(outputsParams(rows, false))
	m, _ = m.Update(ui.KeyRune('4'))

	v := m.View()
	if strings.Contains(v, "trenvy status") {
		t.Errorf("outputs tab names trenvy status despite nothing being unverified:\n%s", v)
	}
}

// The unverified case must not read as a problem: it renders through the
// same plain State word (StateFresh) every other fresh output uses, never a
// distinct alarming label -- Phase 1's fix wave settled that an output whose
// freshness merely could not be checked here is not itself a problem.
func TestOutputsTabUnverifiedRowStaysPlainFresh(t *testing.T) {
	rows := []ui.OutputRow{
		{Name: "docker", Format: "dotenv", Path: "src/.env", Selects: "all", State: ui.StateFresh, IsDefault: true},
	}
	m := ui.New(outputsParams(rows, true))
	m, _ = m.Update(ui.KeyRune('4'))

	v := m.View()
	var row string
	for _, line := range strings.Split(v, "\n") {
		if strings.Contains(line, "docker") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("docker row not found:\n%s", v)
	}
	if !strings.Contains(row, ui.StateFresh) {
		t.Errorf("unverified output's own row does not read plainly as %q: %q", ui.StateFresh, row)
	}
}

// outputsModelOnTab builds a model over rows and switches straight to the
// Outputs tab, mirroring allCellsModel's own switch-then-return for
// Variables in model_test.go: every key test below assumes the tab is
// already active before the first keypress it sends.
func outputsModelOnTab(t *testing.T, rows []ui.OutputRow) ui.Model {
	t.Helper()
	m := ui.New(outputsParams(rows, false))
	m, _ = m.Update(ui.KeyRune('4'))
	return m
}

// `a` is a three-step wizard, staging nothing until all three facts are
// known: there is no output worth having with only a name, or only a name
// and a format.
func TestOutputAddPromptsNameThenFormatThenPath(t *testing.T) {
	m := outputsModelOnTab(t, nil)

	m, _ = m.Update(ui.KeyRune('a'))
	if m.Mode() != ui.ModeOutputAddName {
		t.Fatalf("mode after a = %v, want ModeOutputAddName", m.Mode())
	}
	for _, r := range "public" {
		m, _ = m.Update(ui.KeyRune(r))
	}
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	if m.Mode() != ui.ModeOutputAddFormat {
		t.Fatalf("mode after name = %v, want ModeOutputAddFormat", m.Mode())
	}
	if !strings.Contains(m.View(), "public") {
		t.Errorf("format prompt lost the name typed at the previous step:\n%s", m.View())
	}
	for _, r := range "dotenv" {
		m, _ = m.Update(ui.KeyRune(r))
	}
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	if m.Mode() != ui.ModeOutputAddPath {
		t.Fatalf("mode after format = %v, want ModeOutputAddPath", m.Mode())
	}
	for _, r := range "public/.env" {
		m, _ = m.Update(ui.KeyRune(r))
	}
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))

	if m.Mode() != ui.ModeBrowse {
		t.Fatalf("mode after path = %v, want ModeBrowse", m.Mode())
	}
	staged := m.Staged()
	want := ui.Change{Kind: ui.ChangeAddOutput, Name: "public", Format: "dotenv", Path: "public/.env"}
	if len(staged) != 1 || staged[0] != want {
		t.Fatalf("Staged() = %+v, want exactly %+v", staged, want)
	}
	if !strings.Contains(m.View(), "public") {
		t.Errorf("added output does not appear in the view:\n%s", m.View())
	}
}

// esc must abandon the whole flow without staging a half-finished output --
// mirrors TestEscCancelsInputModeWithoutStaging for the Variables tab.
func TestEscCancelsOutputAddWithoutStaging(t *testing.T) {
	m := outputsModelOnTab(t, nil)
	m, _ = m.Update(ui.KeyRune('a'))
	for _, r := range "public" {
		m, _ = m.Update(ui.KeyRune(r))
	}
	m, _ = m.Update(ui.KeyType(tea.KeyEsc))

	if m.Mode() != ui.ModeBrowse {
		t.Fatalf("mode after esc = %v, want ModeBrowse", m.Mode())
	}
	if len(m.Staged()) != 0 {
		t.Errorf("Staged() = %+v, want empty after esc", m.Staged())
	}
}

// An invalid name must refuse the way `trenvy output add` would, reusing
// manifest.ValidOutputName rather than a second rule that could disagree
// with it -- and the refusal must be visible while typing, not discovered
// only by pressing enter and watching nothing happen.
func TestOutputAddRefusesInvalidName(t *testing.T) {
	m := outputsModelOnTab(t, nil)
	m, _ = m.Update(ui.KeyRune('a'))
	for _, r := range "-bad" {
		m, _ = m.Update(ui.KeyRune(r))
	}

	v := m.View()
	if !strings.Contains(v, "letters, digits, - and _") {
		t.Errorf("no live warning for an invalid output name:\n%s", v)
	}

	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	if m.Mode() != ui.ModeOutputAddName {
		t.Errorf("mode after enter on an invalid name = %v, want to stay ModeOutputAddName", m.Mode())
	}
	if len(m.Staged()) != 0 {
		t.Errorf("Staged() = %+v, want empty after refusing an invalid name", m.Staged())
	}
}

// A name already defined must refuse the same way `trenvy output add`
// would ("already defined"), not silently stage a second output under a
// key that collides at commit time.
func TestOutputAddRefusesDuplicateName(t *testing.T) {
	rows := []ui.OutputRow{{Name: "api", Format: "dotenv", Path: ".env"}}
	m := outputsModelOnTab(t, rows)
	m, _ = m.Update(ui.KeyRune('a'))
	for _, r := range "api" {
		m, _ = m.Update(ui.KeyRune(r))
	}

	v := m.View()
	if !strings.Contains(v, `"api" is already defined`) {
		t.Errorf("no live warning for a duplicate output name:\n%s", v)
	}

	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	if m.Mode() != ui.ModeOutputAddName {
		t.Errorf("mode after enter on a duplicate name = %v, want to stay ModeOutputAddName", m.Mode())
	}
	if len(m.Staged()) != 0 {
		t.Errorf("Staged() = %+v, want empty after refusing a duplicate name", m.Staged())
	}
}

// `r` renames the output under the cursor, staging rename-output rather than
// touching anything on disk.
func TestOutputRenameStagesRenameOutput(t *testing.T) {
	rows := []ui.OutputRow{{Name: "api", Format: "dotenv", Path: "src/.env", IsDefault: true}}
	m := outputsModelOnTab(t, rows)

	m, _ = m.Update(ui.KeyRune('r'))
	if m.Mode() != ui.ModeOutputRename {
		t.Fatalf("mode after r = %v, want ModeOutputRename", m.Mode())
	}
	for _, r := range "gateway" {
		m, _ = m.Update(ui.KeyRune(r))
	}
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))

	staged := m.Staged()
	want := ui.Change{Kind: ui.ChangeRenameOutput, Name: "api", Value: "gateway"}
	if len(staged) != 1 || staged[0] != want {
		t.Fatalf("Staged() = %+v, want exactly %+v", staged, want)
	}
	line := lineFor(t, m.View(), "api")
	if !strings.Contains(line, "gateway") {
		t.Errorf("staged rename target not shown in the view:\n%s", line)
	}
}

// Renaming to a name another output already has must refuse and say why,
// the same way deleteBlockedReason does for the Variables tab -- derived
// live from state, not discovered only after enter does nothing.
func TestOutputRenameRefusesExistingName(t *testing.T) {
	rows := []ui.OutputRow{
		{Name: "api", Format: "dotenv", Path: "src/.env"},
		{Name: "public", Format: "json", Path: "public/env.json"},
	}
	m := outputsModelOnTab(t, rows) // cursor starts on row 0, "api"

	m, _ = m.Update(ui.KeyRune('r'))
	for _, r := range "public" {
		m, _ = m.Update(ui.KeyRune(r))
	}

	v := m.View()
	if !strings.Contains(v, `"public" is already defined`) {
		t.Errorf("no live warning for renaming to an existing name:\n%s", v)
	}

	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	if m.Mode() != ui.ModeOutputRename {
		t.Errorf("mode after enter on a taken name = %v, want to stay ModeOutputRename", m.Mode())
	}
	if len(m.Staged()) != 0 {
		t.Errorf("Staged() = %+v, want empty after refusing a taken name", m.Staged())
	}
}

// Renaming to an invalid name refuses the same way add does, reusing the
// same manifest.ValidOutputName rule.
func TestOutputRenameRefusesInvalidName(t *testing.T) {
	rows := []ui.OutputRow{{Name: "api", Format: "dotenv", Path: "src/.env"}}
	m := outputsModelOnTab(t, rows)
	m, _ = m.Update(ui.KeyRune('r'))
	for _, r := range "-bad" {
		m, _ = m.Update(ui.KeyRune(r))
	}

	v := m.View()
	if !strings.Contains(v, "letters, digits, - and _") {
		t.Errorf("no live warning for renaming to an invalid name:\n%s", v)
	}

	m, _ = m.Update(ui.KeyType(tea.KeyEnter))
	if len(m.Staged()) != 0 {
		t.Errorf("Staged() = %+v, want empty after refusing an invalid rename", m.Staged())
	}
}

// Renaming an output back to the name it already has must not be flagged as
// a collision with itself -- the "already defined" scan excludes the row's
// own current identity.
func TestOutputRenameToItsOwnNameIsNotACollision(t *testing.T) {
	rows := []ui.OutputRow{{Name: "api", Format: "dotenv", Path: "src/.env"}}
	m := outputsModelOnTab(t, rows)
	m, _ = m.Update(ui.KeyRune('r'))
	for _, r := range "api" {
		m, _ = m.Update(ui.KeyRune(r))
	}

	if v := m.View(); strings.Contains(v, "already defined") {
		t.Errorf("renaming a row to its own name wrongly flagged as taken:\n%s", v)
	}
}

// `D` -- capital, since `d` already means delete -- stages making the row
// under the cursor project.output, and any earlier `D` staged for a
// different row is replaced: only one output can be the default, so leaving
// both staged would be ambiguous about which one w actually applies.
func TestOutputDefaultStagesDefaultOutput(t *testing.T) {
	rows := []ui.OutputRow{
		{Name: "api", Format: "dotenv", Path: "src/.env", IsDefault: true},
		{Name: "public", Format: "json", Path: "public/env.json"},
	}
	m := outputsModelOnTab(t, rows)
	m, _ = m.Update(ui.KeyRune('j')) // move the cursor to "public"
	m, _ = m.Update(ui.KeyRune('D'))

	staged := m.Staged()
	want := ui.Change{Kind: ui.ChangeDefaultOutput, Name: "public"}
	if len(staged) != 1 || staged[0] != want {
		t.Fatalf("Staged() = %+v, want exactly %+v", staged, want)
	}

	v := m.View()
	if !strings.Contains(lineFor(t, v, "public"), "*") {
		t.Errorf("newly staged default not marked in the view:\n%s", v)
	}
	if strings.Contains(lineFor(t, v, "api"), "*") {
		t.Errorf("old default still marked once a different row is staged as default:\n%s", v)
	}
}

// D on the row that is already the default does nothing and says why --
// mirrors promoteBlockedReason's refusal on an already-secret row.
func TestOutputDefaultRefusesAlreadyDefaultRow(t *testing.T) {
	rows := []ui.OutputRow{{Name: "api", Format: "dotenv", Path: "src/.env", IsDefault: true}}
	m := outputsModelOnTab(t, rows)
	m, _ = m.Update(ui.KeyRune('D'))

	if len(m.Staged()) != 0 {
		t.Errorf("Staged() = %+v, want empty: api is already the default", m.Staged())
	}
	v := m.View()
	if !strings.Contains(v, "D does nothing here") {
		t.Errorf("no explanation for D's refusal:\n%s", v)
	}
	if !strings.Contains(v, "api") {
		t.Errorf("explanation does not name the row:\n%s", v)
	}
}

// `d` marks an output deleted without removing it from the view -- the same
// staged-not-applied convention the Variables tab already uses, so the user
// is staging an intention they can still abandon with q, and every row
// below it does not shift under a stationary cursor.
func TestOutputDeleteMarksRowWithoutRemovingIt(t *testing.T) {
	rows := []ui.OutputRow{{Name: "api", Format: "dotenv", Path: "src/.env"}}
	m := outputsModelOnTab(t, rows)
	m, _ = m.Update(ui.KeyRune('d'))

	staged := m.Staged()
	want := ui.Change{Kind: ui.ChangeRemoveOutput, Name: "api"}
	if len(staged) != 1 || staged[0] != want {
		t.Fatalf("Staged() = %+v, want exactly %+v", staged, want)
	}
	line := lineFor(t, m.View(), "api")
	if !strings.Contains(line, "deleted") {
		t.Errorf("deleted output not marked in the view:\n%s", line)
	}
}

// d is a toggle, mirroring the Variables tab: pressing it again un-stages
// the delete since nothing has reached disk yet.
func TestOutputDeleteTogglesOff(t *testing.T) {
	rows := []ui.OutputRow{{Name: "api", Format: "dotenv", Path: "src/.env"}}
	m := outputsModelOnTab(t, rows)
	m, _ = m.Update(ui.KeyRune('d'))
	m, _ = m.Update(ui.KeyRune('d'))

	if len(m.Staged()) != 0 {
		t.Errorf("Staged() = %+v, want empty after toggling delete off", m.Staged())
	}
	if strings.Contains(lineFor(t, m.View(), "api"), "deleted") {
		t.Errorf("row still marked deleted after toggling off:\n%s", m.View())
	}
}

// d must refuse over a staged rename rather than let both reach commitStaged
// -- the Outputs-tab counterpart of TestDeleteRefusesOverAStagedEdit -- and
// say why, mirroring TestDeleteRefusalExplainsWhy.
func TestOutputDeleteRefusesOverStagedRename(t *testing.T) {
	rows := []ui.OutputRow{{Name: "api", Format: "dotenv", Path: "src/.env"}}
	m := outputsModelOnTab(t, rows)
	m, _ = m.Update(ui.KeyRune('r'))
	for _, r := range "gateway" {
		m, _ = m.Update(ui.KeyRune(r))
	}
	m, _ = m.Update(ui.KeyType(tea.KeyEnter)) // stage rename-output

	m, _ = m.Update(ui.KeyRune('d')) // must refuse

	staged := m.Staged()
	want := ui.Change{Kind: ui.ChangeRenameOutput, Name: "api", Value: "gateway"}
	if len(staged) != 1 || staged[0] != want {
		t.Fatalf("Staged() = %+v, want exactly %+v (d must refuse over a staged rename)", staged, want)
	}
	v := m.View()
	if !strings.Contains(v, "d is disabled") {
		t.Errorf("no explanation for d's refusal:\n%s", v)
	}
	if !strings.Contains(v, "api") {
		t.Errorf("explanation does not name the row:\n%s", v)
	}
}

// Same refusal, over a staged default instead of a staged rename.
func TestOutputDeleteRefusesOverStagedDefault(t *testing.T) {
	rows := []ui.OutputRow{
		{Name: "api", Format: "dotenv", Path: "src/.env", IsDefault: true},
		{Name: "public", Format: "json", Path: "public/env.json"},
	}
	m := outputsModelOnTab(t, rows)
	m, _ = m.Update(ui.KeyRune('j')) // move to "public"
	m, _ = m.Update(ui.KeyRune('D')) // stage default-output for public

	m, _ = m.Update(ui.KeyRune('d')) // must refuse

	staged := m.Staged()
	want := ui.Change{Kind: ui.ChangeDefaultOutput, Name: "public"}
	if len(staged) != 1 || staged[0] != want {
		t.Fatalf("Staged() = %+v, want exactly %+v (d must refuse over a staged default)", staged, want)
	}
	if !strings.Contains(m.View(), "d is disabled") {
		t.Errorf("no explanation for d's refusal:\n%s", m.View())
	}
}

// r and D must cancel a staged delete first, mirroring beginEdit's own
// cancel-on-edit for the Variables tab: renaming or defaulting a row the
// user marked deleted is a clear, visible decision to keep it.
func TestOutputRenameCancelsAPendingDelete(t *testing.T) {
	rows := []ui.OutputRow{{Name: "api", Format: "dotenv", Path: "src/.env"}}
	m := outputsModelOnTab(t, rows)
	m, _ = m.Update(ui.KeyRune('d')) // stage delete

	m, _ = m.Update(ui.KeyRune('r'))
	for _, r := range "gateway" {
		m, _ = m.Update(ui.KeyRune(r))
	}
	m, _ = m.Update(ui.KeyType(tea.KeyEnter))

	staged := m.Staged()
	want := ui.Change{Kind: ui.ChangeRenameOutput, Name: "api", Value: "gateway"}
	if len(staged) != 1 || staged[0] != want {
		t.Fatalf("Staged() = %+v, want exactly %+v (the delete must be cancelled, not merely outlived)", staged, want)
	}
}

func TestOutputDefaultCancelsAPendingDelete(t *testing.T) {
	rows := []ui.OutputRow{{Name: "api", Format: "dotenv", Path: "src/.env"}}
	m := outputsModelOnTab(t, rows)
	m, _ = m.Update(ui.KeyRune('d')) // stage delete
	m, _ = m.Update(ui.KeyRune('D'))

	staged := m.Staged()
	want := ui.Change{Kind: ui.ChangeDefaultOutput, Name: "api"}
	if len(staged) != 1 || staged[0] != want {
		t.Fatalf("Staged() = %+v, want exactly %+v (the delete must be cancelled, not merely outlived)", staged, want)
	}
}

// D and d are deliberately distinct keys on this tab: capital D makes a row
// the default, lowercase d deletes it. Pressed on the same non-default row,
// they must stage different Change kinds, never the same one.
func TestCapitalDAndLowercaseDStageDifferentKinds(t *testing.T) {
	rows := []ui.OutputRow{{Name: "api", Format: "dotenv", Path: "src/.env"}}

	def := outputsModelOnTab(t, rows)
	def, _ = def.Update(ui.KeyRune('D'))
	if got := def.Staged(); len(got) != 1 || got[0].Kind != ui.ChangeDefaultOutput {
		t.Fatalf("D staged %+v, want exactly one %s", got, ui.ChangeDefaultOutput)
	}

	del := outputsModelOnTab(t, rows)
	del, _ = del.Update(ui.KeyRune('d'))
	if got := del.Staged(); len(got) != 1 || got[0].Kind != ui.ChangeRemoveOutput {
		t.Fatalf("d staged %+v, want exactly one %s", got, ui.ChangeRemoveOutput)
	}
}

// The Outputs tab's own hint line must name its own keys, not the Variables
// tab's -- e/reveal/promote/filter mean nothing here, and D does not exist
// anywhere else.
func TestOutputsTabKeyHintNamesItsOwnKeys(t *testing.T) {
	m := outputsModelOnTab(t, []ui.OutputRow{{Name: "api", Format: "dotenv", Path: "src/.env"}})
	v := m.View()
	for _, want := range []string{"a add", "r rename", "D default", "d delete"} {
		if !strings.Contains(v, want) {
			t.Errorf("outputs tab hint line missing %q:\n%s", want, v)
		}
	}
}

// The cursor on the Outputs tab is its own, separate from the Variables
// matrix's row/col -- mirrors overviewRow's own isolation. j/k here must
// move which output a/r/d/D act on without disturbing the other tab.
func TestOutputsCursorMovesWithJK(t *testing.T) {
	rows := []ui.OutputRow{
		{Name: "api", Format: "dotenv", Path: "a"},
		{Name: "public", Format: "json", Path: "b"},
	}
	m := outputsModelOnTab(t, rows)
	m, _ = m.Update(ui.KeyRune('j'))
	m, _ = m.Update(ui.KeyRune('d')) // must mark "public", the second row

	if strings.Contains(lineFor(t, m.View(), "api"), "deleted") {
		t.Errorf("j moved the cursor onto the wrong row:\n%s", m.View())
	}
	if !strings.Contains(lineFor(t, m.View(), "public"), "deleted") {
		t.Errorf("j did not move the cursor to the second row:\n%s", m.View())
	}

	m, _ = m.Update(ui.KeyRune('k')) // back up to "api"
	m, _ = m.Update(ui.KeyRune('d')) // marks "api" too; "public"'s own delete must be untouched
	if !strings.Contains(lineFor(t, m.View(), "api"), "deleted") {
		t.Errorf("k did not move the cursor back to the first row:\n%s", m.View())
	}
	if len(m.Staged()) != 2 {
		t.Fatalf("Staged() = %+v, want both api and public marked deleted", m.Staged())
	}
}
