package credits

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type overageRepo struct {
	repository.CreditRepository
	ledger    models.CreditLedger
	aiFromBal models.Micro
	debitErr  error
	last      models.CreditDebit
}

func (r *overageRepo) GetBalance(context.Context, uuid.UUID) (*models.CreditLedger, error) {
	l := r.ledger
	return &l, nil
}

func (r *overageRepo) AIFromPurchasedThisMonth(context.Context, uuid.UUID) (models.Micro, error) {
	return r.aiFromBal, nil
}

func (r *overageRepo) AgencyAvailable(context.Context, uuid.UUID, bool) (models.Micro, error) {
	return 0, nil
}

func (r *overageRepo) Debit(_ context.Context, _ uuid.UUID, d models.CreditDebit) (models.Micro, *models.CreditTransaction, bool, error) {
	r.last = d
	if r.debitErr != nil {
		return 0, nil, false, r.debitErr
	}
	return 0, &models.CreditTransaction{Amount: -d.Amount}, false, nil
}

type overageSettings struct {
	repository.AISettingsRepository
	cfg *models.AISpendSettings
}

func (s *overageSettings) Get(context.Context, uuid.UUID) (*models.AISpendSettings, error) {
	return s.cfg, nil
}

func newOverageService(t *testing.T, ledger models.CreditLedger, cfg *models.AISpendSettings) (*creditService, *overageRepo) {
	t.Setenv("BILLING_PROVIDER", "stripe")
	repo := &overageRepo{ledger: ledger}
	return NewService(repo, &overageSettings{cfg: cfg}, nil, nil).(*creditService), repo
}

func TestReserveRefusesAIWithTheWorkspacesReason(t *testing.T) {
	ctx, org := context.Background(), uuid.New()
	usd := models.MicroPerUSD
	limit := 5 * usd

	// Never saved: today's behaviour, purchased money pays once the allowance is gone.
	s, _ := newOverageService(t, models.CreditLedger{Purchased: 10 * usd}, nil)
	if err := s.Reserve(ctx, org, usd); err != nil {
		t.Fatalf("default settings: %v", err)
	}

	off := &models.AISpendSettings{AIOverageEnabled: false}
	s, _ = newOverageService(t, models.CreditLedger{Purchased: 10 * usd}, off)
	if err := s.Reserve(ctx, org, usd); !errors.Is(err, ErrAIAllowanceUsed) || !errors.Is(err, ErrInsufficientCredits) {
		t.Fatalf("overage off: %v", err)
	}
	if code, _ := PaymentRefusal(s.Reserve(ctx, org, usd)); code != "ai_allowance_used" {
		t.Fatalf("code = %q", code)
	}

	capped := &models.AISpendSettings{AIOverageEnabled: true, AIOverageMonthlyLimit: &limit}
	s, repo := newOverageService(t, models.CreditLedger{Purchased: 10 * usd}, capped)
	repo.aiFromBal = 5 * usd
	if err := s.Reserve(ctx, org, usd); !errors.Is(err, ErrAIOverageLimitReached) {
		t.Fatalf("limit reached: %v", err)
	}
	repo.aiFromBal = 3 * usd
	if err := s.Reserve(ctx, org, usd); err != nil {
		t.Fatalf("room left under the limit: %v", err)
	}

	// An empty balance is still the plain refusal.
	s, _ = newOverageService(t, models.CreditLedger{}, capped)
	if code, _ := PaymentRefusal(s.Reserve(ctx, org, usd)); code != "insufficient_credits" {
		t.Fatalf("empty balance code = %q", code)
	}
}

func TestAIDebitsCarryTheSettingAndStoreDebitsDoNot(t *testing.T) {
	ctx, org := context.Background(), uuid.New()
	limit := 5 * models.MicroPerUSD
	s, repo := newOverageService(t, models.CreditLedger{}, &models.AISpendSettings{AIOverageEnabled: false, AIOverageMonthlyLimit: &limit})
	if _, err := s.ChargeAmount(ctx, org, models.MicroPerUSD, "Mailboxes", "store:month:x"); err != nil {
		t.Fatal(err)
	}
	if d := repo.last; !d.PurchasedOnly || d.AllowanceOnly || d.OverageMonthlyLimit != nil || d.AgencyAllowance {
		t.Fatalf("store debit = %+v", d)
	}
	d := s.aiDebit(ctx, org, models.CreditDebit{Amount: models.MicroPerUSD})
	if !d.AllowanceOnly || d.OverageMonthlyLimit == nil || *d.OverageMonthlyLimit != limit {
		t.Fatalf("AI debit = %+v", d)
	}
	repo.debitErr = repository.ErrAllowanceUsed
	if _, err := s.debit(ctx, org, Usage{}, d); !errors.Is(err, ErrAIAllowanceUsed) {
		t.Fatalf("mapped = %v", err)
	}
	repo.debitErr = repository.ErrOverageLimitReached
	if _, err := s.debit(ctx, org, Usage{}, d); !errors.Is(err, ErrAIOverageLimitReached) {
		t.Fatalf("mapped = %v", err)
	}
}
