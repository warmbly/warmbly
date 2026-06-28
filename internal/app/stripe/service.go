package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v76"
	portalsession "github.com/stripe/stripe-go/v76/billingportal/session"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/coupon"
	"github.com/stripe/stripe-go/v76/customer"
	balancetxn "github.com/stripe/stripe-go/v76/customerbalancetransaction"
	"github.com/stripe/stripe-go/v76/invoice"
	"github.com/stripe/stripe-go/v76/subscription"
	"github.com/stripe/stripe-go/v76/webhook"
	"github.com/warmbly/warmbly/internal/app/discount"
	"github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// ProrationPreview represents the preview of a plan change proration
type ProrationPreview struct {
	CurrentPlan     *models.Plan `json:"current_plan"`
	NewPlan         *models.Plan `json:"new_plan"`
	ProrationAmount int64        `json:"proration_amount"`
	AmountDue       int64        `json:"amount_due"`
	NextBillingDate time.Time    `json:"next_billing_date"`
	Currency        string       `json:"currency"`
}

type StripeService interface {
	// Customer management
	CreateCustomer(ctx context.Context, userID uuid.UUID, email, name string) (string, *errx.Error)
	GetCustomer(ctx context.Context, customerID string) (*stripe.Customer, *errx.Error)

	// Checkout. discountCode is optional ("" for none); when set, a one-off
	// Stripe coupon (money discounts) or trial extension is applied.
	CreateCheckoutSession(ctx context.Context, userID uuid.UUID, orgID uuid.UUID, priceID, successURL, cancelURL, discountCode string) (*stripe.CheckoutSession, *errx.Error)
	CreatePortalSession(ctx context.Context, customerID, returnURL string) (string, *errx.Error)

	// Subscriptions
	GetSubscription(ctx context.Context, subscriptionID string) (*stripe.Subscription, *errx.Error)
	CancelSubscription(ctx context.Context, subscriptionID string, cancelAtPeriodEnd bool) *errx.Error

	// Plan changes with proration. discountCode is optional ("" for none).
	ChangePlan(ctx context.Context, orgID uuid.UUID, newPlanID uuid.UUID, prorationBehavior, discountCode, interval string) (*stripe.Subscription, *errx.Error)
	PreviewPlanChange(ctx context.Context, orgID uuid.UUID, newPlanID uuid.UUID) (*ProrationPreview, *errx.Error)

	// Webhooks
	VerifyWebhook(payload []byte, signature string) (*stripe.Event, *errx.Error)
	ProcessWebhookEvent(ctx context.Context, event *stripe.Event) *errx.Error

	// ApplyCustomerCredit adds a signed cents delta to a customer's Stripe
	// balance (negative = credit the customer). Satisfies referral.StripeBalancer.
	ApplyCustomerCredit(ctx context.Context, customerID string, amountCents int64, currency, idempotencyKey string) (string, *errx.Error)

	// WireReferral attaches the referral program (post-construction; nil = the
	// referral hooks in the webhook flow are skipped).
	WireReferral(r ReferralRewarder)
}

// ReferralRewarder is the slice of the referral service the Stripe webhook flow
// drives. *referral.Service satisfies it; injected via WireReferral so the
// stripe package needs no import of referral (no cycle).
type ReferralRewarder interface {
	QualifyOnConversion(ctx context.Context, inviteeOrgID uuid.UUID)
	RewardOnFirstInvoice(ctx context.Context, inviteeOrgID, planID uuid.UUID, eventID string) *errx.Error
	ClawbackForInvitee(ctx context.Context, inviteeOrgID uuid.UUID, eventID, reason string)
	SyncStripeBalance(ctx context.Context, orgID uuid.UUID)
	InviteeDiscountCode(ctx context.Context, inviteeOrgID uuid.UUID) string
}

type stripeService struct {
	cfg              *config.StripeConfig
	subRepo          repository.SubscriptionRepository
	planRepo         repository.PlanRepository
	workerAssignment worker.WorkerAssignmentService
	discountService  discount.DiscountService
	referral         ReferralRewarder
}

func (s *stripeService) WireReferral(r ReferralRewarder) { s.referral = r }

