package leader

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

func liveDB(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	handle, err := db.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(handle.Pool.Close)
	return handle
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLiveOneProcessLeadsAndTheOtherTakesOverWhenItStops(t *testing.T) {
	handle := liveDB(t)
	role := "leader-test-" + time.Now().Format("150405.000000000")
	var runningA, runningB atomic.Int32
	elector := func(running *atomic.Int32) *Elector {
		e := New(handle, role)
		e.retry, e.ping = 50*time.Millisecond, 50*time.Millisecond
		e.Go("probe", func(ctx context.Context) {
			running.Add(1)
			<-ctx.Done()
			running.Add(-1)
		})
		return e
	}

	ctxA, stopA := context.WithCancel(context.Background())
	a := elector(&runningA)
	doneA := make(chan struct{})
	go func() { a.Run(ctxA); close(doneA) }()
	waitFor(t, "the first process to lead", a.Leading)

	ctxB, stopB := context.WithCancel(context.Background())
	defer stopB()
	b := elector(&runningB)
	go b.Run(ctxB)
	time.Sleep(300 * time.Millisecond)
	if b.Leading() || runningB.Load() != 0 {
		t.Fatal("a second process led while the first held the lock")
	}
	if runningA.Load() != 1 {
		t.Fatalf("the leader runs %d copies of its loop, want 1", runningA.Load())
	}

	stopA()
	<-doneA
	if runningA.Load() != 0 {
		t.Fatal("the old leader's loop outlived its leadership")
	}
	waitFor(t, "the second process to take over", b.Leading)
	waitFor(t, "the new leader's loop to start", func() bool { return runningB.Load() == 1 })
}

func TestLiveALostLockConnectionStopsTheLoops(t *testing.T) {
	handle := liveDB(t)
	e := New(handle, "leader-test-lost-"+time.Now().Format("150405.000000000"))
	e.retry, e.ping = 50*time.Millisecond, 50*time.Millisecond
	var started, stopped atomic.Int32
	e.Go("probe", func(ctx context.Context) {
		started.Add(1)
		<-ctx.Done()
		stopped.Add(1)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.Run(ctx)
	waitFor(t, "leadership", e.Leading)

	if _, err := handle.Pool.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_locks
		WHERE locktype = 'advisory' AND classid::bigint = $1 AND objid::bigint = $2`, int64(lockClass), int64(e.key)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the loop to stop with its lock", func() bool { return stopped.Load() >= 1 })
	// A second start is a new term; Leading alone can still read true from the old one.
	waitFor(t, "leadership to be won back", func() bool { return started.Load() >= 2 && e.Leading() })
}

func TestRunWithNoLoopsReturnsWithoutConnecting(t *testing.T) {
	e := New(nil, "empty")
	done := make(chan struct{})
	go func() { e.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("an elector with nothing to run held on")
	}
}
