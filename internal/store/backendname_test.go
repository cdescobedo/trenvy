package store_test

import (
	"runtime"
	"testing"

	"github.com/cdescobedo/envctl/internal/store"
)

// ResolveBackendName is what status reports to the user, and it is also the
// function Open routes through, so a drift here is a drift in what Open
// actually does. These cases pin the precedence itself rather than any one
// platform's answer.
func TestResolveBackendNamePrecedence(t *testing.T) {
	t.Run("an explicit flag wins over the environment", func(t *testing.T) {
		t.Setenv("ENVCTL_BACKEND", "file")
		if got := store.ResolveBackendName("mem"); got != "mem" {
			t.Errorf("ResolveBackendName(\"mem\") = %q, want %q", got, "mem")
		}
	})

	t.Run("the environment wins over the platform default", func(t *testing.T) {
		t.Setenv("ENVCTL_BACKEND", "file")
		if got := store.ResolveBackendName(""); got != "file" {
			t.Errorf("ResolveBackendName(\"\") = %q, want %q", got, "file")
		}
	})

	t.Run("an empty environment falls through to the platform default", func(t *testing.T) {
		t.Setenv("ENVCTL_BACKEND", "")
		want := store.DefaultBackend()
		if got := store.ResolveBackendName(""); got != want {
			t.Errorf("ResolveBackendName(\"\") = %q, want the platform default %q", got, want)
		}
	})
}

// The platform default is a global constraint: macOS must never silently fall
// back to the file backend, because that would store secrets in a passphrase
// file instead of the Keychain the spec promises.
func TestDefaultBackendMatchesThePlatform(t *testing.T) {
	got := store.DefaultBackend()
	var want string
	switch runtime.GOOS {
	case "darwin":
		want = store.BackendKeychain
	case "linux", "windows":
		want = store.BackendSystem
	default:
		want = store.BackendFile
	}
	if got != want {
		t.Errorf("DefaultBackend() on %s = %q, want %q", runtime.GOOS, got, want)
	}
}
