package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// partnerMailboxFixture is one partner-managed workspace with one mailbox, and the cleanup that removes them.
type partnerMailboxFixture struct {
	pool      *pgxpool.Pool
	partnerID uuid.UUID
	orgID     uuid.UUID
	accountID uuid.UUID
	extID     string
}

func newPartnerMailboxFixture(t *testing.T, pool *pgxpool.Pool, mode models.PartnerMode) *partnerMailboxFixture {
	t.Helper()
	ctx := context.Background()
	f := &partnerMailboxFixture{pool: pool, partnerID: uuid.New(), orgID: uuid.New(), accountID: uuid.New(), extID: "ws-" + uuid.NewString()[:8]}
	service, owner := uuid.New(), uuid.New()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Svc', '', $2), ($3, 'Own', '', $4)`,
		service, "svc-"+service.String()+"@p.invalid", owner, "own-"+owner.String()+"@example.test")
	exec(`INSERT INTO partners (id, slug, name, display_name, service_user_id) VALUES ($1, $2, 'Partner', 'Partner', $3)`,
		f.partnerID, "p"+f.partnerID.String()[:12], service)
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Managed', $2)`, f.orgID, owner)
	exec(`INSERT INTO partner_organizations (partner_id, mode, external_id, organization_id, plan_key, campaign_sends_per_mailbox_per_day)
		VALUES ($1, $2, $3, $4, 'graphkit_managed', 3)`, f.partnerID, mode, f.extID, f.orgID)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, campaign_limit)
		VALUES ($1, $2, $3, $4, 'Box', '', '', 'smtp_imap', 50)`, f.accountID, owner, f.orgID, "box-"+f.accountID.String()[:8]+"@example.test")
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM tasks WHERE email_account_id = $1`, f.accountID)
		_, _ = pool.Exec(bg, `DELETE FROM partner_mailboxes WHERE partner_id = $1`, f.partnerID)
		_, _ = pool.Exec(bg, `DELETE FROM partner_statements WHERE partner_id = $1`, f.partnerID)
		_, _ = pool.Exec(bg, `DELETE FROM email_accounts WHERE id = $1`, f.accountID)
		_, _ = pool.Exec(bg, `DELETE FROM partner_organizations WHERE partner_id = $1`, f.partnerID)
		_, _ = pool.Exec(bg, `DELETE FROM organizations WHERE id = $1`, f.orgID)
		_, _ = pool.Exec(bg, `DELETE FROM partners WHERE id = $1`, f.partnerID)
		_, _ = pool.Exec(bg, `DELETE FROM users WHERE id IN ($1, $2)`, service, owner)
	})
	return f
}

