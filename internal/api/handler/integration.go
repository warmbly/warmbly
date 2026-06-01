package handler

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/models"
)

// ListIntegrationCatalog returns the static metadata for every integration
// Warmbly supports. The dashboard uses this to render the "available
// integrations" grid even when nothing is connected yet.
func (h *Handler) ListIntegrationCatalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"catalog": h.IntegrationService.Catalog(),
	})
}

// ListIntegrationConnections returns this org's connection rows.
// No secrets, no encrypted config.
func (h *Handler) ListIntegrationConnections(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	conns, err := h.IntegrationService.ListConnections(c.Request.Context(), orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list connections"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"connections": conns})
}

// integrationConnectPayload is the create-connection request body. The
// `config` map is per-provider; see integration.buildDisplayFields for
// which keys are recognized.
type integrationConnectPayload struct {
	Provider string         `json:"provider"`
	Label    string         `json:"label"`
	Config   map[string]any `json:"config"`
}

// ConnectIntegration creates or updates a connection. For inbound-webhook
// providers (Calendly, Cal.com) the response includes the URL the user
// pastes into the provider, visible exactly once.
func (h *Handler) ConnectIntegration(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var p integrationConnectPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	provider := models.IntegrationProvider(strings.TrimSpace(p.Provider))
	if !models.IsValidIntegrationProvider(string(provider)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown provider"})
		return
	}

	conn, err := h.IntegrationService.Connect(c.Request.Context(), orgID, provider, p.Label, p.Config)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.auditOrg(c, models.AuditActionConnect, models.AuditEntityIntegration, &conn.ID, nil, map[string]string{"provider": string(provider)})

	c.JSON(http.StatusCreated, conn)
}

// DisconnectIntegration removes a connection row. Cascading FKs handle
// dependent data (bookings) per the migration.
func (h *Handler) DisconnectIntegration(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.IntegrationService.Disconnect(c.Request.Context(), orgID, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}

	h.auditOrg(c, models.AuditActionDisconnect, models.AuditEntityIntegration, &id, nil, nil)

	c.Status(http.StatusNoContent)
}

// Inbound webhooks
//
// Per-provider inbound endpoints. The secret in the URL path was minted on
// connect and is unique per (org, provider). No org context comes from
// the auth middleware here because providers POST from their own
// infrastructure with no Warmbly bearer.

// InboundCalendly handles invitee.created webhooks. The secret in the URL
// routes the event to the right org.
func (h *Handler) InboundCalendly(c *gin.Context) {
	h.handleInboundBooking(c, models.IntegrationCalendly)
}

func (h *Handler) InboundCalCom(c *gin.Context) {
	h.handleInboundBooking(c, models.IntegrationCalCom)
}

// handleInboundBooking is shared between Calendly and Cal.com. The
// per-provider parsing logic differs but the routing (secret to org, save
// booking, fire webhook) is identical.
func (h *Handler) handleInboundBooking(c *gin.Context, provider models.IntegrationProvider) {
	secret := strings.TrimSpace(c.Param("secret"))
	if secret == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "secret required"})
		return
	}
	conn, err := h.IntegrationService.Repo().GetConnectionByInboundSecret(c.Request.Context(), provider, secret)
	if err != nil || conn == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown secret"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
		return
	}

	matcher := integration.NewBookingMatcher(func(ctx context.Context, orgID uuid.UUID, email string) (*uuid.UUID, error) {
		if h.ContactRepo == nil {
			return nil, nil
		}
		contact, xerr := h.ContactRepo.GetByEmailAndOrganization(ctx, orgID, email)
		if xerr != nil {
			return nil, xerr
		}
		if contact == nil {
			return nil, nil
		}
		return &contact.ID, nil
	})

	var booking *models.MeetingBooking
	switch provider {
	case models.IntegrationCalendly:
		booking, err = integration.HandleCalendlyEvent(c.Request.Context(), h.IntegrationService.Repo(), matcher, conn.OrganizationID, body)
	case models.IntegrationCalCom:
		booking, err = integration.HandleCalComEvent(c.Request.Context(), h.IntegrationService.Repo(), matcher, conn.OrganizationID, body)
	}

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if booking != nil && h.WebhookService != nil {
		_, _ = h.WebhookService.Dispatch(c.Request.Context(), conn.OrganizationID, models.WebhookEventCampaignReplyReceived, map[string]any{
			"source":        booking.Source,
			"invitee_email": booking.InviteeEmail,
			"event_name":    booking.EventName,
			"scheduled_for": booking.ScheduledFor,
			"contact_id":    booking.ContactID,
			"booking_id":    booking.ID,
			"trigger":       "meeting_booked",
		})
	}
	c.JSON(http.StatusOK, gin.H{"received": true})
}

// ListMeetingBookings surfaces booked meetings for the integrations page.
func (h *Handler) ListMeetingBookings(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	rows, err := h.IntegrationService.Repo().ListMeetingBookings(c.Request.Context(), orgID, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"bookings": rows})
}
