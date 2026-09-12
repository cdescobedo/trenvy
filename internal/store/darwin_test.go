//go:build darwin

package store_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/store"
)

// These tests touch the real login Keychain, so they are opt-in and scoped to
// a throwaway service prefix that no real project uses.
func requireIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("ENVCTL_INTEGRATION") != "1" {
		t.Skip("set ENVCTL_INTEGRATION=1 to run Keychain integration tests")
	}
}

func TestKeychainSatisfiesStore(t *testing.T) {
	var _ store.Store = store.NewKeychain("someone")
}

func TestKeychainRoundTrip(t *testing.T) {
	requireIntegration(t)
	user := os.Getenv("USER")
	k := store.NewKeychain(user)
	svc := fmt.Sprintf("envctl-test/%d/VALUE", os.Getpid())
	t.Cleanup(func() { _ = k.Delete(svc) })

	if ok, err := k.Has(svc); err != nil || ok {
		t.Fatalf("Has before Set = (%v, %v)", ok, err)
	}
	if _, err := k.Get(svc); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get before Set = %v, want ErrNotFound", err)
	}

	// A value containing the characters that break naive shell quoting.
	const secret = `p@ss"w\ord ;$(whoami)`
	if err := k.Set(svc, secret); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := k.Get(svc)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != secret {
		t.Errorf("Get = %q, want %q", got, secret)
	}

	if err := k.Set(svc, "replaced"); err != nil {
		t.Fatalf("Set (update): %v", err)
	}
	if got, _ := k.Get(svc); got != "replaced" {
		t.Errorf("after update Get = %q", got)
	}

	if err := k.Delete(svc); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok, _ := k.Has(svc); ok {
		t.Error("Has after Delete = true")
	}
}

// The value must never reach argv, because ps shows argv to every user on the
// machine. This asserts the property directly on the command that is built.
func TestKeychainSetPutsNoValueInArgv(t *testing.T) {
	var captured []string
	restore := store.SetKeychainRunner(func(cmd *exec.Cmd) ([]byte, error) {
		captured = append([]string(nil), cmd.Args...)
		return nil, nil
	})
	t.Cleanup(restore)

	k := store.NewKeychain("someone")
	if err := k.Set("envctl/p/TOKEN", "s3cretvalue"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	for _, arg := range captured {
		if strings.Contains(arg, "s3cretvalue") {
			t.Fatalf("secret value appeared in argv: %v", captured)
		}
	}
}

func TestKeychainSetRejectsNewline(t *testing.T) {
	restore := store.SetKeychainRunner(func(cmd *exec.Cmd) ([]byte, error) {
		return nil, nil
	})
	t.Cleanup(restore)

	k := store.NewKeychain("someone")
	err := k.Set("envctl/service/KEY", "value\nmalicious")
	if !errors.Is(err, store.ErrControlChar) {
		t.Fatalf("Set with newline: got %v, want ErrControlChar", err)
	}
	if strings.Contains(err.Error(), "value\nmalicious") {
		t.Error("error message contains the secret value")
	}
	if !strings.Contains(err.Error(), "envctl/service/KEY") {
		t.Error("error message does not name the service")
	}
}

func TestKeychainSetRejectsCarriageReturn(t *testing.T) {
	restore := store.SetKeychainRunner(func(cmd *exec.Cmd) ([]byte, error) {
		return nil, nil
	})
	t.Cleanup(restore)

	k := store.NewKeychain("someone")
	err := k.Set("envctl/service/KEY", "value\rmalicious")
	if !errors.Is(err, store.ErrControlChar) {
		t.Fatalf("Set with carriage return: got %v, want ErrControlChar", err)
	}
	if strings.Contains(err.Error(), "value\rmalicious") {
		t.Error("error message contains the secret value")
	}
	if !strings.Contains(err.Error(), "envctl/service/KEY") {
		t.Error("error message does not name the service")
	}
}

func TestKeychainGetRemovesOnlyOneTrailingNewline(t *testing.T) {
	restore := store.SetKeychainRunner(func(cmd *exec.Cmd) ([]byte, error) {
		return []byte("value\n\n"), nil
	})
	t.Cleanup(restore)

	k := store.NewKeychain("someone")
	got, err := k.Get("envctl/service/KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "value\n" {
		t.Errorf("Get returned %q, want %q", got, "value\n")
	}
}