func NewService(
	cfg *config.StripeConfig,
	subRepo repository.SubscriptionRepository,
	planRepo repository.PlanRepository,
	workerAssignment worker.WorkerAssignmentService,
	discountService discount.DiscountService,
) StripeService {
	stripe.Key = cfg.SecretKey
	return &stripeService{
		cfg:              cfg,
		subRepo:          subRepo,
		planRepo:         planRepo,
		workerAssignment: workerAssignment,
		discountService:  discountService,
	}
}

func (s *stripeService) CreateCustomer(ctx context.Context, userID uuid.UUID, email, name string) (string, *errx.Error) {
	params := &stripe.CustomerParams{
		Email: stripe.String(email),
		Name:  stripe.String(name),
		Metadata: map[string]string{
			"user_id": userID.String(),
		},
	}

	cust, err := customer.New(params)
	if err != nil {
		sentry.CaptureException(fmt.Errorf("stripe customer creation failed: %w", err))
		return "", errx.New(errx.Internal, "failed to create billing account")
	}

	return cust.ID, nil
}

func (s *stripeService) GetCustomer(ctx context.Context, customerID string) (*stripe.Customer, *errx.Error) {
	cust, err := customer.Get(customerID, nil)
	if err != nil {
		return nil, errx.New(errx.NotFound, "customer not found")
	}
	return cust, nil
}

// ApplyCustomerCredit posts a customer balance transaction. amountCents is the
// signed delta applied to the Stripe balance: negative credits the customer
// (reduces future invoices), positive debits them (reverses a credit). The
// idempotency key prevents a retried webhook from double-applying.
func (s *stripeService) ApplyCustomerCredit(ctx context.Context, customerID string, amountCents int64, currency, idempotencyKey string) (string, *errx.Error) {
	if customerID == "" || amountCents == 0 {
		return "", nil
	}
	params := &stripe.CustomerBalanceTransactionParams{
		Customer:    stripe.String(customerID),
		Amount:      stripe.Int64(amountCents),
		Currency:    stripe.String(currency),
		Description: stripe.String("Referral credit"),
	}
	if idempotencyKey != "" {
		params.SetIdempotencyKey(idempotencyKey)
	}
	txn, err := balancetxn.New(params)
	if err != nil {
		sentry.CaptureException(fmt.Errorf("stripe customer balance txn failed: %w", err))
		return "", errx.New(errx.Internal, "failed to apply referral credit")
	}
	return txn.ID, nil
}

func (s *stripeService) CreateCheckoutSession(ctx context.Context, userID uuid.UUID, orgID uuid.UUID, priceID, successURL, cancelURL, discountCode string) (*stripe.CheckoutSession, *errx.Error) {
	// Get or create customer
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to get subscription")
	}

	var customerID string
	if sub != nil {
		customerID = sub.StripeCustomerID
	}

	params := &stripe.CheckoutSessionParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
		Metadata: map[string]string{
			"user_id": userID.String(),
			"org_id":  orgID.String(),
		},
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Metadata: map[string]string{
				"user_id": userID.String(),
				"org_id":  orgID.String(),
			},
		},
	}

	if customerID != "" {
		params.Customer = stripe.String(customerID)
	} else {
		params.CustomerCreation = stripe.String("always")
	}

	// Auto-apply the invitee's referral discount when none was supplied, so a
	// user who signed up with ?ref= still gets their 10%/3-month discount even
	// if the billing page didn't prefill the code.
	if discountCode == "" && s.referral != nil {
		if code := s.referral.InviteeDiscountCode(ctx, orgID); code != "" {
			discountCode = code
		}
	}

	// Resolve and attach a discount code, if supplied. The code is validated
	// against the plan the chosen price belongs to; money discounts mint a
	// one-off Stripe coupon, trial extensions add trial days.
	var (
		couponID   *string
		reservedID *uuid.UUID
	)
	if discountCode != "" && s.discountService != nil {
		plan, perr := s.planRepo.GetByStripePriceID(ctx, priceID)
		if perr != nil {
			return nil, errx.New(errx.Internal, "failed to resolve plan for price")
		}
		if plan == nil {
			return nil, errx.New(errx.BadRequest, "plan not found for price")
		}

		dc, xerr := s.discountService.ValidateForCheckout(ctx, orgID, discountCode, plan.ID)
		if xerr != nil {
			return nil, xerr
		}

		// Reserve the cap slot BEFORE minting any coupon. This keeps cap
		// accounting exact and prevents an orphaned coupon or an untracked
		// discount if the reservation fails (cap race or DB error).
		redeemedBy := userID
		redID, xerr := s.discountService.ReservePendingRedemption(ctx, dc, orgID, &redeemedBy, &plan.ID)
		if xerr != nil {
			return nil, xerr
		}
		reservedID = &redID

		if dc.Type.IsMoney() {
			cid, xerr := s.mintCoupon(dc)
			if xerr != nil {
				_ = s.discountService.CancelRedemptionByID(ctx, redID)
				return nil, xerr
			}
			couponID = &cid
			params.Discounts = []*stripe.CheckoutSessionDiscountParams{{Coupon: stripe.String(cid)}}
		} else {
			params.SubscriptionData.TrialPeriodDays = stripe.Int64(int64(*dc.TrialExtensionDays))
		}
		params.Metadata["discount_code_id"] = dc.ID.String()
		params.SubscriptionData.Metadata["discount_code_id"] = dc.ID.String()
	}

	sess, err := session.New(params)
	if err != nil {
		if reservedID != nil {
			_ = s.discountService.CancelRedemptionByID(ctx, *reservedID)
		}
		sentry.CaptureException(fmt.Errorf("stripe checkout session failed: %w", err))
		return nil, errx.New(errx.Internal, "failed to create checkout session")
	}

	// Link the reserved redemption to the session + coupon. It flips to applied
	// on checkout.session.completed (idempotent), or is released on
	// checkout.session.expired.
	if reservedID != nil {
		if xerr := s.discountService.AttachRedemptionStripe(ctx, *reservedID, &sess.ID, couponID); xerr != nil {
			sentry.CaptureException(fmt.Errorf("attach discount redemption refs failed: %s", xerr.Message))
		}
	}

	return sess, nil
}

