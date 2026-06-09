package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// oauthStateTTL bounds how long a started OAuth handshake stays valid.
const oauthStateTTL = 15 * time.Minute

// ErrOAuthNotConfigured is returned when a provider's OAuth client credentials
// are not present in the environment.
var ErrOAuthNotConfigured = errors.New("oauth is not configured for this provider")

// ErrUseOAuth is returned when a caller tries to paste credentials for a
// provider that should be connected via the OAuth handshake instead.
var ErrUseOAuth = errors.New("this provider connects via OAuth; start the authorize flow instead")

// Service exposes the integration surface the dashboard and event pipeline
// talk to. Provider-specific behaviour (OAuth identity, event actions, inbound
// webhooks) lives in the per-provider files in this package.
type Service interface {
	Catalog() []models.IntegrationCatalogEntry
	ListConnections(ctx context.Context, orgID uuid.UUID) ([]models.IntegrationConnection, error)
	GetConnection(ctx context.Context, orgID, id uuid.UUID) (*models.IntegrationConnection, error)

	// Connect registers a credential-based connection (api-key / webhook-URL
	// providers). OAuth providers must use OAuthStart instead. The config map's
	// secret values are sealed with the connecting user's envelope DEK before
	// they touch the database.
	Connect(ctx context.Context, orgID, userID uuid.UUID, provider models.IntegrationProvider, label string, config map[string]any) (*models.IntegrationConnection, error)
	Disconnect(ctx context.Context, orgID, id uuid.UUID) error

	// OAuthStart returns the provider authorization URL for a one-click connect.
	OAuthStart(ctx context.Context, orgID, userID uuid.UUID, provider models.IntegrationProvider, label string) (*models.IntegrationOAuthStartResponse, error)
	// OAuthFinish completes the handshake: validates state, exchanges the code,
	// resolves the account identity, and persists encrypted tokens.
	OAuthFinish(ctx context.Context, userID uuid.UUID, code, state string) (*models.IntegrationConnection, error)
	// Reauth starts a fresh OAuth handshake for an existing connection whose
	// token expired or was revoked.
	Reauth(ctx context.Context, orgID, userID, id uuid.UUID) (*models.IntegrationOAuthStartResponse, error)

	// RotateInboundSecret regenerates the inbound URL secret (Calendly/Cal.com).
	RotateInboundSecret(ctx context.Context, orgID, id uuid.UUID, provider models.IntegrationProvider) (string, error)

	// Event subscriptions wire a Warmbly event to a provider action.
	ListEventSubscriptions(ctx context.Context, orgID, connID uuid.UUID) ([]models.IntegrationEventSubscription, error)
	CreateEventSubscription(ctx context.Context, orgID, connID uuid.UUID, eventType string, action models.IntegrationAction, config map[string]any, enabled bool) (*models.IntegrationEventSubscription, error)
	DeleteEventSubscription(ctx context.Context, orgID, id uuid.UUID) error

	// Automations: the visual flow builder. An automation is a trigger event +
	// action steps; steps persist as automation-tagged event-subscriptions and
	// run through the same dispatcher.
	ListAutomations(ctx context.Context, orgID uuid.UUID) ([]models.Automation, error)
	GetAutomation(ctx context.Context, orgID, id uuid.UUID) (*models.Automation, error)
	CreateAutomation(ctx context.Context, orgID uuid.UUID, w models.AutomationWrite) (*models.Automation, error)
	UpdateAutomation(ctx context.Context, orgID, id uuid.UUID, w models.AutomationWrite) (*models.Automation, error)
	DeleteAutomation(ctx context.Context, orgID, id uuid.UUID) error
	// RunAutomationByID executes one automation graph on demand (e.g. launched
	// from a campaign step), regardless of its configured trigger event. data is
	// the synthetic event payload used for condition + template evaluation.
	RunAutomationByID(ctx context.Context, orgID, automationID uuid.UUID, data map[string]any) error
	// DryRunAutomation walks the graph against sample/provided data WITHOUT side
	// effects, returning the path + per-action previews (the builder "Test").
	DryRunAutomation(ctx context.Context, orgID, id uuid.UUID, req models.DryRunRequest) (*models.DryRunResponse, error)
	// ListAutomationRuns returns recent run history for an automation.
	ListAutomationRuns(ctx context.Context, orgID, id uuid.UUID, limit int) ([]models.AutomationRun, error)
	// SetNativeActions wires the native CRM/contact action executor + the realtime
	// publisher post-construction (they depend on services built after this one).
	SetNativeActions(n NativeActions)
	SetPublisher(p *pubsub.StreamingPublisher)

	// ListSyncRuns returns recent observability records for a connection.
	ListSyncRuns(ctx context.Context, orgID, connID uuid.UUID, limit int) ([]models.IntegrationSyncRun, error)

	// PushContacts upserts a batch of contacts into a connected CRM on demand.
	// Used by the contextual "push to CRM" action in the dashboard. Per-record
	// results are returned so the UI can report exactly what synced.
	PushContacts(ctx context.Context, orgID, connID uuid.UUID, contacts []PushContact) (*PushResult, error)

	// Field mappings drive how Warmbly fields project onto provider fields.
	ListFieldMappings(ctx context.Context, orgID, connID uuid.UUID) ([]models.IntegrationFieldMapping, error)
	// ReplaceFieldMappings swaps the connection-default map for one object.
	ReplaceFieldMappings(ctx context.Context, orgID, connID uuid.UUID, object string, mappings []models.IntegrationFieldMapping) error
	// UpdateConnectionConfig persists the onboarding/capability snapshot + sync
	// direction for a connection.
	UpdateConnectionConfig(ctx context.Context, orgID, connID uuid.UUID, configCapabilities map[string]any, syncDirection string) (*models.IntegrationConnection, error)

	// WebhookSigningSecret returns the HMAC signing secret for an automation
	// connection's outbound webhook deliveries, generating + persisting one on
	// first request so the user can configure signature verification on their end.
	WebhookSigningSecret(ctx context.Context, orgID, connID uuid.UUID) (string, error)
	// SendTestEvent delivers a synthetic event through the connection's
	// configured notify/webhook automations so the user can verify the wiring.
	// Returns how many automations it fired.
	SendTestEvent(ctx context.Context, orgID, connID uuid.UUID) (int, error)

	// MarkSynced records a successful/failed round-trip against a connection.
	MarkSynced(ctx context.Context, id uuid.UUID, status models.IntegrationStatus, displayFields map[string]any, errMsg string) error

	// --- Google Sheets read helpers (used by the lead-sync feature) ---------
	// These expose the existing google_sheets OAuth token + Sheets client to
	// the leadsync package without leaking secret handling out of this service.

	// GoogleConnection returns the org's google_sheets OAuth connection, or nil
	// if the org has not connected Google. Even though google_sheets is hidden
	// from the integrations catalog, the underlying OAuth connection still lives
	// in integration_connections — this is how lead-sync finds it.
	GoogleConnection(ctx context.Context, orgID uuid.UUID) (*models.IntegrationConnection, error)
	// SpreadsheetMeta returns the sheet's title + tabs using the connection's
	// (refreshed) Google token.
	SpreadsheetMeta(ctx context.Context, orgID, connID uuid.UUID, sheetID string) (*SheetMeta, error)
	// SpreadsheetValues reads an A1 range from the sheet using the connection's
	// (refreshed) Google token.
	SpreadsheetValues(ctx context.Context, orgID, connID uuid.UUID, sheetID, a1Range string) ([][]string, error)

	// Dispatch fans a platform event out to every matching event subscription,
	// executing each provider action. Best-effort: action failures are recorded
	// on the connection's health but never block the caller.
	Dispatch(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data map[string]any)

	// DispatchAny is the loosely-typed adapter wired into the webhook fan-out
	// sink. It forwards only map-shaped payloads (the common event shape) to
	// Dispatch; struct payloads are ignored.
	DispatchAny(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any)

	// Repo exposes the underlying repository for the inbound webhook handlers.
	Repo() repository.IntegrationRepository
}

