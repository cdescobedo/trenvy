package cli_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/store"
)

// completionFixture has both kinds of variable and, among the secrets, one
// stored and one missing -- the four states completion has to distinguish.
func completionFixture(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
profiles = ["dev"]
default_profile = "dev"
output = "app"
salt = "abc123"

[outputs.app]
format = "dotenv"
path = "src/.env"

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
	h.mem.Seed("trenvy/vpb/dev/ALPHA_TOKEN", "k")
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

// noFileComp is cobra's ShellCompDirectiveNoFileComp. Every trenvy name
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

func TestProfileCompletionOffersDeclaredProfiles(t *testing.T) {
	h := completionHarness(t)
	m := reloadManifest(t, "vpb")
	m.Project.Profiles = []string{"dev", "prod"}
	if err := manifest.Save(config.ManifestPath("vpb"), m); err != nil {
		t.Fatal(err)
	}

	cands, directive := complete(t, h, "profile", "default", "")

	if got := names(cands); !equalNames(got, []string{"dev", "prod"}) {
		t.Errorf("candidates = %v, want both profiles", got)
	}
	if directive != noFileComp {
		t.Errorf("directive = %q, want %q", directive, noFileComp)
	}
}

func TestOutputCompletionOffersDefinedOutputs(t *testing.T) {
	h := completionHarness(t)

	cands, _ := complete(t, h, "render", "")

	if got := names(cands); !equalNames(got, []string{"app"}) {
		t.Errorf("candidates = %v, want the defined output", got)
	}
}

func TestProjectCompletionOffersRegisteredIDs(t *testing.T) {
	h := completionHarness(t)

	cands, _ := complete(t, h, "project", "rm", "")

	if got := names(cands); !equalNames(got, []string{"vpb"}) {
		t.Errorf("candidates = %v, want the registered project", got)
	}
}

// filterDirs is cobra's ShellCompDirectiveFilterDirs. project move's second
// argument is a directory on disk, so its completion hands the choice to the
// shell rather than inventing candidates of its own.
const filterDirs = ":16"

// Prefix filtering is exercised elsewhere for var names (see
// TestCompletionFiltersByThePrefixTyped); these three cover the same guard on
// each of the new completers, so a completer that forgot to check
// strings.HasPrefix cannot slip through just because every earlier test typed
// an empty prefix.
func TestProfileCompletionFiltersByThePrefixTyped(t *testing.T) {
	h := completionHarness(t)

	cands, _ := complete(t, h, "profile", "default", "z")

	if len(cands) != 0 {
		t.Errorf("candidates = %v, want none for a prefix nothing matches", cands)
	}
}

func TestOutputCompletionFiltersByThePrefixTyped(t *testing.T) {
	h := completionHarness(t)

	cands, _ := complete(t, h, "render", "z")

	if len(cands) != 0 {
		t.Errorf("candidates = %v, want none for a prefix nothing matches", cands)
	}
}

func TestProjectCompletionFiltersByThePrefixTyped(t *testing.T) {
	h := completionHarness(t)

	cands, _ := complete(t, h, "project", "rm", "z")

	if len(cands) != 0 {
		t.Errorf("candidates = %v, want none for a prefix nothing matches", cands)
	}
}

// The brief's own output and project tests check candidates only; these two
// pin the directive on the same commands, so a wrong directive on either
// completer -- one that would let the shell fall back to filename completion
// on a miss -- has a test to catch it.
func TestOutputCompletionUsesNoFileComp(t *testing.T) {
	h := completionHarness(t)

	_, directive := complete(t, h, "render", "")

	if directive != noFileComp {
		t.Errorf("directive = %q, want %q", directive, noFileComp)
	}
}

func TestProjectCompletionUsesNoFileComp(t *testing.T) {
	h := completionHarness(t)

	_, directive := complete(t, h, "project", "rm", "")

	if directive != noFileComp {
		t.Errorf("directive = %q, want %q", directive, noFileComp)
	}
}

// Each of the three new completers takes exactly one positional argument (the
// name being completed); past it there is nothing left to offer.
func TestProfileCompletionOffersNothingPastFirstArg(t *testing.T) {
	h := completionHarness(t)

	cands, directive := complete(t, h, "profile", "default", "dev", "")

	if len(cands) != 0 {
		t.Errorf("candidates = %v, want none past the first argument", cands)
	}
	if directive != noFileComp {
		t.Errorf("directive = %q, want %q", directive, noFileComp)
	}
}

func TestOutputCompletionOffersNothingPastFirstArg(t *testing.T) {
	h := completionHarness(t)

	cands, directive := complete(t, h, "render", "app", "")

	if len(cands) != 0 {
		t.Errorf("candidates = %v, want none past the first argument", cands)
	}
	if directive != noFileComp {
		t.Errorf("directive = %q, want %q", directive, noFileComp)
	}
}

func TestProjectCompletionOffersNothingPastFirstArg(t *testing.T) {
	h := completionHarness(t)

	cands, directive := complete(t, h, "project", "rm", "vpb", "")

	if len(cands) != 0 {
		t.Errorf("candidates = %v, want none past the first argument", cands)
	}
	if directive != noFileComp {
		t.Errorf("directive = %q, want %q", directive, noFileComp)
	}
}

// completeProjectIDs must read the registry directly rather than resolveApp:
// project rm and project move name a project by id from anywhere on disk,
// including from outside every registered root -- exactly where resolveApp
// fails. Running completion from a directory that owns no project at all is
// what would catch a regression back to resolveApp.
func TestProjectCompletionWorksOutsideEveryRegisteredRoot(t *testing.T) {
	h := completionHarness(t)
	h.opts.Dir = t.TempDir()

	cands, _ := complete(t, h, "project", "rm", "")

	if got := names(cands); !equalNames(got, []string{"vpb"}) {
		t.Errorf("candidates = %v, want the registered project even from outside its root", got)
	}
}

// project move takes <id> <new-root>. Its first argument completes like
// project rm's; its second is a directory on disk, handed to the shell via
// ShellCompDirectiveFilterDirs rather than completed from the registry.
func TestProjectMoveCompletesIDsForTheFirstArgument(t *testing.T) {
	h := completionHarness(t)

	cands, directive := complete(t, h, "project", "move", "")

	if got := names(cands); !equalNames(got, []string{"vpb"}) {
		t.Errorf("candidates = %v, want the registered project", got)
	}
	if directive != noFileComp {
		t.Errorf("directive = %q, want %q", directive, noFileComp)
	}
}

func TestProjectMoveOffersDirectoriesForTheSecondArgument(t *testing.T) {
	h := completionHarness(t)

	cands, directive := complete(t, h, "project", "move", "vpb", "")

	if len(cands) != 0 {
		t.Errorf("candidates = %v, want none -- directories come from the shell", cands)
	}
	if directive != filterDirs {
		t.Errorf("directive = %q, want %q (FilterDirs)", directive, filterDirs)
	}
}

// The brief wires render, clean and show to completeOutputNames; only render
// has a test above. These two cover the other two commands, so a wiring
// mistake on either -- forgetting the ValidArgsFunction, or wiring the wrong
// completer -- has a test to catch it.
func TestCleanCompletionOffersDefinedOutputs(t *testing.T) {
	h := completionHarness(t)

	cands, directive := complete(t, h, "clean", "")

	if got := names(cands); !equalNames(got, []string{"app"}) {
		t.Errorf("candidates = %v, want the defined output", got)
	}
	if directive != noFileComp {
		t.Errorf("directive = %q, want %q", directive, noFileComp)
	}
}

func TestShowCompletionOffersDefinedOutputs(t *testing.T) {
	h := completionHarness(t)

	cands, directive := complete(t, h, "show", "")

	if got := names(cands); !equalNames(got, []string{"app"}) {
		t.Errorf("candidates = %v, want the defined output", got)
	}
	if directive != noFileComp {
		t.Errorf("directive = %q, want %q", directive, noFileComp)
	}
}

// The -p/--profile persistent flag is registered with completeProfileNames in
// Execute, separately from any subcommand's ValidArgsFunction, so it needs
// its own test: nothing above exercises flag completion at all.
func TestProfileFlagCompletionOffersDeclaredProfiles(t *testing.T) {
	h := completionHarness(t)

	cands, directive := complete(t, h, "get", "--profile", "")

	if got := names(cands); !equalNames(got, []string{"dev"}) {
		t.Errorf("candidates = %v, want the declared profile", got)
	}
	if directive != noFileComp {
		t.Errorf("directive = %q, want %q", directive, noFileComp)
	}
}