func (f *partnerMailboxFixture) mailbox(t *testing.T, repo PartnerMailboxRepository, accountID *uuid.UUID) *models.PartnerMailbox {
	t.Helper()
	pm := "omb_" + uuid.NewString()[:8]
	m := &models.PartnerMailbox{PartnerID: f.partnerID, Mode: models.PartnerModeLive, OrgExternalID: f.extID, OrganizationID: &f.orgID,
		EmailAccountID: accountID, PartnerMailboxID: &pm, Source: models.PartnerSourcePartner, Email: pm + "@example.test", Provider: "smtp",
		ConnectState: models.PartnerConnectConnected}
	if err := repo.InsertPartnerMailbox(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	return m
}

func (f *partnerMailboxFixture) lane(t *testing.T, lane string) bool {
	t.Helper()
	var ok bool
	if err := f.pool.QueryRow(context.Background(), `SELECT `+partnerLaneSQL("ea", "$2::text")+` FROM email_accounts ea WHERE ea.id = $1`,
		f.accountID, lane).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

// The dispatch gate and the scheduler's admission read one predicate: off blocks campaigns and warmup,
// warmup only allows warmup and blocks campaigns, and test mode sends nothing.
func TestLivePartnerEnablementGatesEveryLane(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	repo := NewPartnerMailboxRepository(pool)
	tasks := NewTaskRepository(pool).(*taskRepository)
	f := newPartnerMailboxFixture(t, pool, models.PartnerModeLive)

	check := func(name string, campaign, warmup, manual bool, hold string) {
		t.Helper()
		got := [3]bool{f.lane(t, "campaign"), f.lane(t, "warmup") && f.lane(t, "placement"), f.lane(t, "email")}
		if got != [3]bool{campaign, warmup, manual} {
			t.Errorf("%s: campaign/warmup/manual = %v, want %v", name, got, [3]bool{campaign, warmup, manual})
		}
		a := &SendAdmission{}
		if err := tasks.partnerColdHold(ctx, a, f.orgID, f.accountID, time.Now()); err != nil {
			t.Fatal(err)
		}
		if a.ColdHold != hold {
			t.Errorf("%s: scheduler hold = %q, want %q", name, a.ColdHold, hold)
		}
	}
	check("no row: a mailbox connected inside a managed workspace starts off", false, false, true, ColdHoldPartnerEnablement)
	m := f.mailbox(t, repo, &f.accountID)
	check("off", false, false, true, ColdHoldPartnerEnablement)
	for e, want := range map[models.PartnerEnablement][2]bool{models.PartnerEnablementWarmupOnly: {false, true}, models.PartnerEnablementSendingWarmup: {true, true}} {
		if _, err := repo.SetPartnerEnablement(ctx, m.ID, e, time.Now()); err != nil {
			t.Fatal(err)
		}
		hold := ColdHoldPartnerEnablement
		if want[0] {
			hold = ""
		}
		check(string(e), want[0], want[1], true, hold)
	}
	if err := repo.DetachPartnerMailbox(ctx, m.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	check("detached", false, false, true, ColdHoldPartnerEnablement)

	if _, err := pool.Exec(ctx, `UPDATE partner_organizations SET mode = 'test' WHERE organization_id = $1`, f.orgID); err != nil {
		t.Fatal(err)
	}
	check("test mode", false, false, false, ColdHoldPartnerTestMode)
	if _, err := pool.Exec(ctx, `DELETE FROM partner_organizations WHERE organization_id = $1`, f.orgID); err != nil {
		t.Fatal(err)
	}
	check("a workspace no partner manages", true, true, true, "")
}

// The partner's cap counts campaign sends in the UTC day: yesterday's and walked-back ones do not count,
// and at the cap the scheduler holds the mailbox until the next UTC midnight and dispatch refuses.
func TestLivePartnerDailyCapResetsEachUTCDay(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	repo := NewPartnerMailboxRepository(pool)
	tasks := NewTaskRepository(pool).(*taskRepository)
	f := newPartnerMailboxFixture(t, pool, models.PartnerModeLive)
	m := f.mailbox(t, repo, &f.accountID)
	if _, err := repo.SetPartnerEnablement(ctx, m.ID, models.PartnerEnablementSendingWarmup, time.Now()); err != nil {
		t.Fatal(err)
	}
	var midnight time.Time
	if err := pool.QueryRow(ctx, `SELECT `+utcDayStartSQL).Scan(&midnight); err != nil {
		t.Fatal(err)
	}
	send := func(at time.Time, released bool) uuid.UUID {
		t.Helper()
		id := uuid.New()
		var rel *time.Time
		if released {
			rel = &at
		}
		if _, err := pool.Exec(ctx, `INSERT INTO tasks (id, task_type, email_account_id, status, message_id, send_reserved_at, send_released_at)
			VALUES ($1, 'campaign', $2, 'completed', $5, $3, $4)`, id, f.accountID, at, rel, "<"+id.String()+"@example.test>"); err != nil {
			t.Fatal(err)
		}
		return id
	}
	room := func() (*SendAdmission, error) {
		a := &SendAdmission{}
		return a, tasks.partnerColdHold(ctx, a, f.orgID, f.accountID, time.Now())
	}
	// Yesterday's sends, right up to midnight, and a walked-back send today count for nothing.
	send(midnight.Add(-time.Minute), false)
	send(midnight.Add(-3*time.Hour), false)
	send(midnight.Add(time.Second), true)
	if a, err := room(); err != nil || a.ColdHold != "" || a.ColdRoom == nil || *a.ColdRoom != 3 {
		t.Fatalf("fresh day = %+v %v", a, err)
	}
	send(midnight.Add(time.Second), false)
	last := send(midnight.Add(2*time.Second), false)
	if a, _ := room(); a.ColdHold != "" || *a.ColdRoom != 1 {
		t.Fatalf("two sent = %+v", a)
	}
	send(midnight.Add(3*time.Second), false)
	a, _ := room()
	if a.ColdHold != ColdHoldPartnerDailyCap || *a.ColdRoom != 0 || a.ColdReopensAt == nil || !a.ColdReopensAt.Equal(midnight.Add(24*time.Hour)) {
		t.Fatalf("at the cap = %+v (reopens %v)", a, a.ColdReopensAt)
	}
	// Dispatch reads the same count inside the reservation, leaving out the send being reserved.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := checkPartnerDailyCap(ctx, tx, uuid.New(), f.accountID); err != ErrCampaignDailyLimit {
		t.Fatalf("a fourth send = %v, want the daily limit", err)
	}
	if err := checkPartnerDailyCap(ctx, tx, last, f.accountID); err != nil {
		t.Fatalf("re-reserving the third send = %v", err)
	}
}

// Enabled mailbox-days over a month with mid-month changes: a day counts once, under the highest
// enablement it had; a span ending exactly at midnight does not reach the next day.
func TestLivePartnerMailboxDaysAcrossMidMonthChanges(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	repo := NewPartnerMailboxRepository(pool)
	f := newPartnerMailboxFixture(t, pool, models.PartnerModeLive)
	at := func(month time.Month, day, hour, minute int) time.Time {
		return time.Date(2026, month, day, hour, minute, 0, 0, time.UTC)
	}
	set := func(m *models.PartnerMailbox, e models.PartnerEnablement, when time.Time) {
		t.Helper()
		if _, err := repo.SetPartnerEnablement(ctx, m.ID, e, when); err != nil {
			t.Fatal(err)
		}
	}
	a, b, c := f.mailbox(t, repo, nil), f.mailbox(t, repo, nil), f.mailbox(t, repo, nil)
	set(a, models.PartnerEnablementWarmupOnly, at(10, 3, 10, 0))
	set(a, models.PartnerEnablementSendingWarmup, at(10, 5, 9, 0))
	set(a, models.PartnerEnablementOff, at(10, 5, 18, 0))
	set(a, models.PartnerEnablementWarmupOnly, at(10, 20, 23, 59))
	set(a, models.PartnerEnablementOff, at(10, 21, 0, 0))
	set(a, models.PartnerEnablementSendingWarmup, at(10, 31, 23, 0))
	set(b, models.PartnerEnablementSendingWarmup, at(9, 28, 8, 0))
	set(b, models.PartnerEnablementOff, at(10, 2, 12, 0))
	set(c, models.PartnerEnablementWarmupOnly, at(10, 10, 12, 0))
	set(c, models.PartnerEnablementOff, at(10, 10, 12, 0))

	now := at(11, 18, 12, 0)
	days := func(month time.Month) map[uuid.UUID][2]int {
		t.Helper()
		start := time.Date(2026, month, 1, 0, 0, 0, 0, time.UTC)
		rows, err := repo.PartnerMailboxDays(ctx, f.partnerID, models.PartnerModeLive, start, start.AddDate(0, 1, 0), now)
		if err != nil {
			t.Fatal(err)
		}
		out := map[uuid.UUID][2]int{}
		for _, r := range rows {
			out[r.MailboxID] = [2]int{r.SendingWarmupDays, r.WarmupOnlyDays}
		}
		return out
	}
	oct := days(10)
	if oct[a.ID] != [2]int{2, 3} || oct[b.ID] != [2]int{2, 0} || oct[c.ID] != [2]int{0, 1} || len(oct) != 3 {
		t.Fatalf("October = a %v, b %v, c %v (%d rows)", oct[a.ID], oct[b.ID], oct[c.ID], len(oct))
	}
	if sep := days(9); sep[b.ID] != [2]int{3, 0} || len(sep) != 1 {
		t.Fatalf("September = %v", sep)
	}
	// The open span runs to now: November 1 to 18.
	if nov := days(11); nov[a.ID] != [2]int{18, 0} || len(nov) != 1 {
		t.Fatalf("November = %v", nov)
	}
	if dec := days(12); len(dec) != 0 {
		t.Fatalf("a month after now = %v", dec)
	}
	// Another partner's mode sees none of it.
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if rows, err := repo.PartnerMailboxDays(ctx, f.partnerID, models.PartnerModeTest, start, start.AddDate(0, 1, 0), now); err != nil || len(rows) != 0 {
		t.Fatalf("test mode = %v %v", rows, err)
	}
}

// A mailbox's lock is read within the caller's workspace: another workspace naming the same mailbox id learns nothing.
func TestLivePartnerMailboxLockIsScopedToTheWorkspace(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	repo := NewPartnerMailboxRepository(pool)
	f := newPartnerMailboxFixture(t, pool, models.PartnerModeLive)
	f.mailbox(t, repo, &f.accountID)

	if name, err := repo.PartnerMailboxLock(ctx, f.orgID, f.accountID); err != nil || name != "Partner" {
		t.Fatalf("own workspace: lock %q err %v, want Partner", name, err)
	}
	if name, err := repo.PartnerMailboxLock(ctx, uuid.New(), f.accountID); err != nil || name != "" {
		t.Fatalf("another workspace: lock %q err %v, want none", name, err)
	}
}
