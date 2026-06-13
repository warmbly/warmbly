package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/safehttp"
)

// actionHTTP is the shared client for outbound provider action calls. It is
// SSRF-hardened (resolves + blocks non-public hosts at dial time) because most of
// these calls go to user-supplied URLs. Short timeout — a slow third party
// shouldn't pin a dispatch goroutine for long.
var actionHTTP = safehttp.Client(15 * time.Second)

// automationEventPayload is the structured, versioned body delivered to generic
// automation webhooks (Zapier / Make / n8n). The legacy flat fields
// (content/email/subject/title) are kept so existing Zaps keep working; the
// `event` discriminator + `data` object are what automations actually branch on.
type automationEventPayload struct {
	ID        string         `json:"id"`
	Event     string         `json:"event"`
	Version   string         `json:"version"`
	CreatedAt string         `json:"created_at"`
	Data      map[string]any `json:"data"`
	Content   string         `json:"content"`
	Email     string         `json:"email,omitempty"`
	Subject   string         `json:"subject,omitempty"`
	Title     string         `json:"title,omitempty"`
}

// automationDeliver POSTs a structured event to a generic automation webhook,
// HMAC-signing it with the connection's signing secret (same scheme as our
// customer webhooks: `X-Warmbly-Signature: t=<unix>,v1=<hmac>` over `t.body`)
// and retrying transient (5xx / network) failures with linear backoff. 4xx is
// treated as a permanent misconfiguration and not retried.
func automationDeliver(ctx context.Context, targetURL, secret, eventType string, payload automationEventPayload) error {
	body, _ := json.Marshal(payload)
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 400 * time.Millisecond):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(webhook.EventHeader, eventType)
		req.Header.Set(webhook.EventIDHeader, payload.ID)
		if secret != "" {
			ts := time.Now()
			sig := webhook.Sign(secret, ts, body)
			req.Header.Set(webhook.SignatureHeader, webhook.FormatSignatureHeader(ts, sig))
		}
		resp, err := actionHTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("webhook POST: HTTP %d", resp.StatusCode)
		if resp.StatusCode < 500 {
			return lastErr // client error: retrying won't help
		}
	}
	return lastErr
}

// newDeliveryID returns a fresh idempotency / delivery id for a webhook event.
func newDeliveryID() string { return uuid.New().String() }

// httpResponseBodyLimit caps how much of a response we read + keep so a huge
// response can't blow up memory (it lives in the run's in-memory event data).
const httpResponseBodyLimit = 64 << 10 // 64 KiB

