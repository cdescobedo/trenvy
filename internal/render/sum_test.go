package render_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/render"
)

func TestSumIsStableForTheSameValues(t *testing.T) {
	r := resolved(map[string]string{"A": "1", "B": "2"})
	first := render.Sum("salt", r)
	if first != render.Sum("salt", r) {
		t.Error("Sum is not stable")
	}
}

func TestSumChangesWhenAValueChanges(t *testing.T) {
	a := render.Sum("salt", resolved(map[string]string{"A": "1"}))
	b := render.Sum("salt", resolved(map[string]string{"A": "2"}))
	if a == b {
		t.Error("Sum did not change when a value changed")
	}
}

func TestSumChangesWhenAVariableIsAddedOrRemoved(t *testing.T) {
	a := render.Sum("salt", resolved(map[string]string{"A": "1"}))
	b := render.Sum("salt", resolved(map[string]string{"A": "1", "B": "2"}))
	if a == b {
		t.Error("Sum did not change when a variable was added")
	}
}

// The salt is what stops the state file from weakening the secrets it
// describes: without it, a low-entropy secret's digest would be brute-forceable
// by anyone who read the file.
func TestSumIsKeyedBySalt(t *testing.T) {
	r := resolved(map[string]string{"A": "1"})
	if render.Sum("salt-one", r) == render.Sum("salt-two", r) {
		t.Error("Sum ignores the salt")
	}
}

// Concatenating name and value without a separator would let {AB: "c"} and
// {A: "Bc"} collide.
func TestSumDistinguishesAmbiguousSplits(t *testing.T) {
	a := render.Sum("s", resolved(map[string]string{"AB": "c"}))
	b := render.Sum("s", resolved(map[string]string{"A": "Bc"}))
	if a == b {
		t.Error("Sum collides on an ambiguous name/value split")
	}
}

func TestSumNeverContainsAValue(t *testing.T) {
	got := render.Sum("salt", resolved(map[string]string{"A": "s3cretvalue"}))
	if strings.Contains(got, "s3cret") {
		t.Errorf("Sum leaked a value: %q", got)
	}
}

func TestFileSumAtReadsTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := render.FileSumAt(path)
	if err != nil {
		t.Fatalf("FileSumAt: %v", err)
	}
	if got != render.FileSum([]byte("hello")) {
		t.Errorf("FileSumAt = %q, does not match FileSum", got)
	}
}
