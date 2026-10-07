//go:build windows

package termcolor

import (
	"io"
	"os"

	"golang.org/x/sys/windows"
)

// enableVT turns on VT processing so the queries reach the terminal.
func enableVT(w io.Writer) func() {
	f, ok := w.(*os.File)
	if !ok {
		return func() {}
	}
	h := windows.Handle(f.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return func() {}
	}
	_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	return func() { _ = windows.SetConsoleMode(h, mode) }
}
