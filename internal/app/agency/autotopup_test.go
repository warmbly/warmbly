package agency

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// fakeCharger keeps one invoice per top-up, as Stripe does, and answers as told.
type fakeCharger struct {
	card     *SavedCard
	declined bool
	// failBefore fails once before any money moves; lostAnswer takes the money once and then fails; down fails every time.
	failBefore, lostAnswer, down bool
	invoices                     map[uuid.UUID]string
	claims                       []uuid.UUID
	cents                        []int64
}

func (f *fakeCharger) Card(context.Context, *models.Agency) (*SavedCard, error) { return f.card, nil }

func (f *fakeCharger) Charge(_ context.Context, _ *models.Agency, card *SavedCard, req chargeRequest) (chargeResult, error) {
	if f.invoices == nil {
		f.invoices = map[uuid.UUID]string{}
	}
	status, made := f.invoices[req.ClaimID]
	if !made {
		if req.Cancel {
			return chargeResult{Cancelled: true}, nil
		}
		status = "open"
		f.invoices[req.ClaimID] = status
	}
	if req.InvoiceID == "" {
		if err := req.Saved("in_" + req.ClaimID.String()); err != nil {
			return chargeResult{}, err
		}
	}
	switch status {
	case "paid":
		return chargeResult{Paid: true, Ref: "pi_" + req.ClaimID.String()}, nil
	case "void":
		return chargeResult{Cancelled: req.Cancel, Declined: !req.Cancel}, nil
	}
	if req.Cancel || card == nil {
		f.invoices[req.ClaimID] = "void"
		return chargeResult{Cancelled: true}, nil
	}
	if f.failBefore || f.down {
		f.failBefore = false
		return chargeResult{}, errors.New("stripe is unavailable")
	}
	if f.declined {
		f.invoices[req.ClaimID] = "void"
		return chargeResult{Declined: true}, nil
	}
	f.invoices[req.ClaimID] = "paid"
	f.claims = append(f.claims, req.ClaimID)
	f.cents = append(f.cents, req.Cents)
	if f.lostAnswer {
		f.lostAnswer = false
		return chargeResult{}, errors.New("the answer was lost")
	}
	return chargeResult{Paid: true, Ref: "pi_" + req.ClaimID.String()}, nil
}

// topupRepo is operatorRepo plus the automatic top-up's own writes.
type topupRepo struct {
	*operatorRepo
	disabled string
	claims   []*models.AgencyAutoTopupClaim
	members  []models.AgencyMember
	stuck    map[uuid.UUID]bool
	at       time.Time
	// membersDown fails the lookup of who to tell.
	membersDown bool
}

// clock stamps a claim; tests move it to age one.
func (r *topupRepo) clock() time.Time {
	if r.at.IsZero() {
		return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	}
	return r.at
}

func (r *topupRepo) ListMembers(context.Context, uuid.UUID) ([]models.AgencyMember, error) {
	if r.membersDown {
		return nil, errors.New("members unavailable")
	}
	return r.members, nil
}

// RecordTopup adds a pack once per payment, like the table's unique payment reference.
func (r *topupRepo) RecordTopup(ctx context.Context, t *models.AgencyPoolTopup) (bool, error) {
	for _, seen := range r.recorded {
		if seen.StripePaymentIntentID != nil && t.StripePaymentIntentID != nil && *seen.StripePaymentIntentID == *t.StripePaymentIntentID {
			return false, nil
		}
	}
	return r.operatorRepo.RecordTopup(ctx, t)
}

func (r *topupRepo) used(kind models.AgencyAutoTopupKind, period time.Time) (n int, cents int64, pending bool) {
	for _, c := range r.claims {
		if c.Kind != kind {
			continue
		}
		if c.Status == models.AgencyAutoTopupPending {
			pending = true
		}
		if c.PeriodStart.Equal(period) && (c.Status == models.AgencyAutoTopupPending || c.Status == models.AgencyAutoTopupPaid) {
			n++
			cents += c.AmountCents
		}
	}
	return n, cents, pending
}

