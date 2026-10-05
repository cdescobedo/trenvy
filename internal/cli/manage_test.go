package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/store"
)

// drive plays keys into m, running each Cmd and feeding its message back,
// the way the tea runtime would -- including the real Deps, so this is the
// seam between the interface and the disk.
func drive(m manage.Model, keys ...tea.Msg) manage.Model {
	m = step(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	for _, k := range keys {
		m = step(m, k)
	}
	return m
}

func step(m manage.Model, msg tea.Msg) manage.Model {
	next, cmd := m.Update(msg)
	m = next.(manage.Model)
	for cmd != nil {
		out := cmd()
		if _, q := out.(tea.QuitMsg); q || out == nil {
			return m
		}
		next, cmd = m.Update(out)
		m = next.(manage.Model)
	}
	return m
}

func r(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

var enter = tea.KeyMsg{Type: tea.KeyEnter}

func TestUiSavesAndStaysOpen(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t)) // ALPHA_TOKEN (secret), APP_URL
	var view string
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		// APP_URL, detail, shared row, type, stage, review, save.
		m = drive(m, r("j"), enter, enter, r("http://+:9090"), enter, r("w"), enter)
		view = m.View()
		if m.Quitting() {
			t.Error("saving ended the session")
		}
		return drive(m, r("q")), nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view, "saved 1 change") {
		t.Errorf("view after save:\n%s", view)
	}
	got, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	if v := got.Vars["APP_URL"]; v.Value == nil || *v.Value != "http://+:9090" {
		t.Errorf("APP_URL = %+v", v)
	}
	if h.out.Len() != 0 {
		t.Errorf("wrote to stdout while the interface owned it: %q", h.out.String())
	}
	if h.errb.Len() != 0 {
		t.Errorf("wrote to stderr while the interface owned the terminal: %q", h.errb.String())
	}
}

// The reload after a save shows what is on disk, not what was staged.
func TestUiReloadShowsDiskState(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		m = drive(m, enter, enter, r("tok"), enter, r("w"), enter) // store ALPHA_TOKEN for dev
		if v := m.View(); !strings.Contains(v, "stored") {
			t.Errorf("after reload ALPHA_TOKEN dev does not read stored:\n%s", v)
		}
		return m, nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatal(err)
	}
	if got, err := h.mem.Get("trenvy/vpb/dev/ALPHA_TOKEN"); err != nil || got != "tok" {
		t.Errorf("keyring = %q, %v", got, err)
	}
}

func TestUiRejectsABadThemeName(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	t.Setenv("TRENVY_THEME", "neon")
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		t.Fatal("the interface started with an invalid TRENVY_THEME")
		return m, nil
	}
	err := h.run("ui")
	if err == nil || !strings.Contains(err.Error(), "TRENVY_THEME") {
		t.Errorf("err = %v", err)
	}
}

// commitStaged's errors state facts only. "everything else staged this
// session is gone" is false here, where the batch stays staged and enter
// retries -- so it must not reach the interface.
func TestUiCommitErrorDoesNotClaimTheSessionIsGone(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		return failOnSetStore{Store: h.mem, failOn: "ALPHA_TOKEN"}, nil
	}
	var view string
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		m = drive(m, enter, enter, r("tok"), enter, r("w"), enter) // store ALPHA_TOKEN for dev
		view = m.View()
		return m, nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view, "keyring is locked") {
		t.Errorf("the failure panel does not show the commit error:\n%s", view)
	}
	if strings.Contains(view, "staged this session is gone") {
		t.Errorf("new interface told the user their session is gone:\n%s", view)
	}
}

// callLog records the presence checks and reads that reach a store.
type callLog struct {
	store.Store
	mu    sync.Mutex
	calls []string // "Has <service>" or "Get <service>"
}

func (c *callLog) add(call string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, call)
}

func (c *callLog) Has(service string) (bool, error) {
	c.add("Has " + service)
	return c.Store.Has(service)
}

func (c *callLog) Get(service string) (string, error) {
	c.add("Get " + service)
	return c.Store.Get(service)
}

func (c *callLog) since(n int) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls[n:]...)
}

