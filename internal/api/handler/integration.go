package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
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

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityIntegration, &subID, nil, map[string]string{"detail": "event_subscription"})

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

// --- Synchronous contact push -----------------------------------------------

type pushContactsPayload struct {
	ContactIDs []string `json:"contact_ids"`
}

// PushContactsToIntegration synchronously upserts the given org contacts into a
// connected CRM (HubSpot, Pipedrive, Salesforce, Close). This is the contextual
// "push to CRM" action surfaced in Contacts. It is gated on the operational
// PermUseIntegrations / APIPermIntegrations (see routes.go) rather than the
// settings permission, so an operator can push without full settings access.
//
// Retries are naturally safe: every provider upsert is keyed by email, so a
// repeated push converges rather than duplicating records — no Idempotency-Key
// bookkeeping is required. Per-record results are returned so the dashboard can
// report exactly which contacts synced.
func (h *Handler) PushContactsToIntegration(c *gin.Context) {
	orgID, userID, ok := h.requireIntegrationActor(c, true)
	if !ok {
		return
	}
	connID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	var p pushContactsPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}

	// Parse + dedupe the requested contact IDs.
	seen := make(map[uuid.UUID]struct{}, len(p.ContactIDs))
	ids := make([]uuid.UUID, 0, len(p.ContactIDs))
	for _, raw := range p.ContactIDs {
		id, perr := uuid.Parse(strings.TrimSpace(raw))
		if perr != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid contact id: "+raw))
			return
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		errx.JSON(c, errx.New(errx.BadRequest, "no contacts provided"))
		return
	}
	if len(ids) > 500 {
		errx.JSON(c, errx.New(errx.BadRequest, "too many contacts in one push (max 500)"))
		return
	}
	if h.ContactRepo == nil {
		errx.JSON(c, errx.New(errx.Internal, "contacts unavailable"))
		return
	}

	contacts, xerr := h.ContactRepo.GetByIDsAndOrganization(c.Request.Context(), orgID, ids)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if len(contacts) == 0 {
		errx.JSON(c, errx.New(errx.NotFound, "no matching contacts found"))
		return
	}

	push := make([]integration.PushContact, 0, len(contacts))
	for _, ct := range contacts {
		push = append(push, integration.PushContact{
			ID:        ct.ID,
			Email:     ct.Email,
			FirstName: ct.FirstName,
			LastName:  ct.LastName,
			Company:   ct.Company,
			Phone:     ct.Phone,
		})
	}

	res, perr := h.IntegrationService.PushContacts(c.Request.Context(), orgID, connID, push)
	if perr != nil {
		switch {
		case errors.Is(perr, integration.ErrPushReauth):
			errx.JSON(c, errx.New(errx.Conflict, perr.Error()))
		default:
			errx.JSON(c, errx.New(errx.BadRequest, perr.Error()))
		}
		return
	}
	h.auditIntegration(c, userID, models.AuditActionUpdate, connID, fmt.Sprintf("push:%d", res.Pushed))
	c.JSON(http.StatusOK, res)
}

// --- Field mappings + connection config -------------------------------------

// ListConnectionFieldMappings returns the field maps configured for a connection
// (readable by operational integration users, not only settings managers).
func (h *Handler) ListConnectionFieldMappings(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	connID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	rows, err := h.IntegrationService.ListFieldMappings(c.Request.Context(), orgID, connID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to list field mappings"))
		return
	}
	if rows == nil {
		rows = []models.IntegrationFieldMapping{}
	}
	c.JSON(http.StatusOK, gin.H{"mappings": rows})
}

type fieldMappingPayload struct {
	WarmblyField  string `json:"warmbly_field"`
	ExternalField string `json:"external_field"`
	Transform     string `json:"transform"`
	StaticValue   string `json:"static_value"`
}

type replaceFieldMappingsPayload struct {
	Object   string                `json:"object"`
	Mappings []fieldMappingPayload `json:"mappings"`
}