// ClaimAutoTopup decides as the statement does: on the stored settings, the period's limit and nothing in flight.
func (r *topupRepo) ClaimAutoTopup(_ context.Context, agencyID uuid.UUID, kind models.AgencyAutoTopupKind, period time.Time, cents int64, sends int, level int64) (*models.AgencyAutoTopupClaim, error) {
	a := r.agency
	n, usedCents, pending := r.used(kind, period)
	if pending {
		return nil, nil
	}
	if kind == models.AgencyAutoTopupAI {
		if !a.AIAutoTopupEnabled || level >= int64(a.AIAutoTopupThreshold) || a.AIAutoTopupAmount != models.MicroFromCents(cents) ||
			models.MicroFromCents(usedCents+cents) > a.AIAutoTopupLimit {
			return nil, nil
		}
	} else if !a.AutoTopupEnabled || level > int64(a.AutoTopupThreshold) || n >= a.AutoTopupMaxPacks {
		return nil, nil
	}
	c := &models.AgencyAutoTopupClaim{ID: uuid.New(), AgencyID: agencyID, Kind: kind, PeriodStart: period, AmountCents: cents, Sends: sends, Status: models.AgencyAutoTopupPending, CreatedAt: r.clock()}
	r.claims = append(r.claims, c)
	out := *c
	return &out, nil
}

