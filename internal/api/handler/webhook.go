package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// webhookEndpointPayload is the wire shape for create/update requests.
// The secret is server-generated and only returned at create / rotate
// time — clients cannot set it directly.
type webhookEndpointPayload struct {
	URL         string   `json:"url"`
	Description string   `json:"description"`
	EventTypes  []string `json:"event_types"`
	Enabled     *bool    `json:"enabled"`
}

// ListWebhookEndpoints returns every endpoint configured for the caller's
// organization. Secrets are not included.
func (h *Handler) ListWebhookEndpoints(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	endpoints, err := h.WebhookService.ListEndpoints(c.Request.Context(), orgID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to list endpoints"))
		return
	}
	if endpoints == nil {
		endpoints = []models.WebhookEndpoint{}
	}
	c.JSON(http.StatusOK, gin.H{
		"endpoints":   endpoints,
		"event_types": models.AllWebhookEventTypes,
	})
}

// CreateWebhookEndpoint creates a new subscription. The response includes
// the signing secret — this is the only time it is returned, so clients
// must capture it. Subsequent reads return the endpoint without the secret.
func (h *Handler) CreateWebhookEndpoint(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var p webhookEndpointPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	enabled := true
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	endpoint, err := h.WebhookService.CreateEndpoint(c.Request.Context(), orgID, p.URL, p.Description, p.EventTypes, enabled)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	endpointID := endpoint.ID
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityWebhook, &endpointID, nil, map[string]string{"url": endpoint.URL})
	c.JSON(http.StatusCreated, endpoint)
}

// UpdateWebhookEndpoint updates a subscription. The secret is not changed
// here — use POST /webhooks/:id/rotate-secret to rotate it.
func (h *Handler) UpdateWebhookEndpoint(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	endpointID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid endpoint id"))
		return
	}
	var p webhookEndpointPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	enabled := true
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	endpoint, err := h.WebhookService.UpdateEndpoint(c.Request.Context(), orgID, endpointID, p.URL, p.Description, p.EventTypes, enabled)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityWebhook, &endpointID, nil, nil)
	c.JSON(http.StatusOK, endpoint)
}

// DeleteWebhookEndpoint deletes a subscription and cascades to its
// delivery history.
func (h *Handler) DeleteWebhookEndpoint(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	endpointID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid endpoint id"))
		return
	}
	if err := h.WebhookService.DeleteEndpoint(c.Request.Context(), orgID, endpointID); err != nil {
		errx.JSON(c, errx.New(errx.NotFound, err.Error()))
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityWebhook, &endpointID, nil, nil)
	c.Status(http.StatusNoContent)
}

// RotateWebhookSecret issues a new signing secret and returns it once.
// Existing in-flight deliveries continue to use the old secret until they
// settle; new deliveries use the new one.
func (h *Handler) RotateWebhookSecret(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	endpointID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid endpoint id"))
		return
	}
	secret, err := h.WebhookService.RotateSecret(c.Request.Context(), orgID, endpointID)
	if err != nil {
		errx.JSON(c, errx.New(errx.NotFound, err.Error()))
		return
	}
	h.auditOrg(c, models.AuditActionRotate, models.AuditEntityWebhook, &endpointID, nil, map[string]string{"rotated": "true"})
	c.JSON(http.StatusOK, gin.H{"secret": secret})
}

// ListWebhookDeliveries returns recent delivery attempts for an endpoint.
// Useful for the dashboard and for debugging integration failures.
func (h *Handler) ListWebhookDeliveries(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	endpointID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid endpoint id"))
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > 200 {
			errx.JSON(c, errx.New(errx.BadRequest, "limit must be between 1 and 200"))
			return
		}
		limit = n
	}
	deliveries, err := h.WebhookService.ListDeliveries(c.Request.Context(), orgID, endpointID, limit)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to list deliveries"))
		return
	}
	if deliveries == nil {
		deliveries = []models.WebhookDelivery{}
	}
	c.JSON(http.StatusOK, gin.H{"deliveries": deliveries})
}
