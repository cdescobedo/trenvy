package store_test

import (
	"testing"

	"github.com/cdescobedo/envctl/internal/store"
)

// A backend claims CheapPresence only if Has is fast and cannot prompt. These
// assertions are the enforcement: marking a backend that decrypts or reads to
// answer Has would make shell completion hang or pop a dialog on every TAB.
func TestMemIsCheapToProbe(t *testing.T) {
	if _, ok := store.Store(store.NewMem()).(store.CheapPresence); !ok {
		t.Error("Mem does not implement CheapPresence; every in-process test loses its presence tags")
	}
}

func TestFileIsNotCheapToProbe(t *testing.T) {
	if _, ok := store.Store(store.NewFile("x", "p")).(store.CheapPresence); ok {
		t.Error("File claims CheapPresence, but its Has scrypt-decrypts the whole file")
	}
}
