package manage

import (
	"errors"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/session"
	"github.com/cdescobedo/trenvy/internal/style"
)

// pathStep is a Model with the add-output panel open on its path step.
func pathStep(path string) Model {
	m := New(Snapshot{Matrix: &session.Matrix{}}, Deps{}, style.Theme{})
	m.push(&addOutputOverlay{step: 2, name: "api", format: "dotenv", f: newField(path, false)})
	return m
}

// git's answer arrives as a message after the Cmd runs. One for a path the
// user has since edited answers a question no longer asked, so it is
// dropped; the panel stays open and nothing is staged.
func TestIgnoredAnswerForAnEditedPathIsDropped(t *testing.T) {
	next, _ := pathStep("b.env").Update(ignoredMsg{path: "a.env", ignored: true})
	m := next.(Model)
	if m.staged.Len() != 0 || len(m.overlays) != 1 {
		t.Errorf("a stale answer acted: staged %v, %d overlays", m.Staged(), len(m.overlays))
	}
	next, _ = m.Update(ignoredMsg{path: "b.env", ignored: true})
	if m = next.(Model); m.staged.Len() != 1 || len(m.overlays) != 0 {
		t.Errorf("the current answer did not stage: staged %v, %d overlays", m.Staged(), len(m.overlays))
	}
}

// A git failure warns the same way an unignored path does: shown, and a
// second enter adds anyway.
func TestIgnoredErrorWarnsAndAllowsAddingAnyway(t *testing.T) {
	next, _ := pathStep("a.env").Update(ignoredMsg{path: "a.env", err: errors.New("git: not found")})
	m := next.(Model)
	if m.staged.Len() != 0 || !strings.Contains(m.msg.text, "could not be checked") || !strings.Contains(m.msg.text, "git: not found") {
		t.Fatalf("staged %v, message %q", m.Staged(), m.msg.text)
	}
	if a := m.top().(*addOutputOverlay); a.hints(&m)[0].desc != "add anyway" {
		t.Errorf("hint = %v", a.hints(&m))
	}
}
