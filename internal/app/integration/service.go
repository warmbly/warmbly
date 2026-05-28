package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Service exposes the generic CRUD surface the dashboard talks to.
// Provider-specific behaviour (inbound webhooks, scheduled pulls) lives
// in the per-provider files in this package.
type Service interface {
	Catalog() []models.IntegrationCatalogEntry
	ListConnections(ctx context.Context, orgID uuid.UUID) ([]models.IntegrationConnection, error)

	// Connect registers a new connection. The provider-specific config is
	// stored encrypted via the existing KMS envelope path.
	Connect(ctx context.Context, orgID uuid.UUID, provider models.IntegrationProvider, label string, config map[string]any) (*models.IntegrationConnection, error)
	Disconnect(ctx context.Context, orgID, id uuid.UUID) error

	// RotateInboundSecret regenerates the shared secret for inbound
	// providers like Calendly. Called by the dashboard to refresh the URL.
	RotateInboundSecret(ctx context.Context, orgID, id uuid.UUID, provider models.IntegrationProvider) (string, error)

	// MarkSynced is the call-site every per-provider implementation makes
	// after a successful round-trip with the provider.
	MarkSynced(ctx context.Context, id uuid.UUID, status models.IntegrationStatus, displayFields map[string]any, errMsg string) error

	// Repo exposes the underlying repository so the per-provider files and
	// HTTP handlers can persist provider-specific data without dragging
	// the repo through every method signature.
	Repo() repository.IntegrationRepository
}

type service struct {
	repo repository.IntegrationRepository
}

func NewService(repo repository.IntegrationRepository) Service {
	return &service{repo: repo}
}

func (s *service) Repo() repository.IntegrationRepository { return s.repo }

func (s *service) Catalog() []models.IntegrationCatalogEntry { return Catalog() }

func (s *service) ListConnections(ctx context.Context, orgID uuid.UUID) ([]models.IntegrationConnection, error) {
	return s.repo.ListConnections(ctx, orgID)
}

func (s *service) Connect(ctx context.Context, orgID uuid.UUID, provider models.IntegrationProvider, label string, config map[string]any) (*models.IntegrationConnection, error) {
	if !models.IsValidIntegrationProvider(string(provider)) {
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = string(provider)
	}

	displayFields := buildDisplayFields(provider, config)

	// For providers that POST inbound, mint a secret immediately so the
	// dashboard can surface the URL on the same response.
	var inboundSecret string
	var err error
	if provider == models.IntegrationCalendly ||
		provider == models.IntegrationCalCom {
		inboundSecret, err = generateInboundSecret(provider)
		if err != nil {
			return nil, err
		}
	}

	encrypted, err := encodeConfig(config)
	if err != nil {
		return nil, err
	}

	status := models.IntegrationStatusPending
	switch provider {
	case models.IntegrationCalendly, models.IntegrationCalCom, models.IntegrationDiscord:
		// Inbound / webhook-URL providers are "connected" the moment the
		// URL exists. Data arrives whenever the provider POSTs.
		status = models.IntegrationStatusConnected
	default:
		// API-key and OAuth providers: if the user provided the credential,
		// mark connected optimistically. The next round-trip downgrades to
		// degraded if the credential is bad.
		if _, ok := config["api_token"]; ok {
			status = models.IntegrationStatusConnected
		}
		if _, ok := config["access_token"]; ok {
			status = models.IntegrationStatusConnected
		}
	}

	df, _ := json.Marshal(displayFields)
	conn := &models.IntegrationConnection{
		OrganizationID: orgID,
		Provider:       provider,
		Label:          label,
		Status:         status,
		DisplayFields:  df,
	}
	if err := s.repo.UpsertConnection(ctx, conn, encrypted, inboundSecret); err != nil {
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
	}
	if err := s.repo.UpsertConnection(ctx, conn, nil, secret); err != nil {
		return "", err
	}
	return BuildInboundURL(provider, secret), nil
}

func (s *service) MarkSynced(ctx context.Context, id uuid.UUID, status models.IntegrationStatus, displayFields map[string]any, errMsg string) error {
	df, _ := json.Marshal(displayFields)
	return s.repo.MarkConnectionSynced(ctx, id, status, df, errMsg)
}

// generateInboundSecret returns a 24-byte hex string.
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

// BuildInboundURL is exported so the routes file and the handler tests can
// generate the same URL the dashboard surfaces.
func BuildInboundURL(provider models.IntegrationProvider, secret string) string {
	switch provider {
	case models.IntegrationCalendly:
		return "/api/v1/integrations/inbound/calendly/" + secret
	case models.IntegrationCalCom:
		return "/api/v1/integrations/inbound/cal-com/" + secret
	}
	return ""
}

// encodeConfig serializes the per-provider config map to JSON. The bytes
// returned are what the persistence layer treats as the "encrypted blob".
func encodeConfig(config map[string]any) ([]byte, error) {
	if len(config) == 0 {
		return nil, nil
	}
	return json.Marshal(config)
}

// buildDisplayFields extracts the public, non-secret bits of the config
// that the dashboard surfaces next to a connection card. Anything not
// listed here stays out of the API response.
func buildDisplayFields(provider models.IntegrationProvider, config map[string]any) map[string]any {
	df := map[string]any{}
	switch provider {
	case models.IntegrationCalendly, models.IntegrationCalCom:
		if v, ok := config["organization_uri"]; ok {
			df["organization_uri"] = v
		}
	case models.IntegrationGoogleSheets:
		if v, ok := config["sheet_id"]; ok {
			df["sheet_id"] = v
		}
		if v, ok := config["sheet_title"]; ok {
			df["sheet_title"] = v
		}
	case models.IntegrationHubSpot, models.IntegrationSalesforce, models.IntegrationPipedrive, models.IntegrationClose:
		if v, ok := config["workspace"]; ok {
			df["workspace"] = v
		}
		if v, ok := config["account_email"]; ok {
			df["account_email"] = v
		}
	case models.IntegrationSlack:
		if v, ok := config["workspace"]; ok {
			df["workspace"] = v
		}
		if v, ok := config["channel"]; ok {
			df["channel"] = v
		}
	case models.IntegrationDiscord:
		if v, ok := config["server"]; ok {
			df["server"] = v
		}
	case models.IntegrationZapier, models.IntegrationMake, models.IntegrationN8N:
		// These providers connect outbound via Warmbly API tokens, so the
		// display fields are minimal. Users authenticate on the provider
		// side using a Warmbly API key.
	}
	return df
}
