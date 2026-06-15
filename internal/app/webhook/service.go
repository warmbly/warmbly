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
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/safehttp"
	"github.com/warmbly/warmbly/internal/pkg/whdomain"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// SignatureHeader is the HTTP header carrying the HMAC signature on each
// outbound webhook POST. Format matches Stripe's: `t=<unix>,v1=<hex>`.
const SignatureHeader = "X-Warmbly-Signature"

// EventHeader carries the event type so subscribers can route without
// having to parse the body.
const EventHeader = "X-Warmbly-Event"

// EventIDHeader carries the unique event identifier so subscribers can
// dedupe replays. Stable across retries — it is the idempotency key.
const EventIDHeader = "X-Warmbly-Event-Id"

// ChallengeHeader lets a receiver echo the verification challenge in a response
// header instead of the body, if that is easier for their framework.
const ChallengeHeader = "X-Warmbly-Webhook-Challenge"

// verificationMaxAttempts bounds challenge retries so a never-echoing endpoint
// is not hammered (separate from the larger event-delivery budget).
const verificationMaxAttempts = 3

// DispatchSink receives every dispatched event so other subsystems (notably
// third-party integration actions: Slack pings, CRM upserts) can react to the
// same event vocabulary that drives customer webhooks, without every call site
// having to know about them. Wired optionally via WireDispatchSink.
type DispatchSink func(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any)

// AppDomainResolver returns an OAuth app's allowed_webhook_domains, used to
// enforce the per-app domain allowlist on app-scoped endpoints at write and
// delivery time. Wired optionally; nil disables the resolve-side check.
type AppDomainResolver func(ctx context.Context, appID uuid.UUID) ([]string, error)

// EndpointInput is the create payload. OAuthApplicationID is set when an OAuth
// app registers the endpoint (its URL host is then constrained to the app's
// allowed_webhook_domains).
type EndpointInput struct {
	URL                string
	Description        string
	EventTypes         []string
	Enabled            bool
	CreatedBy          *uuid.UUID
	OAuthApplicationID *uuid.UUID
}

// Service is the call-site API. Internal events call Dispatch — the
// service writes one delivery row per matching endpoint and returns
// immediately. The DeliveryWorker drains the queue asynchronously.
type Service interface {
	// Dispatch finds endpoints subscribed to (orgID, eventType) and
	// enqueues a delivery for each. Returns the event ID so callers can
	// log/audit. If no endpoints match, this is a no-op.
	Dispatch(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any) (uuid.UUID, error)

	// WireDispatchSink attaches a single fan-out sink invoked for every
	// dispatched event (even when no webhook endpoint matches). Idempotent
	// replacement; pass nil to detach.
	WireDispatchSink(sink DispatchSink)

	// WireThrottle attaches a Redis-backed per-org, per-event-type dispatch
	// throttle. resolveLimit returns the org's per-minute cap on how many events
	// of a single type it may fan out. Over the cap, further events of that type
	// in the same minute are dropped (and recorded in the visible drop rollup)
	// instead of reaching webhooks or integration sinks. Pass a nil cache or
	// resolver to disable (fail-open).
	WireThrottle(c *cache.Cache, resolveLimit func(ctx context.Context, orgID uuid.UUID) int)

	// WireAppDomainResolver attaches the OAuth-app allowed-domain lookup used to
	// enforce app-scoped endpoint domains.
	WireAppDomainResolver(r AppDomainResolver)

	// Endpoint CRUD.
	CreateEndpoint(ctx context.Context, orgID uuid.UUID, in EndpointInput) (*models.WebhookEndpointWithSecret, error)
	UpdateEndpoint(ctx context.Context, orgID, endpointID uuid.UUID, url, description string, eventTypes []string, enabled bool) (*models.WebhookEndpoint, error)
	DeleteEndpoint(ctx context.Context, orgID, endpointID uuid.UUID) error
	RotateSecret(ctx context.Context, orgID, endpointID uuid.UUID) (string, error)
	GetEndpoint(ctx context.Context, orgID, endpointID uuid.UUID) (*models.WebhookEndpoint, error)
	ListEndpoints(ctx context.Context, orgID uuid.UUID) ([]models.WebhookEndpoint, error)

	// VerifyEndpoint (re)arms ownership verification and enqueues a single signed
	// challenge/test request to the endpoint URL. Doubles as "send test event".
	VerifyEndpoint(ctx context.Context, orgID, endpointID uuid.UUID) error

	// Deliveries.
	ListDeliveries(ctx context.Context, orgID uuid.UUID, filter models.WebhookDeliveryFilter) (*models.WebhookDeliveriesResult, error)
	Redeliver(ctx context.Context, orgID, deliveryID uuid.UUID) error

	// ListEventDrops surfaces throttle-dropped events (rate-limiting visibility).
	ListEventDrops(ctx context.Context, orgID uuid.UUID, since time.Time) ([]models.WebhookEventDrop, error)

	// App observability: the per-org endpoints an OAuth app has materialized, and
	// the delivery log across all of them (every org that authorized the app).
	ListAppEndpoints(ctx context.Context, appID uuid.UUID) ([]models.WebhookEndpoint, error)
	ListAppDeliveries(ctx context.Context, appID uuid.UUID, filter models.WebhookDeliveryFilter) (*models.WebhookDeliveriesResult, error)
}

