package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/geo"
	"github.com/warmbly/warmbly/internal/repository"
)

// A campaign in recipient mode reads its window on each lead's clock, so the
// same 09:00-11:00 window is open for one lead and hours away for another.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/scheduler/ -run LiveRecipientClock -v

// offsetWhereItIs is a fixed-offset timezone value whose local hour is now `hour`.
func offsetWhereItIs(hour int) string {
	h := ((hour-time.Now().UTC().Hour())%24 + 24) % 24
	if h > 14 {
		h -= 24
	}
	return fmt.Sprintf("UTC%+d", h)
}

func offsetLocation(t *testing.T, value string) *time.Location {
	t.Helper()
	zone, ok := geo.ParseTimezone(value)
	if !ok {
		t.Fatalf("%q does not parse", value)
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// recipientClockFixture is the live fixture with its mailbox on no clock of
// its own (so no 8am-8pm band) and its campaign open 09:00-11:00 on each
// lead's clock.
func recipientClockFixture(t *testing.T) (*liveFixture, SchedulerService) {
	t.Helper()
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "")
	if _, err := pool.Exec(context.Background(),
		`UPDATE campaigns SET timezone_mode = 'recipient', start_time = '09:00', end_time = '11:00', days = 127 WHERE id = $1`,
		f.campaign); err != nil {
		t.Fatalf("recipient mode: %v", err)
	}
	return f, liveScheduler(t, handle, pool)
}

func TestLiveRecipientClockSendsALeadWhoseDayIsOpen(t *testing.T) {
	f, s := recipientClockFixture(t)
	tz := offsetWhereItIs(9)
	f.setContactTimezone(t, tz)

	at, _ := scheduleSlot(t, s, f.campaign)
	if local := at.In(offsetLocation(t, tz)); local.Hour() < 9 || local.Hour() >= 11 {
		t.Errorf("slot %s is outside the lead's 09:00-11:00", local.Format(time.RFC3339))
	}
	if time.Until(at) > 90*time.Minute {
		t.Errorf("an open lead was held %v", time.Until(at).Truncate(time.Minute))
	}
}

func TestLiveRecipientClockHoldsALeadUntilTheirMorning(t *testing.T) {
	f, s := recipientClockFixture(t)
	ctx := context.Background()
	tz := offsetWhereItIs(21)
	f.setContactTimezone(t, tz)

	// The campaign holds the lead and looks again soon, in case routing's stored
	// zones were hiding a lead whose day is open.
	at, pair, _, err := s.CalculateNextCampaignTime(ctx, f.campaign)
	if !errors.Is(err, ErrCampaignDeferred) || pair != nil {
		t.Fatalf("a lead at 21:00 local was offered: pair=%v err=%v", pair, err)
	}
	if time.Until(at) > recipientClockRecheck+time.Minute {
		t.Errorf("the campaign rechecks in %v, want within %v", time.Until(at).Truncate(time.Minute), recipientClockRecheck)
	}

	// The lead itself waits for its own morning, and the drawer says why.
	var lead uuid.UUID
	if err := f.pool.QueryRow(ctx, `SELECT contact_id FROM campaign_leads WHERE campaign_id = $1`, f.campaign).Scan(&lead); err != nil {
		t.Fatal(err)
	}
	pv, err := s.(ContactSendPreviewer).PreviewContactSend(ctx, f.campaign, lead)
	if err != nil {
		t.Fatal(err)
	}
	if pv.NotBefore == nil {
		t.Fatalf("preview %+v has no not-before", pv)
	}
	if local := pv.NotBefore.In(offsetLocation(t, tz)); local.Hour() != 9 {
		t.Errorf("lead held until %s, want their next 09:00", local.Format(time.RFC3339))
	}
	if pv.Constraint != ConstraintRecipientHours {
		t.Errorf("constraint %q, want %q", pv.Constraint, ConstraintRecipientHours)
	}
}

// One lead's night is nobody else's: a closed lead first in line must not
// park the campaign while the lead behind them is in their working day.
func TestLiveRecipientClockMovesPastALeadAtNight(t *testing.T) {
	f, s := recipientClockFixture(t)
	ctx := context.Background()
	f.setContactTimezone(t, offsetWhereItIs(21))

	open := uuid.New()
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, created_at)
		VALUES ($1, $2, $3, $4, 'Open', 'Lead', '', '', jsonb_build_object('timezone', $5::text), NOW() + interval '1 second')`,
		open, f.user, f.org, "open-"+open.String()[:8]+"@test.local", offsetWhereItIs(9)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, 1)`, f.campaign, open); err != nil {
		t.Fatal(err)
	}

	_, pair, _, err := s.CalculateNextCampaignTime(ctx, f.campaign)
	if err != nil {
		t.Fatalf("the campaign deferred instead of sending the open lead: %v", err)
	}
	if pair == nil || pair.ContactID != open {
		t.Fatalf("sent %v, want the lead whose day is open (%s)", pair, open)
	}
}

