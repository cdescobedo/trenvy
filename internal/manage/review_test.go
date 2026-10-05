package manage_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/manage"
)

func stageTwo(f *fakeDeps) manageModel {
	m := press(newModel(f), "j", "enter", "j", "j", "j", "d") // APP_MODE prod override removed
	m = press(m, "left", "j", "d")                            // DATABASE_URL marked deleted
	return m
}

func TestWOpensAGroupedReviewWithBeforeAndAfter(t *testing.T) {
	m := press(stageTwo(&fakeDeps{}), "w")
	v := m.View()
	for _, want := range []string{"Review", "2", "APP_MODE", "remove prod override", "was production", "DATABASE_URL", "delete variable"} {
		if !strings.Contains(v, want) {
			t.Errorf("review lacks %q:\n%s", want, v)
		}
	}
}

func TestReviewNeverShowsASecretValue(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "enter", "j", "enter")
	m = typeText(m, "s3cret")
	m = press(m, "enter", "w")
	if v := m.View(); strings.Contains(v, "s3cret") || !strings.Contains(v, "store new value (keyring)") {
		t.Errorf("review:\n%s", v)
	}
}

func TestReviewMarksIrreversibleLines(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "d", "w") // delete API_TOKEN (secret, 2 stored)
	v := m.View()
	if !strings.Contains(v, "removes 2 keyring items: dev, prod") || !strings.Contains(v, "cannot be undone") {
		t.Errorf("review:\n%s", v)
	}
}

func TestUInReviewDropsTheLineAndClosesWhenEmpty(t *testing.T) {
	m := press(stageTwo(&fakeDeps{}), "w", "u")
	if len(m.Staged()) != 1 {
		t.Fatalf("staged after one drop = %v", staged(m))
	}
	m = press(m, "u")
	if len(m.Staged()) != 0 || strings.Contains(m.View(), "Review") {
		t.Errorf("review still open with nothing staged:\n%s", m.View())
	}
}

func TestUInReviewSaysWhatDependentsWentWithIt(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "a")
	m = typeText(m, "NEW")
	m = press(m, "enter", "p")
	m = typeText(m, "v")
	m = press(m, "enter", "w", "u") // first line: add variable NEW
	if len(m.Staged()) != 0 || !strings.Contains(m.View(), "1 change that depended on it") {
		t.Errorf("staged %v, view:\n%s", staged(m), m.View())
	}
}

// Spec §3: saving keeps the session, reloads, and keeps the cursor.
func TestEnterSavesStaysAndReloads(t *testing.T) {
	f := &fakeDeps{}
	m := press(stageTwo(f), "w", "enter")
	if len(f.commits) != 1 || len(f.commits[0]) != 2 {
		t.Fatalf("commits = %v", f.commits)
	}
	if f.reloads != 1 {
		t.Errorf("reloads = %d, want 1", f.reloads)
	}
	if m.Quitting() || len(m.Staged()) != 0 {
		t.Errorf("quitting %v, staged %v", m.Quitting(), staged(m))
	}
	v := m.View()
	if !strings.Contains(v, "saved 2 changes") || strings.Contains(v, "Review") {
		t.Errorf("after save:\n%s", v)
	}
	if !strings.Contains(v, "DATABASE_URL") || !strings.Contains(v, "postgres://") {
		t.Errorf("cursor did not stay on DATABASE_URL:\n%s", v)
	}
}

// Spec §3: a failed commit keeps the whole batch staged and the review
// open with the report, and enter retries all of it.
func TestFailedCommitKeepsEverythingAndRetries(t *testing.T) {
	f := &fakeDeps{commitErr: errors.New("keyring is locked\n  stored 1 of 2 secrets")}
	m := press(stageTwo(f), "w", "enter")
	v := m.View()
	if len(m.Staged()) != 2 || !strings.Contains(v, "keyring is locked") || !strings.Contains(v, "still staged") {
		t.Fatalf("staged %v, view:\n%s", staged(m), v)
	}
	if f.reloads != 1 {
		t.Errorf("reloads = %d, want 1 after a failure too", f.reloads)
	}
	f.commitErr = nil
	m = press(m, "enter")
	if len(f.commits) != 2 || len(f.commits[1]) != 2 || len(m.Staged()) != 0 {
		t.Errorf("retry: commits %v, staged %v", f.commits, staged(m))
	}
}

