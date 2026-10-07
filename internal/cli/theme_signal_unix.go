//go:build !windows

package cli

import (
	"os"
	"os/signal"
	"syscall"
)

// watchSignal calls onSignal on each SIGUSR2 until stop is called. An
// Omarchy theme hook sends it (spec 2026-10-06 §3). stop is a fence: it
// waits for the watcher to end, and a signal still waiting when stop is
// called is dropped, so onSignal never runs once stop has returned.
func watchSignal(onSignal func()) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR2)
	done := make(chan struct{})
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		for {
			select {
			case <-ch:
				select {
				case <-done: // select picks at random when both are ready
					return
				default:
				}
				onSignal()
			case <-done:
				return
			}
		}
	}()
	return func() { signal.Stop(ch); close(done); <-ended }
}
