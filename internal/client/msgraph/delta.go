package msgraph

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

func itoa(n int) string { return strconv.Itoa(n) }

// ErrStop is returned by a callback to end the walk now; the cursor stays
// where it was last persisted.
var ErrStop = errors.New("msgraph: stop")

// deltaSelect keeps delta pages light: we only need the id, read state, and the
// @removed marker to decide add/update vs remove. Full envelope + body + headers
// are hydrated per admitted message via FetchMessage.
const deltaSelect = "id,isRead"

// msgSelect is the property set fetched for each new message: enough to build a
// complete EmailMessageData (envelope, threading, body) plus internetMessageHeaders
// so the warmup verification token survives into sync.
const msgSelect = "id,internetMessageId,conversationId,subject,bodyPreview,isRead," +
	"receivedDateTime,from,sender,toRecipients,ccRecipients,bccRecipients,replyTo,body,internetMessageHeaders"

// deltaPagesPerPass bounds one folder's walk per tick, like Gmail's
// historyPagesPerPass: a mailbox far behind, or held by fair use so its
// cursor cannot advance, is caught up over several ticks.
const deltaPagesPerPass = 20

// deltaPage is one page of a messages/delta response.
type deltaPage struct {
	Value     []GraphMessage `json:"value"`
	NextLink  string         `json:"@odata.nextLink"`
	DeltaLink string         `json:"@odata.deltaLink"`
}

// listPage is one page of a folder listing (the backfill).
type listPage struct {
	Value    []GraphMessage `json:"value"`
	NextLink string         `json:"@odata.nextLink"`
}

// TrackedFolders are the well-known folders live sync follows: inbox and junk
// for placement, sent so a conversation shows both sides (as the Gmail and
// IMAP paths already do), and drafts so the Drafts scope is populated on
// Outlook the way it is on the other two providers.
var TrackedFolders = []string{FolderInbox, FolderJunk, FolderSent, FolderDrafts}

// BackfillFolders are the folders the initial import walks. Junk is followed
// live for placement signals but its history is not worth importing, and would
// consume the message budget that belongs to real conversations.
var BackfillFolders = []string{FolderInbox, FolderSent, FolderArchive, FolderDrafts}

// Sync walks the delta stream for the tracked folders and drives the
// OnMessage* callbacks. It is the Graph equivalent of goog.FetchHistory and
// fits the disposable worker natively: no webhook endpoint, no subscription
// lifecycle, just a cursor the control plane persists via OnDelta.
func (c *Client) Sync(ctx context.Context) error {
	for _, folder := range TrackedFolders {
		if err := c.syncFolder(ctx, folder); err != nil {
			if errors.Is(err, ErrStop) {
				return nil
			}
			return err
		}
	}
	return nil
}

func (c *Client) syncFolder(ctx context.Context, folder string) error {
	next := c.DeltaLinks[folder]

	// First run for this folder: there is no cursor yet. We page to the end
	// purely to capture a deltaLink representing "now" and do NOT import the
	// mailbox's existing history; the backfill does that under its budget.
	// The walk is uncapped and nothing is persisted before the deltaLink: a
	// saved intermediate nextLink would make the next pass treat the rest of
	// the priming walk as live mail.
	if next == "" {
		next = graphBase + "/me/mailFolders/" + folder + "/messages/delta?$select=" + url.QueryEscape(deltaSelect)
		for {
			var pg deltaPage
			if err := c.doJSON(ctx, "GET", next, nil, &pg); err != nil {
				return err
			}
			switch {
			case pg.NextLink != "":
				next = pg.NextLink
			case pg.DeltaLink != "":
				return c.saveCursor(ctx, folder, pg.DeltaLink)
			default:
				return nil
			}
		}
	}

	// The persisted cursor only moves past a page once everything on it was
	// stored. A page with a deferred message (fair use) pins the cursor there
	// while the walk continues, so admitted messages further on still land
	// and the deferred ones are re-offered next tick from the pinned link.
	pinned := false
	for page := 0; page < deltaPagesPerPass; page++ {
		var pg deltaPage
		if err := c.doJSON(ctx, "GET", next, nil, &pg); err != nil {
			return err
		}

		complete := true
		for i := range pg.Value {
			stored, err := c.applyDelta(ctx, folder, &pg.Value[i])
			if err != nil {
				return err
			}
			if !stored {
				complete = false
			}
		}
		if !complete {
			pinned = true
		}

		switch {
		case pg.NextLink != "":
			if !pinned {
				if err := c.saveCursor(ctx, folder, pg.NextLink); err != nil {
					return err
				}
			}
			next = pg.NextLink
		case pg.DeltaLink != "":
			if !pinned {
				return c.saveCursor(ctx, folder, pg.DeltaLink)
			}
			return nil
		default:
			return nil
		}
	}
	return nil
}

