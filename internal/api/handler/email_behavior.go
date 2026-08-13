package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/behavior"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Sending-behaviour endpoints. The profile is a mailbox-scoped resource rather
// than more fields on PATCH /emails/:id: it has its own validation surface, its
// own derived read (today's rolled plan), and a customer can reasonably let a
// teammate tune sending hours without handing them the mailbox itself.

// resolveOwnedMailbox parses the :id param and confirms the caller's
// organization owns that mailbox. Every behaviour route goes through it, so a
// mailbox id from another workspace 404s rather than leaking its schedule.
func (h *Handler) resolveOwnedMailbox(c *gin.Context) (uuid.UUID, bool) {
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return uuid.Nil, false
	}

	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return uuid.Nil, false
	}

	if _, gerr := h.EmailService.Get(c.Request.Context(), orgID.String(), accountID.String()); gerr != nil {
		errx.Handle(c, gerr)
		return uuid.Nil, false
	}
	return accountID, true
}

// behaviorUnavailable reports the feature as unconfigured rather than 500ing,
// for deployments running without the behaviour engine wired.
func (h *Handler) behaviorUnavailable(c *gin.Context) bool {
	if h.BehaviorService == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "sending behaviour is not enabled on this deployment"))
		return true
	}
	return false
}

// handleBehaviorError maps the service's errors onto the API's error shape,
// turning a validation failure into a 400 that names the offending field.
func handleBehaviorError(c *gin.Context, err error) {
	var verr *models.BehaviorValidationError
	if errors.As(err, &verr) {
		errx.Handle(c, errx.New(errx.BadRequest, verr.Message))
		return
	}
	if errors.Is(err, behavior.ErrNotFound) {
		errx.Handle(c, errx.ErrNotFound)
		return
	}
	var apiErr *errx.Error
	if errors.As(err, &apiErr) {
		errx.Handle(c, apiErr)
		return
	}
	errx.Handle(c, errx.InternalError())
}

// GetEmailBehavior returns a mailbox's sending-behaviour profile, substituting
// the defaults for a mailbox that has never been configured so the client
// always has a complete object to render.
// GET /emails/:id/behavior
func (h *Handler) GetEmailBehavior(c *gin.Context) {
	if h.behaviorUnavailable(c) {
		return
	}
	accountID, ok := h.resolveOwnedMailbox(c)
	if !ok {
		return
	}

	profile, err := h.BehaviorService.Get(c.Request.Context(), accountID)
	if err != nil {
		handleBehaviorError(c, err)
		return
	}
	c.JSON(http.StatusOK, profile)
}

// UpdateEmailBehavior applies a partial update to the profile. Omitted fields
// keep their stored value, so toggling `enabled` does not require resending
// every range.
//
// Naturally idempotent: the body is the desired state, so a retry converges on
// the same profile and no Idempotency-Key is needed.
// PUT /emails/:id/behavior
func (h *Handler) UpdateEmailBehavior(c *gin.Context) {
	if h.behaviorUnavailable(c) {
		return
	}
	accountID, ok := h.resolveOwnedMailbox(c)
	if !ok {
		return
	}

	var patch models.UpdateSendingBehavior
	if err := c.ShouldBindJSON(&patch); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	profile, err := h.BehaviorService.Update(c.Request.Context(), accountID, patch)
	if err != nil {
		handleBehaviorError(c, err)
		return
	}

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityEmailAccount, &accountID, nil, nil)
	c.JSON(http.StatusOK, profile)
}

// GetEmailBehaviorPlan returns the workday this mailbox actually rolled for the
// current local date, plus how much of it is already spent. This is the read
// that answers "why is nothing sending right now" without anyone guessing.
// GET /emails/:id/behavior/plan
func (h *Handler) GetEmailBehaviorPlan(c *gin.Context) {
	if h.behaviorUnavailable(c) {
		return
	}
	accountID, ok := h.resolveOwnedMailbox(c)
	if !ok {
		return
	}

	plan, err := h.BehaviorService.Today(c.Request.Context(), accountID)
	if err != nil {
		handleBehaviorError(c, err)
		return
	}
	c.JSON(http.StatusOK, plan)
}
