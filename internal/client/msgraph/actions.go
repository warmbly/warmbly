package msgraph

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/warmbly/warmbly/internal/errx"
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
// copy+delete, so the id changes), or "" when the message was not in Junk and
// nothing was moved.
//
// The Junk check is the point: engagementPlan runs move_to_warmbly first, so by
// the time this runs the message is usually in the untracked Warmbly folder.
// Moving it unconditionally would undo that foldering and drop it back into the
// tracked Inbox under a new id, where live sync reads it as new mail. The IMAP
// path guards the same way with IsSpamMailbox.
func (c *Client) RemoveFromSpam(ctx context.Context, messageID string) (string, error) {
	junkID, err := c.wellKnownFolderID(ctx, FolderJunk)
	if err != nil {
		return "", err
	}
	parentID, err := c.messageParentFolder(ctx, messageID)
	if err != nil {
		return "", err
	}
	if parentID != junkID {
		return "", nil
	}
	return c.move(ctx, messageID, FolderInbox)
}

// wellKnownFolderID resolves a well-known folder name to the opaque id Graph
// reports as a message's parentFolderId. The two are not interchangeable: the
// name is accepted in a path, but never returned. Cached for the mailbox's life.
func (c *Client) wellKnownFolderID(ctx context.Context, name string) (string, error) {
	c.mu.Lock()
	if id, ok := c.folderIDs[name]; ok {
		c.mu.Unlock()
		return id, nil
	}
	c.mu.Unlock()

	var folder struct {
		ID string `json:"id"`
	}
	if err := c.doJSON(ctx, "GET", c.root()+"/mailFolders/"+url.PathEscape(name)+"?$select=id", nil, &folder); err != nil {
		return "", err
	}
	c.cacheFolder(name, folder.ID)
	return folder.ID, nil
}

// messageParentFolder returns the id of the folder a message currently sits in.
func (c *Client) messageParentFolder(ctx context.Context, messageID string) (string, error) {
	var msg struct {
		ParentFolderID string `json:"parentFolderId"`
	}
	if err := c.doJSON(ctx, "GET", c.messageURL(messageID)+"?$select=parentFolderId", nil, &msg); err != nil {
		return "", err
	}
	return msg.ParentFolderID, nil
}

func (c *Client) IsMessageInFolder(ctx context.Context, messageID, folder string) (bool, error) {
	folderID, err := c.wellKnownFolderID(ctx, folder)
	if err != nil {
		return false, err
	}
	parentID, err := c.messageParentFolder(ctx, messageID)
	return parentID == folderID, err
}

// MoveToFolder moves the message into a named folder, creating it if needed.
// Used for the warmup sorting folder. Returns the message's new id.
func (c *Client) MoveToFolder(ctx context.Context, messageID, folderName string) (string, error) {
	folderID, err := c.ensureFolder(ctx, folderName)
	if err != nil {
		return "", err
	}
	parentID, err := c.messageParentFolder(ctx, messageID)
	if err != nil {
		return "", err
	}
	if parentID == folderID {
		return messageID, nil
	}
	return c.move(ctx, messageID, folderID)
}

