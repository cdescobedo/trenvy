//go:build darwin

package store_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/store"
)

// exitErrorWithCode fabricates a genuine *exec.ExitError carrying code, by
// re-executing this test binary as a subprocess that does nothing but
// os.Exit(code). This is the standard technique for producing a real
// *exec.ExitError in a test (os/exec's own tests use it): no external tool,
// no real keychain, and no network are involved, only the test binary itself
// exiting deterministically.
func exitErrorWithCode(t *testing.T, code int) *exec.ExitError {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestDarwinHelperProcess")
	cmd.Env = append(os.Environ(),
		"ENVCTL_WANT_HELPER_PROCESS=1",
		"ENVCTL_HELPER_EXIT_CODE="+strconv.Itoa(code))
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("could not synthesize an *exec.ExitError: %v", err)
	}
	return ee
}

// TestDarwinHelperProcess is not a real test: it is the subprocess body
// exitErrorWithCode spawns. It exits immediately unless invoked the way that
// helper invokes it.
func TestDarwinHelperProcess(t *testing.T) {
	if os.Getenv("ENVCTL_WANT_HELPER_PROCESS") != "1" {
		return
	}
	code, _ := strconv.Atoi(os.Getenv("ENVCTL_HELPER_EXIT_CODE"))
	os.Exit(code)
}

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

func TestKeychainSetRejectsNULByte(t *testing.T) {
	restore := store.SetKeychainRunner(func(cmd *exec.Cmd) ([]byte, error) {
		return nil, nil
	})
	t.Cleanup(restore)

	k := store.NewKeychain("someone")
	err := k.Set("envctl/service/KEY", "value\x00malicious")
	if !errors.Is(err, store.ErrControlChar) {
		t.Fatalf("Set with a NUL byte: got %v, want ErrControlChar", err)
	}
	if !strings.Contains(err.Error(), "envctl/service/KEY") {
		t.Error("error message does not name the service")
	}
}

func TestKeychainSetRejectsOversizedValues(t *testing.T) {
	restore := store.SetKeychainRunner(func(cmd *exec.Cmd) ([]byte, error) {
		return nil, nil
	})
	t.Cleanup(restore)

	k := store.NewKeychain("someone")
	err := k.Set("envctl/p/BIG", strings.Repeat("x", store.MaxValueBytes+1))
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

// Only exit code 44 means "not found". Every other non-zero status --
// authorization denied, user cancelled, and so on -- must surface as a real
// error rather than being read as the secret simply being absent, or a locked
// keychain would make status report a credential the user already has as
// MISSING.
func TestKeychainHasDistinguishesNotFoundFromOtherFailures(t *testing.T) {
	t.Run("exit 44 is not found", func(t *testing.T) {
		restore := store.SetKeychainRunner(func(cmd *exec.Cmd) ([]byte, error) {
			return nil, exitErrorWithCode(t, 44)
		})
		t.Cleanup(restore)

		k := store.NewKeychain("someone")
		ok, err := k.Has("envctl/p/A")
		if err != nil {
			t.Fatalf("Has: %v", err)
		}
		if ok {
			t.Error("Has = true, want false for exit 44")
		}
	})

	t.Run("a different exit status is a real error", func(t *testing.T) {
		restore := store.SetKeychainRunner(func(cmd *exec.Cmd) ([]byte, error) {
			return nil, exitErrorWithCode(t, 51) // e.g. authorization denied
		})
		t.Cleanup(restore)

		k := store.NewKeychain("someone")
		ok, err := k.Has("envctl/p/A")
		if err == nil {
			t.Fatal("Has swallowed a non-44 exit status as not-found")
		}
		if ok {
			t.Error("Has = true on error")
		}
		if !strings.Contains(err.Error(), "envctl/p/A") {
			t.Errorf("error does not name the service: %v", err)
		}
	})
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

// security(1) without -w answers presence without an authorization prompt,
// which is the whole reason completion may probe the Keychain per variable.
func TestKeychainIsCheapToProbe(t *testing.T) {
	if _, ok := store.Store(store.NewKeychain("u")).(store.CheapPresence); !ok {
		t.Error("Keychain does not implement CheapPresence; completion loses its presence tags on macOS")
	}
}
