package repository

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
)

// httpEmailMessageMapRepository is the worker-side adapter. Workers cannot
// connect to Postgres directly (per CLAUDE.md), so they reach the message map
// over HTTPS by calling the backend's internal endpoint, mirroring the DEK HTTP
// store (internal/infrastructure/encryptedkeys/http.go).
//
//	PUT    {BaseURL}/api/v1/internal/email-message-map  body: emailMessageMapPayload -> 204
//	GET    {BaseURL}/api/v1/internal/email-message-map?user_id=&email_id=&message_id=
//	   200 emailMessageMapPayload | 404 (no mapping -> nil)
//	DELETE {BaseURL}/api/v1/internal/email-message-map?user_id=&email_id=&message_id=&id= -> 204
//
// Auth: Authorization: Bearer <config.NodeAPIToken()>.
type httpEmailMessageMapRepository struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewHTTPEmailMessageMapRepository returns the worker-side implementation.
func NewHTTPEmailMessageMapRepository(baseURL, token string) (EmailMessageMapRepository, error) {
	if baseURL == "" {
		return nil, errors.New("email_message_map.http: baseURL is required")
	}
	if token == "" {
		return nil, errors.New("email_message_map.http: token is required")
	}
	return &httpEmailMessageMapRepository{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// emailMessageMapPayload is the wire shape shared with the backend handler. It
// is field-compatible with EmailMessageData (same field names/types/order), so
// the two convert directly.
type emailMessageMapPayload struct {
	UserID    string `json:"user_id"`
	EmailID   string `json:"email_id"`
	MessageID string `json:"message_id"`
	ID        string `json:"id"`
	ThreadID  string `json:"thread_id"`
}

func (r *httpEmailMessageMapRepository) endpoint() string {
	return r.baseURL + "/api/v1/internal/email-message-map"
}

func (r *httpEmailMessageMapRepository) authed(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("User-Agent", "warmbly-worker/email-message-map-http")
}

func (r *httpEmailMessageMapRepository) Add(ctx context.Context, data EmailMessageData) error {
	body, err := json.Marshal(emailMessageMapPayload(data))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, r.endpoint(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	r.authed(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("email_message_map.http: add: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK, http.StatusCreated:
		return nil
	default:
		return fmt.Errorf("email_message_map.http: add: unexpected status %d", resp.StatusCode)
	}
}

func (r *httpEmailMessageMapRepository) AdmitArrival(ctx context.Context, data EmailMessageData, pending *PendingArrival) error {
	body, err := json.Marshal(struct {
		Map     emailMessageMapPayload `json:"map"`
		Pending *PendingArrival        `json:"pending"`
	}{emailMessageMapPayload(data), pending})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint()+"/arrival", bytes.NewReader(body))
	if err != nil {
		return err
	}
	r.authed(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("arrival admission: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return ErrArrivalOutboxUnsupported
	}
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("X-Warmbly-Arrival-Durable") != "1" {
		return fmt.Errorf("arrival admission not confirmed: status %d", resp.StatusCode)
	}
	return nil
}

func (r *httpEmailMessageMapRepository) Get(ctx context.Context, userID, emailID uuid.UUID, messageID string) (*EmailMessageData, error) {
	q := url.Values{}
	q.Set("user_id", userID.String())
	q.Set("email_id", emailID.String())
	q.Set("message_id", messageID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.endpoint()+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	r.authed(req)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("email_message_map.http: get: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return nil, nil
	case http.StatusOK:
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		var p emailMessageMapPayload
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("email_message_map.http: decode: %w", err)
		}
		d := EmailMessageData(p)
		return &d, nil
	default:
		return nil, fmt.Errorf("email_message_map.http: get: unexpected status %d", resp.StatusCode)
	}
}

func (r *httpEmailMessageMapRepository) Del(ctx context.Context, userID, emailID uuid.UUID, messageID string, id uuid.UUID) error {
	q := url.Values{}
	q.Set("user_id", userID.String())
	q.Set("email_id", emailID.String())
	q.Set("message_id", messageID)
	if id != uuid.Nil {
		q.Set("id", id.String())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, r.endpoint()+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	r.authed(req)

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("email_message_map.http: del: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("email_message_map.http: del: unexpected status %d", resp.StatusCode)
	}
	return nil
}
