package companyenrich

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/integration"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Connections opens credentials only after checking organization ownership.
type Connections interface {
	ConnectionConfig(context.Context, uuid.UUID, uuid.UUID) (map[string]any, *models.IntegrationConnection, error)
	MarkConnectionHealth(context.Context, uuid.UUID, models.IntegrationStatus, models.IntegrationHealth, string)
}

// Contacts is the repository's organization-scoped profile read/write API.
// It deliberately does not invoke campaign or segment enrollment services.
type Contacts interface {
	GetByIDsAndOrganization(context.Context, uuid.UUID, []uuid.UUID) ([]models.Contact, *errx.Error)
	ApplyCompanyEnrichProfile(context.Context, uuid.UUID, uuid.UUID, time.Time, string, *models.CompanyEnrichProfilePatch) *errx.Error
}

// Deps supplies the existing encrypted connection store and contact repository.
type Deps struct {
	Integrations Connections
	Contacts     Contacts
}

// Service exposes explicit queries and enrichment of existing org contacts.
type Service struct {
	Deps
	newClient func(string) *Client
}

func NewService(d Deps) *Service { return &Service{Deps: d, newClient: NewClient} }

// CredentialCheck calls only the free /me endpoint. Its raw body can contain
// secrets and is never returned, logged or persisted as connection metadata.
func (s *Service) CredentialCheck(ctx context.Context, config map[string]any) (map[string]any, error) {
	key, _ := config["api_key"].(string)
	key = strings.TrimSpace(key)
	if !validKey(key) {
		return nil, ErrKey
	}
	out, err := s.newClient(key).request(ctx, "GET", "/me", nil, nil)
	if err != nil {
		return nil, err
	}
	obj, ok := out.Data.(map[string]any)
	if !ok {
		return nil, ErrResponse
	}
	facts := map[string]any{"account_name": "CompanyEnrich"}
	if value, ok := obj["userId"].(string); ok {
		if id, e := uuid.Parse(value); e == nil && id != uuid.Nil {
			facts["account_id"] = id.String()
		}
	}
	config["api_key"] = key
	return facts, nil
}

func (s *Service) connection(ctx context.Context, orgID, connID uuid.UUID) (*Client, error) {
	if orgID == uuid.Nil || connID == uuid.Nil {
		return nil, errx.New(errx.NotFound, "CompanyEnrich connection not found")
	}
	if s.Integrations == nil {
		return nil, errx.New(errx.ServiceUnavailable, "CompanyEnrich is not available on this instance")
	}
	cfg, conn, err := s.Integrations.ConnectionConfig(ctx, orgID, connID)
	if errors.Is(err, integration.ErrConnectionNotFound) {
		return nil, errx.New(errx.NotFound, "CompanyEnrich connection not found")
	}
	if err != nil {
		return nil, err
	}
	if conn == nil || conn.ID != connID || conn.OrganizationID != orgID || conn.Provider != models.IntegrationCompanyEnrich {
		return nil, errx.New(errx.NotFound, "CompanyEnrich connection not found")
	}
	if conn.Status == models.IntegrationStatusDisconnected || conn.Status == models.IntegrationStatusPending || conn.Status == models.IntegrationStatusAuthorizing {
		return nil, errx.NewWithIdentifier(errx.Unprocessable, "companyenrich_not_connected", "Connect CompanyEnrich before making a request")
	}
	key, _ := cfg["api_key"].(string)
	if !validKey(strings.TrimSpace(key)) {
		return nil, publicError(ErrKey)
	}
	return s.newClient(key), nil
}

// Query makes one explicit provider request for an organization-owned connection.
func (s *Service) Query(ctx context.Context, orgID, connID uuid.UUID, in models.CompanyEnrichQuery) (*models.CompanyEnrichResult, error) {
	cl, err := s.connection(ctx, orgID, connID)
	if err != nil {
		return nil, err
	}
	out, err := cl.Query(ctx, in)
	s.noteHealth(ctx, connID, err)
	if err != nil {
		return nil, publicError(err)
	}
	return out, nil
}

func (s *Service) noteHealth(ctx context.Context, connID uuid.UUID, err error) {
	switch {
	case err == nil:
		s.Integrations.MarkConnectionHealth(ctx, connID, models.IntegrationStatusConnected, models.IntegrationHealthHealthy, "")
	case errors.Is(err, ErrKey):
		s.Integrations.MarkConnectionHealth(ctx, connID, models.IntegrationStatusReauthRequired, models.IntegrationHealthDown, ErrKey.Error())
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrCredits), errors.Is(err, ErrRateLimited), errors.Is(err, ErrUnavailable):
		s.Integrations.MarkConnectionHealth(ctx, connID, models.IntegrationStatusConnected, models.IntegrationHealthDegraded, err.Error())
	}
}

func publicError(err error) error {
	var xe *errx.Error
	if errors.As(err, &xe) {
		return xe
	}
	switch {
	case errors.Is(err, ErrKey):
		return errx.NewWithIdentifier(errx.Unprocessable, "companyenrich_key_rejected", ErrKey.Error())
	case errors.Is(err, ErrForbidden):
		return errx.NewWithIdentifier(errx.Unprocessable, "companyenrich_forbidden", ErrForbidden.Error())
	case errors.Is(err, ErrCredits):
		return errx.NewWithIdentifier(errx.Unprocessable, "companyenrich_credits_required", ErrCredits.Error())
	case errors.Is(err, ErrRateLimited):
		return errx.NewWithIdentifier(errx.TooManyRequests, "companyenrich_rate_limited", ErrRateLimited.Error())
	case errors.Is(err, ErrNotFound):
		return errx.NewWithIdentifier(errx.NotFound, "companyenrich_not_found", ErrNotFound.Error())
	case errors.Is(err, ErrRejected):
		return errx.NewWithIdentifier(errx.Unprocessable, "companyenrich_request_rejected", ErrRejected.Error())
	case errors.Is(err, ErrResponse):
		return errx.NewWithIdentifier(errx.Unprocessable, "companyenrich_invalid_response", ErrResponse.Error())
	default:
		return errx.NewWithIdentifier(errx.Unprocessable, "companyenrich_unavailable", ErrUnavailable.Error())
	}
}
