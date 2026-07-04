package msgraph

import (
	"context"
	"net/url"
)

// deltaSelect keeps delta pages light: we only need the id, read state, and the
// @removed marker to decide add/update vs remove. Full envelope + body + headers
// are hydrated per added message via fetchMessage.
const deltaSelect = "id,isRead"

// msgSelect is the property set fetched for each new message: enough to build a
// complete EmailMessageData (envelope, threading, body) plus internetMessageHeaders
// so the warmup verification token survives into sync.
const msgSelect = "id,internetMessageId,conversationId,subject,bodyPreview,isRead," +
	"receivedDateTime,from,sender,toRecipients,ccRecipients,bccRecipients,replyTo,body,internetMessageHeaders"

// deltaPage is one page of a messages/delta response.
type deltaPage struct {
	Value     []graphMessage `json:"value"`
	NextLink  string         `json:"@odata.nextLink"`
	DeltaLink string         `json:"@odata.deltaLink"`
}

// Sync walks the delta stream for the tracked folders (inbox + junk) and drives
// the OnMessage* callbacks. It is the Graph equivalent of goog.FetchHistory and
// fits the disposable worker natively: no webhook endpoint, no subscription
// lifecycle, just a cursor the control plane persists via OnDelta.
func (c *Client) Sync(ctx context.Context) error {
	for _, folder := range []string{FolderInbox, FolderJunk} {
		if err := c.syncFolder(ctx, folder); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) syncFolder(ctx context.Context, folder string) error {
	next := c.DeltaLinks[folder]

	// First run for this folder: there is no cursor yet. We page to the end
	// purely to capture a deltaLink representing "now" and do NOT import the
	// mailbox's existing history, mirroring how the Gmail path starts from the
	// history id captured at connect time rather than backfilling everything.
	priming := next == ""
	if priming {
		next = graphBase + "/me/mailFolders/" + folder + "/messages/delta?$select=" + url.QueryEscape(deltaSelect)
	}

	for {
		var page deltaPage
		if err := c.doJSON(ctx, "GET", next, nil, &page); err != nil {
			return err
		}

		if !priming {
			for i := range page.Value {
				if err := c.applyDelta(ctx, folder, &page.Value[i]); err != nil {
					return err
				}
			}
		}

		switch {
		case page.NextLink != "":
			next = page.NextLink
		case page.DeltaLink != "":
			c.DeltaLinks[folder] = page.DeltaLink
			if c.OnDelta != nil {
				if err := c.OnDelta(ctx, folder, page.DeltaLink); err != nil {
					return err
				}
			}
			return nil
		default:
			return nil
		}
	}
}

// applyDelta turns one delta item into the right callback. Removed items (delete
// or move-out) fire OnMessageRemove; live items are hydrated and fire
// OnMessageAdd (the wmail layer dedupes by Message-ID) plus OnFlagsChange to keep
// read state fresh for messages that already exist.
func (c *Client) applyDelta(ctx context.Context, folder string, item *graphMessage) error {
	if item.Removed != nil {
		if c.OnMessageRemove != nil {
			return c.OnMessageRemove(ctx, item.ID)
		}
		return nil
	}

	full, err := c.fetchMessage(ctx, item.ID)
	if err != nil {
		return err
	}
	if full == nil {
		return nil
	}

	if c.OnMessageAdd != nil {
		data := full.toEmailData()
		// Junk-folder arrivals carry a spam flag so warmup spam-placement
		// detection and placement tests see where the message landed.
		if folder == FolderJunk {
			data.Flags = append(data.Flags, "\\Junk")
		}
		if err := c.OnMessageAdd(ctx, data); err != nil {
			return err
		}
	}
	if c.OnFlagsChange != nil {
		if err := c.OnFlagsChange(ctx, item.ID, full.IsRead); err != nil {
			return err
		}
	}
	return nil
}

// fetchMessage hydrates a single message with the full property set.
func (c *Client) fetchMessage(ctx context.Context, id string) (*graphMessage, error) {
	u := graphBase + "/me/messages/" + url.PathEscape(id) + "?$select=" + url.QueryEscape(msgSelect)
	var msg graphMessage
	if err := c.doJSON(ctx, "GET", u, nil, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
