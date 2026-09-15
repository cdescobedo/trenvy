package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cdescobedo/trenvy/internal/fsutil"
)

func TestWriteAtomicCreatesAPrivateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", ".env")

	if err := fsutil.WriteAtomic(path, []byte("A=1\n")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "A=1\n" {
		t.Errorf("contents = %q", data)
	}
}

func TestWriteAtomicReplacesAndTightensPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := fsutil.WriteAtomic(path, []byte("new")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600 (a world-readable predecessor must be tightened)", got)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Errorf("contents = %q", data)
	}
}

func TestWriteAtomicLeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	if err := fsutil.WriteAtomic(filepath.Join(dir, ".env"), []byte("x")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory contains %v, want only the target file", names)
	}
}
