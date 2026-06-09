package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// ConnectionWrite is the full upsert payload for an integration connection.
// Encrypted secret fields use empty-string / nil "leave unchanged" semantics
// on conflict so partial writes (e.g. rotating an inbound secret) don't wipe
// the rest of the connection.
type ConnectionWrite struct {
	Conn            *models.IntegrationConnection
	ConfigEncrypted []byte // sealed JSON config (api-key / webhook providers)
	AccessTokenEnc  string // base64 ciphertext; "" = leave unchanged
	RefreshTokenEnc string // base64 ciphertext; "" = leave unchanged
	InboundSecret   string // "" = leave unchanged
}

// ConnectionSecrets carries the encrypted secret material for a connection so
// the service can decrypt it with the connecting user's DEK. Never serialized
// to the API.
type ConnectionSecrets struct {
	Conn            models.IntegrationConnection
	ConfigEncrypted []byte
	AccessTokenEnc  string
	RefreshTokenEnc string
}

// DispatchTarget pairs an event subscription with the connection (and its
// secrets) needed to execute the action.
type DispatchTarget struct {
	Subscription models.IntegrationEventSubscription
	Secrets      ConnectionSecrets
}

// IntegrationRepository owns persistence for third-party integrations:
// connections + their encrypted secrets, OAuth handshake state, event-driven
// action subscriptions, sync-run history, and meeting bookings.
type IntegrationRepository interface {
	// Connections
	UpsertConnection(ctx context.Context, w *ConnectionWrite) error
	ListConnections(ctx context.Context, orgID uuid.UUID) ([]models.IntegrationConnection, error)
	GetConnection(ctx context.Context, orgID uuid.UUID, provider models.IntegrationProvider, label string) (*models.IntegrationConnection, error)
	GetConnectionByID(ctx context.Context, orgID, id uuid.UUID) (*models.IntegrationConnection, error)
	GetConnectionSecrets(ctx context.Context, id uuid.UUID) (*ConnectionSecrets, error)
	GetConnectionByInboundSecret(ctx context.Context, provider models.IntegrationProvider, secret string) (*models.IntegrationConnection, error)
	DeleteConnection(ctx context.Context, orgID, id uuid.UUID) error
	MarkConnectionSynced(ctx context.Context, id uuid.UUID, status models.IntegrationStatus, displayFields json.RawMessage, errMsg string) error
	UpdateConnectionTokens(ctx context.Context, id uuid.UUID, accessEnc, refreshEnc string, expiresAt *time.Time, scopes []string) error
	SetConnectionStatus(ctx context.Context, id uuid.UUID, status models.IntegrationStatus, health models.IntegrationHealth, detail string) error

	// OAuth handshake state
	CreateOAuthState(ctx context.Context, st *models.IntegrationOAuthState) error
	TakeOAuthState(ctx context.Context, state string) (*models.IntegrationOAuthState, error)

	// Event subscriptions
	CreateEventSubscription(ctx context.Context, sub *models.IntegrationEventSubscription) error
	ListEventSubscriptions(ctx context.Context, orgID, connID uuid.UUID) ([]models.IntegrationEventSubscription, error)
	DeleteEventSubscription(ctx context.Context, orgID, id uuid.UUID) error
	MatchingDispatchTargets(ctx context.Context, orgID uuid.UUID, eventType string) ([]DispatchTarget, error)

	// Automations (named groups of subscriptions sharing a trigger). Steps are
	// persisted as automation-tagged event-subscription rows, so the dispatcher
	// runs them unchanged.
	CreateAutomation(ctx context.Context, a *models.Automation) error
	ListAutomations(ctx context.Context, orgID uuid.UUID) ([]models.Automation, error)
	ListEnabledAutomationsForEvent(ctx context.Context, orgID uuid.UUID, eventType string) ([]models.Automation, error)
	GetAutomation(ctx context.Context, orgID, id uuid.UUID) (*models.Automation, error)
	UpdateAutomation(ctx context.Context, a *models.Automation) error
	DeleteAutomation(ctx context.Context, orgID, id uuid.UUID) error
	// CampaignsUsingAutomation returns the names of campaigns whose sequence has a
	// "run_automation" step pointing at this automation (so deletion can refuse to
	// orphan those steps). Capped; order is stable for a readable message.
	CampaignsUsingAutomation(ctx context.Context, orgID, automationID uuid.UUID) ([]string, error)

	// Field mappings (Warmbly field -> provider field). ListFieldMappings returns
	// every mapping for a connection; ReplaceConnectionFieldMappings swaps the
	// connection-default (unscoped) push map for one object atomically.
	ListFieldMappings(ctx context.Context, orgID, connID uuid.UUID) ([]models.IntegrationFieldMapping, error)
	ReplaceConnectionFieldMappings(ctx context.Context, orgID, connID uuid.UUID, object string, mappings []models.IntegrationFieldMapping) error
	UpdateConnectionConfig(ctx context.Context, orgID, connID uuid.UUID, configCapabilities []byte, syncDirection string) error

	// Sync runs
	CreateSyncRun(ctx context.Context, run *models.IntegrationSyncRun) error
	FinishSyncRun(ctx context.Context, id uuid.UUID, status, detail string, records int) error
	ListSyncRuns(ctx context.Context, orgID, connID uuid.UUID, limit int) ([]models.IntegrationSyncRun, error)

	CreateAutomationRun(ctx context.Context, run *models.AutomationRun) error
	FinishAutomationRun(ctx context.Context, id uuid.UUID, status, errorDetail string, nodeResults []models.AutomationNodeResult) error
	ListAutomationRuns(ctx context.Context, orgID, automationID uuid.UUID, limit int) ([]models.AutomationRun, error)

	// Bookings
	UpsertMeetingBooking(ctx context.Context, b *models.MeetingBooking) error
	ListMeetingBookings(ctx context.Context, orgID uuid.UUID, limit int) ([]models.MeetingBooking, error)
	SearchMeetingBookings(ctx context.Context, orgID uuid.UUID, f models.MeetingBookingFilter) (*models.MeetingBookingPage, error)
	MeetingBookingSummary(ctx context.Context, orgID uuid.UUID) (*models.MeetingBookingSummary, error)
	GetMeetingBooking(ctx context.Context, orgID, id uuid.UUID) (*models.MeetingBooking, error)
	DeleteMeetingBooking(ctx context.Context, orgID, id uuid.UUID) error
}

