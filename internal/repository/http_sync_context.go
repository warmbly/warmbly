package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// StoredFolderMessage is one message the platform holds for a mailbox folder:
// the UID it was filed under, the internal row id, and its RFC Message-ID.
// The IMAP drafts reconciliation diffs this against the folder's current UID
// set, so it can remove the rows an expunge left behind.
type StoredFolderMessage struct {
	UID       uint32    `json:"uid"`
	ID        uuid.UUID `json:"id"`
	MessageID string    `json:"message_id"`
}

// ProviderFolderMessage is one message the platform holds for a mailbox whose
// provider keys messages by id (Gmail): the row, that id, the folder the
// provider last had it in, and its date. The Gmail folder reconciliation
// checks these against where Gmail has each message now.
type ProviderFolderMessage struct {
	ID             uuid.UUID `json:"id"`
	ProviderID     string    `json:"provider_id"`
	MessageID      string    `json:"message_id,omitempty"`
	ProviderFolder string    `json:"provider_folder"`
	InternalDate   time.Time `json:"internal_date"`
	Flags          []string  `json:"flags,omitempty"`
}

// SyncContextRepository is the worker's view of the control plane's answer to
// "is this a conversation the mailbox owns?" and "what does the platform still
// hold for this folder?". Workers cannot reach Postgres, so the only
// implementation is the HTTP proxy below.
type SyncContextRepository interface {
	IsOwnConversation(ctx context.Context, userID, emailID uuid.UUID, messageIDs []string, threadID string) (bool, error)
	// ListFolderMessages returns the platform's rows for one folder in the
	// current UIDVALIDITY generation. uidValidity scopes it so rows whose
	// UIDs a UIDVALIDITY change voided are not compared, and therefore not
	// deleted.
	ListFolderMessages(ctx context.Context, userID, emailID uuid.UUID, folderPath string, uidValidity uint32) ([]StoredFolderMessage, error)
	// ListProviderFolderMessages returns the newest rows the provider last
	// placed in one of folders, at most limit of them.
	ListProviderFolderMessages(ctx context.Context, userID, emailID uuid.UUID, folders []string, limit int) ([]ProviderFolderMessage, error)
	ListProviderMessages(ctx context.Context, userID, emailID uuid.UUID, after *uuid.UUID, limit int) ([]ProviderFolderMessage, error)
}

var ErrSyncContextUnsupported = errors.New("provider message enumeration unavailable; upgrade backend first")

func (r *httpSyncContextRepository) ListProviderMessages(ctx context.Context, userID, emailID uuid.UUID, after *uuid.UUID, limit int) ([]ProviderFolderMessage, error) {
	q := url.Values{}
	q.Set("user_id", userID.String())
	q.Set("email_id", emailID.String())
	q.Set("limit", strconv.Itoa(limit))
	if after != nil {
		q.Set("after", after.String())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/api/v1/internal/sync/provider-messages?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("User-Agent", "warmbly-worker/sync-context-http")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return nil, ErrSyncContextUnsupported
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &ControlPlaneHTTPError{Status: resp.StatusCode}
	}
	var out struct {
		Messages []ProviderFolderMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Messages, nil
}

type httpSyncContextRepository struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewHTTPSyncContextRepository returns the worker-side proxy for
// GET {BaseURL}/api/v1/internal/sync/own-conversation,
// GET {BaseURL}/api/v1/internal/sync/folder-messages and
// GET {BaseURL}/api/v1/internal/sync/provider-folder-messages.
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
		return false, &ControlPlaneHTTPError{Status: resp.StatusCode}
	}
	var out struct {
		Own bool `json:"own"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	return out.Own, nil
}

// ListFolderMessages asks the control plane what it still holds for one folder.
func (r *httpSyncContextRepository) ListFolderMessages(ctx context.Context, userID, emailID uuid.UUID, folderPath string, uidValidity uint32) ([]StoredFolderMessage, error) {
	q := url.Values{}
	q.Set("user_id", userID.String())
	q.Set("email_id", emailID.String())
	q.Set("folder_path", folderPath)
	q.Set("uid_validity", strconv.FormatUint(uint64(uidValidity), 10))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/api/v1/internal/sync/folder-messages?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("User-Agent", "warmbly-worker/sync-context-http")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &ControlPlaneHTTPError{Status: resp.StatusCode}
	}
	var out struct {
		Messages []StoredFolderMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Messages, nil
}

// ListProviderFolderMessages asks the control plane which rows the provider
// last placed in folders.
func (r *httpSyncContextRepository) ListProviderFolderMessages(ctx context.Context, userID, emailID uuid.UUID, folders []string, limit int) ([]ProviderFolderMessage, error) {
	q := url.Values{}
	q.Set("user_id", userID.String())
	q.Set("email_id", emailID.String())
	q.Set("folders", strings.Join(folders, ","))
	q.Set("limit", strconv.Itoa(limit))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/api/v1/internal/sync/provider-folder-messages?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("User-Agent", "warmbly-worker/sync-context-http")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &ControlPlaneHTTPError{Status: resp.StatusCode}
	}
	var out struct {
		Messages []ProviderFolderMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Messages, nil
}
