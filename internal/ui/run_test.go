package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
)

// revealTestModel builds a model over one two-profile, one-secret fixture --
// just enough for host's reveal plumbing, which does not care about the rest
// of the matrix.
func revealTestModel() Model {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev", "prod"}},
		Vars: map[string]manifest.Var{
			"TOKEN": {Secret: true},
		},
	}
	mx := NewMatrix(m, nil)
	return New("demo", mx, nil)
}

// A failed reveal -- including one for a secret that is genuinely absent --
// must leave the cell masked, not blank. Turning the error into
// RevealedMsg{Value: ""} would be enough to break it: RevealedMsg sets the
// cell revealed unconditionally, and View shows a revealed cell verbatim, so
// the cell would render blank instead of MISSING. Mirrors internal/tui's
// identically-named test.
func TestHostRevealFailureLeavesCellMasked(t *testing.T) {
	m := revealTestModel()
	m, _ = m.Update(KeyRune('2')) // Variables tab: MISSING only renders there.
	h := host{m: m, reveal: func(string, string) (string, error) {
		return "", errors.New("not found")
	}}

	_, cmd := h.Update(RevealRequestMsg{Name: "TOKEN", Profile: "dev"})
	if cmd == nil {
		t.Fatal("reveal issued no command to fetch the value")
	}

	msg := cmd()
	if msg != nil {
		t.Errorf("a failed reveal produced a message (%#v); it must produce none", msg)
	}

	if out := h.m.View(); !strings.Contains(out, "MISSING") {
		t.Errorf("view does not still show MISSING after a failed reveal:\n%s", out)
	}
}

// Unlike internal/tui's flat table, this matrix has one cell per profile, so
// host.reveal must receive both Name and Profile -- Name alone cannot say
// whether a reveal on the "dev" or the "prod" column asked for it.
func TestHostRevealPassesNameAndProfile(t *testing.T) {
	m := revealTestModel()
	var gotName, gotProfile string
	h := host{m: m, reveal: func(name, profile string) (string, error) {
		gotName, gotProfile = name, profile
		return "s3cret", nil
	}}

	_, cmd := h.Update(RevealRequestMsg{Name: "TOKEN", Profile: "prod"})
	msg := cmd()

	if gotName != "TOKEN" || gotProfile != "prod" {
		t.Errorf("reveal received (%q, %q), want (TOKEN, prod)", gotName, gotProfile)
	}

	revealed, ok := msg.(RevealedMsg)
	if !ok {
		t.Fatalf("cmd() = %#v, want a RevealedMsg", msg)
	}
	if revealed.Name != "TOKEN" || revealed.Profile != "prod" || revealed.Value != "s3cret" {
		t.Errorf("RevealedMsg = %+v, want {TOKEN prod s3cret}", revealed)
	}
}
