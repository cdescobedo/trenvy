package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/store"
)

func TestFileStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.age")
	f := store.NewFile(path, "correct horse")

	if ok, err := f.Has("envctl/p/A"); err != nil || ok {
		t.Fatalf("Has on empty = (%v, %v)", ok, err)
	}
	if err := f.Set("envctl/p/A", "one"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := f.Set("envctl/p/B", "two"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// A fresh instance proves the data survived the round trip to disk.
	again := store.NewFile(path, "correct horse")
	got, err := again.Get("envctl/p/A")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "one" {
		t.Errorf("Get = %q, want one", got)
	}

	if err := again.Delete("envctl/p/A"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok, _ := store.NewFile(path, "correct horse").Has("envctl/p/A"); ok {
		t.Error("deleted item came back")
	}
}

func TestFileStoreIsEncryptedAtRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.age")
	f := store.NewFile(path, "pw")
	if err := f.Set("envctl/p/A", "plaintextsecret"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "plaintextsecret") {
		t.Error("the value is readable in the file on disk")
	}
	if strings.Contains(string(data), "envctl/p/A") {
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

func TestFileStoreMissingKeyIsNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.age")
	if err := store.NewFile(path, "pw").Set("a", "v"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewFile(path, "pw").Get("b"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get = %v, want ErrNotFound", err)
	}
}
