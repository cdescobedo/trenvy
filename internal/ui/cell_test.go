package ui_test

import (
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/ui"
)

func s(v string) *string { return &v }

// Classify must mirror resolve.Value's precedence exactly: Profiles[p], then
// Value, then the keyring for secrets, then optional-or-error. If the two ever
// disagree, the matrix is lying about what render will do.
func TestClassify(t *testing.T) {
	cases := []struct {
		name    string
		v       manifest.Var
		profile string
		stored  bool
		want    ui.Cell
	}{
		{"profile override wins", manifest.Var{Value: s("a"), Profiles: map[string]string{"dev": "b"}}, "dev", false, ui.CellOverride},
		{"shared value", manifest.Var{Value: s("a")}, "dev", false, ui.CellShared},
		{"shared empty string is a value", manifest.Var{Value: s("")}, "dev", false, ui.CellShared},
		{"override under another profile only", manifest.Var{Profiles: map[string]string{"dev": "b"}}, "prod", false, ui.CellMissing},
		{"optional and unset", manifest.Var{Optional: true}, "dev", false, ui.CellOptional},
		{"secret stored", manifest.Var{Secret: true}, "dev", true, ui.CellStored},
		{"secret not stored", manifest.Var{Secret: true}, "dev", false, ui.CellMissing},
		{"optional secret not stored", manifest.Var{Secret: true, Optional: true}, "dev", false, ui.CellOptional},
	}
	for _, c := range cases {
		if got := ui.Classify(c.v, c.profile, c.stored); got != c.want {
			t.Errorf("%s: Classify = %q, want %q", c.name, got, c.want)
		}
	}
}
