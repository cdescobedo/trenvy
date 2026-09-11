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
