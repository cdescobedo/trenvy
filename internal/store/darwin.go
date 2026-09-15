//go:build darwin

package store

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrControlChar is returned when a secret value contains control characters
// (newline or carriage return) that would break the security(1) command stream.
var ErrControlChar = errors.New("value contains a control character")

// KeychainKind labels trenvy's items in Keychain Access.app so a human can
// tell at a glance which items the tool owns.
const KeychainKind = "trenvy secret"

// Keychain is the macOS login Keychain, driven through security(1).
//
// It is hand-rolled rather than delegated to a keyring library for two
// reasons. First, a presence check must not prompt: security(1) without -w
// answers "does this exist?" silently, while adding -w can raise a GUI
// authorization prompt, and a library that only exposes Get would make the
// status table cost one prompt per secret. Second, driving security(1)
// directly gives control over -D so items are labelled as trenvy's.
//
// Using the security binary also means no cgo, so the tool cross-compiles
// from any machine.
type Keychain struct {
	account string
}

// NewKeychain returns a Store backed by the login Keychain for account.
func NewKeychain(account string) *Keychain {
	return &Keychain{account: account}
}

// runner is swappable so tests can assert on the command without running it.
type runnerFunc func(*exec.Cmd) ([]byte, error)

var keychainRunner runnerFunc = func(cmd *exec.Cmd) ([]byte, error) {
	return cmd.Output()
}

// SetKeychainRunner replaces the command runner and returns a function that
// restores the previous one. For tests only, and not concurrency-safe.
func SetKeychainRunner(f func(*exec.Cmd) ([]byte, error)) func() {
	prev := keychainRunner
	keychainRunner = f
	return func() { keychainRunner = prev }
}

func (k *Keychain) Has(service string) (bool, error) {
	// No -w: this asks whether the item exists without reading its value, and
	// therefore without the authorization prompt a read can trigger.
	cmd := exec.Command("security", "find-generic-password",
		"-a", k.account, "-s", service)
	if _, err := keychainRunner(cmd); err != nil {
		if isNotFoundExit(err) {
			return false, nil
		}
		return false, fmt.Errorf("keychain lookup for %s: %w", service, err)
	}
	return true, nil
}

// cheapPresence marks Has as safe to call per variable: see the no--w note
// above, and store.CheapPresence.
func (k *Keychain) cheapPresence() {}

func (k *Keychain) Get(service string) (string, error) {
	cmd := exec.Command("security", "find-generic-password",
		"-a", k.account, "-s", service, "-w")
	out, err := keychainRunner(cmd)
	if err != nil {
		if isNotFoundExit(err) {
			return "", fmt.Errorf("%s: %w", service, ErrNotFound)
		}
		return "", fmt.Errorf("keychain read for %s: %w", service, err)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// Set stores or replaces an item.
//
// The value is passed on stdin through security's interactive mode, never as
// an argument: ps exposes argv to every user on the machine, so a secret in
// argv is a secret published to the box for the lifetime of the call.
//
// The value must not contain newlines or carriage returns, as security(1)
// interprets them as command terminators in interactive mode, nor a NUL byte:
// security is a C program reading stdin, so a NUL could truncate the stored
// secret silently, which is worse than the newline case since that fails
// loudly instead.
func (k *Keychain) Set(service, value string) error {
	if strings.ContainsAny(value, "\n\r\x00") {
		return fmt.Errorf("%s: %w", service, ErrControlChar)
	}
	if len(value) > MaxValueBytes {
		// Name the variable and the limit, never the value.
		return fmt.Errorf("%s is %d bytes, over the %d-byte platform limit: %w",
			service, len(value), MaxValueBytes, ErrTooLong)
	}
	script := fmt.Sprintf(
		"add-generic-password -a %s -s %s -D %s -w %s -U\nquit\n",
		shellQuote(k.account), shellQuote(service),
		shellQuote(KeychainKind), shellQuote(value),
	)
	cmd := exec.Command("security", "-i")
	cmd.Stdin = bytes.NewBufferString(script)
	if _, err := keychainRunner(cmd); err != nil {
		return fmt.Errorf("keychain write for %s: %w", service, err)
	}
	return nil
}

func (k *Keychain) Delete(service string) error {
	cmd := exec.Command("security", "delete-generic-password",
		"-a", k.account, "-s", service)
	if _, err := keychainRunner(cmd); err != nil {
		if isNotFoundExit(err) {
			return fmt.Errorf("%s: %w", service, ErrNotFound)
		}
		return fmt.Errorf("keychain delete for %s: %w", service, err)
	}
	return nil
}

// shellQuote wraps a value for security's interactive parser, which uses
// double quotes with backslash escapes.
func shellQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range s {
		if c == '\\' || c == '"' {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	b.WriteByte('"')
	return b.String()
}

// keychainNotFoundExitCode is security(1)'s exit status for "no such item".
// Every other non-zero status (authorization denied, user cancelled, and so
// on) is a real failure and must not be read as the secret being absent.
const keychainNotFoundExitCode = 44

func isNotFoundExit(err error) bool {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	return ee.ExitCode() == keychainNotFoundExitCode
}

func openNative(account string) (Store, error) { return NewKeychain(account), nil }
