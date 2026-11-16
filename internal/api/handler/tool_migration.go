package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/toolmigrate"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

const (
	toolMigrationListDefault = 20
	toolMigrationListMax     = 100
)

var (
	errToolMigrationsOff = errx.New(errx.ServiceUnavailable, "migrations from other tools are not available on this instance")
	// A migration writes workspace-wide, so a caller held to granted mailboxes or campaigns cannot run one.
	errToolMigrationScoped = errx.NewWithIdentifier(errx.Forbidden, toolmigrate.CodePermission,
		"A migration writes across the whole workspace. Use a key or a role that is not limited to particular mailboxes or campaigns.")
)

// toolMigrationScope resolves a workspace caller: the tenant, and who acts with which permissions.
func (h *Handler) toolMigrationScope(c *gin.Context) (toolmigrate.Scope, toolmigrate.Actor, bool) {
	if h.ToolMigrations == nil {
		errx.JSON(c, errToolMigrationsOff)
		return toolmigrate.Scope{}, toolmigrate.Actor{}, false
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return toolmigrate.Scope{}, toolmigrate.Actor{}, false
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return toolmigrate.Scope{}, toolmigrate.Actor{}, false
	}
	if keyMailboxLimited(c) || middleware.IsScopeRestricted(c) {
		errx.JSON(c, errToolMigrationScoped)
		return toolmigrate.Scope{}, toolmigrate.Actor{}, false
	}
	member := middleware.GetAuthMember(c)
	if member == nil || member.OrganizationID != *orgID || member.UserID != userID {
		m, xerr := h.OrganizationService.GetMembership(c.Request.Context(), *orgID, userID)
		if xerr != nil {
			errx.JSON(c, xerr)
			return toolmigrate.Scope{}, toolmigrate.Actor{}, false
		}
		member = m
	}
	if member == nil || member.IsRestricted() {
		errx.JSON(c, errToolMigrationScoped)
		return toolmigrate.Scope{}, toolmigrate.Actor{}, false
	}
	perms := member.Permissions
	if member.IsOwner() {
		perms = models.AllPermissions
	}
	authType := middleware.GetAuthType(c)
	isKey := authType == middleware.AuthTypeAPIKey || authType == middleware.AuthTypeOAuth
	actor := toolmigrate.ActorFor(userID, perms, middleware.GetAPIKeyPermissions(c), isKey, authType == middleware.AuthTypeJWT)
	return toolmigrate.Scope{OrgID: orgID}, actor, true
}

// agencyMigrationScope resolves an agency caller; the service checks staff and each client membership.
func (h *Handler) agencyMigrationScope(c *gin.Context) (toolmigrate.Scope, toolmigrate.Actor, bool) {
	if h.ToolMigrations == nil {
		errx.JSON(c, errToolMigrationsOff)
		return toolmigrate.Scope{}, toolmigrate.Actor{}, false
	}
	userID, agencyID, ok := h.agencyCaller(c)
	if !ok {
		return toolmigrate.Scope{}, toolmigrate.Actor{}, false
	}
	viaSession := middleware.GetAgencyKeyID(c) == nil && middleware.GetAuthType(c) == middleware.AuthTypeJWT
	return toolmigrate.Scope{AgencyID: &agencyID}, toolmigrate.Actor{UserID: userID, ViaSession: viaSession}, true
}

func toolMigrationParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid migration id"))
		return uuid.Nil, false
	}
	return id, true
}

