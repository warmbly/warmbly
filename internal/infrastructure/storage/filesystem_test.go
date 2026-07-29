package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
)

func TestFilesystemStore_RoundTrip(t *testing.T) {
	s, err := NewFilesystem(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "campaigns/abc/body.txt"
	body := []byte("hello, warmbly")

	if err := s.Put(ctx, key, bytes.NewReader(body), "text/plain"); err != nil {
		t.Fatalf("put: %v", err)
	}

	rc, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body mismatch: got %q want %q", got, body)
	}

	ok, err := s.Has(ctx, key)
	if err != nil || !ok {
		t.Fatalf("Has: %v / %v", ok, err)
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}

	ok, _ = s.Has(ctx, key)
	if ok {
		t.Fatal("key still exists after delete")
	}
}

func TestFilesystemStore_GetMissingIsErrNotFound(t *testing.T) {
	s, err := NewFilesystem(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Get(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFilesystemStore_DeleteMissingIsNoop(t *testing.T) {
	s, err := NewFilesystem(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(context.Background(), "missing"); err != nil {
		t.Fatalf("delete should not error on missing key: %v", err)
	}
}

func TestFilesystemStore_RejectsTraversal(t *testing.T) {
	s, err := NewFilesystem(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(context.Background(), "../escape", bytes.NewReader([]byte("x")), ""); err == nil {
		t.Fatal("expected '..' path component to be rejected")
	}
}

func TestFilesystemStore_PresignedUnsupported(t *testing.T) {
	s, err := NewFilesystem(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.PresignedGetURL(context.Background(), "x", 0)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
}

func TestFilesystemStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFilesystem(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	// Write twice to verify the rename-over works (overwrites cleanly).
	for _, body := range []string{"first", "second"} {
		if err := s.Put(context.Background(), "k", bytes.NewReader([]byte(body)), ""); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	// Confirm no temp files left over.
	matches, _ := filepath.Glob(filepath.Join(dir, ".put-*"))
	if len(matches) != 0 {
		t.Fatalf("leftover temp files: %v", matches)
	}
}

func TestFilesystemStore_Name(t *testing.T) {
	s, err := NewFilesystem(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "filesystem" {
		t.Fatalf("expected 'filesystem', got %q", s.Name())
	}
}