func TestKeysAreIgnoredWhileSaving(t *testing.T) {
	f := &fakeDeps{}
	m := press(stageTwo(f), "w")
	next, cmd := m.Update(key("enter")) // start the save but do not run it
	m = next.(manageModel)
	m = press(m, "enter", "u", "q")
	if len(m.Staged()) != 2 || m.Quitting() {
		t.Errorf("keys acted while saving: staged %v, quitting %v", staged(m), m.Quitting())
	}
	m = send(m, cmd())
	if len(f.commits) != 1 {
		t.Errorf("commits = %d, want exactly 1", len(f.commits))
	}
}

// Spec Risks: a failed reload blocks staging until one succeeds.
func TestFailedReloadBlocksStagingUntilReloaded(t *testing.T) {
	f := &fakeDeps{reloadErr: errors.New("manifest unreadable")}
	m := press(stageTwo(f), "w", "enter")
	if !strings.Contains(m.View(), "reload failed") {
		t.Fatalf("view:\n%s", m.View())
	}
	if m = press(m, "d"); len(m.Staged()) != 0 || !strings.Contains(m.View(), "can't edit") {
		t.Errorf("staged while stale: %v", staged(m))
	}
	f.reloadErr = nil
	m = press(m, ".", "j", "j", "j", "enter") // Reload from disk comes after Rename
	if m = press(m, "d"); len(m.Staged()) != 1 {
		t.Errorf("still blocked after a good reload: %v", staged(m))
	}
}

// Review Focus 4.
func TestRevealDoesNotSurviveAReload(t *testing.T) {
	f := &fakeDeps{secrets: map[string]string{"API_TOKEN/dev": "tok-dev"}}
	m := press(newModel(f), "enter", "v")
	if !strings.Contains(m.View(), "tok-dev") {
		t.Fatal("setup: reveal failed")
	}
	// Stay on API_TOKEN throughout -- leaving it would hide the value for a
	// different reason. Store staging, save, and the reload must hide dev.
	m = press(m, "j", "enter")
	m = typeText(m, "x")
	m = press(m, "enter", "w", "enter")
	if f.reloads != 1 {
		t.Fatalf("reloads = %d, want 1", f.reloads)
	}
	if strings.Contains(m.View(), "tok-dev") {
		t.Error("a revealed value survived a reload")
	}
}

func TestWWithNothingStagedSaysSo(t *testing.T) {
	if v := press(newModel(&fakeDeps{}), "w").View(); !strings.Contains(v, "nothing to save") {
		t.Errorf("view:\n%s", v)
	}
}

// held runs the commit but returns the reload Cmd unrun, so the window
// between committedMsg and reloadedMsg can be probed.
func held(t *testing.T, f *fakeDeps) (manageModel, func() tea.Msg) {
	t.Helper()
	m := press(stageTwo(f), "w")
	next, cmd := m.Update(key("enter"))
	m = next.(manageModel)
	next, reload := m.Update(cmd())
	if reload == nil {
		t.Fatal("afterCommit returned no reload cmd")
	}
	return next.(manageModel), reload
}

func TestKeysAreIgnoredBetweenCommitAndReload(t *testing.T) {
	for _, tc := range []struct {
		name      string
		commitErr error
		staged    int
	}{{"success", nil, 0}, {"failure", errors.New("keyring is locked"), 2}} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeDeps{commitErr: tc.commitErr}
			m, reload := held(t, f)
			m = press(m, "enter", "d", "u", "q")
			if len(f.commits) != 1 || len(m.Staged()) != tc.staged || m.Quitting() {
				t.Fatalf("keys acted before the reload: commits %d, staged %v, quitting %v", len(f.commits), staged(m), m.Quitting())
			}
			m = send(m, reload())
			f.commitErr = nil
			if m = press(m, "d"); len(m.Staged()) != tc.staged+1 && tc.staged == 0 {
				t.Errorf("keys still blocked after the reload: %v", staged(m))
			}
			if tc.staged == 2 {
				if m = press(m, "w", "enter"); len(f.commits) != 2 {
					t.Errorf("retry blocked after the reload: commits %d", len(f.commits))
				}
			}
		})
	}
}

func TestPromotionReviewNamesTheKeyringItems(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", ".", "enter", "enter", "w") // APP_MODE, Make secret, confirm
	v := m.View()
	if !strings.Contains(v, "moves 3 into keyring: dev, staging, prod") {
		t.Errorf("review:\n%s", v)
	}
}

func TestRevealDoesNotSurviveAFailedReload(t *testing.T) {
	f := &fakeDeps{secrets: map[string]string{"API_TOKEN/dev": "tok-dev"}, reloadErr: errors.New("manifest unreadable")}
	m := press(newModel(f), "enter", "v", "j", "enter")
	m = typeText(m, "x")
	m = press(m, "enter", "w", "enter")
	if f.reloads != 1 || !strings.Contains(m.View(), "reload failed") {
		t.Fatalf("setup: reloads %d, view:\n%s", f.reloads, m.View())
	}
	if strings.Contains(m.View(), "tok-dev") {
		t.Error("a revealed value survived a failed reload")
	}
}

