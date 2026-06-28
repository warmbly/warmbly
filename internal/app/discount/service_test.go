package discount

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// ---- helpers ----

func iptr(i int) *int             { return &i }
func fptr(f float64) *float64     { return &f }
func sptr(s string) *string       { return &s }
func tptr(t time.Time) *time.Time { return &t }

func percentCode(pct int) *models.DiscountCode {
	return &models.DiscountCode{
		ID:                uuid.New(),
		Code:              "SAVE",
		Type:              models.DiscountTypePercent,
		PercentOff:        iptr(pct),
		Duration:          models.DiscountDurationOnce,
		PerAccountLimit:   1,
		AppliesToAllPlans: true,
		Status:            models.DiscountCodeStatusActive,
	}
}

// ---- fakes ----

type fakeCodeRepo struct {
	byCode map[string]*models.DiscountCode
	byID   map[uuid.UUID]*models.DiscountCode
}

func newFakeCodeRepo(codes ...*models.DiscountCode) *fakeCodeRepo {
	r := &fakeCodeRepo{byCode: map[string]*models.DiscountCode{}, byID: map[uuid.UUID]*models.DiscountCode{}}
	for _, c := range codes {
		r.byCode[c.Code] = c
		r.byID[c.ID] = c
	}
	return r
}

func (r *fakeCodeRepo) Create(context.Context, *models.DiscountCode) error { return nil }
func (r *fakeCodeRepo) Update(context.Context, *models.DiscountCode) error { return nil }
func (r *fakeCodeRepo) GetByID(_ context.Context, id uuid.UUID) (*models.DiscountCode, error) {
	return r.byID[id], nil
}
func (r *fakeCodeRepo) GetByCode(_ context.Context, code string) (*models.DiscountCode, error) {
	return r.byCode[code], nil
}
func (r *fakeCodeRepo) List(context.Context, *models.AdminDiscountSearch) (*models.AdminDiscountsResult, error) {
	return &models.AdminDiscountsResult{}, nil
}
func (r *fakeCodeRepo) Delete(context.Context, uuid.UUID) error { return nil }

type fakeRedRepo struct {
	orgCount   int
	reserveErr error
}

func (r *fakeRedRepo) ReserveRedemption(_ context.Context, red *models.DiscountRedemption, _ *int, _ int) error {
	return r.reserveErr
}
func (r *fakeRedRepo) AttachStripeRefs(context.Context, uuid.UUID, *string, *string) error {
	return nil
}
func (r *fakeRedRepo) MarkAppliedBySession(context.Context, string, *uuid.UUID) error { return nil }
func (r *fakeRedRepo) CancelBySession(context.Context, string) error                  { return nil }
func (r *fakeRedRepo) CancelByID(context.Context, uuid.UUID) error                    { return nil }
func (r *fakeRedRepo) CountActiveByCodeAndOrg(context.Context, uuid.UUID, uuid.UUID) (int, error) {
	return r.orgCount, nil
}
func (r *fakeRedRepo) ListByCode(context.Context, uuid.UUID, int, int) (*models.AdminDiscountRedemptionsResult, error) {
	return &models.AdminDiscountRedemptionsResult{}, nil
}
func (r *fakeRedRepo) ListByOrganization(context.Context, uuid.UUID, int) ([]models.DiscountRedemption, error) {
	return nil, nil
}

type fakePlanRepo struct{ plan *models.Plan }

func (r *fakePlanRepo) Create(context.Context, *models.Plan) error { return nil }
func (r *fakePlanRepo) Update(context.Context, *models.Plan) error { return nil }
func (r *fakePlanRepo) GetByID(_ context.Context, id uuid.UUID) (*models.Plan, error) {
	if r.plan != nil && r.plan.ID == id {
		return r.plan, nil
	}
	return r.plan, nil
}
func (r *fakePlanRepo) GetByStripePriceID(context.Context, string) (*models.Plan, error) {
	return r.plan, nil
}
func (r *fakePlanRepo) GetByStripeProductID(context.Context, string) (*models.Plan, error) {
	return r.plan, nil
}
func (r *fakePlanRepo) List(context.Context, bool) ([]*models.Plan, error) { return nil, nil }
func (r *fakePlanRepo) GetRateLimits(context.Context, uuid.UUID) (*models.PlanRateLimits, error) {
	return nil, nil
}
func (r *fakePlanRepo) SetRateLimits(context.Context, *models.PlanRateLimits) error { return nil }

func newSvc(cr repository.DiscountCodeRepository, rr repository.DiscountRedemptionRepository, pr repository.PlanRepository) DiscountService {
	return NewService(cr, rr, pr, nil)
}

// ---- pure-function tests ----