// mintCoupon creates a one-off Stripe coupon for a money discount code.
func (s *stripeService) mintCoupon(dc *models.DiscountCode) (string, *errx.Error) {
	params := &stripe.CouponParams{
		Name:           stripe.String(dc.Code),
		MaxRedemptions: stripe.Int64(1),
	}

	switch dc.Duration {
	case models.DiscountDurationForever:
		params.Duration = stripe.String(string(stripe.CouponDurationForever))
	case models.DiscountDurationRepeating:
		params.Duration = stripe.String(string(stripe.CouponDurationRepeating))
		if dc.DurationInMonths != nil {
			params.DurationInMonths = stripe.Int64(int64(*dc.DurationInMonths))
		}
	default:
		params.Duration = stripe.String(string(stripe.CouponDurationOnce))
	}

	switch dc.Type {
	case models.DiscountTypePercent:
		if dc.PercentOff != nil {
			params.PercentOff = stripe.Float64(float64(*dc.PercentOff))
		}
	case models.DiscountTypeFixed:
		if dc.AmountOff != nil && dc.Currency != nil {
			params.AmountOff = stripe.Int64(int64(math.Round(*dc.AmountOff * 100)))
			params.Currency = stripe.String(*dc.Currency)
		}
	}

	c, err := coupon.New(params)
	if err != nil {
		sentry.CaptureException(fmt.Errorf("stripe coupon creation failed: %w", err))
		return "", errx.New(errx.Internal, "failed to apply discount")
	}
	return c.ID, nil
}

func (s *stripeService) CreatePortalSession(ctx context.Context, customerID, returnURL string) (string, *errx.Error) {
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	}

	sess, err := portalsession.New(params)
	if err != nil {
		sentry.CaptureException(fmt.Errorf("stripe portal session failed: %w", err))
		return "", errx.New(errx.Internal, "failed to create billing portal session")
	}

	return sess.URL, nil
}

func (s *stripeService) GetSubscription(ctx context.Context, subscriptionID string) (*stripe.Subscription, *errx.Error) {
	sub, err := subscription.Get(subscriptionID, nil)
	if err != nil {
		return nil, errx.New(errx.NotFound, "subscription not found")
	}
	return sub, nil
}

func (s *stripeService) CancelSubscription(ctx context.Context, subscriptionID string, cancelAtPeriodEnd bool) *errx.Error {
	params := &stripe.SubscriptionParams{
		CancelAtPeriodEnd: stripe.Bool(cancelAtPeriodEnd),
	}

	_, err := subscription.Update(subscriptionID, params)
	if err != nil {
		sentry.CaptureException(fmt.Errorf("stripe subscription cancel failed: %w", err))
		return errx.New(errx.Internal, "failed to update subscription")
	}

	return nil
}

