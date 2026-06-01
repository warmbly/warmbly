package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// requireIntegrationActor resolves the org + user for a mutating integration
// request and enforces the paid-plan gate. Browsing the catalog / listing
// connections is open (so non-paid orgs see the upsell); connecting or
// authorizing requires an active paid subscription.
func (h *Handler) requireIntegrationActor(c *gin.Context, requirePaid bool) (orgID, userID uuid.UUID, ok bool) {
	orgID, ok = requireOrgID(c)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	uid, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.JSON(c, errx.New(errx.Unauthorized, "invalid user"))
		return uuid.Nil, uuid.Nil, false
	}
	if requirePaid && h.FeatureGateService != nil {
		paid, xerr := h.FeatureGateService.IsPaidOrganization(c.Request.Context(), orgID)
		if xerr != nil {
			errx.JSON(c, xerr)
			return uuid.Nil, uuid.Nil, false
		}
		if !paid {
			errx.JSON(c, errx.New(errx.Forbidden, "Integrations are available on paid plans. Upgrade to connect this app."))
			return uuid.Nil, uuid.Nil, false
		}
	}
	return orgID, uid, true
}

// ListIntegrationCatalog returns the static metadata for every integration
// Warmbly supports, annotated with whether each OAuth provider is wired.
func (h *Handler) ListIntegrationCatalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"catalog": h.IntegrationService.Catalog()})
}

// ListIntegrationConnections returns this org's connection rows (no secrets).
func (h *Handler) ListIntegrationConnections(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	conns, err := h.IntegrationService.ListConnections(c.Request.Context(), orgID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to list connections"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"connections": conns})
}

// GetIntegrationConnection returns a single connection with its event
// subscriptions and recent sync runs — the detail drawer payload.
func (h *Handler) GetIntegrationConnection(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	conn, err := h.IntegrationService.GetConnection(c.Request.Context(), orgID, id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to load connection"))
		return
	}
	if conn == nil {
		errx.JSON(c, errx.New(errx.NotFound, "connection not found"))
		return
	}
	subs, _ := h.IntegrationService.ListEventSubscriptions(c.Request.Context(), orgID, id)
	runs, _ := h.IntegrationService.ListSyncRuns(c.Request.Context(), orgID, id, 20)
	if subs == nil {
		subs = []models.IntegrationEventSubscription{}
	}
	if runs == nil {
		runs = []models.IntegrationSyncRun{}
	}
	c.JSON(http.StatusOK, gin.H{"connection": conn, "events": subs, "runs": runs})
}

type integrationConnectPayload struct {
	Provider string         `json:"provider"`
	Label    string         `json:"label"`
	Config   map[string]any `json:"config"`
}

// ConnectIntegration creates a credential-based connection (api-key / webhook
// providers). OAuth providers are rejected here with a hint to use the
// authorize flow.
func (h *Handler) ConnectIntegration(c *gin.Context) {
	orgID, userID, ok := h.requireIntegrationActor(c, true)
	if !ok {
		return
	}
	var p integrationConnectPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	provider := models.IntegrationProvider(strings.TrimSpace(p.Provider))
	if !models.IsValidIntegrationProvider(string(provider)) {
		errx.JSON(c, errx.New(errx.BadRequest, "unknown provider"))
		return
	}
	conn, err := h.IntegrationService.Connect(c.Request.Context(), orgID, userID, provider, p.Label, p.Config)
	if err != nil {
		if errors.Is(err, integration.ErrUseOAuth) {
			errx.JSON(c, errx.New(errx.BadRequest, "This provider connects via OAuth — start the authorize flow instead."))
			return
		}
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	h.auditIntegration(c, userID, models.AuditActionCreate, conn.ID, string(provider))
	c.JSON(http.StatusCreated, conn)
}

// DisconnectIntegration removes a connection row.
func (h *Handler) DisconnectIntegration(c *gin.Context) {
	orgID, userID, ok := h.requireIntegrationActor(c, false)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	if err := h.IntegrationService.Disconnect(c.Request.Context(), orgID, id); err != nil {
		errx.JSON(c, errx.New(errx.Internal, "delete failed"))
		return
	}
	h.auditIntegration(c, userID, models.AuditActionDelete, id, "")
	c.Status(http.StatusNoContent)
}

type oauthStartPayload struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
}

// StartIntegrationOAuth returns the provider authorization URL for the SPA to
// open in a popup. JWT-only: it writes user-encrypted tokens on completion.
func (h *Handler) StartIntegrationOAuth(c *gin.Context) {
	orgID, userID, ok := h.requireIntegrationActor(c, true)
	if !ok {
		return
	}
	var p oauthStartPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	provider := models.IntegrationProvider(strings.TrimSpace(p.Provider))
	if !models.IsValidIntegrationProvider(string(provider)) {
		errx.JSON(c, errx.New(errx.BadRequest, "unknown provider"))
		return
	}
	resp, err := h.IntegrationService.OAuthStart(c.Request.Context(), orgID, userID, provider, p.Label)
	if err != nil {
		if errors.Is(err, integration.ErrOAuthNotConfigured) {
			errx.JSON(c, errx.New(errx.NotImplemented, "This provider isn't available yet — OAuth credentials are not configured on the server."))
			return
		}
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	c.JSON(http.StatusOK, resp)
}

type oauthFinishPayload struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

// FinishIntegrationOAuth completes the handshake and persists the connection.
func (h *Handler) FinishIntegrationOAuth(c *gin.Context) {
	userID, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.JSON(c, errx.New(errx.Unauthorized, "invalid user"))
		return
	}
	var p oauthFinishPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	conn, xerr := h.IntegrationService.OAuthFinish(c.Request.Context(), userID, p.Code, p.State)
	if xerr != nil {
		errx.JSON(c, errx.New(errx.BadRequest, xerr.Error()))
		return
	}
	h.auditIntegration(c, userID, models.AuditActionCreate, conn.ID, string(conn.Provider))
	c.JSON(http.StatusCreated, conn)
}

// ReauthIntegration starts a fresh OAuth handshake for an existing connection.
func (h *Handler) ReauthIntegration(c *gin.Context) {
	orgID, userID, ok := h.requireIntegrationActor(c, true)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	resp, rerr := h.IntegrationService.Reauth(c.Request.Context(), orgID, userID, id)
	if rerr != nil {
		errx.JSON(c, errx.New(errx.BadRequest, rerr.Error()))
		return
	}
	c.JSON(http.StatusOK, resp)
}

// IntegrationOAuthCallback is the public bouncer page the provider redirects to.
// It postMessages the code+state back to the SPA opener, which then calls
// FinishIntegrationOAuth. Mirrors the mailbox onboarding callback.
func (h *Handler) IntegrationOAuthCallback(c *gin.Context) {
	payload := map[string]string{
		"source": "warmbly-integration-oauth",
		"code":   c.Query("code"),
		"state":  c.Query("state"),
		"error":  c.Query("error"),
	}
	// json.Marshal escapes <, >, & so the blob is safe to inline in <script>.
	blob, _ := json.Marshal(payload)
	html := `<!doctype html><html><head><meta charset="utf-8"><title>Connecting…</title></head>
<body style="font-family:system-ui;background:#f8fafc;color:#0f172a;display:flex;align-items:center;justify-content:center;height:100vh;margin:0">
<div style="text-align:center">
<p style="font-size:14px">Finishing connection… you can close this window.</p>
</div>
<script>
(function(){
  var msg = ` + string(blob) + `;
  try { if (window.opener) { window.opener.postMessage(msg, "*"); } } catch (e) {}
  setTimeout(function(){ window.close(); }, 300);
})();
</script>
</body></html>`
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, html)
}

