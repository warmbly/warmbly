package advanced

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/safehttp"
)

// campaignStepHTTP is the SSRF-safe client for the campaign "HTTP request" step:
// it validates the resolved IP at dial time (no internal/metadata targets) on top
// of the URL-level check, so a templated URL can't reach a private address.
var campaignStepHTTP = safehttp.Client(15 * time.Second)

// FireCampaignEvent publishes a developer-defined custom event to the realtime
// gateway from a campaign "fire event" step. The event name + each field value
// are templated against the contact; the fields become the event payload.
// Subscribers (an API key with REALTIME_SUBSCRIBE on the org websocket) receive
// it with no public URL. Best-effort — a publish hiccup never blocks sending.
func (s *service) FireCampaignEvent(ctx context.Context, orgID uuid.UUID, sourceID, name string, fields []models.ActionKV, contact *models.Contact) {
	if s.realtime == nil || orgID == uuid.Nil || contact == nil {
		return
	}
	evName := strings.TrimSpace(renderContactTemplate(name, contact))
	if evName == "" {
		return
	}
	payload := make(map[string]string, len(fields))
	for _, f := range fields {
		key := strings.TrimSpace(f.Key)
		if key == "" {
			continue
		}
		payload[key] = renderContactTemplate(f.Value, contact)
	}
	s.realtime.PublishCustomEvent(ctx, orgID, uuid.Nil, evName, payload, "campaign", sourceID)
}

// RunCampaignHTTPRequest makes a configurable outbound call from a campaign
// "HTTP request" step. Method/URL/headers/body are templated against the contact
// and the URL is SSRF-validated. Best-effort: a non-2xx or transport error is
// returned for logging but never aborts the campaign.
func (s *service) RunCampaignHTTPRequest(ctx context.Context, orgID uuid.UUID, cfg *models.ActionConfig, contact *models.Contact) error {
	_ = orgID
	if cfg == nil || contact == nil {
		return nil
	}
	method := strings.ToUpper(strings.TrimSpace(cfg.HTTPMethod))
	if method == "" {
		method = http.MethodPost
	}
	rawURL := strings.TrimSpace(renderContactTemplate(cfg.HTTPURL, contact))
	if rawURL == "" {
		return nil
	}
	if err := webhook.ValidateOutboundURL(rawURL); err != nil {
		return fmt.Errorf("http url rejected: %w", err)
	}
	body := renderContactTemplate(cfg.HTTPBody, contact)
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewBufferString(body))
	if err != nil {
		return err
	}
	if body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range cfg.HTTPHeaders {
		if k = strings.TrimSpace(k); k != "" {
			req.Header.Set(k, renderContactTemplate(v, contact))
		}
	}
	resp, err := campaignStepHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http request returned %d", resp.StatusCode)
	}
	return nil
}