// The send plan's day is the leads' day: a lead inside their window keeps the
// plan open while the campaign's own clock is at night.
func TestLiveRecipientClockPlanIsOpenWhileALeadsDayIs(t *testing.T) {
	f, s := recipientClockFixture(t)
	ctx := context.Background()
	night, _ := geo.ParseTimezone(offsetWhereItIs(21))
	if _, err := f.pool.Exec(ctx, `UPDATE campaigns SET timezone = $2 WHERE id = $1`, f.campaign, night); err != nil {
		t.Fatal(err)
	}
	f.setContactTimezone(t, offsetWhereItIs(9))
	sweepFixtureLocations(t, f)

	planner, ok := s.(interface {
		PlanCampaignDay(context.Context, uuid.UUID, int) (*models.CampaignSendPlan, error)
	})
	if !ok {
		t.Fatal("scheduler does not plan")
	}
	plan, err := planner.PlanCampaignDay(ctx, f.campaign, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Window.OpenNow || plan.Window.MinutesLeft == 0 {
		t.Fatalf("plan window %+v, want open with time left: the lead's day is open", plan.Window)
	}

	if _, err := f.pool.Exec(ctx, `UPDATE campaigns SET timezone_mode = 'campaign' WHERE id = $1`, f.campaign); err != nil {
		t.Fatal(err)
	}
	if plan, err = planner.PlanCampaignDay(ctx, f.campaign, 0); err != nil {
		t.Fatal(err)
	}
	if plan.Window.OpenNow {
		t.Fatalf("a campaign-clock plan at 21:00 campaign time reads open: %+v", plan.Window)
	}
}

// sweepFixtureLocations stores the fixture's contacts' locations the way the sweep does.
func sweepFixtureLocations(t *testing.T, f *liveFixture) {
	t.Helper()
	repo := repository.NewContactRepostory(&db.DB{Pool: f.pool})
	pending, err := repo.ListLocationPending(context.Background(), 100000)
	if err != nil {
		t.Fatal(err)
	}
	results := make([]repository.ContactLocationResult, len(pending))
	for i, p := range pending {
		loc := geo.Resolve(geo.Input{Fields: p.CustomFields, Phone: p.Phone, Email: p.Email})
		results[i] = repository.ContactLocationResult{ID: p.ID, Fingerprint: p.Fingerprint, Timezone: loc.Timezone, Country: loc.Country, Source: string(loc.Source)}
	}
	if _, err := repo.SetContactLocations(context.Background(), results); err != nil {
		t.Fatal(err)
	}
}

// The start check a worker asks before sending reads the same clock the
// scheduler placed the send on, or every lead outside the campaign's own
// hours would be placed and then refused.
func TestLiveRecipientClockStartCheckFollowsTheLead(t *testing.T) {
	f, s := recipientClockFixture(t)
	ctx := context.Background()
	night, _ := geo.ParseTimezone(offsetWhereItIs(21))
	if _, err := f.pool.Exec(ctx, `UPDATE campaigns SET timezone = $2 WHERE id = $1`, f.campaign, night); err != nil {
		t.Fatal(err)
	}
	f.setContactTimezone(t, offsetWhereItIs(9))
	var lead uuid.UUID
	if err := f.pool.QueryRow(ctx, `SELECT contact_id FROM campaign_leads WHERE campaign_id = $1`, f.campaign).Scan(&lead); err != nil {
		t.Fatal(err)
	}

	gate := s.(OutboundExecutionGate)
	now := time.Now()
	at, err := gate.OutboundExecutionNotBefore(ctx, f.mailbox, f.campaign, lead, uuid.New(), now)
	if err != nil {
		t.Fatal(err)
	}
	if at.After(now) {
		t.Fatalf("a lead inside their window was held until %s", at)
	}
	if at, err = gate.OutboundExecutionNotBefore(ctx, f.mailbox, f.campaign, uuid.Nil, uuid.New(), now); err != nil || !at.After(now) {
		t.Fatalf("with no lead the campaign's night clock must hold the send: at=%s err=%v", at, err)
	}
}

// A closed lead whose morning falls outside the mailbox's own hours is that
// lead's wait, not the pool's: the open lead behind them is still sent.
func TestLiveRecipientClockMovesPastALeadOutsideMailboxHours(t *testing.T) {
	f, s := recipientClockFixture(t)
	ctx := context.Background()
	noon, _ := geo.ParseTimezone(offsetWhereItIs(12))
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET timezone = $2 WHERE id = $1`, f.mailbox, noon); err != nil {
		t.Fatal(err)
	}
	// The lead's next 09:00 is midnight on the mailbox's clock.
	f.setContactTimezone(t, offsetWhereItIs(21))

	open := uuid.New()
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, created_at)
		VALUES ($1, $2, $3, $4, 'Open', 'Lead', '', '', jsonb_build_object('timezone', $5::text), NOW() + interval '1 second')`,
		open, f.user, f.org, "open-"+open.String()[:8]+"@test.local", offsetWhereItIs(9)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, 1)`, f.campaign, open); err != nil {
		t.Fatal(err)
	}

	_, pair, _, err := s.CalculateNextCampaignTime(ctx, f.campaign)
	if err != nil {
		t.Fatalf("the campaign deferred instead of sending the open lead: %v", err)
	}
	if pair == nil || pair.ContactID != open {
		t.Fatalf("sent %v, want the lead whose day is open (%s)", pair, open)
	}
}