// --- Event subscriptions ----------------------------------------------------

type eventSubscriptionPayload struct {
	EventType string         `json:"event_type"`
	Action    string         `json:"action"`
	Config    map[string]any `json:"config"`
	Enabled   *bool          `json:"enabled"`
}

func (h *Handler) ListConnectionEventSubscriptions(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	connID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	subs, err := h.IntegrationService.ListEventSubscriptions(c.Request.Context(), orgID, connID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to list event subscriptions"))
		return
	}
	if subs == nil {
		subs = []models.IntegrationEventSubscription{}
	}
	c.JSON(http.StatusOK, gin.H{"events": subs})
}

func (h *Handler) CreateConnectionEventSubscription(c *gin.Context) {
	orgID, userID, ok := h.requireIntegrationActor(c, true)
	if !ok {
		return
	}
	connID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	var p eventSubscriptionPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	enabled := true
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	sub, err := h.IntegrationService.CreateEventSubscription(c.Request.Context(), orgID, connID,
		strings.TrimSpace(p.EventType), models.IntegrationAction(strings.TrimSpace(p.Action)), p.Config, enabled)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	h.auditIntegration(c, userID, models.AuditActionUpdate, connID, "event:"+p.EventType)
	c.JSON(http.StatusCreated, sub)
}

func (h *Handler) DeleteConnectionEventSubscription(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	subID, err := uuid.Parse(c.Param("eventId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	if err := h.IntegrationService.DeleteEventSubscription(c.Request.Context(), orgID, subID); err != nil {
		errx.JSON(c, errx.New(errx.Internal, "delete failed"))
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ListConnectionSyncRuns(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	connID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	runs, err := h.IntegrationService.ListSyncRuns(c.Request.Context(), orgID, connID, 50)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to list runs"))
		return
	}
	if runs == nil {
		runs = []models.IntegrationSyncRun{}
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs})
}

// --- Inbound webhooks (Calendly / Cal.com) ----------------------------------

func (h *Handler) InboundCalendly(c *gin.Context) {
	h.handleInboundBooking(c, models.IntegrationCalendly)
}

func (h *Handler) InboundCalCom(c *gin.Context) {
	h.handleInboundBooking(c, models.IntegrationCalCom)
}

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

	if booking != nil {
		data := map[string]any{
			"source":        booking.Source,
			"invitee_email": booking.InviteeEmail,
			"event_name":    booking.EventName,
			"scheduled_for": booking.ScheduledFor,
			"contact_id":    booking.ContactID,
			"booking_id":    booking.ID,
			"trigger":       "meeting_booked",
		}
		// WebhookService.Dispatch fans the booking out to customer webhooks AND,
		// via the wired sink, to integration event actions (Slack ping, CRM upsert).
		if h.WebhookService != nil {
			_, _ = h.WebhookService.Dispatch(c.Request.Context(), conn.OrganizationID, models.WebhookEventCampaignReplyReceived, data)
		}
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
		errx.JSON(c, errx.New(errx.Internal, "list failed"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"bookings": rows})
}

// auditIntegration is a thin best-effort audit-log wrapper.
func (h *Handler) auditIntegration(c *gin.Context, userID uuid.UUID, action models.AuditAction, entityID uuid.UUID, detail string) {
	if h.AuditService == nil {
		return
	}
	meta := map[string]string{}
	if detail != "" {
		meta["detail"] = detail
	}
	id := entityID
	h.AuditService.LogAction(c.Request.Context(), userID, action, models.AuditEntityIntegration, &id, c.ClientIP(), c.Request.UserAgent(), meta, nil)
}
