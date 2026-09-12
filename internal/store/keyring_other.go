//go:build linux || windows

package store

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// MaxValueBytes is the Windows Credential Manager limit on a credential blob.
// It is enforced on Linux too so that a value that works on one machine does
// not mysteriously fail on another.
const MaxValueBytes = 2560

// ErrTooLong is returned by Set when a value exceeds MaxValueBytes.
var ErrTooLong = errors.New("value exceeds the keyring size limit")

// SystemKeyring is the Secret Service on Linux and Credential Manager on
// Windows, both reached through go-keyring's pure-Go implementations.
//
// Unlike the macOS backend this one can implement Has by attempting a read:
// neither platform raises an interactive prompt for a read from an unlocked
// store, so there is nothing to avoid.
type SystemKeyring struct {
	account string
}

// NewSystemKeyring returns a Store backed by the platform keyring.
func NewSystemKeyring(account string) *SystemKeyring {
	return &SystemKeyring{account: account}
}

func (k *SystemKeyring) Has(service string) (bool, error) {
	_, err := keyring.Get(service, k.account)
	if errors.Is(err, keyring.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("keyring lookup for %s: %w", service, err)
	}
	return true, nil
}

func (k *SystemKeyring) Get(service string) (string, error) {
	v, err := keyring.Get(service, k.account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", fmt.Errorf("%s: %w", service, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("keyring read for %s: %w", service, err)
	}
	return v, nil
}

func (k *SystemKeyring) Set(service, value string) error {
	if len(value) > MaxValueBytes {
		// Name the variable and the limit, never the value.
		return fmt.Errorf("%s is %d bytes, over the %d-byte platform limit: %w",
			service, len(value), MaxValueBytes, ErrTooLong)
	}
	if err := keyring.Set(service, k.account, value); err != nil {
		return fmt.Errorf("keyring write for %s: %w", service, err)
	}
	return nil
}

func (k *SystemKeyring) Delete(service string) error {
	err := keyring.Delete(service, k.account)
	if errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("%s: %w", service, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("keyring delete for %s: %w", service, err)
	}
	return nil
}

func openNative(account string) (Store, error) { return NewSystemKeyring(account), nil }
