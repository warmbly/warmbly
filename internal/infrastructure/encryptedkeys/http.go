package encryptedkeys

import (
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

// HTTPStore is the worker-side adapter. Workers cannot connect to Postgres
// directly (per CLAUDE.md), so they reach DEKs over HTTPS by calling the
// backend's internal DEK endpoint, which proxies to the chosen durable store.
//
// The endpoint contract (served by internal/api/handler/internal_dek.go):
//
//	GET    {BaseURL}/api/v1/internal/dek/{orgID}
//	   200 {"encrypted_data_key":"..."}  -> success
//	   404                                -> no DEK stored (returns "")
//
//	PUT    {BaseURL}/api/v1/internal/dek/{orgID}   body: {"encrypted_data_key":"..."}
//	   201                                -> created
//	   409                                -> ErrAlreadyExists
//
//	DELETE {BaseURL}/api/v1/internal/dek/{orgID}
//	   204                                -> idempotent ok
//
// Auth: Authorization: Bearer <ENCRYPTED_KEYS_WORKER_TOKEN>
type HTTPStore struct {
	baseURL string
	token   string
	client  *http.Client
}

// HTTPOption configures HTTPStore.
type HTTPOption func(*HTTPStore)

// WithHTTPClient overrides the underlying http.Client (useful for tests and
// for callers that need custom transports such as netbind.Dialer).
func WithHTTPClient(c *http.Client) HTTPOption {
	return func(s *HTTPStore) { s.client = c }
}

func NewHTTP(baseURL, token string, opts ...HTTPOption) (*HTTPStore, error) {
	if baseURL == "" {
		return nil, errors.New("encryptedkeys.http: baseURL is required")
	}
	if token == "" {
		return nil, errors.New("encryptedkeys.http: token is required")
	}
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("encryptedkeys.http: invalid baseURL: %w", err)
	}
	s := &HTTPStore{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

func (s *HTTPStore) Name() string { return "http" }

func (s *HTTPStore) url(orgID uuid.UUID) string {
	return s.baseURL + "/api/v1/internal/dek/" + orgID.String()
}

func (s *HTTPStore) authed(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("User-Agent", "warmbly-worker/encryptedkeys-http")
}

type dekPayload struct {
	EncryptedDataKey string `json:"encrypted_data_key"`
}

func (s *HTTPStore) Put(ctx context.Context, orgID uuid.UUID, encryptedDEKB64 string) error {
	body, err := json.Marshal(dekPayload{EncryptedDataKey: encryptedDEKB64})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.url(orgID), strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	s.authed(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("encryptedkeys.http: put: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		return nil
	case http.StatusConflict:
		return ErrAlreadyExists
	default:
		return fmt.Errorf("encryptedkeys.http: put: unexpected status %d", resp.StatusCode)
	}
}

func (s *HTTPStore) Get(ctx context.Context, orgID uuid.UUID) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url(orgID), nil)
	if err != nil {
		return "", err
	}
	s.authed(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("encryptedkeys.http: get: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return "", nil
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		var p dekPayload
		if err := json.Unmarshal(body, &p); err != nil {
			return "", fmt.Errorf("encryptedkeys.http: decode: %w", err)
		}
		return p.EncryptedDataKey, nil
	default:
		return "", fmt.Errorf("encryptedkeys.http: get: unexpected status %d", resp.StatusCode)
	}
}

func (s *HTTPStore) Delete(ctx context.Context, orgID uuid.UUID) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.url(orgID), nil)
	if err != nil {
		return err
	}
	s.authed(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("encryptedkeys.http: delete: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("encryptedkeys.http: delete: unexpected status %d", resp.StatusCode)
	}
	return nil
}
