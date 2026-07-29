package msgraph

import (
	"context"
	"net/url"
	"strings"
)

// Warmup mailbox actions. These mirror the goog/imap warmup actions so the
// recipient side generates the same natural-looking engagement signals.
//
// Note: Graph's move is a copy+delete that returns a NEW message id in the
// destination folder (the old id shows up @removed in the next delta). The
// warmup engagement flow is fire-and-forget per message, so we don't thread the
// new id back here; the messageId map self-heals on the next sync.

// MarkAsRead sets isRead on the message.
func (c *Client) MarkAsRead(ctx context.Context, messageID string) error {
	return c.doJSON(ctx, "PATCH", c.messageURL(messageID), map[string]any{"isRead": true}, nil)
}

// MarkImportant raises the message importance to high, the closest Outlook
// equivalent of Gmail's IMPORTANT label.
func (c *Client) MarkImportant(ctx context.Context, messageID string) error {
	return c.doJSON(ctx, "PATCH", c.messageURL(messageID), map[string]any{"importance": "high"}, nil)
}

// AddFlag sets the follow-up flag on the message, a deliberate positive signal
// analogous to a Gmail star.
func (c *Client) AddFlag(ctx context.Context, messageID string) error {
	body := map[string]any{"flag": map[string]any{"flagStatus": "flagged"}}
	return c.doJSON(ctx, "PATCH", c.messageURL(messageID), body, nil)
}

// RemoveFromSpam rescues a message out of Junk Email back into the Inbox.
// Graph has no stable v1.0 "not junk" action (markAsNotJunk is retired), so the
// move is the reliable primitive. Returns the message's new id (move is a
// copy+delete, so the id changes).
func (c *Client) RemoveFromSpam(ctx context.Context, messageID string) (string, error) {
	return c.move(ctx, messageID, FolderInbox)
}

// MoveToFolder moves the message into a named folder, creating it if needed.
// Used for the "Warmbly" sorting folder. Returns the message's new id.
func (c *Client) MoveToFolder(ctx context.Context, messageID, folderName string) (string, error) {
	folderID, err := c.ensureFolder(ctx, folderName)
	if err != nil {
		return "", err
	}
	return c.move(ctx, messageID, folderID)
}

// move relocates a message and returns the new id from the destination folder
// (Graph move is copy+delete; the source id is invalidated).
func (c *Client) move(ctx context.Context, messageID, destinationID string) (string, error) {
	body := map[string]any{"destinationId": destinationID}
	var moved struct {
		ID string `json:"id"`
	}
	if err := c.doJSON(ctx, "POST", c.messageURL(messageID)+"/move", body, &moved); err != nil {
		return "", err
	}
	return moved.ID, nil
}

// ResolveMessageID returns the current Graph message id for the message with the
// given immutable RFC 5322 internetMessageId, searching across folders. Graph
// ids change on move, so warmup actions re-resolve against this stable key.
// Returns an empty string (no error) when the message can't be found.
func (c *Client) ResolveMessageID(ctx context.Context, internetMessageID string) (string, error) {
	filter := "internetMessageId eq '" + strings.ReplaceAll(internetMessageID, "'", "''") + "'"
	u := c.mailboxBase() + "/messages?$select=id&$top=1&$filter=" + url.QueryEscape(filter)
	var resp struct {
		Value []struct {
			ID string `json:"id"`
		} `json:"value"`
	}
	if err := c.doJSON(ctx, "GET", u, nil, &resp); err != nil {
		return "", err
	}
	if len(resp.Value) == 0 {
		return "", nil
	}
	return resp.Value[0].ID, nil
}

func (c *Client) messageURL(messageID string) string {
	return c.mailboxBase() + "/messages/" + url.PathEscape(messageID)
}

// ensureFolder resolves a top-level mail folder id by display name, creating the
// folder on first use. The id is cached for the mailbox's lifetime.
func (c *Client) ensureFolder(ctx context.Context, name string) (string, error) {
	c.mu.Lock()
	if id, ok := c.folderIDs[name]; ok {
		c.mu.Unlock()
		return id, nil
	}
	c.mu.Unlock()

	// Look for an existing folder with this display name.
	listURL := c.mailboxBase() + "/mailFolders?$select=id,displayName&$top=100"
	var list struct {
		Value []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"value"`
	}
	if err := c.doJSON(ctx, "GET", listURL, nil, &list); err != nil {
		return "", err
	}
	for _, f := range list.Value {
		if f.DisplayName == name {
			c.cacheFolder(name, f.ID)
			return f.ID, nil
		}
	}

	// Create it.
	var created struct {
		ID string `json:"id"`
	}
	if err := c.doJSON(ctx, "POST", c.mailboxBase()+"/mailFolders", map[string]any{"displayName": name}, &created); err != nil {
		return "", err
	}
	c.cacheFolder(name, created.ID)
	return created.ID, nil
}

func (c *Client) cacheFolder(name, id string) {
	c.mu.Lock()
	c.folderIDs[name] = id
	c.mu.Unlock()
}