// ChangePlan changes the organization's subscription to a new plan with proration
func (s *stripeService) ChangePlan(ctx context.Context, orgID uuid.UUID, newPlanID uuid.UUID, prorationBehavior, discountCode, interval string) (*stripe.Subscription, *errx.Error) {
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to get subscription")
	}
	if sub == nil || sub.StripeSubscriptionID == nil {
		return nil, errx.New(errx.BadRequest, "no active subscription")
	}

	newPlan, err := s.planRepo.GetByID(ctx, newPlanID)
	if err != nil || newPlan == nil {
		return nil, errx.New(errx.NotFound, "plan not found")
	}

	// Pick the monthly or yearly Stripe price for the requested interval.
	priceID := newPlan.StripePriceID
	if interval == string(models.DurationYear) && newPlan.StripePriceIDYearly != nil {
		priceID = newPlan.StripePriceIDYearly
	}
	if priceID == nil {
		return nil, errx.New(errx.BadRequest, "plan has no Stripe price")
	}

	// Validate a discount code (if supplied) up front. Only money discounts can
	// apply to a mid-subscription plan change; trial extensions can't.
	var resolved *models.DiscountCode
	if discountCode != "" && s.discountService != nil {
		dc, xerr := s.discountService.ValidateForCheckout(ctx, orgID, discountCode, newPlanID)
		if xerr != nil {
			return nil, xerr
		}
		if !dc.Type.IsMoney() {
			return nil, errx.New(errx.BadRequest, "this discount code can only be applied at checkout, not to a plan change")
		}
		resolved = dc
	}

	// Get current subscription from Stripe
	stripeSub, xerr := s.GetSubscription(ctx, *sub.StripeSubscriptionID)
	if xerr != nil {
		return nil, xerr
	}

	if len(stripeSub.Items.Data) == 0 {
		return nil, errx.New(errx.Internal, "subscription has no items")
	}

	itemID := stripeSub.Items.Data[0].ID

	// Set proration behavior (default to create_prorations)
	if prorationBehavior == "" {
		prorationBehavior = "create_prorations"
	}

	// Reserve the cap slot and mint the coupon only after all read-only checks
	// pass, so a failure can't leave an orphaned coupon or an untracked discount.
	var (
		couponID   *string
		reservedID *uuid.UUID
	)
	if resolved != nil {
		planID := newPlanID
		subID := sub.ID
		redID, rerr := s.discountService.ReserveAppliedRedemption(ctx, resolved, orgID, &sub.UserID, &planID, &subID)
		if rerr != nil {
			return nil, rerr
		}
		reservedID = &redID
		cid, cerr := s.mintCoupon(resolved)
		if cerr != nil {
			_ = s.discountService.CancelRedemptionByID(ctx, redID)
			return nil, cerr
		}
		couponID = &cid
	}

	params := &stripe.SubscriptionParams{
		Items: []*stripe.SubscriptionItemsParams{
			{
				ID:    stripe.String(itemID),
				Price: stripe.String(*priceID),
			},
		},
		ProrationBehavior: stripe.String(prorationBehavior),
	}
	if couponID != nil {
		params.Coupon = stripe.String(*couponID)
	}

	updated, stripeErr := subscription.Update(*sub.StripeSubscriptionID, params)
	if stripeErr != nil {
		if reservedID != nil {
			_ = s.discountService.CancelRedemptionByID(ctx, *reservedID)
		}
		return nil, errx.New(errx.Internal, fmt.Sprintf("failed to update subscription: %v", stripeErr))
	}

	// Link the applied redemption to the minted coupon (no checkout session for
	// a direct plan change). Best-effort: the discount is already live.
	if reservedID != nil {
		if xerr := s.discountService.AttachRedemptionStripe(ctx, *reservedID, nil, couponID); xerr != nil {
			sentry.CaptureException(fmt.Errorf("attach discount redemption refs failed: %s", xerr.Message))
		}
	}

	return updated, nil
}