type service struct {
	repo         repository.WebhookRepository
	now          func() time.Time
	sink         DispatchSink
	cache        *cache.Cache
	resolveLimit func(ctx context.Context, orgID uuid.UUID) int
	appDomains   AppDomainResolver
}

func NewService(repo repository.WebhookRepository) Service {
	return &service{repo: repo, now: time.Now}
}

func (s *service) WireDispatchSink(sink DispatchSink) {
	s.sink = sink
}

func (s *service) WireThrottle(c *cache.Cache, resolveLimit func(ctx context.Context, orgID uuid.UUID) int) {
	s.cache = c
	s.resolveLimit = resolveLimit
}

func (s *service) WireAppDomainResolver(r AppDomainResolver) {
	s.appDomains = r
}

// StaticLimit adapts a fixed per-minute cap into a throttle resolver, for
// callers without plan context (e.g. the consumer, which dispatches lower-volume
// reply/warmup events rather than per-contact campaign fan-out).
func StaticLimit(perMinute int) func(context.Context, uuid.UUID) int {
	return func(context.Context, uuid.UUID) int { return perMinute }
}

// orgLimit returns the org's per-minute dispatch cap, caching the resolved
// value in Redis for a minute so resolveLimit (a plan lookup) is not hit on
// every event.
func (s *service) orgLimit(ctx context.Context, orgID uuid.UUID) int {
	key := "wh:limit:" + orgID.String()
	if v, err := s.cache.Get(ctx, key).Int(); err == nil && v > 0 {
		return v
	}
	limit := s.resolveLimit(ctx, orgID)
	if limit > 0 {
		s.cache.Set(ctx, key, limit, time.Minute)
	}
	return limit
}

// throttled reports whether this (org, eventType) has exceeded its per-minute
// dispatch cap. Fixed one-minute window in Redis; fail-open on any cache error
// so a Redis hiccup never silently swallows events. On the first over-cap hit in
// a window it records a visible (flood-safe, one-per-window) drop rollup so the
// dashboard can show rate-limiting instead of dropping silently.
func (s *service) throttled(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType) bool {
	if s.cache == nil || s.resolveLimit == nil {
		return false
	}
	limit := s.orgLimit(ctx, orgID)
	if limit <= 0 {
		return false // resolver opted out / fail-open
	}
	bucket := s.now().Unix() / 60
	key := fmt.Sprintf("wh:disp:%s:%s:%d", orgID, eventType, bucket)
	n, err := s.cache.Incr(ctx, key).Result()
	if err != nil {
		return false // fail-open
	}
	if n == 1 {
		// First hit in this window — set a short TTL so the key self-expires.
		s.cache.Expire(ctx, key, 2*time.Minute)
	}
	if n > int64(limit) {
		if n == int64(limit)+1 {
			// Record once per window (the +1 boundary): flood-safe visibility.
			if err := s.repo.RecordEventDrop(ctx, orgID, eventType); err != nil {
				log.Warn().Err(err).Msg("Failed to record webhook throttle drop")
			}
		}
		log.Warn().
			Str("org_id", orgID.String()).
			Str("event_type", string(eventType)).
			Int64("count", n).
			Int("limit_per_min", limit).
			Msg("Webhook dispatch throttled — dropping event for this minute")
		return true
	}
	return false
}

