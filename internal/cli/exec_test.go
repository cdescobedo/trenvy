package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecPassesValuesThroughTheEnvironment(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "k3y")

	var captured *exec.Cmd
	h.opts.Run = func(c *exec.Cmd) error { captured = c; return nil }

	if err := h.run("exec", "local", "--", "printenv"); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if captured == nil {
		t.Fatal("no command was run")
	}

	env := map[string]string{}
	for _, kv := range captured.Env {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	if env["APP_URL"] != "http://+:8080" {
		t.Errorf("APP_URL = %q", env["APP_URL"])
	}
	if env["ALPHA_TOKEN"] != "k3y" {
		t.Errorf("ALPHA_TOKEN = %q", env["ALPHA_TOKEN"])
	}
	// The parent environment must survive, or PATH is gone and nothing runs.
	if env["PATH"] == "" {
		t.Error("PATH was dropped from the child environment")
	}
}

// ps shows argv to every user on the machine, so a secret must never be there.
func TestExecPutsNoValueInArgv(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "s3cretvalue")

	var captured *exec.Cmd
	h.opts.Run = func(c *exec.Cmd) error { captured = c; return nil }

	if err := h.run("exec", "local", "--", "echo", "hi"); err != nil {
		t.Fatal(err)
	}
	for _, arg := range captured.Args {
		if strings.Contains(arg, "s3cretvalue") {
			t.Fatalf("a secret appeared in argv: %v", captured.Args)
		}
	}
}

func TestExecWritesNoFile(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "k")
	h.opts.Run = func(*exec.Cmd) error { return nil }

	before := listDir(t, h.root)
	if err := h.run("exec", "local", "--", "true"); err != nil {
		t.Fatal(err)
	}
	if after := listDir(t, h.root); len(after) != len(before) {
		t.Errorf("exec created files: before %v, after %v", before, after)
	}
}

func TestExecRunsInTheWorktree(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "k")
	sub := filepath.Join(h.root, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	h.opts.Dir = sub

	var captured *exec.Cmd
	h.opts.Run = func(c *exec.Cmd) error { captured = c; return nil }
	if err := h.run("exec", "local", "--", "true"); err != nil {
		t.Fatal(err)
	}
	if captured.Dir != sub {
		t.Errorf("Dir = %q, want the invoking directory %q", captured.Dir, sub)
	}
}

func TestExecRequiresACommand(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	if err := h.run("exec", "local"); err == nil {
		t.Fatal("exec ran with no command")
	}
}

func TestExecWarnsAboutAStaleRenderedFile(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "k")
	if err := h.run("render", "docker"); err != nil {
		t.Fatal(err)
	}
	if err := h.run("set", "APP_URL", "http://+:9999"); err != nil {
		t.Fatal(err)
	}

	h.opts.Run = func(*exec.Cmd) error { return nil }
	if err := h.run("exec", "local", "--", "true"); err != nil {
		t.Fatal(err)
	}
	// A stale .env silently overrides what exec injects; this warning is the
	// whole reason the state file records a sum.
	if !strings.Contains(h.errb.String(), "STALE") {
		t.Errorf("no stale warning on stderr:\n%s", h.errb.String())
	}
}

func TestShellRunsTheUsersShell(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "k")
	t.Setenv("SHELL", "/bin/sh")

	var captured *exec.Cmd
	h.opts.Run = func(c *exec.Cmd) error { captured = c; return nil }

	if err := h.run("shell", "local"); err != nil {
		t.Fatalf("shell: %v", err)
	}
	if captured.Path != "/bin/sh" && !strings.HasSuffix(captured.Path, "/sh") {
		t.Errorf("Path = %q, want the user's shell", captured.Path)
	}
	var sawMarker bool
	for _, kv := range captured.Env {
		if strings.HasPrefix(kv, "ENVCTL_TARGET=") {
			sawMarker = true
		}
	}
	if !sawMarker {
		t.Error("the subshell has no ENVCTL_TARGET marker to prompt from")
	}
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
