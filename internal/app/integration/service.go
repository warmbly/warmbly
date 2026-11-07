package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	emailverifyapp "github.com/warmbly/warmbly/internal/app/emailverify"
	"github.com/warmbly/warmbly/internal/pkg/emailverify"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
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
	// Catalog returns every built-in integration, ranked by popularity.
	Catalog(ctx context.Context) []models.IntegrationCatalogEntry
	ListConnections(ctx context.Context, orgID uuid.UUID) ([]models.IntegrationConnection, error)
	GetConnection(ctx context.Context, orgID, id uuid.UUID) (*models.IntegrationConnection, error)

	// Connect registers a credential-based connection (api-key / webhook-URL
	// providers). OAuth providers must use OAuthStart instead. The config map's
	// secret values are sealed with the connecting user's envelope DEK before
	// they touch the database.
	Connect(ctx context.Context, orgID, userID uuid.UUID, provider models.IntegrationProvider, label string, config map[string]any) (*models.IntegrationConnection, error)
	Disconnect(ctx context.Context, orgID, id uuid.UUID) error

	// OAuthStart returns the provider authorization URL for a one-click connect.
	// params carries provider options (Salesforce: "environment", "domain").
	OAuthStart(ctx context.Context, orgID, userID uuid.UUID, provider models.IntegrationProvider, label string, params map[string]string) (*models.IntegrationOAuthStartResponse, error)
	OAuthReturnOrigin(ctx context.Context, state string) string
	// OAuthFinish completes the handshake: validates state, exchanges the code,
	// resolves the account identity, and persists encrypted tokens. authorize
	// runs against the state's organization before the code is exchanged.
	OAuthFinish(ctx context.Context, userID uuid.UUID, code, state string, authorize func(ctx context.Context, orgID uuid.UUID) error) (*models.IntegrationConnection, error)
	// Reauth starts a fresh OAuth handshake for an existing connection whose
	// token expired or was revoked.
	Reauth(ctx context.Context, orgID, userID, id uuid.UUID) (*models.IntegrationOAuthStartResponse, error)

	// RotateInboundSecret replaces a Calendly/Cal.com connection's inbound URL
	// secret, so the old URL stops working at once, and returns the new URL.
	RotateInboundSecret(ctx context.Context, orgID, id uuid.UUID) (string, error)
	// SetInboundSigningKey stores (empty key: removes) the key Calendly/Cal.com
	// deliveries must be signed with; InboundSigningKey reads it back.
	SetInboundSigningKey(ctx context.Context, orgID, connID uuid.UUID, key string) (*models.IntegrationConnection, error)
	InboundSigningKey(ctx context.Context, conn *models.IntegrationConnection) (string, error)

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
	// UpdateAutomationLayout persists only node canvas coordinates (drag-to-stick),
	// separate from a full graph save and without bumping updated_at.
	UpdateAutomationLayout(ctx context.Context, orgID, id uuid.UUID, positions []models.AutomationNodePosition) error
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
	// TriggerInboundAutomation runs the automation whose inbound-webhook token is
	// given, using body (JSON) as the event payload. Returns
	// ErrInboundAutomationNotFound for an unknown/non-inbound token.
	TriggerInboundAutomation(ctx context.Context, token string, body []byte) error
	// SetNativeActions wires the native CRM/contact action executor + the realtime
	// publisher post-construction (they depend on services built after this one).
	SetNativeActions(n NativeActions)
	SetPublisher(p *pubsub.StreamingPublisher)
	// SetEvents wires the webhook dispatcher that announces automation.run.
	SetEvents(e EventDispatcher)
	// SetAI wires the LLM provider + credit ledger the AI automation nodes
	// (ai_step / ai_switch) use. Both may be nil (AI steps then fail with a clear
	// "not available" error); the provider is nil when no AI_PROVIDER is
	// configured.
	SetAI(p generation.Provider, c credits.CreditService)
	// SetAISearch wires the optional web-search backend an ai-mode AI switch may
	// use to look up the event's company. Nil leaves web search unavailable (the
	// node runs without it). Mirrors tasks.Service.SetAISearch.
	SetAISearch(sc generation.SearchClient)

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
	// SetConfigKey replaces one key of config_capabilities, keeping the rest.
	SetConfigKey(ctx context.Context, orgID, connID uuid.UUID, key string, value any) error

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

	// Slack app access for internal/app/slackapp. The bot token never leaves
	// this package except through SlackBotToken.
	SlackConnection(ctx context.Context, orgID uuid.UUID) (*models.IntegrationConnection, error)
	SlackConnectionsForTeam(ctx context.Context, teamID string) ([]models.IntegrationConnection, error)
	SlackBotToken(ctx context.Context, orgID, connID uuid.UUID) (string, error)
	SlackDefaultChannel(ctx context.Context, orgID uuid.UUID, conn *models.IntegrationConnection) string
	UpdateSlackSettings(ctx context.Context, orgID, connID uuid.UUID, settings models.SlackSettings) (*models.IntegrationConnection, error)
	MarkSlackTeamRevoked(ctx context.Context, teamID string, status models.IntegrationStatus, detail string) ([]uuid.UUID, error)
	SlackOAuthConfigured() bool
	SlackOAuthRedirectURL() string
	// SlackOAuthClient is the Slack app's client id and secret, for Sign in
	// with Slack.
	SlackOAuthClient() (clientID, clientSecret string)

	// VerificationProviderFor and ReportVerificationProviderError implement
	// emailverify.ProviderSource: the org's paid verification backend, if any.
	VerificationProviderFor(ctx context.Context, orgID uuid.UUID) (*emailverifyapp.Provider, error)
	ReportVerificationProviderError(ctx context.Context, connectionID uuid.UUID, err error)
	ClearVerificationProviderError(ctx context.Context, connectionID uuid.UUID)

	// Repo exposes the underlying repository for the inbound webhook handlers.
	Repo() repository.IntegrationRepository

	// AccessToken returns a usable OAuth access token for one of the org's
	// connections, refreshing it when near expiry. Used by the CRM sync engine.
	AccessToken(ctx context.Context, orgID, connID uuid.UUID) (string, *models.IntegrationConnection, error)
	// MarkConnectionHealth records a provider call's outcome on the connection.
	MarkConnectionHealth(ctx context.Context, connID uuid.UUID, status models.IntegrationStatus, health models.IntegrationHealth, detail string)
	// MergeConnectionDisplay overlays non-secret facts (a Pipedrive company
	// domain) onto a connection's display fields.
	MergeConnectionDisplay(ctx context.Context, connID uuid.UUID, patch map[string]any) error
	// SetCRMModeCheck reports which CRM a workspace runs on, so the legacy
	// upsert action of that provider steps aside.
	SetCRMModeCheck(check func(ctx context.Context, orgID uuid.UUID) models.CRMProvider)
	// ProviderAccess returns a usable OAuth access token for an org-owned
	// connection, refreshing first when force is set or the token is expiring.
	ProviderAccess(ctx context.Context, orgID, connID uuid.UUID, force bool) (*ProviderAccess, error)

	// SetCredentialCheck registers the check Connect runs on a pasted credential.
	SetCredentialCheck(provider models.IntegrationProvider, check CredentialCheck)
	// ConnectionConfig opens an org-owned connection's sealed config.
	ConnectionConfig(ctx context.Context, orgID, connID uuid.UUID) (map[string]any, *models.IntegrationConnection, error)
	// MergeConnectionConfig overlays keys onto a connection's sealed config; an
	// empty string removes a key.
	MergeConnectionConfig(ctx context.Context, orgID, connID uuid.UUID, patch map[string]any) error
	// ClaySubscriptions and SetClaySubscriptions read and replace the events a
	// Clay connection sends to its default table.
	ClaySubscriptions(ctx context.Context, orgID, connID uuid.UUID) ([]string, error)
	SetClaySubscriptions(ctx context.Context, orgID, connID uuid.UUID, events []string) error
	// ClayConnectionByInboundSecret resolves a Clay inbound URL to its connection.
	ClayConnectionByInboundSecret(ctx context.Context, secret string) (*models.IntegrationConnection, error)
	// SetSalesforce routes Salesforce pushes, upsert actions and new
	// connections through the native sync.
	SetSalesforce(b SalesforceBridge)
	// SetSlackInstallHook is told who approved a Slack install, so the member
	// who connected Slack is linked without a separate step.
	SetSlackInstallHook(h SlackInstallHook)
}

