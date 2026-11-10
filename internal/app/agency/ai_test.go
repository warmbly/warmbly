package agency

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type aiAccessRepo struct {
	*fakeRepo
	role models.AgencyRole
}

func (r aiAccessRepo) GetMember(_ context.Context, agencyID, userID uuid.UUID) (*models.AgencyMember, error) {
	return &models.AgencyMember{AgencyID: agencyID, UserID: userID, Role: r.role}, nil
}

func (r aiAccessRepo) Get(context.Context, uuid.UUID) (*models.Agency, error) {
	a := *r.agency
	return &a, nil
}

func (r aiAccessRepo) ListClientOrganizations(context.Context, uuid.UUID) ([]models.AgencyClient, error) {
	return []models.AgencyClient{*r.client}, nil
}

type aiPlans struct{ allowance models.Micro }

func (p aiPlans) GetByID(_ context.Context, id uuid.UUID) (*models.Plan, error) {
	return &models.Plan{ID: id, MonthlySendPool: intp(0), AgencyAIAllowance: &p.allowance}, nil
}
func (p aiPlans) GetByStripePriceID(context.Context, string) (*models.Plan, error) { return nil, nil }
func (p aiPlans) List(context.Context, bool) ([]*models.Plan, error)               { return nil, nil }

type fakeAI struct {
	repository.AgencyAIRepository
	short     bool
	transfers []models.AgencyAIPeriod
}

func (f *fakeAI) Transfer(_ context.Context, _, _ uuid.UUID, amount models.Micro, p models.AgencyAIPeriod, _ string, _ uuid.UUID) (*models.AgencyAITransfer, error) {
	f.transfers = append(f.transfers, p)
	if f.short {
		return nil, repository.ErrAgencyAIBalanceShort
	}
	return &models.AgencyAITransfer{Amount: amount}, nil
}

func newAIService(role models.AgencyRole, metered bool, now time.Time) (*Service, *fakeAI) {
	org := uuid.New()
	repo := aiAccessRepo{fakeRepo: &fakeRepo{agency: activeAgency(now), client: &models.AgencyClient{OrganizationID: org}}, role: role}
	s := NewService(repo, nil, nil, nil, aiPlans{allowance: 10 * models.MicroPerUSD})
	s.billing, s.now = true, func() time.Time { return now }
	ai := &fakeAI{}
	s.WireAIBalance(ai, metered)
	return s, ai
}

func identifier(x *errx.Error) string {
	if x == nil {
		return ""
	}
	return x.Identifier
}

func TestAgencyAIBalanceDoesNotApplyWhereAIIsUnmetered(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	s, ai := newAIService(models.AgencyRoleOwner, false, now)
	b, xerr := s.AIBalance(context.Background(), uuid.New(), uuid.New())
	if xerr != nil || !b.Unmetered {
		t.Fatalf("self-host balance: %+v %v", b, xerr)
	}
	if _, xerr := s.TransferAI(context.Background(), uuid.New(), uuid.New(), uuid.New(), 5*models.MicroPerUSD, "k"); identifier(xerr) != "agency_billing_disabled" {
		t.Fatalf("self-host move: %v", xerr)
	}
	if _, xerr := s.AITopup(context.Background(), uuid.New(), uuid.New(), 25*models.MicroPerUSD, "", ""); identifier(xerr) != "agency_billing_disabled" {
		t.Fatalf("self-host purchase: %v", xerr)
	}
	if len(ai.transfers) != 0 {
		t.Fatal("a self-host moved balance")
	}
}

func TestTransferAIChecksRoleAmountAndBalance(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	member, _ := newAIService(models.AgencyRoleMember, true, now)
	if _, xerr := member.TransferAI(ctx, uuid.New(), uuid.New(), uuid.New(), 5*models.MicroPerUSD, ""); identifier(xerr) != "agency_forbidden" {
		t.Fatalf("member: %v", xerr)
	}

	s, ai := newAIService(models.AgencyRoleAdmin, true, now)
	for _, amount := range []models.Micro{0, models.MicroPerUSD / 2, 5_001 * models.MicroPerUSD} {
		if _, xerr := s.TransferAI(ctx, uuid.New(), uuid.New(), uuid.New(), amount, ""); identifier(xerr) != "invalid_ai_amount" {
			t.Fatalf("amount %d: %v", amount, xerr)
		}
	}
	if _, xerr := s.TransferAI(ctx, uuid.New(), uuid.New(), uuid.New(), 5*models.MicroPerUSD, "k"); xerr != nil {
		t.Fatal(xerr)
	}
	if len(ai.transfers) != 1 || ai.transfers[0].Allowance != 10*models.MicroPerUSD {
		t.Fatalf("the move refills to the plan's allowance: %+v", ai.transfers)
	}
	ai.short = true
	_, xerr := s.TransferAI(ctx, uuid.New(), uuid.New(), uuid.New(), 5*models.MicroPerUSD, "k2")
	if identifier(xerr) != "agency_ai_balance_insufficient" || xerr.Code != errx.PaymentRequired {
		t.Fatalf("short balance: %v", xerr)
	}
}