// ListToolMigrationProviders is GET /migrations/providers and its agency twin.
func (h *Handler) ListToolMigrationProviders(c *gin.Context) {
	if h.ToolMigrations == nil {
		c.JSON(http.StatusOK, gin.H{"data": []source.Info{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": h.ToolMigrations.Providers()})
}

// ListToolMigrations is GET /migrations.
func (h *Handler) ListToolMigrations(c *gin.Context) {
	if scope, _, ok := h.toolMigrationScope(c); ok {
		h.listToolMigrations(c, scope)
	}
}

// CreateToolMigration is POST /migrations: it checks the keys and starts the scan.
func (h *Handler) CreateToolMigration(c *gin.Context) {
	if scope, actor, ok := h.toolMigrationScope(c); ok {
		h.createToolMigration(c, scope, actor)
	}
}

// GetToolMigration is GET /migrations/:id.
func (h *Handler) GetToolMigration(c *gin.Context) {
	if scope, _, ok := h.toolMigrationScope(c); ok {
		h.getToolMigration(c, scope)
	}
}

// StartToolMigration is POST /migrations/:id/start.
func (h *Handler) StartToolMigration(c *gin.Context) {
	if scope, actor, ok := h.toolMigrationScope(c); ok {
		h.startToolMigration(c, scope, actor)
	}
}

// CancelToolMigration is POST /migrations/:id/cancel.
func (h *Handler) CancelToolMigration(c *gin.Context) {
	if scope, actor, ok := h.toolMigrationScope(c); ok {
		h.cancelToolMigration(c, scope, actor)
	}
}

// LinkToolMigrationSenders is POST /migrations/:id/link-senders.
func (h *Handler) LinkToolMigrationSenders(c *gin.Context) {
	if scope, actor, ok := h.toolMigrationScope(c); ok {
		h.linkToolMigrationSenders(c, scope, actor)
	}
}

// DismissToolMigration is DELETE /migrations/:id.
func (h *Handler) DismissToolMigration(c *gin.Context) {
	if scope, actor, ok := h.toolMigrationScope(c); ok {
		h.dismissToolMigration(c, scope, actor)
	}
}

// ListAgencyToolMigrations is GET /agency/:agencyId/migrations.
func (h *Handler) ListAgencyToolMigrations(c *gin.Context) {
	if scope, actor, ok := h.agencyMigrationScope(c); ok && h.agencyStaff(c, scope, actor) {
		h.listToolMigrations(c, scope)
	}
}

// CreateAgencyToolMigration is POST /agency/:agencyId/migrations.
func (h *Handler) CreateAgencyToolMigration(c *gin.Context) {
	if scope, actor, ok := h.agencyMigrationScope(c); ok {
		h.createToolMigration(c, scope, actor)
	}
}

// GetAgencyToolMigration is GET /agency/:agencyId/migrations/:id.
func (h *Handler) GetAgencyToolMigration(c *gin.Context) {
	if scope, actor, ok := h.agencyMigrationScope(c); ok && h.agencyStaff(c, scope, actor) {
		h.getToolMigration(c, scope)
	}
}

// StartAgencyToolMigration is POST /agency/:agencyId/migrations/:id/start.
func (h *Handler) StartAgencyToolMigration(c *gin.Context) {
	if scope, actor, ok := h.agencyMigrationScope(c); ok {
		h.startToolMigration(c, scope, actor)
	}
}

// CancelAgencyToolMigration is POST /agency/:agencyId/migrations/:id/cancel.
func (h *Handler) CancelAgencyToolMigration(c *gin.Context) {
	if scope, actor, ok := h.agencyMigrationScope(c); ok {
		h.cancelToolMigration(c, scope, actor)
	}
}

// LinkAgencyToolMigrationSenders is POST /agency/:agencyId/migrations/:id/link-senders.
func (h *Handler) LinkAgencyToolMigrationSenders(c *gin.Context) {
	if scope, actor, ok := h.agencyMigrationScope(c); ok {
		h.linkToolMigrationSenders(c, scope, actor)
	}
}

// DismissAgencyToolMigration is DELETE /agency/:agencyId/migrations/:id.
func (h *Handler) DismissAgencyToolMigration(c *gin.Context) {
	if scope, actor, ok := h.agencyMigrationScope(c); ok {
		h.dismissToolMigration(c, scope, actor)
	}
}

// agencyStaff refuses anyone not on the agency's staff before a read.
func (h *Handler) agencyStaff(c *gin.Context, scope toolmigrate.Scope, actor toolmigrate.Actor) bool {
	if _, xerr := h.AgencyService.Get(c.Request.Context(), *scope.AgencyID, actor.UserID); xerr != nil {
		errx.JSON(c, xerr)
		return false
	}
	return true
}

func (h *Handler) listToolMigrations(c *gin.Context, scope toolmigrate.Scope) {
	limit := toolMigrationListDefault
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > toolMigrationListMax {
			errx.JSON(c, errx.New(errx.BadRequest, "limit must be between 1 and 100"))
			return
		}
		limit = n
	}
	list, xerr := h.ToolMigrations.List(c.Request.Context(), scope, c.Query("cursor"), limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) createToolMigration(c *gin.Context, scope toolmigrate.Scope, actor toolmigrate.Actor) {
	var in models.NewToolMigration
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	m, xerr := h.ToolMigrations.Create(c.Request.Context(), scope, actor, in)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusCreated, m)
}

func (h *Handler) getToolMigration(c *gin.Context, scope toolmigrate.Scope) {
	id, ok := toolMigrationParam(c)
	if !ok {
		return
	}
	m, xerr := h.ToolMigrations.Get(c.Request.Context(), scope, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, m)
}

func (h *Handler) startToolMigration(c *gin.Context, scope toolmigrate.Scope, actor toolmigrate.Actor) {
	id, ok := toolMigrationParam(c)
	if !ok {
		return
	}
	var in models.StartToolMigration
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	if middleware.GetAgencyKeyID(c) != nil {
		// An agency key's own scopes bound what it writes; the service checks the member it acts for.
		if xerr := toolmigrate.AgencyKeyCheck(middleware.GetAPIKeyPermissions(c), in.Options.Kinds, toolmigrate.CreatesClients(in), false); xerr != nil {
			errx.JSON(c, xerr)
			return
		}
	}
	m, xerr := h.ToolMigrations.Start(c.Request.Context(), scope, actor, id, in)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, m)
}

func (h *Handler) cancelToolMigration(c *gin.Context, scope toolmigrate.Scope, actor toolmigrate.Actor) {
	id, ok := toolMigrationParam(c)
	if !ok {
		return
	}
	m, xerr := h.ToolMigrations.Cancel(c.Request.Context(), scope, actor, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, m)
}

func (h *Handler) linkToolMigrationSenders(c *gin.Context, scope toolmigrate.Scope, actor toolmigrate.Actor) {
	id, ok := toolMigrationParam(c)
	if !ok {
		return
	}
	if middleware.GetAgencyKeyID(c) != nil {
		if xerr := toolmigrate.AgencyKeyCheck(middleware.GetAPIKeyPermissions(c), nil, false, true); xerr != nil {
			errx.JSON(c, xerr)
			return
		}
	}
	m, xerr := h.ToolMigrations.LinkSenders(c.Request.Context(), scope, actor, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, m)
}

func (h *Handler) dismissToolMigration(c *gin.Context, scope toolmigrate.Scope, actor toolmigrate.Actor) {
	id, ok := toolMigrationParam(c)
	if !ok {
		return
	}
	if xerr := h.ToolMigrations.Dismiss(c.Request.Context(), scope, actor, id); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.Status(http.StatusNoContent)
}
