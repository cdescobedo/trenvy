package cli_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/store"
)

// completionFixture has both kinds of variable and, among the secrets, one
// stored and one missing -- the four states completion has to distinguish.
func completionFixture(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
default_target = "local"
salt = "abc123"

[targets.local]
dest = "process"
include = ["*"]

[vars.APP_MODE]
value = "development"

[vars.LOG_LEVEL]
value = "info"

[vars.ALPHA_TOKEN]
secret = true

[vars.BETA_TOKEN]
secret = true
`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// completionHarness registers the fixture and stores exactly one of the two
// secrets, leaving BETA_TOKEN missing.
func completionHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.register("vpb", completionFixture(t))
	h.mem.Seed("envctl/vpb/ALPHA_TOKEN", "k")
	return h
}

// complete drives cobra's hidden __complete command, which is the same entry
// point the generated shell script calls. It returns the candidate lines and
// the directive line, so a test can assert on both.
func complete(t *testing.T, h *harness, args ...string) (cands []string, directive string) {
	t.Helper()
	if err := h.run(append([]string{"__complete"}, args...)...); err != nil {
		t.Fatalf("__complete %v: %v", args, err)
	}
	lines := strings.Split(strings.TrimRight(h.out.String(), "\n"), "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, ":") {
			directive = l
			continue
		}
		if l != "" {
			cands = append(cands, l)
		}
	}
	return cands, directive
}

// leakedStderr is whatever completion wrote to stderr beyond cobra's own
// directive diagnostic, which the generated shell script discards.
func leakedStderr(h *harness) string {
	var out []string
	for _, l := range strings.Split(h.errb.String(), "\n") {
		if l == "" || strings.HasPrefix(l, "Completion ended with directive:") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// names strips the description half of each candidate line.
func names(cands []string) []string {
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, strings.SplitN(c, "\t", 2)[0])
	}
	sort.Strings(out)
	return out
}

func equalNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// noFileComp is cobra's ShellCompDirectiveNoFileComp. Every envctl name
// completion must use it: a variable name is never a filename, so a miss has
// to yield nothing rather than the contents of the current directory.
const noFileComp = ":4"

func TestSecretSetCompletesOnlySecretVarNames(t *testing.T) {
	h := completionHarness(t)

	cands, directive := complete(t, h, "secret", "set", "")

	if got := names(cands); !equalNames(got, []string{"ALPHA_TOKEN", "BETA_TOKEN"}) {
		t.Errorf("candidates = %v, want the two secrets only", got)
	}
	if directive != noFileComp {
		t.Errorf("directive = %q, want %q", directive, noFileComp)
	}
}

func TestSecretSetTagsWhichSecretsAreStored(t *testing.T) {
	h := completionHarness(t)

	cands, _ := complete(t, h, "secret", "set", "")

	want := map[string]string{
		"ALPHA_TOKEN": "secret, stored",
		"BETA_TOKEN":  "secret, MISSING",
	}
	for _, c := range cands {
		name, desc, _ := strings.Cut(c, "\t")
		if want[name] != desc {
			t.Errorf("%s described as %q, want %q", name, desc, want[name])
		}
	}
}

func TestSecretRmCompletesOnlySecretVarNames(t *testing.T) {
	h := completionHarness(t)

	cands, _ := complete(t, h, "secret", "rm", "")

	if got := names(cands); !equalNames(got, []string{"ALPHA_TOKEN", "BETA_TOKEN"}) {
		t.Errorf("candidates = %v, want the two secrets only", got)
	}
}

// set refuses to touch a secret, so offering one would be offering an error.
func TestSetCompletesOnlyPlainVarNames(t *testing.T) {
	h := completionHarness(t)

	cands, _ := complete(t, h, "set", "")

	if got := names(cands); !equalNames(got, []string{"APP_MODE", "LOG_LEVEL"}) {
		t.Errorf("candidates = %v, want the two plain vars only", got)
	}
}

func TestUnsetCompletesEveryVarName(t *testing.T) {
	h := completionHarness(t)

	cands, _ := complete(t, h, "unset", "")

	want := []string{"ALPHA_TOKEN", "APP_MODE", "BETA_TOKEN", "LOG_LEVEL"}
	if got := names(cands); !equalNames(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}

func TestCompletionFiltersByThePrefixTyped(t *testing.T) {
	h := completionHarness(t)

	cands, _ := complete(t, h, "unset", "LOG")

	if got := names(cands); !equalNames(got, []string{"LOG_LEVEL"}) {
		t.Errorf("candidates = %v, want only the prefix match", got)
	}
}

// set takes NAME then VALUE. A value is the user's to invent; completing it
// from the manifest would be nonsense, and falling through to filenames would
// be worse.
func TestSetOffersNothingForTheValueArgument(t *testing.T) {
	h := completionHarness(t)

	cands, directive := complete(t, h, "set", "APP_MODE", "")

	if len(cands) != 0 {
		t.Errorf("candidates = %v, want none for the value position", cands)
	}
	if directive != noFileComp {
		t.Errorf("directive = %q, want %q", directive, noFileComp)
	}
}

// Pressing TAB outside a project must be silent. A completion function has no
// channel to report on: anything it writes lands in the middle of the user's
// command line.
func TestCompletionOutsideAProjectIsSilent(t *testing.T) {
	h := newHarness(t)

	cands, directive := complete(t, h, "secret", "set", "")

	if len(cands) != 0 {
		t.Errorf("candidates = %v, want none outside a project", cands)
	}
	if directive != noFileComp {
		t.Errorf("directive = %q, want %q", directive, noFileComp)
	}
	if leak := leakedStderr(h); leak != "" {
		t.Errorf("wrote to stderr outside a project: %q", leak)
	}
}

// costlyStore wraps the in-memory store but, being declared outside package
// store, cannot implement store.CheapPresence -- exactly the position the file
// and system backends are in.
type costlyStore struct{ store.Store }

func TestCompletionSkipsPresenceOnACostlyBackend(t *testing.T) {
	h := completionHarness(t)
	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		return costlyStore{h.mem}, nil
	}

	cands, _ := complete(t, h, "secret", "set", "")

	if got := names(cands); !equalNames(got, []string{"ALPHA_TOKEN", "BETA_TOKEN"}) {
		t.Fatalf("candidates = %v, want both secrets", got)
	}
	for _, c := range cands {
		_, desc, _ := strings.Cut(c, "\t")
		if desc != "secret" {
			t.Errorf("%s described as %q, want a bare %q", c, desc, "secret")
		}
	}
	for _, call := range h.mem.Calls() {
		t.Errorf("completion probed a costly backend: %s", call)
	}
}

// A backend that cannot be opened at all -- the file backend with no
// passphrase -- must degrade to names, not to an error in the shell.
func TestCompletionSurvivesAnUnopenableBackend(t *testing.T) {
	h := completionHarness(t)
	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		return nil, store.ErrNoPassphrase
	}

	cands, directive := complete(t, h, "secret", "set", "")

	if got := names(cands); !equalNames(got, []string{"ALPHA_TOKEN", "BETA_TOKEN"}) {
		t.Errorf("candidates = %v, want both secrets", got)
	}
	if directive != noFileComp {
		t.Errorf("directive = %q, want %q", directive, noFileComp)
	}
	if leak := leakedStderr(h); leak != "" {
		t.Errorf("wrote to stderr on an unopenable backend: %q", leak)
	}
}

// Presence, never value. A secret must not be read to be offered.
func TestCompletionNeverReadsASecretValue(t *testing.T) {
	h := completionHarness(t)

	complete(t, h, "secret", "set", "")

	for _, call := range h.mem.Calls() {
		if !strings.HasPrefix(call, "Has ") {
			t.Errorf("completion made a non-presence call: %s", call)
		}
	}
}

// resolveApp deliberately never opens a store, so that a project with no
// secrets works on a backend that cannot be opened. Completing a plain-only
// command must hold that line: there is no presence to report, so there is no
// reason to pay for a backend.
func TestPlainCompletionNeverOpensAStore(t *testing.T) {
	h := completionHarness(t)
	opened := false
	h.opts.NewStore = func(*config.Config, string) (store.Store, error) {
		opened = true
		return h.mem, nil
	}

	cands, _ := complete(t, h, "set", "")

	if got := names(cands); !equalNames(got, []string{"APP_MODE", "LOG_LEVEL"}) {
		t.Fatalf("candidates = %v, want the two plain vars", got)
	}
	if opened {
		t.Error("completing plain var names opened the secret store")
	}
}
