package ui_test

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/style"
	"github.com/cdescobedo/trenvy/internal/ui"
)

// colourStyler forces TrueColor explicitly: under plain `go test` the default
// renderer is Ascii and strips every escape, which is also why the existing
// assertions survive a Styler they never set. termenv.WithProfile() as a
// NewRenderer option silently does nothing, so SetColorProfile is the only
// route.
func colourStyler() style.Styler {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.TrueColor)
	return style.New(r)
}

// allCellsModelStyled builds allCellsModel's fixture with a Styler set.
// allCellsModel itself stays colourless: many tests depend on that.
func allCellsModelStyled(t *testing.T, st style.Styler) ui.Model {
	t.Helper()
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev", "prod"}},
		Vars: map[string]manifest.Var{
			"SHARED_VAR":    {Value: s("v")},
			"OVERRIDE_VAR":  {Value: s("a"), Profiles: map[string]string{"dev": "b"}},
			"STORED_SECRET": {Secret: true},
			"MISSING_VAR":   {},
			"OPTIONAL_VAR":  {Optional: true},
		},
	}
	stored := map[string]map[string]bool{
		"STORED_SECRET": {"dev": true, "prod": true},
	}
	mx := ui.NewMatrix(m, stored)
	p := testParams(mx, ui.Problems(mx, nil))
	p.Styler = st
	mod := ui.New(p)
	mod, _ = mod.Update(ui.KeyRune('2')) // Variables tab
	return mod
}

func TestMatrixColoursMissingWhenTheProfileAllowsIt(t *testing.T) {
	m := allCellsModelStyled(t, colourStyler())
	out := m.View()

	if !strings.Contains(out, "MISSING") {
		t.Fatalf("no MISSING row in the fixture; the test proves nothing:\n%s", out)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("MISSING rendered without colour under a TrueColor profile:\n%q", out)
	}
}

// Each cell word must reach its own token, not merely some colour: a routing
// that sent Stored through Missing would still contain an escape.
func TestMatrixRoutesEachCellThroughItsOwnToken(t *testing.T) {
	st := colourStyler()
	out := allCellsModelStyled(t, st).View()

	cases := []struct {
		word string
		want string
	}{
		{string(ui.CellMissing), st.Missing(string(ui.CellMissing))},
		{string(ui.CellStored), st.Stored(string(ui.CellStored))},
		{string(ui.CellOverride), st.Override(string(ui.CellOverride))},
		{string(ui.CellOptional), st.Optional(string(ui.CellOptional))},
		{string(ui.CellShared), st.Shared(string(ui.CellShared))},
	}
	for _, c := range cases {
		if !strings.Contains(out, c.word) {
			t.Fatalf("fixture has no %q cell; the case proves nothing:\n%s", c.word, out)
		}
		if !strings.Contains(out, c.want) {
			t.Errorf("%q not routed through its token; want %q in:\n%q", c.word, c.want, out)
		}
	}
	// The guard against a vacuous table: the coloured tokens really differ
	// from plain text and from one another.
	if st.Missing("x") == "x" || st.Missing("x") == st.Stored("x") {
		t.Fatalf("Missing and Stored are indistinguishable; routing cannot be tested")
	}
}

func TestOutputsTabRoutesEachStateThroughItsOwnToken(t *testing.T) {
	st := colourStyler()
	states := []struct {
		state string
		want  string
	}{
		{ui.StateFresh, st.Fresh(ui.StateFresh)},
		{ui.StateStale, st.Stale(ui.StateStale)},
		{ui.StateModified, st.Modified(ui.StateModified)},
		{ui.StateForeign, st.Foreign(ui.StateForeign)},
		{ui.StateAbsent, st.Absent(ui.StateAbsent)},
		{ui.StateUnknown, st.Unknown(ui.StateUnknown)},
		{ui.StateUnverified, st.Unknown(ui.StateUnverified)},
	}
	var rows []ui.OutputRow
	for i, c := range states {
		rows = append(rows, ui.OutputRow{Name: "o" + string(rune('a'+i)), Format: "dotenv", Path: "p", Selects: "all", State: c.state})
	}
	p := outputsParams(rows, false)
	p.Styler = st
	m := ui.New(p)
	m, _ = m.Update(ui.KeyRune('4'))
	out := m.View()

	for _, c := range states {
		if !strings.Contains(out, c.state) {
			t.Fatalf("fixture has no %q state; the case proves nothing:\n%s", c.state, out)
		}
		if c.want == c.state {
			t.Fatalf("token for %q renders plain under TrueColor; the case proves nothing", c.state)
		}
		if !strings.Contains(out, c.want) {
			t.Errorf("state %q not routed through its token; want %q in:\n%q", c.state, c.want, out)
		}
	}
}

// The zero Styler is the invariant the existing assertions stand on.
func TestZeroStylerEmitsNoEscapes(t *testing.T) {
	out := allCellsModelStyled(t, style.Styler{}).View()
	if !strings.Contains(out, "MISSING") {
		t.Fatalf("no MISSING row in the fixture:\n%s", out)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("zero Styler emitted an escape:\n%q", out)
	}
}

