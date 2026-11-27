package graphkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"time"
)

// SocialAPI is the social accounts section of the partner contract.
type SocialAPI interface {
	SocialWorkspace(ctx context.Context, externalID string) (*SocialWorkspace, error)
	CreateLinkSession(ctx context.Context, externalID, returnURL, key string) (*LinkSession, error)
	GetLinkSession(ctx context.Context, externalID, id string) (*LinkSession, error)
	// Unlink ends a linked workspace's link; its accounts stay in the customer's GraphKit workspace.
	Unlink(ctx context.Context, externalID, key string) error

	CreateConnection(ctx context.Context, externalID string, in ConnectInput, key string) (*ConnectSession, error)
	GetConnection(ctx context.Context, externalID, id string) (*ConnectSession, error)
	// SignIn passes credentials or cookies once; GraphKit never stores or returns them.
	SignIn(ctx context.Context, externalID, id string, in SignInInput, key string) (*ConnectSession, error)
	AnswerCheckpoint(ctx context.Context, externalID, id string, in CheckpointAnswer, key string) (*ConnectSession, error)
	ChooseMethod(ctx context.Context, externalID, id, challenge, key string) (*ConnectSession, error)
	ResendCheckpoint(ctx context.Context, externalID, id, key string) (*ConnectSession, error)
	CancelConnection(ctx context.Context, externalID, id, key string) (*ConnectSession, error)

	ListSocialAccounts(ctx context.Context, externalID, status, cursor string, limit int) (*Page[SocialAccount], error)
	GetSocialAccount(ctx context.Context, externalID, id string) (*SocialAccount, error)
	UpdateSocialAccount(ctx context.Context, externalID, id string, patch SocialAccountPatch, key string) (*SocialAccount, error)
	PauseSocialAccount(ctx context.Context, externalID, id, key string) (*SocialAccount, error)
	ResumeSocialAccount(ctx context.Context, externalID, id, key string) (*SocialAccount, error)
	ResyncSocialAccount(ctx context.Context, externalID, id, key string) (*SocialAccount, error)
	RemoveSocialAccount(ctx context.Context, externalID, id, key string) (*SocialAccount, error)

	// SearchCatalog is {"sources": [...]} as GraphKit sends it.
	SearchCatalog(ctx context.Context, externalID string) (json.RawMessage, error)
	SearchPeople(ctx context.Context, externalID string, in SearchInput, key string) (*SearchPage, error)
	Typeahead(ctx context.Context, externalID, source, filter, q, accountID string) ([]TypeaheadItem, error)
	// ParseSearchURL is {"query": Query, ...} as GraphKit sends it.
	ParseSearchURL(ctx context.Context, externalID, rawURL, key string) (json.RawMessage, error)
	SearchLinkedInURL(ctx context.Context, externalID string, query json.RawMessage, key string) (string, error)
	BuildQuery(ctx context.Context, externalID string, in AIInput, key string) (*AIResult, error)
	GetProfile(ctx context.Context, externalID, identifier, accountID string) (*Profile, error)

	CreateAction(ctx context.Context, externalID string, in ActionInput, key string) (*Action, error)
	GetAction(ctx context.Context, externalID, id string) (*Action, error)
	ListActions(ctx context.Context, externalID string, f ActionFilter) (*Page[Action], error)
	CancelAction(ctx context.Context, externalID, id, key string) (*Action, error)

	ListConversations(ctx context.Context, externalID string, f ConversationFilter) (*Page[Conversation], error)
	GetConversation(ctx context.Context, externalID, id string) (*Conversation, error)
	ListMessages(ctx context.Context, externalID, conversationID, cursor string, limit int) (*Page[SocialMessage], error)
	SendMessage(ctx context.Context, externalID, conversationID string, in MessageInput, key string) (*SocialMessage, error)
	MarkConversationRead(ctx context.Context, externalID, conversationID, key string) (*Conversation, error)

	// UploadAudio stores a recording for voice notes in the workspace's audio storage.
	UploadAudio(ctx context.Context, externalID string, in AudioInput, key string) (*Audio, error)
	DeleteAudio(ctx context.Context, externalID, id, key string) error
}

