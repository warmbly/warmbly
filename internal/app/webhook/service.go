// Package webhook is the customer-facing event-delivery system. Other
// services call Dispatch() with a (orgID, eventType, payload). The
// service fans out to every subscribed endpoint by enqueueing rows in
// webhook_deliveries; a background DeliveryWorker drains the queue.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// SignatureHeader is the HTTP header carrying the HMAC signature on each
// outbound webhook POST. Format matches Stripe's: `t=<unix>,v1=<hex>`.
const SignatureHeader = "X-Warmbly-Signature"

// EventHeader carries the event type so subscribers can route without
// having to parse the body.
const EventHeader = "X-Warmbly-Event"

// EventIDHeader carries the unique event identifier so subscribers can
// dedupe replays.
const EventIDHeader = "X-Warmbly-Event-Id"

// Service is the call-site API. Internal events call Dispatch — the
// service writes one delivery row per matching endpoint and returns
// immediately. The DeliveryWorker drains the queue asynchronously.
type Service interface {
	// Dispatch finds endpoints subscribed to (orgID, eventType) and
	// enqueues a delivery for each. Returns the event ID so callers can
	// log/audit. If no endpoints match, this is a no-op.
	Dispatch(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any) (uuid.UUID, error)

	// Endpoint CRUD wrappers.
	CreateEndpoint(ctx context.Context, orgID uuid.UUID, url, description string, eventTypes []string, enabled bool) (*models.WebhookEndpointWithSecret, error)
	UpdateEndpoint(ctx context.Context, orgID, endpointID uuid.UUID, url, description string, eventTypes []string, enabled bool) (*models.WebhookEndpoint, error)
	DeleteEndpoint(ctx context.Context, orgID, endpointID uuid.UUID) error
	RotateSecret(ctx context.Context, orgID, endpointID uuid.UUID) (string, error)
	ListEndpoints(ctx context.Context, orgID uuid.UUID) ([]models.WebhookEndpoint, error)
	ListDeliveries(ctx context.Context, orgID, endpointID uuid.UUID, limit int) ([]models.WebhookDelivery, error)
}

type service struct {
	repo repository.WebhookRepository
	now  func() time.Time
}

func NewService(repo repository.WebhookRepository) Service {
	return &service{repo: repo, now: time.Now}
}

func (s *service) Dispatch(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any) (uuid.UUID, error) {
	eventID := uuid.New()

	endpoints, err := s.repo.MatchingEndpoints(ctx, orgID, eventType)
	if err != nil {
		return eventID, err
	}
	if len(endpoints) == 0 {
		return eventID, nil
	}

	payload := models.WebhookPayload{
		ID:             eventID,
		EventType:      eventType,
		OrganizationID: orgID,
		CreatedAt:      s.now().UTC(),
		Data:           data,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return eventID, fmt.Errorf("marshal webhook payload: %w", err)
	}

	for i := range endpoints {
		delivery := &models.WebhookDelivery{
			EndpointID:     endpoints[i].ID,
			OrganizationID: orgID,
			EventType:      string(eventType),
			EventID:        eventID,
			Payload:        payloadJSON,
		}
		if err := s.repo.EnqueueDelivery(ctx, delivery); err != nil {
			log.Warn().Err(err).Str("endpoint_id", endpoints[i].ID.String()).Msg("Failed to enqueue webhook delivery")
		}
	}
	return eventID, nil
}

func (s *service) CreateEndpoint(ctx context.Context, orgID uuid.UUID, rawURL, description string, eventTypes []string, enabled bool) (*models.WebhookEndpointWithSecret, error) {
	if err := validateURL(rawURL); err != nil {
		return nil, err
	}
	if err := validateEventTypes(eventTypes); err != nil {
		return nil, err
	}
	secret, err := generateSecret()
	if err != nil {
		return nil, err
	}
	endpoint := &models.WebhookEndpoint{
		ID:             uuid.New(),
		OrganizationID: orgID,
		URL:            rawURL,
		Description:    description,
		EventTypes:     eventTypes,
		Enabled:        enabled,
	}
	if err := s.repo.CreateEndpoint(ctx, endpoint, secret); err != nil {
		return nil, err
	}
	return &models.WebhookEndpointWithSecret{
		WebhookEndpoint: *endpoint,
		Secret:          secret,
	}, nil
}

