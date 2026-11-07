package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveSyncArrivalBatchDeliversInOrderAndHandsBackUnusedLeases(t *testing.T) {
	d := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, d.Pool)
	r := NewDurableEmailMessageMapRepository(d, arrivalLiveCipher(t, d))
	ctx := t.Context()
	var admitted []string
	for i := range 5 {
		data, pending := arrivalLivePayload(f, "batch-"+string(rune('a'+i)))
		if err := r.AdmitArrival(ctx, data, pending); err != nil {
			t.Fatal(err)
		}
		admitted = append(admitted, data.MessageID)
	}
	// A pass cancelled during its second arrival hands back the three it never reached;
	// the interrupted one keeps its lease, as it did before batching.
	pass, cancel := context.WithCancel(ctx)
	var seen []string
	err := r.DeliverArrivals(pass, func(_ context.Context, _ models.JobEventType, body any) error {
		seen = append(seen, body.(*models.JobEventNewEmail).Message.MessageID)
		if len(seen) == 2 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if len(seen) != 2 || seen[0] != admitted[0] || seen[1] != admitted[1] {
		t.Fatalf("delivered %v, want the two oldest of %v", seen, admitted)
	}
	var leased, left int
	if err := d.QueryRow(ctx, `SELECT count(*) FILTER (WHERE locked_until > now()), count(*) FROM sync_arrival_outbox WHERE email_id=$1`, f.mailbox).Scan(&leased, &left); err != nil {
		t.Fatal(err)
	}
	if leased != 1 || left != 4 {
		t.Fatalf("leased=%d left=%d after the pass ended", leased, left)
	}
	if err := r.DeliverArrivals(ctx, func(_ context.Context, _ models.JobEventType, body any) error {
		seen = append(seen, body.(*models.JobEventNewEmail).Message.MessageID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 5 || seen[2] != admitted[2] || seen[4] != admitted[4] {
		t.Fatalf("delivered %v", seen)
	}
	if err := d.QueryRow(ctx, `SELECT count(*) FROM sync_arrival_outbox WHERE email_id=$1`, f.mailbox).Scan(&left); err != nil || left != 1 {
		t.Fatalf("left=%d err=%v", left, err)
	}
}
