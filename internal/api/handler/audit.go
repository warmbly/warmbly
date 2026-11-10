package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/advisor"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

// audit records an action performed in the current request's organization. It
// pulls the actor, org, client IP and user-agent from the gin context, so
// handlers only describe what happened. It is a no-op when there is no org
// context (e.g. pre-auth flows); those paths log explicitly with the org they
// create or resolve.
//
// IMPORTANT: never pass secret values (API key material, webhook secrets,
// passwords) in changes or metadata — record only that a field changed.
func (h *Handler) auditOrg(c *gin.Context, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, changes, metadata map[string]string) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		return
	}
	h.auditInOrg(c, *orgID, action, entityType, entityID, changes, metadata)
}

// auditInOrg is auditOrg for an entity whose organization is not the session's.
func (h *Handler) auditInOrg(c *gin.Context, orgID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, changes, metadata map[string]string) {
	actorID, err := middleware.GetUserUUID(c)
	if err != nil {
		return
	}
	metadata = withAgencyKeyActor(c, metadata)
	h.AuditService.LogAction(c.Request.Context(), orgID, actorID, action, entityType, entityID, c.ClientIP(), c.Request.UserAgent(), changes, metadata)
	h.refreshAdvisorAfter(orgID, entityType)
}

// withAgencyKeyActor names the agency API key behind a change, so the trail shows the key as well as the staff member it acts for.
func withAgencyKeyActor(c *gin.Context, metadata map[string]string) map[string]string {
	keyID := middleware.GetAgencyKeyID(c)
	if keyID == nil {
		return metadata
	}
	out := make(map[string]string, len(metadata)+3)
	for k, v := range metadata {
		out[k] = v
	}
	out["actor_type"] = "agency_api_key"
	out["agency_api_key_id"] = keyID.String()
	out["agency_api_key_name"] = middleware.GetAPIKeyName(c)
	return out
}

// Campaign and step edits refresh findings; concurrent refreshes coalesce per org.
func (h *Handler) refreshAdvisorAfter(orgID uuid.UUID, entityType models.AuditEntityType) {
	switch entityType {
	case models.AuditEntityEmailAccount, models.AuditEntityCampaign, models.AuditEntitySequence:
		h.refreshAdvisorNow(orgID)
	}
}

// refreshAdvisorNow requests a background evaluation of the organization's current state.
func (h *Handler) refreshAdvisorNow(orgID uuid.UUID) {
	if h.AdvisorService == nil {
		return
	}
	advisor.RefreshNow(h.AdvisorService, orgID, "change")
}

// GetAuditLogs returns the organization-wide activity trail for the caller's
// current organization ("who did what, when, from where"). Gated to
// owners/admins via PermViewAnalytics. The organization is always taken from
// the session, never from a client parameter, so one org can never read
// another's trail.
//
// GET /audit-logs
func (h *Handler) GetAuditLogs(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrAuth)
		return
	}

	limit, xerr := validate.Limit(c.Query("limit"))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	cursor, xerr := paging.DecodeCursor(c.Query("cursor"))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	params := &models.AuditLogSearch{
		OrgID: orgID,
		Limit: int(limit),
	}
	if cursor != nil {
		params.Cursor = *cursor
	}

	if v := c.Query("actor_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			errx.Handle(c, errx.ErrUuid)
			return
		}
		params.ActorID = &id
	}

	if v := c.Query("entity_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			errx.Handle(c, errx.ErrUuid)
			return
		}
		params.EntityID = &id
	}

	if v := c.Query("entity_type"); v != "" {
		et := models.AuditEntityType(v)
		params.EntityType = &et
	}

	if v := c.Query("action"); v != "" {
		a := models.AuditAction(v)
		params.Action = &a
	}

	// Single-day filter (the dashboard sends ?date=YYYY-MM-DD).
	if v := c.Query("date"); v != "" {
		day, err := time.Parse("2006-01-02", v)
		if err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "date must be in YYYY-MM-DD format"))
			return
		}
		since := day
		until := day.Add(24 * time.Hour)
		params.Since = &since
		params.Until = &until
	}

	// Optional explicit range; overrides the single-day bounds when present.
	if v := c.Query("start_date"); v != "" {
		t, err := parseAuditDate(v)
		if err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "start_date must be RFC3339 or YYYY-MM-DD"))
			return
		}
		params.Since = &t
	}
	if v := c.Query("end_date"); v != "" {
		t, err := parseAuditDate(v)
		if err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "end_date must be RFC3339 or YYYY-MM-DD"))
			return
		}
		params.Until = &t
	}

	result, xerr := h.AuditService.Search(c.Request.Context(), params)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

func parseAuditDate(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", v)
}

// GetUserRateLimits gets rate limits for a user (admin only)
// GET /admin/users/:id/rate-limits
func (h *Handler) GetUserRateLimits(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrNotFound)
		return
	}

	limits, xerr := h.RateLimitService.GetUserLimits(c.Request.Context(), userID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, limits)
}

// UpdateUserRateLimits updates rate limits for a user (admin only)
// PATCH /admin/users/:id/rate-limits
func (h *Handler) UpdateUserRateLimits(c *gin.Context) {
	adminIDStr := middleware.GetUserID(c)
	adminID, err := uuid.Parse(adminIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrAuth)
		return
	}

	userIDStr := c.Param("id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrNotFound)
		return
	}

	var data models.UpdateUserRateLimits
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}

	limits, xerr := h.RateLimitService.UpdateUserLimits(c.Request.Context(), userID, &data, adminID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, limits)
}