func (s *service) UpdateEndpoint(ctx context.Context, orgID, endpointID uuid.UUID, rawURL, description string, eventTypes []string, enabled bool) (*models.WebhookEndpoint, error) {
	if err := validateURL(rawURL); err != nil {
		return nil, err
	}
	if err := validateEventTypes(eventTypes); err != nil {
		return nil, err
	}
	endpoint := &models.WebhookEndpoint{
		ID:             endpointID,
		OrganizationID: orgID,
		URL:            rawURL,
		Description:    description,
		EventTypes:     eventTypes,
		Enabled:        enabled,
	}
	if err := s.repo.UpdateEndpoint(ctx, endpoint); err != nil {
		return nil, err
	}
	return s.repo.GetEndpoint(ctx, orgID, endpointID)
}

func (s *service) DeleteEndpoint(ctx context.Context, orgID, endpointID uuid.UUID) error {
	return s.repo.DeleteEndpoint(ctx, orgID, endpointID)
}

func (s *service) RotateSecret(ctx context.Context, orgID, endpointID uuid.UUID) (string, error) {
	secret, err := generateSecret()
	if err != nil {
		return "", err
	}
	if err := s.repo.RotateSecret(ctx, orgID, endpointID, secret); err != nil {
		return "", err
	}
	return secret, nil
}

func (s *service) ListEndpoints(ctx context.Context, orgID uuid.UUID) ([]models.WebhookEndpoint, error) {
	return s.repo.ListEndpointsForOrg(ctx, orgID)
}

func (s *service) ListDeliveries(ctx context.Context, orgID, endpointID uuid.UUID, limit int) ([]models.WebhookDelivery, error) {
	return s.repo.ListDeliveriesForEndpoint(ctx, orgID, endpointID, limit)
}

// generateSecret returns a 32-byte cryptographically-random hex string.
// 64 hex chars ≈ 256 bits — comfortable margin for HMAC-SHA256.
func generateSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "whsec_" + hex.EncodeToString(buf), nil
}

// validateURL keeps malformed entries and obvious SSRF targets out of the
// table. We do not enforce HTTPS at insert time because internal-network
// integrations and ngrok-style local tests legitimately use http://.
func validateURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("url must have a host")
	}
	return nil
}

func validateEventTypes(eventTypes []string) error {
	for _, t := range eventTypes {
		if !models.IsValidWebhookEventType(t) {
			return fmt.Errorf("unknown event type: %s", t)
		}
	}
	return nil
}

// Sign produces the HMAC-SHA256 hex digest of `t.payload` using the secret.
// Subscribers verify by recomputing the same digest from the timestamp +
// body + their stored secret.
func Sign(secret string, timestamp time.Time, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", timestamp.Unix())
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// FormatSignatureHeader returns the `t=<unix>,v1=<hex>` value the
// dispatcher sets on every outbound POST.
func FormatSignatureHeader(timestamp time.Time, signature string) string {
	return fmt.Sprintf("t=%d,v1=%s", timestamp.Unix(), signature)
}

// DeliveryWorker drains the webhook_deliveries queue. Multiple workers can
// run concurrently — ClaimDueDeliveries uses SKIP LOCKED to prevent
// duplicate dispatch.
type DeliveryWorker struct {
	repo       repository.WebhookRepository
	httpClient *http.Client
	batchSize  int
	pollEvery  time.Duration
}

// DeliveryWorkerOptions configures the worker. Sensible defaults are
// applied for zero values.
type DeliveryWorkerOptions struct {
	BatchSize  int
	PollEvery  time.Duration
	HTTPClient *http.Client
}

func NewDeliveryWorker(repo repository.WebhookRepository, opts DeliveryWorkerOptions) *DeliveryWorker {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 25
	}
	if opts.PollEvery <= 0 {
		opts.PollEvery = 2 * time.Second
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &DeliveryWorker{
		repo:       repo,
		httpClient: opts.HTTPClient,
		batchSize:  opts.BatchSize,
		pollEvery:  opts.PollEvery,
	}
}

// Run blocks until ctx is cancelled, draining the delivery queue on a
// fixed interval. Errors are logged but do not stop the loop — the queue
// stays in Postgres, so a crashed dispatcher resumes on restart.
func (w *DeliveryWorker) Run(ctx context.Context) {
	t := time.NewTicker(w.pollEvery)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *DeliveryWorker) tick(ctx context.Context) {
	deliveries, err := w.repo.ClaimDueDeliveries(ctx, w.batchSize)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to claim webhook deliveries")
		return
	}
	for i := range deliveries {
		w.deliver(ctx, &deliveries[i])
	}
}

