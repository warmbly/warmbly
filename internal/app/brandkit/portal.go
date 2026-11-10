package brandkit

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/customhost"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

// PortalLookup reads the agency holding a host verified as its portal domain; satisfied by repository.BrandRepository.
type PortalLookup interface {
	VerifiedPortal(ctx context.Context, host string) (*models.AgencyBrand, string, error)
}

// Portal is a verified agency portal domain and the agency behind it.
type Portal struct {
	Host       string
	AgencyID   uuid.UUID
	AgencyName string
	Brand      models.BrandSettings
	SignIn     models.PortalSignIn
	// IconURL, SignInImageURL and Theme are the agency's portal theme, set or not.
	IconURL        string
	SignInImageURL string
	Theme          models.PortalTheme
}

const (
	portalHitTTL    = 2 * time.Minute
	portalMissTTL   = 30 * time.Second
	portalMaxCached = 8192
	portalTimeout   = 3 * time.Second
)

type portalEntry struct {
	portal  *Portal
	expires time.Time
}

// Portals answers which verified portal domain a host is, briefly cached so origin checks stay off the database.
type Portals struct {
	lookup PortalLookup
	now    func() time.Time

	mu    sync.Mutex
	cache map[string]portalEntry
}

// NewPortals builds the cached portal registry.
func NewPortals(lookup PortalLookup) *Portals {
	return &Portals{lookup: lookup, now: time.Now, cache: map[string]portalEntry{}}
}

// Lookup is the verified portal at host, nil when none; a failed read is not cached.
func (p *Portals) Lookup(ctx context.Context, host string) (*Portal, error) {
	host = customhost.Normalize(host)
	if p == nil || p.lookup == nil || !customhost.ValidHost(host) || customhost.Platform(host) || host == config.PortalCNAMETarget() {
		return nil, nil
	}
	now := p.now()
	p.mu.Lock()
	if e, ok := p.cache[host]; ok && now.Before(e.expires) {
		p.mu.Unlock()
		return e.portal, nil
	}
	p.mu.Unlock()

	b, name, err := p.lookup.VerifiedPortal(ctx, host)
	if err != nil {
		return nil, err
	}
	entry := portalEntry{expires: now.Add(portalMissTTL)}
	if b != nil && b.PortalDomainVerified && b.PortalDomain == host {
		entry = portalEntry{
			portal: &Portal{Host: host, AgencyID: b.AgencyID, AgencyName: displayname.Displayable(name), Brand: b.BrandSettings,
				SignIn: signInOrDefault(b.PortalSignIn), IconURL: b.IconURL, SignInImageURL: b.SignInImageURL, Theme: b.Theme},
			expires: now.Add(portalHitTTL),
		}
	}
	p.mu.Lock()
	if len(p.cache) >= portalMaxCached {
		p.cache = map[string]portalEntry{}
	}
	p.cache[host] = entry
	p.mu.Unlock()
	return entry.portal, nil
}

// Verified reports whether host is a verified portal domain; a failed read answers no.
func (p *Portals) Verified(host string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), portalTimeout)
	defer cancel()
	portal, err := p.Lookup(ctx, host)
	return err == nil && portal != nil
}

// Invalidate drops every cached answer after a portal domain changed on this replica.
func (p *Portals) Invalidate() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.cache = map[string]portalEntry{}
	p.mu.Unlock()
}

// PortalView is GET /v1/auth/portal on a verified portal host.
type PortalView struct {
	Portal   bool      `json:"portal"`
	Host     string    `json:"host,omitempty"`
	AgencyID uuid.UUID `json:"agency_id,omitempty"`
	Brand    *Brand    `json:"brand,omitempty"`
	// MainURL is the deployment's own dashboard, where passkeys and single sign-on work.
	MainURL string `json:"main_url,omitempty"`
	// CaptchaFrame is whether the API serves the challenge bridge the portal sign-in embeds.
	CaptchaFrame bool `json:"captcha_frame"`
	// SignIn is where the agency's clients sign in: on the portal, or on MainURL.
	SignIn models.PortalSignIn `json:"sign_in,omitempty"`
}

// PortalView is what the dashboard on host shows: the agency's brand, or portal false.
func (s *Service) PortalView(ctx context.Context, host string, captchaFrame bool) (*PortalView, error) {
	if s == nil || s.portals == nil {
		return &PortalView{}, nil
	}
	p, err := s.portals.Lookup(ctx, host)
	if err != nil || p == nil {
		return &PortalView{}, err
	}
	entitled, xerr := s.agencyEntitled(ctx, p.AgencyID)
	if xerr != nil {
		entitled = false
	}
	agency := &models.AgencyBrand{AgencyID: p.AgencyID, BrandSettings: p.Brand,
		IconURL: p.IconURL, SignInImageURL: p.SignInImageURL, Theme: p.Theme}
	brand := Compose(Default(), nil, agency, entitled).Brand
	if !p.Brand.Set() {
		// The theme does not depend on the agency having named or coloured its brand.
		brand = Brand{Name: p.AgencyName, PoweredBy: Default().PoweredBy}
		applyPortalTheme(&brand, agency)
	}
	if brand.Name == "" {
		brand.Name = p.AgencyName
	}
	return &PortalView{
		Portal:       true,
		Host:         p.Host,
		AgencyID:     p.AgencyID,
		Brand:        &brand,
		MainURL:      config.PrimaryDashboardOrigin(),
		CaptchaFrame: captchaFrame,
		SignIn:       p.SignIn,
	}, nil
}

// Binding is the agency holding host as its verified portal domain and where its clients sign in; nil when none.
func (p *Portals) Binding(ctx context.Context, host string) (*uuid.UUID, models.PortalSignIn, error) {
	portal, err := p.Lookup(ctx, host)
	if err != nil || portal == nil {
		return nil, "", err
	}
	id := portal.AgencyID
	return &id, portal.SignIn, nil
}

// WirePortals attaches the registry this service invalidates when a portal domain changes.
func (s *Service) WirePortals(p *Portals) { s.portals = p }

// Portals is the registry origin checks read; nil when none is wired.
func (s *Service) Portals() *Portals {
	if s == nil {
		return nil
	}
	return s.portals
}