type service struct {
	repo      repository.IntegrationRepository
	cipher    cipher.CipherService
	oauth     *OAuthManager
	native    NativeActions
	publisher *pubsub.StreamingPublisher
}

// NewService builds the integration service. cipherSvc seals provider secrets
// with the connecting user's envelope DEK; oauth drives the OAuth handshakes.
// Native actions + realtime publisher are wired post-construction (SetNativeActions
// / SetPublisher) since they depend on services built after this one.
func NewService(repo repository.IntegrationRepository, cipherSvc cipher.CipherService, oauth *OAuthManager) Service {
	if oauth == nil {
		oauth = NewOAuthManager()
	}
	return &service{repo: repo, cipher: cipherSvc, oauth: oauth}
}

func (s *service) SetNativeActions(n NativeActions)          { s.native = n }
func (s *service) SetPublisher(p *pubsub.StreamingPublisher) { s.publisher = p }

func (s *service) Repo() repository.IntegrationRepository { return s.repo }

func (s *service) Catalog() []models.IntegrationCatalogEntry {
	entries := Catalog()
	for i := range entries {
		e := &entries[i]
		if e.AuthMethod == string(models.IntegrationAuthOAuth) {
			e.Configured = s.oauth.Configured(e.Provider)
			if len(e.Scopes) == 0 {
				e.Scopes = s.oauth.Scopes(e.Provider)
			}
		} else {
			// api-key and webhook providers are always usable.
			e.Configured = true
		}
		// Attach the configurable-action descriptor so the dashboard can render
		// the onboarding + field-mapping UI generically.
		e.Capability = models.CapabilityFor(e.Provider)
	}
	return entries
}

