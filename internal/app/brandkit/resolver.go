package brandkit

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

// Source names which stored brand a resolution came from.
type Source string

const (
	SourceWorkspace  Source = "workspace"
	SourceAgency     Source = "agency"
	SourceDeployment Source = "deployment"
)

// Resolution is a workspace's brand plus how it was decided.
type Resolution struct {
	Brand  Brand  `json:"brand"`
	Source Source `json:"source"`
	// AgencyID is the agency the workspace is a client of, set or not.
	AgencyID *uuid.UUID `json:"agency_id,omitempty"`
	// PortalHost is the agency's verified portal domain, empty when none.
	PortalHost string `json:"portal_host,omitempty"`
	// FullWhiteLabel is whether "Powered by" may be removed.
	FullWhiteLabel bool `json:"full_white_label"`
}

// Custom reports whether a workspace or agency brand replaced the deployment's.
func (r Resolution) Custom() bool { return r.Source != SourceDeployment }

// Store reads the stored brands; satisfied by repository.BrandRepository.
type Store interface {
	GetWorkspaceBrand(ctx context.Context, orgID uuid.UUID) (*models.BrandSettings, error)
	AgencyBrandForOrganization(ctx context.Context, orgID uuid.UUID) (*models.AgencyBrand, error)
}

// Entitlements answers whether a workspace may remove the deployment's attribution.
type Entitlements interface {
	FullWhiteLabel(ctx context.Context, orgID uuid.UUID) (bool, error)
}

// Compose applies the precedence: a workspace brand that names anything replaces
// its agency's as a whole, an agency brand replaces the deployment's, and
// hide_powered_by is honoured only when entitled.
func Compose(deployment Brand, workspace *models.BrandSettings, agency *models.AgencyBrand, entitled bool) Resolution {
	out := Resolution{Brand: deployment, Source: SourceDeployment, FullWhiteLabel: entitled}
	if agency != nil {
		id := agency.AgencyID
		out.AgencyID = &id
		if agency.PortalDomainVerified {
			out.PortalHost = agency.PortalDomain
		}
	}
	var chosen *models.BrandSettings
	switch {
	case workspace.Set():
		chosen, out.Source = workspace, SourceWorkspace
	case agency != nil && agency.BrandSettings.Set():
		chosen, out.Source = &agency.BrandSettings, SourceAgency
	default:
		return out
	}
	out.Brand = Brand{
		// A stored name may predate a rule or arrive by import, so it is checked again where every public page reads it.
		Name:        displayname.Displayable(chosen.DisplayName),
		LogoURL:     chosen.LogoURL,
		AccentColor: chosen.AccentColor,
		WebsiteURL:  chosen.WebsiteURL,
		PoweredBy:   deployment.PoweredBy,
	}
	if chosen.HidePoweredBy && entitled {
		out.Brand.PoweredBy = nil
	}
	if out.Source == SourceAgency {
		applyPortalTheme(&out.Brand, agency)
	}
	return out
}

// applyPortalTheme adds what only an agency's brand carries: its tab icon, sign-in image and theme, cleaned as stored values.
func applyPortalTheme(b *Brand, agency *models.AgencyBrand) {
	b.IconURL, b.SignInImageURL = agency.IconURL, agency.SignInImageURL
	if theme := CleanTheme(agency.Theme); !theme.Empty() {
		b.Theme = &theme
	}
}

// resolveTTL bounds how stale a brand on a public page may be on another replica.
const resolveTTL = 30 * time.Second

// maxCached bounds the cache; past it the whole map is dropped and refilled.
const maxCached = 10000

type cached struct {
	res     Resolution
	expires time.Time
}

// DBResolver resolves brands from the database, briefly cached per workspace.
type DBResolver struct {
	store Store
	ent   Entitlements
	now   func() time.Time

	mu    sync.Mutex
	cache map[uuid.UUID]cached
}

// NewDBResolver builds the resolver every public surface reads.
func NewDBResolver(store Store, ent Entitlements) *DBResolver {
	return &DBResolver{store: store, ent: ent, now: time.Now, cache: map[uuid.UUID]cached{}}
}

// ForOrganization implements Resolver.
func (r *DBResolver) ForOrganization(ctx context.Context, orgID uuid.UUID) (Brand, error) {
	res, err := r.Resolve(ctx, orgID)
	return res.Brand, err
}

// Resolve returns the brand and how it was decided. A failed read answers the deployment brand with the error.
func (r *DBResolver) Resolve(ctx context.Context, orgID uuid.UUID) (Resolution, error) {
	fallback := Resolution{Brand: Default(), Source: SourceDeployment, FullWhiteLabel: config.BillingProvider() == "none"}
	if r == nil {
		return fallback, nil
	}
	now := r.now()
	r.mu.Lock()
	if c, ok := r.cache[orgID]; ok && now.Before(c.expires) {
		r.mu.Unlock()
		return c.res, nil
	}
	r.mu.Unlock()

	workspace, err := r.store.GetWorkspaceBrand(ctx, orgID)
	if err != nil {
		return fallback, err
	}
	agency, err := r.store.AgencyBrandForOrganization(ctx, orgID)
	if err != nil {
		return fallback, err
	}
	entitled := config.BillingProvider() == "none"
	if !entitled && r.ent != nil {
		if entitled, err = r.ent.FullWhiteLabel(ctx, orgID); err != nil {
			return fallback, err
		}
	}
	res := Compose(Default(), workspace, agency, entitled)

	r.mu.Lock()
	if len(r.cache) >= maxCached {
		r.cache = map[uuid.UUID]cached{}
	}
	r.cache[orgID] = cached{res: res, expires: now.Add(resolveTTL)}
	r.mu.Unlock()
	return res, nil
}

// Invalidate drops one workspace's cached brand after a write on this replica.
func (r *DBResolver) Invalidate(orgID uuid.UUID) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.cache, orgID)
	r.mu.Unlock()
}

// InvalidateAll drops every cached brand, after an agency brand changed.
func (r *DBResolver) InvalidateAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.cache = map[uuid.UUID]cached{}
	r.mu.Unlock()
}