// runHTTPRequest performs the configurable HTTP node: render method/URL/headers/
// query/body against the event data, SSRF-guard the URL, call it with bounded
// retry, and write the response back into `data` under the node's output key
// (default "response") so downstream nodes can template {{.response.body...}}
// and condition nodes can branch on {{.response.ok}}.
func runHTTPRequest(ctx context.Context, orgID, automationID uuid.UUID, n models.AutomationNode, cfg nativeActionConfig, data map[string]any) error {
	method := strings.ToUpper(strings.TrimSpace(cfg.HTTPMethod))
	if method == "" {
		method = http.MethodPost
	}
	rawURL := strings.TrimSpace(renderTemplate(cfg.HTTPURL, data))
	if rawURL == "" {
		return fmt.Errorf("http request needs a url")
	}
	// SSRF + HTTPS guard (same policy as outbound webhooks), re-checked here at
	// execution time, not just at save time.
	if err := webhook.ValidateOutboundURL(rawURL); err != nil {
		return fmt.Errorf("http url rejected: %w", err)
	}
	if len(cfg.HTTPQuery) > 0 {
		u, err := url.Parse(rawURL)
		if err != nil {
			return fmt.Errorf("invalid http url: %w", err)
		}
		q := u.Query()
		for k, v := range cfg.HTTPQuery {
			q.Set(k, renderTemplate(v, data))
		}
		u.RawQuery = q.Encode()
		rawURL = u.String()
	}
	body := renderTemplate(cfg.HTTPBody, data)

	outKey := strings.TrimSpace(cfg.HTTPOutputKey)
	if outKey == "" {
		outKey = "response"
	}

	// Abuse trail: log every outbound HTTP-request action with org/automation
	// attribution so a misuse pattern (scanning, relaying) is reviewable.
	host := rawURL
	if pu, perr := url.Parse(rawURL); perr == nil {
		host = pu.Hostname()
	}
	log.Info().Str("org_id", orgID.String()).Str("automation_id", automationID.String()).
		Str("method", method).Str("host", host).Msg("automation http_request outbound")

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Warmbly-Automations/1.0")
		for k, v := range cfg.HTTPHeaders {
			req.Header.Set(k, renderTemplate(v, data))
		}

		resp, err := actionHTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		out := readHTTPOutput(resp)
		_ = resp.Body.Close()
		data[outKey] = out
		recordStepOutput(n, data, out)

		if ok, _ := out["ok"].(bool); ok {
			return nil
		}
		status, _ := out["status"].(int)
		lastErr = fmt.Errorf("http %s -> %d", method, status)
		if status >= 400 && status < 500 {
			return lastErr // client error: retrying won't help
		}
	}

	// A blocked destination is an SSRF attempt worth flagging with attribution.
	if errors.Is(lastErr, safehttp.ErrBlockedAddress) {
		log.Warn().Str("org_id", orgID.String()).Str("automation_id", automationID.String()).
			Str("host", host).Msg("automation http_request blocked: non-public destination")
	}

	// Network failure on every attempt — still record a failure response so a
	// downstream condition on {{.response.ok}} can route to an error branch.
	if _, ok := data[outKey]; !ok {
		fail := map[string]any{"ok": false, "status": 0, "error": lastErr.Error()}
		data[outKey] = fail
		recordStepOutput(n, data, fail)
	}
	return lastErr
}

func readHTTPOutput(resp *http.Response) map[string]any {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, httpResponseBodyLimit))
	out := map[string]any{
		"status":  resp.StatusCode,
		"ok":      resp.StatusCode >= 200 && resp.StatusCode < 300,
		"text":    string(raw),
		"headers": flattenHeaders(resp.Header),
	}
	// Parse JSON bodies so {{.response.body.field}} works downstream.
	var parsed any
	if json.Unmarshal(raw, &parsed) == nil {
		out["body"] = parsed
	}
	return out
}

func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k := range h {
		out[k] = h.Get(k)
	}
	return out
}

// recordStepOutput also stores the response under data["steps"][nodeID] so a
// later node can reference a specific earlier call via {{index .steps "<id>"}}.
func recordStepOutput(n models.AutomationNode, data map[string]any, out map[string]any) {
	steps, ok := data["steps"].(map[string]any)
	if !ok {
		steps = map[string]any{}
		data["steps"] = steps
	}
	steps[n.ID] = out
}

// Warmbly's sky accent (Tailwind sky-500, #0EA5E9) brands outbound notification
// cards: an integer for Discord embeds, a hex string for Slack attachments.
const (
	notifyAccentInt = 0x0EA5E9
	notifyAccentHex = "#0EA5E9"
)

// truncateRunes caps a string at max runes (provider embed/field limits), adding
// an ellipsis when it trims so a long custom template can't get rejected.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

// notifyFields turns the message's contact/subject into structured key-value
// fields shared by the Slack and Discord cards (omitted when empty).
func (m eventMessage) notifyFields() (contact, subject string) {
	return m.Email, m.Subject
}