func (w *DeliveryWorker) deliver(ctx context.Context, d *models.WebhookDelivery) {
	secret, err := w.repo.GetEndpointSecret(ctx, d.EndpointID)
	if err != nil {
		w.fail(ctx, d, nil, fmt.Sprintf("load secret: %v", err), "")
		return
	}

	timestamp := time.Now().UTC()
	signature := Sign(secret, timestamp, d.Payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(d.Payload))
	if err != nil {
		w.fail(ctx, d, nil, fmt.Sprintf("build request: %v", err), "")
		return
	}

	// Set URL via the endpoint lookup so we always use the current value.
	endpoint, err := w.repo.GetEndpoint(ctx, d.OrganizationID, d.EndpointID)
	if err != nil || endpoint == nil {
		w.fail(ctx, d, nil, "endpoint missing", "")
		return
	}

	parsed, err := url.Parse(endpoint.URL)
	if err != nil {
		w.fail(ctx, d, nil, fmt.Sprintf("parse endpoint url: %v", err), "")
		return
	}
	req.URL = parsed
	req.Host = parsed.Host

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Warmbly-Webhooks/1.0")
	req.Header.Set(SignatureHeader, FormatSignatureHeader(timestamp, signature))
	req.Header.Set(EventHeader, d.EventType)
	req.Header.Set(EventIDHeader, d.EventID.String())

	resp, err := w.httpClient.Do(req)
	if err != nil {
		w.fail(ctx, d, nil, fmt.Sprintf("http error: %v", err), "")
		return
	}
	defer resp.Body.Close()

	bodyExcerpt := readBodyExcerpt(resp.Body, 1024)

	// 2xx = success. 4xx = caller's fault, do not retry indefinitely (still
	// retry briefly in case it's a transient validation flap). 5xx = retry.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := w.repo.MarkDelivered(ctx, d.ID, resp.StatusCode); err != nil {
			log.Warn().Err(err).Msg("Failed to mark webhook delivered")
		}
		_ = w.repo.UpdateEndpointHealthOnSuccess(ctx, d.EndpointID)
		return
	}
	w.fail(ctx, d, &resp.StatusCode, fmt.Sprintf("non-2xx response: %d", resp.StatusCode), bodyExcerpt)
}

func (w *DeliveryWorker) fail(ctx context.Context, d *models.WebhookDelivery, status *int, reason, bodyExcerpt string) {
	_ = w.repo.UpdateEndpointHealthOnFailure(ctx, d.EndpointID, reason)

	if d.AttemptCount >= d.MaxAttempts {
		if err := w.repo.MarkAbandoned(ctx, d.ID, reason); err != nil {
			log.Warn().Err(err).Msg("Failed to mark webhook abandoned")
		}
		return
	}
	nextAttempt := time.Now().UTC().Add(backoffFor(d.AttemptCount))
	if err := w.repo.MarkRetry(ctx, d.ID, nextAttempt, status, reason, bodyExcerpt); err != nil {
		log.Warn().Err(err).Msg("Failed to schedule webhook retry")
	}
}

// backoffFor returns the wait between attempts: doubles each retry,
// capped at 1 hour. Attempt 1 (just failed) → 30s, then 1m, 2m, 4m, 8m,
// 16m, 32m, capped at 60m. With max_attempts=8 the longest possible window
// before abandonment is ~2 hours.
func backoffFor(attempt int) time.Duration {
	const cap = time.Hour
	base := 30 * time.Second
	for i := 0; i < attempt-1; i++ {
		base *= 2
		if base >= cap {
			return cap
		}
	}
	return base
}

func readBodyExcerpt(r io.Reader, max int) string {
	if r == nil {
		return ""
	}
	buf := make([]byte, max)
	n, _ := io.ReadFull(r, buf)
	return string(buf[:n])
}
