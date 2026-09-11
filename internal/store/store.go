// Package store holds secret values behind one small interface, with an
// implementation per platform plus an in-memory fake for tests.
package store

import "errors"

// ErrNotFound is returned by Get and Delete when no item exists.
var ErrNotFound = errors.New("secret not found")

// Store is a keyed secret store. Services are full names rendered from the
// configured pattern, for example "envctl/demo-backend/ALPHA_TOKEN".
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
