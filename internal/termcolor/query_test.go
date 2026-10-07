package termcolor

import (
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/muesli/cancelreader"
)

func TestQueryReadsARound(t *testing.T) {
	inR, inW, _ := os.Pipe()
	defer inR.Close()
	defer inW.Close()
	var sent strings.Builder
	go func() { time.Sleep(5 * time.Millisecond); io.WriteString(inW, ghosttyMoon) }()
	cr, err := cancelreader.NewReader(inR)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := query(cr, cr.Cancel, &sent, 500*time.Millisecond)
	if err != nil || !rep.Complete() || rep.Background.Hex() != "#222436" {
		t.Fatalf("rep = %+v err = %v", rep, err)
	}
	if sent.String() != string(QueryBytes()) {
		t.Errorf("sent %q", sent.String())
	}
}

// Review Focus 3: a terminal that answers nothing.
func TestQueryGivesUpWithoutStealingInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.File read deadlines do not work on Windows pipes; the startup query there is exercised by hand")
	}
	inR, inW, _ := os.Pipe()
	defer inR.Close()
	defer inW.Close()
	cr, err := cancelreader.NewReader(inR)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	rep, _ := query(cr, cr.Cancel, io.Discard, 100*time.Millisecond)
	if took := time.Since(start); took > 400*time.Millisecond {
		t.Errorf("took %v", took)
	}
	if rep.HasBackground {
		t.Error("silence parsed as a colour")
	}
	// The next keystroke must reach whoever reads next, not the query.
	io.WriteString(inW, "k")
	buf := make([]byte, 4)
	inR.SetReadDeadline(time.Now().Add(time.Second))
	n, err := inR.Read(buf)
	if err != nil || string(buf[:n]) != "k" {
		t.Errorf("after the query, read %q, %v: the keystroke was stolen", buf[:n], err)
	}
}