func TestValidateShape(t *testing.T) {
	cases := []struct {
		name string
		dc   *models.DiscountCode
		ok   bool
	}{
		{"percent valid", &models.DiscountCode{Type: models.DiscountTypePercent, PercentOff: iptr(20), Duration: models.DiscountDurationOnce, PerAccountLimit: 1}, true},
		{"percent out of range", &models.DiscountCode{Type: models.DiscountTypePercent, PercentOff: iptr(0), PerAccountLimit: 1}, false},
		{"percent with amount", &models.DiscountCode{Type: models.DiscountTypePercent, PercentOff: iptr(10), AmountOff: fptr(5), PerAccountLimit: 1}, false},
		{"fixed valid", &models.DiscountCode{Type: models.DiscountTypeFixed, AmountOff: fptr(10), Currency: sptr("usd"), Duration: models.DiscountDurationOnce, PerAccountLimit: 1}, true},
		{"fixed no currency", &models.DiscountCode{Type: models.DiscountTypeFixed, AmountOff: fptr(10), PerAccountLimit: 1}, false},
		{"fixed bad currency", &models.DiscountCode{Type: models.DiscountTypeFixed, AmountOff: fptr(10), Currency: sptr("dollars"), PerAccountLimit: 1}, false},
		{"trial valid", &models.DiscountCode{Type: models.DiscountTypeTrialExtension, TrialExtensionDays: iptr(14), PerAccountLimit: 1}, true},
		{"trial zero", &models.DiscountCode{Type: models.DiscountTypeTrialExtension, TrialExtensionDays: iptr(0), PerAccountLimit: 1}, false},
		{"repeating missing months", &models.DiscountCode{Type: models.DiscountTypePercent, PercentOff: iptr(10), Duration: models.DiscountDurationRepeating, PerAccountLimit: 1}, false},
		{"repeating with months", &models.DiscountCode{Type: models.DiscountTypePercent, PercentOff: iptr(10), Duration: models.DiscountDurationRepeating, DurationInMonths: iptr(3), PerAccountLimit: 1}, true},
		{"per-account zero", &models.DiscountCode{Type: models.DiscountTypePercent, PercentOff: iptr(10), Duration: models.DiscountDurationOnce, PerAccountLimit: 0}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reason := validateShape(c.dc)
			if (reason == "") != c.ok {
				t.Fatalf("validateShape ok=%v, got reason=%q", c.ok, reason)
			}
		})
	}
}

func TestApplyMoneyDiscount(t *testing.T) {
	cases := []struct {
		name string
		base float64
		dc   *models.DiscountCode
		want float64
	}{
		{"20pct of 100", 100, &models.DiscountCode{Type: models.DiscountTypePercent, PercentOff: iptr(20)}, 80},
		{"100pct floors at 0", 50, &models.DiscountCode{Type: models.DiscountTypePercent, PercentOff: iptr(100)}, 0},
		{"fixed 10 off 30", 30, &models.DiscountCode{Type: models.DiscountTypeFixed, AmountOff: fptr(10)}, 20},
		{"fixed over price floors at 0", 5, &models.DiscountCode{Type: models.DiscountTypeFixed, AmountOff: fptr(10)}, 0},
		{"trial unchanged", 99, &models.DiscountCode{Type: models.DiscountTypeTrialExtension, TrialExtensionDays: iptr(7)}, 99},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := applyMoneyDiscount(c.base, c.dc); got != c.want {
				t.Fatalf("applyMoneyDiscount(%v)=%v want %v", c.base, got, c.want)
			}
		})
	}
}

func TestNormalizeCodeAndCurrency(t *testing.T) {
	if got := NormalizeCode("  welcome10 "); got != "WELCOME10" {
		t.Fatalf("NormalizeCode = %q", got)
	}
	if got := normalizeCurrency(sptr("  USD ")); got == nil || *got != "usd" {
		t.Fatalf("normalizeCurrency = %v", got)
	}
	if normalizeCurrency(sptr("   ")) != nil {
		t.Fatalf("blank currency should be nil")
	}
}

// ---- validation flow tests ----

