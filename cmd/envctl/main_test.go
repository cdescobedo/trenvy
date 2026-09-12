package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// report is the piece of main that decides envctl's own exit code and
// whether a diagnostic is printed. This pins the fix for a child process's
// exit code being swallowed: a fabricated *exec.ExitError (no real process is
// run) must flow through as the process's own exit code, with nothing
// written to stderr, while every other error keeps the "envctl: <message>"
// diagnostic and exits 1.
func TestReportSurfacesAChildsExitCodeWithNoDiagnostic(t *testing.T) {
	var ps os.ProcessState
	ee := &exec.ExitError{ProcessState: &ps}

	var buf bytes.Buffer
	code := report(ee, &buf)

	if code != ee.ExitCode() {
		t.Errorf("code = %d, want the child's own exit code %d", code, ee.ExitCode())
	}
	if buf.Len() != 0 {
		t.Errorf("report wrote a diagnostic for the child's own exit: %q", buf.String())
	}
}

func TestReportPrefixesOtherErrorsAndExitsOne(t *testing.T) {
	var buf bytes.Buffer
	code := report(errors.New("boom"), &buf)

	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "envctl: boom") {
		t.Errorf("output = %q, want the envctl: prefix", buf.String())
	}
}

func TestReportOnSuccessWritesNothingAndExitsZero(t *testing.T) {
	var buf bytes.Buffer
	code := report(nil, &buf)

	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if buf.Len() != 0 {
		t.Errorf("report wrote a diagnostic on success: %q", buf.String())
	}
}