// ProviderAccess is a live credential for one connection.
type ProviderAccess struct {
	Token       string
	InstanceURL string
	Conn        *models.IntegrationConnection
}

// SalesforceBridge is the native Salesforce sync as the integration surface
// sees it. Implemented by the salesforce package, wired after construction.
type SalesforceBridge interface {
	PushContacts(ctx context.Context, orgID, connID uuid.UUID, contacts []PushContact) (*PushResult, error)
	// UpsertFromEvent finds or creates the event's person; data may carry
	// "_salesforce_fields", an automation's own projected field values.
	UpsertFromEvent(ctx context.Context, orgID, connID uuid.UUID, data map[string]any) error
	Connected(ctx context.Context, conn *models.IntegrationConnection)
}

// SlackInstallHook is implemented by the slackapp package.
type SlackInstallHook interface {
	SlackInstalled(ctx context.Context, conn *models.IntegrationConnection, slackUserID string, userID uuid.UUID)
}

// EventDispatcher delivers a platform event to customer webhooks. Satisfied by
// webhook.Service.
type EventDispatcher interface {
	Dispatch(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any) (uuid.UUID, error)
}

type service struct {
	refresh    singleflight.Group
	crmMode    func(ctx context.Context, orgID uuid.UUID) models.CRMProvider
	repo       repository.IntegrationRepository
	cipher     cipher.CipherService
	oauth      *OAuthManager
	native     NativeActions
	publisher  *pubsub.StreamingPublisher
	aiProvider generation.Provider
	credits    credits.CreditService
	aiSearch   generation.SearchClient
	salesforce SalesforceBridge
	slackHook  SlackInstallHook
	events     EventDispatcher

	checkMu sync.RWMutex
	checks  map[models.IntegrationProvider]CredentialCheck

	popMu         sync.Mutex
	pop           map[models.IntegrationProvider]int
	popAt         time.Time
	popRefreshing bool
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
func (s *service) SetEvents(e EventDispatcher)               { s.events = e }
func (s *service) SetAI(p generation.Provider, c credits.CreditService) {
	s.aiProvider = p
	s.credits = c
}
func (s *service) SetAISearch(sc generation.SearchClient) { s.aiSearch = sc }
func (s *service) SetSalesforce(b SalesforceBridge)       { s.salesforce = b }
func (s *service) SetSlackInstallHook(h SlackInstallHook) { s.slackHook = h }

func (s *service) Repo() repository.IntegrationRepository { return s.repo }

func (s *service) Catalog(ctx context.Context) []models.IntegrationCatalogEntry {
	entries := Catalog()
	rankByPopularity(entries, s.popularity(ctx))
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

	verifier := slices.Contains(models.VerificationProviders, provider)
	if verifier {
		// One verifier at a time. Which connection wins would otherwise be
		// decided by creation order alone, so connecting a second service
		// would silently move every check onto a different bill.
		if err := s.refuseSecondVerifier(ctx, orgID); err != nil {
			return nil, err
		}
		// A verification key is checked before it is stored: a mistyped key
		// would otherwise quietly leave every contact on the built-in check.
		credits, err := checkVerificationKey(ctx, provider, config)
		if err != nil {
			return nil, err
		}
		if credits != nil {
			displayFields["credits"] = *credits
		}
	}

	if check := s.credentialCheck(provider); check != nil {
		facts, err := check(ctx, config)
		if err != nil {
			return nil, err
		}
		for k, v := range facts {
			displayFields[k] = v
		}
	}

	var inboundSecret string
	var err error
	if mintsInboundURL(provider) {
		inboundSecret, err = generateInboundSecret(provider)
		if err != nil {
			return nil, err
		}
	}

	configEnc, err := s.sealConfig(ctx, orgID, config)
	if err != nil {
		return nil, err
	}

	// Asked again next to the write: validating the key above is a round trip
	// to the provider, which is long enough for a second connect to pass the
	// first check. Two that still cross leave an inert connection on the
	// Integrations page, never a lost one.
	if verifier {
		if err := s.refuseSecondVerifier(ctx, orgID); err != nil {
			return nil, err
		}
	}

	status := models.IntegrationStatusPending
	switch {
	case mintsInboundURL(provider):
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
	if conn, err := s.repo.GetConnectionByID(ctx, orgID, id); err == nil && conn != nil && conn.Provider == models.IntegrationSalesforce {
		s.revokeSalesforce(ctx, conn)
	}
	return s.repo.DeleteConnection(ctx, orgID, id)
}

// revokeSalesforce invalidates the refresh token (and every access token it
// minted) so a disconnected org stops trusting Warmbly at once. Best-effort.
func (s *service) revokeSalesforce(ctx context.Context, conn *models.IntegrationConnection) {
	sec, err := s.repo.GetConnectionSecrets(ctx, conn.ID)
	if err != nil || sec == nil {
		return
	}
	refresh, err := s.open(ctx, conn.OrganizationID, sec.RefreshTokenEnc)
	if err != nil || refresh == "" {
		return
	}
	host := configString(conn.DisplayFields, "login_host")
	if host == "" {
		host = "login.salesforce.com"
	}
	if _, herr := SalesforceLoginHost(host); herr != nil {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = s.oauth.Revoke(rctx, "https://"+host+"/services/oauth2/revoke", refresh)
}

func (s *service) OAuthStart(ctx context.Context, orgID, userID uuid.UUID, provider models.IntegrationProvider, label string, params map[string]string) (*models.IntegrationOAuthStartResponse, error) {
	if !s.oauth.Configured(provider) {
		return nil, ErrOAuthNotConfigured
	}
	stParams := map[string]string{}
	if origin := config.DashboardOriginFromContext(ctx); origin != "" {
		stParams["return_origin"] = origin
	}
	loginHost := ""
	if provider == models.IntegrationSalesforce {
		in := params["domain"]
		if strings.TrimSpace(in) == "" {
			in = params["environment"]
		}
		host, herr := SalesforceLoginHost(in)
		if herr != nil {
			return nil, errx.NewWithIdentifier(errx.BadRequest, "invalid_salesforce_domain", herr.Error())
		}
		loginHost = host
		stParams["login_host"] = host
		if strings.TrimSpace(label) == "" {
			label = "Salesforce"
			if host == "test.salesforce.com" || strings.Contains(host, ".sandbox.") {
				label = "Salesforce sandbox"
			}
		}
	}
	state := randomURLToken(24)
	authURL, verifier, err := s.oauth.AuthCodeURL(provider, state, loginHost)
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
		Params:          stParams,
		ExpiresAt:       time.Now().UTC().Add(oauthStateTTL),
	}
	if err := s.repo.CreateOAuthState(ctx, st); err != nil {
		return nil, err
	}
	return &models.IntegrationOAuthStartResponse{URL: authURL, State: state}, nil
}

func (s *service) OAuthReturnOrigin(ctx context.Context, state string) string {
	origin, err := s.repo.OAuthReturnOrigin(ctx, state)
	if err != nil {
		return ""
	}
	if origin == "" {
		return config.PrimaryDashboardOrigin()
	}
	return config.DashboardOrigin(origin)
}

func (s *service) OAuthFinish(ctx context.Context, userID uuid.UUID, code, state string, authorize func(ctx context.Context, orgID uuid.UUID) error) (*models.IntegrationConnection, error) {
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
	if authorize == nil {
		return nil, errors.New("oauth finish needs an authorization check")
	}
	if err := authorize(ctx, st.OrganizationID); err != nil {
		return nil, err
	}

	loginHost := st.Params["login_host"]
	if loginHost != "" {
		if _, herr := SalesforceLoginHost(loginHost); herr != nil {
			return nil, herr
		}
	}
	tokens, account, err := s.oauth.Exchange(ctx, st.Provider, code, st.CodeVerifier, loginHost)
	if err != nil {
		return nil, err
	}

	accessEnc, err := s.seal(ctx, st.OrganizationID, tokens.AccessToken)
	if err != nil {
		return nil, err
	}
	refreshEnc, err := s.seal(ctx, st.OrganizationID, tokens.RefreshToken)
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
	if account.UIDomain != "" {
		display["ui_domain"] = account.UIDomain
	}
	if account.APIDomain != "" {
		display["api_domain"] = account.APIDomain
	}
	if account.CompanyID != "" {
		display["company_id"] = account.CompanyID
	}
	if account.CompanyDomain != "" {
		display["company_domain"] = account.CompanyDomain
	}
	if st.Provider == models.IntegrationSalesforce {
		if loginHost == "" {
			loginHost = "login.salesforce.com"
		}
		display["login_host"] = loginHost
		display["environment"] = "production"
		if loginHost == "test.salesforce.com" || strings.Contains(loginHost, ".sandbox.") {
			display["environment"] = "sandbox"
		}
		if orgSF, userSF := salesforceIdentityIDs(account.IdentityURL); orgSF != "" {
			display["sf_org_id"] = orgSF
			display["sf_user_id"] = userSF
		}
	}
	if st.Provider == models.IntegrationSalesforce {
		// Connections are keyed by label; a login into a different org must
		// become its own connection, never swap the org under this one's
		// links and history.
		if prev, perr := s.repo.GetConnection(ctx, st.OrganizationID, st.Provider, label); perr == nil && prev != nil {
			prevOrg := configString(prev.DisplayFields, "sf_org_id")
			if prevOrg != "" && display["sf_org_id"] != nil && prevOrg != display["sf_org_id"] {
				label = fmt.Sprintf("%s (%s)", label, orFallback(account.Name, fmt.Sprint(display["sf_org_id"])))
			}
		}
	} else if account.ID != "" {
		label = s.accountLabel(ctx, st.OrganizationID, st.Provider, label, account)
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
		if st.Provider == models.IntegrationSalesforce && s.salesforce != nil {
			s.salesforce.Connected(ctx, stored)
		}
		if st.Provider == models.IntegrationSlack && s.slackHook != nil && account.InstallerID != "" {
			s.slackHook.SlackInstalled(ctx, stored, account.InstallerID, userID)
		}
		return stored, nil
	}
	return conn, nil
}

// accountLabel keeps a different account of the same provider from replacing
// an existing connection: the label falls to the first one that is free or
// already this account's.
func (s *service) accountLabel(ctx context.Context, orgID uuid.UUID, provider models.IntegrationProvider, base string, account extAccount) string {
	derived := fmt.Sprintf("%s (%s)", base, orFallback(account.Name, account.ID))
	for n := 0; n < 50; n++ {
		label := base
		switch {
		case n == 1:
			label = derived
		case n > 1:
			label = fmt.Sprintf("%s %d", derived, n)
		}
		prev, err := s.repo.GetConnection(ctx, orgID, provider, label)
		if err != nil {
			return label
		}
		if prev == nil || prev.ExternalAccountID == "" || prev.ExternalAccountID == account.ID {
			return label
		}
	}
	return fmt.Sprintf("%s (%s)", base, account.ID)
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
	var params map[string]string
	if host := configString(conn.DisplayFields, "login_host"); host != "" {
		params = map[string]string{"domain": host}
	}
	return s.OAuthStart(ctx, orgID, userID, conn.Provider, conn.Label, params)
}

func (s *service) RotateInboundSecret(ctx context.Context, orgID, id uuid.UUID) (string, error) {
	conn, err := s.repo.GetConnectionByID(ctx, orgID, id)
	if err != nil {
		return "", err
	}
	if conn == nil {
		return "", repository.ErrInboundConnectionNotFound
	}
	if !mintsInboundURL(conn.Provider) {
		return "", ErrNotInboundProvider
	}
	secret, err := generateInboundSecret(conn.Provider)
	if err != nil {
		return "", err
	}
	if err := s.repo.SetInboundSecret(ctx, orgID, id, secret); err != nil {
		return "", err
	}
	return BuildInboundURL(conn.Provider, secret), nil
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
	if !models.ProviderSupportsAction(conn.Provider, action) {
		return nil, ErrActionProviderMismatch
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
	list, err := s.repo.ListAutomations(ctx, orgID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		decorateAutomation(&list[i])
	}
	return list, nil
}

func (s *service) GetAutomation(ctx context.Context, orgID, id uuid.UUID) (*models.Automation, error) {
	a, err := s.repo.GetAutomation(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	decorateAutomation(a)
	return a, nil
}

func (s *service) CreateAutomation(ctx context.Context, orgID uuid.UUID, w models.AutomationWrite) (*models.Automation, error) {
	a, err := s.buildAutomation(ctx, orgID, w)
	if err != nil {
		return nil, err
	}
	if isInboundTrigger(a.TriggerEvent) {
		tok, terr := generateAutomationInboundToken()
		if terr != nil {
			return nil, terr
		}
		a.InboundToken = tok
	}
	if err := s.keepFedCampaignsRunning(ctx, a); err != nil {
		return nil, err
	}
	if err := s.repo.CreateAutomation(ctx, a); err != nil {
		return nil, err
	}
	return s.GetAutomation(ctx, orgID, a.ID)
}

func (s *service) UpdateAutomation(ctx context.Context, orgID, id uuid.UUID, w models.AutomationWrite) (*models.Automation, error) {
	a, err := s.buildAutomation(ctx, orgID, w)
	if err != nil {
		return nil, err
	}
	a.ID = id
	// Preserve the inbound token across edits; mint one the first time a flow
	// becomes inbound; clear it when the trigger changes away (so the old URL
	// stops firing).
	if isInboundTrigger(a.TriggerEvent) {
		existing, _ := s.repo.GetAutomation(ctx, orgID, id)
		switch {
		case existing != nil && existing.InboundToken != "":
			a.InboundToken = existing.InboundToken
		default:
			tok, terr := generateAutomationInboundToken()
			if terr != nil {
				return nil, terr
			}
			a.InboundToken = tok
		}
	}
	if err := s.keepFedCampaignsRunning(ctx, a); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateAutomation(ctx, a); err != nil {
		return nil, err
	}
	return s.GetAutomation(ctx, orgID, id)
}

// UpdateAutomationLayout persists only the canvas coordinates of the given
// nodes. Cosmetic and high-churn (written as cards are dragged), so it stays out
// of the build/validate path of a full save and never bumps updated_at.
func (s *service) UpdateAutomationLayout(ctx context.Context, orgID, id uuid.UUID, positions []models.AutomationNodePosition) error {
	return s.repo.UpdateAutomationLayout(ctx, orgID, id, positions)
}

// isInboundTrigger reports whether a trigger event is the inbound webhook.
func isInboundTrigger(trigger string) bool {
	return trigger == string(models.WebhookEventInboundWebhook)
}

// decorateAutomation fills the computed, non-stored fields on an automation
// (today: the public InboundURL derived from its token).
func decorateAutomation(a *models.Automation) {
	if a == nil {
		return
	}
	a.InboundURL = BuildInboundAutomationURL(a.InboundToken)
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
		case models.AutomationNodeStop:
			// A terminal marker: no action, no config, no outgoing edges. Accepted
			// so a path can explicitly end at a visible Stop node.
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
			if !models.ProviderSupportsAction(conn.Provider, n.Action) {
				return ErrActionProviderMismatch
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
			} else if n.Condition.Field == models.AutoCondAI {
				// Ask-AI branch: no operator; just a bounded yes/no question.
				p := strings.TrimSpace(n.Condition.Prompt)
				if p == "" {
					return errors.New("an Ask AI branch needs a question")
				}
				if len(p) > maxAIConditionPrompt {
					return fmt.Errorf("an Ask AI question is limited to %d characters", maxAIConditionPrompt)
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
		// Branch labels: a condition's edges are the yes/no paths; an action may
		// have a plain "then" edge plus an optional "on error" branch; an AI
		// classify action may additionally carry per-label branches
		// ("label:<x>", x from the node's configured labels) so the canvas
		// routes multi-way on the model's verdict; anything else is an
		// unconditional "then".
		switch {
		case src.Type == models.AutomationNodeCondition:
			if e.When != "true" && e.When != "false" {
				return errors.New("a condition's branches must be a yes or no path")
			}
		case src.Type == models.AutomationNodeAction:
			if lbl, ok := strings.CutPrefix(e.When, aiLabelEdgePrefix); ok {
				if !aiNodeRoutesByLabel(src) {
					return errors.New("only an AI switch can have per-case branches")
				}
				if !aiNodeHasBranch(src, lbl) {
					return fmt.Errorf("branch case %q is not one of the AI switch's cases", lbl)
				}
			} else if e.When != "" && e.When != "error" {
				return errors.New("an action edge must be a plain path or an on-error branch")
			}
		case e.When != "":
			return errors.New("only conditions and actions can have branch labels")
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

func (s *service) seal(ctx context.Context, orgID uuid.UUID, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if s.cipher == nil {
		return "", lowerLayer(errors.New("cipher service unavailable"))
	}
	c, err := s.cipher.Cipher(ctx, orgID)
	if err != nil {
		return "", lowerLayer(err)
	}
	out, err := c.Encrypt(ctx, plaintext)
	return out, lowerLayer(err)
}

func (s *service) open(ctx context.Context, orgID uuid.UUID, ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if s.cipher == nil {
		return "", lowerLayer(errors.New("cipher service unavailable"))
	}
	c, err := s.cipher.Cipher(ctx, orgID)
	if err != nil {
		return "", lowerLayer(err)
	}
	out, err := c.Decrypt(ctx, ciphertext)
	return out, lowerLayer(err)
}

func (s *service) sealConfig(ctx context.Context, orgID uuid.UUID, config map[string]any) ([]byte, error) {
	if len(config) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	b64, err := s.seal(ctx, orgID, string(raw))
	if err != nil {
		return nil, err
	}
	return []byte(b64), nil
}

func (s *service) openConfig(ctx context.Context, sec *repository.ConnectionSecrets) (map[string]any, error) {
	if len(sec.ConfigEncrypted) == 0 {
		return map[string]any{}, nil
	}
	plain, err := s.open(ctx, sec.Conn.OrganizationID, string(sec.ConfigEncrypted))
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
	tok, _, err := s.accessToken(ctx, sec, false)
	return tok, err
}

// accessToken is accessTokenFor that can force a refresh, and also reports the
// API host when the provider has one per org.
func (s *service) accessToken(ctx context.Context, sec *repository.ConnectionSecrets, force bool) (token, instanceURL string, err error) {
	orgID := sec.Conn.OrganizationID
	// Display fields can arrive through an import, so the host is re-checked on every read.
	instanceURL, _ = SalesforceInstanceURL(configString(sec.Conn.DisplayFields, "instance_url"))

	access, err := s.open(ctx, orgID, sec.AccessTokenEnc)
	if err != nil {
		return "", "", err
	}
	refresh, err := s.open(ctx, orgID, sec.RefreshTokenEnc)
	if err != nil {
		return "", "", err
	}

	current := models.IntegrationTokens{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    sec.Conn.TokenExpiresAt,
		Scopes:       sec.Conn.GrantedScopes,
	}
	loginHost := configString(sec.Conn.DisplayFields, "login_host")
	if loginHost != "" {
		if _, herr := SalesforceLoginHost(loginHost); herr != nil {
			loginHost = ""
		}
	}
	refreshed, didRefresh, rerr := s.oauth.RefreshIfNeeded(ctx, sec.Conn.Provider, current, force, loginHost)
	if rerr != nil {
		// Only the provider refusing the grant needs a person; a timeout or a
		// 5xx at the token endpoint is retried on the next call.
		if refreshRefused(rerr) {
			_ = s.repo.SetConnectionStatus(ctx, sec.Conn.ID, models.IntegrationStatusReauthRequired, models.IntegrationHealthDown, "token refresh failed: reconnect required")
		}
		return "", "", rerr
	}
	if didRefresh {
		accessEnc, _ := s.seal(ctx, orgID, refreshed.AccessToken)
		refreshEnc, _ := s.seal(ctx, orgID, refreshed.RefreshToken)
		_ = s.repo.UpdateConnectionTokens(ctx, sec.Conn.ID, accessEnc, refreshEnc, refreshed.ExpiresAt, refreshed.Scopes)
		// An org migration moves the instance host; the refresh is where it shows.
		if refreshed.InstanceURL != "" && refreshed.InstanceURL != instanceURL {
			instanceURL = refreshed.InstanceURL
			_ = s.repo.MergeDisplayFields(ctx, sec.Conn.ID, map[string]any{"instance_url": instanceURL})
		}
	}
	return refreshed.AccessToken, instanceURL, nil
}

func orFallback(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}

// refreshRefused reports a refresh the provider rejected outright: a revoked
// or expired grant, or a client it no longer recognises.
func refreshRefused(err error) bool {
	var re *oauth2.RetrieveError
	if !errors.As(err, &re) {
		return false
	}
	switch re.ErrorCode {
	case "invalid_grant", "invalid_client", "unauthorized_client", "invalid_scope":
		return true
	}
	return re.Response != nil && (re.Response.StatusCode == http.StatusBadRequest || re.Response.StatusCode == http.StatusUnauthorized)
}

// ProviderAccess returns a usable token for an org-owned OAuth connection.
func (s *service) ProviderAccess(ctx context.Context, orgID, connID uuid.UUID, force bool) (*ProviderAccess, error) {
	conn, err := s.repo.GetConnectionByID(ctx, orgID, connID)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, errors.New("connection not found")
	}
	if conn.Status == models.IntegrationStatusReauthRequired || conn.Status == models.IntegrationStatusDisconnected {
		return nil, ErrPushReauth
	}
	sec, err := s.repo.GetConnectionSecrets(ctx, connID)
	if err != nil {
		return nil, err
	}
	if sec == nil || sec.Conn.OrganizationID != orgID {
		return nil, errors.New("connection not found")
	}
	// Concurrent callers share one refresh: with refresh-token rotation, two
	// parallel refreshes would leave one holding a dead token.
	// Its own keyspace: AccessToken shares the group with a different result type.
	key := "provider:" + connID.String()
	if force {
		key += ":force"
	}
	v, err, _ := s.refresh.Do(key, func() (any, error) {
		tok, instance, err := s.accessToken(ctx, sec, force)
		if err != nil {
			return nil, err
		}
		return [2]string{tok, instance}, nil
	})
	if err != nil {
		if refreshRefused(err) {
			return nil, fmt.Errorf("%w: %v", ErrPushReauth, err)
		}
		return nil, fmt.Errorf("could not refresh the access token: %w", err)
	}
	pair := v.([2]string)
	return &ProviderAccess{Token: pair[0], InstanceURL: pair[1], Conn: conn}, nil
}

// AccessToken loads, refreshes and returns a connection's OAuth token after
// checking the connection belongs to orgID. Concurrent callers share one refresh.
func (s *service) AccessToken(ctx context.Context, orgID, connID uuid.UUID) (string, *models.IntegrationConnection, error) {
	v, err, _ := s.refresh.Do(connID.String(), func() (any, error) {
		sec, err := s.repo.GetConnectionSecrets(ctx, connID)
		if err != nil {
			return nil, err
		}
		if sec == nil || sec.Conn.OrganizationID != orgID {
			return nil, errors.New("connection not found")
		}
		tok, err := s.accessTokenFor(ctx, sec)
		if err != nil {
			return nil, err
		}
		conn := sec.Conn
		return tokenResult{token: tok, conn: &conn}, nil
	})
	if err != nil {
		return "", nil, err
	}
	r := v.(tokenResult)
	return r.token, r.conn, nil
}

type tokenResult struct {
	token string
	conn  *models.IntegrationConnection
}

func (s *service) SetCRMModeCheck(check func(ctx context.Context, orgID uuid.UUID) models.CRMProvider) {
	s.crmMode = check
}

func (s *service) MergeConnectionDisplay(ctx context.Context, connID uuid.UUID, patch map[string]any) error {
	return s.repo.MergeDisplayFields(ctx, connID, patch)
}

func (s *service) MarkConnectionHealth(ctx context.Context, connID uuid.UUID, status models.IntegrationStatus, health models.IntegrationHealth, detail string) {
	_ = s.repo.SetConnectionStatus(ctx, connID, status, health, detail)
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
		// so it always passes full validation here, whatever it contains.
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

// signingSecretField is the sealed-config key of an automation connection's outbound HMAC secret.
const signingSecretField = "signing_secret"

// WebhookSigningSecret returns the connection's outbound-webhook HMAC secret,
// generating one on first request. It is kept in the connection's sealed config.
func (s *service) WebhookSigningSecret(ctx context.Context, orgID, connID uuid.UUID) (string, error) {
	sec, err := s.repo.GetConnectionSecrets(ctx, connID)
	if err != nil {
		return "", err
	}
	if sec == nil || sec.Conn.OrganizationID != orgID {
		return "", fmt.Errorf("connection not found")
	}
	cfg, err := s.openConfig(ctx, sec)
	if err != nil {
		return "", err
	}
	if existing := s.signingSecretFor(ctx, sec, cfg); existing != "" {
		return existing, nil
	}
	secret, err := generateSigningSecret()
	if err != nil {
		return "", err
	}
	cfg[signingSecretField] = secret
	sealed, err := s.sealConfig(ctx, orgID, cfg)
	if err != nil {
		return "", err
	}
	if err := s.repo.SetSigningSecretConfig(ctx, orgID, connID, sealed); err != nil {
		return "", err
	}
	return secret, nil
}

// signingSecretFor reads the signing secret from the opened config, moving one
// still held in config_capabilities into it (best effort; the value is used either way).
func (s *service) signingSecretFor(ctx context.Context, sec *repository.ConnectionSecrets, cfg map[string]any) string {
	if v := stringFromMap(cfg, signingSecretField); v != "" {
		return v
	}
	legacy := configString(sec.Conn.ConfigCapabilities, models.ConfigCapabilitiesSigningSecret)
	if legacy == "" {
		return ""
	}
	next := make(map[string]any, len(cfg)+1)
	for k, v := range cfg {
		next[k] = v
	}
	next[signingSecretField] = legacy
	if sealed, err := s.sealConfig(ctx, sec.Conn.OrganizationID, next); err == nil {
		if err := s.repo.SetSigningSecretConfig(ctx, sec.Conn.OrganizationID, sec.Conn.ID, sealed); err != nil {
			log.Warn().Err(err).Str("connection_id", sec.Conn.ID.String()).Msg("integration: sealing signing secret failed")
		}
	}
	return legacy
}

// StartSigningSecretMigration seals every signing secret still held in
// config_capabilities, in one paced pass per process.
func (s *service) StartSigningSecretMigration(ctx context.Context) {
	var cursor uuid.UUID
	for {
		ids, err := s.repo.ListPlaintextSigningSecrets(ctx, cursor, 100)
		if err != nil {
			log.Warn().Err(err).Msg("integration: listing signing secrets to seal failed")
			return
		}
		if len(ids) == 0 {
			return
		}
		for _, id := range ids {
			cursor = id
			sec, err := s.repo.GetConnectionSecrets(ctx, id)
			if err != nil || sec == nil {
				continue
			}
			cfg, err := s.openConfig(ctx, sec)
			if err != nil {
				continue
			}
			_ = s.signingSecretFor(ctx, sec, cfg)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
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
			models.IntegrationActionGenericWebhookPing,
			models.IntegrationActionClaySendRow:
			target := repository.DispatchTarget{Subscription: sub, Secrets: *sec}
			if err := s.execAction(ctx, target, sample); err != nil {
				return count, err
			}
			count++
		}
	}

	// The visual automation builder never writes legacy event-subscription rows,
	// so also walk this org's enabled automations for notify/webhook action nodes
	// wired to this connection. Native + CRM-upsert actions are skipped so a test
	// never writes a junk CRM record or fires native side effects.
	autos, err := s.repo.ListAutomations(ctx, orgID)
	if err != nil {
		return count, err
	}
	for _, a := range autos {
		if !a.Enabled {
			continue
		}
		for _, n := range a.Graph.Nodes {
			if n.Type != models.AutomationNodeAction || n.ConnectionID == nil || *n.ConnectionID != connID {
				continue
			}
			switch n.Action {
			case models.IntegrationActionSlackNotify,
				models.IntegrationActionDiscordNotify,
				models.IntegrationActionGenericWebhookPing,
				models.IntegrationActionClaySendRow:
				sub := models.IntegrationEventSubscription{
					ID:             uuid.New(),
					ConnectionID:   connID,
					OrganizationID: orgID,
					EventType:      a.TriggerEvent,
					Action:         n.Action,
					Config:         n.Config,
					Enabled:        true,
					UseCase:        "automation",
				}
				target := repository.DispatchTarget{Subscription: sub, Secrets: *sec}
				if err := s.execAction(ctx, target, sample); err != nil {
					return count, err
				}
				count++
			}
		}
	}

	if count == 0 && conn.Provider == models.IntegrationClay {
		return 0, fmt.Errorf("choose at least one event to send to Clay, then send a test")
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
	case models.IntegrationClay:
		prefix = "clay"
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
	case models.IntegrationClay:
		return "/api/v1/integrations/inbound/clay/" + secret
	}
	return ""
}

// ErrInboundAutomationNotFound means no enabled inbound-webhook automation owns
// the given token (unknown, or the automation's trigger is no longer inbound).
var ErrInboundAutomationNotFound = errors.New("inbound automation not found")

// generateAutomationInboundToken returns the high-entropy secret embedded in an
// automation's inbound-webhook URL (192 bits of randomness, prefixed).
func generateAutomationInboundToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "wmauto_" + hex.EncodeToString(buf), nil
}

// BuildInboundAutomationURL is the public POST path that fires an inbound-webhook
// automation. Empty token (non-inbound automation) yields an empty URL.
func BuildInboundAutomationURL(token string) string {
	if token == "" {
		return ""
	}
	return "/api/v1/integrations/inbound/automation/" + token
}

// parseInboundPayload turns an inbound webhook body into the event data map. A
// JSON object is used as-is (minus internal underscore keys a caller must not
// set); anything else is exposed verbatim under "body" so a template/condition
// can still read it.
func parseInboundPayload(body []byte) map[string]any {
	if len(body) > 0 {
		var m map[string]any
		if json.Unmarshal(body, &m) == nil && m != nil {
			for k := range m {
				if strings.HasPrefix(k, "_") {
					delete(m, k)
				}
			}
			return m
		}
	}
	out := map[string]any{}
	if len(body) > 0 {
		out["body"] = string(body)
	}
	return out
}

// TriggerInboundAutomation resolves the automation an inbound-webhook token
// points at and runs its graph (in the background) with the POSTed body as the
// event payload. Unknown/non-inbound token -> ErrInboundAutomationNotFound; a
// disabled automation is accepted as a no-op (it must not leak that it exists).
func (s *service) TriggerInboundAutomation(ctx context.Context, token string, body []byte) error {
	a, err := s.repo.GetAutomationByInboundToken(ctx, strings.TrimSpace(token))
	if err != nil {
		return err
	}
	if a == nil || !isInboundTrigger(a.TriggerEvent) {
		return ErrInboundAutomationNotFound
	}
	if !a.Enabled {
		return nil
	}
	data := parseInboundPayload(body)
	data["trigger"] = "inbound"
	go func(au models.Automation, d map[string]any) {
		bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.executeAutomationGraph(bg, au, string(models.WebhookEventInboundWebhook), d)
	}(*a, data)
	return nil
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
	case models.IntegrationMillionVerifier, models.IntegrationCleanMyList:
		// Credits are filled in at connect time from the provider.
	case models.IntegrationApollo:
		// The account the key acts as is filled in by the credential check.
	case models.IntegrationGooglePlaces, models.IntegrationFoursquare, models.IntegrationOutscraper:
		// A key and nothing to show beyond the connection itself.
	case models.IntegrationClay:
		if raw, ok := config["webhook_url"].(string); ok && raw != "" {
			df["webhook_host"] = urlHost(raw)
		}
	}
	return df
}

func verificationClient(provider models.IntegrationProvider, key string) (emailverify.ProviderClient, error) {
	switch provider {
	case models.IntegrationMillionVerifier:
		return emailverify.NewMillionVerifier(key, ""), nil
	case models.IntegrationCleanMyList:
		return emailverify.NewCleanMyList(key, ""), nil
	default:
		return nil, fmt.Errorf("%s does not verify addresses", provider)
	}
}

// checkVerificationKey validates a pasted key without spending credits.
func checkVerificationKey(ctx context.Context, provider models.IntegrationProvider, config map[string]any) (*int, error) {
	name := ProviderLabel(provider)
	key, _ := config["api_key"].(string)
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("paste your %s API key", name)
	}
	client, err := verificationClient(provider, key)
	if err != nil {
		return nil, err
	}
	balance, err := client.Account(ctx)
	switch {
	case errors.Is(err, emailverify.ErrProviderCredits):
		// A valid empty account can connect; the built-in check covers it.
		zero := 0
		return &zero, nil
	case errors.Is(err, emailverify.ErrProviderUnconfirmed):
		return nil, fmt.Errorf("confirm your %s account email address, then connect", name)
	case errors.Is(err, emailverify.ErrProviderKey):
		return nil, fmt.Errorf("%s rejected this API key", name)
	case err != nil:
		return nil, fmt.Errorf("could not reach %s: %w", name, err)
	}
	return balance, nil
}

// refuseSecondVerifier reports the verifier already connected to the workspace,
// so connecting another one is refused rather than silently taking over.
func (s *service) refuseSecondVerifier(ctx context.Context, orgID uuid.UUID) error {
	active, err := s.activeVerificationConnection(ctx, orgID)
	if err != nil || active == nil {
		return err
	}
	return fmt.Errorf("%s already verifies this workspace's addresses. Disconnect it first", ProviderLabel(active.Provider))
}

// activeVerificationConnection returns the connection that verifies this
// workspace's addresses, or nil when none does. Connect keeps there being at
// most one, so the first match is the answer. A disconnected or
// reauth-required connection does not count.
func (s *service) activeVerificationConnection(ctx context.Context, orgID uuid.UUID) (*models.IntegrationConnection, error) {
	conns, err := s.repo.ListConnections(ctx, orgID)
	if err != nil {
		return nil, err
	}
	for _, c := range conns {
		if !slices.Contains(models.VerificationProviders, c.Provider) {
			continue
		}
		if c.Status != models.IntegrationStatusConnected && c.Status != models.IntegrationStatusDegraded {
			continue
		}
		return &c, nil
	}
	return nil, nil
}

// VerificationProviderFor returns the org's connected verification client, or
// nil when none is connected.
func (s *service) VerificationProviderFor(ctx context.Context, orgID uuid.UUID) (*emailverifyapp.Provider, error) {
	c, err := s.activeVerificationConnection(ctx, orgID)
	if err != nil || c == nil {
		return nil, err
	}
	sec, err := s.repo.GetConnectionSecrets(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	cfg, err := s.openConfig(ctx, sec)
	if err != nil {
		return nil, err
	}
	key, _ := cfg["api_key"].(string)
	if strings.TrimSpace(key) == "" {
		return nil, nil
	}
	client, err := verificationClient(c.Provider, key)
	if err != nil {
		return nil, err
	}
	id := c.ID
	return &emailverifyapp.Provider{
		Name:         string(c.Provider),
		Label:        ProviderLabel(c.Provider),
		ConnectionID: &id,
		Client:       client,
	}, nil
}

// ReportVerificationProviderError records why the provider stopped being
// usable on the connection card.
func (s *service) ReportVerificationProviderError(ctx context.Context, connectionID uuid.UUID, err error) {
	if err == nil {
		return
	}
	status, health := models.IntegrationStatusDegraded, models.IntegrationHealthDegraded
	detail := err.Error()
	switch {
	case errors.Is(err, emailverify.ErrProviderUnconfirmed):
		status, health = models.IntegrationStatusReauthRequired, models.IntegrationHealthDown
		detail = "The account email address is not confirmed yet; confirm it, then reconnect"
	case errors.Is(err, emailverify.ErrProviderKey):
		status, health = models.IntegrationStatusReauthRequired, models.IntegrationHealthDown
		detail = "The API key was rejected; reconnect with a current key"
	case errors.Is(err, emailverify.ErrProviderCredits):
		detail = "No allowance or credits left; the built-in check is used until the account is topped up"
	}
	_ = s.repo.SetConnectionStatus(ctx, connectionID, status, health, detail)
}

// ClearVerificationProviderError puts the connection card back to healthy once
// the provider answers again, since nothing else ever withdrew the error.
func (s *service) ClearVerificationProviderError(ctx context.Context, connectionID uuid.UUID) {
	_ = s.repo.ClearConnectionHealth(ctx, connectionID)
}

// slackChannelFor resolves the channel to post org notifications to. The
// OAuth connect flow doesn't capture a default channel, so we look (in order)
// at the connection's own config, then reuse whatever channel the org already
// configured for a Slack automation/event subscription. Empty when none.
func (s *service) slackChannelFor(ctx context.Context, orgID uuid.UUID, c models.IntegrationConnection) string {
	if ch := configString(c.DisplayFields, "channel"); ch != "" {
		return ch
	}
	if ch := configString(c.ConfigCapabilities, "channel"); ch != "" {
		return ch
	}
	subs, err := s.repo.ListEventSubscriptions(ctx, orgID, c.ID)
	if err != nil {
		return ""
	}
	for _, sub := range subs {
		if sub.Action == models.IntegrationActionSlackNotify {
			if ch := configString(sub.Config, "channel"); ch != "" {
				return ch
			}
		}
	}
	return ""
}
