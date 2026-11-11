package agency

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/checkout/session"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/businesscheckout"
	"github.com/warmbly/warmbly/internal/repository"
)

const purposeAgencyAITopup = "agency_ai_topup"

// Bounds on what one move of agency balance into a client may carry.
const (
	minAITransfer = models.MicroPerUSD
	maxAITransfer = credits.TopUpMax
)

// aiTopUpPresets are the workspace balance's top-up presets, offered for the agency balance too.
var aiTopUpPresets = []int{10, 25, 50, 100, 250}

var errAIUnavailable = errx.NewWithIdentifier(errx.ServiceUnavailable, "agency_billing_disabled",
	"This instance does not charge for AI, so there is no agency balance to manage.")

// WireAIBalance attaches the shared AI balance; metered is false where AI is not charged (a self-host).
func (s *Service) WireAIBalance(repo repository.AgencyAIRepository, metered bool) {
	s.ai, s.aiMetered = repo, metered
}

// aiPeriod is the agency balance's current period and the allowance it refills to.
func (s *Service) aiPeriod(ctx context.Context, a *models.Agency) models.AgencyAIPeriod {
	var allowance *models.Micro
	now := s.now()
	if planID := a.EffectivePlanID(now); planID != nil {
		if p, err := s.plans.GetByID(ctx, *planID); err == nil && p != nil {
			allowance = p.AgencyAIAllowance
		}
	}
	return models.AgencyAIPeriodOf(a, allowance, s.billing, now)
}

func usdPtr(m *models.Micro) *float64 {
	if m == nil {
		return nil
	}
	v := m.USD()
	return &v
}

// AIBalance is the shared AI balance meter for the agency's staff.
func (s *Service) AIBalance(ctx context.Context, agencyID, userID uuid.UUID) (*models.AgencyAIBalance, *errx.Error) {
	a, _, xerr := s.access(ctx, agencyID, userID, false)
	if xerr != nil {
		return nil, xerr
	}
	return s.aiBalance(ctx, a)
}

func (s *Service) aiBalance(ctx context.Context, a *models.Agency) (*models.AgencyAIBalance, *errx.Error) {
	if s.ai == nil || !s.aiMetered {
		return &models.AgencyAIBalance{Unmetered: true, Clients: []models.AgencyAIClient{}}, nil
	}
	period := s.aiPeriod(ctx, a)
	stored, err := s.ai.Ledger(ctx, a.ID)
	if err != nil {
		return nil, internal(err)
	}
	l := stored.In(period)
	use, bought, err := s.ai.PeriodUse(ctx, a.ID, *l.PeriodStart)
	if err != nil {
		return nil, internal(err)
	}
	clients, err := s.repo.ListClientOrganizations(ctx, a.ID)
	if err != nil {
		return nil, internal(err)
	}
	out := &models.AgencyAIBalance{
		PeriodStart: *l.PeriodStart, PeriodEnd: period.End,
		AllowanceUSD: period.Allowance.USD(), AllowanceLeftUSD: l.Allowance.USD(),
		PurchasedUSD: l.Purchased.USD(), BoughtUSD: bought.USD(), BalanceUSD: (l.Allowance + l.Purchased).USD(),
		Clients: []models.AgencyAIClient{},
		TopUp: models.AgencyAITopUpInfo{
			MinUSD: credits.TopUpMin.USD(), MaxUSD: credits.TopUpMax.USD(), PresetsUSD: aiTopUpPresets, DefaultUSD: 25,
		},
	}
	var spent, moved models.Micro
	for _, u := range use {
		spent += u.Spent
		moved += u.Moved
	}
	out.SpentUSD, out.MovedUSD = spent.USD(), moved.USD()
	for _, c := range clients {
		u := use[c.OrganizationID]
		out.Clients = append(out.Clients, models.AgencyAIClient{
			OrganizationID: c.OrganizationID, Name: c.Name, SpentUSD: u.Spent.USD(), MovedUSD: u.Moved.USD(),
			MonthlyAILimitUSD: usdPtr(c.MonthlyAILimit),
		})
	}
	return out, nil
}

// SetClientAILimit caps one client's spend from the agency balance per period; nil removes the cap.
func (s *Service) checkAILimit(limit *models.Micro) *errx.Error {
	if s.ai == nil {
		return errAIUnavailable
	}
	if limit != nil && (*limit < 0 || *limit > credits.ReloadMonthlyLimitMax) {
		return errx.NewWithIdentifier(errx.BadRequest, "invalid_monthly_ai_limit", "The monthly AI cap must be between $0 and $50,000.")
	}
	return nil
}

// CheckClientLimits validates a client cap change before any of it is written, so a request setting both never saves one.
func (s *Service) CheckClientLimits(send *int, setSend bool, ai *models.Micro, setAI bool) *errx.Error {
	if setSend {
		if xerr := checkSendLimit(send); xerr != nil {
			return xerr
		}
	}
	if setAI {
		return s.checkAILimit(ai)
	}
	return nil
}

func (s *Service) SetClientAILimit(ctx context.Context, agencyID, actorID, orgID uuid.UUID, limit *models.Micro) (*models.AgencyClient, *errx.Error) {
	if _, _, xerr := s.access(ctx, agencyID, actorID, true); xerr != nil {
		return nil, xerr
	}
	if xerr := s.checkAILimit(limit); xerr != nil {
		return nil, xerr
	}
	ok, err := s.ai.SetClientAILimit(ctx, agencyID, orgID, limit)
	if err != nil {
		return nil, internal(err)
	}
	if !ok {
		return nil, errClientNotFound
	}
	c, err := s.repo.GetClient(ctx, orgID)
	if err != nil {
		return nil, internal(err)
	}
	return c, nil
}

