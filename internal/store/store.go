// Package store holds secret values behind one small interface, with an
// implementation per platform plus an in-memory fake for tests.
package store

import "errors"

// ErrNotFound is returned by Get and Delete when no item exists.
var ErrNotFound = errors.New("secret not found")

// MaxValueBytes is the Windows Credential Manager limit on a credential blob.
// It is enforced on every backend, not just Windows, so that a value that
// works on one machine does not mysteriously fail on a teammate's.
const MaxValueBytes = 2560

// ErrTooLong is returned by Set when a value exceeds MaxValueBytes.
var ErrTooLong = errors.New("value exceeds the keyring size limit")

// Store is a keyed secret store. Services are full names rendered from the
// configured pattern, for example "trenvy/demo-backend/prod/ALPHA_TOKEN".
//
// Has is separate from Get on purpose. On macOS, reading a value can raise a
// GUI authorization prompt while a presence check cannot, so every code path
// that only needs to know whether a secret exists must be able to ask without
// paying that cost. An interface with only Get would make the status table and
// the TUI cost one prompt per secret on every run.
type Store interface {
	Has(service string) (bool, error)
	Get(service string) (string, error)
	Set(service, value string) error
	Delete(service string) error
}

// CheapPresence is a Store whose Has is fast and cannot raise a prompt, so a
// caller on a latency budget may probe every variable it knows about. Shell
// completion is that caller: it runs on every TAB, and a backend that takes a
// second or opens a dialog to answer "does this exist?" would make completion
// worse than useless.
//
// The method is unexported, so only a backend in this package can make the
// claim, and making it is a deliberate act rather than an accident of shape.
// Keychain qualifies: security(1) without -w answers presence silently. Mem
// qualifies: it is a map. File does not: its Has scrypt-decrypts the whole
// store. SystemKeyring does not: go-keyring answers Has with a read, which
// can raise an unlock prompt.
type CheapPresence interface {
	Store
	cheapPresence()
}
