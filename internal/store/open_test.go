package store_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/store"
)

func TestOpenFileBackendUsesThePassphraseEnvVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENVCTL_CONFIG_DIR", dir)
	t.Setenv("ENVCTL_PASSPHRASE", "pw")

	s, err := store.Open("file", "someone")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Set("envctl/p/A", "v"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Prove Open wired the configured path and passphrase by constructing a
	// fresh File on the same path and passphrase and checking the value
	// round-trips through it.
	again := store.NewFile(filepath.Join(config.Dir(), "secrets.age"), "pw")
	got, err := again.Get("envctl/p/A")
	if err != nil {
		t.Fatalf("Get on fresh File: %v", err)
	}
	if got != "v" {
		t.Errorf("Get = %q, want v", got)
	}
}

func TestOpenFileBackendWithoutAPassphraseExplainsItself(t *testing.T) {
	t.Setenv("ENVCTL_CONFIG_DIR", t.TempDir())
	t.Setenv("ENVCTL_PASSPHRASE", "")

	_, err := store.Open("file", "someone")
	if err == nil {
		t.Fatal("Open succeeded with no passphrase available")
	}
	if !strings.Contains(err.Error(), "ENVCTL_PASSPHRASE") {
		t.Errorf("error does not say how to supply one: %v", err)
	}
}

func TestOpenMemBackend(t *testing.T) {
	s, err := store.Open("mem", "someone")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, ok := s.(*store.Mem); !ok {
		t.Errorf("Open(mem) returned %T", s)
	}
}

func TestOpenRejectsAnUnknownBackend(t *testing.T) {
	_, err := store.Open("telepathy", "someone")
	if err == nil || !strings.Contains(err.Error(), "telepathy") {
		t.Fatalf("err = %v, want one naming the unknown backend", err)
	}
}

func TestDefaultBackendIsNotEmpty(t *testing.T) {
	if store.DefaultBackend() == "" {
		t.Error("DefaultBackend returned an empty string")
	}
}
