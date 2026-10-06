package cli_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/store"
)

func TestEditOpensTheInterface(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	called := false
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) { called = true; return m, nil }
	if err := h.run("edit"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("edit did not open the interface")
	}
}

func TestUiOpensTheInterfaceWithoutAFlag(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	called := false
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) { called = true; return m, nil }
	if err := h.run("ui"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("ui did not open the new interface")
	}
	if err := h.run("ui", "--new"); err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("--new should be rejected as an unknown flag; err = %v", err)
	}
}

// failOnSetStore fails a store for one variable's item, so a test can arrange
// a partial success -- some writes applied, one not -- rather than an
// all-or-nothing failure.
type failOnSetStore struct {
	store.Store
	failOn string
}

func (f failOnSetStore) Set(service, value string) error {
	if strings.HasSuffix(service, "/"+f.failOn) {
		return errors.New("keyring is locked")
	}
	return f.Store.Set(service, value)
}
