package partner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	eventRetention     = 30 * 24 * time.Hour
	deliveryWindow     = 72 * time.Hour
	deliveryTimeout    = 10 * time.Second
	previousSecretTTL  = 24 * time.Hour
	deliveryBatch      = 50
	deliveryLease      = 2 * time.Minute
	maxDeliveryBackoff = 6 * time.Hour
)

// Envelope is an event as a partner receives it, by webhook and from GET /events.
type Envelope struct {
	ID                     string          `json:"id"`
	Type                   string          `json:"type"`
	CreatedAt              time.Time       `json:"created_at"`
	OrganizationExternalID *string         `json:"organization_external_id"`
	Livemode               bool            `json:"livemode"`
	Data                   json.RawMessage `json:"data"`
}

func envelope(e models.PartnerEvent) Envelope {
	return Envelope{ID: e.ID, Type: e.Type, CreatedAt: e.CreatedAt.UTC(), OrganizationExternalID: e.OrganizationExternalID,
		Livemode: e.Mode.Livemode(), Data: e.Data}
}

// Publish records an event for a partner: it is delivered to the partner's webhook and listed by GET /events.
// data is the full current object the event is about.
func (s *Service) Publish(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, eventType, orgExternalID string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	e := &models.PartnerEvent{ID: "evt_" + randomHex(12), PartnerID: partnerID, Mode: mode, Type: eventType, Data: raw}
	if orgExternalID != "" {
		e.OrganizationExternalID = &orgExternalID
	}
	return s.Repo.InsertPartnerEvent(ctx, e)
}

// Events is GET /events?after=&limit=: the caller's events in order, kept 30 days.
func (s *Service) Events(ctx context.Context, c *Caller, after string, limit int) (*Page[Envelope], *errx.Error) {
	rows, more, err := s.Repo.ListPartnerEvents(ctx, c.Partner.ID, c.Mode, after, limit)
	if errors.Is(err, repository.ErrPartnerEventCursor) {
		return nil, invalid("after does not name an event.")
	}
	if err != nil {
		return nil, internal(err)
	}
	page := &Page[Envelope]{Data: make([]Envelope, 0, len(rows))}
	for _, e := range rows {
		page.Data = append(page.Data, envelope(e))
	}
	page.Pagination.HasMore = more
	if more && len(rows) > 0 {
		next := rows[len(rows)-1].ID
		page.Pagination.NextCursor = &next
	}
	return page, nil
}

// SignatureHeader signs body with the current secret and, inside a day of a rotation, the previous one too.
func SignatureHeader(now time.Time, body []byte, secrets ...string) string {
	h := fmt.Sprintf("t=%d", now.Unix())
	for _, sec := range secrets {
		if sec != "" {
			h += ",v1=" + webhook.Sign(sec, now, body)
		}
	}
	return h
}

// DeliverDue sends events whose delivery is due; a 2xx within ten seconds counts as delivered.
func (s *Service) DeliverDue(ctx context.Context) error {
	if s.Sealer == nil {
		return nil
	}
	due, err := s.Repo.ClaimDueWebhookEvents(ctx, deliveryBatch, deliveryLease)
	if err != nil {
		return err
	}
	partners := map[uuid.UUID]*deliveryTarget{}
	for _, e := range due {
		t, ok := partners[e.PartnerID]
		if !ok {
			t = s.target(ctx, e.PartnerID)
			partners[e.PartnerID] = t
		}
		s.deliver(ctx, t, e)
	}
	return nil
}

type deliveryTarget struct {
	url     string
	secrets []string
}

func (s *Service) target(ctx context.Context, partnerID uuid.UUID) *deliveryTarget {
	p, err := s.Repo.GetPartner(ctx, partnerID)
	if err != nil || p == nil || p.WebhookURL == nil {
		return nil
	}
	sec, err := s.Repo.GetPartnerSecrets(ctx, partnerID)
	if err != nil || sec == nil || sec.WebhookSecret == "" {
		return nil
	}
	cur, err := s.Sealer.Decrypt(sec.WebhookSecret)
	if err != nil {
		errs.CaptureException(fmt.Errorf("partner webhook secret does not open: %w", err))
		return nil
	}
	t := &deliveryTarget{url: *p.WebhookURL, secrets: []string{cur}}
	if sec.WebhookSecretPrevious != "" && sec.WebhookSecretRotatedAt != nil && s.now().Sub(*sec.WebhookSecretRotatedAt) < previousSecretTTL {
		if prev, err := s.Sealer.Decrypt(sec.WebhookSecretPrevious); err == nil {
			t.secrets = append(t.secrets, prev)
		}
	}
	return t
}

func (s *Service) deliver(ctx context.Context, t *deliveryTarget, e models.PartnerEvent) {
	fail := func(reason string) {
		now := s.now()
		backoff := min(maxDeliveryBackoff, time.Duration(1<<min(e.Attempts, 16))*30*time.Second)
		abandon := now.Sub(e.CreatedAt) > deliveryWindow
		if err := s.Repo.MarkPartnerEventFailed(ctx, e.Seq, now.Add(backoff), reason, abandon); err != nil {
			errs.CaptureException(err)
		}
	}
	if t == nil {
		fail("no webhook endpoint or secret")
		return
	}
	body, err := json.Marshal(envelope(e))
	if err != nil {
		fail("encode")
		return
	}
	rctx, cancel := context.WithTimeout(ctx, deliveryTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		fail("request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Warmbly-Partner-Webhooks/1")
	req.Header.Set(webhook.SignatureHeader, SignatureHeader(s.now(), body, t.secrets...))
	req.Header.Set("X-Warmbly-Event", e.Type)
	req.Header.Set("X-Warmbly-Event-Id", e.ID)
	res, err := s.HTTP.Do(req)
	if err != nil {
		fail("transport error")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	_ = res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		fail(fmt.Sprintf("status %d", res.StatusCode))
		return
	}
	if err := s.Repo.MarkPartnerEventDelivered(ctx, e.Seq); err != nil {
		errs.CaptureException(err)
	}
}