// MoveToArchive moves the message into the mailbox's Archive, the destination
// for the warmup placement that wants the mail out of sight without a folder of
// its own. Returns the message's new id.
func (c *Client) MoveToArchive(ctx context.Context, messageID string) (string, error) {
	folderID, err := c.wellKnownFolderID(ctx, FolderArchive)
	if err != nil {
		return "", err
	}
	parentID, err := c.messageParentFolder(ctx, messageID)
	if err != nil {
		return "", err
	}
	if parentID == folderID {
		return messageID, nil
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
// Only an explicit terminal empty collection establishes absence.
func (c *Client) ResolveMessageID(ctx context.Context, internetMessageID string) (string, error) {
	if strings.TrimSpace(internetMessageID) == "" {
		return "", errMessageLookupIncomplete
	}
	u := c.messagesByInternetID(internetMessageID, "id", 1)
	var resp struct {
		Value    []GraphMessage  `json:"value"`
		NextLink json.RawMessage `json:"@odata.nextLink"`
	}
	if err := c.doJSON(ctx, "GET", u, nil, &resp); err != nil {
		if errors.Is(err, errGraphTrailingJSON) {
			return "", errMessageLookupIncomplete
		}
		return "", err
	}
	// Never follow a continuation or infer absence from an incomplete page.
	if resp.Value == nil || resp.NextLink != nil || len(resp.Value) > 1 {
		return "", errMessageLookupIncomplete
	}
	if len(resp.Value) == 0 {
		return "", nil
	}
	id := resp.Value[0].ID
	if id == "" || strings.TrimSpace(id) != id {
		return "", errMessageLookupIncomplete
	}
	return id, nil
}

var errMessageLookupIncomplete = errx.MError(errx.MailErrorWarning, errx.MailErrorCodeServerUnreachable, "The message lookup could not establish a complete identity or confirmed absence. Retry later.", errx.MailErrorResolveMethodRetry)

// LocateRFCMessageID reports whether any folder still holds the message, and
// whether every copy is in Deleted Items. Delta reports a move as a removal.
func (c *Client) LocateRFCMessageID(ctx context.Context, internetMessageID string) (found, trashed bool, err error) {
	id := strings.TrimSpace(internetMessageID)
	if id == "" {
		return false, false, errors.New("msgraph: no message id to look up")
	}
	deletedID, err := c.wellKnownFolderID(ctx, FolderDeletedItems)
	if err != nil {
		return false, false, err
	}
	// Graph stores the id bracketed; a bare one is tried both ways.
	forms := []string{id}
	if !strings.HasPrefix(id, "<") {
		forms = append(forms, "<"+id+">")
	}
	for _, form := range forms {
		u := c.messagesByInternetID(form, "id,parentFolderId", 10)
		var resp struct {
			Value []struct {
				ParentFolderID string `json:"parentFolderId"`
			} `json:"value"`
		}
		if err := c.doJSON(ctx, "GET", u, nil, &resp); err != nil {
			return false, false, err
		}
		for _, m := range resp.Value {
			found = true
			if m.ParentFolderID != deletedID {
				return true, false, nil
			}
		}
		if found {
			return true, true, nil
		}
	}
	return false, false, nil
}

// messagesByInternetID lists the mailbox's messages carrying one internetMessageId.
func (c *Client) messagesByInternetID(internetMessageID, fields string, top int) string {
	filter := "internetMessageId eq '" + strings.ReplaceAll(internetMessageID, "'", "''") + "'"
	return c.root() + "/messages?$select=" + fields + "&$top=" + itoa(top) + "&$filter=" + url.QueryEscape(filter)
}

func (c *Client) messageURL(messageID string) string {
	return c.root() + "/messages/" + url.PathEscape(messageID)
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
	listURL := c.root() + "/mailFolders?$select=id,displayName&$top=100"
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
	if err := c.doJSON(ctx, "POST", c.root()+"/mailFolders", map[string]any{"displayName": name}, &created); err != nil {
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

// SetSeen flips the read state of one message. Graph has no batch equivalent
// of Gmail's batchModify that is worth the complexity here, so the caller
// loops.
func (c *Client) SetSeen(ctx context.Context, messageID string, seen bool) error {
	return c.doJSON(ctx, "PATCH", c.messageURL(messageID), map[string]any{"isRead": seen}, nil)
}

// Delete removes a message the way Outlook's Delete key does: into Deleted
// Items, where the mailbox's own retention policy takes it from. Exchange
// answers a message that is already gone with 404, which is the state being
// asked for, so that is not an error here.
func (c *Client) Delete(ctx context.Context, messageID string) error {
	resp, err := c.do(ctx, http.MethodDelete, c.messageURL(messageID), "", nil)
	if err != nil {
		return transportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return HandleError(resp)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