func TestValidatePreview(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()

	t.Run("unknown code", func(t *testing.T) {
		svc := newSvc(newFakeCodeRepo(), &fakeRedRepo{}, &fakePlanRepo{})
		p, xerr := svc.Validate(ctx, org, "NOPE", nil)
		if xerr != nil {
			t.Fatal(xerr)
		}
		if p.Valid || p.Reason == "" {
			t.Fatalf("expected invalid with reason, got %+v", p)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		dc := percentCode(10)
		dc.Status = models.DiscountCodeStatusDisabled
		svc := newSvc(newFakeCodeRepo(dc), &fakeRedRepo{}, &fakePlanRepo{})
		p, _ := svc.Validate(ctx, org, dc.Code, nil)
		if p.Valid {
			t.Fatal("disabled code should be invalid")
		}
	})

	t.Run("expired", func(t *testing.T) {
		dc := percentCode(10)
		dc.ExpiresAt = tptr(time.Now().Add(-time.Hour))
		svc := newSvc(newFakeCodeRepo(dc), &fakeRedRepo{}, &fakePlanRepo{})
		p, _ := svc.Validate(ctx, org, dc.Code, nil)
		if p.Valid {
			t.Fatal("expired code should be invalid")
		}
	})

	t.Run("global cap reached", func(t *testing.T) {
		dc := percentCode(10)
		dc.MaxRedemptions = iptr(5)
		dc.TimesRedeemed = 5
		svc := newSvc(newFakeCodeRepo(dc), &fakeRedRepo{}, &fakePlanRepo{})
		p, _ := svc.Validate(ctx, org, dc.Code, nil)
		if p.Valid {
			t.Fatal("exhausted code should be invalid")
		}
	})

	t.Run("per-account cap reached", func(t *testing.T) {
		dc := percentCode(10)
		svc := newSvc(newFakeCodeRepo(dc), &fakeRedRepo{orgCount: 1}, &fakePlanRepo{})
		p, _ := svc.Validate(ctx, org, dc.Code, nil)
		if p.Valid {
			t.Fatal("already-redeemed code should be invalid")
		}
	})

	t.Run("plan ineligible", func(t *testing.T) {
		dc := percentCode(10)
		dc.AppliesToAllPlans = false
		dc.PlanIDs = []uuid.UUID{uuid.New()}
		other := uuid.New()
		svc := newSvc(newFakeCodeRepo(dc), &fakeRedRepo{}, &fakePlanRepo{})
		p, _ := svc.Validate(ctx, org, dc.Code, &other)
		if p.Valid {
			t.Fatal("plan-ineligible code should be invalid")
		}
	})

	t.Run("valid with price preview", func(t *testing.T) {
		dc := percentCode(10)
		plan := &models.Plan{ID: uuid.New(), Price: 100}
		svc := newSvc(newFakeCodeRepo(dc), &fakeRedRepo{}, &fakePlanRepo{plan: plan})
		p, xerr := svc.Validate(ctx, org, "save", &plan.ID) // lowercase normalizes
		if xerr != nil {
			t.Fatal(xerr)
		}
		if !p.Valid {
			t.Fatalf("expected valid, got reason %q", p.Reason)
		}
		if p.DiscountedAmount == nil || *p.DiscountedAmount != 90 {
			t.Fatalf("discounted = %v want 90", p.DiscountedAmount)
		}
		if p.SavingsAmount == nil || *p.SavingsAmount != 10 {
			t.Fatalf("savings = %v want 10", p.SavingsAmount)
		}
	})
}

func TestValidateForCheckout(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	plan := uuid.New()

	dc := percentCode(10)
	svc := newSvc(newFakeCodeRepo(dc), &fakeRedRepo{}, &fakePlanRepo{})
	got, xerr := svc.ValidateForCheckout(ctx, org, dc.Code, plan)
	if xerr != nil || got == nil {
		t.Fatalf("expected ok, got err=%v code=%v", xerr, got)
	}

	// invalid → errx.BadRequest
	expired := percentCode(10)
	expired.ExpiresAt = tptr(time.Now().Add(-time.Hour))
	svc2 := newSvc(newFakeCodeRepo(expired), &fakeRedRepo{}, &fakePlanRepo{})
	_, xerr = svc2.ValidateForCheckout(ctx, org, expired.Code, plan)
	if xerr == nil || xerr.Code != errx.BadRequest {
		t.Fatalf("expected BadRequest, got %v", xerr)
	}
}

func TestReserveMapsCapErrors(t *testing.T) {
	ctx := context.Background()
	dc := percentCode(10)

	exhausted := newSvc(newFakeCodeRepo(dc), &fakeRedRepo{reserveErr: repository.ErrDiscountExhausted}, &fakePlanRepo{})
	if _, xerr := exhausted.ReservePendingRedemption(ctx, dc, uuid.New(), nil, nil); xerr == nil || xerr.Code != errx.BadRequest {
		t.Fatalf("exhausted: expected BadRequest, got %v", xerr)
	}

	dup := newSvc(newFakeCodeRepo(dc), &fakeRedRepo{reserveErr: repository.ErrDiscountAlreadyRedeemed}, &fakePlanRepo{})
	if _, xerr := dup.ReserveAppliedRedemption(ctx, dc, uuid.New(), nil, nil, nil); xerr == nil || xerr.Code != errx.BadRequest {
		t.Fatalf("already-redeemed: expected BadRequest, got %v", xerr)
	}

	ok := newSvc(newFakeCodeRepo(dc), &fakeRedRepo{}, &fakePlanRepo{})
	if id, xerr := ok.ReservePendingRedemption(ctx, dc, uuid.New(), nil, nil); xerr != nil || id == uuid.Nil {
		t.Fatalf("ok reserve: id=%v err=%v", id, xerr)
	}
}
