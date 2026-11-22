package handler

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/partner"
	"github.com/warmbly/warmbly/internal/app/store"
	"github.com/warmbly/warmbly/internal/errx"
)

// mailboxLock is why members may not remove or reconnect a mailbox: a partner
// attached it, or the workspace bought it and still pays for it. Nil when neither.
func (h *Handler) mailboxLock(ctx context.Context, orgID, accountID uuid.UUID) *errx.Error {
	return MailboxLock(h.Partners, h.StoreService)(ctx, orgID, accountID)
}

// MailboxLock is the one remove-or-reconnect check, shared by the HTTP routes and the AI tools.
func MailboxLock(partners *partner.Service, st *store.Service) func(ctx context.Context, orgID, accountID uuid.UUID) *errx.Error {
	return func(ctx context.Context, orgID, accountID uuid.UUID) *errx.Error {
		return lockOf(ctx, partners, st, orgID, accountID)
	}
}

func lockOf(ctx context.Context, partners *partner.Service, st *store.Service, orgID, accountID uuid.UUID) *errx.Error {
	name, err := partners.MailboxLock(ctx, orgID, accountID)
	if err != nil {
		return errx.InternalError()
	}
	if name != "" {
		return partnerMailboxLocked(name)
	}
	bought, err := st.MailboxLocked(ctx, orgID, accountID)
	if err != nil {
		return errx.InternalError()
	}
	if bought && !st.Enabled() {
		return store.PurchasedMailboxPaused()
	}
	if bought {
		return store.PurchasedMailboxLocked()
	}
	return nil
}

// refuseLockedMailbox answers a remove or reconnect of a locked mailbox and reports that it did.
func (h *Handler) refuseLockedMailbox(c *gin.Context, accountID string) bool {
	orgID := middleware.GetOrganizationID(c)
	id, err := uuid.Parse(accountID)
	if orgID == nil || err != nil {
		return false
	}
	if xerr := h.mailboxLock(c.Request.Context(), *orgID, id); xerr != nil {
		errx.Handle(c, xerr)
		return true
	}
	return false
}

func partnerMailboxLocked(name string) *errx.Error {
	return errx.NewWithIdentifier(errx.Forbidden, "partner_managed_mailbox",
		"This mailbox is managed by "+name+". Remove or reconnect it there.")
}
