package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/cdescobedo/trenvy/internal/config"
)

// Backend names.
const (
	BackendKeychain = "keychain"
	BackendSystem   = "system"
	BackendFile     = "file"
	BackendMem      = "mem"
)

// ErrNoPassphrase is returned when the file backend has no passphrase.
var ErrNoPassphrase = errors.New("no passphrase for the file backend")

// DefaultBackend is the platform's native store.
func DefaultBackend() string {
	if runtime.GOOS == "darwin" {
		return BackendKeychain
	}
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		return BackendSystem
	}
	return BackendFile
}

// ResolveBackendName returns the backend name Open would use for flag,
// applying the same precedence Open does: the flag, then TRENVY_BACKEND, then
// the platform default. It exists so callers that only need the name (status,
// for instance) do not have to duplicate that precedence.
func ResolveBackendName(flag string) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("TRENVY_BACKEND"); env != "" {
		return env
	}
	return DefaultBackend()
}

// Open returns the named backend, or the platform default when name is empty.
func Open(name, account string) (Store, error) {
	name = ResolveBackendName(name)

	switch name {
	case BackendMem:
		return NewMem(), nil
	case BackendFile:
		pass := os.Getenv("TRENVY_PASSPHRASE")
		if pass == "" {
			return nil, fmt.Errorf(
				"%w: set TRENVY_PASSPHRASE, or use a platform keyring", ErrNoPassphrase)
		}
		return NewFile(filepath.Join(config.Dir(), "secrets.age"), pass), nil
	case BackendKeychain, BackendSystem:
		return openNative(account)
	default:
		return nil, fmt.Errorf("unknown backend %q; want one of keychain, system, file, mem", name)
	}
}