// PreviewPlanChange previews the proration for changing to a new plan
func (s *stripeService) PreviewPlanChange(ctx context.Context, orgID uuid.UUID, newPlanID uuid.UUID) (*ProrationPreview, *errx.Error) {
	sub, err := s.subRepo.GetByOrganizationID(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to get subscription")
	}
	if sub == nil || sub.StripeSubscriptionID == nil {
		return nil, errx.New(errx.BadRequest, "no active subscription")
	}

	currentPlan, _ := s.planRepo.GetByID(ctx, sub.PlanID)

	newPlan, err := s.planRepo.GetByID(ctx, newPlanID)
	if err != nil || newPlan == nil {
		return nil, errx.New(errx.NotFound, "plan not found")
	}
	if newPlan.StripePriceID == nil {
		return nil, errx.New(errx.BadRequest, "plan has no Stripe price")
	}

	// Get current subscription from Stripe
	stripeSub, xerr := s.GetSubscription(ctx, *sub.StripeSubscriptionID)
	if xerr != nil {
		return nil, xerr
	}

	if len(stripeSub.Items.Data) == 0 {
		return nil, errx.New(errx.Internal, "subscription has no items")
	}

	itemID := stripeSub.Items.Data[0].ID

	// Preview the upcoming invoice with the plan change
	params := &stripe.InvoiceUpcomingParams{
		Customer:     stripe.String(sub.StripeCustomerID),
		Subscription: stripe.String(*sub.StripeSubscriptionID),
		SubscriptionItems: []*stripe.SubscriptionItemsParams{
			{
				ID:    stripe.String(itemID),
				Price: stripe.String(*newPlan.StripePriceID),
			},
		},
		SubscriptionProrationBehavior: stripe.String("create_prorations"),
	}

	preview, stripeErr := invoice.Upcoming(params)
	if stripeErr != nil {
		return nil, errx.New(errx.Internal, fmt.Sprintf("failed to preview invoice: %v", stripeErr))
	}

	// Calculate proration amount from line items
	var prorationAmount int64
	for _, line := range preview.Lines.Data {
		if line.Proration {
			prorationAmount += line.Amount
		}
	}

	return &ProrationPreview{
		CurrentPlan:     currentPlan,
		NewPlan:         newPlan,
		ProrationAmount: prorationAmount,
		AmountDue:       preview.AmountDue,
		NextBillingDate: time.Unix(preview.PeriodEnd, 0),
		Currency:        string(preview.Currency),
	}, nil
}

func (s *stripeService) VerifyWebhook(payload []byte, signature string) (*stripe.Event, *errx.Error) {
	event, err := webhook.ConstructEvent(payload, signature, s.cfg.WebhookSecret)
	if err != nil {
		return nil, errx.New(errx.BadRequest, "invalid webhook signature")
	}
	return &event, nil
}

func (s *stripeService) ProcessWebhookEvent(ctx context.Context, event *stripe.Event) *errx.Error {
	// Check idempotency
	exists, err := s.subRepo.WebhookEventExists(ctx, event.ID)
	if err != nil {
		return errx.New(errx.Internal, "failed to check webhook event")
	}
	if exists {
		return nil // Already processed
	}

	// Process based on event type
	var processErr *errx.Error
	switch event.Type {
	case "checkout.session.completed":
		processErr = s.handleCheckoutCompleted(ctx, event)
	case "checkout.session.expired":
		processErr = s.handleCheckoutExpired(ctx, event)
	case "customer.subscription.created":
		processErr = s.handleSubscriptionCreated(ctx, event)
	case "customer.subscription.updated":
		processErr = s.handleSubscriptionUpdated(ctx, event)
	case "customer.subscription.deleted":
		processErr = s.handleSubscriptionDeleted(ctx, event)
	case "invoice.paid":
		processErr = s.handleInvoicePaid(ctx, event)
	case "invoice.payment_failed":
		processErr = s.handleInvoicePaymentFailed(ctx, event)
	case "charge.refunded":
		processErr = s.handleChargeRefunded(ctx, event)
	}

	// Record event for idempotency
	webhookEvent := &models.StripeWebhookEvent{
		ID:          event.ID,
		EventType:   string(event.Type),
		ProcessedAt: time.Now(),
	}
	if err := s.subRepo.RecordWebhookEvent(ctx, webhookEvent); err != nil {
		// Log but don't fail - idempotency is best-effort
	}

	return processErr
}

