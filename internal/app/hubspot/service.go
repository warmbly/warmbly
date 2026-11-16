package hubspot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/crm"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const provider = models.CRMProviderHubSpot

var _ crm.External = (*Service)(nil)

// Tokens hands out a connection's current OAuth token (the integration service).
type Tokens interface {
	AccessToken(ctx context.Context, orgID, connID uuid.UUID) (string, *models.IntegrationConnection, error)
	GetConnection(ctx context.Context, orgID, id uuid.UUID) (*models.IntegrationConnection, error)
	MarkConnectionHealth(ctx context.Context, connID uuid.UUID, status models.IntegrationStatus, health models.IntegrationHealth, detail string)
}

// Contacts reads Warmbly contacts.
type Contacts interface {
	GetByIDsAndOrganization(ctx context.Context, organizationID uuid.UUID, ids []uuid.UUID) ([]models.Contact, *errx.Error)
	GetByEmailAndOrganization(ctx context.Context, organizationID uuid.UUID, email string) (*models.Contact, *errx.Error)
}

// LeadHolder parks a contact's campaigns (the CRM exit rules).
type LeadHolder interface {
	HoldLeadEverywhere(ctx context.Context, contactID uuid.UUID, until *time.Time, reason, source string) ([]uuid.UUID, error)
}

// Suppressor adds an address to the workspace suppression list.
type Suppressor interface {
	UpsertSuppressedRecipient(ctx context.Context, entry *models.SuppressedRecipient) error
}

// Importer starts a contact import draft from CSV.
type Importer interface {
	Create(ctx context.Context, orgID, userID uuid.UUID, r io.Reader, filename string) (*models.ContactImport, *errx.Error)
}

// Realtime tells the dashboard mirrored data moved.
type Realtime interface {
	PublishCRMSynced(ctx context.Context, orgID uuid.UUID, objects []string, contactID string)
}

// Deps are the Service's collaborators. Importer, Holds, Suppress and Realtime
// may be nil in processes that do not need them.
type Deps struct {
	Repo     repository.CRMProviderRepository
	CRM      repository.CRMRepository
	Tokens   Tokens
	Contacts Contacts
	Holds    LeadHolder
	Suppress Suppressor
	Importer Importer
	Leads    Leads
	Realtime Realtime
	Cache    *cache.Cache
	// AppURL is the dashboard origin, for the "Open in Warmbly" property.
	AppURL string
	// ClientSecret verifies HubSpot webhook signatures (HUBSPOT_OAUTH_CLIENT_SECRET).
	ClientSecret string
}

// Service is the HubSpot CRM mode for every workspace on the instance.
type Service struct {
	d Deps

	mu       sync.Mutex
	settings map[uuid.UUID]cachedSettings
	clients  map[uuid.UUID]*orgClient
}

// orgClient is a workspace's client, valid for one connection: a reconnect
// makes a new connection row, and the old one's token is gone.
type orgClient struct {
	*Client
	connID uuid.UUID
}

type cachedSettings struct {
	row *repository.CRMSettingsRow
	at  time.Time
}

// settingsTTL bounds how stale a process's view of a workspace's mode can be;
// the process that changes it drops its own copy at once.
const settingsTTL = 30 * time.Second

func New(d Deps) *Service {
	return &Service{d: d, settings: map[uuid.UUID]cachedSettings{}, clients: map[uuid.UUID]*orgClient{}}
}

// org is a workspace in HubSpot mode, resolved for one operation.
type org struct {
	ID       uuid.UUID
	ConnID   uuid.UUID
	Portal   string
	UIDomain string
	Config   models.CRMProviderConfig
	Client   *Client
	Settings *repository.CRMSettingsRow
}

