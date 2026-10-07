//go:build !windows

package termcolor

import "io"

func enableVT(io.Writer) func() { return func() {} }
