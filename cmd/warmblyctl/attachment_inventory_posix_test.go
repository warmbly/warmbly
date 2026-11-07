//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestOrgAttachmentsPreflightRefusesFIFOWithoutWriter(t *testing.T) {
	name := filepath.Join(t.TempDir(), "input.fifo")
	if err := syscall.Mkfifo(name, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- dispatch(t.Context(), []string{"org", "attachments-preflight", "--file", name}) }()
	select {
	case err := <-done:
		if err == nil || err.Error() != "--file must name a regular local archive" {
			t.Fatalf("regular-file refusal: %v", err)
		}
	case <-time.After(time.Second):
		// Release and join the dispatch before reporting a failed bounded wait.
		f, err := os.OpenFile(name, os.O_RDWR|syscall.O_NONBLOCK, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("dispatch did not return after opening the FIFO writer")
		}
		t.Fatal("regular-file refusal must complete without a FIFO writer")
	}
}
