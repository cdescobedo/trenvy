//go:build !unix

package termcolor

import "time"

// readable never waits off Unix: the live filter is not used there (spec
// 2026-10-06 §5), so a cut sequence passes through at once.
func readable(uintptr, time.Duration) bool { return false }