// MessageInput is a reply: a body, or a voice note where the channel takes one.
type MessageInput struct {
	Body  *string     `json:"body,omitempty"`
	Voice *VoiceInput `json:"voice,omitempty"`
}

func socialPath(externalID string, parts ...string) string {
	p := "/accounts/" + esc(externalID) + "/social"
	for _, s := range parts {
		p += "/" + esc(s)
	}
	return p
}

func (c *Client) SocialWorkspace(ctx context.Context, externalID string) (*SocialWorkspace, error) {
	var out SocialWorkspace
	return ret(&out, c.get(ctx, socialPath(externalID), nil, &out))
}

func (c *Client) CreateLinkSession(ctx context.Context, externalID, returnURL, key string) (*LinkSession, error) {
	var out LinkSession
	in := map[string]string{"return_url": returnURL}
	return ret(&out, c.send(ctx, http.MethodPost, socialPath(externalID, "link-sessions"), in, key, &out))
}

func (c *Client) GetLinkSession(ctx context.Context, externalID, id string) (*LinkSession, error) {
	var out LinkSession
	return ret(&out, c.get(ctx, socialPath(externalID, "link-sessions", id), nil, &out))
}

func (c *Client) Unlink(ctx context.Context, externalID, key string) error {
	return c.send(ctx, http.MethodDelete, socialPath(externalID, "link"), nil, key, nil)
}

func (c *Client) CreateConnection(ctx context.Context, externalID string, in ConnectInput, key string) (*ConnectSession, error) {
	var out ConnectSession
	return ret(&out, c.send(ctx, http.MethodPost, socialPath(externalID, "connections"), in, key, &out))
}

func (c *Client) GetConnection(ctx context.Context, externalID, id string) (*ConnectSession, error) {
	var out ConnectSession
	return ret(&out, c.get(ctx, socialPath(externalID, "connections", id), nil, &out))
}

func (c *Client) SignIn(ctx context.Context, externalID, id string, in SignInInput, key string) (*ConnectSession, error) {
	var out ConnectSession
	return ret(&out, c.send(ctx, http.MethodPost, socialPath(externalID, "connections", id, "sign-in"), in, key, &out))
}

func (c *Client) AnswerCheckpoint(ctx context.Context, externalID, id string, in CheckpointAnswer, key string) (*ConnectSession, error) {
	var out ConnectSession
	return ret(&out, c.send(ctx, http.MethodPost, socialPath(externalID, "connections", id, "answer"), in, key, &out))
}

func (c *Client) ChooseMethod(ctx context.Context, externalID, id, challenge, key string) (*ConnectSession, error) {
	var out ConnectSession
	in := map[string]string{"challenge": challenge}
	return ret(&out, c.send(ctx, http.MethodPost, socialPath(externalID, "connections", id, "method"), in, key, &out))
}

func (c *Client) ResendCheckpoint(ctx context.Context, externalID, id, key string) (*ConnectSession, error) {
	var out ConnectSession
	return ret(&out, c.send(ctx, http.MethodPost, socialPath(externalID, "connections", id, "resend"), nil, key, &out))
}

func (c *Client) CancelConnection(ctx context.Context, externalID, id, key string) (*ConnectSession, error) {
	var out ConnectSession
	return ret(&out, c.send(ctx, http.MethodDelete, socialPath(externalID, "connections", id), nil, key, &out))
}

func (c *Client) ListSocialAccounts(ctx context.Context, externalID, status, cursor string, limit int) (*Page[SocialAccount], error) {
	q := pageQuery(cursor, limit)
	if status != "" {
		q.Set("status", status)
	}
	var out Page[SocialAccount]
	return ret(&out, c.get(ctx, socialPath(externalID, "accounts"), q, &out))
}

