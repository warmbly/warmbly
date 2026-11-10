package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Client connect links on the dashboard side: a member with manage-emails creates,
// lists and revokes the links a client uses to connect mailboxes to the workspace.

const connectLinkListDefault = 50

// errConnectLinksUnavailable answers when the service is not wired on this instance.
var errConnectLinksUnavailable = errx.NewWithIdentifier(errx.NotImplemented, "client_links_unavailable",
	"Client connect links are not available on this instance.")

// CreateConnectLink is POST /v1/emails/client-links. The URL is in this response only.
// A retry with the same Idempotency-Key replays the same link instead of minting a second one.
func (h *Handler) CreateConnectLink(c *gin.Context) {
	if h.ConnectLinkService == nil {
		errx.Handle(c, errConnectLinksUnavailable)
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUnauthorized)
		return
	}
	var req models.NewClientConnectLink
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	out, xerr := h.ConnectLinkService.Create(c.Request.Context(), *orgID, userID, req)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityClientConnectLink, &out.Link.ID, nil, map[string]string{
		"providers":     strings.Join(out.Link.AllowedProviders, ","),
		"max_mailboxes": strconv.Itoa(out.Link.MaxMailboxes),
	})
	c.JSON(http.StatusCreated, out)
}

// ListConnectLinks is GET /v1/emails/client-links.
func (h *Handler) ListConnectLinks(c *gin.Context) {
	if h.ConnectLinkService == nil {
		errx.Handle(c, errConnectLinksUnavailable)
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}
	limit := connectLinkListDefault
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			errx.Handle(c, errx.ErrLimit)
			return
		}
		limit = n
	}
	out, xerr := h.ConnectLinkService.List(c.Request.Context(), *orgID, c.Query("cursor"), limit)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// ListConnectLinkUses is GET /v1/emails/client-links/:id/uses: the mailboxes a link connected.
func (h *Handler) ListConnectLinkUses(c *gin.Context) {
	if h.ConnectLinkService == nil {
		errx.Handle(c, errConnectLinksUnavailable)
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}
	uses, xerr := h.ConnectLinkService.Uses(c.Request.Context(), *orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": uses})
}

// RevokeConnectLink is POST /v1/emails/client-links/:id/revoke. A repeat returns the
// already revoked link, so a retry is safe without an Idempotency-Key.
func (h *Handler) RevokeConnectLink(c *gin.Context) {
	if h.ConnectLinkService == nil {
		errx.Handle(c, errConnectLinksUnavailable)
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.ErrNoOrganization)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}
	link, xerr := h.ConnectLinkService.Revoke(c.Request.Context(), *orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionRevoke, models.AuditEntityClientConnectLink, &link.ID, nil, nil)
	c.JSON(http.StatusOK, link)
}
