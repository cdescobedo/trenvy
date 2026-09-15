package cli_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/store"
)

func TestProjectInitRegistersTheProject(t *testing.T) {
	h := newHarness(t)

	if err := h.run("project", "init", "--id", "demo"); err != nil {
		t.Fatalf("project init: %v", err)
	}

	reg, err := config.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Lookup(h.root); !ok {
		t.Error("the project was not registered")
	}
}

// init moved under project; leaving a working top-level alias would leave two
// spellings of one command in --help forever.
func TestTopLevelInitIsGone(t *testing.T) {
	h := newHarness(t)

	err := h.run("init", "--id", "demo")

	if err == nil {
		t.Fatal("top-level init still works")
	}
}

func TestProjectListShowsRegisteredProjects(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	if err := h.run("project", "list"); err != nil {
		t.Fatalf("project list: %v", err)
	}

	out := h.out.String()
	if !strings.Contains(out, "vpb") || !strings.Contains(out, h.root) {
		t.Errorf("output does not list the project and its root:\n%s", out)
	}
}

func TestProjectListOnAnEmptyRegistry(t *testing.T) {
	h := newHarness(t)

	if err := h.run("project", "list"); err != nil {
		t.Fatalf("project list: %v", err)
	}
	if !strings.Contains(h.out.String(), "trenvy project init") {
		t.Errorf("output does not suggest how to register one:\n%s", h.out)
	}
}

func TestProjectMoveUpdatesTheRegistryAndTheRenderState(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	st, err := config.LoadState("vpb")
	if err != nil {
		t.Fatal(err)
	}
	st.Record(config.RenderEntry{
		Worktree: filepath.Join(h.root, "main"), Output: "app", Path: "src/.env",
	})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	newRoot := t.TempDir()
	if err := h.run("project", "move", "vpb", newRoot); err != nil {
		t.Fatalf("project move: %v", err)
	}

	reg, err := config.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	e, _ := reg.Find("vpb")
	if e.Root != newRoot {
		t.Errorf("root = %q, want %q", e.Root, newRoot)
	}

	after, err := config.LoadState("vpb")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.Find(filepath.Join(newRoot, "main"), "app"); !ok {
		t.Error("the render record was not rebased onto the new root")
	}
}

func TestProjectMoveRejectsARootThatDoesNotExist(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	err := h.run("project", "move", "vpb", filepath.Join(t.TempDir(), "nope"))

	if err == nil {
		t.Fatal("project move accepted a nonexistent root")
	}
}

// A path that exists but is a plain file, not a directory, must be rejected
// by the IsDir check specifically -- TestProjectMoveRejectsARootThatDoesNotExist
// only reaches os.Stat's not-exist error and never exercises IsDir at all.
func TestProjectMoveRejectsARootThatIsAFile(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := h.run("project", "move", "vpb", filePath)

	if err == nil {
		t.Fatal("project move accepted a root that is a plain file")
	}
}

func TestProjectMoveRejectsAnUnknownProject(t *testing.T) {
	h := newHarness(t)

	err := h.run("project", "move", "nope", t.TempDir())

	if err == nil {
		t.Fatal("project move accepted an unknown id")
	}
	// Pinned to the friendlier message, not just any error: SetRoot's own
	// internal "no project registered" guard would also make err non-nil,
	// so a bare err != nil check cannot tell the command's early, more
	// helpful check apart from that fallback.
	if !strings.Contains(err.Error(), "trenvy project list") {
		t.Errorf("error does not point at trenvy project list: %v", err)
	}
}

// The old root is left behind on disk by every test above (t.TempDir() is
// not removed mid-test), so this is really asserting on behavior the
// implementation already has -- there is no RED for a missing feature here,
// only for a missing assertion.
func TestProjectMoveWarnsWhenTheOldRootStillExists(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	newRoot := t.TempDir()
	if err := h.run("project", "move", "vpb", newRoot); err != nil {
		t.Fatalf("project move: %v", err)
	}

	stderr := h.errb.String()
	if !strings.Contains(stderr, h.root) {
		t.Errorf("stderr does not mention the old root %q:\n%s", h.root, stderr)
	}
	if !strings.Contains(stderr, "trenvy project init") {
		t.Errorf("stderr does not suggest trenvy project init:\n%s", stderr)
	}
}

func TestProjectRmDeletesEverySecretAcrossEveryProfile(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "d")
	h.mem.Seed("trenvy/vpb/prod/ALPHA_TOKEN", "p")

	if err := h.run("project", "rm", "vpb", "--yes"); err != nil {
		t.Fatalf("project rm: %v", err)
	}

	calls := strings.Join(h.mem.Calls(), " ")
	for _, want := range []string{
		"Delete trenvy/vpb/dev/ALPHA_TOKEN",
		"Delete trenvy/vpb/prod/ALPHA_TOKEN",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %q in %v", want, h.mem.Calls())
		}
	}
}

