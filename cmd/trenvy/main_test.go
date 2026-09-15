package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/cdescobedo/trenvy/internal/cli"
)

// report is the piece of main that decides trenvy's own exit code and
// whether a diagnostic is printed. This pins the fix for a child process's
// exit code being swallowed: the child's own status must flow through as
// trenvy's, with nothing written to stderr, while every other error keeps the
// "trenvy: <message>" diagnostic and exits 1.
//
// These run REAL children, and wrap the resulting *exec.ExitError in a
// *cli.ChildExitError exactly as exec and shell do -- that wrapper is what
// report() actually receives in production, and is the whole reason it can
// tell this child's own exit status apart from an unrelated subprocess
// failure (e.g. the Keychain backend shelling out to security(1)) elsewhere
// in the chain. A fabricated exec.ExitError around a zero-value
// os.ProcessState reports exit code 0, so it demonstrates only the
// no-diagnostic half and would pass even if the code were hard-wired to
// return zero.
func TestReportSurfacesAChildsExitCodeWithNoDiagnostic(t *testing.T) {
	t.Run("a non-zero exit becomes trenvy's own exit code", func(t *testing.T) {
		err := &cli.ChildExitError{Err: childExiting(t, "7")}
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
	// did exit 255. Shells report 128+signal instead, so trenvy does too.
	t.Run("a signalled child becomes 128+signal, not 255", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("no POSIX signals")
		}
		err := &cli.ChildExitError{Err: childExiting(t, "signal")}
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
	cmd.Env = append(os.Environ(), "TRENVY_TEST_CHILD="+mode)
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
	mode := os.Getenv("TRENVY_TEST_CHILD")
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
	if !strings.Contains(buf.String(), "trenvy: boom") {
		t.Errorf("output = %q, want the trenvy: prefix", buf.String())
	}
}

// A Keychain write shells out to security(1) internally, so its failure also
// unwraps to a genuine *exec.ExitError -- but it is not the exit status of a
// command the user asked trenvy to run via exec or shell, and must not be
// mistaken for one. Only a *cli.ChildExitError, which exec and shell wrap
// around their own child's error, gets the silent-passthrough treatment;
// every other subprocess failure anywhere else in the call chain must still
// print a diagnostic and exit 1, exactly like any other error.
func TestReportPrintsADiagnosticForANonChildSubprocessFailure(t *testing.T) {
	inner := childExiting(t, "1") // a genuine *exec.ExitError, not wrapped in ChildExitError
	err := fmt.Errorf("keychain write for %s: %w", "trenvy/p/TOKEN", inner)

	var buf bytes.Buffer
	code := report(err, &buf)

	if code != 1 {
		t.Errorf("code = %d, want 1 (a non-child subprocess failure must not pass through)", code)
	}
	if !strings.Contains(buf.String(), "trenvy: keychain write for trenvy/p/TOKEN") {
		t.Errorf("report wrote %q, want a trenvy: diagnostic naming the failure", buf.String())
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