func (s *service) ListConnections(ctx context.Context, orgID uuid.UUID) ([]models.IntegrationConnection, error) {
	conns, err := s.repo.ListConnections(ctx, orgID)
	if err != nil {
		return nil, err
	}
	// Google Sheets is no longer a catalog integration — its OAuth connection
	// exists only to power the on-demand Lead Sync feature (see
	// internal/app/leadsync). Hide it from the Integrations page so it doesn't
	// render as an integration tile. GoogleConnection() still reaches it via the
	// repository directly.
	out := conns[:0]
	for _, c := range conns {
		if c.Provider == models.IntegrationGoogleSheets {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *service) GetConnection(ctx context.Context, orgID, id uuid.UUID) (*models.IntegrationConnection, error) {
	return s.repo.GetConnectionByID(ctx, orgID, id)
}

func (s *service) Connect(ctx context.Context, orgID, userID uuid.UUID, provider models.IntegrationProvider, label string, config map[string]any) (*models.IntegrationConnection, error) {
	if !models.IsValidIntegrationProvider(string(provider)) {
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}
	authMethod := catalogAuthMethod(provider)
	if authMethod == string(models.IntegrationAuthOAuth) {
		// Credential pasting is not allowed for OAuth providers.
		return nil, ErrUseOAuth
	}

	label = strings.TrimSpace(label)
	if label == "" {
		label = string(provider)
	}

	// SSRF guard: any user-supplied outbound URL we'll later POST to must be
	// HTTPS + publicly routable, matching the customer-webhook policy.
	if err := validateOutboundConfigURLs(config); err != nil {
		return nil, err
	}

	displayFields := buildDisplayFields(provider, config)

	var inboundSecret string
	var err error
	if provider == models.IntegrationCalendly || provider == models.IntegrationCalCom {
		inboundSecret, err = generateInboundSecret(provider)
		if err != nil {
			return nil, err
		}
	}

	configEnc, err := s.sealConfig(ctx, userID, config)
	if err != nil {
		return nil, err
	}

	status := models.IntegrationStatusPending
	switch {
	case provider == models.IntegrationCalendly || provider == models.IntegrationCalCom:
		// Inbound providers are "connected" once the URL exists.
		status = models.IntegrationStatusConnected
	case isAutomationProvider(provider):
		// Automation tools (Zapier/Make/n8n) need no credential to connect: we
		// fan events to a per-automation webhook URL, and the reverse direction
		// (the tool calling us) authenticates with a Warmbly API key created in
		// the API-keys page, not stored here. So connecting is one click.
		status = models.IntegrationStatusConnected
	case hasAnyCredential(config):
		status = models.IntegrationStatusConnected
	}

	df, _ := json.Marshal(displayFields)
	conn := &models.IntegrationConnection{
		OrganizationID:    orgID,
		Provider:          provider,
		Label:             label,
		Status:            status,
		AuthMethod:        authMethod,
		DisplayFields:     df,
		ConnectedByUserID: &userID,
		Health:            string(models.IntegrationHealthUnknown),
	}
	if status == models.IntegrationStatusConnected {
		conn.Health = string(models.IntegrationHealthHealthy)
		now := time.Now().UTC()
		conn.HealthCheckedAt = &now
	}

	if err := s.repo.UpsertConnection(ctx, &repository.ConnectionWrite{
		Conn:            conn,
		ConfigEncrypted: configEnc,
		InboundSecret:   inboundSecret,
	}); err != nil {
		return nil, err
	}

	if inboundSecret != "" {
		conn.InboundWebhookURL = BuildInboundURL(provider, inboundSecret)
	}
	return conn, nil
}

func (s *service) Disconnect(ctx context.Context, orgID, id uuid.UUID) error {
	return s.repo.DeleteConnection(ctx, orgID, id)
}

func (s *service) OAuthStart(ctx context.Context, orgID, userID uuid.UUID, provider models.IntegrationProvider, label string) (*models.IntegrationOAuthStartResponse, error) {
	if !s.oauth.Configured(provider) {
		return nil, ErrOAuthNotConfigured
	}
	state := randomURLToken(24)
	authURL, verifier, err := s.oauth.AuthCodeURL(provider, state)
	if err != nil {
		return nil, err
	}

	st := &models.IntegrationOAuthState{
		OrganizationID:  orgID,
		UserID:          userID,
		Provider:        provider,
		State:           state,
		CodeVerifier:    verifier,
		Label:           strings.TrimSpace(label),
		RequestedScopes: s.oauth.Scopes(provider),
		ExpiresAt:       time.Now().UTC().Add(oauthStateTTL),
	}
	if err := s.repo.CreateOAuthState(ctx, st); err != nil {
		return nil, err
	}
	return &models.IntegrationOAuthStartResponse{URL: authURL, State: state}, nil
}

func (s *service) OAuthFinish(ctx context.Context, userID uuid.UUID, code, state string) (*models.IntegrationConnection, error) {
	code = strings.TrimSpace(code)
	state = strings.TrimSpace(state)
	if code == "" || state == "" {
		return nil, errors.New("missing code or state")
	}

	st, err := s.repo.TakeOAuthState(ctx, state)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errors.New("invalid or expired oauth state")
	}
	if st.UserID != userID {
		return nil, errors.New("oauth state does not belong to this user")
	}

	tokens, account, err := s.oauth.Exchange(ctx, st.Provider, code, st.CodeVerifier)
	if err != nil {
		return nil, err
	}

	accessEnc, err := s.seal(ctx, userID, tokens.AccessToken)
	if err != nil {
		return nil, err
	}
	refreshEnc, err := s.seal(ctx, userID, tokens.RefreshToken)
	if err != nil {
		return nil, err
	}

	label := st.Label
	if label == "" {
		label = string(st.Provider)
	}

	display := map[string]any{}
	if account.Name != "" {
		display["account"] = account.Name
	}
	// Persist the provider API host (Salesforce per-org domain) as a non-secret
	// display field so action handlers know which host to call.
	if account.InstanceURL != "" {
		display["instance_url"] = account.InstanceURL
	}
	df, _ := json.Marshal(display)

	now := time.Now().UTC()
	conn := &models.IntegrationConnection{
		OrganizationID:      st.OrganizationID,
		Provider:            st.Provider,
		Label:               label,
		Status:              models.IntegrationStatusConnected,
		AuthMethod:          string(models.IntegrationAuthOAuth),
		DisplayFields:       df,
		ConnectedByUserID:   &userID,
		ExternalAccountID:   account.ID,
		ExternalAccountName: account.Name,
		GrantedScopes:       tokens.Scopes,
		TokenExpiresAt:      tokens.ExpiresAt,
		Health:              string(models.IntegrationHealthHealthy),
		HealthCheckedAt:     &now,
	}
	if err := s.repo.UpsertConnection(ctx, &repository.ConnectionWrite{
		Conn:            conn,
		AccessTokenEnc:  accessEnc,
		RefreshTokenEnc: refreshEnc,
	}); err != nil {
		return nil, err
	}

	// Re-read so the caller gets the canonical row (id, timestamps).
	stored, err := s.repo.GetConnection(ctx, st.OrganizationID, st.Provider, label)
	if err == nil && stored != nil {
		_ = s.repo.CreateSyncRun(ctx, &models.IntegrationSyncRun{
			ConnectionID:   stored.ID,
			OrganizationID: st.OrganizationID,
			Kind:           "oauth_connect",
			Status:         "success",
			Detail:         "authorized " + string(st.Provider),
		})
		return stored, nil
	}
	return conn, nil
}

func (s *service) Reauth(ctx context.Context, orgID, userID, id uuid.UUID) (*models.IntegrationOAuthStartResponse, error) {
	conn, err := s.repo.GetConnectionByID(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, errors.New("connection not found")
	}
	if conn.AuthMethod != string(models.IntegrationAuthOAuth) {
		return nil, ErrUseOAuth
	}
	_ = s.repo.SetConnectionStatus(ctx, id, models.IntegrationStatusAuthorizing, models.IntegrationHealthDegraded, "reauthorizing")
	return s.OAuthStart(ctx, orgID, userID, conn.Provider, conn.Label)
}

func (s *service) RotateInboundSecret(ctx context.Context, orgID, id uuid.UUID, provider models.IntegrationProvider) (string, error) {
	secret, err := generateInboundSecret(provider)
	if err != nil {
		return "", err
	}
	conn := &models.IntegrationConnection{
		ID:             id,
		OrganizationID: orgID,
		Provider:       provider,
		Status:         models.IntegrationStatusConnected,
		AuthMethod:     string(models.IntegrationAuthWebhook),
		Health:         string(models.IntegrationHealthHealthy),
	}
	if err := s.repo.UpsertConnection(ctx, &repository.ConnectionWrite{Conn: conn, InboundSecret: secret}); err != nil {
		return "", err
	}
	return BuildInboundURL(provider, secret), nil
}

func (s *service) ListEventSubscriptions(ctx context.Context, orgID, connID uuid.UUID) ([]models.IntegrationEventSubscription, error) {
	return s.repo.ListEventSubscriptions(ctx, orgID, connID)
}

func (s *service) CreateEventSubscription(ctx context.Context, orgID, connID uuid.UUID, eventType string, action models.IntegrationAction, config map[string]any, enabled bool) (*models.IntegrationEventSubscription, error) {
	conn, err := s.repo.GetConnectionByID(ctx, orgID, connID)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, errors.New("connection not found")
	}
	if !models.IsValidWebhookEventType(eventType) {
		return nil, fmt.Errorf("unknown event type: %s", eventType)
	}
	// SSRF guard for action configs that carry an outbound URL.
	if err := validateOutboundConfigURLs(config); err != nil {
		return nil, err
	}
	cfg, _ := json.Marshal(config)
	sub := &models.IntegrationEventSubscription{
		ConnectionID:   connID,
		OrganizationID: orgID,
		EventType:      eventType,
		Action:         action,
		Config:         cfg,
		Enabled:        enabled,
	}
	if err := s.repo.CreateEventSubscription(ctx, sub); err != nil {
		return nil, err
	}
	return sub, nil
}

