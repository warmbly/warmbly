// Package planoffer holds the operator's offers that let one workspace buy a private plan.
package planoffer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
)

// ErrOfferRequired refuses a private plan to a workspace without an active offer for it.
var ErrOfferRequired = errx.NewWithIdentifier(errx.Forbidden, "plan_offer_required",
	"This plan is available by invitation only. Ask Warmbly to offer it to this workspace.")

// Service manages plan offers.
type Service interface {
	ListOffers(ctx context.Context, orgID uuid.UUID) ([]*models.PlanOffer, *errx.Error)
	// ActiveOffers lists offers still open, each with its plan.
	ActiveOffers(ctx context.Context, orgID uuid.UUID) ([]*models.PlanOffer, *errx.Error)
	// ActiveOffer returns the open offer for one plan, or nil.
	ActiveOffer(ctx context.Context, orgID, planID uuid.UUID) (*models.PlanOffer, *errx.Error)
	GrantOffer(ctx context.Context, orgID, planID, adminID uuid.UUID, reason string, expiresAt *time.Time) (*models.PlanOffer, *errx.Error)
	WithdrawOffer(ctx context.Context, orgID, planID uuid.UUID) *errx.Error
	// AuthorizePurchase refuses a plan the workspace may not buy through the generic checkout.
	AuthorizePurchase(ctx context.Context, orgID uuid.UUID, plan *models.Plan) *errx.Error
	// AuthorizePrice resolves a Stripe price to its plan and applies AuthorizePurchase.
	AuthorizePrice(ctx context.Context, orgID uuid.UUID, priceID string) *errx.Error
	// AuthorizePlanID loads a plan and applies AuthorizePurchase.
	AuthorizePlanID(ctx context.Context, orgID, planID uuid.UUID) *errx.Error
}

type service struct {
	offers repository.PlanOfferRepository
	plans  repository.PlanRepository
	now    func() time.Time
}

// New returns the plan offer service.
func New(offers repository.PlanOfferRepository, plans repository.PlanRepository) Service {
	return &service{offers: offers, plans: plans, now: time.Now}
}

func internalErr(err error, msg string) *errx.Error {
	errs.CaptureException(fmt.Errorf("%s: %w", msg, err))
	return errx.InternalError()
}

func (s *service) ListOffers(ctx context.Context, orgID uuid.UUID) ([]*models.PlanOffer, *errx.Error) {
	offers, err := s.offers.ListPlanOffers(ctx, orgID)
	if err != nil {
		return nil, internalErr(err, "list plan offers")
	}
	for _, o := range offers {
		if xerr := s.attachPlan(ctx, o); xerr != nil {
			return nil, xerr
		}
	}
	return offers, nil
}

func (s *service) ActiveOffers(ctx context.Context, orgID uuid.UUID) ([]*models.PlanOffer, *errx.Error) {
	all, xerr := s.ListOffers(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}
	now := s.now()
	active := make([]*models.PlanOffer, 0, len(all))
	for _, o := range all {
		if o.Active(now) && o.Plan != nil {
			active = append(active, o)
		}
	}
	return active, nil
}

func (s *service) ActiveOffer(ctx context.Context, orgID, planID uuid.UUID) (*models.PlanOffer, *errx.Error) {
	o, err := s.offers.GetPlanOffer(ctx, orgID, planID)
	if err != nil {
		return nil, internalErr(err, "get plan offer")
	}
	if !o.Active(s.now()) {
		return nil, nil
	}
	if xerr := s.attachPlan(ctx, o); xerr != nil {
		return nil, xerr
	}
	return o, nil
}

func (s *service) attachPlan(ctx context.Context, o *models.PlanOffer) *errx.Error {
	if s.plans == nil {
		return nil
	}
	p, err := s.plans.GetByID(ctx, o.PlanID)
	if err != nil {
		return internalErr(err, "get offered plan")
	}
	o.Plan = p
	return nil
}

func validateGrant(reason string, expiresAt *time.Time, now time.Time) (string, *errx.Error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", errx.New(errx.BadRequest, "a reason is required, so the grant is answerable later")
	}
	if len(reason) > 500 {
		return "", errx.New(errx.BadRequest, "keep the reason under 500 characters")
	}
	if expiresAt != nil && !expiresAt.After(now) {
		return "", errx.New(errx.BadRequest, "the grant would already have expired; leave it open-ended or pick a future date")
	}
	return reason, nil
}

func (s *service) GrantOffer(ctx context.Context, orgID, planID, adminID uuid.UUID, reason string, expiresAt *time.Time) (*models.PlanOffer, *errx.Error) {
	now := s.now()
	reason, xerr := validateGrant(reason, expiresAt, now)
	if xerr != nil {
		return nil, xerr
	}
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		return nil, internalErr(err, "get plan for offer")
	}
	if plan == nil {
		return nil, errx.New(errx.NotFound, "that plan does not exist")
	}
	if plan.Public {
		return nil, errx.New(errx.BadRequest, "that plan is already public; offers are for private plans")
	}
	if planID == models.WarmupPlanID {
		return nil, errx.New(errx.BadRequest, "the Warmup plan has its own checkout and needs no offer")
	}
	admin := adminID
	offer, err := s.offers.UpsertPlanOffer(ctx, &models.PlanOffer{
		OrganizationID: orgID, PlanID: planID, GrantedBy: &admin, Reason: reason, GrantedAt: now.UTC(), ExpiresAt: expiresAt,
	})
	if err != nil {
		if repository.IsForeignKeyViolation(err) {
			return nil, errx.New(errx.NotFound, "that workspace does not exist")
		}
		return nil, internalErr(err, "upsert plan offer")
	}
	offer.Plan = plan
	return offer, nil
}

func (s *service) WithdrawOffer(ctx context.Context, orgID, planID uuid.UUID) *errx.Error {
	if _, err := s.offers.DeletePlanOffer(ctx, orgID, planID); err != nil {
		return internalErr(err, "delete plan offer")
	}
	return nil
}

// AuthorizePurchase holds the invariant that the generic checkout sells public
// plans, and a private plan only to a workspace holding an active offer for it.
func (s *service) AuthorizePurchase(ctx context.Context, orgID uuid.UUID, plan *models.Plan) *errx.Error {
	if plan == nil {
		return errx.New(errx.BadRequest, "that price is not a plan on this instance")
	}
	if plan.Public {
		return nil
	}
	offer, xerr := s.ActiveOffer(ctx, orgID, plan.ID)
	if xerr != nil {
		return xerr
	}
	if offer == nil {
		return ErrOfferRequired
	}
	return nil
}

func (s *service) AuthorizePrice(ctx context.Context, orgID uuid.UUID, priceID string) *errx.Error {
	priceID = strings.TrimSpace(priceID)
	if priceID == "" {
		return errx.New(errx.BadRequest, "price_id is required")
	}
	plan, err := s.plans.GetByStripePriceID(ctx, priceID)
	if err != nil {
		return internalErr(err, "resolve plan for price")
	}
	return s.AuthorizePurchase(ctx, orgID, plan)
}

func (s *service) AuthorizePlanID(ctx context.Context, orgID, planID uuid.UUID) *errx.Error {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		return internalErr(err, "get plan")
	}
	if plan == nil {
		return errx.New(errx.NotFound, "plan not found")
	}
	return s.AuthorizePurchase(ctx, orgID, plan)
}