// The staged-delete marker is the staged signal; the name beside it is
// ordinary text. Only the marker is coloured, its literal word survives, and
// the row still lines up with an undeleted one -- the first styled string to
// enter a Columns cell.
func TestMatrixColoursTheDeletedMarkerAndKeepsColumnsAligned(t *testing.T) {
	st := colourStyler()
	m := allCellsModelStyled(t, st)
	// Cursor starts on the first visible row; move to a row with no other
	// variable depending on it and mark it deleted.
	m, _ = m.Update(ui.KeyRune('d'))
	out := m.View()

	if !strings.Contains(out, "(deleted)") {
		t.Fatalf("no row was marked deleted; the test proves nothing:\n%s", out)
	}
	if want := " " + st.Staged("(deleted)"); !strings.Contains(out, want) {
		t.Errorf("marker not routed through Staged; want %q in:\n%q", want, out)
	}
	if st.Staged("(deleted)") == "(deleted)" {
		t.Fatal("Staged renders plain under TrueColor; the test proves nothing")
	}
	assertProfileColumnAligned(t, out)
}

func TestOutputsTabColoursTheDeletedMarker(t *testing.T) {
	st := colourStyler()
	rows := []ui.OutputRow{
		{Name: "docker", Format: "dotenv", Path: "p", Selects: "all", State: ui.StateFresh},
		{Name: "public", Format: "json", Path: "q", Selects: "all", State: ui.StateFresh},
	}
	p := outputsParams(rows, false)
	p.Styler = st
	m := ui.New(p)
	m, _ = m.Update(ui.KeyRune('4'))
	m, _ = m.Update(ui.KeyRune('d'))
	out := m.View()

	if !strings.Contains(out, "(deleted)") {
		t.Fatalf("no output was marked deleted; the test proves nothing:\n%s", out)
	}
	if want := " " + st.Staged("(deleted)"); !strings.Contains(out, want) {
		t.Errorf("marker not routed through Staged; want %q in:\n%q", want, out)
	}
	assertColumnAligned(t, out, "FORMAT")
}

// assertProfileColumnAligned checks the dev column starts at the same visible
// offset on the header and on every data row, once escapes are stripped.
func assertProfileColumnAligned(t *testing.T, out string) {
	t.Helper()
	assertColumnAligned(t, out, "dev")
}

func assertColumnAligned(t *testing.T, out, header string) {
	t.Helper()
	plain := stripANSI(out)
	var hdr string
	var rest []string
	for _, l := range strings.Split(plain, "\n") {
		if hdr == "" && strings.Contains(l, "NAME") && strings.Contains(l, header) {
			hdr = l
			continue
		}
		if hdr != "" {
			if strings.TrimSpace(l) == "" {
				break // the grid ends at the first blank line
			}
			rest = append(rest, l)
		}
	}
	if hdr == "" || len(rest) == 0 {
		t.Fatalf("no header/rows found in:\n%s", plain)
	}
	col := strings.Index(hdr, header)
	for _, l := range rest {
		if len(l) <= col || l[col-1] != ' ' || l[col] == ' ' {
			t.Errorf("column %q misaligned at offset %d in row %q\nheader %q", header, col, l, hdr)
		}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// A revealed secret is DATA, not a token. cellText reports isCell=false for
// it precisely so a value that happens to spell a Cell word is not routed
// through that word's colour: revealing a secret whose value is literally
// "MISSING" must not paint it red and tell the user the variable is unset.
//
// Coverage finding, not red-green: the safeguard was already correct, but
// flipping cellText's `return v, false` to `return v, true` left the whole
// suite green. This test is green on arrival and was mutation-tested instead.
func TestRevealedSecretSpellingAWordIsNotColouredAsThatWord(t *testing.T) {
	st := colourStyler()
	m := allCellsModelStyled(t, st)
	m, _ = m.Update(ui.RevealedMsg{Name: "STORED_SECRET", Profile: "dev", Value: "MISSING"})
	out := m.View()

	revealed := lineFor(t, out, "STORED_SECRET")
	if !strings.Contains(revealed, "MISSING") {
		t.Fatalf("revealed value not on screen; the test proves nothing:\n%q", revealed)
	}
	if strings.Contains(revealed, st.Missing("MISSING")) {
		t.Errorf("revealed value %q was painted as the MISSING token:\n%q", "MISSING", revealed)
	}
	// The same line still carries a genuine token (prod stays masked), so
	// "no escape on the revealed cell" is not just "no colour anywhere".
	if !strings.Contains(revealed, st.Stored("stored")) {
		t.Fatalf("STORED_SECRET/prod lost its real token; fixture is wrong:\n%q", revealed)
	}
	// And a genuine MISSING token elsewhere on the screen is still coloured.
	if !strings.Contains(lineFor(t, out, "MISSING_VAR"), st.Missing("MISSING")) {
		t.Errorf("a real MISSING cell is no longer coloured:\n%q", out)
	}
}
