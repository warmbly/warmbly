package balance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/stripe"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type fakeCredits struct {
	credits.CreditService
	balance models.Micro
	grants  map[string]models.Micro
}

func (f *fakeCredits) GetBalance(context.Context, uuid.UUID) (models.Micro, *errx.Error) {
	return f.balance, nil
}

func (f *fakeCredits) GrantPurchased(_ context.Context, _ uuid.UUID, amount models.Micro, _, key string) (models.Micro, error) {
	if _, done := f.grants[key]; !done {
		f.grants[key] = amount
		f.balance += amount
	}
	return f.balance, nil
}

type fakeSettings struct {
	repository.AISettingsRepository
	cfg      *models.AISpendSettings
	disabled string
}

func (f *fakeSettings) Get(context.Context, uuid.UUID) (*models.AISpendSettings, error) {
	return f.cfg, nil
}
func (f *fakeSettings) RearmNotices(context.Context, uuid.UUID) error { return nil }
func (f *fakeSettings) UpsertAutoReload(_ context.Context, _ uuid.UUID, enabled bool, threshold, amount, limit models.Micro) (*models.AISpendSettings, error) {
	f.cfg.AutoReloadEnabled, f.cfg.AutoReloadThreshold, f.cfg.AutoReloadAmount, f.cfg.AutoReloadMonthlyMax = enabled, threshold, amount, limit
	return f.cfg, nil
}
func (f *fakeSettings) DisableAutoReload(_ context.Context, _ uuid.UUID, code string) (bool, error) {
	if !f.cfg.AutoReloadEnabled {
		return false, nil
	}
	f.cfg.AutoReloadEnabled, f.disabled = false, code
	return true, nil
}

type fakeCharges struct {
	repository.SavedCardChargeRepository
	rows     map[uuid.UUID]*models.SavedCardCharge
	reloaded models.Micro
}

func (f *fakeCharges) Pending(_ context.Context, orgID uuid.UUID) (*models.SavedCardCharge, error) {
	for _, c := range f.rows {
		if c.OrgID == orgID && c.Status == "pending" {
			cp := *c
			return &cp, nil
		}
	}
	return nil, nil
}
func (f *fakeCharges) Create(_ context.Context, c *models.SavedCardCharge) (*models.SavedCardCharge, error) {
	c.ID, c.Status = uuid.New(), "pending"
	f.rows[c.ID] = c
	cp := *c
	return &cp, nil
}
func (f *fakeCharges) Get(_ context.Context, id uuid.UUID) (*models.SavedCardCharge, error) {
	cp := *f.rows[id]
	return &cp, nil
}
func (f *fakeCharges) BumpTries(_ context.Context, id uuid.UUID) (int, error) {
	f.rows[id].Tries++
	f.rows[id].RetryAt = nil
	return f.rows[id].Tries, nil
}
func (f *fakeCharges) Snooze(_ context.Context, id uuid.UUID, at time.Time, code string) error {
	f.rows[id].RetryAt, f.rows[id].FailureCode = &at, code
	return nil
}
func (f *fakeCharges) MarkSucceeded(_ context.Context, id uuid.UUID) (bool, error) {
	if f.rows[id].Status != "pending" {
		return false, nil
	}
	f.rows[id].Status = "succeeded"
	return true, nil
}
func (f *fakeCharges) MarkFailed(_ context.Context, id uuid.UUID, code string) error {
	f.rows[id].Status, f.rows[id].FailureCode = "failed", code
	return nil
}
func (f *fakeCharges) ReloadedSince(context.Context, uuid.UUID, time.Time) (models.Micro, error) {
	return f.reloaded, nil
}
func (f *fakeCharges) SetPaymentMethod(_ context.Context, id uuid.UUID, pm string) error {
	f.rows[id].PaymentMethodID = pm
	return nil
}

type fakeSubs struct {
	repository.SubscriptionRepository
}

func (fakeSubs) GetByOrganizationID(_ context.Context, orgID uuid.UUID) (*models.Subscription, error) {
	return &models.Subscription{OrganizationID: orgID, StripeCustomerID: "cus_1"}, nil
}

type fakeCard struct {
	results []stripe.SavedCardResult
	charged []models.Micro
	voided  []string
	// fail answers every charge with this error, as a Stripe outage would.
	fail error
}

func (f *fakeCard) ChargeSavedCard(_ context.Context, c *models.SavedCardCharge, _ int, _ bool) (*stripe.SavedCardResult, error) {
	f.charged = append(f.charged, c.Amount)
	if f.fail != nil {
		return nil, f.fail
	}
	r := f.results[0]
	f.results = f.results[1:]
	return &r, nil
}
func (f *fakeCard) VoidChargeInvoice(_ context.Context, id string) { f.voided = append(f.voided, id) }
func (f *fakeCard) DefaultPaymentMethod(context.Context, string) (*stripe.PaymentMethodSummary, error) {
	return &stripe.PaymentMethodSummary{ID: "pm_1", Brand: "visa", Last4: "4242"}, nil
}

