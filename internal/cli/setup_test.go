package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/manifest"
)

func driveSetup(s manage.Setup, keys ...tea.KeyMsg) manage.Setup {
	next, _ := s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	s = next.(manage.Setup)
	for _, k := range keys {
		next, _ = s.Update(k)
		s = next.(manage.Setup)
	}
	return s
}

var esc = tea.KeyMsg{Type: tea.KeyEsc}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// bareLayoutForCLI builds <root>/.bare with worktrees <root>/main and
// <root>/other, the layout this project itself uses.
func bareLayoutForCLI(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	gitIn(t, src, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(src, "README"), "x\n")
	gitIn(t, src, "add", "README")
	gitIn(t, src, "commit", "-qm", "init")
	root := t.TempDir()
	gitIn(t, root, "clone", "-q", "--bare", src, ".bare")
	writeFile(t, filepath.Join(root, ".git"), "gitdir: ./.bare\n")
	gitIn(t, root, "worktree", "add", "-q", "main")
	gitIn(t, root, "worktree", "add", "-q", "-b", "other", "other")
	return root
}

func TestUiOutsideAProjectOpensSetupAndCreatesIt(t *testing.T) {
	h := newHarness(t)
	writeFile(t, filepath.Join(h.root, ".env.example"), "APP_MODE=development\nAPI_TOKEN=\n")
	h.opts.RunSetup = func(s manage.Setup) (manage.Setup, error) {
		return driveSetup(s, enter, enter, enter, enter), nil // project, seed, review, create
	}
	var view string
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
		m = drive(m)
		view = m.View()
		return m, nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatal(err)
	}
	id := filepath.Base(h.root)
	m, err := manifest.Load(config.ManifestPath(id))
	if err != nil {
		t.Fatalf("no manifest for %s: %v", id, err)
	}
	if v := m.Vars["APP_MODE"]; v.Value == nil || *v.Value != "development" {
		t.Errorf("APP_MODE = %+v", v)
	}
	if v := m.Vars["API_TOKEN"]; !v.Secret {
		t.Errorf("API_TOKEN = %+v, want secret", v)
	}
	if !strings.Contains(view, "1 secret to store") {
		t.Errorf("setup should land on Variables with the missing secret:\n%s", view)
	}
}

// Review Focus 4.
func TestCancellingSetupWritesNothing(t *testing.T) {
	h := newHarness(t)
	writeFile(t, filepath.Join(h.root, ".env.example"), "APP_MODE=development\n")
	for _, keys := range [][]tea.KeyMsg{{esc}, {enter, enter, tea.KeyMsg{Type: tea.KeyCtrlC}}} {
		h.opts.RunSetup = func(s manage.Setup) (manage.Setup, error) { return driveSetup(s, keys...), nil }
		h.opts.RunManage = func(m manage.Model) (manage.Model, error) {
			t.Fatal("the interface opened after a cancelled setup")
			return m, nil
		}
		if err := h.run("ui"); err != nil {
			t.Fatal(err)
		}
		if reg, _ := config.LoadRegistry(); len(reg.Projects) != 0 {
			t.Errorf("registry = %+v after a cancel", reg.Projects)
		}
		if !strings.Contains(h.errb.String(), "nothing was written") {
			t.Errorf("stderr = %q", h.errb.String())
		}
	}
}

// Review Focus 3.
func TestSetupSkipsAnUnparsableSample(t *testing.T) {
	h := newHarness(t)
	writeFile(t, filepath.Join(h.root, ".env.example"), "APP_MODE=development\n")
	writeFile(t, filepath.Join(h.root, "appsettings.json.example"), "{ not json")
	var seed string
	h.opts.RunSetup = func(s manage.Setup) (manage.Setup, error) {
		s = driveSetup(s, enter)
		seed = s.View()
		return driveSetup(s, esc, esc), nil
	}
	if err := h.run("ui"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seed, ".env.example") || strings.Contains(seed, "appsettings") || !strings.Contains(seed, "Start empty") {
		t.Errorf("seed step:\n%s", seed)
	}
}

func TestEditOutsideAProjectOpensSetupToo(t *testing.T) {
	h := newHarness(t)
	opened := false
	h.opts.RunSetup = func(s manage.Setup) (manage.Setup, error) { opened = true; return driveSetup(s, esc), nil }
	if err := h.run("edit"); err != nil {
		t.Fatal(err)
	}
	if !opened {
		t.Error("trenvy edit outside a project did not open setup")
	}
}

// Review Focus 1, end to end.
func TestSetupFromAWorktreeIsFoundFromItsSibling(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	h := newHarness(t)
	root := bareLayoutForCLI(t)
	h.opts.Dir = filepath.Join(root, "main")
	h.opts.RunSetup = func(s manage.Setup) (manage.Setup, error) { return driveSetup(s, enter, enter, enter), nil } // project, Start empty, create
	h.opts.RunManage = func(m manage.Model) (manage.Model, error) { return m, nil }
	if err := h.run("ui"); err != nil {
		t.Fatal(err)
	}
	h.opts.Dir = filepath.Join(root, "other")
	h.out.Reset()
	if err := h.run("profile"); err != nil {
		t.Fatalf("the sibling worktree did not find the project: %v", err)
	}
	if !strings.Contains(h.out.String(), "dev") {
		t.Errorf("profile from the sibling = %q", h.out.String())
	}
}
