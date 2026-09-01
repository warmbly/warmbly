package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/google/uuid"
)

// claimedIDFile keeps the flock-ed id file open for the life of the process.
// os.File carries a finalizer that would close the fd (and drop the lock) if
// the file were garbage-collected.
var claimedIDFile *os.File

// claimStateID claims a stable worker identity from the pool of "*.id" files
// under dir, each holding one UUID. A file is claimed by taking a non-blocking
// exclusive flock held until the process exits, so:
//
//   - a recreated container reclaims the UUID its predecessor released, which
//     keeps the Kafka topic subscription and mailbox assignments intact
//   - same-host replicas sharing the volume (compose --scale) each claim a
//     distinct file; shared workers are interchangeable, so replicas swapping
//     files between boots is harmless
//
// When every existing file is locked by a sibling, a new UUID is minted and
// persisted, growing the pool to the replica count.
func claimStateID(dir string) (uuid.UUID, bool) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("WORKER_STATE_DIR %q is not usable (%v), falling back", dir, err)
		return uuid.Nil, false
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("WORKER_STATE_DIR %q is not readable (%v), falling back", dir, err)
		return uuid.Nil, false
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".id") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		if id, ok := tryClaimIDFile(filepath.Join(dir, name)); ok {
			return id, true
		}
	}

	id := uuid.New()
	path := filepath.Join(dir, id.String()+".id")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		log.Printf("cannot persist worker id to %q (%v), falling back", path, err)
		return uuid.Nil, false
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return uuid.Nil, false
	}
	if _, err := fmt.Fprintln(f, id.String()); err != nil {
		f.Close()
		os.Remove(path)
		log.Printf("cannot write worker id to %q (%v), falling back", path, err)
		return uuid.Nil, false
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(path)
		return uuid.Nil, false
	}
	claimedIDFile = f
	return id, true
}

// tryClaimIDFile locks and parses one id file. A held lock means a sibling
// replica owns it; a corrupt file is skipped rather than deleted, since the
// bytes may still matter to whoever wrote them.
func tryClaimIDFile(path string) (uuid.UUID, bool) {
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return uuid.Nil, false
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return uuid.Nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		f.Close()
		return uuid.Nil, false
	}
	id, err := uuid.Parse(strings.TrimSpace(string(raw)))
	if err != nil {
		log.Printf("ignoring state file %q: not a UUID", path)
		f.Close()
		return uuid.Nil, false
	}
	claimedIDFile = f
	return id, true
}