func TestProjectRmClearsTheRegistryManifestAndState(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	// The state file has to exist before the run, or the assertion that it
	// is gone afterwards would hold for a command that never removed it:
	// LoadState invents an empty state rather than writing one, so a project
	// that has never rendered has no state file to begin with.
	seedState(t, h, "vpb")

	if err := h.run("project", "rm", "vpb", "--yes"); err != nil {
		t.Fatalf("project rm: %v", err)
	}

	reg, err := config.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Find("vpb"); ok {
		t.Error("the registry entry survived")
	}
	if _, err := os.Stat(config.ManifestPath("vpb")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the manifest survived: %v", err)
	}
	if _, err := os.Stat(config.StatePath("vpb")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the state file survived: %v", err)
	}
}

// The manifest is the only inventory of which keyring items exist. If a delete
// fails, destroying the manifest would strand the rest with no record of them,
// so nothing else may be touched and the command must be safe to re-run.
//
// This is also what pins the ordering: an implementation that removed the
// manifest, the state file or the registry entry before touching the keyring
// would still return this error, but would fail these assertions.
func TestProjectRmKeepsTheManifestWhenAKeyringDeleteFails(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	// The state file has to exist before the run for the assertion that it
	// survived to mean anything: LoadState invents an empty state rather than
	// writing one, so without this os.Stat reports ErrNotExist either way and
	// hoisting the state removal above the keyring loop would go unnoticed.
	seedState(t, h, "vpb")
	attempts := 0
	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		return failingDeleteStore{Store: h.mem, attempts: &attempts}, nil
	}

	err := h.run("project", "rm", "vpb", "--yes")
	if err == nil {
		t.Fatal("project rm succeeded despite a failing keyring delete")
	}
	// Without these two, the assertions below would also hold for a command
	// that failed before it ever reached the keyring -- a missing flag, an
	// unknown subcommand -- and the test would pin nothing.
	if attempts == 0 {
		t.Error("no keyring delete was attempted")
	}
	if !strings.Contains(err.Error(), "trenvy/vpb/dev/ALPHA_TOKEN") {
		t.Errorf("error does not name the item that could not be deleted: %v", err)
	}

	if _, err := os.Stat(config.ManifestPath("vpb")); err != nil {
		t.Errorf("the manifest was destroyed after a failed delete: %v", err)
	}
	if _, err := os.Stat(config.StatePath("vpb")); err != nil {
		t.Errorf("the state file was destroyed after a failed delete: %v", err)
	}
	reg, err := config.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Find("vpb"); !ok {
		t.Error("the registry entry was removed after a failed delete")
	}
}

// seedState writes a real state file for id, so that an assertion about the
// state file surviving or being removed is about the command's behavior and
// not about a file that was never there.
func seedState(t *testing.T, h *harness, id string) {
	t.Helper()
	st, err := config.LoadState(id)
	if err != nil {
		t.Fatal(err)
	}
	st.Record(config.RenderEntry{
		Worktree: filepath.Join(h.root, "main"), Output: "docker", Path: "src/.env",
	})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config.StatePath(id)); err != nil {
		t.Fatalf("arranging the state file: %v", err)
	}
}

// A delete that fails partway through has already destroyed the items before
// it, permanently. Saying "nothing else was removed" there would tell someone
// their secrets are intact when some of them are gone for good, and is exactly
// the kind of false reassurance that stops a user re-running the command.
func TestProjectRmSaysWhatWasAlreadyDeletedWhenADeleteFails(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Vars["OTHER_TOKEN"] = manifest.Var{Secret: true}
	h.register("vpb", m)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "a")
	h.mem.Seed("trenvy/vpb/dev/OTHER_TOKEN", "b")
	// secretNames sorts, so ALPHA_TOKEN is deleted before OTHER_TOKEN
	// fails: one item really is gone by the time the error is written.
	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		return failOnStore{Store: h.mem, failOn: "OTHER_TOKEN"}, nil
	}

	err := h.run("project", "rm", "vpb", "--yes")
	if err == nil {
		t.Fatal("project rm succeeded despite a failing keyring delete")
	}
	if strings.Contains(err.Error(), "nothing else was removed") {
		t.Errorf("error claims nothing was removed, but an item was already deleted: %v", err)
	}
	if !strings.Contains(err.Error(), "1 items were already deleted") {
		t.Errorf("error does not say how many items are already gone: %v", err)
	}
	if !strings.Contains(err.Error(), "the manifest, state and registry are untouched") {
		t.Errorf("error does not say what is still intact: %v", err)
	}
}

type failOnStore struct {
	store.Store
	failOn string
}

func (f failOnStore) Delete(service string) error {
	if strings.HasSuffix(service, "/"+f.failOn) {
		return errors.New("keyring is locked")
	}
	return f.Store.Delete(service)
}

// Without --yes and with nothing on stdin -- a CI job, a closed pipe -- the
// read returns io.EOF. That is an abort, not a plumbing error: reporting "EOF"
// would tell the user something broke when in fact nothing was confirmed and
// nothing was touched.
func TestProjectRmAbortsWhenStdinIsEmpty(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	err := h.run("project", "rm", "vpb") // harness stdin is an empty reader
	if err == nil {
		t.Fatal("project rm proceeded with no confirmation at all")
	}
	if !strings.Contains(err.Error(), "aborted") || strings.Contains(err.Error(), "EOF") {
		t.Errorf("error should be an abort, not an EOF failure: %v", err)
	}
	if _, err := os.Stat(config.ManifestPath("vpb")); err != nil {
		t.Errorf("the manifest was removed without a confirmation: %v", err)
	}
}