func (c *Client) GetSocialAccount(ctx context.Context, externalID, id string) (*SocialAccount, error) {
	var out SocialAccount
	return ret(&out, c.get(ctx, socialPath(externalID, "accounts", id), nil, &out))
}

func (c *Client) UpdateSocialAccount(ctx context.Context, externalID, id string, patch SocialAccountPatch, key string) (*SocialAccount, error) {
	var out SocialAccount
	return ret(&out, c.send(ctx, http.MethodPatch, socialPath(externalID, "accounts", id), patch, key, &out))
}

func (c *Client) accountVerb(ctx context.Context, externalID, id, verb, key string) (*SocialAccount, error) {
	var out SocialAccount
	return ret(&out, c.send(ctx, http.MethodPost, socialPath(externalID, "accounts", id, verb), nil, key, &out))
}

func (c *Client) PauseSocialAccount(ctx context.Context, externalID, id, key string) (*SocialAccount, error) {
	return c.accountVerb(ctx, externalID, id, "pause", key)
}

func (c *Client) ResumeSocialAccount(ctx context.Context, externalID, id, key string) (*SocialAccount, error) {
	return c.accountVerb(ctx, externalID, id, "resume", key)
}

func (c *Client) ResyncSocialAccount(ctx context.Context, externalID, id, key string) (*SocialAccount, error) {
	return c.accountVerb(ctx, externalID, id, "resync", key)
}

func (c *Client) RemoveSocialAccount(ctx context.Context, externalID, id, key string) (*SocialAccount, error) {
	var out SocialAccount
	return ret(&out, c.send(ctx, http.MethodDelete, socialPath(externalID, "accounts", id), nil, key, &out))
}

