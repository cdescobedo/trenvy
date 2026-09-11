package store

import (
	"fmt"
	"sync"
)

// Mem is an in-memory Store for tests. Every command test uses it, which is
// what keeps the suite from ever touching a real keyring.
type Mem struct {
	mu    sync.Mutex
	items map[string]string
	calls []string
}

// NewMem returns an empty in-memory store.
func NewMem() *Mem {
	return &Mem{items: map[string]string{}}
}

// Seed adds an item without recording a call, for arranging test state.
func (m *Mem) Seed(service, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[service] = value
}

// Calls returns the operations performed, in order, so a test can assert that
// a code path did not read a value it only needed to probe.
func (m *Mem) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

func (m *Mem) record(op, service string) {
	m.calls = append(m.calls, fmt.Sprintf("%s %s", op, service))
}

func (m *Mem) Has(service string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Has", service)
	_, ok := m.items[service]
	return ok, nil
}

func (m *Mem) Get(service string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Get", service)
	v, ok := m.items[service]
	if !ok {
		return "", fmt.Errorf("%s: %w", service, ErrNotFound)
	}
	return v, nil
}

func (m *Mem) Set(service, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Set", service)
	m.items[service] = value
	return nil
}

func (m *Mem) Delete(service string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record("Delete", service)
	if _, ok := m.items[service]; !ok {
		return fmt.Errorf("%s: %w", service, ErrNotFound)
	}
	delete(m.items, service)
	return nil
}
