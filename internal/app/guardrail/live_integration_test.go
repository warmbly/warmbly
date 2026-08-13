package guardrail

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/repository"
)

// Live guardrail checks against a real Postgres. Skipped unless
// WARMBLY_TEST_DB is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/app/guardrail/ -run Live -v
//
// Evaluate() is unit-tested on pure counts. What only a real database can prove
// is that the windowed LATERAL query attributes bounces and complaints to the
// right campaign, and that the conditional UPDATE actually parks it.

func liveDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return pool
}

type guardrailFixture struct {
	pool     *pgxpool.Pool
	user     uuid.UUID
	org      uuid.UUID
	campaign uuid.UUID
}

// newGuardrailFixture builds an active campaign with `sent` delivered contacts,
// of which `bounced` bounced and `replied` replied.
func newGuardrailFixture(t *testing.T, pool *pgxpool.Pool, sent, bounced, replied int) *guardrailFixture {
	t.Helper()
	ctx := context.Background()
	f := &guardrailFixture{pool: pool, user: uuid.New(), org: uuid.New(), campaign: uuid.New()}

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}

	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'GR', 'Test')`,
		f.user, "gr-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'GR Test', $2, $3)`,
		f.org, "gr-"+f.org.String()[:8], f.user)
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, status,
	          daily_limit, timezone, days, start_time, end_time, updated_at, created_at,
	          guardrail_enabled, guardrail_bounce_rate_max, guardrail_complaint_rate_max,
	          guardrail_reply_rate_min, guardrail_min_sample, guardrail_window_days)
	      VALUES ($1, $2, $3, 'GR Test', '', 'active', 50, 'UTC', 127, '00:00', '23:59', NOW(), NOW(),
	              true, 5.00, 0.10, 0, 50, 7)`, f.campaign, f.user, f.org)

	seq := uuid.New()
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
	          body_plain, body_html, wait_after, position, kind)
	      VALUES ($1, $2, $3, 'S1', 'Hi', 'x', '<p>x</p>', 0, 0, 'email')`, seq, f.campaign, f.org)

	for i := 0; i < sent; i++ {
		contact := uuid.New()
		exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields)
		      VALUES ($1, $2, $3, $4, 'C', 'T', '', '', '{}')`,
			contact, f.user, f.org, "c-"+contact.String()[:12]+"@test.local")
		// sent_at inside the 7-day window so the LATERAL filter includes it.
		exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, bounced_at, replied_at)
		      VALUES ($1, $2, $3, NOW() - INTERVAL '1 day',
		              CASE WHEN $4 THEN NOW() - INTERVAL '1 day' END,
		              CASE WHEN $5 THEN NOW() - INTERVAL '1 day' END)`,
			f.campaign, contact, seq, i < bounced, i >= bounced && i < bounced+replied)
	}

	// FK order, innermost first, one argument per statement (see the note on
	// the scheduler fixture: a mismatched argument count fails every delete),
	// with errors surfaced. These build hundreds of contacts each.
	t.Cleanup(func() {
		c := context.Background()
		steps := []struct {
			sql string
			arg any
		}{
			{`DELETE FROM campaign_contact_progress WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaign_leads WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM sequences WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaigns WHERE id = $1`, f.campaign},
			{`DELETE FROM contacts WHERE organization_id = $1`, f.org},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		}
		for _, step := range steps {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return f
}

func (f *guardrailFixture) status(t *testing.T) (string, string) {
	t.Helper()
	var status, reason string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT status::text, guardrail_reason FROM campaigns WHERE id = $1`, f.campaign).
		Scan(&status, &reason); err != nil {
		t.Fatalf("read status: %v", err)
	}
	return status, reason
}

func liveService(pool *pgxpool.Pool) Service {
	// Audit, notification and campaign-log sinks are nil on purpose: the sweep
	// has to pause the campaign whether or not any of them are wired.
	return NewService(repository.NewGuardrailRepository(pool), nil, nil, nil)
}

// TestLiveSweepPausesABouncingCampaign is the headline check: real rows, the
// real windowed query, the real conditional UPDATE.
func TestLiveSweepPausesABouncingCampaign(t *testing.T) {
	pool := liveDB(t)
	// 100 sends, 12 bounces = 12%, well above the 5% ceiling and past the
	// 50-send sample floor.
	f := newGuardrailFixture(t, pool, 100, 12, 0)

	if status, _ := f.status(t); status != "active" {
		t.Fatalf("fixture should start active, got %s", status)
	}

	paused, err := liveService(pool).Sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if paused < 1 {
		t.Fatal("sweep paused nothing")
	}

	status, reason := f.status(t)
	if status != "paused_guardrail" {
		t.Fatalf("status = %s, want paused_guardrail", status)
	}
	if reason == "" {
		t.Fatal("no reason stored; the user would see a pause with no explanation")
	}
	t.Logf("paused with: %s", reason)
}

// TestLiveSweepRespectsTheSampleFloor proves a tiny sample cannot trip a
// campaign, which is what keeps the feature from being switched off in disgust.
func TestLiveSweepRespectsTheSampleFloor(t *testing.T) {
	pool := liveDB(t)
	// 4 sends, 4 bounces = 100%, but far below the 50-send floor.
	f := newGuardrailFixture(t, pool, 4, 4, 0)

	if _, err := liveService(pool).Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if status, reason := f.status(t); status != "active" {
		t.Fatalf("a 4-send campaign was paused (%s): %s", status, reason)
	}
	t.Log("100% bounce on 4 sends left alone — the floor holds")
}

// TestLiveSweepLeavesAHealthyCampaignAlone is the false-positive guard.
func TestLiveSweepLeavesAHealthyCampaignAlone(t *testing.T) {
	pool := liveDB(t)
	// 200 sends, 2 bounces = 1%, 20 replies = 10%.
	f := newGuardrailFixture(t, pool, 200, 2, 20)

	if _, err := liveService(pool).Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if status, reason := f.status(t); status != "active" {
		t.Fatalf("healthy campaign paused (%s): %s", status, reason)
	}
	t.Log("1% bounce over 200 sends left running")
}

// TestLiveSweepIsIdempotent proves a second pass cannot re-pause or re-announce
// an already-parked campaign — the conditional UPDATE is the only guard.
func TestLiveSweepIsIdempotent(t *testing.T) {
	pool := liveDB(t)
	f := newGuardrailFixture(t, pool, 100, 12, 0)
	svc := liveService(pool)

	first, err := svc.Sweep(context.Background())
	if err != nil || first < 1 {
		t.Fatalf("first sweep: paused=%d err=%v", first, err)
	}
	before, _ := f.status(t)

	second, err := svc.Sweep(context.Background())
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	after, _ := f.status(t)
	if after != before {
		t.Fatalf("status changed on the second pass: %s -> %s", before, after)
	}
	// The campaign is no longer active, so it is not even a candidate.
	if second != 0 {
		t.Fatalf("second sweep paused %d campaigns; an already-paused one must not re-trip", second)
	}
	t.Log("second sweep was a no-op")
}

// TestLiveRestartClearsTheMarker proves the pause is recoverable and the badge
// does not outlive it.
func TestLiveRestartClearsTheMarker(t *testing.T) {
	pool := liveDB(t)
	f := newGuardrailFixture(t, pool, 100, 12, 0)

	if _, err := liveService(pool).Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if status, _ := f.status(t); status != "paused_guardrail" {
		t.Fatalf("expected the campaign to be parked, got %s", status)
	}

	// StartCampaign is what the API calls; it must clear the marker.
	if _, err := pool.Exec(context.Background(), `
		UPDATE campaigns SET status = 'active', guardrail_tripped_at = NULL, guardrail_reason = ''
		WHERE id = $1`, f.campaign); err != nil {
		t.Fatalf("restart: %v", err)
	}

	var trippedAt *string
	var reason string
	if err := pool.QueryRow(context.Background(),
		`SELECT guardrail_tripped_at::text, guardrail_reason FROM campaigns WHERE id = $1`, f.campaign).
		Scan(&trippedAt, &reason); err != nil {
		t.Fatalf("read: %v", err)
	}
	if trippedAt != nil || reason != "" {
		t.Fatalf("restart left a stale marker: at=%v reason=%q", trippedAt, reason)
	}

	// And the very next sweep re-trips it, because nothing was actually fixed.
	if _, err := liveService(pool).Sweep(context.Background()); err != nil {
		t.Fatalf("re-sweep: %v", err)
	}
	if status, _ := f.status(t); status != "paused_guardrail" {
		t.Fatalf("re-sweep should have parked it again, got %s", status)
	}
	t.Log("restart cleared the marker, and the next sweep re-tripped on the unchanged numbers")
}
