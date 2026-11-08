// Package brandkit resolves the identity a workspace's public pages show: the
// client connect page, shared reports, report emails and public forms. A
// workspace (or the agency that runs it) can replace the deployment's brand
// with its own; everything public reads it through Resolver.
package brandkit

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
)

// Brand is what a public page renders in its header and footer. Empty strings
// render nothing.
type Brand struct {
	Name        string `json:"name"`
	LogoURL     string `json:"logo_url,omitempty"`
	AccentColor string `json:"accent_color,omitempty"`
	WebsiteURL  string `json:"website_url,omitempty"`
	// PoweredBy is the deployment's attribution, nil when the workspace removed it
	// or the deployment configured none.
	PoweredBy *PoweredBy `json:"powered_by,omitempty"`
}

// PoweredBy is the deployment's attribution line.
type PoweredBy struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// Resolver returns the brand a workspace's public surfaces show.
type Resolver interface {
	ForOrganization(ctx context.Context, orgID uuid.UUID) (Brand, error)
}

// Default is the deployment's own brand, used when nothing overrides it.
func Default() Brand {
	b := config.Brand()
	out := Brand{Name: b.Name, WebsiteURL: b.WebsiteURL}
	if b.WebsiteURL != "" {
		out.PoweredBy = &PoweredBy{Name: b.Name, URL: b.WebsiteURL}
	}
	return out
}

type deploymentResolver struct{}

// DeploymentResolver resolves every workspace to the deployment's brand.
func DeploymentResolver() Resolver { return deploymentResolver{} }

func (deploymentResolver) ForOrganization(context.Context, uuid.UUID) (Brand, error) {
	return Default(), nil
}