func (s *service) Dispatch(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any) (uuid.UUID, error) {
	eventID := uuid.New()

	// Global per-org, per-event-type fan-out throttle. Stops a per-contact
	// event source (notably a campaign "notify" action over a large lead list)
	// from flooding the org's webhooks and integration sinks. Checked before
	// both the sink and endpoint enqueue so an over-cap event reaches neither.
	if s.throttled(ctx, orgID, eventType) {
		return eventID, nil
	}

	// Fan the event to non-webhook subscribers (integration actions) first,
	// independently of whether any webhook endpoint is configured.
	if s.sink != nil {
		s.sink(ctx, orgID, eventType, data)
	}

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

func (s *service) CreateEndpoint(ctx context.Context, orgID uuid.UUID, in EndpointInput) (*models.WebhookEndpointWithSecret, error) {
	if err := validateURL(in.URL); err != nil {
		return nil, err
	}
	if err := validateEventTypes(in.EventTypes); err != nil {
		return nil, err
	}
	if err := s.enforceAppDomain(ctx, in.OAuthApplicationID, in.URL); err != nil {
		return nil, err
	}
	secret, err := generateSecret()
	if err != nil {
		return nil, err
	}
	token, err := generateChallenge()
	if err != nil {
		return nil, err
	}
	endpoint := &models.WebhookEndpoint{
		ID:                 uuid.New(),
		OrganizationID:     orgID,
		URL:                in.URL,
		Description:        in.Description,
		EventTypes:         in.EventTypes,
		Enabled:            in.Enabled,
		CreatedBy:          in.CreatedBy,
		OAuthApplicationID: in.OAuthApplicationID,
	}
	if err := s.repo.CreateEndpoint(ctx, endpoint, secret, token); err != nil {
		return nil, err
	}
	// Kick off ownership verification immediately so the endpoint can go live as
	// soon as it echoes the challenge (or, for org endpoints, simply accepts it).
	s.enqueueChallenge(ctx, endpoint, token)
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
	existing, err := s.repo.GetEndpoint(ctx, orgID, endpointID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("webhook endpoint not found")
	}
	if err := s.enforceAppDomain(ctx, existing.OAuthApplicationID, rawURL); err != nil {
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
	// Changing the URL host invalidates prior verification — re-arm and re-send
	// the challenge so we never stream events to a freshly-pointed, unproven URL.
	if !sameHost(existing.URL, rawURL) {
		if token, terr := generateChallenge(); terr == nil {
			if err := s.repo.ArmVerification(ctx, orgID, endpointID, token); err == nil {
				updated, _ := s.repo.GetEndpoint(ctx, orgID, endpointID)
				if updated != nil {
					s.enqueueChallenge(ctx, updated, token)
				}
			}
		}
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

func (s *service) GetEndpoint(ctx context.Context, orgID, endpointID uuid.UUID) (*models.WebhookEndpoint, error) {
	return s.repo.GetEndpoint(ctx, orgID, endpointID)
}

func (s *service) ListEndpoints(ctx context.Context, orgID uuid.UUID) ([]models.WebhookEndpoint, error) {
	return s.repo.ListEndpointsForOrg(ctx, orgID)
}

func (s *service) VerifyEndpoint(ctx context.Context, orgID, endpointID uuid.UUID) error {
	endpoint, err := s.repo.GetEndpoint(ctx, orgID, endpointID)
	if err != nil {
		return err
	}
	if endpoint == nil {
		return fmt.Errorf("webhook endpoint not found")
	}
	token, err := generateChallenge()
	if err != nil {
		return err
	}
	if err := s.repo.ArmVerification(ctx, orgID, endpointID, token); err != nil {
		return err
	}
	s.enqueueChallenge(ctx, endpoint, token)
	return nil
}

func (s *service) ListDeliveries(ctx context.Context, orgID uuid.UUID, filter models.WebhookDeliveryFilter) (*models.WebhookDeliveriesResult, error) {
	rows, hasMore, err := s.repo.ListDeliveries(ctx, orgID, filter)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []models.WebhookDelivery{}
	}
	result := &models.WebhookDeliveriesResult{Data: rows}
	result.Pagination.HasMore = hasMore
	if hasMore {
		limit := filter.Limit
		if limit <= 0 || limit > 200 {
			limit = 50
		}
		result.Pagination.NextCursor = paging.EncodeOffset(filter.Offset + limit)
	}
	return result, nil
}

func (s *service) Redeliver(ctx context.Context, orgID, deliveryID uuid.UUID) error {
	d, err := s.repo.GetDelivery(ctx, orgID, deliveryID)
	if err != nil {
		return err
	}
	if d == nil {
		return fmt.Errorf("webhook delivery not found")
	}
	return s.repo.RedeliverDelivery(ctx, orgID, deliveryID)
}

func (s *service) ListEventDrops(ctx context.Context, orgID uuid.UUID, since time.Time) ([]models.WebhookEventDrop, error) {
	return s.repo.ListEventDrops(ctx, orgID, since)
}

func (s *service) ListAppEndpoints(ctx context.Context, appID uuid.UUID) ([]models.WebhookEndpoint, error) {
	eps, err := s.repo.ListEndpointsByApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	if eps == nil {
		eps = []models.WebhookEndpoint{}
	}
	return eps, nil
}

func (s *service) ListAppDeliveries(ctx context.Context, appID uuid.UUID, filter models.WebhookDeliveryFilter) (*models.WebhookDeliveriesResult, error) {
	rows, hasMore, err := s.repo.ListDeliveriesByApp(ctx, appID, filter)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []models.WebhookDelivery{}
	}
	result := &models.WebhookDeliveriesResult{Data: rows}
	result.Pagination.HasMore = hasMore
	if hasMore {
		limit := filter.Limit
		if limit <= 0 || limit > 200 {
			limit = 50
		}
		result.Pagination.NextCursor = paging.EncodeOffset(filter.Offset + limit)
	}
	return result, nil
}

// enforceAppDomain rejects an app-scoped endpoint whose URL host is outside the
// owning app's allowed_webhook_domains. A no-op for org-level endpoints, and a
// fail-open no-op when no resolver is wired (so non-app paths keep working).
func (s *service) enforceAppDomain(ctx context.Context, appID *uuid.UUID, rawURL string) error {
	if appID == nil || s.appDomains == nil {
		return nil
	}
	domains, err := s.appDomains(ctx, *appID)
	if err != nil {
		return fmt.Errorf("resolve app webhook domains: %w", err)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if !whdomain.HostAllowed(u.Hostname(), domains) {
		return fmt.Errorf("webhook URL host %q is not in this app's allowed webhook domains", u.Hostname())
	}
	return nil
}

// enqueueChallenge sends a single signed challenge/test request to the endpoint.
func (s *service) enqueueChallenge(ctx context.Context, endpoint *models.WebhookEndpoint, token string) {
	payload := models.WebhookPayload{
		ID:             uuid.New(),
		EventType:      models.WebhookEventEndpointTest,
		OrganizationID: endpoint.OrganizationID,
		CreatedAt:      s.now().UTC(),
		Data: map[string]any{
			"challenge":   token,
			"endpoint_id": endpoint.ID,
			"message":     "Echo the challenge value (in the body or the X-Warmbly-Webhook-Challenge header) to verify this endpoint.",
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	delivery := &models.WebhookDelivery{
		EndpointID:     endpoint.ID,
		OrganizationID: endpoint.OrganizationID,
		EventType:      string(models.WebhookEventEndpointTest),
		EventID:        payload.ID,
		Payload:        body,
		MaxAttempts:    verificationMaxAttempts,
	}
	if err := s.repo.EnqueueDelivery(ctx, delivery); err != nil {
		log.Warn().Err(err).Str("endpoint_id", endpoint.ID.String()).Msg("Failed to enqueue webhook challenge")
	}
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

// generateChallenge returns a random verification token the receiver must echo.
func generateChallenge() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "whcg_" + hex.EncodeToString(buf), nil
}

// ValidateOutboundURL is the exported SSRF/HTTPS guard reused by any subsystem
// that stores a user-supplied URL we will later POST to (e.g. third-party
// integration actions such as Discord/generic webhooks). Same policy as the
// customer-webhook endpoints: HTTPS + publicly-routable host, unless
// WARMBLY_ALLOW_UNSAFE_WEBHOOK_URLS=true for local/self-hosted development.
func ValidateOutboundURL(raw string) error {
	return validateURL(raw)
}

// validateURL keeps malformed entries and obvious SSRF targets out of the
// table. Public webhook endpoints must use HTTPS and route to public hosts.
// Local/self-hosted development can set WARMBLY_ALLOW_UNSAFE_WEBHOOK_URLS=true
// to permit HTTP and private targets.
func validateURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	allowUnsafe := strings.EqualFold(os.Getenv("WARMBLY_ALLOW_UNSAFE_WEBHOOK_URLS"), "true")
	if u.Scheme != "https" && !(allowUnsafe && u.Scheme == "http") {
		return fmt.Errorf("url scheme must be https")
	}
	if u.Host == "" {
		return fmt.Errorf("url must have a host")
	}
	// Reject userinfo (user:pass@host) — a classic URL-parser-confusion SSRF
	// bypass and never legitimate on a webhook endpoint.
	if u.User != nil {
		return fmt.Errorf("url must not contain credentials")
	}
	if !allowUnsafe && isPrivateWebhookHost(u.Hostname()) {
		return fmt.Errorf("url host must be publicly routable")
	}
	return nil
}

func isPrivateWebhookHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return safehttp.IsBlockedIP(ip)
}

func validateEventTypes(eventTypes []string) error {
	for _, t := range eventTypes {
		if !models.IsValidWebhookEventType(t) {
			return fmt.Errorf("unknown event type: %s", t)
		}
	}
	return nil
}

// sameHost reports whether two URLs target the same host (case-insensitive).
func sameHost(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.EqualFold(ua.Hostname(), ub.Hostname())
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
	repo             repository.WebhookRepository
	httpClient       *http.Client
	cache            *cache.Cache
	appDomains       AppDomainResolver
	batchSize        int
	pollEvery        time.Duration
	ratePerSecond    int
	leaseTimeout     time.Duration
	reapEvery        time.Duration
	autoDisableAfter time.Duration
	lastReap         time.Time
}

// DeliveryWorkerOptions configures the worker. Sensible defaults are
// applied for zero values.
type DeliveryWorkerOptions struct {
	BatchSize  int
	PollEvery  time.Duration
	HTTPClient *http.Client
	// Cache enables the per-endpoint delivery rate limit (defends against using
	// the platform to fan high volume at a single victim URL). Nil disables it.
	Cache *cache.Cache
	// AppDomains re-checks app-scoped endpoint hosts at delivery time. Nil skips.
	AppDomains AppDomainResolver
	// RatePerSecond caps deliveries to a single endpoint (default 20).
	RatePerSecond int
	// LeaseTimeout is how long an in_flight row may sit before the reaper
	// re-queues it (default 5m). AutoDisableAfter is the sustained-failure window
	// after which an endpoint is auto-disabled (default 72h).
	LeaseTimeout     time.Duration
	AutoDisableAfter time.Duration
}

func NewDeliveryWorker(repo repository.WebhookRepository, opts DeliveryWorkerOptions) *DeliveryWorker {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 25
	}
	if opts.PollEvery <= 0 {
		opts.PollEvery = 2 * time.Second
	}
	if opts.RatePerSecond <= 0 {
		opts.RatePerSecond = 20
	}
	if opts.LeaseTimeout <= 0 {
		opts.LeaseTimeout = 5 * time.Minute
	}
	if opts.AutoDisableAfter <= 0 {
		opts.AutoDisableAfter = 72 * time.Hour
	}
	if opts.HTTPClient == nil {
		// SSRF-hardened: customer webhook URLs are user-supplied, so block delivery
		// to non-public hosts at dial time (covers DNS-resolved + rebinding cases
		// the literal-IP ValidateOutboundURL check on write cannot catch).
		opts.HTTPClient = safehttp.Client(15 * time.Second)
	}
	// Never follow redirects on delivery: a 302-to-private-IP is the classic way
	// to defeat the write-time URL check. We treat any 3xx as a failure.
	opts.HTTPClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &DeliveryWorker{
		repo:             repo,
		httpClient:       opts.HTTPClient,
		cache:            opts.Cache,
		appDomains:       opts.AppDomains,
		batchSize:        opts.BatchSize,
		pollEvery:        opts.PollEvery,
		ratePerSecond:    opts.RatePerSecond,
		leaseTimeout:     opts.LeaseTimeout,
		reapEvery:        time.Minute,
		autoDisableAfter: opts.AutoDisableAfter,
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
	// Periodically re-queue deliveries stranded in_flight by a crashed worker.
	if now := time.Now(); now.Sub(w.lastReap) >= w.reapEvery {
		w.lastReap = now
		if n, err := w.repo.ReclaimStuckDeliveries(ctx, w.leaseTimeout); err != nil {
			log.Warn().Err(err).Msg("Failed to reclaim stuck webhook deliveries")
		} else if n > 0 {
			log.Info().Int64("count", n).Msg("Reclaimed stuck webhook deliveries")
		}
	}

	deliveries, err := w.repo.ClaimDueDeliveries(ctx, w.batchSize)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to claim webhook deliveries")
		return
	}
	for i := range deliveries {
		w.deliver(ctx, &deliveries[i])
	}
}

// rateLimited reports whether the endpoint is over its per-second delivery
// budget. Fail-open when no cache is wired.
func (w *DeliveryWorker) rateLimited(ctx context.Context, endpointID uuid.UUID) bool {
	if w.cache == nil {
		return false
	}
	key := fmt.Sprintf("wh:rate:%s:%d", endpointID, time.Now().Unix())
	n, err := w.cache.Incr(ctx, key).Result()
	if err != nil {
		return false
	}
	if n == 1 {
		w.cache.Expire(ctx, key, 2*time.Second)
	}
	return n > int64(w.ratePerSecond)
}

func (w *DeliveryWorker) deliver(ctx context.Context, d *models.WebhookDelivery) {
	endpoint, err := w.repo.GetEndpoint(ctx, d.OrganizationID, d.EndpointID)
	if err != nil || endpoint == nil {
		w.fail(ctx, d, nil, "endpoint missing", "")
		return
	}

	// Re-check the app-scoped domain allowlist at delivery time (it may have been
	// narrowed since the URL was stored, or DNS may have drifted).
	if endpoint.OAuthApplicationID != nil && w.appDomains != nil {
		domains, derr := w.appDomains(ctx, *endpoint.OAuthApplicationID)
		if derr == nil {
			if u, perr := url.Parse(endpoint.URL); perr == nil && !whdomain.HostAllowed(u.Hostname(), domains) {
				_ = w.repo.DisableEndpoint(ctx, endpoint.ID, "URL host left the app's allowed webhook domains")
				w.fail(ctx, d, nil, "host not in app allowed domains", "")
				return
			}
		}
	}

	// Per-endpoint delivery rate limit: defer (don't drop, don't count the
	// attempt) so a single victim URL can't be fanned at high velocity.
	if w.rateLimited(ctx, d.EndpointID) {
		_ = w.repo.DeferDelivery(ctx, d.ID, time.Now().UTC().Add(time.Second))
		return
	}

	secret, err := w.repo.GetEndpointSecret(ctx, d.EndpointID)
	if err != nil {
		w.fail(ctx, d, nil, fmt.Sprintf("load secret: %v", err), "")
		return
	}

	timestamp := time.Now().UTC()
	signature := Sign(secret, timestamp, d.Payload)

	parsed, err := url.Parse(endpoint.URL)
	if err != nil {
		w.fail(ctx, d, nil, fmt.Sprintf("parse endpoint url: %v", err), "")
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, bytes.NewReader(d.Payload))
	if err != nil {
		w.fail(ctx, d, nil, fmt.Sprintf("build request: %v", err), "")
		return
	}
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

	// Cap the response read so a hostile/large body can't exhaust memory; keep a
	// small excerpt for the delivery log. Drain so the connection is reused.
	bodyExcerpt, respHeaderEcho := readResponse(resp)

	// 2xx = success. 3xx = treated as failure (we never follow redirects). 4xx =
	// caller's fault (still retried briefly in case of a transient flap). 5xx =
	// retry.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Verification/test deliveries flip the endpoint to verified on a 2xx.
		if d.EventType == string(models.WebhookEventEndpointTest) {
			w.settleVerification(ctx, endpoint, d, bodyExcerpt, respHeaderEcho)
		}
		if err := w.repo.MarkDelivered(ctx, d.ID, resp.StatusCode); err != nil {
			log.Warn().Err(err).Msg("Failed to mark webhook delivered")
		}
		_ = w.repo.UpdateEndpointHealthOnSuccess(ctx, d.EndpointID)
		return
	}
	w.fail(ctx, d, &resp.StatusCode, fmt.Sprintf("non-2xx response: %d", resp.StatusCode), bodyExcerpt)
}

// settleVerification marks an endpoint verified after a successful challenge. A
// 2xx proves reachability (enough for org-level endpoints); echoing the
// challenge proves URL control and is REQUIRED for app-scoped endpoints.
func (w *DeliveryWorker) settleVerification(ctx context.Context, endpoint *models.WebhookEndpoint, d *models.WebhookDelivery, bodyExcerpt, headerEcho string) {
	token := challengeToken(d.Payload)
	echoed := token != "" && (strings.Contains(bodyExcerpt, token) || headerEcho == token)
	if endpoint.OAuthApplicationID != nil && !echoed {
		// App endpoints must prove ownership by echoing — reachability alone is
		// not enough, so leave the endpoint unverified.
		log.Info().Str("endpoint_id", endpoint.ID.String()).Msg("App webhook endpoint responded but did not echo the challenge; staying unverified")
		return
	}
	if err := w.repo.MarkVerified(ctx, endpoint.ID, echoed); err != nil {
		log.Warn().Err(err).Msg("Failed to mark webhook endpoint verified")
	}
}

func (w *DeliveryWorker) fail(ctx context.Context, d *models.WebhookDelivery, status *int, reason, bodyExcerpt string) {
	consecutive, firstFailure, herr := w.repo.UpdateEndpointHealthOnFailure(ctx, d.EndpointID, reason)
	if herr != nil {
		log.Warn().Err(herr).Msg("Failed to update webhook endpoint failure health")
	}
	// Hysteretic auto-disable: only after sustained failure, so a brief outage
	// never trips it. Stops the platform from hammering a dead/unwilling target.
	if firstFailure != nil && consecutive >= 5 && time.Since(*firstFailure) >= w.autoDisableAfter {
		_ = w.repo.DisableEndpoint(ctx, d.EndpointID, "auto-disabled after sustained delivery failures")
		log.Warn().Str("endpoint_id", d.EndpointID.String()).Msg("Auto-disabled webhook endpoint after sustained failures")
	}

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

// readResponse returns a bounded body excerpt and the echoed challenge header.
func readResponse(resp *http.Response) (excerpt, headerEcho string) {
	headerEcho = strings.TrimSpace(resp.Header.Get(ChallengeHeader))
	if resp.Body == nil {
		return "", headerEcho
	}
	const maxRead = 64 << 10 // 64KB
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, maxRead))
	if len(buf) > 1024 {
		buf = buf[:1024]
	}
	return string(buf), headerEcho
}

// challengeToken extracts the challenge value from a verification delivery's
// payload so the worker can match it against the receiver's echo.
func challengeToken(payload []byte) string {
	var p struct {
		Data struct {
			Challenge string `json:"challenge"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return ""
	}
	return p.Data.Challenge
}
