package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// report is the piece of main that decides envctl's own exit code and
// whether a diagnostic is printed. This pins the fix for a child process's
// exit code being swallowed: the child's own status must flow through as
// envctl's, with nothing written to stderr, while every other error keeps the
// "envctl: <message>" diagnostic and exits 1.
//
// These run REAL children. A fabricated exec.ExitError around a zero-value
// os.ProcessState reports exit code 0, so it demonstrates only the
// no-diagnostic half and would pass even if the code were hard-wired to
// return zero.
func TestReportSurfacesAChildsExitCodeWithNoDiagnostic(t *testing.T) {
	t.Run("a non-zero exit becomes envctl's own exit code", func(t *testing.T) {
		err := childExiting(t, "7")
		var buf bytes.Buffer
		if code := report(err, &buf); code != 7 {
			t.Errorf("code = %d, want the child's own exit code 7", code)
		}
		if buf.Len() != 0 {
			t.Errorf("report wrote a diagnostic for the child's own exit: %q", buf.String())
		}
	})

	// A signalled child has no exit code at all: ExitCode reports -1, and
	// os.Exit(-1) surfaces as 255, indistinguishable from a child that really
	// did exit 255. Shells report 128+signal instead, so envctl does too.
	t.Run("a signalled child becomes 128+signal, not 255", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("no POSIX signals")
		}
		err := childExiting(t, "signal")
		var buf bytes.Buffer
		code := report(err, &buf)
		if want := 128 + int(syscall.SIGTERM); code != want {
			t.Errorf("code = %d, want %d (128+SIGTERM)", code, want)
		}
		if buf.Len() != 0 {
			t.Errorf("report wrote a diagnostic for a signalled child: %q", buf.String())
		}
	})
}

// childExiting re-runs this test binary as a child that either exits with the
// given status or kills itself, and returns the *exec.ExitError that results.
func childExiting(t *testing.T, mode string) error {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperChildProcess")
	cmd.Env = append(os.Environ(), "ENVCTL_TEST_CHILD="+mode)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("child did not produce an *exec.ExitError: %v", err)
	}
	return ee
}

// TestHelperChildProcess is not a real test: it is the body of the child
// process childExiting spawns, and does nothing unless that variable is set.
func TestHelperChildProcess(t *testing.T) {
	mode := os.Getenv("ENVCTL_TEST_CHILD")
	if mode == "" {
		return
	}
	if mode == "signal" {
		p, err := os.FindProcess(os.Getpid())
		if err != nil {
			os.Exit(1)
		}
		_ = p.Signal(syscall.SIGTERM)
		select {} // the signal lands; never reached
	}
	code, err := strconv.Atoi(mode)
	if err != nil {
		code = 1
	}
	os.Exit(code)
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
