package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// restrictedMailboxes is the caller's mailbox allowlist; nil when the caller
// is not limited to specific mailboxes.
func restrictedMailboxes(c *gin.Context) []uuid.UUID {
	return middleware.AllowedEmailAccounts(c)
}

// allowedMailboxFilter narrows a mailbox filter to the caller's allowlist: no
// filter becomes the allowlist, and a named mailbox outside it is refused.
func allowedMailboxFilter(c *gin.Context, ids []uuid.UUID) ([]uuid.UUID, *errx.Error) {
	allowed := restrictedMailboxes(c)
	if len(allowed) == 0 {
		return ids, nil
	}
	if len(ids) == 0 {
		return allowed, nil
	}
	for _, id := range ids {
		if xerr := mailboxAllowed(c, id); xerr != nil {
			return nil, xerr
		}
	}
	return ids, nil
}

// uniboxMessagesAllowed refuses when a named message, or any message in a
// named conversation, sits in a mailbox outside the caller's allowlist.
func (h *Handler) uniboxMessagesAllowed(c *gin.Context, orgID uuid.UUID, ids []uuid.UUID, threadIDs []string) *errx.Error {
	if len(restrictedMailboxes(c)) == 0 || (len(ids) == 0 && len(threadIDs) == 0) {
		return nil
	}
	boxes, xerr := h.UniboxService.MessageMailboxes(c.Request.Context(), orgID, ids, threadIDs)
	if xerr != nil {
		return xerr
	}
	for _, id := range boxes {
		if xerr := mailboxAllowed(c, id); xerr != nil {
			return xerr
		}
	}
	return nil
}

// keepAllowedMessages drops the messages in mailboxes outside the caller's allowlist.
func keepAllowedMessages(c *gin.Context, res *models.MailSearchResult) {
	if res == nil || len(restrictedMailboxes(c)) == 0 {
		return
	}
	kept := res.Data[:0]
	for _, m := range res.Data {
		if middleware.EmailAccountAllowed(c, m.EmailID) {
			// Which other mailbox a message answers is not shown outside the allowlist.
			if m.AnswersMailboxID != nil && !middleware.EmailAccountAllowed(c, *m.AnswersMailboxID) {
				m.AnswersMailboxID = nil
			}
			kept = append(kept, m)
		}
	}
	res.Data = kept
}