func (s *stripeService) handleCheckoutCompleted(ctx context.Context, event *stripe.Event) *errx.Error {
	var checkoutSession stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &checkoutSession); err != nil {
		return errx.New(errx.Internal, "failed to parse checkout session")
	}

	userIDStr, ok := checkoutSession.Metadata["user_id"]
	if !ok {
		return errx.New(errx.BadRequest, "missing user_id in metadata")
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return errx.New(errx.BadRequest, "invalid user_id format")
	}

	// Extract org_id from metadata
	orgIDStr, hasOrgID := checkoutSession.Metadata["org_id"]
	var orgID uuid.UUID
	if hasOrgID {
		orgID, err = uuid.Parse(orgIDStr)
		if err != nil {
			return errx.New(errx.BadRequest, "invalid org_id format")
		}
	}

	// Get or create subscription record
	var sub *models.Subscription
	if hasOrgID {
		sub, _ = s.subRepo.GetByOrganizationID(ctx, orgID)
	}
	// Fallback to user-based lookup for backward compatibility with in-flight checkouts
	if sub == nil {
		sub, _ = s.subRepo.GetByUserID(ctx, userID)
	}

	if sub == nil {
		// Find plan by Stripe price ID
		priceID := ""
		if checkoutSession.Subscription != nil {
			stripeSub, _ := subscription.Get(checkoutSession.Subscription.ID, nil)
			if stripeSub != nil && len(stripeSub.Items.Data) > 0 {
				priceID = stripeSub.Items.Data[0].Price.ID
			}
		}

		plan, err := s.planRepo.GetByStripePriceID(ctx, priceID)
		if err != nil || plan == nil {
			return errx.New(errx.BadRequest, "plan not found for price")
		}

		// Create subscription
		var customerID string
		if checkoutSession.Customer != nil {
			customerID = checkoutSession.Customer.ID
		}

		newSub := &models.Subscription{
			ID:               uuid.New(),
			UserID:           userID,
			OrganizationID:   orgID,
			PlanID:           plan.ID,
			StripeCustomerID: customerID,
			Status:           models.SubscriptionStatusIncomplete,
		}

		if checkoutSession.Subscription != nil {
			newSub.StripeSubscriptionID = &checkoutSession.Subscription.ID
		}

		if err := s.subRepo.Create(ctx, newSub); err != nil {
			return errx.New(errx.Internal, "failed to create subscription")
		}
		sub = newSub
	} else {
		// Update existing
		if checkoutSession.Customer != nil {
			sub.StripeCustomerID = checkoutSession.Customer.ID
		}
		if checkoutSession.Subscription != nil {
			sub.StripeSubscriptionID = &checkoutSession.Subscription.ID
		}
		if err := s.subRepo.Update(ctx, sub); err != nil {
			return errx.New(errx.Internal, "failed to update subscription")
		}
	}

	// If a discount code rode along on this checkout, flip its reservation to
	// applied. Idempotent: Stripe may retry the webhook.
	if codeID, ok := checkoutSession.Metadata["discount_code_id"]; ok && codeID != "" && s.discountService != nil {
		var subID *uuid.UUID
		if sub != nil {
			subID = &sub.ID
		}
		if xerr := s.discountService.MarkRedemptionApplied(ctx, checkoutSession.ID, subID); xerr != nil {
			sentry.CaptureException(fmt.Errorf("mark discount redemption applied failed: %s", xerr.Message))
		}
	}

	// Referral hooks: mark the invitee's attribution qualified (they reached a
	// paid checkout), and flush any referral credit this org earned before it
	// had a Stripe customer.
	if hasOrgID && s.referral != nil {
		s.referral.QualifyOnConversion(ctx, orgID)
		s.referral.SyncStripeBalance(ctx, orgID)
	}

	return nil
}

// handleCheckoutExpired releases a pending discount reservation when its
// checkout session expires without completing, so the redemption slot is freed.
func (s *stripeService) handleCheckoutExpired(ctx context.Context, event *stripe.Event) *errx.Error {
	var checkoutSession stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &checkoutSession); err != nil {
		return errx.New(errx.Internal, "failed to parse checkout session")
	}
	if s.discountService == nil {
		return nil
	}
	if codeID, ok := checkoutSession.Metadata["discount_code_id"]; ok && codeID != "" {
		return s.discountService.CancelRedemption(ctx, checkoutSession.ID)
	}
	return nil
}

func (s *stripeService) handleSubscriptionCreated(ctx context.Context, event *stripe.Event) *errx.Error {
	return s.handleSubscriptionUpdated(ctx, event)
}

