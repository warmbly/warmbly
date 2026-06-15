package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
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

// ListWebhookEventCatalog serves the full outbound event vocabulary so
// integrators can discover every event (and which are high-volume/opt-in)
// without scraping the docs. Public within the org gate.
//
// GET /webhooks/event-types
func (h *Handler) ListWebhookEventCatalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"event_types": models.WebhookEventCatalog})
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
		"event_types": models.WebhookEventCatalog,
	})
}

// CreateWebhookEndpoint creates a new subscription. The response includes
// the signing secret — this is the only time it is returned, so clients
// must capture it. Subsequent reads return the endpoint without the secret.
//
// When the caller authenticated with an OAuth access token, the endpoint is
// bound to that app and its URL host must fall within the app's allowed
// webhook domains.
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
	in := webhook.EndpointInput{
		URL:                p.URL,
		Description:        p.Description,
		EventTypes:         p.EventTypes,
		Enabled:            enabled,
		OAuthApplicationID: middleware.GetOAuthApplicationID(c),
	}
	if uid, err := middleware.GetUserUUID(c); err == nil {
		in.CreatedBy = &uid
	}
	endpoint, err := h.WebhookService.CreateEndpoint(c.Request.Context(), orgID, in)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	endpointID := endpoint.ID
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityWebhook, &endpointID, nil, map[string]string{"url": endpoint.URL})
	c.JSON(http.StatusCreated, endpoint)
}

// UpdateWebhookEndpoint updates a subscription. The secret is not changed
// here — use POST /webhooks/:id/rotate-secret to rotate it. Changing the URL
// host re-arms ownership verification.
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

// VerifyWebhookEndpoint (re)sends the signed challenge / test request to the
// endpoint so it can prove it accepts our deliveries. Doubles as "send a test
// event": a 2xx verifies the endpoint and starts the event stream.
//
// POST /webhooks/:id/verify
func (h *Handler) VerifyWebhookEndpoint(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	endpointID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid endpoint id"))
		return
	}
	if err := h.WebhookService.VerifyEndpoint(c.Request.Context(), orgID, endpointID); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "challenge_sent"})
}

// ListWebhookDeliveries returns recent delivery attempts (org-wide, or for one
// endpoint via ?endpoint_id=), filterable by status/event_type, with opaque
// cursor pagination.
//
// GET /webhooks/deliveries  and  GET /webhooks/:id/deliveries
func (h *Handler) ListWebhookDeliveries(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}

	filter := models.WebhookDeliveryFilter{
		Status:    c.Query("status"),
		EventType: c.Query("event_type"),
	}

	// Endpoint id can come from the path (/:id/deliveries) or the query.
	if raw := c.Param("id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid endpoint id"))
			return
		}
		filter.EndpointID = &id
	} else if raw := c.Query("endpoint_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid endpoint id"))
			return
		}
		filter.EndpointID = &id
	}

	filter.Limit = 50
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > 200 {
			errx.JSON(c, errx.New(errx.BadRequest, "limit must be between 1 and 200"))
			return
		}
		filter.Limit = n
	}

	offset, xerr := paging.DecodeOffsetCursor(c.Query("cursor"))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	filter.Offset = offset

	result, err := h.WebhookService.ListDeliveries(c.Request.Context(), orgID, filter)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to list deliveries"))
		return
	}
	c.JSON(http.StatusOK, result)
}

// RedeliverWebhookDelivery re-queues an existing delivery (same event id) for a
// fresh attempt cycle.
//
// POST /webhooks/deliveries/:deliveryId/redeliver
func (h *Handler) RedeliverWebhookDelivery(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	deliveryID, err := uuid.Parse(c.Param("deliveryId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid delivery id"))
		return
	}
	if err := h.WebhookService.Redeliver(c.Request.Context(), orgID, deliveryID); err != nil {
		errx.JSON(c, errx.New(errx.NotFound, err.Error()))
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "queued"})
}

// ListWebhookDrops surfaces events the dispatch throttle dropped (rate-limiting
// visibility), rolled up by day for the last 30 days.
//
// GET /webhooks/throttle-drops
func (h *Handler) ListWebhookDrops(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	since := time.Now().UTC().AddDate(0, 0, -30)
	drops, err := h.WebhookService.ListEventDrops(c.Request.Context(), orgID, since)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to list throttle drops"))
		return
	}
	if drops == nil {
		drops = []models.WebhookEventDrop{}
	}
	c.JSON(http.StatusOK, gin.H{"drops": drops})
}