func (s *Service) settingsRow(ctx context.Context, orgID uuid.UUID) (*repository.CRMSettingsRow, error) {
	s.mu.Lock()
	if c, ok := s.settings[orgID]; ok && time.Since(c.at) < settingsTTL {
		s.mu.Unlock()
		return c.row, nil
	}
	s.mu.Unlock()
	row, err := s.d.Repo.GetSettings(ctx, orgID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.settings[orgID] = cachedSettings{row: row, at: time.Now()}
	s.mu.Unlock()
	return row, nil
}

// Name is the CRM this service runs.
func (s *Service) Name() models.CRMProvider { return provider }

// Integration is the connection type HubSpot mode runs on.
func (s *Service) Integration() models.IntegrationProvider { return models.IntegrationHubSpot }

// Forget drops this process's cached view of a workspace's mode.
func (s *Service) Forget(orgID uuid.UUID) { s.forget(orgID) }

// Left runs when a workspace stops using HubSpot; HubSpot keeps nothing that
// needs undoing.
func (s *Service) Left(context.Context, uuid.UUID, uuid.UUID) {}

func (s *Service) forget(orgID uuid.UUID) {
	s.mu.Lock()
	delete(s.settings, orgID)
	delete(s.clients, orgID)
	s.mu.Unlock()
}

// Active reports whether the workspace runs its CRM on HubSpot.
func (s *Service) Active(ctx context.Context, orgID uuid.UUID) bool {
	row, err := s.settingsRow(ctx, orgID)
	return err == nil && row != nil && row.Provider == provider && row.ConnectionID != nil
}

// resolve returns the workspace's HubSpot context, or nil when it is not in
// HubSpot mode.
func (s *Service) resolve(ctx context.Context, orgID uuid.UUID) (*org, error) {
	row, err := s.settingsRow(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if row == nil || row.Provider != provider || row.ConnectionID == nil {
		return nil, nil
	}
	conn, err := s.d.Tokens.GetConnection(ctx, orgID, *row.ConnectionID)
	if err != nil || conn == nil {
		return nil, errNotConnected
	}
	o := &org{
		ID:       orgID,
		ConnID:   conn.ID,
		Portal:   conn.ExternalAccountID,
		UIDomain: displayString(conn.DisplayFields, "ui_domain"),
		Config:   row.Config,
		Settings: row,
	}
	if o.UIDomain == "" {
		o.UIDomain = "app.hubspot.com"
	}
	s.mu.Lock()
	cl := s.clients[orgID]
	if cl == nil || cl.portal != o.Portal || cl.connID != conn.ID {
		connID := conn.ID
		cl = &orgClient{Client: NewClient(o.Portal, func(ctx context.Context) (string, error) {
			tok, _, err := s.d.Tokens.AccessToken(ctx, orgID, connID)
			return tok, err
		}, s.d.Cache), connID: connID}
		s.clients[orgID] = cl
	}
	s.mu.Unlock()
	o.Client = cl.Client
	return o, nil
}

var errNotConnected = errors.New("HubSpot is not connected")

// mustResolve is resolve for paths that only run in HubSpot mode.
func (s *Service) mustResolve(ctx context.Context, orgID uuid.UUID) (*org, *errx.Error) {
	o, err := s.resolve(ctx, orgID)
	if err != nil {
		return nil, s.userError(ctx, nil, err)
	}
	if o == nil {
		return nil, errx.NewWithIdentifier(errx.Conflict, "crm_not_connected", "Connect HubSpot and choose it as your CRM first.")
	}
	return o, nil
}

// userError turns a provider failure into the answer a person can act on, and
// flags the connection when HubSpot stopped trusting the token.
func (s *Service) userError(ctx context.Context, o *org, err error) *errx.Error {
	if err == nil {
		return nil
	}
	var xe *errx.Error
	if errors.As(err, &xe) {
		return xe
	}
	if errors.Is(err, errNotConnected) {
		return errx.NewWithIdentifier(errx.Conflict, "crm_reauth_required", "HubSpot is disconnected. Reconnect it in Integrations > HubSpot.")
	}
	if ae, ok := AsAPIError(err); ok {
		switch {
		case ae.AuthProblem():
			if o != nil {
				s.d.Tokens.MarkConnectionHealth(ctx, o.ConnID, models.IntegrationStatusReauthRequired, models.IntegrationHealthDown,
					"HubSpot refused the token or a permission is missing: reconnect required")
			}
			return errx.NewWithIdentifier(errx.Conflict, "crm_reauth_required",
				"Reconnect HubSpot: Warmbly's access was revoked or is missing a permission.")
		case ae.Retryable():
			return errx.NewWithIdentifier(errx.ServiceUnavailable, "crm_unavailable", "HubSpot did not answer. Try again in a moment.")
		case ae.NotFound():
			return errx.NewWithIdentifier(errx.NotFound, "crm_record_missing", "This record no longer exists in HubSpot.")
		default:
			return errx.NewWithIdentifier(errx.Unprocessable, "crm_provider_rejected", "HubSpot refused the change: "+ae.Message)
		}
	}
	if strings.Contains(err.Error(), "token refresh failed") || strings.Contains(err.Error(), "connection not found") {
		return errx.NewWithIdentifier(errx.Conflict, "crm_reauth_required", "Reconnect HubSpot: Warmbly's access was revoked or is missing a permission.")
	}
	log.Warn().Err(err).Msg("hubspot: call failed")
	return errx.NewWithIdentifier(errx.ServiceUnavailable, "crm_unavailable", "HubSpot did not answer. Try again in a moment.")
}

func (s *Service) notify(ctx context.Context, orgID uuid.UUID, contactID string, objects ...string) {
	if s.d.Realtime != nil {
		s.d.Realtime.PublishCRMSynced(ctx, orgID, objects, contactID)
	}
}

func displayString(raw []byte, key string) string {
	if len(raw) == 0 {
		return ""
	}
	m := map[string]any{}
	if err := jsonUnmarshal(raw, &m); err != nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// recordURL is the HubSpot web app link for a record. typeID is HubSpot's
// object type id (0-1 contact, 0-2 company, 0-3 deal).
func (o *org) recordURL(typeID, id string) string {
	if id == "" || o.Portal == "" {
		return ""
	}
	return fmt.Sprintf("https://%s/contacts/%s/record/%s/%s", o.UIDomain, o.Portal, typeID, id)
}

func (o *org) tasksURL() string {
	if o.Portal == "" {
		return ""
	}
	return fmt.Sprintf("https://%s/tasks/%s/view/all", o.UIDomain, o.Portal)
}

func (o *org) pipelineSettingsURL() string {
	if o.Portal == "" {
		return ""
	}
	return fmt.Sprintf("https://%s/sales-products-settings/%s/deals", o.UIDomain, o.Portal)
}