func (r *topupRepo) PendingAutoTopups(context.Context, uuid.UUID) ([]models.AgencyAutoTopupClaim, error) {
	var out []models.AgencyAutoTopupClaim
	for _, c := range r.claims {
		if c.Status == models.AgencyAutoTopupPending {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (r *topupRepo) AutoTopupByInvoice(_ context.Context, invoiceID string) (*models.AgencyAutoTopupClaim, error) {
	for _, c := range r.claims {
		if c.StripeInvoiceID == invoiceID {
			out := *c
			return &out, nil
		}
	}
	return nil, nil
}

func (r *topupRepo) SetAutoTopupInvoice(_ context.Context, id uuid.UUID, invoiceID string) error {
	for _, c := range r.claims {
		if c.ID == id && c.StripeInvoiceID == "" {
			c.StripeInvoiceID = invoiceID
		}
	}
	return nil
}

func (r *topupRepo) FinishAutoTopup(_ context.Context, id uuid.UUID, status, _ string) (bool, error) {
	for _, c := range r.claims {
		if c.ID == id && c.Status == models.AgencyAutoTopupPending {
			c.Status = status
			return true, nil
		}
	}
	return false, nil
}

func (r *topupRepo) MarkAutoTopupStuck(_ context.Context, id uuid.UUID) (bool, error) {
	if r.stuck[id] {
		return false, nil
	}
	if r.stuck == nil {
		r.stuck = map[uuid.UUID]bool{}
	}
	r.stuck[id] = true
	return true, nil
}

func (r *topupRepo) ClearAutoTopupStuck(_ context.Context, id uuid.UUID) error {
	delete(r.stuck, id)
	return nil
}

func (r *topupRepo) AutoTopupUsed(_ context.Context, _ uuid.UUID, kind models.AgencyAutoTopupKind, period time.Time) (int, int64, error) {
	n, cents, _ := r.used(kind, period)
	return n, cents, nil
}

func (r *topupRepo) statuses() []string {
	out := make([]string, 0, len(r.claims))
	for _, c := range r.claims {
		out = append(out, c.Status)
	}
	return out
}

func (r *topupRepo) DisableAutoTopup(_ context.Context, _ uuid.UUID, failure string) error {
	r.disabled = failure
	r.agency.AutoTopupEnabled = false
	r.agency.AutoTopupFailure = &failure
	return nil
}

func (r *topupRepo) SetAutoTopup(_ context.Context, _ uuid.UUID, enabled bool, threshold, maxPacks int) error {
	r.agency.AutoTopupEnabled, r.agency.AutoTopupThreshold, r.agency.AutoTopupMaxPacks = enabled, threshold, maxPacks
	if enabled {
		r.agency.AutoTopupFailure = nil
	}
	return nil
}

// told records the notifications sent.
type told struct {
	orgs   []*uuid.UUID
	titles []string
}

func (n *told) Notify(_ context.Context, _ uuid.UUID, orgID *uuid.UUID, _ models.NotificationCategory, title, _, _ string, _ map[string]any) {
	n.orgs = append(n.orgs, orgID)
	n.titles = append(n.titles, title)
}

func newTopupService(a *models.Agency, used int64, charger *fakeCharger, now time.Time) (*Service, *topupRepo, uuid.UUID) {
	actor := uuid.New()
	repo := &topupRepo{operatorRepo: &operatorRepo{
		fakeRepo: &fakeRepo{agency: a, grants: map[uuid.UUID]uuid.UUID{}, sends: map[uuid.UUID]int64{uuid.New(): used}},
		staff:    map[uuid.UUID]bool{actor: true}, untils: map[uuid.UUID]*time.Time{},
	}}
	s := NewService(repo, operatorOrgs{}, operatorUsers{}, operatorSubs{}, fakePlans{pool: intp(150_000)})
	s.billing = true
	s.now = func() time.Time { return now }
	s.charger = charger
	return s, repo, actor
}

func topupAgency(now time.Time, threshold, maxPacks int) *models.Agency {
	a := activeAgency(now)
	a.StripeCustomerID, a.StripeSubscriptionID = strp("cus_1"), strp("sub_1")
	a.AutoTopupEnabled, a.AutoTopupThreshold, a.AutoTopupMaxPacks = true, threshold, maxPacks
	return a
}

func TestAutoTopupBuysOnePackWhenThePoolIsLow(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := topupAgency(now, 10_000, 2)
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1", Brand: "visa", Last4: "4242"}}
	// 145,000 of 150,000 used: 5,000 left, under the 10,000 threshold.
	s, repo, _ := newTopupService(a, 145_000, charger, now)
	ctx := context.Background()

	s.autoTopup(ctx, a)
	if len(repo.recorded) != 1 || !repo.recorded[0].Automatic || repo.recorded[0].Sends != models.AgencyTopupSends || repo.recorded[0].AmountCents != 8_500 {
		t.Fatalf("recorded: %+v", repo.recorded)
	}
	if len(charger.cents) != 1 || charger.cents[0] != 8_500 {
		t.Fatalf("charged: %v", charger.cents)
	}
	// 55,000 are left after the pack, so the next sweep buys nothing more.
	s.autoTopup(ctx, a)
	if len(charger.claims) != 1 {
		t.Fatalf("a pool back above its threshold was charged again: %v", charger.claims)
	}
}

func TestAutoTopupStopsAtThePeriodsPacks(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := topupAgency(now, 10_000, 2)
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1"}}
	// So far over the pool that every pack leaves it empty.
	s, repo, _ := newTopupService(a, 400_000, charger, now)
	ctx := context.Background()
	for range 5 {
		s.autoTopup(ctx, a)
	}
	if len(repo.recorded) != 2 || len(charger.claims) != 2 {
		t.Fatalf("bought %d packs with %d charges, want the period's 2", len(repo.recorded), len(charger.claims))
	}
	if charger.claims[0] == charger.claims[1] {
		t.Fatal("each pack of a period needs its own charge")
	}
}

func TestAutoTopupLeavesAHealthyPoolAlone(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := topupAgency(now, 10_000, 2)
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1"}}
	s, repo, _ := newTopupService(a, 20_000, charger, now)
	s.autoTopup(context.Background(), a)
	a.AutoTopupEnabled = false
	s.autoTopup(context.Background(), a)
	if len(charger.claims) != 0 || len(repo.recorded) != 0 || len(repo.claims) != 0 {
		t.Fatalf("charged %v, recorded %v", charger.claims, repo.recorded)
	}
}

func TestAutoTopupTurnsItselfOffWhenTheCardRefuses(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	a := topupAgency(now, 10_000, 2)
	s, repo, _ := newTopupService(a, 149_000, &fakeCharger{card: &SavedCard{ID: "pm_1"}, declined: true}, now)
	s.autoTopup(ctx, a)
	if repo.disabled != models.AgencyAutoTopupDeclined || a.AutoTopupEnabled || len(repo.recorded) != 0 {
		t.Fatalf("declined: disabled=%q enabled=%v recorded=%v", repo.disabled, a.AutoTopupEnabled, repo.recorded)
	}

	b := topupAgency(now, 10_000, 2)
	charger := &fakeCharger{}
	s, repo, _ = newTopupService(b, 149_000, charger, now)
	s.autoTopup(ctx, b)
	if repo.disabled != models.AgencyAutoTopupNoCard || len(charger.claims) != 0 {
		t.Fatalf("no card: disabled=%q charges=%v", repo.disabled, charger.claims)
	}
}

func TestAutoTopupChargesOnceWhenTheAnswerIsLost(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := topupAgency(now, 10_000, 2)
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1"}, lostAnswer: true}
	s, repo, _ := newTopupService(a, 145_000, charger, now)
	ctx := context.Background()

	// The card is charged but the answer never arrives: nothing is added yet and the top-up stays in flight.
	s.autoTopup(ctx, a)
	if len(charger.cents) != 1 || len(repo.recorded) != 0 || repo.statuses()[0] != models.AgencyAutoTopupPending {
		t.Fatalf("after the lost answer: charged %v, recorded %v, claims %v", charger.cents, repo.recorded, repo.statuses())
	}
	// The next pass finds that payment and adds its sends; it does not charge again.
	for range 3 {
		s.autoTopup(ctx, a)
	}
	if len(charger.cents) != 1 || len(repo.recorded) != 1 || len(repo.claims) != 1 || repo.statuses()[0] != models.AgencyAutoTopupPaid {
		t.Fatalf("after recovery: charged %v, recorded %d, claims %v", charger.cents, len(repo.recorded), repo.statuses())
	}
}

func TestAutoTopupPaidInvoiceIsSettledFromItsEvent(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := topupAgency(now, 10_000, 2)
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1"}, lostAnswer: true}
	s, repo, _ := newTopupService(a, 145_000, charger, now)
	ctx := context.Background()
	s.autoTopup(ctx, a)
	// The owner turns it off before anything else runs; the money was taken, so the sends are still owed.
	a.AutoTopupEnabled = false
	invoiceID := repo.claims[0].StripeInvoiceID
	for range 2 {
		if err := s.SettleAutoTopupInvoice(ctx, invoiceID); err != nil {
			t.Fatal(err)
		}
	}
	if len(repo.recorded) != 1 || len(charger.cents) != 1 || repo.statuses()[0] != models.AgencyAutoTopupPaid {
		t.Fatalf("recorded %d, charged %v, claims %v", len(repo.recorded), charger.cents, repo.statuses())
	}
	if err := s.SettleAutoTopupInvoice(ctx, "in_unknown"); err != nil {
		t.Fatalf("an invoice that is not a top-up: %v", err)
	}
}

func TestAutoTopupTurnedOffCallsOffAnUnpaidTopup(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := topupAgency(now, 10_000, 2)
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1"}, failBefore: true}
	s, repo, actor := newTopupService(a, 145_000, charger, now)
	ctx := context.Background()

	s.autoTopup(ctx, a)
	if len(charger.cents) != 0 || repo.statuses()[0] != models.AgencyAutoTopupPending {
		t.Fatalf("after the failure: charged %v, claims %v", charger.cents, repo.statuses())
	}
	if _, xerr := s.SetAutoTopup(ctx, a.ID, actor, false, 10_000, 2); xerr != nil {
		t.Fatal(xerr)
	}
	s.autoTopup(ctx, a)
	if len(charger.cents) != 0 || len(repo.recorded) != 0 || repo.statuses()[0] != models.AgencyAutoTopupCancelled {
		t.Fatalf("turned off: charged %v, recorded %v, claims %v", charger.cents, repo.recorded, repo.statuses())
	}
}

func TestAutoTopupThatStaysUnsettledIsToldOnceAndNeverGuessed(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := topupAgency(now, 10_000, 2)
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1"}, down: true}
	s, repo, actor := newTopupService(a, 145_000, charger, now)
	note := &told{}
	s.WireNotifier(note)
	repo.members = []models.AgencyMember{{UserID: actor, Role: models.AgencyRoleOwner}}
	ctx := context.Background()

	// Within the first hours it is only tried again.
	for range 3 {
		s.autoTopup(ctx, a)
	}
	if len(note.titles) != 0 || len(repo.claims) != 1 {
		t.Fatalf("early: told %v, claims %v", note.titles, repo.statuses())
	}
	// Still unsettled much later, but nobody can be looked up to tell: the notice is owed, not spent.
	s.now = func() time.Time { return now.Add(models.AgencyAutoTopupStuckAfter + time.Minute) }
	repo.membersDown = true
	s.autoTopup(ctx, a)
	if len(note.titles) != 0 || len(repo.stuck) != 0 {
		t.Fatalf("told %v with nobody to tell, marked %v", note.titles, repo.stuck)
	}
	repo.membersDown = false
	// Then the owner is told once, and it stays in flight so nothing else is bought.
	for range 3 {
		s.autoTopup(ctx, a)
	}
	if len(note.titles) != 1 || len(repo.claims) != 1 || repo.statuses()[0] != models.AgencyAutoTopupPending || len(charger.cents) != 0 {
		t.Fatalf("late: told %v, claims %v, charged %v", note.titles, repo.statuses(), charger.cents)
	}
}

func TestAutoTopupTurnedOnAgainAfterARefusalChargesAfresh(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := topupAgency(now, 10_000, 2)
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1"}, declined: true}
	s, repo, actor := newTopupService(a, 145_000, charger, now)
	note := &told{}
	s.WireNotifier(note)
	repo.members = []models.AgencyMember{{UserID: actor, Role: models.AgencyRoleOwner}}
	ctx := context.Background()

	s.autoTopup(ctx, a)
	// The agency has no client workspace; its owner is told all the same.
	if a.AutoTopupEnabled || len(note.titles) != 1 || note.orgs[0] != nil {
		t.Fatalf("refused: enabled=%v told=%v", a.AutoTopupEnabled, note.titles)
	}
	// A new card, turned on again in the same period: a new top-up, not the refused one over again.
	charger.declined = false
	view, xerr := s.SetAutoTopup(ctx, a.ID, actor, true, 10_000, 2)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if !view.Enabled || view.BoughtThisPeriod != 1 || len(repo.recorded) != 1 || len(repo.claims) != 2 || repo.claims[0].ID == repo.claims[1].ID {
		t.Fatalf("view %+v, recorded %d, claims %v", view, len(repo.recorded), repo.statuses())
	}
	if got := repo.statuses(); got[0] != models.AgencyAutoTopupWasRefused || got[1] != models.AgencyAutoTopupPaid {
		t.Fatalf("claims: %v", got)
	}
}

func TestSetAutoTopupNeedsACardAndSaneLimits(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	a := activeAgency(now)
	a.StripeCustomerID = strp("cus_1")
	charger := &fakeCharger{}
	s, _, actor := newTopupService(a, 0, charger, now)

	if _, xerr := s.SetAutoTopup(ctx, a.ID, actor, true, 10_000, 2); xerr == nil || xerr.Identifier != "agency_no_payment_method" {
		t.Fatalf("no card: got %v", xerr)
	}
	charger.card = &SavedCard{ID: "pm_1", Brand: "visa", Last4: "4242"}
	for _, c := range [][2]int{{-1, 2}, {10_000, 0}, {10_000, 21}} {
		if _, xerr := s.SetAutoTopup(ctx, a.ID, actor, true, c[0], c[1]); xerr == nil || xerr.Identifier != "invalid_auto_topup" {
			t.Fatalf("threshold %d, packs %d: got %v", c[0], c[1], xerr)
		}
	}
	view, xerr := s.SetAutoTopup(ctx, a.ID, actor, true, 10_000, 3)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if !view.Enabled || view.ThresholdSends != 10_000 || view.MaxPacks != 3 || view.Card == nil || view.Card.Last4 != "4242" || view.PackCents != 8_500 || !view.Available {
		t.Fatalf("view: %+v", view)
	}
	// Turning it off needs no card.
	charger.card = nil
	if view, xerr := s.SetAutoTopup(ctx, a.ID, actor, false, 10_000, 3); xerr != nil || view.Enabled {
		t.Fatalf("off: %+v %v", view, xerr)
	}
}

// aiLedger is the shared AI balance in memory: what is left and what the automatic top-up added.
type aiLedger struct {
	fakeAI
	left    models.Micro
	added   []models.Micro
	periods []time.Time
	refs    map[string]bool
}

func (l *aiLedger) Ledger(context.Context, uuid.UUID) (models.AgencyAILedger, error) {
	// A ledger already in the current period, so the plan's allowance is not refilled over it.
	far := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	return models.AgencyAILedger{Purchased: l.left, PeriodStart: &far}, nil
}

func (l *aiLedger) GrantAutoPurchased(_ context.Context, _ uuid.UUID, amount models.Micro, p models.AgencyAIPeriod, ref string) (bool, error) {
	if l.refs[ref] {
		return false, nil
	}
	if l.refs == nil {
		l.refs = map[string]bool{}
	}
	l.refs[ref] = true
	l.added = append(l.added, amount)
	l.periods = append(l.periods, p.Start)
	l.left += amount
	return true, nil
}

// aiTopupRepo is topupRepo plus the AI balance's own settings.
type aiTopupRepo struct {
	*topupRepo
	aiDisabled string
}

func (r *aiTopupRepo) SetAIAutoTopup(_ context.Context, _ uuid.UUID, enabled bool, threshold, amount, limit models.Micro) error {
	a := r.agency
	a.AIAutoTopupEnabled, a.AIAutoTopupThreshold, a.AIAutoTopupAmount, a.AIAutoTopupLimit = enabled, threshold, amount, limit
	if enabled {
		a.AIAutoTopupFailure = nil
	}
	return nil
}

func (r *aiTopupRepo) DisableAIAutoTopup(_ context.Context, _ uuid.UUID, failure string) error {
	r.aiDisabled = failure
	r.agency.AIAutoTopupEnabled = false
	return nil
}

func newAITopupService(a *models.Agency, left models.Micro, charger *fakeCharger, now time.Time) (*Service, *aiTopupRepo, *aiLedger, uuid.UUID) {
	actor := uuid.New()
	repo := &aiTopupRepo{topupRepo: &topupRepo{operatorRepo: &operatorRepo{
		fakeRepo: &fakeRepo{agency: a, grants: map[uuid.UUID]uuid.UUID{}},
		staff:    map[uuid.UUID]bool{actor: true}, untils: map[uuid.UUID]*time.Time{},
	}}}
	s := NewService(repo, operatorOrgs{}, operatorUsers{}, operatorSubs{}, fakePlans{pool: intp(150_000)})
	s.billing = true
	s.now = func() time.Time { return now }
	s.charger = charger
	ledger := &aiLedger{left: left}
	s.WireAIBalance(ledger, true)
	return s, repo, ledger, actor
}

const usd = models.MicroPerUSD

func aiTopupAgency(now time.Time, threshold, amount, limit models.Micro) *models.Agency {
	a := activeAgency(now)
	a.StripeCustomerID, a.StripeSubscriptionID = strp("cus_1"), strp("sub_1")
	a.AIAutoTopupEnabled, a.AIAutoTopupThreshold, a.AIAutoTopupAmount, a.AIAutoTopupLimit = true, threshold, amount, limit
	return a
}

func TestAIAutoTopupAddsTheAmountUnderTheThreshold(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := aiTopupAgency(now, 5*usd, 25*usd, 100*usd)
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1"}}
	s, _, ledger, _ := newAITopupService(a, 2*usd, charger, now)
	ctx := context.Background()

	s.autoTopupAI(ctx, a)
	if len(ledger.added) != 1 || ledger.added[0] != 25*usd || len(charger.cents) != 1 || charger.cents[0] != 2_500 {
		t.Fatalf("added %v, charged %v", ledger.added, charger.cents)
	}
	// $27 is left now, above the $5 threshold.
	s.autoTopupAI(ctx, a)
	if len(charger.claims) != 1 {
		t.Fatalf("a balance above its threshold was charged again: %v", charger.claims)
	}
}

func TestAIAutoTopupNeverPassesThePeriodsLimit(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	// $25 at a time with a $60 limit: two fit, a third would pass it.
	a := aiTopupAgency(now, 5*usd, 25*usd, 60*usd)
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1"}}
	s, _, ledger, _ := newAITopupService(a, 0, charger, now)
	ctx := context.Background()
	for range 5 {
		ledger.left = 0
		s.autoTopupAI(ctx, a)
	}
	if len(ledger.added) != 2 || len(charger.claims) != 2 || charger.claims[0] == charger.claims[1] {
		t.Fatalf("added %v with charges %v, want two top-ups under the $60 limit", ledger.added, charger.claims)
	}
}

func TestAIAutoTopupChargesOnceWhenTheAnswerIsLost(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := aiTopupAgency(now, 5*usd, 25*usd, 100*usd)
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1"}, lostAnswer: true}
	s, repo, ledger, _ := newAITopupService(a, 2*usd, charger, now)
	ctx := context.Background()
	s.autoTopupAI(ctx, a)
	if len(charger.cents) != 1 || len(ledger.added) != 0 {
		t.Fatalf("after the lost answer: charged %v, added %v", charger.cents, ledger.added)
	}
	// Still under the threshold, but the top-up in flight is finished before any other is bought.
	for range 3 {
		s.autoTopupAI(ctx, a)
	}
	if len(charger.cents) != 1 || len(ledger.added) != 1 || len(repo.claims) != 1 {
		t.Fatalf("after recovery: charged %v, added %v, claims %v", charger.cents, ledger.added, repo.statuses())
	}
}

func TestAISpendChecksAreCoalesced(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := aiTopupAgency(now, 5*usd, 25*usd, 100*usd)
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1"}}
	s, repo, ledger, _ := newAITopupService(a, 2*usd, charger, now)
	org := uuid.New()
	repo.client = &models.AgencyClient{OrganizationID: org}
	// A check already running: every charge that arrives leaves one follow-up and returns at once.
	check := &aiCheck{running: true}
	s.aiChecks.Store(a.ID, check)
	for range 50 {
		s.OnAgencyAISpend(org)
	}
	if !check.again || len(charger.cents) != 0 {
		t.Fatalf("again=%v charged=%v", check.again, charger.cents)
	}
	check.running, check.again = false, false
	s.OnAgencyAISpend(org)
	if len(ledger.added) != 1 || check.running {
		t.Fatalf("added %v running=%v", ledger.added, check.running)
	}
}

func TestAIAutoTopupTurnsItselfOffWhenTheCardRefuses(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := aiTopupAgency(now, 5*usd, 25*usd, 100*usd)
	s, repo, ledger, _ := newAITopupService(a, 0, &fakeCharger{card: &SavedCard{ID: "pm_1"}, declined: true}, now)
	s.autoTopupAI(context.Background(), a)
	if repo.aiDisabled != models.AgencyAutoTopupDeclined || a.AIAutoTopupEnabled || len(ledger.added) != 0 {
		t.Fatalf("disabled=%q enabled=%v added=%v", repo.aiDisabled, a.AIAutoTopupEnabled, ledger.added)
	}
}

func TestSetAIAutoTopupChecksItsAmounts(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	a := activeAgency(now)
	a.StripeCustomerID = strp("cus_1")
	charger := &fakeCharger{card: &SavedCard{ID: "pm_1", Brand: "visa", Last4: "4242"}}
	s, _, _, actor := newAITopupService(a, 50*usd, charger, now)

	for _, c := range [][3]models.Micro{{5 * usd, 2 * usd, 100 * usd}, {5 * usd, 25 * usd, 10 * usd}, {-1, 25 * usd, 100 * usd}, {5 * usd, 6_000 * usd, 10_000 * usd}} {
		if _, xerr := s.SetAIAutoTopup(ctx, a.ID, actor, true, c[0], c[1], c[2]); xerr == nil || xerr.Identifier != "invalid_ai_auto_topup" {
			t.Fatalf("threshold %d amount %d limit %d: got %v", c[0], c[1], c[2], xerr)
		}
	}
	view, xerr := s.SetAIAutoTopup(ctx, a.ID, actor, true, 5*usd, 25*usd, 100*usd)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if !view.Enabled || view.ThresholdUSD != 5 || view.AmountUSD != 25 || view.LimitUSD != 100 || view.Card == nil || !view.Available || view.AddedThisPeriodUSD != 0 {
		t.Fatalf("view: %+v", view)
	}
	charger.card = nil
	if _, xerr := s.SetAIAutoTopup(ctx, a.ID, actor, true, 5*usd, 25*usd, 100*usd); xerr == nil || xerr.Identifier != "agency_no_payment_method" {
		t.Fatalf("no card: got %v", xerr)
	}
}
