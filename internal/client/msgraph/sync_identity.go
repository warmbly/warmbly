package msgraph

import (
	"context"
	"errors"
	"net/http"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

var ErrIncompleteIDConversion = errors.New("graph: incomplete immutable id conversion")

func (c *Client) ImmutableIDMode() bool {
	c.idMu.RLock()
	defer c.idMu.RUnlock()
	return c.ImmutableIDs
}

func (c *Client) SetImmutableIDMode(enabled bool) {
	c.idMu.Lock()
	defer c.idMu.Unlock()
	c.ImmutableIDs = enabled
}

// ImmutableMessageIDs converts saved regular ids without changing mailbox contents.
func (c *Client) ImmutableMessageIDs(ctx context.Context, ids []string) (map[string]string, error) {
	return c.translateIDs(ctx, ids, "restId", "restImmutableEntryId")
}

func (c *Client) RegularMessageIDs(ctx context.Context, ids []string) (map[string]string, error) {
	return c.translateIDs(ctx, ids, "restImmutableEntryId", "restId")
}

func (c *Client) translateIDs(ctx context.Context, ids []string, source, target string) (map[string]string, error) {
	var out struct {
		Value []struct {
			SourceID string `json:"sourceId"`
			TargetID string `json:"targetId"`
		} `json:"value"`
	}
	if err := c.doJSON(ctx, http.MethodPost, c.root()+"/translateExchangeIds", map[string]any{
		"inputIds": ids, "sourceIdType": source, "targetIdType": target,
	}, &out); err != nil {
		return nil, err
	}
	converted := make(map[string]string, len(out.Value))
	for _, id := range out.Value {
		if id.SourceID != "" && id.TargetID != "" {
			converted[id.SourceID] = id.TargetID
		}
	}
	for _, id := range ids {
		if converted[id] == "" {
			return converted, ErrIncompleteIDConversion
		}
	}
	return converted, nil
}

// MessageFolder resolves current placement, not the source of a stale delta item.
func (c *Client) MessageFolder(ctx context.Context, msg *GraphMessage, fallback string) (string, error) {
	if msg.ParentFolderID == "" {
		return msg.ToEmailData(fallback).Folder, nil
	}
	for _, folder := range append(append([]string{}, TrackedFolders...), FolderDeletedItems) {
		id, err := c.wellKnownFolderID(ctx, folder)
		if err != nil {
			if merr, ok := err.(*errx.MailError); ok && folder == FolderArchive && merr.Code == errx.MailErrorCodeNotFound {
				continue
			}
			return "", err
		}
		if msg.ParentFolderID == id {
			if folder == FolderDeletedItems {
				return models.FolderTrash, nil
			}
			return msg.ToEmailData(folder).Folder, nil
		}
	}
	return "", nil
}
