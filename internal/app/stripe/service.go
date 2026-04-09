package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v76"
	portalsession "github.com/stripe/stripe-go/v76/billingportal/session"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/customer"
	"github.com/stripe/stripe-go/v76/invoice"
	"github.com/stripe/stripe-go/v76/subscription"
	"github.com/stripe/stripe-go/v76/webhook"
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

	// Checkout
	CreateCheckoutSession(ctx context.Context, userID uuid.UUID, orgID uuid.UUID, priceID, successURL, cancelURL string) (*stripe.CheckoutSession, *errx.Error)
	CreatePortalSession(ctx context.Context, customerID, returnURL string) (string, *errx.Error)

	// Subscriptions
	GetSubscription(ctx context.Context, subscriptionID string) (*stripe.Subscription, *errx.Error)
	CancelSubscription(ctx context.Context, subscriptionID string, cancelAtPeriodEnd bool) *errx.Error

	// Plan changes with proration
	ChangePlan(ctx context.Context, orgID uuid.UUID, newPlanID uuid.UUID, prorationBehavior string) (*stripe.Subscription, *errx.Error)
	PreviewPlanChange(ctx context.Context, orgID uuid.UUID, newPlanID uuid.UUID) (*ProrationPreview, *errx.Error)

	// Webhooks
	VerifyWebhook(payload []byte, signature string) (*stripe.Event, *errx.Error)
	ProcessWebhookEvent(ctx context.Context, event *stripe.Event) *errx.Error
}

type stripeService struct {
	cfg              *config.StripeConfig
	subRepo          repository.SubscriptionRepository
	planRepo         repository.PlanRepository
	workerAssignment worker.WorkerAssignmentService
}

func NewService(
	cfg *config.StripeConfig,
	subRepo repository.SubscriptionRepository,
	planRepo repository.PlanRepository,
	workerAssignment worker.WorkerAssignmentService,
) StripeService {
	stripe.Key = cfg.SecretKey
	return &stripeService{
		cfg:              cfg,
		subRepo:          subRepo,
		planRepo:         planRepo,
		workerAssignment: workerAssignment,
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

func (s *stripeService) CreateCheckoutSession(ctx context.Context, userID uuid.UUID, orgID uuid.UUID, priceID, successURL, cancelURL string) (*stripe.CheckoutSession, *errx.Error) {
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

	sess, err := session.New(params)
	if err != nil {
		sentry.CaptureException(fmt.Errorf("stripe checkout session failed: %w", err))
		return nil, errx.New(errx.Internal, "failed to create checkout session")
	}

	return sess, nil
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
func (s *stripeService) ChangePlan(ctx context.Context, orgID uuid.UUID, newPlanID uuid.UUID, prorationBehavior string) (*stripe.Subscription, *errx.Error) {
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

	// Set proration behavior (default to create_prorations)
	if prorationBehavior == "" {
		prorationBehavior = "create_prorations"
	}

	params := &stripe.SubscriptionParams{
		Items: []*stripe.SubscriptionItemsParams{
			{
				ID:    stripe.String(itemID),
				Price: stripe.String(*newPlan.StripePriceID),
			},
		},
		ProrationBehavior: stripe.String(prorationBehavior),
	}

	updated, stripeErr := subscription.Update(*sub.StripeSubscriptionID, params)
	if stripeErr != nil {
		return nil, errx.New(errx.Internal, fmt.Sprintf("failed to update subscription: %v", stripeErr))
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

	return nil
}

func (s *stripeService) handleInvoicePaid(ctx context.Context, event *stripe.Event) *errx.Error {
	// Invoice paid - subscription should be active via subscription.updated event
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
