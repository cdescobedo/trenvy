//go:build !darwin && !linux && !windows

package store

import "fmt"

func openNative(string) (Store, error) {
	return nil, fmt.Errorf("no native keyring on this platform; use --backend file")
}