type failingDeleteStore struct {
	store.Store
	attempts *int
}

func (f failingDeleteStore) Delete(string) error {
	*f.attempts++
	return errors.New("keyring is locked")
}

// A secret that is declared but was never stored is the ordinary case, not a
// failure: most projects declare secrets they only fill in under one profile.
// store.ErrNotFound on an individual item must therefore be tolerated -- and
// the count reported must not claim an item that was never there was deleted.
func TestProjectRmToleratesSecretsThatWereNeverStored(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Vars["OTHER_TOKEN"] = manifest.Var{Secret: true}
	h.register("vpb", m)
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "d") // OTHER_TOKEN deliberately not seeded

	if err := h.run("project", "rm", "vpb", "--yes"); err != nil {
		t.Fatalf("project rm: %v", err)
	}

	calls := strings.Join(h.mem.Calls(), " ")
	if !strings.Contains(calls, "Delete trenvy/vpb/dev/OTHER_TOKEN") {
		t.Errorf("the never-stored secret was not attempted: %v", h.mem.Calls())
	}
	if !strings.Contains(h.out.String(), "1 keyring items deleted") {
		t.Errorf("output claims a count that includes the item that was never stored:\n%s", h.out)
	}
}

// Without --yes the id must be typed in full: y/N is too easy to fire off for
// something with no undo.
func TestProjectRmRequiresTheIdTypedBack(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.opts.Stdin = strings.NewReader("wrong\n")

	if err := h.run("project", "rm", "vpb"); err == nil {
		t.Fatal("project rm proceeded on a mistyped confirmation")
	}
	if _, err := os.Stat(config.ManifestPath("vpb")); err != nil {
		t.Errorf("the manifest was removed despite a mistyped confirmation: %v", err)
	}
	if calls := h.mem.Calls(); len(calls) != 0 {
		t.Errorf("the keyring was touched despite a mistyped confirmation: %v", calls)
	}
}

// The other half of the confirmation: a correctly typed id must go through,
// or "always abort without --yes" would pass the test above.
func TestProjectRmProceedsWhenTheIdIsTypedBack(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.opts.Stdin = strings.NewReader("vpb\n")

	if err := h.run("project", "rm", "vpb"); err != nil {
		t.Fatalf("project rm: %v", err)
	}
	if _, err := os.Stat(config.ManifestPath("vpb")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the manifest survived a confirmed removal: %v", err)
	}
}

// Rendered files live in worktrees and may have been hand-edited, so this
// command never deletes them -- it names them, both before asking and after
// the fact, so nobody is left unaware that they are still on disk.
func TestProjectRmLeavesRenderedFilesInPlaceAndNamesThem(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))

	worktree := filepath.Join(h.root, "main")
	rendered := filepath.Join(worktree, "src", ".env")
	if err := mkdirAll(filepath.Dir(rendered)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rendered, []byte("X=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := config.LoadState("vpb")
	if err != nil {
		t.Fatal(err)
	}
	st.Record(config.RenderEntry{Worktree: worktree, Output: "docker", Path: "src/.env"})
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	h.opts.Stdin = strings.NewReader("vpb\n")
	if err := h.run("project", "rm", "vpb"); err != nil {
		t.Fatalf("project rm: %v", err)
	}

	if _, err := os.Stat(rendered); err != nil {
		t.Errorf("the rendered file was deleted: %v", err)
	}
	if !strings.Contains(h.errb.String(), rendered) {
		t.Errorf("the confirmation prompt does not name the rendered file:\n%s", h.errb)
	}
	if !strings.Contains(h.out.String(), "left in place") {
		t.Errorf("output does not say the rendered files were left:\n%s", h.out)
	}
}

func TestProjectRmRejectsAnUnknownProject(t *testing.T) {
	h := newHarness(t)

	err := h.run("project", "rm", "nope", "--yes")

	if err == nil {
		t.Fatal("project rm accepted an unknown id")
	}
	// Pinned to the friendlier message, not just any error: without the
	// registry check the command would still fail, but on manifest.Load's
	// bare no-such-file, which names a path nobody asked about.
	if !strings.Contains(err.Error(), "trenvy project list") {
		t.Errorf("error does not point at trenvy project list: %v", err)
	}
}

// A project with no secrets must not pay for a store it never uses: the file
// backend errors outright when no passphrase is configured, and a project
// that never stored a secret would then be undeletable.
func TestProjectRmDoesNotOpenTheStoreWithoutSecrets(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	delete(m.Vars, "ALPHA_TOKEN")
	h.register("vpb", m)
	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		return nil, errors.New("no passphrase configured")
	}

	if err := h.run("project", "rm", "vpb", "--yes"); err != nil {
		t.Fatalf("project rm on a project with no secrets: %v", err)
	}
}
