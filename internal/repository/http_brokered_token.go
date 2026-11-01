package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/pkg/nodeauth"
	"golang.org/x/oauth2"
)

// BrokeredTokenClient fetches short-lived access tokens for cloud-managed mailboxes; no refresh grant reaches the worker.
type BrokeredTokenClient interface {
	Token(ctx context.Context, accountID uuid.UUID) (*oauth2.Token, error)
	// Source adapts a mailbox to oauth2; the returned source caches until expiry.
	Source(accountID uuid.UUID) oauth2.TokenSource
}

type httpBrokeredTokenClient struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewHTTPBrokeredTokenClient returns the proxy for GET {BaseURL}/api/v1/internal/cloud-link/token/:id.
func NewHTTPBrokeredTokenClient(baseURL, token string) (BrokeredTokenClient, error) {
	if baseURL == "" {
		return nil, errors.New("brokered_token.http: baseURL is required")
	}
	if token == "" {
		return nil, errors.New("brokered_token.http: token is required")
	}
	return &httpBrokeredTokenClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  newControlPlaneClient(25 * time.Second),
	}, nil
}

// BrokeredTokenRefused is a definitive no from the cloud, not a transient failure.
type BrokeredTokenRefused struct {
	Code    string
	Message string
}

func (e *BrokeredTokenRefused) Error() string {
	return fmt.Sprintf("brokered token refused (%s): %s", e.Code, e.Message)
}

func (r *httpBrokeredTokenClient) Token(ctx context.Context, accountID uuid.UUID) (*oauth2.Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/api/v1/internal/cloud-link/token/"+accountID.String(), nil)
	if err != nil {
		return nil, err
	}
	nodeauth.Authorize(req, r.token)
	req.Header.Set("User-Agent", "warmbly-worker/brokered-token-http")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		var env struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		_ = json.Unmarshal(body, &env)
		if env.Message == "" {
			env.Message = env.Error
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized {
			return nil, &BrokeredTokenRefused{Code: env.Code, Message: env.Message}
		}
		return nil, fmt.Errorf("brokered_token.http: status %d: %s", resp.StatusCode, env.Message)
	}
	var out struct {
		AccessToken string    `json:"access_token"`
		ExpiresAt   time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		return nil, errors.New("brokered_token.http: empty access token")
	}
	return &oauth2.Token{AccessToken: out.AccessToken, TokenType: "Bearer", Expiry: out.ExpiresAt}, nil
}

func (r *httpBrokeredTokenClient) Source(accountID uuid.UUID) oauth2.TokenSource {
	return oauth2.ReuseTokenSource(nil, brokeredSource{client: r, accountID: accountID})
}

type brokeredSource struct {
	client    *httpBrokeredTokenClient
	accountID uuid.UUID
}

func (b brokeredSource) Token() (*oauth2.Token, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	return b.client.Token(ctx, b.accountID)
}