func (s *service) DeleteEventSubscription(ctx context.Context, orgID, id uuid.UUID) error {
	return s.repo.DeleteEventSubscription(ctx, orgID, id)
}

// --- Automations ------------------------------------------------------------

func (s *service) ListAutomations(ctx context.Context, orgID uuid.UUID) ([]models.Automation, error) {
	return s.repo.ListAutomations(ctx, orgID)
}

func (s *service) GetAutomation(ctx context.Context, orgID, id uuid.UUID) (*models.Automation, error) {
	return s.repo.GetAutomation(ctx, orgID, id)
}

func (s *service) CreateAutomation(ctx context.Context, orgID uuid.UUID, w models.AutomationWrite) (*models.Automation, error) {
	a, err := s.buildAutomation(ctx, orgID, w)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateAutomation(ctx, a); err != nil {
		return nil, err
	}
	return s.repo.GetAutomation(ctx, orgID, a.ID)
}

func (s *service) UpdateAutomation(ctx context.Context, orgID, id uuid.UUID, w models.AutomationWrite) (*models.Automation, error) {
	a, err := s.buildAutomation(ctx, orgID, w)
	if err != nil {
		return nil, err
	}
	a.ID = id
	if err := s.repo.UpdateAutomation(ctx, a); err != nil {
		return nil, err
	}
	return s.repo.GetAutomation(ctx, orgID, id)
}