// slackPostMessage posts to a channel using a bot token from the OAuth connect.
// It renders a sky-accented attachment card (title + contact/subject fields)
// rather than a bare line, with a plain-text fallback for the notification
// preview. Slack returns HTTP 200 with {ok:false,error:...} on failure, so we
// inspect the body rather than the status code.
func slackPostMessage(ctx context.Context, token, channel string, msg eventMessage) error {
	if channel == "" {
		return fmt.Errorf("no slack channel configured")
	}
	attachment := map[string]any{
		"color":    notifyAccentHex,
		"fallback": msg.plainText(),
		"title":    truncateRunes(msg.Title, 256),
		"footer":   "Warmbly",
		"ts":       time.Now().Unix(),
	}
	if msg.Custom != "" {
		attachment["text"] = truncateRunes(msg.Custom, 3000)
	}
	contact, subject := msg.notifyFields()
	var fields []map[string]any
	if contact != "" {
		fields = append(fields, map[string]any{"title": "Contact", "value": contact, "short": true})
	}
	if subject != "" {
		fields = append(fields, map[string]any{"title": "Subject", "value": subject, "short": true})
	}
	if len(fields) > 0 {
		attachment["fields"] = fields
	}
	body, _ := json.Marshal(map[string]any{
		"channel":     channel,
		"text":        msg.Title,
		"attachments": []map[string]any{attachment},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://slack.com/api/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := actionHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = json.Unmarshal(raw, &out)
	if !out.OK {
		if out.Error == "" {
			out.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("slack chat.postMessage: %s", out.Error)
	}
	return nil
}

// discordEmbedPayload builds a Discord webhook body as a single rich embed in
// Warmbly's sky theme (title + optional description + contact/subject fields +
// footer/timestamp) rather than a plain content line, so notifications render as
// branded cards. Discord ignores unknown top-level keys, so embeds are the body.
func discordEmbedPayload(msg eventMessage) map[string]any {
	embed := map[string]any{
		"title":     truncateRunes(msg.Title, 256),
		"color":     notifyAccentInt,
		"footer":    map[string]any{"text": "Warmbly"},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	if msg.Custom != "" {
		embed["description"] = truncateRunes(msg.Custom, 4096)
	}
	contact, subject := msg.notifyFields()
	var fields []map[string]any
	if contact != "" {
		fields = append(fields, map[string]any{"name": "Contact", "value": truncateRunes(contact, 1024), "inline": true})
	}
	if subject != "" {
		fields = append(fields, map[string]any{"name": "Subject", "value": truncateRunes(subject, 1024), "inline": true})
	}
	if len(fields) > 0 {
		embed["fields"] = fields
	}
	return map[string]any{"embeds": []map[string]any{embed}}
}

// discordNotify delivers a sky-themed embed card to a Discord channel webhook.
func discordNotify(ctx context.Context, url string, msg eventMessage) error {
	body, _ := json.Marshal(discordEmbedPayload(msg))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := actionHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook POST: HTTP %d", resp.StatusCode)
	}
	return nil
}

// hubspotUpsertContact creates or updates a HubSpot contact keyed by email using
// the caller-projected props, and optionally logs a note on the contact timeline.
// props are already resolved through the connection's field map.
func hubspotUpsertContact(ctx context.Context, token, email string, props map[string]any, note string) error {
	if email == "" {
		// Nothing to key on; treat as a no-op success.
		return nil
	}
	if props == nil {
		props = map[string]any{}
	}
	props["email"] = email // email is the match key; always present

	// Find existing contact by email.
	searchBody, _ := json.Marshal(map[string]any{
		"filterGroups": []map[string]any{{
			"filters": []map[string]any{{
				"propertyName": "email", "operator": "EQ", "value": email,
			}},
		}},
		"properties": []string{"email"},
		"limit":      1,
	})
	var search struct {
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	if err := hubspotJSON(ctx, http.MethodPost, "https://api.hubapi.com/crm/v3/objects/contacts/search", token, searchBody, &search); err != nil {
		return err
	}

	contactID := ""
	if len(search.Results) > 0 {
		contactID = search.Results[0].ID
		upd, _ := json.Marshal(map[string]any{"properties": props})
		if err := hubspotJSON(ctx, http.MethodPatch, "https://api.hubapi.com/crm/v3/objects/contacts/"+contactID, token, upd, nil); err != nil {
			return err
		}
	} else {
		create, _ := json.Marshal(map[string]any{"properties": props})
		var created struct {
			ID string `json:"id"`
		}
		if err := hubspotJSON(ctx, http.MethodPost, "https://api.hubapi.com/crm/v3/objects/contacts", token, create, &created); err != nil {
			return err
		}
		contactID = created.ID
	}

	// Best-effort note on the timeline.
	if contactID != "" && note != "" {
		noteBody, _ := json.Marshal(map[string]any{
			"properties": map[string]any{
				"hs_note_body": note,
				"hs_timestamp": time.Now().UTC().Format(time.RFC3339),
			},
			"associations": []map[string]any{{
				"to": map[string]any{"id": contactID},
				"types": []map[string]any{{
					"associationCategory": "HUBSPOT_DEFINED",
					"associationTypeId":   202,
				}},
			}},
		})
		_ = hubspotJSON(ctx, http.MethodPost, "https://api.hubapi.com/crm/v3/objects/notes", token, noteBody, nil)
	}
	return nil
}

func hubspotJSON(ctx context.Context, method, url, token string, body []byte, dst any) error {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := actionHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("hubspot %s %s: HTTP %d", method, shortURL(url), resp.StatusCode)
	}
	if dst != nil && len(raw) > 0 {
		return json.Unmarshal(raw, dst)
	}
	return nil
}

// pipedriveUpsertPerson creates a Pipedrive person keyed by email using the
// caller-projected props. Pipedrive's REST API has no true upsert, so we search
// first and skip when the person already exists (keeping the action idempotent).
// email/phone props are reshaped into Pipedrive's array form; other keys (name,
// custom-field hashes) pass through as-is.
func pipedriveUpsertPerson(ctx context.Context, token, email string, props map[string]any) error {
	if email == "" {
		return nil
	}

	// Search for an existing person by email.
	searchURL := "https://api.pipedrive.com/v1/persons/search?term=" + url.QueryEscape(email) + "&fields=email&exact_match=true"
	var search struct {
		Data struct {
			Items []struct {
				Item struct {
					ID int64 `json:"id"`
				} `json:"item"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := pipedriveJSON(ctx, http.MethodGet, searchURL, token, nil, &search); err != nil {
		return err
	}
	if len(search.Data.Items) > 0 {
		return nil // already present; nothing to do
	}

	payload := map[string]any{}
	for k, v := range props {
		switch k {
		case "email":
			payload["email"] = []string{toStr(v)}
		case "phone":
			payload["phone"] = []string{toStr(v)}
		default:
			payload[k] = v
		}
	}
	if strProp(payload, "name") == "" {
		payload["name"] = email
	}
	if _, ok := payload["email"]; !ok {
		payload["email"] = []string{email}
	}
	create, _ := json.Marshal(payload)
	return pipedriveJSON(ctx, http.MethodPost, "https://api.pipedrive.com/v1/persons", token, create, nil)
}

func pipedriveJSON(ctx context.Context, method, url, token string, body []byte, dst any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := actionHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("pipedrive %s: HTTP %d", method, resp.StatusCode)
	}
	if dst != nil && len(raw) > 0 {
		return json.Unmarshal(raw, dst)
	}
	return nil
}

func shortURL(u string) string {
	if i := len("https://api.hubapi.com"); len(u) > i {
		return u[i:]
	}
	return u
}

// closeUpsertLead creates a Close lead (with an embedded contact) keyed by email
// using the caller-projected props. Close has no native upsert, so we search by
// email first and skip when a lead already carries the address — keeping the
// action idempotent so retries / repeated pushes don't fan out duplicate leads.
// Auth is HTTP Basic with the API key as the username and a blank password.
func closeUpsertLead(ctx context.Context, apiKey, email string, props map[string]any) error {
	if email == "" {
		return nil
	}

	// Idempotency guard: skip if a lead already has this email address.
	searchURL := "https://api.close.com/api/v1/lead/?_fields=id&query=" +
		url.QueryEscape("email_address:\""+email+"\"")
	var search struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := closeJSON(ctx, http.MethodGet, searchURL, apiKey, nil, &search); err != nil {
		return err
	}
	if len(search.Data) > 0 {
		return nil
	}

	name := strProp(props, "name")
	if name == "" {
		name = email
	}
	leadName := strProp(props, "company")
	if leadName == "" {
		leadName = name
	}

	contact := map[string]any{
		"name":   name,
		"emails": []map[string]any{{"email": email, "type": "office"}},
	}
	if phone := strProp(props, "phone"); phone != "" {
		contact["phones"] = []map[string]any{{"phone": phone, "type": "office"}}
	}
	body, _ := json.Marshal(map[string]any{
		"name":     leadName,
		"contacts": []map[string]any{contact},
	})
	return closeJSON(ctx, http.MethodPost, "https://api.close.com/api/v1/lead/", apiKey, body, nil)
}

func closeJSON(ctx context.Context, method, reqURL, apiKey string, body []byte, dst any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		return err
	}
	req.SetBasicAuth(apiKey, "")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := actionHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("close %s: HTTP %d", method, resp.StatusCode)
	}
	if dst != nil && len(raw) > 0 {
		return json.Unmarshal(raw, dst)
	}
	return nil
}

// salesforceAPIVersion is the REST API version actions target. Salesforce keeps
// old versions live for years, so pinning one keeps request shapes stable.
const salesforceAPIVersion = "v59.0"

// salesforceUpsertContact creates or updates a Salesforce Contact keyed by email
// using the caller-projected props. instanceURL is the connected org's API host,
// captured at OAuth time and stored in the connection's display fields. LastName
// is mandatory on the Contact object, so we fall back to the email when unset.
func salesforceUpsertContact(ctx context.Context, token, instanceURL, email string, props map[string]any) error {
	instanceURL = strings.TrimRight(strings.TrimSpace(instanceURL), "/")
	if instanceURL == "" {
		return fmt.Errorf("salesforce instance url unavailable; reconnect the integration")
	}
	if email == "" {
		return nil
	}

	fields := map[string]any{}
	for k, v := range props {
		fields[k] = v
	}
	fields["Email"] = email
	if strProp(fields, "LastName") == "" {
		fields["LastName"] = email // LastName is a required Contact field.
	}

	// Find an existing contact by email (SOQL — escape embedded single quotes).
	soql := "SELECT Id FROM Contact WHERE Email = '" + strings.ReplaceAll(email, "'", "\\'") + "' LIMIT 1"
	queryURL := instanceURL + "/services/data/" + salesforceAPIVersion + "/query?q=" + url.QueryEscape(soql)
	var q struct {
		Records []struct {
			ID string `json:"Id"`
		} `json:"records"`
	}
	if err := salesforceJSON(ctx, http.MethodGet, queryURL, token, nil, &q); err != nil {
		return err
	}

	base := instanceURL + "/services/data/" + salesforceAPIVersion + "/sobjects/Contact"
	body, _ := json.Marshal(fields)
	if len(q.Records) > 0 {
		// PATCH returns 204 No Content on success.
		return salesforceJSON(ctx, http.MethodPatch, base+"/"+q.Records[0].ID, token, body, nil)
	}
	return salesforceJSON(ctx, http.MethodPost, base+"/", token, body, nil)
}

func salesforceJSON(ctx context.Context, method, reqURL, token string, body []byte, dst any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := actionHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("salesforce %s: HTTP %d", method, resp.StatusCode)
	}
	if dst != nil && len(raw) > 0 {
		return json.Unmarshal(raw, dst)
	}
	return nil
}
