package store_test

import (
	"errors"
	"testing"

	"github.com/cdescobedo/envctl/internal/store"
)

func TestMemSetGetHasDelete(t *testing.T) {
	m := store.NewMem()
	const svc = "envctl/p/TOKEN"

	if ok, err := m.Has(svc); err != nil || ok {
		t.Fatalf("Has on empty store = (%v, %v), want (false, nil)", ok, err)
	}
	if _, err := m.Get(svc); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get on empty store = %v, want ErrNotFound", err)
	}

	if err := m.Set(svc, "s3cret"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if ok, err := m.Has(svc); err != nil || !ok {
		t.Fatalf("Has after Set = (%v, %v), want (true, nil)", ok, err)
	}
	got, err := m.Get(svc)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "s3cret" {
		t.Errorf("Get = %q, want s3cret", got)
	}

	if err := m.Delete(svc); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok, _ := m.Has(svc); ok {
		t.Error("Has after Delete = true")
	}
	if err := m.Delete(svc); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Delete of missing = %v, want ErrNotFound", err)
	}
}

// Has must never read the value. On macOS the value read is the call that can
// raise a GUI authorization prompt, so any code path that only needs presence
// must not fall through to Get. Recording calls lets tests assert that.
func TestMemRecordsCallsSoTestsCanAssertHasDoesNotRead(t *testing.T) {
	m := store.NewMem()
	m.Seed("envctl/p/A", "v")

	if _, err := m.Has("envctl/p/A"); err != nil {
		t.Fatal(err)
	}
	calls := m.Calls()
	if len(calls) != 1 || calls[0] != "Has envctl/p/A" {
		t.Fatalf("Calls() = %v, want exactly one Has", calls)
	}
}

func TestMemSeedDoesNotCountAsACall(t *testing.T) {
	m := store.NewMem()
	m.Seed("envctl/p/A", "v")
	if len(m.Calls()) != 0 {
		t.Errorf("Calls() = %v, want empty after Seed", m.Calls())
	}
}

// Store is the contract every backend must satisfy.
func TestMemSatisfiesStore(t *testing.T) {
	var _ store.Store = store.NewMem()
}