func (s *stripeService) handleSubscriptionUpdated(ctx context.Context, event *stripe.Event) *errx.Error {
	var stripeSub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &stripeSub); err != nil {
		return errx.New(errx.Internal, "failed to parse subscription")
	}

	sub, err := s.subRepo.GetByStripeSubscriptionID(ctx, stripeSub.ID)
	if err != nil || sub == nil {
		// Try to find by customer
		sub, err = s.subRepo.GetByStripeCustomerID(ctx, stripeSub.Customer.ID)
		if err != nil || sub == nil {
			// No local subscription found, might be created via webhook before checkout complete
			return nil
		}
	}

	// Store old state for migration checks
	oldPlanID := sub.PlanID
	wasTrialOnly := sub.StripeSubscriptionID == nil
	oldPlan, _ := s.planRepo.GetByID(ctx, oldPlanID)

	// Update status
	sub.Status = mapStripeStatus(stripeSub.Status)
	sub.StripeSubscriptionID = &stripeSub.ID

	// Update period
	periodStart := time.Unix(stripeSub.CurrentPeriodStart, 0)
	periodEnd := time.Unix(stripeSub.CurrentPeriodEnd, 0)
	sub.CurrentPeriodStart = &periodStart
	sub.CurrentPeriodEnd = &periodEnd
	sub.CancelAtPeriodEnd = stripeSub.CancelAtPeriodEnd

	if stripeSub.CanceledAt > 0 {
		canceledAt := time.Unix(stripeSub.CanceledAt, 0)
		sub.CanceledAt = &canceledAt
	}

	if stripeSub.TrialStart > 0 {
		trialStart := time.Unix(stripeSub.TrialStart, 0)
		sub.TrialStart = &trialStart
	}
	if stripeSub.TrialEnd > 0 {
		trialEnd := time.Unix(stripeSub.TrialEnd, 0)
		sub.TrialEnd = &trialEnd
	}

	// Update plan if price changed
	var newPlan *models.Plan
	if len(stripeSub.Items.Data) > 0 {
		priceID := stripeSub.Items.Data[0].Price.ID
		sub.StripePriceID = &priceID

		newPlan, _ = s.planRepo.GetByStripePriceID(ctx, priceID)
		if newPlan != nil {
			sub.PlanID = newPlan.ID
		}
	}

	if err := s.subRepo.Update(ctx, sub); err != nil {
		return errx.New(errx.Internal, "failed to update subscription")
	}

	// Handle worker migrations if workerAssignment service is available
	if s.workerAssignment != nil {
		isNowPaid := sub.HasPaidSubscription()

		// Trial user converting to paid - migrate to premium workers.
		// Use a bounded timeout context since these goroutines outlive the HTTP request.
		if wasTrialOnly && isNowPaid {
			go func() {
				bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				s.workerAssignment.MigrateOrgToPremiumWorkers(bgCtx, sub.OrganizationID)
			}()
		}

		// Handle dedicated worker migration on plan change
		if newPlan != nil && newPlan.ID != oldPlanID {
			hadDedicated := oldPlan != nil && oldPlan.DedicatedWorkers > 0
			needsDedicated := newPlan.DedicatedWorkers > 0

			if !hadDedicated && needsDedicated {
				go func() {
					bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer cancel()
					s.workerAssignment.MigrateOrgToDedicated(bgCtx, sub.OrganizationID, sub.ID)
				}()
			} else if hadDedicated && !needsDedicated {
				go func() {
					bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer cancel()
					s.workerAssignment.MigrateOrgToShared(bgCtx, sub.OrganizationID)
				}()
			}
		}
	}

	return nil
}

func (s *stripeService) handleSubscriptionDeleted(ctx context.Context, event *stripe.Event) *errx.Error {
	var stripeSub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &stripeSub); err != nil {
		return errx.New(errx.Internal, "failed to parse subscription")
	}

	sub, err := s.subRepo.GetByStripeSubscriptionID(ctx, stripeSub.ID)
	if err != nil || sub == nil {
		return nil
	}

	// Check if org had dedicated workers
	oldPlan, _ := s.planRepo.GetByID(ctx, sub.PlanID)
	hadDedicated := oldPlan != nil && oldPlan.DedicatedWorkers > 0

	sub.Status = models.SubscriptionStatusCanceled
	canceledAt := time.Now()
	sub.CanceledAt = &canceledAt

	if err := s.subRepo.Update(ctx, sub); err != nil {
		return errx.New(errx.Internal, "failed to update subscription")
	}

	// Handle worker migration - move back to free tier workers.
	// Use bounded timeout context since these goroutines outlive the HTTP request.
	if s.workerAssignment != nil {
		orgID := sub.OrganizationID
		if hadDedicated {
			go func() {
				bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				s.workerAssignment.MigrateOrgToShared(bgCtx, orgID)
			}()
		}
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			s.workerAssignment.MigrateOrgToFreeWorkers(bgCtx, orgID)
		}()
	}

	// Claw back a referral reward if the invitee cancels inside the window.
	if s.referral != nil {
		s.referral.ClawbackForInvitee(ctx, sub.OrganizationID, event.ID, "subscription_canceled")
	}

	return nil
}