func (s *service) DeleteAutomation(ctx context.Context, orgID, id uuid.UUID) error {
	// Referential integrity: refuse to orphan campaign "Run automation" steps that
	// point at this automation. A blocked delete names the campaigns so the user
	// knows where to remove the step first (a typed Conflict the handler maps to 409).
	used, err := s.repo.CampaignsUsingAutomation(ctx, orgID, id)
	if err != nil {
		return err
	}
	if len(used) > 0 {
		return errx.New(errx.Conflict, automationInUseMessage(used))
	}
	return s.repo.DeleteAutomation(ctx, orgID, id)
}

// automationInUseMessage builds a human, actionable conflict message naming up to
// three referencing campaigns.
func automationInUseMessage(names []string) string {
	shown, more := names, 0
	if len(shown) > 3 {
		shown, more = shown[:3], len(shown)-3
	}
	list := strings.Join(shown, ", ")
	if more > 0 {
		list = fmt.Sprintf("%s and %d more", list, more)
	}
	noun := "campaign"
	if len(names) != 1 {
		noun = "campaigns"
	}
	return fmt.Sprintf("This automation is still used by %d %s (%s). Remove the 'Run automation' step from those campaigns before deleting it.", len(names), noun, list)
}

// buildAutomation validates a write payload (trigger event, step connections,
// SSRF on step URLs) and merges the automation-level filter into each step's
// config so the existing subscription filter logic applies to every step.
func (s *service) buildAutomation(ctx context.Context, orgID uuid.UUID, w models.AutomationWrite) (*models.Automation, error) {
	name := strings.TrimSpace(w.Name)
	if name == "" {
		name = "Automation"
	}
	trigger := strings.TrimSpace(w.TriggerEvent)
	if !models.IsValidWebhookEventType(trigger) {
		return nil, fmt.Errorf("unknown trigger event: %s", trigger)
	}

	if err := s.validateAutomationGraph(ctx, orgID, w.Graph); err != nil {
		return nil, err
	}

	filter := w.Filter
	if len(filter) == 0 {
		filter = json.RawMessage("{}")
	}
	return &models.Automation{
		OrganizationID: orgID,
		Name:           name,
		Enabled:        w.Enabled,
		TriggerEvent:   trigger,
		Filter:         filter,
		Graph:          w.Graph,
	}, nil
}

// validateAutomationGraph checks node/edge integrity: a single trigger, edges
// referencing real nodes, action nodes pointing at org-owned connections (with
// an SSRF check on any outbound URL), valid condition fields/operators, and no
// cycles. An empty graph (just being drafted) is allowed.
func (s *service) validateAutomationGraph(ctx context.Context, orgID uuid.UUID, g models.AutomationGraph) error {
	byID := make(map[string]models.AutomationNode, len(g.Nodes))
	triggers := 0
	for _, n := range g.Nodes {
		if n.ID == "" {
			return errors.New("a node is missing its id")
		}
		if _, dup := byID[n.ID]; dup {
			return fmt.Errorf("duplicate node id: %s", n.ID)
		}
		byID[n.ID] = n
		switch n.Type {
		case models.AutomationNodeTrigger:
			triggers++
		case models.AutomationNodeAction:
			if strings.TrimSpace(string(n.Action)) == "" {
				return errors.New("an action node is missing its action")
			}
			// Native (Warmbly-internal) actions run on the event's contact with no
			// external connection — validate their own config instead.
			if models.IsNativeAction(n.Action) {
				if err := validateNativeActionConfig(n.Action, n.Config); err != nil {
					return err
				}
				break
			}
			if n.ConnectionID == nil {
				return errors.New("an action node has no integration selected")
			}
			conn, err := s.repo.GetConnectionByID(ctx, orgID, *n.ConnectionID)
			if err != nil {
				return err
			}
			if conn == nil {
				return errors.New("an action node references an unknown integration")
			}
			cfg := map[string]any{}
			if len(n.Config) > 0 {
				_ = json.Unmarshal(n.Config, &cfg)
			}
			if err := validateOutboundConfigURLs(cfg); err != nil {
				return err
			}
		case models.AutomationNodeCondition:
			if n.Condition == nil {
				return errors.New("a condition node has no condition set")
			}
			if !models.ValidAutomationConditionField(n.Condition.Field) {
				return fmt.Errorf("unknown condition field: %s", n.Condition.Field)
			}
			if n.Condition.Field == models.AutoCondExpression {
				// Free-form predicate: no operator; just validate it compiles.
				if err := ValidExpression(n.Condition.Expression); err != nil {
					return fmt.Errorf("invalid condition expression: %w", err)
				}
			} else {
				if !models.ValidAutomationConditionOperator(n.Condition.Operator) {
					return fmt.Errorf("unknown condition operator: %s", n.Condition.Operator)
				}
				if n.Condition.Field == models.AutoCondField && strings.TrimSpace(n.Condition.Key) == "" {
					return errors.New("a condition is missing the field to test")
				}
			}
		default:
			return fmt.Errorf("unknown node type: %s", n.Type)
		}
	}
	if len(g.Nodes) > 0 && triggers != 1 {
		return errors.New("the flow must have exactly one trigger")
	}

	adj := map[string][]string{}
	for _, e := range g.Edges {
		src, ok := byID[e.Source]
		if !ok {
			return errors.New("an edge starts from a node that does not exist")
		}
		if _, ok := byID[e.Target]; !ok {
			return errors.New("an edge points to a node that does not exist")
		}
		if e.Source == e.Target {
			return errors.New("a node cannot connect to itself")
		}
		// Branch labels are only valid (and required) on edges out of a
		// condition node; every other edge is an unconditional "then".
		if src.Type == models.AutomationNodeCondition {
			if e.When != "true" && e.When != "false" {
				return errors.New("a condition's branches must be a yes or no path")
			}
		} else if e.When != "" {
			return errors.New("only conditions can have yes/no branches")
		}
		adj[e.Source] = append(adj[e.Source], e.Target)
	}
	if hasCycle(byID, adj) {
		return errors.New("the flow has a loop; remove the cycle")
	}
	return nil
}

