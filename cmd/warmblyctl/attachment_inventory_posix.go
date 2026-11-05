//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package main

import (
	"os"
	"syscall"
)

func openInventoryFile(path string) (*os.File, error) {
	// Keep descriptor verification reachable if the path changes to a FIFO.
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
