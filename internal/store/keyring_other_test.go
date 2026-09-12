//go:build linux || windows

package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/store"
)

func TestSystemKeyringSatisfiesStore(t *testing.T) {
	var _ store.Store = store.NewSystemKeyring("someone")
}

// Windows Credential Manager caps a credential blob at 2560 bytes. Failing
// there with the platform's own opaque error would be a bad experience, so the
// limit is checked up front on every platform that shares this backend.
func TestSystemKeyringRejectsOversizedValues(t *testing.T) {
	k := store.NewSystemKeyring("someone")
	err := k.Set("envctl/p/BIG", strings.Repeat("x", 2561))
	if err == nil {
		t.Fatal("Set accepted a value over the 2560-byte limit")
	}
	if !errors.Is(err, store.ErrTooLong) {
		t.Errorf("err = %v, want ErrTooLong", err)
	}
	if !strings.Contains(err.Error(), "BIG") {
		t.Errorf("error does not name the variable: %v", err)
	}
	if strings.Contains(err.Error(), "xxxx") {
		t.Errorf("error leaked the value: %v", err)
	}
}
