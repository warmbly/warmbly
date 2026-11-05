//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package main

import "os"

func openInventoryFile(path string) (*os.File, error) {
	return os.Open(path)
}
