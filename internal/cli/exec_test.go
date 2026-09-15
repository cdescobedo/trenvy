package cli_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/manifest"
)

func TestExecPassesValuesThroughTheEnvironment(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k3y")

	var captured *exec.Cmd
	h.opts.Run = func(c *exec.Cmd) error { captured = c; return nil }

	if err := h.run("exec", "--", "printenv"); err != nil {
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
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "s3cretvalue")

	var captured *exec.Cmd
	h.opts.Run = func(c *exec.Cmd) error { captured = c; return nil }

	if err := h.run("exec", "--", "echo", "hi"); err != nil {
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
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k")
	h.opts.Run = func(*exec.Cmd) error { return nil }

	before := listDir(t, h.root)
	if err := h.run("exec", "--", "true"); err != nil {
		t.Fatal(err)
	}
	if after := listDir(t, h.root); len(after) != len(before) {
		t.Errorf("exec created files: before %v, after %v", before, after)
	}
}

func TestExecRunsInTheWorktree(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k")
	sub := filepath.Join(h.root, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	h.opts.Dir = sub

	var captured *exec.Cmd
	h.opts.Run = func(c *exec.Cmd) error { captured = c; return nil }
	if err := h.run("exec", "--", "true"); err != nil {
		t.Fatal(err)
	}
	if captured.Dir != sub {
		t.Errorf("Dir = %q, want the invoking directory %q", captured.Dir, sub)
	}
}

// Cobra needs the -- separator: without it there is no reliable way to tell a
// flag meant for envctl from one meant for the child, so a missing dash is an
// error rather than a guess.
func TestExecRequiresTheDashSeparator(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	err := h.run("exec", "printenv")
	if err == nil {
		t.Fatal("exec ran a command given without --")
	}
	// A command WAS given here -- it just had no separator -- so "no command
	// given" would be telling the user something they can see is false.
	if !strings.Contains(err.Error(), "a command must follow --") {
		t.Errorf("error misdescribes the problem as a missing command: %v", err)
	}
	if !strings.Contains(err.Error(), "envctl exec -- <command>") {
		t.Errorf("error does not give the usage: %v", err)
	}
}

// A stale .env silently beats an injected environment in frameworks that load
// dotenv files over the process environment, so a rendered file this worktree
// holds that no longer matches the manifest has to be called out.
func TestExecWarnsAboutAStaleRenderedFile(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k")
	if err := h.run("render", "docker"); err != nil {
		t.Fatal(err)
	}
	if err := h.run("set", "APP_URL", "http://+:9999"); err != nil {
		t.Fatal(err)
	}

	h.opts.Run = func(*exec.Cmd) error { return nil }
	if err := h.run("exec", "--", "true"); err != nil {
		t.Fatal(err)
	}
	// A stale .env silently overrides what exec injects; this warning is the
	// whole reason the state file records a sum.
	if !strings.Contains(h.errb.String(), "STALE") {
		t.Errorf("no stale warning on stderr:\n%s", h.errb.String())
	}
}

// The mirror image of the above, reframed for the output axis: exec no longer
// has a target to be "the one being run", so the line that matters now is
// which outputs it looks at. Only a file envctl actually rendered into THIS
// worktree can shadow the injected environment; an output that was never
// rendered here must produce no warning and no file read.
func TestExecDoesNotWarnAboutAnOutputNeverRenderedHere(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\n")
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k")
	// Deliberately no render: the manifest defines "docker", but nothing has
	// ever written src/.env in this worktree.
	if err := h.run("set", "APP_URL", "http://+:9999"); err != nil {
		t.Fatal(err)
	}

	h.opts.Run = func(*exec.Cmd) error { return nil }
	if err := h.run("exec", "--", "true"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.errb.String(), "STALE") {
		t.Errorf("exec warned about an output it never rendered here:\n%s", h.errb.String())
	}
}

// ENVCTL_PASSPHRASE unlocks the file secret backend; a child process has no
// business inheriting it just because it inherits everything else.
func TestExecStripsThePassphraseFromTheChildEnvironment(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k")
	t.Setenv("ENVCTL_PASSPHRASE", "super-secret-passphrase")

	var captured *exec.Cmd
	h.opts.Run = func(c *exec.Cmd) error { captured = c; return nil }

	if err := h.run("exec", "--", "true"); err != nil {
		t.Fatal(err)
	}
	for _, kv := range captured.Env {
		if strings.HasPrefix(kv, "ENVCTL_PASSPHRASE=") {
			t.Errorf("child environment carried ENVCTL_PASSPHRASE: %v", kv)
		}
	}
}

// main.go must be able to tell a child's own exit status apart from an
// envctl failure, so it can propagate the child's code with no "envctl:"
// prefix. That requires exec's error to flow back unwrapped: this pins the
// property main.go's own logic depends on, using a fabricated ExitError (no
// real process is run) as the Run seam's return value.
func TestExecSurfacesTheChildsExitErrorUnwrapped(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k")

	var ps os.ProcessState
	want := &exec.ExitError{ProcessState: &ps}
	h.opts.Run = func(*exec.Cmd) error { return want }

	err := h.run("exec", "--", "false")
	var got *exec.ExitError
	if !errors.As(err, &got) {
		t.Fatalf("err = %v, want an *exec.ExitError", err)
	}
	if got != want {
		t.Error("exec wrapped or replaced the child's ExitError instead of returning it as-is")
	}
}

func TestShellRunsTheUsersShell(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.mem.Seed("envctl/vpb/dev/ALPHA_TOKEN", "k")
	t.Setenv("SHELL", "/bin/sh")

	var captured *exec.Cmd
	h.opts.Run = func(c *exec.Cmd) error { captured = c; return nil }

	if err := h.run("shell"); err != nil {
		t.Fatalf("shell: %v", err)
	}
	if captured.Path != "/bin/sh" && !strings.HasSuffix(captured.Path, "/sh") {
		t.Errorf("Path = %q, want the user's shell", captured.Path)
	}
	// ENVCTL_PROFILE is both the prompt marker and the variable resolve.Profile
	// reads, so envctl run from inside the subshell stays on the profile the
	// subshell was loaded with.
	var gotProfile, gotProject string
	for _, kv := range captured.Env {
		if v, ok := strings.CutPrefix(kv, "ENVCTL_PROFILE="); ok {
			gotProfile = v
		}
		if v, ok := strings.CutPrefix(kv, "ENVCTL_PROJECT="); ok {
			gotProject = v
		}
	}
	if gotProfile != "dev" {
		t.Errorf("ENVCTL_PROFILE = %q, want the active profile", gotProfile)
	}
	if gotProject != "vpb" {
		t.Errorf("ENVCTL_PROJECT = %q, want the project id", gotProject)
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

// An output's include list is about a file's audience; a process wants its
// whole environment. So exec resolves every variable the profile has, with no
// subset applied -- even when every output in the manifest narrows one.
func TestExecLoadsEveryVariableIgnoringOutputSubsets(t *testing.T) {
	h := newHarness(t)
	m, err := manifest.Parse([]byte(`
[project]
id = "fe"
profiles = ["dev"]
default_profile = "dev"
output = "web"
salt = "abc123"

[outputs.web]
format = "json"
path = "public/env.json"
include = ["PUBLIC_*"]

[vars.APP_MODE]
value = "development"

[vars.PUBLIC_API]
value = "/"
`))
	if err != nil {
		t.Fatal(err)
	}
	h.register("fe", m)

	var captured *exec.Cmd
	h.opts.Run = func(c *exec.Cmd) error { captured = c; return nil }

	if err := h.run("exec", "--", "true"); err != nil {
		t.Fatalf("exec: %v", err)
	}
	env := map[string]string{}
	for _, kv := range captured.Env {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	if env["PUBLIC_API"] != "/" {
		t.Errorf("PUBLIC_API = %q, want the resolved value", env["PUBLIC_API"])
	}
	if env["APP_MODE"] != "development" {
		t.Errorf("APP_MODE = %q; exec applied an output's include list to a process",
			env["APP_MODE"])
	}
}

// `envctl exec local -- cmd` was the old spelling. It must not be read as a
// command now that the argument is gone: silently running "local" would be
// the worst possible answer to a stale muscle memory.
func TestExecRejectsAnArgumentBeforeTheDash(t *testing.T) {
	h := newHarness(t)
	h.register("vpb", fixtureManifest(t))
	h.opts.Run = func(*exec.Cmd) error { t.Error("exec ran a command"); return nil }

	err := h.run("exec", "local", "--", "true")

	if err == nil {
		t.Fatal("exec accepted an argument before --")
	}
	if !strings.Contains(err.Error(), "envctl exec -- <command>") {
		t.Errorf("error does not give the new spelling: %v", err)
	}
}

// twoOutputManifest has two file outputs, only one of which any given test
// renders. "zeta" is deliberately neither first in sorted order nor the
// project's default output, so an implementation that checks a single output
// -- the first one, or project.output, as the pre-output code checked the one
// target the invocation named -- cannot accidentally pass.
func twoOutputManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(`
[project]
id = "multi"
profiles = ["dev"]
default_profile = "dev"
output = "alpha"
salt = "abc123"

[outputs.alpha]
format = "dotenv"
path = "src/.env"
include = ["*"]

[outputs.zeta]
format = "json"
path = "public/env.json"
include = ["*"]

[vars.APP_MODE]
value = "development"
`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// exec has no output argument, so there is no longer "the output being run" to
// scope the staleness check to. Every output this worktree actually holds a
// rendered file for can shadow the injected environment, so every one of them
// is checked -- not just the default, and not just the first.
func TestExecWarnsAboutAnyStaleOutputNotJustTheDefault(t *testing.T) {
	h := newHarness(t)
	h.gitInit("src/.env\npublic/env.json\n")
	h.register("multi", twoOutputManifest(t))

	// Render only zeta, then move the manifest ahead of it.
	if err := h.run("render", "zeta"); err != nil {
		t.Fatal(err)
	}
	if err := h.run("set", "APP_MODE", "production"); err != nil {
		t.Fatal(err)
	}

	h.opts.Run = func(*exec.Cmd) error { return nil }
	if err := h.run("exec", "--", "true"); err != nil {
		t.Fatal(err)
	}

	errOut := h.errb.String()
	if !strings.Contains(errOut, "STALE") {
		t.Errorf("no stale warning for a rendered output that is not the default:\n%s", errOut)
	}
	if !strings.Contains(errOut, "public/env.json") {
		t.Errorf("the warning does not name the stale file:\n%s", errOut)
	}
	if !strings.Contains(errOut, "envctl render zeta") {
		t.Errorf("the warning does not name the output to re-render:\n%s", errOut)
	}
	// alpha is the default output but was never rendered here, so there is no
	// file of its to be stale and nothing to say about it.
	if strings.Contains(errOut, "src/.env") {
		t.Errorf("exec warned about an output it never rendered here:\n%s", errOut)
	}
}

// A script run under exec may itself call envctl -- `envctl -p prod exec --
// ./deploy.sh`, where deploy.sh runs `envctl get`. Without the profile in the
// child's environment that nested call silently falls back to
// default_profile and reads a different keyring namespace than the one its
// parent was told to use. Nothing errors; it just reads the wrong secret.
func TestExecExportsTheActiveProfileToTheChild(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	h.mem.Seed("envctl/vpb/prod/ALPHA_TOKEN", "k")

	var captured *exec.Cmd
	h.opts.Run = func(c *exec.Cmd) error { captured = c; return nil }

	if err := h.run("--profile", "prod", "exec", "--", "true"); err != nil {
		t.Fatalf("exec: %v", err)
	}

	var got string
	for _, kv := range captured.Env {
		if v, ok := strings.CutPrefix(kv, "ENVCTL_PROFILE="); ok {
			got = v
		}
	}
	if got != "prod" {
		t.Errorf("ENVCTL_PROFILE = %q, want the profile exec actually resolved under", got)
	}
}

// The inherited value must lose to the resolved one, or `ENVCTL_PROFILE=dev
// envctl -p prod exec -- ...` would hand the child a profile that contradicts
// the values it was given.
func TestExecOverridesAnInheritedProfileInTheChild(t *testing.T) {
	h := newHarness(t)
	m := fixtureManifest(t)
	m.Project.Profiles = []string{"dev", "prod"}
	h.register("vpb", m)
	h.mem.Seed("envctl/vpb/prod/ALPHA_TOKEN", "k")
	t.Setenv("ENVCTL_PROFILE", "dev")

	var captured *exec.Cmd
	h.opts.Run = func(c *exec.Cmd) error { captured = c; return nil }

	if err := h.run("--profile", "prod", "exec", "--", "true"); err != nil {
		t.Fatalf("exec: %v", err)
	}

	// Go's os/exec deduplicates Env on Start, keeping the last occurrence, so
	// the resolved value appended after the inherited one is what the child
	// actually sees. Assert on the last occurrence for the same reason.
	var got string
	for _, kv := range captured.Env {
		if v, ok := strings.CutPrefix(kv, "ENVCTL_PROFILE="); ok {
			got = v
		}
	}
	if got != "prod" {
		t.Errorf("ENVCTL_PROFILE = %q, want the --profile flag to win over the inherited value", got)
	}
}
