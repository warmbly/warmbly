package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// The pool ledger, pack refunds, staff mirroring and the owner-deletion guard
// against a fresh, migrated database:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveAgencyPool -v

func TestLiveAgencyPool(t *testing.T) {
	pool := liveCampaignReplayDB(t)
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v\n%s", err, query)
		}
	}
	owner, staff := uuid.New(), uuid.New()
	for _, u := range []uuid.UUID{owner, staff} {
		exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'A', 'B', $2)`, u, u.String()+"@example.test")
	}
	agency := uuid.New()
	exec(`INSERT INTO agencies (id, name, owner_user_id, status) VALUES ($1, 'Agency', $2, 'active')`, agency, owner)
	org := uuid.New()
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Client', $2)`, org, owner)
	exec(`INSERT INTO agency_clients (organization_id, agency_id, added_at) VALUES ($1, $2, now() - interval '3 days')`, org, agency)

	campaign, seq := uuid.New(), uuid.New()
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, status, days, timezone, created_at, updated_at) VALUES ($1, $2, $3, 'C', '', 'active', 127, 'UTC', now(), now())`, campaign, owner, org)
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html, wait_after, position) VALUES ($1, $2, $3, 'S', 'S', 'T', '', 0, 1)`, seq, campaign, org)
	now := time.Now().UTC()
	send := func(at time.Time) {
		contact := uuid.New()
		exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields) VALUES ($1, $2, $3, $4, '', '', '', '', '{}')`,
			contact, owner, org, contact.String()+"@lead.test")
		exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at) VALUES ($1, $2, $3, $4)`, campaign, contact, seq, at)
	}
	days := models.NewAgencyPoolDays(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
	today := days.At(now)
	send(today.Start.Add(time.Minute))
	send(today.Start.Add(2 * time.Minute))
	send(today.Start.Add(-time.Hour)) // yesterday

	r := NewAgencyRepository(pool)
	usage := func() int64 {
		t.Helper()
		if err := r.RollupPoolUsage(ctx, agency, days.Recent(now)); err != nil {
			t.Fatal(err)
		}
		u, err := r.PoolUsage(ctx, agency, today.Day.AddDate(0, 0, -1), today.Day.AddDate(0, 0, 1))
		if err != nil {
			t.Fatal(err)
		}
		return u[org]
	}
	if got := usage(); got != 3 {
		t.Fatalf("rolled-up use: got %d, want 3", got)
	}
	if got, err := r.SentTodayByClient(ctx, agency); err != nil || got[org] < 2 {
		t.Fatalf("sent today: %v %v", got, err)
	}

	// Deleting the campaign and detaching then re-attaching give nothing back.
	exec(`DELETE FROM campaigns WHERE id = $1`, campaign)
	exec(`DELETE FROM agency_clients WHERE organization_id = $1`, org)
	exec(`INSERT INTO agency_clients (organization_id, agency_id) VALUES ($1, $2)`, org, agency)
	if got := usage(); got != 3 {
		t.Fatalf("after deleting the campaign and re-attaching: got %d, want 3", got)
	}

	// A refunded pack leaves the period's top-ups, once.
	period := today.Start
	pi := "pi_" + uuid.NewString()
	if ok, err := r.RecordTopup(ctx, &models.AgencyPoolTopup{AgencyID: agency, Sends: 50_000, StripePaymentIntentID: &pi, PeriodStart: period}); err != nil || !ok {
		t.Fatalf("record top-up: %v %v", ok, err)
	}
	if n, _ := r.TopupSends(ctx, agency, period); n != 50_000 {
		t.Fatalf("top-ups: %d", n)
	}
	for range 2 {
		if id, ok, err := r.RefundTopup(ctx, pi); err != nil || !ok || id != agency {
			t.Fatalf("refund: %v %v %v", id, ok, err)
		}
	}
	if n, _ := r.TopupSends(ctx, agency, period); n != 0 {
		t.Fatalf("a refunded pack must leave the pool, got %d", n)
	}
	if _, ok, err := r.RefundTopup(ctx, "pi_unknown"); err != nil || ok {
		t.Fatal("an unknown payment intent is no pack")
	}

	// Mirroring staff writes once, then leaves the row alone.
	if err := r.MirrorStaff(ctx, agency, org, staff, models.AgencyRoleAdmin, owner); err != nil {
		t.Fatal(err)
	}
	var xmin1, xmin2 string
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM organization_members WHERE organization_id = $1 AND user_id = $2`, org, staff).Scan(&xmin1); err != nil {
		t.Fatal(err)
	}
	if err := r.MirrorStaff(ctx, agency, org, staff, models.AgencyRoleAdmin, owner); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM organization_members WHERE organization_id = $1 AND user_id = $2`, org, staff).Scan(&xmin2); err != nil {
		t.Fatal(err)
	}
	if xmin1 != xmin2 {
		t.Fatal("a membership already in its mirrored state must not be rewritten")
	}
	if err := r.MirrorStaff(ctx, agency, org, staff, models.AgencyRoleMember, owner); err != nil {
		t.Fatal(err)
	}
	var role string
	if err := pool.QueryRow(ctx, `SELECT role FROM organization_members WHERE organization_id = $1 AND user_id = $2`, org, staff).Scan(&role); err != nil || role != string(models.RoleManager) {
		t.Fatalf("a changed staff role must still be mirrored, got %q %v", role, err)
	}

	// The agency owner cannot delete the account while the agency is billed.
	exec(`UPDATE agencies SET stripe_subscription_id = $2 WHERE id = $1`, agency, "sub_"+uuid.NewString())
	dz := NewDangerZoneRepository(pool)
	if _, err := dz.HardDeleteUser(ctx, owner, nil); !errors.Is(err, ErrOwnedAgencyBilled) {
		t.Fatalf("got %v, want ErrOwnedAgencyBilled", err)
	}
	renews := func(context.Context, []string) error { return ErrOwnedWorkspaceBilled }
	exec(`UPDATE agencies SET cancel_at_period_end = true WHERE id = $1`, agency)
	if _, err := dz.HardDeleteUser(ctx, owner, renews); !errors.Is(err, ErrOwnedAgencyBilled) {
		t.Fatalf("a subscription the provider says renews must refuse as the agency's, got %v", err)
	}

	// The egress slot goes to the oldest clients with a mailbox.
	w := NewWorkerRepository(pool)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider) VALUES ($1, $2, $3, $4, 'B', '', '', 'smtp_imap')`,
		uuid.New(), owner, org, uuid.NewString()+"@client.test")
	newer := uuid.New()
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Newer', $2)`, newer, owner)
	exec(`INSERT INTO agency_clients (organization_id, agency_id, added_at) VALUES ($1, $2, now() + interval '1 minute')`, newer, agency)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider) VALUES ($1, $2, $3, $4, 'B', '', '', 'smtp_imap')`,
		uuid.New(), owner, newer, uuid.NewString()+"@client.test")
	if ok, err := w.AgencyEgressSlot(ctx, agency, org, 1); err != nil || !ok {
		t.Fatalf("oldest client: %v %v", ok, err)
	}
	if ok, err := w.AgencyEgressSlot(ctx, agency, newer, 1); err != nil || ok {
		t.Fatalf("a client past the cap: %v %v", ok, err)
	}
}
