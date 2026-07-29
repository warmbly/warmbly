package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// mailpitClient is a minimal client for the mailpit HTTP API; the simulator
// uses the Read flag as its processing cursor.
type mailpitClient struct {
	base string
	http *http.Client
}

func newMailpitClient(base string) *mailpitClient {
	return &mailpitClient{base: base, http: &http.Client{Timeout: 15 * time.Second}}
}

type mailpitAddress struct {
	Name    string `json:"Name"`
	Address string `json:"Address"`
}

type mailpitSummary struct {
	ID      string           `json:"ID"`
	Read    bool             `json:"Read"`
	From    mailpitAddress   `json:"From"`
	To      []mailpitAddress `json:"To"`
	Subject string           `json:"Subject"`
}

type mailpitListResponse struct {
	Messages []mailpitSummary `json:"messages"`
}

type mailpitMessage struct {
	ID        string           `json:"ID"`
	MessageID string           `json:"MessageID"`
	From      mailpitAddress   `json:"From"`
	To        []mailpitAddress `json:"To"`
	Subject   string           `json:"Subject"`
	HTML      string           `json:"HTML"`
	Text      string           `json:"Text"`
}

// listUnread returns the newest messages that have not been marked read.
func (c *mailpitClient) listUnread(ctx context.Context, limit int) ([]mailpitSummary, error) {
	var out mailpitListResponse
	if err := c.get(ctx, fmt.Sprintf("/api/v1/messages?limit=%d", limit), &out); err != nil {
		return nil, err
	}
	unread := out.Messages[:0]
	for _, m := range out.Messages {
		if !m.Read {
			unread = append(unread, m)
		}
	}
	return unread, nil
}

func (c *mailpitClient) message(ctx context.Context, id string) (*mailpitMessage, error) {
	var out mailpitMessage
	if err := c.get(ctx, "/api/v1/message/"+id, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// raw returns the full RFC822 source of a captured message.
func (c *mailpitClient) raw(ctx context.Context, id string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/message/"+id+"/raw", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mailpit raw %s: status %d", id, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// markRead flips the Read flag; this is the simulator's "processed" cursor.
func (c *mailpitClient) markRead(ctx context.Context, ids []string) error {
	body, err := json.Marshal(map[string]any{"IDs": ids, "Read": true})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.base+"/api/v1/messages", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mailpit mark read: status %d", resp.StatusCode)
	}
	return nil
}

func (c *mailpitClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mailpit GET %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
