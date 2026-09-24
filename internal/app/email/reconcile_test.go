package email

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/repository"
)

func dueIDs(rows []repository.MailboxAssignment) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for _, r := range rows {
		out[r.ID] = true
	}
	return out
}

func TestReconcileShipsChangesAtOnceAndSpreadsTheRest(t *testing.T) {
	w1, w2, dead := uuid.New(), uuid.New(), uuid.New()
	live := func(id uuid.UUID) bool { return id != dead }
	var steady []repository.MailboxAssignment
	for i := 0; i < 200; i++ {
		steady = append(steady, repository.MailboxAssignment{ID: uuid.New(), WorkerID: &w1})
	}
	unplaced := repository.MailboxAssignment{ID: uuid.New()}
	onDead := repository.MailboxAssignment{ID: uuid.New(), WorkerID: &dead}
	rows := append(append([]repository.MailboxAssignment(nil), steady...), unplaced, onDead)

	seen := map[uuid.UUID]reconcileEntry{}
	now := time.Now()
	due := dueIDs(reconcileDue(rows, seen, now, live))
	if len(due) != 2 || !due[unplaced.ID] || !due[onDead.ID] {
		t.Fatalf("first pass shipped %d mailboxes, want only the unplaced one and the one on a dead worker", len(due))
	}

	// A moved mailbox ships on the next tick, not at its turn.
	seen[steady[0].ID] = reconcileEntry{worker: w1, next: now.Add(time.Hour)}
	steady[0].WorkerID = &w2
	rows[0] = steady[0]
	if due := dueIDs(reconcileDue(rows, seen, now.Add(time.Minute), live)); !due[steady[0].ID] {
		t.Fatal("a moved mailbox waited for its turn")
	}

	// Over one interval every steady mailbox gets exactly one turn, never all in one tick.
	shipped, worst := map[uuid.UUID]int{}, 0
	for tick := now; !tick.After(now.Add(reconcileRepublishInterval)); tick = tick.Add(time.Minute) {
		due := reconcileDue(steady[1:], seen, tick, live)
		if len(due) > worst {
			worst = len(due)
		}
		for _, r := range due {
			shipped[r.ID]++
			seen[r.ID] = reconcileEntry{worker: w1, next: tick.Add(reconcileRepublishInterval)}
		}
	}
	if len(shipped) != len(steady)-1 || worst > 40 {
		t.Fatalf("shipped %d of %d, worst tick %d: turns must cover everyone and stay spread", len(shipped), len(steady)-1, worst)
	}

	// A mailbox that is no longer active is forgotten.
	reconcileDue(nil, seen, now, live)
	if len(seen) != 0 {
		t.Fatalf("%d inactive mailboxes remembered", len(seen))
	}
}