// hasCycle does a DFS colour-walk over the node graph.
func hasCycle(nodes map[string]models.AutomationNode, adj map[string][]string) bool {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var visit func(id string) bool
	visit = func(id string) bool {
		color[id] = gray
		for _, m := range adj[id] {
			switch color[m] {
			case gray:
				return true
			case white:
				if visit(m) {
					return true
				}
			}
		}
		color[id] = black
		return false
	}
	for id := range nodes {
		if color[id] == white {
			if visit(id) {
				return true
			}
		}
	}
	return false
}

func (s *service) ListSyncRuns(ctx context.Context, orgID, connID uuid.UUID, limit int) ([]models.IntegrationSyncRun, error) {
	return s.repo.ListSyncRuns(ctx, orgID, connID, limit)
}

func (s *service) MarkSynced(ctx context.Context, id uuid.UUID, status models.IntegrationStatus, displayFields map[string]any, errMsg string) error {
	df, _ := json.Marshal(displayFields)
	return s.repo.MarkConnectionSynced(ctx, id, status, df, errMsg)
}

// --- Google Sheets read helpers ---------------------------------------------

// GoogleConnection returns the org's google_sheets OAuth connection (the most
// recent one), or nil when none exists. Lead-sync uses this connection's token
// to read sheets even though google_sheets is no longer surfaced as a catalog
// integration.
func (s *service) GoogleConnection(ctx context.Context, orgID uuid.UUID) (*models.IntegrationConnection, error) {
	conns, err := s.repo.ListConnections(ctx, orgID)
	if err != nil {
		return nil, err
	}
	for i := range conns {
		if conns[i].Provider == models.IntegrationGoogleSheets {
			c := conns[i]
			return &c, nil
		}
	}
	return nil, nil
}

// googleSheetsClient resolves the (refreshed) Google access token for a
// connection and returns a ready Sheets client. The connection must belong to
// orgID and be the google_sheets provider.
func (s *service) googleSheetsClient(ctx context.Context, orgID, connID uuid.UUID) (*SheetsClient, error) {
	conn, err := s.repo.GetConnectionByID(ctx, orgID, connID)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, errors.New("connection not found")
	}
	if conn.Provider != models.IntegrationGoogleSheets {
		return nil, errors.New("connection is not a google_sheets connection")
	}
	sec, err := s.repo.GetConnectionSecrets(ctx, connID)
	if err != nil {
		return nil, err
	}
	if sec == nil {
		return nil, errors.New("connection secrets not found")
	}
	token, err := s.accessTokenFor(ctx, sec)
	if err != nil {
		return nil, err
	}
	return NewSheetsClient(token), nil
}

func (s *service) SpreadsheetMeta(ctx context.Context, orgID, connID uuid.UUID, sheetID string) (*SheetMeta, error) {
	client, err := s.googleSheetsClient(ctx, orgID, connID)
	if err != nil {
		return nil, err
	}
	return client.GetSpreadsheet(ctx, sheetID)
}

func (s *service) SpreadsheetValues(ctx context.Context, orgID, connID uuid.UUID, sheetID, a1Range string) ([][]string, error) {
	client, err := s.googleSheetsClient(ctx, orgID, connID)
	if err != nil {
		return nil, err
	}
	return client.ReadValues(ctx, sheetID, a1Range)
}

