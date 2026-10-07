package tasks

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Issue #200 end to end against a real Postgres. Skipped unless
// WARMBLY_TEST_DB is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/tasks/ -run Live -v
//
// The pre-send gates in HandleCampaignTask skip a contact WITHOUT recording any
// progress for it. That is only safe while routing agrees the contact is out of
// the running: otherwise the finder hands back the same pair on every tick, the
// gate skips it again, and the campaign never reaches the leads behind it. The
// customer symptom was a campaign that logged "Unverifiable recipient skipped"
// for the same address every few minutes for hours and sent nothing at all.

func (f *sendFixture) markVerification(t *testing.T, contact uuid.UUID, status, reason string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE contacts SET verification_status = $2, verification_reason = $3, verification_checked_at = NOW() WHERE id = $1`,
		contact, status, reason); err != nil {
		t.Fatalf("set verification %s: %v", status, err)
	}
}

func (f *sendFixture) skipLogCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM campaign_logs WHERE campaign_id = $1 AND event_type = 'suppressed'`,
		f.campaign).Scan(&n); err != nil {
		t.Fatalf("count skip logs: %v", err)
	}
	return n
}

// TestLiveUndeliverableLeadDoesNotWedgeCampaign is the issue itself: lead A is
// first in line and unverifiable, lead B is healthy and behind it.
func TestLiveUndeliverableLeadDoesNotWedgeCampaign(t *testing.T) {
	f := newSendFixture(t)
	f.markVerification(t, f.leadA, "invalid",
		"recipient rejected (504): 5.5.2 <localhost>: Helo command rejected: need fully-qualified hostname")

	for i := 0; i < 5; i++ {
		f.tick(t)
	}

	if row := f.progressFor(t, f.leadB); row == nil || row.sentAt == nil {
		t.Fatalf("the healthy lead behind the undeliverable one was never served: %+v", row)
	}
	if row := f.progressFor(t, f.leadA); row != nil && (row.sentAt != nil || row.dispatchedAt != nil) {
		t.Fatalf("the undeliverable lead was sent to anyway: %+v", row)
	}
	if f.sender.count() != 1 {
		t.Fatalf("%d sends went out; exactly the one healthy lead should have been served", f.sender.count())
	}
	// The old behaviour wrote one of these per tick, forever.
	if n := f.skipLogCount(t); n != 0 {
		t.Fatalf("%d repeated skip entries written; routing should drop the lead instead of re-picking it", n)
	}
}

// A 'risky' address is only undeliverable while the campaign's "send to risky
// emails" toggle is off; that gate wedged the campaign the same way.
func TestLiveRiskyLeadWedgesNothingWhenRiskySendingIsOff(t *testing.T) {
	f := newSendFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE campaigns SET risky_emails = false WHERE id = $1`, f.campaign); err != nil {
		t.Fatalf("disable risky sending: %v", err)
	}
	f.markVerification(t, f.leadA, "risky", "catch-all domain; acceptance is not conclusive")

	for i := 0; i < 3; i++ {
		f.tick(t)
	}
	if row := f.progressFor(t, f.leadB); row == nil || row.sentAt == nil {
		t.Fatalf("the healthy lead was never served past the risky one: %+v", row)
	}
	if row := f.progressFor(t, f.leadA); row != nil && row.dispatchedAt != nil {
		t.Fatal("a risky lead was sent to with the toggle off")
	}

	// With the toggle back on the same lead becomes sendable again. The
	// campaign finished once the only routable lead was served, so turning the
	// toggle on goes with resuming it, exactly as it would in the dashboard.
	if _, err := f.pool.Exec(ctx,
		`UPDATE campaigns SET risky_emails = true, status = 'active' WHERE id = $1`, f.campaign); err != nil {
		t.Fatalf("re-enable risky sending: %v", err)
	}
	f.tick(t)
	if row := f.progressFor(t, f.leadA); row == nil || row.sentAt == nil {
		t.Fatalf("the risky lead stayed unroutable after the toggle was turned on: %+v", row)
	}
}

// A suppressed recipient must not wedge the campaign either.
func TestLiveSuppressedLeadDoesNotWedgeCampaign(t *testing.T) {
	f := newSendFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO suppressed_recipients (organization_id, email, reason, source) VALUES ($1, $2, 'unsubscribed', 'manual')`,
		f.org, f.emailAAddr); err != nil {
		t.Fatalf("suppress lead A: %v", err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM suppressed_recipients WHERE organization_id = $1`, f.org)
	})

	for i := 0; i < 3; i++ {
		f.tick(t)
	}
	if row := f.progressFor(t, f.leadB); row == nil || row.sentAt == nil {
		t.Fatalf("the healthy lead behind the suppressed one was never served: %+v", row)
	}
	if f.sender.count() != 1 {
		t.Fatalf("%d sends went out, want exactly 1", f.sender.count())
	}
}

// Re-verifying an address puts the lead straight back into routing: the filter
// reads the contact's CURRENT state, so correcting a bad verdict (which the
// repair migration does in bulk) is all it takes to resume sending.
func TestLiveRepairedVerificationRestoresRouting(t *testing.T) {
	f := newSendFixture(t)
	f.markVerification(t, f.leadA, "invalid", "recipient rejected (504): 5.5.2 Helo command rejected")
	f.markVerification(t, f.leadB, "invalid", "recipient rejected (504): 5.5.2 Helo command rejected")

	f.tick(t)
	if f.sender.count() != 0 {
		t.Fatalf("%d sends went out while every lead was undeliverable", f.sender.count())
	}

	// Verification refusal parks the campaign; it is not successful completion.
	var reason string
	if err := f.pool.QueryRow(context.Background(), `SELECT message FROM campaign_logs
		WHERE campaign_id = $1 AND event_type = 'auto_paused' ORDER BY created_at DESC LIMIT 1`,
		f.campaign).Scan(&reason); err != nil {
		t.Fatalf("read pause log: %v", err)
	}
	if reason != UndeliverablePauseReason(2) {
		t.Fatalf("pause logged %q; it must report the exact refused lead count", reason)
	}
	var status string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM campaigns WHERE id=$1`, f.campaign).Scan(&status); err != nil || status != "paused_undeliverable" {
		t.Fatalf("refused leads claimed completion: %s, %v", status, err)
	}

	// The verifier is corrected and the contacts are re-checked.
	f.markVerification(t, f.leadA, "valid", "recipient accepted")
	f.markVerification(t, f.leadB, "valid", "recipient accepted")
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE campaigns SET status = 'active' WHERE id = $1`, f.campaign); err != nil {
		t.Fatalf("reactivate campaign: %v", err)
	}

	f.tick(t)
	f.tick(t)
	if f.sender.count() != 2 {
		t.Fatalf("%d sends after the addresses were repaired, want 2", f.sender.count())
	}
}