// ReplaceConnectionFieldMappings swaps the connection-default field map for an
// object. A full replace is naturally idempotent, so retries are safe without an
// Idempotency-Key.
func (h *Handler) ReplaceConnectionFieldMappings(c *gin.Context) {
	orgID, userID, ok := h.requireIntegrationActor(c, true)
	if !ok {
		return
	}
	connID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	var p replaceFieldMappingsPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	mappings := make([]models.IntegrationFieldMapping, 0, len(p.Mappings))
	for _, m := range p.Mappings {
		if strings.TrimSpace(m.ExternalField) == "" {
			errx.JSON(c, errx.New(errx.BadRequest, "every mapping needs a destination field"))
			return
		}
		transform := strings.TrimSpace(m.Transform)
		switch models.FieldTransform(transform) {
		case "", models.FieldTransformNone, models.FieldTransformStatic,
			models.FieldTransformUppercase, models.FieldTransformLowercase, models.FieldTransformTrim:
		default:
			errx.JSON(c, errx.New(errx.BadRequest, "invalid transform: "+transform))
			return
		}
		if models.FieldTransform(transform) == models.FieldTransformStatic {
			if strings.TrimSpace(m.StaticValue) == "" {
				errx.JSON(c, errx.New(errx.BadRequest, "static mapping for "+m.ExternalField+" needs a value"))
				return
			}
		} else if strings.TrimSpace(m.WarmblyField) == "" {
			errx.JSON(c, errx.New(errx.BadRequest, "mapping for "+m.ExternalField+" needs a Warmbly field"))
			return
		}
		mappings = append(mappings, models.IntegrationFieldMapping{
			WarmblyField:  strings.TrimSpace(m.WarmblyField),
			ExternalField: strings.TrimSpace(m.ExternalField),
			Transform:     transform,
			StaticValue:   m.StaticValue,
		})
	}
	if err := h.IntegrationService.ReplaceFieldMappings(c.Request.Context(), orgID, connID, strings.TrimSpace(p.Object), mappings); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	h.auditIntegration(c, userID, models.AuditActionUpdate, connID, "field_mappings")
	rows, _ := h.IntegrationService.ListFieldMappings(c.Request.Context(), orgID, connID)
	if rows == nil {
		rows = []models.IntegrationFieldMapping{}
	}
	c.JSON(http.StatusOK, gin.H{"mappings": rows})
}

type updateConnectionConfigPayload struct {
	ConfigCapabilities map[string]any `json:"config_capabilities"`
	SyncDirection      string         `json:"sync_direction"`
}

// UpdateConnectionConfig saves a connection's onboarding/capability snapshot and
// sync direction (the "what is this integration for" settings).
func (h *Handler) UpdateConnectionConfig(c *gin.Context) {
	orgID, userID, ok := h.requireIntegrationActor(c, true)
	if !ok {
		return
	}
	connID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	var p updateConnectionConfigPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	conn, err := h.IntegrationService.UpdateConnectionConfig(c.Request.Context(), orgID, connID, p.ConfigCapabilities, strings.TrimSpace(p.SyncDirection))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	h.auditIntegration(c, userID, models.AuditActionUpdate, connID, "config")
	c.JSON(http.StatusOK, gin.H{"connection": conn})
}

// GetConnectionWebhookSecret returns (generating on first call) the HMAC signing
// secret for an automation connection, plus the scheme, so the user can verify
// our webhook signatures on their end.
func (h *Handler) GetConnectionWebhookSecret(c *gin.Context) {
	orgID, _, ok := h.requireIntegrationActor(c, true)
	if !ok {
		return
	}
	connID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	secret, err := h.IntegrationService.WebhookSigningSecret(c.Request.Context(), orgID, connID)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"signing_secret":   secret,
		"signature_header": "X-Warmbly-Signature",
		"scheme":           "HMAC-SHA256 of \"{t}.{body}\", sent as t=<unix>,v1=<hex>",
	})
}

// TestConnection fires a synthetic event through the connection's notify/webhook
// automations so the user can confirm their Zap/scenario/channel is wired.
func (h *Handler) TestConnection(c *gin.Context) {
	orgID, userID, ok := h.requireIntegrationActor(c, true)
	if !ok {
		return
	}
	connID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	sent, err := h.IntegrationService.SendTestEvent(c.Request.Context(), orgID, connID)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	h.auditIntegration(c, userID, models.AuditActionUpdate, connID, "test")
	c.JSON(http.StatusOK, gin.H{"sent": sent})
}