// --- encryption helpers -----------------------------------------------------

func (s *service) seal(ctx context.Context, userID uuid.UUID, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if s.cipher == nil {
		return "", errors.New("cipher service unavailable")
	}
	c, err := s.cipher.Cipher(ctx, userID)
	if err != nil {
		return "", err
	}
	return c.Encrypt(ctx, plaintext)
}

func (s *service) open(ctx context.Context, userID uuid.UUID, ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if s.cipher == nil {
		return "", errors.New("cipher service unavailable")
	}
	c, err := s.cipher.Cipher(ctx, userID)
	if err != nil {
		return "", err
	}
	return c.Decrypt(ctx, ciphertext)
}

func (s *service) sealConfig(ctx context.Context, userID uuid.UUID, config map[string]any) ([]byte, error) {
	if len(config) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	b64, err := s.seal(ctx, userID, string(raw))
	if err != nil {
		return nil, err
	}
	return []byte(b64), nil
}

func (s *service) openConfig(ctx context.Context, sec *repository.ConnectionSecrets) (map[string]any, error) {
	if len(sec.ConfigEncrypted) == 0 || sec.Conn.ConnectedByUserID == nil {
		return map[string]any{}, nil
	}
	plain, err := s.open(ctx, *sec.Conn.ConnectedByUserID, string(sec.ConfigEncrypted))
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if plain == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(plain), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// accessTokenFor decrypts the connection's access token, refreshing via the
// stored refresh token when near expiry and persisting the refreshed pair.
// On an unrecoverable refresh failure it flips the connection to
// reauth_required and returns an error.
func (s *service) accessTokenFor(ctx context.Context, sec *repository.ConnectionSecrets) (string, error) {
	if sec.Conn.ConnectedByUserID == nil {
		return "", errors.New("connection has no owning user for decryption")
	}
	userID := *sec.Conn.ConnectedByUserID

	access, err := s.open(ctx, userID, sec.AccessTokenEnc)
	if err != nil {
		return "", err
	}
	refresh, err := s.open(ctx, userID, sec.RefreshTokenEnc)
	if err != nil {
		return "", err
	}

	current := models.IntegrationTokens{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    sec.Conn.TokenExpiresAt,
		Scopes:       sec.Conn.GrantedScopes,
	}
	refreshed, didRefresh, rerr := s.oauth.RefreshIfNeeded(ctx, sec.Conn.Provider, current)
	if rerr != nil {
		_ = s.repo.SetConnectionStatus(ctx, sec.Conn.ID, models.IntegrationStatusReauthRequired, models.IntegrationHealthDown, "token refresh failed: reconnect required")
		return "", rerr
	}
	if didRefresh {
		accessEnc, _ := s.seal(ctx, userID, refreshed.AccessToken)
		refreshEnc, _ := s.seal(ctx, userID, refreshed.RefreshToken)
		_ = s.repo.UpdateConnectionTokens(ctx, sec.Conn.ID, accessEnc, refreshEnc, refreshed.ExpiresAt, refreshed.Scopes)
		return refreshed.AccessToken, nil
	}
	return refreshed.AccessToken, nil
}

// --- shared helpers ---------------------------------------------------------

// validateOutboundConfigURLs enforces the SSRF/HTTPS policy on any config value
// under a url-bearing key (webhook_url / url) we will later POST to.
func validateOutboundConfigURLs(config map[string]any) error {
	for _, k := range []string{"webhook_url", "url"} {
		v, ok := config[k]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			continue
		}
		// Only the action-level "url" is templatable (rendered + re-validated at
		// dispatch by renderOutboundURL), so defer its strict check when it holds
		// a {{ template. The connection's sealed "webhook_url" is NEVER templated,
		// so it must pass full validation here — never skip it (else a {{-laced
		// host could bypass the SSRF guard).
		if k == "url" && strings.Contains(s, "{{") {
			continue
		}
		if err := webhook.ValidateOutboundURL(s); err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
	}
	return nil
}

func hasAnyCredential(config map[string]any) bool {
	for _, k := range []string{"api_token", "access_token", "webhook_url", "api_key"} {
		if v, ok := config[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return true
			}
		}
	}
	return false
}

func catalogAuthMethod(provider models.IntegrationProvider) string {
	for _, e := range Catalog() {
		if e.Provider == provider {
			return e.AuthMethod
		}
	}
	return string(models.IntegrationAuthAPIKey)
}

// isAutomationProvider reports whether a provider is a generic outbound-webhook
// automation tool (Zapier / Make / n8n).
func isAutomationProvider(p models.IntegrationProvider) bool {
	return p == models.IntegrationZapier || p == models.IntegrationMake || p == models.IntegrationN8N
}

// generateSigningSecret returns a Stripe-style `whsec_`-prefixed 32-byte hex
// HMAC key for outbound webhook signatures.
func generateSigningSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "whsec_" + hex.EncodeToString(buf), nil
}