func TestStaleRevealIsDroppedAfterAReload(t *testing.T) {
	f := &fakeDeps{secrets: map[string]string{"API_TOKEN/dev": "tok-dev"}}
	m := press(newModel(f), "enter")
	next, reveal := m.Update(key("v")) // requested, not yet delivered
	m = next.(manageModel)
	if reveal == nil {
		t.Fatal("v returned no cmd")
	}
	m = press(m, "j", "enter")
	m = typeText(m, "x")
	m = press(m, "enter", "w", "enter") // save + reload, back on API_TOKEN
	m = send(m, reveal())
	if strings.Contains(m.View(), "tok-dev") {
		t.Error("a reveal requested before a reload was shown after it")
	}
}

// A variable added as a secret this session has nothing to move: its line
// says it is new, and is not marked irreversible.
func TestReviewDescribesAnAddedSecretAsNew(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "a")
	m = typeText(m, "NEW_KEY")
	m = press(m, "enter", "s")
	m = typeText(m, "k")
	v := press(m, "enter", "w").View()
	if !strings.Contains(v, "new secret") || strings.Contains(v, "moves 0") || strings.Contains(v, "cannot be undone") {
		t.Errorf("review of an added secret:\n%s", v)
	}
	if !strings.Contains(v, "(none)  ->  secret") {
		t.Errorf("add variable line has no before -> after:\n%s", v)
	}
}

// The promotion line is built from what is staged: dropping one profile's
// set-secret means that profile's plain value is erased and nothing stored.
func TestPromotionReviewFollowsTheStagedSecretWrites(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", ".", "enter", "enter", "w") // APP_MODE, Make secret, confirm
	m = press(m, "j", "u")                                             // drop staging's set-secret
	if len(m.Staged()) != 3 {
		t.Fatalf("setup: staged = %v", staged(m))
	}
	v := m.View()
	if !strings.Contains(flat(v), flat("moves 2 into keyring: dev, prod; staging becomes MISSING")) {
		t.Errorf("review after dropping staging's write:\n%s", v)
	}
}

func TestReviewShowsOptionalAndRequiredAsBeforeAndAfter(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", "j", "j", ".", "j", "enter", "w") // LOG_LEVEL, Mark required
	if v := m.View(); !strings.Contains(v, "optional  ->  required") {
		t.Errorf("review:\n%s", v)
	}
	m = press(newModel(&fakeDeps{}), "j", "j", ".", "j", "enter", "w") // DATABASE_URL, Mark optional
	if v := m.View(); !strings.Contains(v, "required  ->  optional") {
		t.Errorf("review:\n%s", v)
	}
}

func TestReviewShowsAnAddedPlainVariableAsBeforeAndAfter(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "a")
	m = typeText(m, "NEW")
	m = press(m, "enter", "p")
	m = typeText(m, "v")
	if v := press(m, "enter", "w").View(); !strings.Contains(v, "(none)  ->  plain") {
		t.Errorf("review:\n%s", v)
	}
}

// A save that lands followed by a reload that fails must not lose the
// fact that the save landed.
func TestAFailedReloadAfterASaveStillSaysItSaved(t *testing.T) {
	f := &fakeDeps{reloadErr: errors.New("manifest unreadable")}
	v := press(stageTwo(f), "w", "enter").View()
	if !strings.Contains(v, "saved 2 changes") || !strings.Contains(v, "reload failed") {
		t.Errorf("message after save then failed reload:\n%s", v)
	}
}

// ctrl+c while saving quits at once, but the commit may already be partway
// through: the hint must not suggest that quitting writes nothing.
func TestTheSavingHintSaysQuittingMayLeaveItPartlyApplied(t *testing.T) {
	m := press(stageTwo(&fakeDeps{}), "w")
	next, _ := m.Update(key("enter")) // saving, the commit not yet run
	if v := next.(manageModel).View(); !strings.Contains(v, "partly applied") {
		t.Errorf("saving footer:\n%s", v)
	}
}