// The global constraint "presence comes from Has, never Get", at the seam:
// opening the interface asks the store only Has, and v on one cell reads
// exactly that cell, once.
func TestUiOpensWithHasAndRevealsWithOneGet(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	if err := h.mem.Set("trenvy/vpb/dev/ALPHA_TOKEN", "tok-dev"); err != nil {
		t.Fatal(err)
	}
	log := &callLog{Store: h.mem}
	h.opts.NewStore = func(*config.Config, string) (store.Store, error) { return log, nil }
	var opened, revealed []string
	var view string
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		opened = log.since(0)
		m = drive(m, enter, r("v")) // ALPHA_TOKEN, detail on dev, reveal
		revealed = log.since(len(opened))
		view = m.View()
		return m, nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatal(err)
	}
	if len(opened) == 0 {
		t.Error("opening asked the store nothing; presence was not checked")
	}
	for _, c := range opened {
		if !strings.HasPrefix(c, "Has ") {
			t.Errorf("opening the interface called %s", c)
		}
	}
	if want := []string{"Get trenvy/vpb/dev/ALPHA_TOKEN"}; strings.Join(revealed, ",") != strings.Join(want, ",") {
		t.Errorf("v called %v, want exactly %v", revealed, want)
	}
	if !strings.Contains(view, "tok-dev") {
		t.Errorf("the revealed value is not on screen:\n%s", view)
	}
}

// The Outputs screen's state word and default tag come from the snapshot
// buildSnapshot fills: the quick-mode state `trenvy status` prints, and
// project.output from disk.
func TestUiOutputsShowsDiskStateAndDefault(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t)) // output docker, the default, never rendered
	var view string
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		m = drive(m, r("2"))
		view = m.View()
		return m, nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatal(err)
	}
	var row string
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "docker") {
			row = l
			break
		}
	}
	if !strings.Contains(row, "default") || !strings.Contains(row, "not rendered") {
		t.Errorf("docker row = %q, want default and not rendered:\n%s", row, view)
	}
}

// Adding an output asks git, in the session's worktree, whether the path is
// ignored: a tracked path is warned about and nothing is staged, an ignored
// one is staged and saved.
func TestUiAddOutputAsksGitWhetherThePathIsIgnored(t *testing.T) {
	h := newHarness(t)
	h.gitInit("api.env\n")
	h.register("vpb", fixtureManifest(t))
	var warned string
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		// Outputs, add, name, dotenv, a path git would track.
		m = drive(m, r("2"), r("a"), r("api"), enter, r("d"), r("tracked.env"), enter)
		warned = m.View()
		if n := len(m.Staged()); n != 0 {
			t.Errorf("a tracked path was staged without a second enter: %d changes", n)
		}
		m = drive(m, tea.KeyMsg{Type: tea.KeyEsc}, r("a"), r("api"), enter, r("d"), r("api.env"), enter, r("w"), enter)
		return drive(m, r("q")), nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warned, "tracked.env is not ignored by git") {
		t.Errorf("no warning for a tracked path:\n%s", warned)
	}
	got, err := manifest.Load(config.ManifestPath("vpb"))
	if err != nil {
		t.Fatal(err)
	}
	if o, ok := got.Outputs["api"]; !ok || o.Path != "api.env" || o.Format != manifest.FormatDotenv {
		t.Errorf("outputs = %+v", got.Outputs)
	}
}

// A worktree outside git has nothing git could commit, so GitIgnored counts
// every path as ignored -- the same judgement render's guard makes -- and
// adding an output there takes one enter, with no warning.
func TestUiAddOutputOutsideGitNeedsNoWarning(t *testing.T) {
	h := newHarness(t) // h.root is a fresh temp dir, not a repository
	h.register("vpb", fixtureManifest(t))
	var view string
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		m = drive(m, r("2"), r("a"), r("api"), enter, r("d"), enter)
		view = m.View()
		if got := m.Staged(); len(got) != 1 || got[0].Path != ".env" {
			t.Errorf("one enter did not add the output: %+v", got)
		}
		return m, nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(view, "not ignored") || strings.Contains(view, "could not be checked") {
		t.Errorf("warned outside git:\n%s", view)
	}
}

// Render now through the real Deps: the file lands at 0600. The fixture's
// output holds a secret, so the reload the action triggers reads it as
// unverified (quick mode never reads secrets); Check now, which does, then
// reads it fresh.
func TestUiRenderNowWritesTheFileAndReloadsItsState(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "k3y")
	var rendered, checked string
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		// Outputs, the . menu, down to Render now (after Make default,
		// Rename and the new-variables item), enter.
		m = drive(m, r("2"), r("."), r("j"), r("j"), r("j"), enter)
		rendered = m.View()
		// Reopen the menu: Check now is two items below Render now.
		m = drive(m, r("."), r("j"), r("j"), r("j"), r("j"), r("j"), enter)
		checked = m.View()
		return drive(m, r("q")), nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(h.root, "src", ".env"))
	if err != nil {
		t.Fatalf("render now wrote nothing: %v\n%s", err, rendered)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", fi.Mode().Perm())
	}
	if !strings.Contains(rendered, "unverified") || strings.Contains(rendered, "not rendered") {
		t.Errorf("state after the reload is not the quick-mode one:\n%s", rendered)
	}
	if !strings.Contains(checked, "docker checked: fresh") {
		t.Errorf("Check now did not read the rendered file as fresh:\n%s", checked)
	}
}