// WebhookSigningSecret returns the connection's outbound-webhook HMAC secret,
// generating + persisting one (into the non-secret config_capabilities, matching
// how customer-webhook signing secrets are stored) on first request.
func (s *service) WebhookSigningSecret(ctx context.Context, orgID, connID uuid.UUID) (string, error) {
	conn, err := s.repo.GetConnectionByID(ctx, orgID, connID)
	if err != nil {
		return "", err
	}
	if conn == nil {
		return "", fmt.Errorf("connection not found")
	}
	cc := map[string]any{}
	if len(conn.ConfigCapabilities) > 0 {
		_ = json.Unmarshal(conn.ConfigCapabilities, &cc)
	}
	if existing, ok := cc["signing_secret"].(string); ok && existing != "" {
		return existing, nil
	}
	secret, err := generateSigningSecret()
	if err != nil {
		return "", err
	}
	cc["signing_secret"] = secret
	raw, _ := json.Marshal(cc)
	dir := conn.SyncDirection
	if dir == "" {
		dir = "push"
	}
	if err := s.repo.UpdateConnectionConfig(ctx, orgID, connID, raw, dir); err != nil {
		return "", err
	}
	return secret, nil
}

// SendTestEvent fires a synthetic event through the connection's notify/webhook
// automations (Slack, Discord, generic webhook), reusing the real delivery path
// so signing + payload shape match production. CRM-upsert automations are
// skipped so a test never writes a junk record into the customer's CRM.
func (s *service) SendTestEvent(ctx context.Context, orgID, connID uuid.UUID) (int, error) {
	conn, err := s.repo.GetConnectionByID(ctx, orgID, connID)
	if err != nil {
		return 0, err
	}
	if conn == nil {
		return 0, fmt.Errorf("connection not found")
	}
	// Ensure a signing secret first so an automation test is signed like prod.
	if isAutomationProvider(conn.Provider) {
		if _, err := s.WebhookSigningSecret(ctx, orgID, connID); err != nil {
			return 0, err
		}
	}
	subs, err := s.repo.ListEventSubscriptions(ctx, orgID, connID)
	if err != nil {
		return 0, err
	}
	sec, err := s.repo.GetConnectionSecrets(ctx, connID)
	if err != nil {
		return 0, err
	}
	if sec == nil {
		return 0, fmt.Errorf("connection not found")
	}

	sample := map[string]any{
		"test":          true,
		"event_name":    "Test event",
		"contact_email": "test@warmbly.com",
		"invitee_email": "test@warmbly.com",
		"subject":       "Warmbly test event",
		"intent":        "positive",
		"content":       "This is a test event from Warmbly.",
	}

	count := 0
	for _, sub := range subs {
		switch sub.Action {
		case models.IntegrationActionSlackNotify,
			models.IntegrationActionDiscordNotify,
			models.IntegrationActionGenericWebhookPing:
			target := repository.DispatchTarget{Subscription: sub, Secrets: *sec}
			if err := s.execAction(ctx, target, sample); err != nil {
				return count, err
			}
			count++
		}
	}
	if count == 0 {
		return 0, fmt.Errorf("add a notification or webhook automation first, then send a test")
	}
	return count, nil
}

// generateInboundSecret returns a prefixed 24-byte hex string.
func generateInboundSecret(provider models.IntegrationProvider) (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	prefix := "wmint"
	switch provider {
	case models.IntegrationCalendly:
		prefix = "calendly"
	case models.IntegrationCalCom:
		prefix = "calcom"
	}
	return prefix + "_" + hex.EncodeToString(buf), nil
}

// BuildInboundURL is exported so the routes file and handler tests can generate
// the same URL the dashboard surfaces.
func BuildInboundURL(provider models.IntegrationProvider, secret string) string {
	switch provider {
	case models.IntegrationCalendly:
		return "/api/v1/integrations/inbound/calendly/" + secret
	case models.IntegrationCalCom:
		return "/api/v1/integrations/inbound/cal-com/" + secret
	}
	return ""
}

// buildDisplayFields extracts the public, non-secret bits of the config that
// the dashboard surfaces next to a connection card.
func buildDisplayFields(provider models.IntegrationProvider, config map[string]any) map[string]any {
	df := map[string]any{}
	pick := func(keys ...string) {
		for _, k := range keys {
			if v, ok := config[k]; ok {
				df[k] = v
			}
		}
	}
	switch provider {
	case models.IntegrationCalendly, models.IntegrationCalCom:
		// scheduling_url is the user's public booking link, surfaced so the
		// contextual "Book a call" button can open it prefilled. It is opened by
		// the browser, never POSTed to, so it isn't an SSRF surface.
		pick("organization_uri", "scheduling_url")
	case models.IntegrationGoogleSheets:
		pick("sheet_id", "sheet_title")
	case models.IntegrationHubSpot, models.IntegrationSalesforce, models.IntegrationPipedrive, models.IntegrationClose:
		pick("workspace", "account_email")
	case models.IntegrationSlack:
		pick("workspace", "channel")
	case models.IntegrationDiscord:
		pick("server")
	case models.IntegrationZapier, models.IntegrationMake, models.IntegrationN8N:
		// Outbound-via-Warmbly-API providers: minimal display fields.
	}
	return df
}