func (c *Client) saveCursor(ctx context.Context, folder, link string) error {
	c.DeltaLinks[folder] = link
	if c.OnDelta != nil {
		return c.OnDelta(ctx, folder, link)
	}
	return nil
}

// applyDelta turns one delta item into the right callback. Removed items
// (delete or move-out) fire OnMessageRemove; live items are offered through
// OnMessageSeen, which dedupes, hydrates and stores as budget allows and
// reports false when the message was left on the server.
func (c *Client) applyDelta(ctx context.Context, folder string, item *GraphMessage) (bool, error) {
	if item.Removed != nil {
		if c.OnMessageRemove != nil {
			return true, c.OnMessageRemove(ctx, item.ID)
		}
		return true, nil
	}
	if c.OnMessageSeen == nil {
		return true, nil
	}
	return c.OnMessageSeen(ctx, folder, item.ID, item.IsRead)
}

// FetchMessage hydrates a single message with the full property set. It
// returns nil, nil when Graph no longer has the message (deleted between the
// delta item and now), which is a skip for the caller, not a mailbox error.
func (c *Client) FetchMessage(ctx context.Context, folder, id string) (*GraphMessage, error) {
	u := graphBase + "/me/messages/" + url.PathEscape(id) + "?$select=" + url.QueryEscape(msgSelect)
	resp, err := c.do(ctx, "GET", u, "", nil)
	if err != nil {
		return nil, transportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, HandleError(resp)
	}
	var msg GraphMessage
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// ListMessagesSince is one page of the backfill for a folder: full messages
// received on or after since, newest first, plus the link to the next page
// ("" when exhausted). next is "" for the first page.
func (c *Client) ListMessagesSince(ctx context.Context, folder string, since time.Time, next string, top int) ([]*GraphMessage, string, error) {
	if next == "" {
		q := url.Values{}
		q.Set("$filter", "receivedDateTime ge "+since.UTC().Format("2006-01-02T15:04:05Z"))
		q.Set("$orderby", "receivedDateTime desc")
		q.Set("$top", itoa(top))
		q.Set("$select", msgSelect)
		next = graphBase + "/me/mailFolders/" + folder + "/messages?" + q.Encode()
	}
	var pg listPage
	if err := c.doJSON(ctx, "GET", next, nil, &pg); err != nil {
		return nil, "", err
	}
	out := make([]*GraphMessage, 0, len(pg.Value))
	for i := range pg.Value {
		out = append(out, &pg.Value[i])
	}
	return out, pg.NextLink, nil
}

// ToEmailData maps a hydrated message; folder sets the canonical placement
// and adds the junk flag warmup placement detection reads.
func (m *GraphMessage) ToEmailData(folder string) *models.EmailMessageData {
	data := m.toEmailData()
	switch folder {
	case FolderJunk:
		data.Flags = append(data.Flags, "\\Junk")
		data.Folder = models.FolderSpam
	case FolderSent:
		data.Folder = models.FolderSent
	case FolderArchive:
		data.Folder = models.FolderArchive
	case FolderDrafts:
		data.Folder = models.FolderDrafts
	case FolderInbox:
		data.Folder = models.FolderInbox
	}
	return data
}
