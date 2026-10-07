//go:build !windows

package cli

import (
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// stop is a fence: once it returns, onSignal never runs again, not even for
// a signal that was already waiting while the last one was handled. A query
// written after the program has quit lands in cooked mode, and the
// terminal's reply echoes at the shell prompt.
func TestWatchSignalStopIsAFence(t *testing.T) {
	for i := 0; i < 20; i++ {
		var stopped atomic.Bool
		var late atomic.Int32
		entered := make(chan struct{}, 4)
		release := make(chan struct{})
		stop := watchSignal(func() {
			if stopped.Load() {
				late.Add(1)
			}
			entered <- struct{}{}
			<-release
		})
		if err := syscall.Kill(os.Getpid(), syscall.SIGUSR2); err != nil {
			t.Fatal(err)
		}
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("the first signal never reached onSignal")
		}
		// A second signal waits in the channel while the first is handled.
		if err := syscall.Kill(os.Getpid(), syscall.SIGUSR2); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
		go func() { time.Sleep(10 * time.Millisecond); close(release) }()
		stop()
		stopped.Store(true)
		time.Sleep(30 * time.Millisecond)
		if n := late.Load(); n != 0 {
			t.Fatalf("round %d: onSignal ran %d times after stop returned", i, n)
		}
	}
}