type fakeNotifier struct{ titles []string }

func (f *fakeNotifier) NotifyOrg(_ context.Context, _ uuid.UUID, _ models.OrganizationPermission, _ uuid.UUID, _ models.NotificationCategory, title, _, _ string, _ map[string]any, _ string) {
	f.titles = append(f.titles, title)
}

func newTestService(balance models.Micro, results ...stripe.SavedCardResult) (*Service, *fakeCredits, *fakeSettings, *fakeCharges, *fakeCard, *fakeNotifier) {
	cr := &fakeCredits{balance: balance, grants: map[string]models.Micro{}}
	cfg := credits.DefaultSpendSettings(uuid.Nil)
	cfg.AutoReloadEnabled = true
	st := &fakeSettings{cfg: cfg}
	ch := &fakeCharges{rows: map[uuid.UUID]*models.SavedCardCharge{}}
	card := &fakeCard{results: results}
	n := &fakeNotifier{}
	return New(cr, st, ch, fakeSubs{}, card, nil, nil, n, nil), cr, st, ch, card, n
}

func TestReloadAddsThePaidAmountOnce(t *testing.T) {
	s, cr, _, ch, card, _ := newTestService(2*models.MicroPerUSD, stripe.SavedCardResult{Paid: true, InvoiceID: "in_1"})
	org := uuid.New()
	if err := s.Reload(context.Background(), org); err != nil {
		t.Fatal(err)
	}
	if len(card.charged) != 1 || card.charged[0] != credits.DefaultReloadAmount {
		t.Fatalf("charged %v, want one default reload", card.charged)
	}
	var id uuid.UUID
	for k := range ch.rows {
		id = k
	}
	// The webhook reporting the same invoice paid adds nothing more.
	if err := s.SettleChargeInvoice(context.Background(), id, "in_1"); err != nil {
		t.Fatal(err)
	}
	if cr.balance != 27*models.MicroPerUSD {
		t.Fatalf("balance = %v, want $27", cr.balance.USD())
	}
	// Above the threshold now: nothing further is charged.
	if err := s.Reload(context.Background(), org); err != nil || len(card.charged) != 1 {
		t.Fatalf("second reload charged again: %v, %v", card.charged, err)
	}
}

