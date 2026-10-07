//go:build !windows

package cli

import (
	"os"
	"os/signal"
	"syscall"
)

// watchSignal calls onSignal on each SIGUSR2 until stop is called. An
// Omarchy theme hook sends it (spec 2026-10-06 §3).
func watchSignal(onSignal func()) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR2)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				onSignal()
			case <-done:
				return
			}
		}
	}()
	return func() { signal.Stop(ch); close(done) }
}
