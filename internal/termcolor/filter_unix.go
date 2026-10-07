//go:build unix

package termcolor

import (
	"time"

	"golang.org/x/sys/unix"
)

// readable reports whether fd has input within d.
func readable(fd uintptr, d time.Duration) bool {
	deadline := time.Now().Add(d)
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		ms := int(time.Until(deadline).Milliseconds())
		if ms < 0 {
			ms = 0
		}
		n, err := unix.Poll(fds, ms)
		if err == unix.EINTR {
			continue // the Go runtime's signals interrupt poll
		}
		return err == nil && n > 0 && fds[0].Revents&unix.POLLIN != 0
	}
}
