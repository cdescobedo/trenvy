package resolve_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/resolve"
)

// OutputHasSecret decides whether "status --quick" can report real freshness
// for an output or must report it unknown. Getting it wrong in the permissive
// direction makes --quick read secrets it promised not to; in the strict
// direction it hides freshness a secret-free output could have reported.
func TestOutputHasSecret(t *testing.T) {
	plain := "x"
	m := &manifest.Manifest{
		Vars: map[string]manifest.Var{
			"API_KEY":   {Secret: true},
			"DB_PASS":   {Secret: true},
			"LOG_LEVEL": {Value: &plain},
		},
		Outputs: map[string]manifest.Output{
			"all":           {Include: []string{"*"}},
			"plain-only":    {Include: []string{"LOG_*"}},
			"excluded":      {Include: []string{"*"}, Exclude: []string{"API_KEY", "DB_PASS"}},
			"empty-include": {},
			"bad-glob":      {Include: []string{"["}},
		},
	}

	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{"a wildcard include selects the secrets", "all", true},
		{"an include that matches only plain vars selects no secret", "plain-only", false},
		{"exclude subtracts the secrets back out", "excluded", false},
		{"an empty include means everything, secrets included", "empty-include", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolve.OutputHasSecret(m, c.output)
			if err != nil {
				t.Fatalf("OutputHasSecret(%q): %v", c.output, err)
			}
			if got != c.want {
				t.Errorf("OutputHasSecret(%q) = %v, want %v", c.output, got, c.want)
			}
		})
	}

	t.Run("an unknown output is named, not silently false", func(t *testing.T) {
		_, err := resolve.OutputHasSecret(m, "nope")
		if !errors.Is(err, resolve.ErrUnknownOutput) {
			t.Fatalf("error = %v, want ErrUnknownOutput", err)
		}
		if !strings.Contains(err.Error(), "nope") {
			t.Errorf("error does not name the output: %v", err)
		}
	})

	// A malformed glob makes path.Match return an error rather than a
	// non-match. Discarding it would make every variable look unselected, so
	// an output with a typo'd include would quietly claim it has no secrets
	// and --quick would report freshness it never actually checked.
	t.Run("a malformed include glob is an error, not a non-match", func(t *testing.T) {
		_, err := resolve.OutputHasSecret(m, "bad-glob")
		if err == nil {
			t.Fatal("a malformed include pattern was silently treated as a non-match")
		}
		if !strings.Contains(err.Error(), "bad-glob") {
			t.Errorf("error does not name the output: %v", err)
		}
	})
}
