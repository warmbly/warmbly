package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// An address that hard-bounces a workspace's warmup leaves that workspace's
// draw and reply path at once, everyone's after a second workspace, and comes
// back as soon as warmup mail arrives at it again.
func TestLiveWarmupBouncedRecipientLeavesTheDraw(t *testing.T) {
	f := newPartnerOrgFixture(t)
	ctx := context.Background()
	repo := &warmupRepository{db: f.pool}
	other := &partnerOrgFixture{pool: f.pool, sender: f.outside, exec: f.exec}
	t.Cleanup(func() {
		f.exec(`DELETE FROM warmup_tokens WHERE sender_account_id = $1`, f.outside)
		f.exec(`DELETE FROM tasks WHERE email_account_id = $1`, f.outside)
	})

	var outsideEmail, siblingEmail string
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT email FROM email_accounts WHERE id=$1), (SELECT email FROM email_accounts WHERE id=$2)`,
		f.outside, f.sibling).Scan(&outsideEmail, &siblingEmail); err != nil {
		t.Fatal(err)
	}
	sentTo := func(fx *partnerOrgFixture, recipient uuid.UUID) string {
		t.Helper()
		fx.warmed(t, recipient)
		var id string
		if err := f.pool.QueryRow(ctx, `SELECT btrim(sent_message_id, '<>') FROM warmup_tokens
			WHERE sender_account_id=$1 AND recipient_account_id=$2 ORDER BY created_at DESC LIMIT 1`, fx.sender, recipient).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	drawn := func(sender, recipient uuid.UUID) bool {
		t.Helper()
		cands, err := repo.WarmupPartnerCandidates(ctx, "premium", sender)
		if err != nil {
			t.Fatal(err)
		}
		_, ok := candidateByID(cands, recipient)
		return ok
	}
	count := func(sender uuid.UUID) int {
		t.Helper()
		counts, err := repo.WarmupPartnerCounts(ctx, "premium", []uuid.UUID{sender})
		if err != nil {
			t.Fatal(err)
		}
		return counts[sender]
	}
	bounced := func(recipient, org uuid.UUID) bool {
		t.Helper()
		got, err := repo.WarmupRecipientBounced(ctx, recipient, org)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	before := count(f.sender)
	toOutside := sentTo(f, f.outside)
	if ok, err := repo.RecordWarmupBounce(ctx, f.sender, toOutside, "someone-else@elsewhere.test"); err != nil || ok {
		t.Fatalf("a DSN naming another recipient stamped the send: %v %v", ok, err)
	}
	if ok, err := repo.RecordWarmupBounce(ctx, f.outside, toOutside, ""); err != nil || ok {
		t.Fatalf("a DSN in a mailbox that did not send it stamped the send: %v %v", ok, err)
	}
	if ok, err := repo.RecordWarmupBounce(ctx, f.sender, toOutside, outsideEmail); err != nil || !ok {
		t.Fatalf("RecordWarmupBounce = %v %v, want stamped", ok, err)
	}
	if drawn(f.sender, f.outside) || !bounced(f.outside, f.org) {
		t.Fatal("an address that bounced this workspace's warmup is still offered to it")
	}
	if got := count(f.sender); got != before-1 {
		t.Fatalf("partner count = %d, want %d", got, before-1)
	}
	if bounced(f.outside, f.other) {
		t.Fatal("one workspace's bounce took the address out for another")
	}

	// A second workspace's bounce takes the sibling out of everyone's draw.
	if !drawn(f.outside, f.sibling) {
		t.Fatal("fixture: sibling not offered to the outside sender")
	}
	if _, err := repo.RecordWarmupBounce(ctx, f.sender, sentTo(f, f.sibling), siblingEmail); err != nil {
		t.Fatal(err)
	}
	if !drawn(f.outside, f.sibling) {
		t.Fatal("one workspace's bounce took the address out of the pool")
	}
	if _, err := repo.RecordWarmupBounce(ctx, f.outside, sentTo(other, f.sibling), siblingEmail); err != nil {
		t.Fatal(err)
	}
	// A third workspace has no bounce of its own, so only the pool-wide cap can drop it.
	capped, err := repo.capPartnerCandidates(ctx, "premium", []models.WarmupPartnerCandidate{{ID: f.sibling}})
	if err != nil {
		t.Fatal(err)
	}
	if len(capped) != 0 || !bounced(f.sibling, uuid.New()) {
		t.Fatal("an address two workspaces saw bounce is still in the pool")
	}

	// Warmup mail arriving after the bounce brings the address back.
	f.exec(`UPDATE warmup_tokens SET consumed_at = NOW() WHERE btrim(sent_message_id, '<>') = $1`, sentTo(f, f.outside))
	if bounced(f.outside, f.org) || !drawn(f.sender, f.outside) {
		t.Fatal("an address that received warmup since its bounce is still held out")
	}
}