// --- Automations (visual flow builder) --------------------------------------

func (h *Handler) ListAutomations(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	list, err := h.IntegrationService.ListAutomations(c.Request.Context(), orgID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "list failed"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"automations": list})
}

func (h *Handler) GetAutomation(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	a, err := h.IntegrationService.GetAutomation(c.Request.Context(), orgID, id)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "lookup failed"))
		return
	}
	if a == nil {
		errx.JSON(c, errx.New(errx.NotFound, "automation not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"automation": a})
}

func (h *Handler) CreateAutomation(c *gin.Context) {
	orgID, userID, ok := h.requireIntegrationActor(c, true)
	if !ok {
		return
	}
	var w models.AutomationWrite
	if err := c.ShouldBindJSON(&w); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	a, err := h.IntegrationService.CreateAutomation(c.Request.Context(), orgID, w)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	h.auditIntegrationEntity(c, userID, models.AuditActionCreate, models.AuditEntityAutomation, a.ID, a.Name)
	h.StreamingPublisher.PublishAutomationEvent(c.Request.Context(), orgID, userID, pubsub.EventAutomationCreated, a.ID.String(), a.Name)
	c.JSON(http.StatusCreated, gin.H{"automation": a})
}

func (h *Handler) UpdateAutomation(c *gin.Context) {
	orgID, userID, ok := h.requireIntegrationActor(c, true)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	var w models.AutomationWrite
	if err := c.ShouldBindJSON(&w); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	a, err := h.IntegrationService.UpdateAutomation(c.Request.Context(), orgID, id, w)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	h.auditIntegrationEntity(c, userID, models.AuditActionUpdate, models.AuditEntityAutomation, id, a.Name)
	h.StreamingPublisher.PublishAutomationEvent(c.Request.Context(), orgID, userID, pubsub.EventAutomationUpdated, id.String(), a.Name)
	c.JSON(http.StatusOK, gin.H{"automation": a})
}

// PatchAutomationLayout persists only the canvas coordinates of automation nodes
// so a teammate's arrangement "sticks" across visits. It is written continuously
// as nodes are dragged (the live cursor/drag stream is the realtime half), so it
// is deliberately NOT audited and does NOT bump updated_at: a reposition is
// cosmetic and must not spam the audit log or read as a content change that makes
// every teammate's builder reload. Retries are naturally safe (positions are
// last-write-wins with no accumulation), so no Idempotency-Key is required.
func (h *Handler) PatchAutomationLayout(c *gin.Context) {
	orgID, _, ok := h.requireIntegrationActor(c, true)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	var w models.AutomationLayout
	if err := c.ShouldBindJSON(&w); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	if len(w.Positions) > 1000 {
		errx.JSON(c, errx.New(errx.BadRequest, "too many positions"))
		return
	}
	if err := h.IntegrationService.UpdateAutomationLayout(c.Request.Context(), orgID, id, w.Positions); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) DeleteAutomation(c *gin.Context) {
	orgID, userID, ok := h.requireIntegrationActor(c, true)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	if err := h.IntegrationService.DeleteAutomation(c.Request.Context(), orgID, id); err != nil {
		// Propagate a typed error (e.g. Conflict when the automation is still used
		// by campaign steps) with its message; anything else becomes a 500.
		errx.Handle(c, err)
		return
	}
	h.auditIntegrationEntity(c, userID, models.AuditActionDelete, models.AuditEntityAutomation, id, "")
	h.StreamingPublisher.PublishAutomationEvent(c.Request.Context(), orgID, userID, pubsub.EventAutomationDeleted, id.String(), "")
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// TestAutomation runs the automation against sample (or provided) data without
// side effects and returns the path + per-action previews (the builder "Test").
func (h *Handler) TestAutomation(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	var req models.DryRunRequest
	_ = c.ShouldBindJSON(&req) // body is optional; server builds a sample if empty
	res, derr := h.IntegrationService.DryRunAutomation(c.Request.Context(), orgID, id, req)
	if derr != nil {
		errx.JSON(c, errx.New(errx.BadRequest, derr.Error()))
		return
	}
	c.JSON(http.StatusOK, res)
}

// ListAutomationRuns returns recent run history for an automation.
func (h *Handler) ListAutomationRuns(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	limit := 50
	if l := c.Query("limit"); l != "" {
		if n, e := strconv.Atoi(l); e == nil {
			limit = n
		}
	}
	runs, rerr := h.IntegrationService.ListAutomationRuns(c.Request.Context(), orgID, id, limit)
	if rerr != nil {
		errx.JSON(c, errx.New(errx.Internal, "lookup failed"))
		return
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

// InboundAutomation runs the automation whose inbound-webhook token is in the
// URL, using the POSTed JSON body as the event payload. Public + token-gated
// (the high-entropy token is the credential); the body is capped and the run is
// dispatched in the background so the caller gets a fast 202.
func (h *Handler) InboundAutomation(c *gin.Context) {
	token := strings.TrimSpace(c.Param("token"))
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "token required"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
		return
	}
	if err := h.IntegrationService.TriggerInboundAutomation(c.Request.Context(), token, body); err != nil {
		if errors.Is(err, integration.ErrInboundAutomationNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "unknown webhook token"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "trigger failed"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"received": true})
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

	matcher := h.bookingMatcher()

	var (
		booking   *models.MeetingBooking
		lifecycle integration.MeetingLifecycle
	)
	switch provider {
	case models.IntegrationCalendly:
		booking, lifecycle, err = integration.HandleCalendlyEvent(c.Request.Context(), h.IntegrationService.Repo(), matcher, conn.OrganizationID, body)
	case models.IntegrationCalCom:
		booking, lifecycle, err = integration.HandleCalComEvent(c.Request.Context(), h.IntegrationService.Repo(), matcher, conn.OrganizationID, body)
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Ignored events (e.g. the cancel half of a reschedule) return no booking.
	if booking != nil && lifecycle != integration.LifecycleIgnore {
		h.fanoutBooking(c.Request.Context(), conn.OrganizationID, booking, lifecycle)
	}
	c.JSON(http.StatusOK, gin.H{"received": true})
}

// bookingMatcher wires the org-scoped contact lookup (by email) and id-hint
// verification the booking parsers use to attribute a meeting to a contact.
func (h *Handler) bookingMatcher() *integration.BookingMatcher {
	byEmail := func(ctx context.Context, orgID uuid.UUID, email string) (*uuid.UUID, error) {
		if h.ContactRepo == nil {
			return nil, nil
		}
		contact, xerr := h.ContactRepo.GetByEmailAndOrganization(ctx, orgID, email)
		if xerr != nil || contact == nil {
			return nil, nil
		}
		return &contact.ID, nil
	}
	verify := func(ctx context.Context, orgID, contactID uuid.UUID) (bool, error) {
		if h.ContactRepo == nil {
			return false, nil
		}
		rows, xerr := h.ContactRepo.GetByIDsAndOrganization(ctx, orgID, []uuid.UUID{contactID})
		if xerr != nil {
			return false, nil
		}
		return len(rows) > 0, nil
	}
	return integration.NewBookingMatcher(byEmail, verify)
}

// fanoutBooking dispatches the lifecycle event to customer webhooks + configured
// integration automations (Slack ping, CRM upsert), and pushes a live update to
// the lead owner's dashboard. Best-effort: a meeting is already persisted.
func (h *Handler) fanoutBooking(ctx context.Context, orgID uuid.UUID, b *models.MeetingBooking, lifecycle integration.MeetingLifecycle) {
	webhookEvent, realtimeEvent := meetingEventTypes(lifecycle)

	if h.WebhookService != nil {
		data := map[string]any{
			"source":         b.Source,
			"status":         string(b.Status),
			"invitee_email":  b.InviteeEmail,
			"invitee_name":   b.InviteeName,
			"event_name":     b.EventName,
			"scheduled_for":  b.ScheduledFor,
			"join_url":       b.JoinURL,
			"cancel_url":     b.CancelURL,
			"reschedule_url": b.RescheduleURL,
			"contact_id":     b.ContactID,
			"booking_id":     b.ID,
		}
		_, _ = h.WebhookService.Dispatch(ctx, orgID, webhookEvent, data)
	}

	// Live dashboard update, routed to the lead owner.
	h.emitMeetingRealtime(ctx, orgID, b, realtimeEvent)
}

// emitMeetingRealtime pushes a live meeting event to the lead owner so the
// Meetings page, contact timeline, and sidebar update without a refresh.
func (h *Handler) emitMeetingRealtime(ctx context.Context, orgID uuid.UUID, b *models.MeetingBooking, realtimeEvent pubsub.EventType) {
	if h.StreamingPublisher == nil || b.ContactID == nil || h.ContactRepo == nil {
		return
	}
	ownerID, oerr := h.ContactRepo.OwnerUserID(ctx, orgID, *b.ContactID)
	if oerr != nil || ownerID == nil {
		return
	}
	ev := &pubsub.MeetingEvent{
		BookingID:    b.ID.String(),
		ContactID:    b.ContactID.String(),
		InviteeEmail: b.InviteeEmail,
		EventName:    b.EventName,
		Source:       b.Source,
		State:        string(b.Status),
	}
	if b.ScheduledFor != nil {
		ev.ScheduledFor = b.ScheduledFor.Format(time.RFC3339)
	}
	h.StreamingPublisher.PublishMeeting(ctx, ownerID.String(), realtimeEvent, ev)
}

// meetingEventTypes maps a lifecycle to its (webhook, realtime) event pair.
func meetingEventTypes(l integration.MeetingLifecycle) (models.WebhookEventType, pubsub.EventType) {
	switch l {
	case integration.LifecycleRescheduled:
		return models.WebhookEventMeetingRescheduled, pubsub.EventMeetingRescheduled
	case integration.LifecycleCanceled:
		return models.WebhookEventMeetingCanceled, pubsub.EventMeetingCanceled
	default:
		return models.WebhookEventMeetingBooked, pubsub.EventMeetingBooked
	}
}

// ListMeetingBookings surfaces recent booked meetings for the integrations page.
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

// SearchMeetings is the Meetings page list: timeframe + status + text filters
// with offset pagination. Reachable by anyone who can view contacts.
func (h *Handler) SearchMeetings(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, cerr := paging.DecodeOffsetCursor(c.Query("cursor"))
	if cerr != nil {
		errx.JSON(c, cerr)
		return
	}
	filter := models.MeetingBookingFilter{
		Timeframe: strings.TrimSpace(c.Query("timeframe")),
		Status:    strings.TrimSpace(c.Query("status")),
		Search:    strings.TrimSpace(c.Query("q")),
		Limit:     limit,  // repo clamps to 1..200
		Offset:    offset, // repo clamps to >= 0
	}
	switch filter.Timeframe {
	case "", "upcoming", "past":
		// ok
	default:
		errx.JSON(c, errx.New(errx.BadRequest, "invalid timeframe"))
		return
	}
	page, err := h.IntegrationService.Repo().SearchMeetingBookings(c.Request.Context(), orgID, filter)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "search failed"))
		return
	}
	c.JSON(http.StatusOK, page)
}

// MeetingsSummary returns the counts the Meetings page header + sidebar show.
func (h *Handler) MeetingsSummary(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	summary, err := h.IntegrationService.Repo().MeetingBookingSummary(c.Request.Context(), orgID)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "summary failed"))
		return
	}
	c.JSON(http.StatusOK, summary)
}