func TestSoftDeclineWaitsAndKeepsReloadOn(t *testing.T) {
	s, cr, st, ch, _, n := newTestService(0, stripe.SavedCardResult{InvoiceID: "in_1", Declined: models.ReloadCardDeclined, Soft: true})
	if err := s.Reload(context.Background(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	for _, c := range ch.rows {
		if c.Status != "pending" || c.RetryAt == nil || time.Until(*c.RetryAt) < 50*time.Minute {
			t.Fatalf("charge = %+v, want pending with a retry in an hour", c)
		}
	}
	if !st.cfg.AutoReloadEnabled || cr.balance != 0 || len(n.titles) != 0 {
		t.Fatal("a soft decline turned reload off, added money or notified")
	}
}

func TestHardDeclineTurnsReloadOffAndTellsBilling(t *testing.T) {
	s, cr, st, ch, card, n := newTestService(0, stripe.SavedCardResult{InvoiceID: "in_1", Declined: models.ReloadAuthenticationRequired})
	if err := s.Reload(context.Background(), uuid.New()); err == nil {
		t.Fatal("a hard decline returned no error")
	}
	for _, c := range ch.rows {
		if c.Status != "failed" {
			t.Fatalf("charge = %+v, want failed", c)
		}
	}
	if st.cfg.AutoReloadEnabled || st.disabled != models.ReloadAuthenticationRequired {
		t.Fatalf("reload still on (%v) or wrong reason %q", st.cfg.AutoReloadEnabled, st.disabled)
	}
	if len(card.voided) != 1 || len(n.titles) != 1 || cr.balance != 0 {
		t.Fatalf("voided %v, notices %v, balance %v", card.voided, n.titles, cr.balance)
	}
}

func TestReloadNearTheMonthlyLimitTakesWhatIsLeft(t *testing.T) {
	s, _, _, ch, card, _ := newTestService(0, stripe.SavedCardResult{Paid: true, InvoiceID: "in_1"})
	ch.reloaded = credits.DefaultReloadMonthlyLimit - 10*models.MicroPerUSD
	if err := s.Reload(context.Background(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	if len(card.charged) != 1 || card.charged[0] != 10*models.MicroPerUSD {
		t.Fatalf("charged %v, want the $10 the limit has left", card.charged)
	}
	ch.reloaded = credits.DefaultReloadMonthlyLimit - models.MicroPerUSD
	s2, _, _, ch2, card2, _ := newTestService(0)
	ch2.reloaded = ch.reloaded
	if err := s2.Reload(context.Background(), uuid.New()); err != nil || len(card2.charged) != 0 {
		t.Fatalf("charged %v under the minimum top-up", card2.charged)
	}
}

func onlyCharge(t *testing.T, ch *fakeCharges) *models.SavedCardCharge {
	t.Helper()
	if len(ch.rows) != 1 {
		t.Fatalf("charges = %d, want 1", len(ch.rows))
	}
	for _, c := range ch.rows {
		return c
	}
	return nil
}

func TestTurningReloadOffCancelsTheSnoozedReload(t *testing.T) {
	s, _, _, ch, card, _ := newTestService(0, stripe.SavedCardResult{InvoiceID: "in_1", Declined: models.ReloadCardDeclined, Soft: true})
	org := uuid.New()
	if err := s.Reload(context.Background(), org); err != nil {
		t.Fatal(err)
	}
	c := onlyCharge(t, ch)
	c.InvoiceID = "in_1"
	if _, err := s.SetAutoReload(context.Background(), org, false, credits.DefaultReloadThreshold, credits.DefaultReloadAmount, credits.DefaultReloadMonthlyLimit); err != nil {
		t.Fatal(err)
	}
	if c.Status != "failed" || c.FailureCode != models.ChargeCanceled || len(card.voided) != 1 || card.voided[0] != "in_1" {
		t.Fatalf("charge = %+v, voided %v; want canceled with its invoice voided", c, card.voided)
	}
}

func TestDueReloadIsNotChargedOnceUnwanted(t *testing.T) {
	for name, change := range map[string]func(*fakeCredits, *fakeSettings){
		"reload off":         func(_ *fakeCredits, st *fakeSettings) { st.cfg.AutoReloadEnabled = false },
		"balance over again": func(cr *fakeCredits, st *fakeSettings) { cr.balance = st.cfg.AutoReloadThreshold },
	} {
		t.Run(name, func(t *testing.T) {
			s, cr, st, ch, card, _ := newTestService(0, stripe.SavedCardResult{InvoiceID: "in_1", Declined: models.ReloadCardDeclined, Soft: true})
			org := uuid.New()
			if err := s.Reload(context.Background(), org); err != nil {
				t.Fatal(err)
			}
			c := onlyCharge(t, ch)
			c.InvoiceID, c.RetryAt = "in_1", nil
			change(cr, st)
			// The sweep resuming the due retry.
			due := *c
			if err := s.run(context.Background(), &due, true); err != nil {
				t.Fatal(err)
			}
			if len(card.charged) != 1 || c.Status != "failed" || c.FailureCode != models.ChargeCanceled || len(card.voided) != 1 {
				t.Fatalf("charged %v, charge %+v, voided %v; want no second charge and a canceled reload", card.charged, c, card.voided)
			}
		})
	}
}

func TestTopUpThatStripeDidNotAnswerIsStillProcessing(t *testing.T) {
	s, cr, _, ch, card, _ := newTestService(0)
	card.fail = errors.New("stripe: connection reset")
	res, err := s.TopUp(context.Background(), uuid.New(), 25*models.MicroPerUSD)
	if err != nil || res.Status != "processing" {
		t.Fatalf("TopUp = %+v, %v; want processing", res, err)
	}
	if c := onlyCharge(t, ch); c.Status != "pending" || cr.balance != 0 {
		t.Fatalf("charge = %+v, balance %v; want pending for the sweep", c, cr.balance)
	}
}

func TestChargeStripeKeepsFailingIsFailedAfterMaxTries(t *testing.T) {
	s, _, st, ch, card, n := newTestService(0)
	card.fail = errors.New("stripe: 500")
	org := uuid.New()
	if err := s.Reload(context.Background(), org); err == nil {
		t.Fatal("a failed Stripe call returned no error")
	}
	c := onlyCharge(t, ch)
	for c.Status == "pending" && c.Tries < maxTries+2 {
		due := *c
		_ = s.run(context.Background(), &due, true)
	}
	if c.Status != "failed" || c.Tries != maxTries || c.FailureCode != models.ReloadUnavailable {
		t.Fatalf("charge = %+v; want failed as unavailable after %d tries", c, maxTries)
	}
	if st.cfg.AutoReloadEnabled || st.disabled != models.ReloadUnavailable || len(n.titles) != 1 {
		t.Fatalf("reload on = %v (%q), notices %v; want it off and billing told", st.cfg.AutoReloadEnabled, st.disabled, n.titles)
	}
}
