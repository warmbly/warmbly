// Package partners attributes signups and subscription payments to the
// partner program, run on Dub (dub.co). Dub's Stripe app turns the payments
// into commissions, voids them on refunds and stops them when a subscription
// ends; this package only tells Dub which customer a partner's click became.
// It exists only when DUB_API_KEY is set, so a self-host stores and sends nothing.
package partners

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

const (
	defaultBaseURL = "https://api.dub.co"
	// leadEventName is deduplicated by Dub per customer, so a retried report is a no-op.
	leadEventName = "Sign up"
	// checkoutMetadataKey is the Checkout Session metadata Dub's Stripe app
	// reads to tie the session's customer, and every later invoice, to the lead.
	checkoutMetadataKey = "dubCustomerExternalId"
	// Bounds a checkout waiting on a retried report as well as the background one.
	requestTimeout = 5 * time.Second
	// Dub's limit on customer name and email.
	dubFieldMax = 100

	// The sweep leaves a signup's own report this long before retrying it,
	// gives up after retryWindow (a click Dub never issued fails forever),
	// and reports at most retryBatch per pass.
	retrySettle = 5 * time.Minute
	retryWindow = 7 * 24 * time.Hour
	retryBatch  = 100
)

// Store holds the click each referred workspace signed up through.
type Store interface {
	RecordPartnerReferral(ctx context.Context, orgID uuid.UUID, dubClickID string) error
	GetPartnerReferral(ctx context.Context, orgID uuid.UUID) (*models.PartnerReferral, error)
	MarkPartnerLeadReported(ctx context.Context, orgID uuid.UUID) error
	MarkPartnerLeadAttempted(ctx context.Context, orgID uuid.UUID) error
	ListUnreportedPartnerReferrals(ctx context.Context, createdAfter, createdBefore time.Time, limit int) ([]models.PartnerReferral, error)
}

// Customer is the person who signed up. The email is what lets Dub's risk
// checks catch a partner referring themselves.
type Customer struct {
	Email string
	Name  string
}

type Service struct {
	apiKey  string
	baseURL string
	client  *http.Client
	store   Store
}

// New returns nil when apiKey is empty, which is how the program stays off.
func New(apiKey string, store Store) *Service {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" || store == nil {
		return nil
	}
	return &Service{apiKey: apiKey, baseURL: defaultBaseURL, client: &http.Client{Timeout: requestTimeout}, store: store}
}

// RecordSignup stores the click a new workspace came through and reports the
// signup to Dub in the background, so Dub being slow or down never slows a signup.
func (s *Service) RecordSignup(ctx context.Context, orgID uuid.UUID, clickID string, customer Customer) {
	clickID = models.NormalizeDubClickID(clickID)
	if clickID == "" {
		return
	}
	if err := s.store.RecordPartnerReferral(ctx, orgID, clickID); err != nil {
		errs.CaptureException(fmt.Errorf("record partner referral: %w", err))
		return
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		if err := s.reportLead(bg, orgID, clickID, customer); err != nil {
			errs.CaptureException(err)
		}
	}()
}

// CheckoutMetadata is what a subscription Checkout Session for orgID carries,
// empty for a workspace no partner referred. A signup that never reached Dub
// is reported first, because Dub can only attribute a payment to a customer it knows.
func (s *Service) CheckoutMetadata(ctx context.Context, orgID uuid.UUID) map[string]string {
	ref, err := s.store.GetPartnerReferral(ctx, orgID)
	if err != nil {
		errs.CaptureException(fmt.Errorf("load partner referral: %w", err))
		return nil
	}
	if ref == nil {
		return nil
	}
	if ref.LeadReportedAt == nil {
		// The signup's email is not kept, so a late report names the customer by id only.
		if err := s.reportLead(ctx, orgID, ref.DubClickID, Customer{}); err != nil {
			errs.CaptureException(err)
		}
	}
	return map[string]string{checkoutMetadataKey: orgID.String()}
}

// Start retries, every interval until ctx ends, signups whose report never
// reached Dub, so a workspace's payments are attributed without waiting for a
// new checkout. Replicas may overlap: Dub deduplicates the lead.
func (s *Service) Start(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.retryUnreported(ctx, time.Now())
		}
	}
}

func (s *Service) retryUnreported(ctx context.Context, now time.Time) {
	refs, err := s.store.ListUnreportedPartnerReferrals(ctx, now.Add(-retryWindow), now.Add(-retrySettle), retryBatch)
	if err != nil {
		errs.CaptureException(fmt.Errorf("list unreported partner referrals: %w", err))
		return
	}
	for _, ref := range refs {
		if ctx.Err() != nil {
			return
		}
		// A failure is expected while Dub is down; the row stays unreported and the next pass retries it.
		_ = s.reportLead(ctx, ref.OrganizationID, ref.DubClickID, Customer{})
	}
}

type leadRequest struct {
	ClickID            string `json:"clickId"`
	EventName          string `json:"eventName"`
	CustomerExternalID string `json:"customerExternalId"`
	CustomerEmail      string `json:"customerEmail,omitempty"`
	CustomerName       string `json:"customerName,omitempty"`
	Mode               string `json:"mode"`
}

func (s *Service) reportLead(ctx context.Context, orgID uuid.UUID, clickID string, customer Customer) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	// A row that keeps failing moves behind the others in the sweep, so it cannot starve them.
	if err := s.store.MarkPartnerLeadAttempted(ctx, orgID); err != nil {
		return fmt.Errorf("mark partner lead attempted: %w", err)
	}

	body, err := json.Marshal(leadRequest{
		ClickID:            clickID,
		EventName:          leadEventName,
		CustomerExternalID: orgID.String(),
		CustomerEmail:      dubEmail(customer.Email),
		CustomerName:       truncateRunes(strings.TrimSpace(customer.Name), dubFieldMax),
		// "wait" answers only once the customer exists, so a success means a checkout can follow.
		Mode: "wait",
	})
	if err != nil {
		return fmt.Errorf("dub track lead: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/track/lead", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("dub track lead: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("dub track lead: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("dub track lead: status %d%s", resp.StatusCode, dubErrorCode(resp.Body))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return s.store.MarkPartnerLeadReported(ctx, orgID)
}

// dubErrorCode names Dub's machine-readable error, never its message or our request.
func dubErrorCode(body io.Reader) string {
	var parsed struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(body, 4<<10)).Decode(&parsed) != nil || parsed.Error.Code == "" {
		return ""
	}
	return " (" + truncateRunes(parsed.Error.Code, 64) + ")"
}

// dubEmail drops an address Dub would refuse, so the lead is still recorded without it.
func dubEmail(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > dubFieldMax {
		return ""
	}
	if addr, err := mail.ParseAddress(v); err != nil || addr.Address != v {
		return ""
	}
	return v
}

func truncateRunes(v string, n int) string {
	r := []rune(v)
	if len(r) <= n {
		return v
	}
	return string(r[:n])
}