type createMeetingPayload struct {
	Title           string `json:"title"`
	InviteeName     string `json:"invitee_name"`
	InviteeEmail    string `json:"invitee_email"`
	ScheduledFor    string `json:"scheduled_for"` // RFC3339
	DurationMinutes int    `json:"duration_minutes"`
	Location        string `json:"location"`
	JoinURL         string `json:"join_url"`
	ContactID       string `json:"contact_id"` // optional explicit link
}

// CreateMeeting creates a meeting the user schedules/logs by hand (source
// "manual"). It lives alongside the auto-captured Calendly/Cal.com bookings on
// the Meetings page + the contact timeline. The contact is attributed by an
// explicit (verified) id or by an org-scoped email match.
func (h *Handler) CreateMeeting(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var p createMeetingPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid payload"))
		return
	}
	p.InviteeName = strings.TrimSpace(p.InviteeName)
	p.InviteeEmail = strings.ToLower(strings.TrimSpace(p.InviteeEmail))
	if p.InviteeName == "" && p.InviteeEmail == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "a name or email is required"))
		return
	}
	scheduledFor, terr := time.Parse(time.RFC3339, strings.TrimSpace(p.ScheduledFor))
	if terr != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "a valid date and time is required"))
		return
	}

	contactID := h.bookingMatcher().Resolve(c.Request.Context(), orgID, p.InviteeEmail, strings.TrimSpace(p.ContactID))

	title := strings.TrimSpace(p.Title)
	if title == "" {
		title = "Call"
	}
	booking := &models.MeetingBooking{
		OrganizationID:  orgID,
		Source:          "manual",
		ExternalEventID: uuid.New().String(),
		Status:          models.MeetingBooked,
		InviteeEmail:    p.InviteeEmail,
		InviteeName:     p.InviteeName,
		EventName:       title,
		Location:        strings.TrimSpace(p.Location),
		JoinURL:         strings.TrimSpace(p.JoinURL),
		ScheduledFor:    &scheduledFor,
		ContactID:       contactID,
	}
	if p.DurationMinutes > 0 {
		end := scheduledFor.Add(time.Duration(p.DurationMinutes) * time.Minute)
		booking.EndTime = &end
	}
	if err := h.IntegrationService.Repo().UpsertMeetingBooking(c.Request.Context(), booking); err != nil {
		errx.JSON(c, errx.New(errx.Internal, "could not create meeting"))
		return
	}

	// Live update only (no webhook-automation fanout: a meeting the user logs
	// by hand shouldn't fire "a prospect booked a call" alerts back at them).
	h.emitMeetingRealtime(c.Request.Context(), orgID, booking, pubsub.EventMeetingBooked)

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityMeeting, &booking.ID, nil, map[string]string{"title": booking.EventName})

	c.JSON(http.StatusCreated, gin.H{"meeting": booking})
}

