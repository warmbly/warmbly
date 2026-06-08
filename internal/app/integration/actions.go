package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/webhook"
)

// actionHTTP is the shared client for outbound provider action calls. Short
// timeout — a slow third party shouldn't pin a dispatch goroutine for long.
var actionHTTP = &http.Client{Timeout: 15 * time.Second}

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

// slackPostMessage posts to a channel using a bot token from the OAuth connect.
// Slack returns HTTP 200 with {ok:false,error:...} on failure, so we inspect
// the body rather than the status code.
func slackPostMessage(ctx context.Context, token, channel string, msg eventMessage) error {
	if channel == "" {
		return fmt.Errorf("no slack channel configured")
	}
	body, _ := json.Marshal(map[string]any{
		"channel": channel,
		"text":    msg.plainText(),
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

// webhookPost delivers a Discord-compatible payload (and works for any generic
// JSON webhook): Discord requires a top-level "content" string.
func webhookPost(ctx context.Context, url string, msg eventMessage) error {
	body, _ := json.Marshal(map[string]any{
		"content": msg.plainText(),
		"email":   msg.Email,
		"subject": msg.Subject,
		"title":   msg.Title,
	})
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