func (s *stripeService) handleInvoicePaid(ctx context.Context, event *stripe.Event) *errx.Error {
	// Subscription activation is handled via subscription.updated. This hook
	// rewards the referrer on the invitee's FIRST paid invoice.
	if s.referral == nil {
		return nil
	}
	var inv stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
		return errx.New(errx.Internal, "failed to parse invoice")
	}
	// Only the first invoice of a new subscription, and only once real money has
	// changed hands (a $0 invoice is not a payment), earns a referral reward.
	if inv.BillingReason != stripe.InvoiceBillingReasonSubscriptionCreate {
		return nil
	}
	if inv.AmountPaid <= 0 {
		return nil
	}

	// Resolve the invitee org from the subscription, falling back to customer.
	var sub *models.Subscription
	if inv.Subscription != nil {
		sub, _ = s.subRepo.GetByStripeSubscriptionID(ctx, inv.Subscription.ID)
	}
	if sub == nil && inv.Customer != nil {
		sub, _ = s.subRepo.GetByStripeCustomerID(ctx, inv.Customer.ID)
	}
	if sub == nil {
		return nil
	}

	// Resolve the invitee's plan: prefer the invoiced price, fall back to the
	// local subscription's plan.
	var plan *models.Plan
	if inv.Lines != nil && len(inv.Lines.Data) > 0 && inv.Lines.Data[0].Price != nil {
		plan, _ = s.planRepo.GetByStripePriceID(ctx, inv.Lines.Data[0].Price.ID)
	}
	if plan == nil {
		plan, _ = s.planRepo.GetByID(ctx, sub.PlanID)
	}
	if plan == nil {
		return nil
	}

	return s.referral.RewardOnFirstInvoice(ctx, sub.OrganizationID, plan.ID, event.ID)
}

// handleChargeRefunded claws back a referral reward when an invitee's charge is
// refunded inside the clawback window. The referral service guards the window
// and one-time semantics, so a refund outside the window is a no-op.
func (s *stripeService) handleChargeRefunded(ctx context.Context, event *stripe.Event) *errx.Error {
	if s.referral == nil {
		return nil
	}
	var ch stripe.Charge
	if err := json.Unmarshal(event.Data.Raw, &ch); err != nil {
		return errx.New(errx.Internal, "failed to parse charge")
	}
	if ch.Customer == nil {
		return nil
	}
	sub, _ := s.subRepo.GetByStripeCustomerID(ctx, ch.Customer.ID)
	if sub == nil {
		return nil
	}
	s.referral.ClawbackForInvitee(ctx, sub.OrganizationID, event.ID, "refund")
	return nil
}

func (s *stripeService) handleInvoicePaymentFailed(ctx context.Context, event *stripe.Event) *errx.Error {
	// Payment failed - subscription status will be updated via subscription.updated event
	return nil
}

func mapStripeStatus(status stripe.SubscriptionStatus) models.SubscriptionStatus {
	switch status {
	case stripe.SubscriptionStatusTrialing:
		return models.SubscriptionStatusTrialing
	case stripe.SubscriptionStatusActive:
		return models.SubscriptionStatusActive
	case stripe.SubscriptionStatusPastDue:
		return models.SubscriptionStatusPastDue
	case stripe.SubscriptionStatusCanceled:
		return models.SubscriptionStatusCanceled
	case stripe.SubscriptionStatusUnpaid:
		return models.SubscriptionStatusUnpaid
	case stripe.SubscriptionStatusIncomplete:
		return models.SubscriptionStatusIncomplete
	case stripe.SubscriptionStatusIncompleteExpired:
		return models.SubscriptionStatusIncompleteExpired
	case stripe.SubscriptionStatusPaused:
		return models.SubscriptionStatusPaused
	default:
		return models.SubscriptionStatusIncomplete
	}
}
