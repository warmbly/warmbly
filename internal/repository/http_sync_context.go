package repository

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

// SyncContextRepository is the worker's view of the control plane's answer to
// "is this a conversation the mailbox owns?". Workers cannot reach Postgres,
// so the only implementation is the HTTP proxy below.
type SyncContextRepository interface {
	IsOwnConversation(ctx context.Context, userID, emailID uuid.UUID, messageIDs []string, threadID string) (bool, error)
}

type httpSyncContextRepository struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewHTTPSyncContextRepository returns the worker-side proxy for
// GET {BaseURL}/api/v1/internal/sync/own-conversation.
func NewHTTPSyncContextRepository(baseURL, token string) (SyncContextRepository, error) {
	if baseURL == "" {
		return nil, errors.New("sync_context.http: baseURL is required")
	}
	if token == "" {
		return nil, errors.New("sync_context.http: token is required")
	}
	return &httpSyncContextRepository{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (r *httpSyncContextRepository) IsOwnConversation(ctx context.Context, userID, emailID uuid.UUID, messageIDs []string, threadID string) (bool, error) {
	q := url.Values{}
	q.Set("user_id", userID.String())
	q.Set("email_id", emailID.String())
	q.Set("message_ids", strings.Join(messageIDs, ","))
	q.Set("thread_id", threadID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/api/v1/internal/sync/own-conversation?"+q.Encode(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("User-Agent", "warmbly-worker/sync-context-http")
	resp, err := r.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return false, fmt.Errorf("sync_context.http: %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		Own bool `json:"own"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	return out.Own, nil
}
