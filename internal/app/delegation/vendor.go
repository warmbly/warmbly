package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// microsoftAppRoles are the Graph application permissions a Microsoft grant runs on.
var microsoftAppRoles = []string{"Mail.ReadWrite", "Mail.Send", "User.Read.All"}

// AppIdentity is what a vendor needs to authorize this instance's app; ok is false with no app for provider.
func (s *Service) AppIdentity(provider string) (clientID string, scopes, appRoles []string, ok bool) {
	switch provider {
	case models.GrantProviderGoogle:
		if !s.googleEnabled() {
			return "", nil, nil, false
		}
		return s.googleClientID, append([]string(nil), config.GoogleDelegationScopes...), nil, true
	case models.GrantProviderMicrosoft:
		if !s.microsoftEnabled() {
			return "", nil, nil, false
		}
		return s.msClientID, []string{"User.Read"}, append([]string(nil), microsoftAppRoles...), true
	}
	return "", nil, nil, false
}

// GrantFromVendor records a grant a vendor authorized on domain, covering only
// domains in owned: the vendor account listing them is the workspace's proof.
func (s *Service) GrantFromVendor(ctx context.Context, orgID, userID uuid.UUID, provider, domain, admin string, owned []string) (*models.DomainGrant, *errx.Error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	var (
		g    *models.DomainGrant
		xerr *errx.Error
	)
	switch provider {
	case models.GrantProviderGoogle:
		if !s.googleEnabled() {
			return nil, notConfigured("Google Workspace")
		}
		admin = strings.ToLower(strings.TrimSpace(admin))
		if !strings.HasSuffix(admin, "@"+domain) {
			return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDProof, "The vendor has no administrator mailbox on "+domain+".")
		}
		s.forget("g\x00" + admin)
		g, xerr = s.verifyGoogle(ctx, domain, admin)
	case models.GrantProviderMicrosoft:
		if !s.microsoftEnabled() {
			return nil, notConfigured("Microsoft 365")
		}
		tenant, err := s.microsoftTenant(ctx, domain)
		if err != nil {
			return nil, unavailableErr()
		}
		s.forget("m\x00" + tenant)
		g, xerr = s.verifyMicrosoft(ctx, tenant)
	default:
		return nil, errx.ErrNotFound
	}
	if xerr != nil {
		return nil, xerr
	}

	ownedSet := map[string]bool{}
	for _, d := range owned {
		ownedSet[strings.ToLower(strings.TrimSpace(d))] = true
	}
	domains := map[string]bool{}
	for _, d := range g.Domains {
		if ownedSet[d] {
			domains[d] = true
		}
	}
	if !domains[domain] {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDNotCovered, "The authorized organization has no mailboxes on "+domain+".")
	}
	// A second domain in the same tenant widens the grant instead of replacing it.
	if list, err := s.repo.List(ctx, orgID); err == nil {
		for _, prev := range list {
			if prev.Provider == g.Provider && prev.Tenant == g.Tenant {
				for _, d := range prev.Domains {
					domains[d] = true
				}
			}
		}
	}
	g.Domains = sortedKeys(domains)
	g.ID, g.OrganizationID = uuid.New(), orgID
	if err := s.repo.Upsert(ctx, g, userID); err != nil {
		return nil, errx.InternalError()
	}
	return s.get(ctx, orgID, g.ID)
}

// microsoftTenant reads the tenant id a domain signs in to from its OpenID configuration.
func (s *Service) microsoftTenant(ctx context.Context, domain string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.msLoginBase+"/"+url.PathEscape(domain)+"/v2.0/.well-known/openid-configuration", nil)
	if err != nil {
		return "", err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openid configuration: status %d", resp.StatusCode)
	}
	var doc struct {
		Issuer string `json:"issuer"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return "", err
	}
	// https://login.microsoftonline.com/<tenant>/v2.0
	parts := strings.Split(strings.TrimSuffix(doc.Issuer, "/"), "/")
	for _, p := range parts {
		if id, err := uuid.Parse(p); err == nil {
			return id.String(), nil
		}
	}
	return "", fmt.Errorf("openid configuration names no tenant")
}
