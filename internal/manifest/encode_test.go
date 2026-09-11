package manifest_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cdescobedo/envctl/internal/manifest"
)

func TestEncodeRoundTrips(t *testing.T) {
	original := parseOrFatal(t, backendTOML)

	data, err := original.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	reparsed, err := manifest.Parse(data)
	if err != nil {
		t.Fatalf("Parse(Encode(m)): %v\n%s", err, data)
	}
	if !reflect.DeepEqual(original, reparsed) {
		t.Errorf("round trip changed the manifest\n--- before ---\n%+v\n--- after ---\n%+v", original, reparsed)
	}
}

func TestEncodePreservesEmptyStringVsNilValue(t *testing.T) {
	// This test guards the critical distinction: Var.Value is a *string so that
	// an empty string ("", a legal value) survives parsing distinct from "no
	// value at all" (nil). This matters for Tasks 19, 20, 25.
	fixture := `
[project]
id = "test"
default_target = "local"

[targets.local]
dest = "process"

[vars.EMPTY]
value = ""

[vars.ABSENT]
secret = true
`
	original := parseOrFatal(t, fixture)

	// Verify the starting state has the right structure before encoding.
	if original.Vars["EMPTY"].Value == nil {
		t.Fatal("fixture parse: EMPTY.Value is nil, want pointer to empty string")
	}
	if *original.Vars["EMPTY"].Value != "" {
		t.Fatalf("fixture parse: EMPTY.Value = %q, want empty", *original.Vars["EMPTY"].Value)
	}
	if original.Vars["ABSENT"].Value != nil {
		t.Fatal("fixture parse: ABSENT.Value should be nil")
	}

	// Encode and reparse.
	data, err := original.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	reparsed, err := manifest.Parse(data)
	if err != nil {
		t.Fatalf("Parse(Encode(m)): %v\n%s", err, data)
	}

	// Assert the critical round-trip behavior: empty string stays non-nil,
	// absent value stays nil.
	if reparsed.Vars["EMPTY"].Value == nil {
		t.Error("after round-trip: EMPTY.Value is nil, want pointer to empty string")
	}
	if reparsed.Vars["EMPTY"].Value != nil && *reparsed.Vars["EMPTY"].Value != "" {
		t.Errorf("after round-trip: EMPTY.Value = %q, want empty", *reparsed.Vars["EMPTY"].Value)
	}
	if reparsed.Vars["ABSENT"].Value != nil {
		t.Errorf("after round-trip: ABSENT.Value = %v, want nil", reparsed.Vars["ABSENT"].Value)
	}
}

func TestEncodeIsStable(t *testing.T) {
	m := parseOrFatal(t, backendTOML)
	first, err := m.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for i := 0; i < 5; i++ {
		next, err := m.Encode()
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if string(next) != string(first) {
			t.Fatal("Encode is not deterministic across calls")
		}
	}
}

func TestSaveWritesPrivateFileAndCreatesParents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects", "p.toml")
	m := parseOrFatal(t, backendTOML)

	if err := manifest.Save(path, m); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600", got)
	}

	if _, err := manifest.Load(path); err != nil {
		t.Errorf("Load after Save: %v", err)
	}
}

func TestSaveTightensExistingFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.toml")
	m := parseOrFatal(t, backendTOML)

	// Create the file with wrong permissions.
	if err := os.WriteFile(path, []byte("[project]\nid=\"test\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat before Save: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("pre-existing file mode = %o, want 644", got)
	}

	// Save over it.
	if err := manifest.Save(path, m); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Permissions should be tightened to 0o600.
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("Stat after Save: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode after Save = %o, want 600", got)
	}
}

func TestNewSaltIsRandomAndHex(t *testing.T) {
	a, err := manifest.NewSalt()
	if err != nil {
		t.Fatalf("NewSalt: %v", err)
	}
	b, err := manifest.NewSalt()
	if err != nil {
		t.Fatalf("NewSalt: %v", err)
	}
	if a == b {
		t.Error("NewSalt returned the same value twice")
	}
	if len(a) != 64 {
		t.Errorf("salt length = %d, want 64 hex chars", len(a))
	}
}