var errClientNotFound = errx.NewWithIdentifier(errx.NotFound, "agency_client_not_found", "That workspace is not one of this agency's clients.")

// TransferAI moves money from the agency balance into a client workspace's own balance, once per key.
func (s *Service) TransferAI(ctx context.Context, agencyID, actorID, orgID uuid.UUID, amount models.Micro, key string) (*models.AgencyAITransfer, *errx.Error) {
	a, _, xerr := s.access(ctx, agencyID, actorID, true)
	if xerr != nil {
		return nil, xerr
	}
	if s.ai == nil || !s.aiMetered {
		return nil, errAIUnavailable
	}
	if amount < minAITransfer || amount > maxAITransfer {
		return nil, errx.NewWithIdentifier(errx.BadRequest, "invalid_ai_amount", "Enter an amount from $1 to $5,000.")
	}
	t, err := s.ai.Transfer(ctx, agencyID, orgID, amount, s.aiPeriod(ctx, a), key, actorID)
	switch {
	case errors.Is(err, repository.ErrAgencyAIBalanceShort):
		return nil, errx.NewWithIdentifier(errx.PaymentRequired, "agency_ai_balance_insufficient",
			"The agency balance does not hold that much. Buy more under Agency > Billing or move a smaller amount.")
	case errors.Is(err, repository.ErrAgencyAIClientNotFound):
		return nil, errClientNotFound
	case err != nil:
		return nil, internal(err)
	}
	return t, nil
}

// AITopup opens Checkout for money added to the agency balance.
func (s *Service) AITopup(ctx context.Context, agencyID, actorID uuid.UUID, amount models.Micro, successURL, cancelURL string) (string, *errx.Error) {
	if !s.billing || s.ai == nil || !s.aiMetered {
		return "", errBillingDisabled
	}
	a, _, xerr := s.access(ctx, agencyID, actorID, true)
	if xerr != nil {
		return "", xerr
	}
	if _, _, ok := s.grantPlan(a); !ok {
		return "", errx.NewWithIdentifier(errx.BadRequest, "agency_no_subscription", "Choose an agency plan before adding to the AI balance.")
	}
	if amount < credits.TopUpMin || amount > credits.TopUpMax {
		return "", errx.NewWithIdentifier(errx.BadRequest, "invalid_ai_amount", "Enter an amount from $5 to $5,000.")
	}
	cents := amount.Cents()
	meta := map[string]string{
		metaPurpose: purposeAgencyAITopup, metaAgencyID: a.ID.String(), "user_id": actorID.String(),
		"amount_cents": strconv.FormatInt(cents, 10),
	}
	params := &stripe.CheckoutSessionParams{
		Params:          stripe.Params{Context: ctx},
		Mode:            stripe.String(string(stripe.CheckoutSessionModePayment)),
		InvoiceCreation: &stripe.CheckoutSessionInvoiceCreationParams{Enabled: stripe.Bool(true)},
		LineItems: []*stripe.CheckoutSessionLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency:    stripe.String("usd"),
				UnitAmount:  stripe.Int64(cents),
				TaxBehavior: stripe.String(businesscheckout.TaxExclusive),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripe.String("Agency AI balance"),
				},
			},
		}},
		PaymentIntentData: &stripe.CheckoutSessionPaymentIntentDataParams{Metadata: meta},
		ClientReferenceID: stripe.String(a.ID.String()),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		Metadata:          meta,
	}
	businesscheckout.Apply(params)
	s.attachCustomer(ctx, a, actorID, params)
	sess, err := session.New(params)
	if err != nil {
		errs.CaptureException(fmt.Errorf("agency AI top-up checkout: %w", err))
		return "", errx.New(errx.Internal, "failed to create checkout session")
	}
	return sess.URL, nil
}

func (s *Service) completeAITopup(ctx context.Context, agencyID uuid.UUID, sess *stripe.CheckoutSession) *errx.Error {
	if sess.PaymentStatus != stripe.CheckoutSessionPaymentStatusPaid {
		return nil
	}
	if s.ai == nil {
		return errx.New(errx.Internal, "the agency AI balance is unavailable")
	}
	a, err := s.repo.Get(ctx, agencyID)
	if err != nil {
		return internal(err)
	}
	if a == nil {
		return nil
	}
	cents, err := strconv.ParseInt(sess.Metadata["amount_cents"], 10, 64)
	if err != nil || cents <= 0 {
		return errx.New(errx.BadRequest, "invalid amount in agency AI top-up metadata")
	}
	ref := sess.ID
	if sess.PaymentIntent != nil && sess.PaymentIntent.ID != "" {
		ref = sess.PaymentIntent.ID
	}
	if a.StripeCustomerID == nil && sess.Customer != nil && sess.Customer.ID != "" {
		if err := s.repo.SetStripeCustomer(ctx, agencyID, sess.Customer.ID); err != nil {
			return internal(err)
		}
	}
	var actor *uuid.UUID
	if id, perr := uuid.Parse(sess.Metadata["user_id"]); perr == nil {
		actor = &id
	}
	if _, err := s.ai.GrantPurchased(ctx, agencyID, models.MicroFromCents(cents), s.aiPeriod(ctx, a), ref, actor); err != nil {
		return internal(err)
	}
	return nil
}
