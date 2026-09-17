package ui_test

import (
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/ui"
)

func TestMatrixOrdersRowsAndColumns(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev", "prod"}},
		Vars: map[string]manifest.Var{
			"ZED": {Value: s("z")}, "ALPHA": {Value: s("a")},
		},
	}
	mx := ui.NewMatrix(m, nil)

	if got := mx.Names; len(got) != 2 || got[0] != "ALPHA" || got[1] != "ZED" {
		t.Errorf("Names = %v, want sorted", got)
	}
	// Profiles keep MANIFEST order, not sorted order: the manifest's order is
	// the user's, and re-sorting would reshuffle their columns.
	if got := mx.Profiles; len(got) != 2 || got[0] != "dev" || got[1] != "prod" {
		t.Errorf("Profiles = %v, want manifest order", got)
	}
}

func TestMatrixCellUsesStoredPresence(t *testing.T) {
	m := &manifest.Manifest{
		Project: manifest.Project{Profiles: []string{"dev", "prod"}},
		Vars:    map[string]manifest.Var{"TOK": {Secret: true}},
	}
	mx := ui.NewMatrix(m, map[string]map[string]bool{"TOK": {"dev": true}})

	if got := mx.Cell("TOK", "dev"); got != ui.CellStored {
		t.Errorf("dev = %q, want stored", got)
	}
	if got := mx.Cell("TOK", "prod"); got != ui.CellMissing {
		t.Errorf("prod = %q, want MISSING", got)
	}
}
