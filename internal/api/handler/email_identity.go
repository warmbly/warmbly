package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// GetEmailSendIdentity reports which addresses this mailbox may send as, which
// one it uses, and where its signature came from. Stored state only: the
// provider is not called, so opening the drawer costs nothing at Google.
// GET /emails/:id/identity
func (h *Handler) GetEmailSendIdentity(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}

	identity, err := h.EmailService.GetSendIdentity(c.Request.Context(), orgID.String(), c.Param("id"))
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, identity)
}

// refreshSendIdentityRequest is the body of the refresh. Importing the
// signature is opt-in because it overwrites what is stored, and the two
// reasons to press refresh (a new alias, a changed signature) are not the
// same request.
type refreshSendIdentityRequest struct {
	ImportSignature bool `json:"import_signature"`
}

// RefreshEmailSendIdentity re-reads the mailbox's send-as addresses from the
// provider and stores them, optionally importing the provider's signature.
//
// Retry-safe without an Idempotency-Key: it reads the provider's current state
// and writes exactly that, so repeating it converges on the same row.
// POST /emails/:id/identity/refresh
func (h *Handler) RefreshEmailSendIdentity(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}

	// An absent body means "just the addresses", which is the common press.
	// Only a declared-empty body skips binding: a chunked request carries -1,
	// and treating that as empty dropped import_signature on the floor.
	var req refreshSendIdentityRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			errx.Handle(c, errx.ErrInvalid)
			return
		}
	}

	emailAccountID := c.Param("id")
	identity, err := h.EmailService.RefreshSendIdentity(c.Request.Context(), orgID.String(), emailAccountID, req.ImportSignature)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	// Audited like any other mailbox write, which is also what refreshes every
	// teammate's view of the mailbox through the realtime spine.
	if accountID, perr := uuid.Parse(emailAccountID); perr == nil {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, &accountID, nil, nil)
	}

	c.JSON(http.StatusOK, identity)
}
