package imap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticRawIsExactBoundedPeekAndChecksFolderGeneration(t *testing.T) {
	c, wire := recordingServer(t, nil)
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	appendMessage(t, c, "INBOX", "<exact@test>")
	c.mu.Lock()
	selected, err := c.selectMailbox("INBOX", nil)
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := c.DiagnosticRawMessage(t.Context(), "INBOX", selected.UIDValidity+1, 1, 1024); err == nil || raw != nil {
		t.Fatal("stale UID generation retrieved", err)
	}
	if len(wire.commands("FETCH")) != 0 {
		t.Fatal("stale locator fetched MIME")
	}
	raw, err := c.DiagnosticRawMessage(t.Context(), "INBOX", selected.UIDValidity, 1, 1024)
	if err != nil || !strings.Contains(string(raw), "Message-ID: <exact@test>") {
		t.Fatal("exact retrieval failed", err)
	}
	for _, cmd := range wire.commands("FETCH") {
		if !strings.Contains(cmd, "UID FETCH 1") || !strings.Contains(cmd, "BODY.PEEK[]<0.1025>") {
			t.Fatal("unbounded or nonpeek fetch", cmd)
		}
	}
	if raw, err = c.DiagnosticRawMessage(t.Context(), "INBOX", selected.UIDValidity, 1, 16); err == nil || raw != nil {
		t.Fatal("oversize MIME returned", err)
	}
}

func TestDiagnosticRawMutexWaitHonorsContext(t *testing.T) {
	c := testServer(t, nil)
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := c.DiagnosticRawMessage(ctx, "INBOX", 1, 1, 1024); err == nil || time.Since(start) > time.Second {
		t.Fatal("diagnostic wait exceeded context", err)
	}
}

func TestDiagnosticRawLifecycleWaitHonorsContextWithoutReconnect(t *testing.T) {
	c, wire := recordingServer(t, nil)
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if raw, err := c.DiagnosticRawMessage(ctx, "INBOX", 1, 1, 1024); raw != nil || !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatal("lifecycle wait escaped deadline", err)
	}
	if len(wire.commands("LOGIN")) != 0 || len(wire.commands("FETCH")) != 0 {
		t.Fatal("waiting diagnostic reconnected or retrieved")
	}
}

func TestDiagnosticRawDisconnectedDoesNotReconnect(t *testing.T) {
	c, wire := recordingServer(t, nil)
	start := time.Now()
	if raw, err := c.DiagnosticRawMessage(t.Context(), "INBOX", 1, 1, 1024); raw != nil || err == nil || time.Since(start) > time.Second {
		t.Fatal("disconnected diagnostic reconnected", err)
	}
	if len(wire.commands("LOGIN")) != 0 || len(wire.commands("FETCH")) != 0 {
		t.Fatal("disconnected diagnostic issued commands")
	}
}

func TestDiagnosticRawMissingUIDRemainsUnknown(t *testing.T) {
	c := testServer(t, nil)
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	selected, err := c.selectMailbox("INBOX", nil)
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := c.DiagnosticRawMessage(t.Context(), "INBOX", selected.UIDValidity, 99, 1024); raw != nil || err == nil {
		t.Fatal("missing message returned diagnostic bytes", err)
	}
}
