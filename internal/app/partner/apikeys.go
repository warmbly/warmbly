package partner

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// partnerKeyRefused are scopes a partner-minted key never holds: a key that mints keys would escape the partner's revocation.
const partnerKeyRefused = models.APIPermAPIKeys

// APIKeyInput is the POST /organizations/{external_id}/api-keys body.
type APIKeyInput struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// APIKey is a workspace key minted for the partner; Secret is only in the creating answer.
type APIKey struct {
	ID          string   `json:"id"`
	Secret      string   `json:"secret"`
	Permissions []string `json:"permissions"`
	APIBase     string   `json:"api_base"`
}

func parsePermissions(names []string) (uint64, []string, *errx.Error) {
	byName := make(map[string]uint64, len(models.AllAPIPermissions))
	for _, p := range models.AllAPIPermissions {
		byName[p.Name] = p.Value
	}
	var mask uint64
	for _, n := range names {
		v, ok := byName[strings.ToUpper(strings.TrimSpace(n))]
		if !ok {
			return 0, nil, invalid("unknown permission: " + n)
		}
		if v&partnerKeyRefused != 0 {
			return 0, nil, errx.NewWithIdentifier(errx.Unprocessable, "permission_not_allowed", "A partner key cannot hold "+n+".")
		}
		mask |= v
	}
	if mask == 0 {
		return 0, nil, invalid("at least one permission is required.")
	}
	out := []string{}
	for _, p := range models.AllAPIPermissions {
		if mask&p.Value != 0 {
			out = append(out, p.Name)
		}
	}
	return mask, out, nil
}

// CreateAPIKey mints a workspace key acting for the partner's service account. apiBase is the public API base it works against.
func (s *Service) CreateAPIKey(ctx context.Context, c *Caller, externalID string, in APIKeyInput, apiBase string) (*APIKey, *errx.Error) {
	if s.APIKeys == nil {
		return nil, errx.NewPublic(errx.ServiceUnavailable, "API keys are not available on this instance.")
	}
	po, xerr := s.ResolveOrganization(ctx, c, externalID)
	if xerr != nil {
		return nil, xerr
	}
	if po.State == models.PartnerOrgCancelled {
		return nil, ErrOrgCancelled
	}
	mask, names, xerr := parsePermissions(in.Permissions)
	if xerr != nil {
		return nil, xerr
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = c.Partner.DisplayName
	}
	if len(name) > 255 {
		return nil, invalid("name is at most 255 characters.")
	}
	orgID := *po.OrganizationID
	k, xerr := s.APIKeys.Create(ctx, orgID, c.Partner.ServiceUserID, &models.CreateAPIKey{Name: name, Permissions: mask})
	if xerr != nil {
		return nil, xerr
	}
	s.audit(ctx, c.Partner, orgID, models.AuditActionCreate, models.AuditEntityAPIKey, map[string]string{"api_key_id": k.ID.String()})
	return &APIKey{ID: k.ID.String(), Secret: k.Secret, Permissions: names, APIBase: apiBase}, nil
}

// RevokeAPIKey revokes a key the partner minted in the workspace. Keys members made are not the partner's to revoke.
func (s *Service) RevokeAPIKey(ctx context.Context, c *Caller, externalID, keyID string) *errx.Error {
	if s.APIKeys == nil {
		return errx.NewPublic(errx.ServiceUnavailable, "API keys are not available on this instance.")
	}
	po, xerr := s.ResolveOrganization(ctx, c, externalID)
	if xerr != nil {
		return xerr
	}
	id, err := uuid.Parse(keyID)
	if err != nil {
		return errx.NewWithIdentifier(errx.NotFound, "not_found", "API key not found.")
	}
	orgID := *po.OrganizationID
	k, xerr := s.APIKeys.Get(ctx, orgID, id)
	if xerr != nil && xerr.Code != errx.NotFound {
		return xerr
	}
	if k == nil || k.OrganizationID != orgID || k.UserID != c.Partner.ServiceUserID {
		return errx.NewWithIdentifier(errx.NotFound, "not_found", "API key not found.")
	}
	if k.Status != models.APIKeyStatusActive {
		return nil
	}
	if xerr := s.APIKeys.Revoke(ctx, orgID, id, "Revoked by partner"); xerr != nil {
		return xerr
	}
	s.audit(ctx, c.Partner, orgID, models.AuditActionRevoke, models.AuditEntityAPIKey, map[string]string{"api_key_id": id.String()})
	return nil
}

// revokePartnerKeys revokes every live key the partner minted in the workspace.
func (s *Service) revokePartnerKeys(ctx context.Context, p *models.Partner, orgID uuid.UUID) *errx.Error {
	if s.APIKeys == nil {
		return nil
	}
	var cursor *uuid.UUID
	for {
		page, xerr := s.APIKeys.List(ctx, orgID, 100, cursor)
		if xerr != nil {
			return xerr
		}
		for _, k := range page.Data {
			if k.UserID == p.ServiceUserID && k.Status == models.APIKeyStatusActive {
				if xerr := s.APIKeys.Revoke(ctx, orgID, k.ID, "Organization cancelled by partner"); xerr != nil {
					return xerr
				}
			}
		}
		if !page.Pagination.HasMore || len(page.Data) == 0 {
			return nil
		}
		last := page.Data[len(page.Data)-1].ID
		cursor = &last
	}
}
