package ui_test

import (
	"strings"
	"testing"

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
// must read as a state, not a blank, broken panel, and must name the real
// command that adds one rather than inventing a key this tab does not wire
// up yet.
func TestOutputsTabEmptyStateNamesTheAddCommand(t *testing.T) {
	m := ui.New(outputsParams(nil, false))
	m, _ = m.Update(ui.KeyRune('4'))

	v := m.View()
	if !strings.Contains(v, "no outputs") {
		t.Errorf("empty outputs tab does not say plainly there are none:\n%s", v)
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