func (c *Client) SearchCatalog(ctx context.Context, externalID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.get(ctx, socialPath(externalID, "search", "catalog"), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SearchPeople(ctx context.Context, externalID string, in SearchInput, key string) (*SearchPage, error) {
	var out SearchPage
	return ret(&out, c.send(ctx, http.MethodPost, socialPath(externalID, "search"), in, key, &out))
}

func (c *Client) Typeahead(ctx context.Context, externalID, source, filter, q, accountID string) ([]TypeaheadItem, error) {
	v := url.Values{}
	v.Set("source", source)
	v.Set("filter", filter)
	v.Set("q", q)
	if accountID != "" {
		v.Set("account_id", accountID)
	}
	var out struct {
		Data []TypeaheadItem `json:"data"`
	}
	if err := c.get(ctx, socialPath(externalID, "search", "typeahead"), v, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *Client) ParseSearchURL(ctx context.Context, externalID, rawURL, key string) (json.RawMessage, error) {
	var out json.RawMessage
	in := map[string]string{"url": rawURL}
	if err := c.send(ctx, http.MethodPost, socialPath(externalID, "search", "parse-url"), in, key, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SearchLinkedInURL(ctx context.Context, externalID string, query json.RawMessage, key string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	in := map[string]json.RawMessage{"query": query}
	if err := c.send(ctx, http.MethodPost, socialPath(externalID, "search", "linkedin-url"), in, key, &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

func (c *Client) BuildQuery(ctx context.Context, externalID string, in AIInput, key string) (*AIResult, error) {
	var out AIResult
	return ret(&out, c.send(ctx, http.MethodPost, socialPath(externalID, "search", "ai"), in, key, &out))
}

func (c *Client) GetProfile(ctx context.Context, externalID, identifier, accountID string) (*Profile, error) {
	v := url.Values{}
	v.Set("recipient", identifier)
	if accountID != "" {
		v.Set("account_id", accountID)
	}
	var out Profile
	return ret(&out, c.get(ctx, socialPath(externalID, "profiles"), v, &out))
}

func (c *Client) CreateAction(ctx context.Context, externalID string, in ActionInput, key string) (*Action, error) {
	var out Action
	return ret(&out, c.send(ctx, http.MethodPost, socialPath(externalID, "actions"), in, key, &out))
}

func (c *Client) GetAction(ctx context.Context, externalID, id string) (*Action, error) {
	var out Action
	return ret(&out, c.get(ctx, socialPath(externalID, "actions", id), nil, &out))
}

func (c *Client) ListActions(ctx context.Context, externalID string, f ActionFilter) (*Page[Action], error) {
	q := pageQuery(f.Cursor, f.Limit)
	for k, v := range map[string]string{"account_id": f.AccountID, "status": f.Status, "reference": f.Reference} {
		if v != "" {
			q.Set(k, v)
		}
	}
	var out Page[Action]
	return ret(&out, c.get(ctx, socialPath(externalID, "actions"), q, &out))
}

func (c *Client) CancelAction(ctx context.Context, externalID, id, key string) (*Action, error) {
	var out Action
	return ret(&out, c.send(ctx, http.MethodDelete, socialPath(externalID, "actions", id), nil, key, &out))
}

func (c *Client) ListConversations(ctx context.Context, externalID string, f ConversationFilter) (*Page[Conversation], error) {
	q := pageQuery(f.Cursor, f.Limit)
	if f.AccountID != "" {
		q.Set("account_id", f.AccountID)
	}
	if f.Channel != "" {
		q.Set("channel", f.Channel)
	}
	if f.UpdatedAfter != nil {
		q.Set("updated_after", f.UpdatedAfter.UTC().Format(time.RFC3339))
	}
	var out Page[Conversation]
	return ret(&out, c.get(ctx, socialPath(externalID, "conversations"), q, &out))
}

func (c *Client) GetConversation(ctx context.Context, externalID, id string) (*Conversation, error) {
	var out Conversation
	return ret(&out, c.get(ctx, socialPath(externalID, "conversations", id), nil, &out))
}

func (c *Client) ListMessages(ctx context.Context, externalID, conversationID, cursor string, limit int) (*Page[SocialMessage], error) {
	var out Page[SocialMessage]
	return ret(&out, c.get(ctx, socialPath(externalID, "conversations", conversationID, "messages"), pageQuery(cursor, limit), &out))
}

func (c *Client) SendMessage(ctx context.Context, externalID, conversationID string, in MessageInput, key string) (*SocialMessage, error) {
	var out SocialMessage
	return ret(&out, c.send(ctx, http.MethodPost, socialPath(externalID, "conversations", conversationID, "messages"), in, key, &out))
}

func (c *Client) MarkConversationRead(ctx context.Context, externalID, conversationID, key string) (*Conversation, error) {
	var out Conversation
	return ret(&out, c.send(ctx, http.MethodPost, socialPath(externalID, "conversations", conversationID, "read"), nil, key, &out))
}

func (c *Client) UploadAudio(ctx context.Context, externalID string, in AudioInput, key string) (*Audio, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("duration_seconds", strconv.Itoa(in.DurationSeconds)); err != nil {
		return nil, &Error{Code: CodeInvalidRequest, cause: err}
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, in.Filename))
	h.Set("Content-Type", in.ContentType)
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, &Error{Code: CodeInvalidRequest, cause: err}
	}
	if _, err := part.Write(in.Data); err != nil {
		return nil, &Error{Code: CodeInvalidRequest, cause: err}
	}
	if err := w.Close(); err != nil {
		return nil, &Error{Code: CodeInvalidRequest, cause: err}
	}
	var out Audio
	r := request{method: http.MethodPost, path: socialPath(externalID, "audio"), body: buf.Bytes(), contentType: w.FormDataContentType(), key: key}
	return ret(&out, c.do(ctx, r, &out))
}

func (c *Client) DeleteAudio(ctx context.Context, externalID, id, key string) error {
	return c.send(ctx, http.MethodDelete, socialPath(externalID, "audio", id), nil, key, nil)
}