// DeleteMeeting removes a meeting booking (used for manually-created ones).
func (h *Handler) DeleteMeeting(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid id"))
		return
	}
	existing, gerr := h.IntegrationService.Repo().GetMeetingBooking(c.Request.Context(), orgID, id)
	if gerr != nil {
		errx.JSON(c, errx.New(errx.Internal, "lookup failed"))
		return
	}
	if existing == nil {
		errx.JSON(c, errx.New(errx.NotFound, "meeting not found"))
		return
	}
	if err := h.IntegrationService.Repo().DeleteMeetingBooking(c.Request.Context(), orgID, id); err != nil {
		errx.JSON(c, errx.New(errx.Internal, "delete failed"))
		return
	}
	h.emitMeetingRealtime(c.Request.Context(), orgID, existing, pubsub.EventMeetingCanceled)
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityMeeting, &id, nil, nil)
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// auditIntegration is a thin best-effort audit-log wrapper.
func (h *Handler) auditIntegration(c *gin.Context, userID uuid.UUID, action models.AuditAction, entityID uuid.UUID, detail string) {
	h.auditIntegrationEntity(c, userID, action, models.AuditEntityIntegration, entityID, detail)
}

// auditIntegrationEntity is auditIntegration with an explicit entity type, so
// automations (and other integration-adjacent surfaces) land in the audit log
// under their own filterable entity instead of a generic "integration" row.
func (h *Handler) auditIntegrationEntity(c *gin.Context, userID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID uuid.UUID, detail string) {
	if h.AuditService == nil {
		return
	}
	meta := map[string]string{}
	if detail != "" {
		meta["detail"] = detail
	}
	id := entityID
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		return
	}
	h.AuditService.LogAction(c.Request.Context(), *orgID, userID, action, entityType, &id, c.ClientIP(), c.Request.UserAgent(), nil, meta)
}
