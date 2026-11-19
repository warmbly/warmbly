package store

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/pkg/signuprisk"
)

// WorkspaceWebsites reads the website a workspace set on its brand.
type WorkspaceWebsites interface {
	GetWorkspaceBrand(ctx context.Context, orgID uuid.UUID) (*models.BrandSettings, error)
}

// DefaultForward is where a new domain forwards unless the buyer changes it:
// the workspace's own website, else the buyer's business email domain, else nothing.
func (s *Service) DefaultForward(ctx context.Context, orgID, userID uuid.UUID) string {
	var website, email string
	if s.Websites != nil {
		if b, err := s.Websites.GetWorkspaceBrand(ctx, orgID); err == nil && b != nil {
			website = b.WebsiteURL
		}
	}
	if s.People != nil {
		if u, err := s.People.GetUser(ctx, userID); err == nil && u != nil {
			email = u.Email
		}
	}
	return defaultForward(website, email)
}

// defaultForward picks the forward from a stored website and the buyer's address.
func defaultForward(website, email string) string {
	if w := strings.TrimSpace(website); w != "" {
		if !strings.Contains(w, "://") {
			w = "https://" + w
		} else if rest, ok := strings.CutPrefix(w, "http://"); ok {
			w = "https://" + rest
		}
		if fwd, xerr := cleanForwarding(w); xerr == nil && fwd != "" {
			return fwd
		}
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return ""
	}
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(email[at+1:]), "."))
	if !domainRe.MatchString(domain) || mailhost.SharedProvider(domain) || signuprisk.IsDisposable(email) {
		return ""
	}
	return "https://" + domain
}
