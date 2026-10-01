package session_test

import (
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
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
		want    session.Cell
	}{
		{"profile override wins", manifest.Var{Value: s("a"), Profiles: map[string]string{"dev": "b"}}, "dev", false, session.CellOverride},
		{"shared value", manifest.Var{Value: s("a")}, "dev", false, session.CellShared},
		{"shared empty string is a value", manifest.Var{Value: s("")}, "dev", false, session.CellShared},
		{"override under another profile only", manifest.Var{Profiles: map[string]string{"dev": "b"}}, "prod", false, session.CellMissing},
		{"optional and unset", manifest.Var{Optional: true}, "dev", false, session.CellOptional},
		{"secret stored", manifest.Var{Secret: true}, "dev", true, session.CellStored},
		{"secret not stored", manifest.Var{Secret: true}, "dev", false, session.CellMissing},
		{"optional secret not stored", manifest.Var{Secret: true, Optional: true}, "dev", false, session.CellOptional},
	}
	for _, c := range cases {
		if got := session.Classify(c.v, c.profile, c.stored); got != c.want {
			t.Errorf("%s: Classify = %q, want %q", c.name, got, c.want)
		}
	}
}
