package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// releaseClaim drops the process-lifetime lock between tests so each test
// starts from an unclaimed pool.
func releaseClaim() {
	if claimedIDFile != nil {
		claimedIDFile.Close()
		claimedIDFile = nil
	}
}

func TestClaimStateID_MintsAndPersists(t *testing.T) {
	t.Cleanup(releaseClaim)
	dir := t.TempDir()

	first, ok := claimStateID(dir)
	if !ok {
		t.Fatal("expected a claim from an empty state dir")
	}
	releaseClaim()

	second, ok := claimStateID(dir)
	if !ok {
		t.Fatal("expected to reclaim the persisted id")
	}
	if first != second {
		t.Fatalf("id changed across boots: %s != %s", first, second)
	}
}

func TestClaimStateID_LockedFileFallsToNext(t *testing.T) {
	t.Cleanup(releaseClaim)
	dir := t.TempDir()

	first, ok := claimStateID(dir)
	if !ok {
		t.Fatal("expected first claim to succeed")
	}
	// The first claim's lock is still held, simulating a sibling replica: a
	// second claim must mint a distinct id instead of reusing the locked one.
	held := claimedIDFile
	claimedIDFile = nil
	defer held.Close()

	second, ok := claimStateID(dir)
	if !ok {
		t.Fatal("expected second claim to mint a new id")
	}
	if first == second {
		t.Fatalf("second replica claimed the locked id %s", first)
	}
}

func TestClaimStateID_SkipsCorruptFile(t *testing.T) {
	t.Cleanup(releaseClaim)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "aaa.id"), []byte("not-a-uuid\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	id, ok := claimStateID(dir)
	if !ok {
		t.Fatal("expected a claim despite the corrupt file")
	}
	if id == uuid.Nil {
		t.Fatal("claimed uuid.Nil")
	}
}

func TestResolveWorkerID_FromStateDir(t *testing.T) {
	t.Cleanup(releaseClaim)
	// t.Setenv (not Unsetenv) so the prior values are restored afterwards;
	// resolveWorkerID treats empty exactly like unset.
	t.Setenv("WORKER_ID", "")
	t.Setenv("WORKER_BIND_IP", "")
	dir := t.TempDir()
	t.Setenv("WORKER_STATE_DIR", dir)

	want := uuid.MustParse("99999999-8888-7777-6666-555555555555")
	if err := os.WriteFile(filepath.Join(dir, want.String()+".id"), []byte(want.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, bind := resolveWorkerID()
	if got != want {
		t.Fatalf("got id %s, want %s", got, want)
	}
	if bind != "default route" {
		t.Fatalf("got bind %q, want %q", bind, "default route")
	}
}