type integrationRepository struct {
	db *pgxpool.Pool
}

func NewIntegrationRepository(db *pgxpool.Pool) IntegrationRepository {
	return &integrationRepository{db: db}
}

// connectionPublicCols is the non-secret projection. Nullable TEXT columns are
// COALESCE'd so they scan into plain string fields.
const connectionPublicCols = `
	id, organization_id, provider, label, status, auth_method, display_fields,
	connected_by_user_id, COALESCE(external_account_id, ''), COALESCE(external_account_name, ''),
	granted_scopes, token_expires_at, health, health_detail, health_checked_at,
	last_synced_at, last_error, last_error_at, created_at, updated_at,
	COALESCE(config_capabilities, '{}'::jsonb), COALESCE(sync_direction, 'push')`

func (r *integrationRepository) UpsertConnection(ctx context.Context, w *ConnectionWrite) error {
	c := w.Conn
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	now := time.Now().UTC()
	c.CreatedAt = now
	c.UpdatedAt = now

	display := c.DisplayFields
	if len(display) == 0 {
		display = json.RawMessage("{}")
	}
	if c.AuthMethod == "" {
		c.AuthMethod = string(models.IntegrationAuthAPIKey)
	}
	if c.Health == "" {
		c.Health = string(models.IntegrationHealthUnknown)
	}

	_, err := r.db.Exec(ctx, `
		INSERT INTO integration_connections (
			id, organization_id, provider, label, status, auth_method,
			inbound_secret, config_encrypted, display_fields,
			connected_by_user_id, access_token_encrypted, refresh_token_encrypted,
			token_expires_at, granted_scopes, external_account_id, external_account_name,
			health, health_detail, health_checked_at,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9,
			$10, $11, $12,
			$13, $14, $15, $16,
			$17, $18, $19,
			$20, $20
		)
		ON CONFLICT (organization_id, provider, label) DO UPDATE SET
			status = EXCLUDED.status,
			auth_method = EXCLUDED.auth_method,
			inbound_secret = COALESCE(EXCLUDED.inbound_secret, integration_connections.inbound_secret),
			config_encrypted = COALESCE(EXCLUDED.config_encrypted, integration_connections.config_encrypted),
			display_fields = EXCLUDED.display_fields,
			connected_by_user_id = COALESCE(EXCLUDED.connected_by_user_id, integration_connections.connected_by_user_id),
			access_token_encrypted = COALESCE(EXCLUDED.access_token_encrypted, integration_connections.access_token_encrypted),
			refresh_token_encrypted = COALESCE(EXCLUDED.refresh_token_encrypted, integration_connections.refresh_token_encrypted),
			token_expires_at = COALESCE(EXCLUDED.token_expires_at, integration_connections.token_expires_at),
			granted_scopes = EXCLUDED.granted_scopes,
			external_account_id = COALESCE(EXCLUDED.external_account_id, integration_connections.external_account_id),
			external_account_name = COALESCE(EXCLUDED.external_account_name, integration_connections.external_account_name),
			health = EXCLUDED.health,
			health_detail = EXCLUDED.health_detail,
			health_checked_at = EXCLUDED.health_checked_at,
			updated_at = EXCLUDED.updated_at
	`,
		c.ID, c.OrganizationID, string(c.Provider), c.Label, string(c.Status), c.AuthMethod,
		nullIfEmptyStr(w.InboundSecret), nullIfEmptyBytes(w.ConfigEncrypted), display,
		c.ConnectedByUserID, nullIfEmptyStr(w.AccessTokenEnc), nullIfEmptyStr(w.RefreshTokenEnc),
		c.TokenExpiresAt, normalizeScopes(c.GrantedScopes), nullIfEmptyStr(c.ExternalAccountID), nullIfEmptyStr(c.ExternalAccountName),
		c.Health, c.HealthDetail, c.HealthCheckedAt,
		now,
	)
	return err
}

func nullIfEmptyStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIfEmptyBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func normalizeScopes(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (r *integrationRepository) ListConnections(ctx context.Context, orgID uuid.UUID) ([]models.IntegrationConnection, error) {
	rows, err := r.db.Query(ctx, `SELECT `+connectionPublicCols+`
		FROM integration_connections WHERE organization_id = $1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.IntegrationConnection{}
	for rows.Next() {
		var c models.IntegrationConnection
		if err := scanConnectionInto(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *integrationRepository) GetConnection(ctx context.Context, orgID uuid.UUID, provider models.IntegrationProvider, label string) (*models.IntegrationConnection, error) {
	row := r.db.QueryRow(ctx, `SELECT `+connectionPublicCols+`
		FROM integration_connections WHERE organization_id = $1 AND provider = $2 AND label = $3`,
		orgID, string(provider), label)
	var c models.IntegrationConnection
	if err := scanConnectionInto(row, &c); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *integrationRepository) GetConnectionByID(ctx context.Context, orgID, id uuid.UUID) (*models.IntegrationConnection, error) {
	row := r.db.QueryRow(ctx, `SELECT `+connectionPublicCols+`
		FROM integration_connections WHERE organization_id = $1 AND id = $2`, orgID, id)
	var c models.IntegrationConnection
	if err := scanConnectionInto(row, &c); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *integrationRepository) GetConnectionSecrets(ctx context.Context, id uuid.UUID) (*ConnectionSecrets, error) {
	row := r.db.QueryRow(ctx, `
		SELECT `+connectionPublicCols+`,
		       config_encrypted, COALESCE(access_token_encrypted, ''), COALESCE(refresh_token_encrypted, '')
		FROM integration_connections WHERE id = $1`, id)
	var sec ConnectionSecrets
	if err := scanConnectionSecretsInto(row, &sec); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &sec, nil
}

func (r *integrationRepository) GetConnectionByInboundSecret(ctx context.Context, provider models.IntegrationProvider, secret string) (*models.IntegrationConnection, error) {
	if secret == "" {
		return nil, nil
	}
	row := r.db.QueryRow(ctx, `SELECT `+connectionPublicCols+`
		FROM integration_connections WHERE provider = $1 AND inbound_secret = $2 LIMIT 1`,
		string(provider), secret)
	var c models.IntegrationConnection
	if err := scanConnectionInto(row, &c); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *integrationRepository) DeleteConnection(ctx context.Context, orgID, id uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM integration_connections WHERE organization_id = $1 AND id = $2`, orgID, id)
	return err
}

func (r *integrationRepository) MarkConnectionSynced(ctx context.Context, id uuid.UUID, status models.IntegrationStatus, displayFields json.RawMessage, errMsg string) error {
	now := time.Now().UTC()
	if len(displayFields) == 0 {
		displayFields = json.RawMessage("{}")
	}
	if errMsg == "" {
		_, err := r.db.Exec(ctx, `
			UPDATE integration_connections
			SET status = $1, display_fields = $2, last_synced_at = $3,
			    health = 'healthy', health_detail = NULL, health_checked_at = $3,
			    last_error = NULL, last_error_at = NULL, updated_at = $3
			WHERE id = $4`, string(status), displayFields, now, id)
		return err
	}
	_, err := r.db.Exec(ctx, `
		UPDATE integration_connections
		SET status = $1, display_fields = $2,
		    health = 'degraded', health_detail = $3, health_checked_at = $4,
		    last_error = $3, last_error_at = $4, updated_at = $4
		WHERE id = $5`, string(status), displayFields, errMsg, now, id)
	return err
}

func (r *integrationRepository) UpdateConnectionTokens(ctx context.Context, id uuid.UUID, accessEnc, refreshEnc string, expiresAt *time.Time, scopes []string) error {
	now := time.Now().UTC()
	_, err := r.db.Exec(ctx, `
		UPDATE integration_connections
		SET access_token_encrypted = COALESCE($1, access_token_encrypted),
		    refresh_token_encrypted = COALESCE($2, refresh_token_encrypted),
		    token_expires_at = $3,
		    granted_scopes = CASE WHEN cardinality($4::text[]) > 0 THEN $4 ELSE granted_scopes END,
		    updated_at = $5
		WHERE id = $6`,
		nullIfEmptyStr(accessEnc), nullIfEmptyStr(refreshEnc), expiresAt, normalizeScopes(scopes), now, id)
	return err
}

func (r *integrationRepository) SetConnectionStatus(ctx context.Context, id uuid.UUID, status models.IntegrationStatus, health models.IntegrationHealth, detail string) error {
	now := time.Now().UTC()
	_, err := r.db.Exec(ctx, `
		UPDATE integration_connections
		SET status = $1, health = $2, health_detail = NULLIF($3, ''), health_checked_at = $4, updated_at = $4
		WHERE id = $5`, string(status), string(health), detail, now, id)
	return err
}

// --- OAuth state ------------------------------------------------------------

func (r *integrationRepository) CreateOAuthState(ctx context.Context, st *models.IntegrationOAuthState) error {
	if st.ID == uuid.Nil {
		st.ID = uuid.New()
	}
	st.CreatedAt = time.Now().UTC()
	_, err := r.db.Exec(ctx, `
		INSERT INTO integration_oauth_states (
			id, organization_id, user_id, provider, state, code_verifier,
			label, requested_scopes, expires_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		st.ID, st.OrganizationID, st.UserID, string(st.Provider), st.State, st.CodeVerifier,
		st.Label, normalizeScopes(st.RequestedScopes), st.ExpiresAt, st.CreatedAt)
	return err
}

// TakeOAuthState atomically consumes a state: it returns the row only if it is
// unused and unexpired, marking it used in the same statement so a replayed
// callback can't be exchanged twice.
func (r *integrationRepository) TakeOAuthState(ctx context.Context, state string) (*models.IntegrationOAuthState, error) {
	row := r.db.QueryRow(ctx, `
		UPDATE integration_oauth_states
		SET used_at = NOW()
		WHERE state = $1 AND used_at IS NULL AND expires_at > NOW()
		RETURNING id, organization_id, user_id, provider, state, code_verifier,
		          label, requested_scopes, used_at, expires_at, created_at`, state)
	var st models.IntegrationOAuthState
	var provider string
	err := row.Scan(&st.ID, &st.OrganizationID, &st.UserID, &provider, &st.State, &st.CodeVerifier,
		&st.Label, &st.RequestedScopes, &st.UsedAt, &st.ExpiresAt, &st.CreatedAt)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st.Provider = models.IntegrationProvider(provider)
	return &st, nil
}

// --- Event subscriptions ----------------------------------------------------

func (r *integrationRepository) CreateEventSubscription(ctx context.Context, sub *models.IntegrationEventSubscription) error {
	if sub.ID == uuid.Nil {
		sub.ID = uuid.New()
	}
	now := time.Now().UTC()
	sub.CreatedAt = now
	sub.UpdatedAt = now
	cfg := sub.Config
	if len(cfg) == 0 {
		cfg = json.RawMessage("{}")
	}
	useCase := sub.UseCase
	if useCase == "" {
		useCase = "custom"
	}
	// Plain insert (no ON CONFLICT): the framework allows several differently
	// filtered automations per (connection, event, action), so each is a row.
	_, err := r.db.Exec(ctx, `
		INSERT INTO integration_event_subscriptions (
			id, connection_id, organization_id, event_type, action, config, enabled, use_case, automation_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)`,
		sub.ID, sub.ConnectionID, sub.OrganizationID, sub.EventType, string(sub.Action), cfg, sub.Enabled, useCase, sub.AutomationID, now)
	sub.UseCase = useCase
	return err
}

func (r *integrationRepository) ListEventSubscriptions(ctx context.Context, orgID, connID uuid.UUID) ([]models.IntegrationEventSubscription, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, connection_id, organization_id, event_type, action, config, enabled, created_at, updated_at, use_case
		FROM integration_event_subscriptions
		WHERE organization_id = $1 AND connection_id = $2 ORDER BY created_at DESC`, orgID, connID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.IntegrationEventSubscription{}
	for rows.Next() {
		s, err := scanEventSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (r *integrationRepository) DeleteEventSubscription(ctx context.Context, orgID, id uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM integration_event_subscriptions WHERE organization_id = $1 AND id = $2`, orgID, id)
	return err
}

// --- Automations ------------------------------------------------------------

// insertAutomationSteps writes one event-subscription row per step, tagged with
// the automation id + its trigger event + enabled flag (so the dispatcher runs
// them). Caller has already merged the automation filter into each step Config.
// automationCols is the shared projection for an automation row.
const automationCols = `id, organization_id, name, enabled, trigger_event, filter, graph, created_at, updated_at`

func scanAutomation(row pgx.Row, a *models.Automation) error {
	var graph []byte
	if err := row.Scan(&a.ID, &a.OrganizationID, &a.Name, &a.Enabled, &a.TriggerEvent, &a.Filter, &graph, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return err
	}
	a.Graph = models.AutomationGraph{Nodes: []models.AutomationNode{}, Edges: []models.AutomationEdge{}}
	if len(graph) > 0 {
		_ = json.Unmarshal(graph, &a.Graph)
	}
	if a.Graph.Nodes == nil {
		a.Graph.Nodes = []models.AutomationNode{}
	}
	if a.Graph.Edges == nil {
		a.Graph.Edges = []models.AutomationEdge{}
	}
	return nil
}

func (r *integrationRepository) CreateAutomation(ctx context.Context, a *models.Automation) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	now := time.Now().UTC()
	a.CreatedAt = now
	a.UpdatedAt = now
	filter := a.Filter
	if len(filter) == 0 {
		filter = json.RawMessage("{}")
	}
	graph, _ := json.Marshal(a.Graph)
	_, err := r.db.Exec(ctx, `
		INSERT INTO automations (id, organization_id, name, enabled, trigger_event, filter, graph, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`,
		a.ID, a.OrganizationID, a.Name, a.Enabled, a.TriggerEvent, filter, graph, now)
	return err
}

func (r *integrationRepository) UpdateAutomation(ctx context.Context, a *models.Automation) error {
	now := time.Now().UTC()
	a.UpdatedAt = now
	filter := a.Filter
	if len(filter) == 0 {
		filter = json.RawMessage("{}")
	}
	graph, _ := json.Marshal(a.Graph)
	tag, err := r.db.Exec(ctx, `
		UPDATE automations SET name = $3, enabled = $4, trigger_event = $5, filter = $6, graph = $7, updated_at = $8
		WHERE id = $1 AND organization_id = $2`,
		a.ID, a.OrganizationID, a.Name, a.Enabled, a.TriggerEvent, filter, graph, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("automation not found")
	}
	return nil
}

func (r *integrationRepository) DeleteAutomation(ctx context.Context, orgID, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM automations WHERE id = $1 AND organization_id = $2`, id, orgID)
	return err
}

func (r *integrationRepository) CampaignsUsingAutomation(ctx context.Context, orgID, automationID uuid.UUID) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT c.name
		FROM sequences s
		JOIN campaigns c ON c.id = s.campaign_id
		WHERE c.organization_id = $1
		  AND s.action->>'type' = 'run_automation'
		  AND s.action->>'automation_id' = $2::text
		ORDER BY c.name
		LIMIT 20`, orgID, automationID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

func (r *integrationRepository) GetAutomation(ctx context.Context, orgID, id uuid.UUID) (*models.Automation, error) {
	var a models.Automation
	row := r.db.QueryRow(ctx, `SELECT `+automationCols+` FROM automations WHERE id = $1 AND organization_id = $2`, id, orgID)
	if err := scanAutomation(row, &a); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (r *integrationRepository) ListAutomations(ctx context.Context, orgID uuid.UUID) ([]models.Automation, error) {
	rows, err := r.db.Query(ctx, `SELECT `+automationCols+` FROM automations WHERE organization_id = $1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Automation{}
	for rows.Next() {
		var a models.Automation
		if err := scanAutomation(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListEnabledAutomationsForEvent returns the enabled automations whose trigger
// matches the fired event, for the graph executor to walk. The 'campaign.action'
// trigger is excluded: those are manual / campaign-launched automations that
// only ever run via RunAutomationByID, never by a real event fan-out.
func (r *integrationRepository) ListEnabledAutomationsForEvent(ctx context.Context, orgID uuid.UUID, eventType string) ([]models.Automation, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+automationCols+` FROM automations WHERE organization_id = $1 AND trigger_event = $2 AND enabled AND trigger_event <> 'campaign.action'`,
		orgID, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Automation{}
	for rows.Next() {
		var a models.Automation
		if err := scanAutomation(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// --- Field mappings ---------------------------------------------------------

const fieldMappingCols = `id, connection_id, organization_id, subscription_id, direction, object_name, warmbly_field, external_field, transform, static_value, is_default, created_at`

func (r *integrationRepository) ListFieldMappings(ctx context.Context, orgID, connID uuid.UUID) ([]models.IntegrationFieldMapping, error) {
	rows, err := r.db.Query(ctx, `SELECT `+fieldMappingCols+`
		FROM integration_field_mappings WHERE organization_id = $1 AND connection_id = $2
		ORDER BY created_at`, orgID, connID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.IntegrationFieldMapping{}
	for rows.Next() {
		var m models.IntegrationFieldMapping
		if err := rows.Scan(&m.ID, &m.ConnectionID, &m.OrganizationID, &m.SubscriptionID, &m.Direction,
			&m.ObjectName, &m.WarmblyField, &m.ExternalField, &m.Transform, &m.StaticValue, &m.IsDefault, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ReplaceConnectionFieldMappings atomically swaps the connection-default
// (subscription_id NULL, direction push) mappings for one object with the
// provided set. Per-automation scoped mappings are left untouched.
func (r *integrationRepository) ReplaceConnectionFieldMappings(ctx context.Context, orgID, connID uuid.UUID, object string, mappings []models.IntegrationFieldMapping) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		DELETE FROM integration_field_mappings
		WHERE organization_id = $1 AND connection_id = $2 AND object_name = $3
		  AND subscription_id IS NULL AND direction = 'push'`, orgID, connID, object); err != nil {
		return err
	}
	// Dedupe by destination field (last wins) so a repeated external_field can't
	// trip the per-destination unique index mid-transaction.
	seen := make(map[string]struct{}, len(mappings))
	for i := len(mappings) - 1; i >= 0; i-- {
		if _, dup := seen[mappings[i].ExternalField]; dup {
			mappings = append(mappings[:i], mappings[i+1:]...)
			continue
		}
		seen[mappings[i].ExternalField] = struct{}{}
	}
	for _, m := range mappings {
		transform := m.Transform
		if transform == "" {
			transform = "none"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO integration_field_mappings
				(id, connection_id, organization_id, subscription_id, direction, object_name,
				 warmbly_field, external_field, transform, static_value, is_default, created_at)
			VALUES (gen_random_uuid(), $1, $2, NULL, 'push', $3, $4, $5, $6, $7, true, now())`,
			connID, orgID, object, m.WarmblyField, m.ExternalField, transform, m.StaticValue); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *integrationRepository) UpdateConnectionConfig(ctx context.Context, orgID, connID uuid.UUID, configCapabilities []byte, syncDirection string) error {
	if len(configCapabilities) == 0 {
		configCapabilities = []byte("{}")
	}
	if syncDirection == "" {
		syncDirection = "push"
	}
	_, err := r.db.Exec(ctx, `
		UPDATE integration_connections
		SET config_capabilities = $1, sync_direction = $2, updated_at = now()
		WHERE organization_id = $3 AND id = $4`, configCapabilities, syncDirection, orgID, connID)
	return err
}

// MatchingDispatchTargets returns enabled subscriptions for an org+event whose
// connection is usable, each hydrated with the connection's encrypted secrets.
// Dispatch volume is per-event and low, so the secrets fetch is done per row.
func (r *integrationRepository) MatchingDispatchTargets(ctx context.Context, orgID uuid.UUID, eventType string) ([]DispatchTarget, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.id, s.connection_id, s.organization_id, s.event_type, s.action, s.config, s.enabled, s.created_at, s.updated_at, s.use_case
		FROM integration_event_subscriptions s
		JOIN integration_connections c ON c.id = s.connection_id
		WHERE s.organization_id = $1 AND s.event_type = $2 AND s.enabled
		  AND s.automation_id IS NULL
		  AND c.status IN ('connected', 'degraded')`, orgID, eventType)
	if err != nil {
		return nil, err
	}
	var subs []models.IntegrationEventSubscription
	for rows.Next() {
		s, err := scanEventSubscription(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		subs = append(subs, *s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]DispatchTarget, 0, len(subs))
	for _, sub := range subs {
		sec, err := r.GetConnectionSecrets(ctx, sub.ConnectionID)
		if err != nil {
			return nil, err
		}
		if sec == nil {
			continue
		}
		out = append(out, DispatchTarget{Subscription: sub, Secrets: *sec})
	}
	return out, nil
}

// --- Sync runs --------------------------------------------------------------

func (r *integrationRepository) CreateSyncRun(ctx context.Context, run *models.IntegrationSyncRun) error {
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	run.StartedAt = time.Now().UTC()
	if run.Status == "" {
		run.Status = "running"
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO integration_sync_runs (id, connection_id, organization_id, kind, status, detail, records_processed, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		run.ID, run.ConnectionID, run.OrganizationID, run.Kind, run.Status, run.Detail, run.RecordsProcessed, run.StartedAt)
	return err
}

func (r *integrationRepository) FinishSyncRun(ctx context.Context, id uuid.UUID, status, detail string, records int) error {
	_, err := r.db.Exec(ctx, `
		UPDATE integration_sync_runs
		SET status = $1, detail = $2, records_processed = $3, finished_at = NOW()
		WHERE id = $4`, status, detail, records, id)
	return err
}

func (r *integrationRepository) ListSyncRuns(ctx context.Context, orgID, connID uuid.UUID, limit int) ([]models.IntegrationSyncRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, connection_id, organization_id, kind, status, detail, records_processed, started_at, finished_at
		FROM integration_sync_runs
		WHERE organization_id = $1 AND connection_id = $2 ORDER BY started_at DESC LIMIT $3`, orgID, connID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.IntegrationSyncRun{}
	for rows.Next() {
		var run models.IntegrationSyncRun
		if err := rows.Scan(&run.ID, &run.ConnectionID, &run.OrganizationID, &run.Kind, &run.Status,
			&run.Detail, &run.RecordsProcessed, &run.StartedAt, &run.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (r *integrationRepository) CreateAutomationRun(ctx context.Context, run *models.AutomationRun) error {
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	run.StartedAt = time.Now().UTC()
	if run.Status == "" {
		run.Status = "running"
	}
	results, _ := json.Marshal(run.NodeResults)
	if len(results) == 0 {
		results = []byte("[]")
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO automation_runs (id, automation_id, organization_id, trigger_event, status, node_results, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		run.ID, run.AutomationID, run.OrganizationID, run.TriggerEvent, run.Status, results, run.StartedAt)
	return err
}

func (r *integrationRepository) FinishAutomationRun(ctx context.Context, id uuid.UUID, status, errorDetail string, nodeResults []models.AutomationNodeResult) error {
	results, _ := json.Marshal(nodeResults)
	if len(results) == 0 {
		results = []byte("[]")
	}
	_, err := r.db.Exec(ctx, `
		UPDATE automation_runs
		SET status = $1, error_detail = $2, node_results = $3, finished_at = NOW()
		WHERE id = $4`, status, errorDetail, results, id)
	return err
}

func (r *integrationRepository) ListAutomationRuns(ctx context.Context, orgID, automationID uuid.UUID, limit int) ([]models.AutomationRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, automation_id, organization_id, trigger_event, status, node_results, error_detail, started_at, finished_at
		FROM automation_runs
		WHERE organization_id = $1 AND automation_id = $2 ORDER BY started_at DESC LIMIT $3`, orgID, automationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.AutomationRun{}
	for rows.Next() {
		var run models.AutomationRun
		var results []byte
		if err := rows.Scan(&run.ID, &run.AutomationID, &run.OrganizationID, &run.TriggerEvent, &run.Status,
			&results, &run.ErrorDetail, &run.StartedAt, &run.FinishedAt); err != nil {
			return nil, err
		}
		if len(results) > 0 {
			_ = json.Unmarshal(results, &run.NodeResults)
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// --- scanning helpers -------------------------------------------------------

type scanner interface {
	Scan(dest ...any) error
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows)
}

func scanConnectionInto(row scanner, c *models.IntegrationConnection) error {
	var provider, status string
	if err := row.Scan(
		&c.ID, &c.OrganizationID, &provider, &c.Label, &status, &c.AuthMethod, &c.DisplayFields,
		&c.ConnectedByUserID, &c.ExternalAccountID, &c.ExternalAccountName,
		&c.GrantedScopes, &c.TokenExpiresAt, &c.Health, &c.HealthDetail, &c.HealthCheckedAt,
		&c.LastSyncedAt, &c.LastError, &c.LastErrorAt, &c.CreatedAt, &c.UpdatedAt,
		&c.ConfigCapabilities, &c.SyncDirection,
	); err != nil {
		return err
	}
	c.Provider = models.IntegrationProvider(provider)
	c.Status = models.IntegrationStatus(status)
	if len(c.DisplayFields) == 0 {
		c.DisplayFields = json.RawMessage("{}")
	}
	if len(c.ConfigCapabilities) == 0 {
		c.ConfigCapabilities = json.RawMessage("{}")
	}
	return nil
}

func scanConnectionSecretsInto(row scanner, sec *ConnectionSecrets) error {
	var provider, status string
	if err := row.Scan(
		&sec.Conn.ID, &sec.Conn.OrganizationID, &provider, &sec.Conn.Label, &status, &sec.Conn.AuthMethod, &sec.Conn.DisplayFields,
		&sec.Conn.ConnectedByUserID, &sec.Conn.ExternalAccountID, &sec.Conn.ExternalAccountName,
		&sec.Conn.GrantedScopes, &sec.Conn.TokenExpiresAt, &sec.Conn.Health, &sec.Conn.HealthDetail, &sec.Conn.HealthCheckedAt,
		&sec.Conn.LastSyncedAt, &sec.Conn.LastError, &sec.Conn.LastErrorAt, &sec.Conn.CreatedAt, &sec.Conn.UpdatedAt,
		&sec.Conn.ConfigCapabilities, &sec.Conn.SyncDirection,
		&sec.ConfigEncrypted, &sec.AccessTokenEnc, &sec.RefreshTokenEnc,
	); err != nil {
		return err
	}
	sec.Conn.Provider = models.IntegrationProvider(provider)
	sec.Conn.Status = models.IntegrationStatus(status)
	if len(sec.Conn.DisplayFields) == 0 {
		sec.Conn.DisplayFields = json.RawMessage("{}")
	}
	if len(sec.Conn.ConfigCapabilities) == 0 {
		sec.Conn.ConfigCapabilities = json.RawMessage("{}")
	}
	return nil
}

func scanEventSubscription(row scanner) (*models.IntegrationEventSubscription, error) {
	var s models.IntegrationEventSubscription
	var action string
	var cfg []byte
	if err := row.Scan(&s.ID, &s.ConnectionID, &s.OrganizationID, &s.EventType, &action, &cfg, &s.Enabled, &s.CreatedAt, &s.UpdatedAt, &s.UseCase); err != nil {
		return nil, err
	}
	s.Action = models.IntegrationAction(action)
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	s.Config = cfg
	return &s, nil
}

// --- Meeting bookings -------------------------------------------------------

// meetingCols is the shared column projection (with a contacts join for the
// display name) so List/Search scan identically.
const meetingCols = `
	mb.id, mb.organization_id, mb.source, mb.external_event_id, mb.status,
	mb.invitee_email, mb.invitee_name, mb.event_name, mb.event_type,
	mb.scheduled_for, mb.end_time, mb.join_url, mb.location,
	mb.cancel_url, mb.reschedule_url, mb.canceled_reason,
	mb.contact_id, mb.campaign_id, mb.created_at, mb.updated_at,
	COALESCE(NULLIF(TRIM(CONCAT_WS(' ', c.first_name, c.last_name)), ''), '') AS contact_name`

func scanMeetingBooking(row pgx.Row, b *models.MeetingBooking) error {
	return row.Scan(
		&b.ID, &b.OrganizationID, &b.Source, &b.ExternalEventID, &b.Status,
		&b.InviteeEmail, &b.InviteeName, &b.EventName, &b.EventType,
		&b.ScheduledFor, &b.EndTime, &b.JoinURL, &b.Location,
		&b.CancelURL, &b.RescheduleURL, &b.CanceledReason,
		&b.ContactID, &b.CampaignID, &b.CreatedAt, &b.UpdatedAt,
		&b.ContactName,
	)
}

func (r *integrationRepository) UpsertMeetingBooking(ctx context.Context, b *models.MeetingBooking) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	if b.Status == "" {
		b.Status = models.MeetingBooked
	}
	raw := b.RawPayload
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO meeting_bookings (
			id, organization_id, source, external_event_id, status,
			invitee_email, invitee_name, event_name, event_type,
			scheduled_for, end_time, join_url, location,
			cancel_url, reschedule_url, canceled_reason,
			contact_id, campaign_id, raw_payload, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, NOW(), NOW())
		ON CONFLICT (organization_id, source, external_event_id) DO UPDATE SET
			-- never let a re-delivered "booked" un-cancel a canceled meeting.
			status = CASE
				WHEN meeting_bookings.status = 'canceled' AND EXCLUDED.status = 'booked'
				THEN meeting_bookings.status ELSE EXCLUDED.status END,
			invitee_email = EXCLUDED.invitee_email,
			invitee_name = EXCLUDED.invitee_name,
			event_name = EXCLUDED.event_name,
			event_type = EXCLUDED.event_type,
			scheduled_for = EXCLUDED.scheduled_for,
			end_time = EXCLUDED.end_time,
			join_url = EXCLUDED.join_url,
			location = EXCLUDED.location,
			cancel_url = EXCLUDED.cancel_url,
			reschedule_url = EXCLUDED.reschedule_url,
			canceled_reason = EXCLUDED.canceled_reason,
			contact_id = COALESCE(EXCLUDED.contact_id, meeting_bookings.contact_id),
			campaign_id = COALESCE(EXCLUDED.campaign_id, meeting_bookings.campaign_id),
			raw_payload = EXCLUDED.raw_payload,
			updated_at = NOW()`,
		b.ID, b.OrganizationID, b.Source, b.ExternalEventID, string(b.Status),
		b.InviteeEmail, b.InviteeName, b.EventName, b.EventType,
		b.ScheduledFor, b.EndTime, b.JoinURL, b.Location,
		b.CancelURL, b.RescheduleURL, b.CanceledReason,
		b.ContactID, b.CampaignID, raw)
	return err
}

func (r *integrationRepository) ListMeetingBookings(ctx context.Context, orgID uuid.UUID, limit int) ([]models.MeetingBooking, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT `+meetingCols+`
		FROM meeting_bookings mb
		LEFT JOIN contacts c ON c.id = mb.contact_id
		WHERE mb.organization_id = $1 ORDER BY mb.created_at DESC LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.MeetingBooking{}
	for rows.Next() {
		var b models.MeetingBooking
		if err := scanMeetingBooking(rows, &b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SearchMeetingBookings powers the Meetings page: timeframe (upcoming/past),
// status, and free-text filters with offset pagination and an honest total.
func (r *integrationRepository) SearchMeetingBookings(ctx context.Context, orgID uuid.UUID, f models.MeetingBookingFilter) (*models.MeetingBookingPage, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	where := []string{"mb.organization_id = $1"}
	args := []any{orgID}
	add := func(clause string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}

	switch f.Timeframe {
	case "upcoming":
		where = append(where, "mb.scheduled_for >= NOW()", "mb.status <> 'canceled'")
	case "past":
		where = append(where, "(mb.scheduled_for < NOW() OR mb.status = 'canceled')")
	}
	if f.Status != "" {
		add("mb.status = $%d", f.Status)
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		args = append(args, "%"+s+"%")
		idx := len(args) // one arg backs all three ILIKE placeholders
		where = append(where, fmt.Sprintf("(mb.invitee_name ILIKE $%d OR mb.invitee_email ILIKE $%d OR mb.event_name ILIKE $%d)", idx, idx, idx))
	}

	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM meeting_bookings mb WHERE `+whereSQL, args...,
	).Scan(&total); err != nil {
		return nil, err
	}

	order := "mb.scheduled_for DESC NULLS LAST, mb.created_at DESC"
	if f.Timeframe == "upcoming" {
		order = "mb.scheduled_for ASC NULLS LAST, mb.created_at DESC"
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM meeting_bookings mb
		LEFT JOIN contacts c ON c.id = mb.contact_id
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, meetingCols, whereSQL, order, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	data := []models.MeetingBooking{}
	for rows.Next() {
		var b models.MeetingBooking
		if err := scanMeetingBooking(rows, &b); err != nil {
			return nil, err
		}
		data = append(data, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	hasMore := offset+len(data) < total
	page := &models.MeetingBookingPage{
		Data: data,
		Pagination: models.MeetingBookingPagination{
			Total:   int64(total),
			Limit:   limit,
			Offset:  offset,
			HasMore: hasMore,
		},
	}
	if hasMore {
		next := offset + limit
		page.Pagination.NextOffset = &next
	}
	return page, nil
}

// GetMeetingBooking fetches one booking (with contact name) scoped to the org.
func (r *integrationRepository) GetMeetingBooking(ctx context.Context, orgID, id uuid.UUID) (*models.MeetingBooking, error) {
	var b models.MeetingBooking
	row := r.db.QueryRow(ctx, `
		SELECT `+meetingCols+`
		FROM meeting_bookings mb
		LEFT JOIN contacts c ON c.id = mb.contact_id
		WHERE mb.organization_id = $1 AND mb.id = $2`, orgID, id)
	if err := scanMeetingBooking(row, &b); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

// DeleteMeetingBooking removes a booking (used to delete a manually-created
// meeting). Org-scoped so a member can't delete another org's row.
func (r *integrationRepository) DeleteMeetingBooking(ctx context.Context, orgID, id uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM meeting_bookings WHERE organization_id = $1 AND id = $2`, orgID, id)
	return err
}

// MeetingBookingSummary returns the counts the sidebar + page header show.
func (r *integrationRepository) MeetingBookingSummary(ctx context.Context, orgID uuid.UUID) (*models.MeetingBookingSummary, error) {
	var s models.MeetingBookingSummary
	err := r.db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE scheduled_for >= NOW() AND status <> 'canceled') AS upcoming,
			COUNT(*) FILTER (WHERE scheduled_for >= date_trunc('day', NOW())
				AND scheduled_for < date_trunc('day', NOW()) + interval '1 day'
				AND status <> 'canceled') AS today,
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE status = 'canceled') AS canceled
		FROM meeting_bookings WHERE organization_id = $1`, orgID).
		Scan(&s.Upcoming, &s.Today, &s.Total, &s.Canceled)
	if err != nil {
		return nil, err
	}
	return &s, nil
}
