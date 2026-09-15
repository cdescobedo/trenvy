package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/store"
)

func TestFileStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.age")
	f := store.NewFile(path, "correct horse")

	if ok, err := f.Has("trenvy/p/A"); err != nil || ok {
		t.Fatalf("Has on empty = (%v, %v)", ok, err)
	}
	if err := f.Set("trenvy/p/A", "one"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := f.Set("trenvy/p/B", "two"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// A fresh instance proves the data survived the round trip to disk.
	again := store.NewFile(path, "correct horse")
	got, err := again.Get("trenvy/p/A")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "one" {
		t.Errorf("Get = %q, want one", got)
	}

	if err := again.Delete("trenvy/p/A"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok, _ := store.NewFile(path, "correct horse").Has("trenvy/p/A"); ok {
		t.Error("deleted item came back")
	}
}

func TestFileStoreIsEncryptedAtRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.age")
	f := store.NewFile(path, "pw")
	if err := f.Set("trenvy/p/A", "plaintextsecret"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "plaintextsecret") {
		t.Error("the value is readable in the file on disk")
	}
	if strings.Contains(string(data), "trenvy/p/A") {
		t.Error("the service name is readable in the file on disk")
	}
}

func TestFileStoreIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.age")
	if err := store.NewFile(path, "pw").Set("s", "v"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600", got)
	}
}

func TestFileStoreWrongPassphraseFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.age")
	if err := store.NewFile(path, "right").Set("s", "v"); err != nil {
		t.Fatal(err)
	}
	_, err := store.NewFile(path, "wrong").Get("s")
	if err == nil {
		t.Fatal("Get succeeded with the wrong passphrase")
	}
	if errors.Is(err, store.ErrNotFound) {
		t.Error("a decryption failure must not be reported as a missing secret")
	}
}

// The file backend must enforce the same size limit as the platform
// keyrings, so a value that fits here does not mysteriously fail once it
// round-trips through a teammate's Windows Credential Manager.
func TestFileStoreRejectsOversizedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.age")
	f := store.NewFile(path, "pw")

	err := f.Set("trenvy/p/BIG", strings.Repeat("x", store.MaxValueBytes+1))
	if !errors.Is(err, store.ErrTooLong) {
		t.Fatalf("Set over the size limit: got %v, want ErrTooLong", err)
	}
	if !strings.Contains(err.Error(), "BIG") {
		t.Errorf("error does not name the variable: %v", err)
	}
	if strings.Contains(err.Error(), "xxxx") {
		t.Errorf("error leaked the value: %v", err)
	}
}

func TestFileStoreMissingKeyIsNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.age")
	if err := store.NewFile(path, "pw").Set("a", "v"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewFile(path, "pw").Get("b"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get = %v, want ErrNotFound", err)
	}
}

// TestFileStoreSaveIsAtomic proves save() routes through the shared atomic
// write primitive rather than truncating the one copy of the secrets in
// place: no leftover temp file, and a pre-existing world-readable file gets
// tightened by the replace rather than left at its old mode (which a plain
// os.WriteFile onto an existing file would do, since WriteFile only applies
// its mode when creating a new file).
func TestFileStoreSaveIsAtomic(t *testing.T) {
	t.Run("leaves no temp file behind", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "secrets.age")
		if err := store.NewFile(path, "pw").Set("a", "v"); err != nil {
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
			t.Errorf("directory contains %v, want only the secrets file", names)
		}
	})

	t.Run("tightens a preexisting world-readable file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "secrets.age")
		f := store.NewFile(path, "pw")
		if err := f.Set("a", "v"); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}

		if err := f.Set("b", "v2"); err != nil {
			t.Fatal(err)
		}

		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("mode = %o, want 600 (a world-readable predecessor must be tightened)", got)
		}
	})
}
