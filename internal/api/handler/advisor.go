// Advisor: read the org's open recommendations, apply a fix, or tell the
// Advisor to stop suggesting something.
//
// Reads are gated on view_analytics (JWT) / READ_ANALYTICS (API key), because
// a finding is a read of the org's sending posture. Applying a fix carries no
// gate of its own: the fix runs through the AI tool registry, which enforces
// the permission the underlying change actually requires. A member who can see
// that a mailbox's cap is too high but cannot edit mailboxes gets a clear 403
// on apply rather than a hidden card.
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/advisor"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// advisorReadMaxAge is how stale the org's findings may be before a read kicks
// off a background refresh. Long enough that browsing the dashboard does not
// trigger repeated evaluations, short enough that a fix you made this morning
// is reflected by the time you look again.
const advisorReadMaxAge = 30 * time.Minute

// advisorOrg resolves the caller's org, or writes the error.
func (h *Handler) advisorOrg(c *gin.Context) (uuid.UUID, bool) {
	if h.AdvisorService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "the Advisor is not configured on this server"))
		return uuid.Nil, false
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return uuid.Nil, false
	}
	return *orgID, true
}

// ListAdvisorFindings — GET /advisor/recommendations
//
// Filters: surface, category, entity_type + entity_id, status, limit. The
// dashboard uses entity_type/entity_id for the inline strip on a campaign or
// mailbox, and surface for a whole tab.
func (h *Handler) ListAdvisorFindings(c *gin.Context) {
	orgID, ok := h.advisorOrg(c)
	if !ok {
		return
	}
	h.refreshAdvisor(c, orgID)

	filter := repository.AdvisorFindingFilter{
		Surface:    models.AdvisorSurface(c.Query("surface")),
		Category:   models.AdvisorCategory(c.Query("category")),
		EntityType: c.Query("entity_type"),
	}
	if raw := c.Query("entity_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid entity_id"))
			return
		}
		filter.EntityID = &id
	}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			errx.JSON(c, errx.New(errx.BadRequest, "limit must be between 1 and 200"))
			return
		}
		filter.Limit = n
	}
	for _, s := range c.QueryArray("status") {
		status := models.AdvisorStatus(s)
		switch status {
		case models.AdvisorStatusOpen, models.AdvisorStatusSnoozed, models.AdvisorStatusDismissed,
			models.AdvisorStatusApplied, models.AdvisorStatusResolved:
			filter.Statuses = append(filter.Statuses, status)
		default:
			errx.JSON(c, errx.New(errx.BadRequest, "invalid status"))
			return
		}
	}

	list, xerr := h.AdvisorService.List(c.Request.Context(), orgID, filter)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// GetAdvisorSummary — GET /advisor/summary
//
// The nav badges and the health score. Cheap enough to poll, but the client
// invalidates it off the audit spine instead.
func (h *Handler) GetAdvisorSummary(c *gin.Context) {
	orgID, ok := h.advisorOrg(c)
	if !ok {
		return
	}
	h.refreshAdvisor(c, orgID)

	summary, xerr := h.AdvisorService.Summary(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, summary)
}

// RefreshAdvisor — POST /advisor/refresh
//
// Forces an evaluation now. Rate-limited by the write limiter on the group;
// the loop keeps things current on its own, so this exists for the moment
// after you have fixed something and want to see it clear.
func (h *Handler) RefreshAdvisor(c *gin.Context) {
	orgID, ok := h.advisorOrg(c)
	if !ok {
		return
	}
	summary, err := h.AdvisorService.Evaluate(c.Request.Context(), orgID, "manual")
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	c.JSON(http.StatusOK, summary)
}

// ApplyAdvisorFinding — POST /advisor/recommendations/:id/apply
//
// Applying twice is a no-op that returns the first outcome, so a retried
// request is safe without an idempotency key.
func (h *Handler) ApplyAdvisorFinding(c *gin.Context) {
	if h.AdvisorService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "the Advisor is not configured on this server"))
		return
	}
	inv, xerr := h.jwtInvocation(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid recommendation id"))
		return
	}

	f, xerr := h.AdvisorService.Apply(c.Request.Context(), inv, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, f)
}

// UndoAdvisorFinding — POST /advisor/recommendations/:id/undo
func (h *Handler) UndoAdvisorFinding(c *gin.Context) {
	if h.AdvisorService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "the Advisor is not configured on this server"))
		return
	}
	inv, xerr := h.jwtInvocation(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid recommendation id"))
		return
	}

	f, xerr := h.AdvisorService.Undo(c.Request.Context(), inv, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, f)
}

// SnoozeAdvisorFinding — POST /advisor/recommendations/:id/snooze
func (h *Handler) SnoozeAdvisorFinding(c *gin.Context) {
	orgID, ok := h.advisorOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid recommendation id"))
		return
	}
	var req models.AdvisorSnoozeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrAdvisorSnoozeRange)
		return
	}
	if xerr := h.AdvisorService.Snooze(c.Request.Context(), orgID, userID, id, req.Days); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DismissAdvisorFinding — POST /advisor/recommendations/:id/dismiss
//
// A dismissal sticks until the underlying condition clears and later recurs,
// so telling the Advisor "this is fine" does not have to be repeated weekly.
func (h *Handler) DismissAdvisorFinding(c *gin.Context) {
	orgID, ok := h.advisorOrg(c)
	if !ok {
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid recommendation id"))
		return
	}
	var req models.AdvisorDismissRequest
	_ = c.ShouldBindJSON(&req)

	if xerr := h.AdvisorService.Dismiss(c.Request.Context(), orgID, userID, id, req.Reason); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// SubmitAdvisorFeedback — POST /advisor/recommendations/:id/feedback
func (h *Handler) SubmitAdvisorFeedback(c *gin.Context) {
	orgID, ok := h.advisorOrg(c)
	if !ok {
		return
	}
	userID, _ := middleware.GetUserUUID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid recommendation id"))
		return
	}
	var req struct {
		Helpful *bool  `json:"helpful" binding:"required"`
		Reason  string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Helpful == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "helpful is required"))
		return
	}
	if xerr := h.AdvisorService.Feedback(c.Request.Context(), orgID, userID, id, *req.Helpful, req.Reason); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GetAdvisorSettings — GET /advisor/settings
func (h *Handler) GetAdvisorSettings(c *gin.Context) {
	orgID, ok := h.advisorOrg(c)
	if !ok {
		return
	}
	s, xerr := h.AdvisorService.GetSettings(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, s)
}

// UpdateAdvisorSettings — PATCH /advisor/settings
func (h *Handler) UpdateAdvisorSettings(c *gin.Context) {
	orgID, ok := h.advisorOrg(c)
	if !ok {
		return
	}
	var req models.AdvisorSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	// This route is JWT-only, so there is always a real member behind it to
	// attribute autopilot to.
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.New(errx.Unauthorized, "invalid user"))
		return
	}
	if xerr := h.AdvisorService.UpdateSettings(c.Request.Context(), orgID, userID, &req); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntitySettings, nil, nil, map[string]string{"advisor": "settings"})
	c.JSON(http.StatusOK, &req)
}

// refreshAdvisor kicks off a background re-evaluation when the org's findings
// have gone stale. It does not block the response: the result arrives on every
// open dashboard through the audit spine a moment later, the same way a
// teammate's change does.
func (h *Handler) refreshAdvisor(c *gin.Context, orgID uuid.UUID) {
	if h.AdvisorRepository == nil {
		return
	}
	advisor.RefreshIfStale(c.Request.Context(), h.AdvisorRepository, h.AdvisorService, orgID, advisorReadMaxAge)
}
