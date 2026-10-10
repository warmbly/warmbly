//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package main

import "os"

// Refuse storage where POSIX owner-only modes cannot be verified.
func privateRemoteFile(_ os.FileInfo, _ bool) bool { return false }
func remoteNoFollow() int                          { return 0 }
func lockRemoteFile(_ *os.File) (bool, error)      { return false, storageFailure() }
