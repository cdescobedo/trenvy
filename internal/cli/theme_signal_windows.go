//go:build windows

package cli

// watchSignal does nothing on Windows: there is no SIGUSR2.
func watchSignal(func()) func() { return func() {} }
