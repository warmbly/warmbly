// Verifies that the worker derives the same UUIDv5 from an IP as the install
// script does. If these ever drift, every worker installed on a multi-IP box
// would come up with a different ID than the one the installer registered,
// silently breaking topic subscription and assignment lookups.
//
// The expected UUIDs below were generated with the same command the installer
// runs:
//
//	uuidgen --sha1 --namespace 6ba7b811-9dad-11d1-80b4-00c04fd430c8 --name <ip>
//
// Equivalently:
//
//	python3 -c "import uuid; print(uuid.uuid5(uuid.UUID('6ba7b811-9dad-11d1-80b4-00c04fd430c8'), '<ip>'))"

package main

import (
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestWorkerIDFromIP_Deterministic(t *testing.T) {
	cases := []struct {
		ip   string
		want string
	}{
		// Verified via `uuidgen --sha1 --namespace 6ba7b811-9dad-11d1-80b4-00c04fd430c8 --name <ip>`.
		{"1.2.3.4", "47ffd7ee-a509-5b1e-a3d2-993b6a56fe65"},
		{"127.0.0.1", "9a4a6d7c-f348-5828-8dfc-db6d3d417ef0"},
		{"203.0.113.10", "016a4ab6-01b5-59ed-9fe5-3f4e8254099b"},
		{"192.168.1.1", "4f98548b-3e6a-5a9d-bc92-47359ed6e2d5"},
	}
	for _, tc := range cases {
		t.Run(tc.ip, func(t *testing.T) {
			got := workerIDFromIP(tc.ip).String()
			if got != tc.want {
				t.Fatalf("workerIDFromIP(%q) = %s, want %s", tc.ip, got, tc.want)
			}
		})
	}
}

func TestWorkerIDFromIP_StableAcrossCalls(t *testing.T) {
	a := workerIDFromIP("10.0.0.1")
	b := workerIDFromIP("10.0.0.1")
	if a != b {
		t.Fatalf("non-deterministic: %s != %s", a, b)
	}
}

func TestResolveWorkerID_ExplicitID(t *testing.T) {
	want := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	t.Setenv("WORKER_ID", want.String())
	t.Setenv("WORKER_BIND_IP", "")

	got, bind := resolveWorkerID()
	if got != want {
		t.Fatalf("got id %s, want %s", got, want)
	}
	if bind != "default route" {
		t.Fatalf("got bind %q, want %q", bind, "default route")
	}
}

func TestResolveWorkerID_ExplicitIDWithBind(t *testing.T) {
	want := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	t.Setenv("WORKER_ID", want.String())
	t.Setenv("WORKER_BIND_IP", "1.2.3.4")

	got, bind := resolveWorkerID()
	if got != want {
		t.Fatalf("got id %s, want %s", got, want)
	}
	if bind != "1.2.3.4" {
		t.Fatalf("got bind %q, want %q", bind, "1.2.3.4")
	}
}

func TestResolveWorkerID_FromBindIP(t *testing.T) {
	// Unset WORKER_ID so the bind-IP branch runs.
	os.Unsetenv("WORKER_ID")
	t.Setenv("WORKER_BIND_IP", "1.2.3.4")

	got, bind := resolveWorkerID()
	want := uuid.MustParse("47ffd7ee-a509-5b1e-a3d2-993b6a56fe65")
	if got != want {
		t.Fatalf("got id %s, want %s", got, want)
	}
	if bind != "1.2.3.4" {
		t.Fatalf("got bind %q, want %q", bind, "1.2.3.4")
	}
}