// A batch the manifest refuses wrote nothing, and retrying it unchanged
// cannot succeed: the review must not offer enter to retry.
func TestARefusedBatchDoesNotOfferARetry(t *testing.T) {
	f := &fakeDeps{commitErr: fmt.Errorf("%w", refused{})}
	v := press(stageTwo(f), "w", "enter").View()
	if !strings.Contains(flat(v), flat("nothing was written -- u to drop the change the manifest refuses, or esc to edit")) {
		t.Errorf("refused batch:\n%s", v)
	}
	if strings.Contains(v, "enter to retry") {
		t.Errorf("a refused batch offered a retry:\n%s", v)
	}
	if !strings.Contains(v, "needs value") {
		t.Errorf("the manifest's reason is not shown:\n%s", v)
	}
}

// refused is a commit error the way the cli reports a refused batch: the
// manifest's reason, with manage.ErrNothingWritten reachable.
type refused struct{}

func (refused) Error() string {
	return `var "APP_MODE": needs value, a profile value, secret = true, or optional = true`
}
func (refused) Unwrap() []error { return []error{manage.ErrNothingWritten} }

func TestReviewGroupsVariablesThenOutputs(t *testing.T) {
	m := press(newModel(&fakeDeps{}), "j", "d", "2", "j", ".")
	m = pickMenu(t, m, "Make default")
	m = press(m, "w")
	v := m.View()
	vi, oi := strings.Index(v, "APP_MODE"), strings.Index(v, "output web")
	if vi < 0 || oi < 0 || vi > oi {
		t.Errorf("variables do not come before outputs:\n%s", v)
	}
	if !strings.Contains(v, "make default") || !strings.Contains(v, "local  ->  web") {
		t.Errorf("default line:\n%s", v)
	}
}

func TestReviewDescribesEveryOutputKind(t *testing.T) {
	m := out(newModel(&fakeDeps{ignored: map[string]bool{"api.env": true}}), "a")
	m = typeText(m, "api")
	m = press(m, "enter", "d", "ctrl+u")
	m = typeText(m, "api.env")
	m = press(m, "enter")
	// The cursor is on api after the add; the list is api, local, web.
	m = press(m, "j", "enter", "enter", "j") // local: detail, format row, json
	m = press(m, "left", "j", "d")           // web removed
	m = press(m, "w")
	v := m.View()
	for _, want := range []string{"add output", "(none)  ->  dotenv api.env", "format", "dotenv  ->  json", "remove output", "web.json stays on disk"} {
		if !strings.Contains(v, want) {
			t.Errorf("review lacks %q:\n%s", want, v)
		}
	}
}

func TestFilesLeftInPlaceAreListedInFull(t *testing.T) {
	long := "/Users/someone/workspace/project-one/deep/path/to/the/worktree/web.json"
	f := &fakeDeps{left: []string{long}}
	m := press(newModel(f), "2", "j", "d", "w", "enter")
	v := m.View()
	// The path has no spaces, so it may wrap mid-word: compare with the
	// borders, padding and line breaks taken out.
	flat := strings.NewReplacer("│", "", " ", "", "\n", "").Replace(v)
	if !strings.Contains(flat, long) || !strings.Contains(v, "left on disk") {
		t.Errorf("view:\n%s", v)
	}
}

func TestReloadFailureAfterASaveWithFilesLeftKeepsTheRecovery(t *testing.T) {
	f := &fakeDeps{left: []string{"/a/very/long/path/that/would/fill/the/message/line/web.json"}, reloadErr: errors.New("manifest unreadable")}
	m := sized(manage.New(fixture(), f.deps(), noTheme()), 80, 24)
	m = press(m, "2", "j", "d", "w", "enter")
	if !strings.Contains(m.View(), "Reload from disk") {
		t.Errorf("the recovery instruction was cut:\n%s", m.View())
	}
}

func TestStaleReviewDoesNotOfferARetryItWillRefuse(t *testing.T) {
	f := &fakeDeps{commitErr: errors.New("keyring is locked"), reloadErr: errors.New("manifest unreadable")}
	m := press(newModel(f), "j", "d", "w", "enter")
	if strings.Contains(m.View(), "enter to retry") || !strings.Contains(m.View(), "Reload from disk") {
		t.Errorf("view:\n%s", m.View())
	}
}

func TestTinyReviewCanScrollToTheEndOfAFailure(t *testing.T) {
	f := &fakeDeps{commitErr: errors.New("keyring is locked\n  stored 0 of 1 secrets; the manifest was not saved\n  left in place: /x/y.env")}
	m := sized(manage.New(fixture(), f.deps(), noTheme()), 40, 10)
	m = press(m, "j", "d", "w", "enter")
	for i := 0; i < 10; i++ {
		m = press(m, "j")
	}
	if !strings.Contains(m.View(), "left in place") {
		t.Errorf("the end of the report never came into view:\n%s", m.View())
	}
}
