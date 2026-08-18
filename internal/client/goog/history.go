package goog

import (
	"context"
	"errors"

	"github.com/warmbly/warmbly/internal/models"
	"google.golang.org/api/googleapi"
)

// ErrStop is returned by a callback to end the walk now; FetchHistory then
// returns the checkpoint reached so far and a nil error.
var ErrStop = errors.New("goog: stop")

// historyPagesPerPass bounds one walk. A mailbox that has fallen far behind
// (or is held by fair use, so the checkpoint cannot advance) is caught up
// over several ticks instead of one unbounded pass.
const historyPagesPerPass = 10

// FetchHistory walks Gmail's history from lastHistoryID and returns the new
// checkpoint. A zero lastHistoryID means the mailbox has no baseline yet, which
// history.list cannot serve: startHistoryId=0 is rejected with "Requested
// entity was not found". Read the mailbox's current historyId instead and
// return it as the baseline; the backfill imports what came before.
//
// The checkpoint only moves past a history record once everything in it was
// stored. OnMessageAdded reports false for a message it left on the server
// (fair use), which pins the checkpoint before that record while the walk
// continues, so later records that are admitted (a reply, say) still land and
// the deferred ones are re-offered next tick.
func (c *Client) FetchHistory(ctx context.Context, lastHistoryID uint64) (uint64, error) {
	if lastHistoryID == 0 {
		profile, err := c.srv.Users.GetProfile("me").Context(ctx).Do()
		if err != nil {
			return 0, HandleError(err)
		}
		return profile.HistoryId, nil
	}

	call := c.srv.Users.History.List("me").MaxResults(500).StartHistoryId(lastHistoryID) // It does not include the record that has that exact HistoryID

	checkpoint := lastHistoryID
	pinned := false

	for page := 0; page < historyPagesPerPass; page++ {
		resp, err := call.Context(ctx).Do()
		if err != nil {
			return checkpoint, HandleError(err)
		}

		for _, h := range resp.History {
			complete := true
			for _, m := range h.MessagesAdded {
				if m.Message == nil {
					continue
				}
				added, err := c.OnMessageAdded(ctx, m.Message.Id, m.Message.ThreadId)
				if errors.Is(err, ErrStop) {
					return checkpoint, nil
				}
				if err != nil {
					return checkpoint, err
				}
				if !added {
					complete = false
				}
			}
			for _, m := range h.MessagesDeleted {
				if err := c.OnMessageRemove(ctx, m.Message.Id); err != nil {
					return checkpoint, err
				}
			}
			for _, m := range h.LabelsAdded {
				if err := c.OnLabelAdd(ctx, m.Message.Id, m.LabelIds); err != nil {
					return checkpoint, err
				}
			}
			for _, m := range h.LabelsRemoved {
				if err := c.OnLabelRemove(ctx, m.Message.Id, m.LabelIds); err != nil {
					return checkpoint, err
				}
			}
			if !complete {
				pinned = true
			}
			if !pinned {
				checkpoint = h.Id
			}
		}
		if resp.NextPageToken == "" {
			if !pinned {
				checkpoint = resp.HistoryId
			}
			break
		}
		call.PageToken(resp.NextPageToken)
	}

	return checkpoint, nil
}

// GetMessage hydrates one message in full. Returns nil, nil when Gmail no
// longer has it (deleted between the history event and now).
func (c *Client) GetMessage(ctx context.Context, id string) (*models.EmailMessageData, error) {
	full, err := c.srv.Users.Messages.Get("me", id).Format("full").Context(ctx).Do()
	if err != nil {
		var gerr *googleapi.Error
		if errors.As(err, &gerr) && gerr.Code == 404 {
			return nil, nil
		}
		return nil, HandleError(err)
	}
	return GmailMessageToEmailData(full), nil
}

// ListMessages is one page of the backfill: message ids matching q, newest
// first, and the token for the next page ("" when the query is exhausted).
func (c *Client) ListMessages(ctx context.Context, q, pageToken string, max int64) ([]string, string, error) {
	call := c.srv.Users.Messages.List("me").Q(q).MaxResults(max).Context(ctx)
	if pageToken != "" {
		call = call.PageToken(pageToken)
	}
	resp, err := call.Do()
	if err != nil {
		return nil, "", HandleError(err)
	}
	ids := make([]string, 0, len(resp.Messages))
	for _, m := range resp.Messages {
		if m != nil && m.Id != "" {
			ids = append(ids, m.Id)
		}
	}
	return ids, resp.NextPageToken, nil
}
