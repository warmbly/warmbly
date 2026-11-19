package partner

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/brandkit"
	"github.com/warmbly/warmbly/internal/app/customhost"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/pkg/trackdns"
)

// PortalChecker verifies a group's portal host and names the TXT proof it publishes (customhost.Checker).
type PortalChecker interface {
	Gate(ctx context.Context, res trackdns.Result, ownerID uuid.UUID, held bool) trackdns.Result
	Record(ownerID uuid.UUID, host string) (name, value string)
}

// portalRecheck is how often an unverified portal host is looked up again.
const portalRecheck = 10 * time.Minute

// BrandingInput is group.branding as a partner sends it.
type BrandingInput struct {
	ProductName   string `json:"product_name"`
	LogoURL       string `json:"logo_url"`
	AccentColor   string `json:"accent_color"`
	Website       string `json:"website"`
	HidePoweredBy bool   `json:"hide_powered_by"`
	PortalDomain  string `json:"portal_domain"`
}

// parseBranding validates group.branding. A name is cleaned rather than refused, like the workspace name.
func parseBranding(raw json.RawMessage) (*BrandingInput, *errx.Error) {
	b := &BrandingInput{}
	if len(raw) == 0 || string(raw) == "null" {
		return b, nil
	}
	if err := json.Unmarshal(raw, b); err != nil {
		return nil, invalid("group.branding could not be read.")
	}
	b.ProductName = displayname.CleanOr(b.ProductName, displayname.Workspace, "")
	if b.LogoURL = strings.TrimSpace(b.LogoURL); b.LogoURL != "" {
		u, err := url.Parse(b.LogoURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(b.LogoURL) > 2048 {
			return nil, invalid("group.branding.logo_url is an https:// address.")
		}
	}
	var ok bool
	if b.AccentColor, ok = brandkit.NormalizeAccent(b.AccentColor); !ok {
		return nil, invalid("group.branding.accent_color is a hex colour like #ff5500.")
	}
	if b.Website, ok = brandkit.NormalizeWebsite(b.Website); !ok {
		return nil, invalid("group.branding.website is an https:// address.")
	}
	if b.PortalDomain = config.NormalizeTrackingHost(b.PortalDomain); b.PortalDomain != "" &&
		(!customhost.ValidHost(b.PortalDomain) || customhost.Platform(b.PortalDomain)) {
		return nil, invalid("group.branding.portal_domain is a host name you control, like mail.example.com.")
	}
	return b, nil
}

// saveGroup stores the group's brand as last sent with a workspace; every workspace of the group shows it.
func (s *Service) saveGroup(ctx context.Context, c *Caller, g *GroupInput) *errx.Error {
	if !s.mailboxesOn() || g == nil || strings.TrimSpace(g.ExternalID) == "" {
		return nil
	}
	b, xerr := parseBranding(g.Branding)
	if xerr != nil {
		return xerr
	}
	_, err := s.mb.Repo.UpsertPartnerGroup(ctx, &models.PartnerGroup{
		PartnerID: c.Partner.ID, Mode: c.Mode, ExternalID: strings.TrimSpace(g.ExternalID), Name: displayname.CleanOr(g.Name, displayname.Workspace, ""),
		ProductName: b.ProductName, LogoURL: b.LogoURL, AccentColor: b.AccentColor, WebsiteURL: b.Website, HidePoweredBy: b.HidePoweredBy,
		PortalDomain: b.PortalDomain,
	})
	if err != nil {
		return internal(err)
	}
	if s.mb.BrandsChanged != nil {
		s.mb.BrandsChanged()
	}
	return nil
}

// PortalView is the group's portal host and the records it publishes.
type PortalView struct {
	Domain     string      `json:"domain"`
	Status     string      `json:"status"`
	URL        *string     `json:"url"`
	DNSRecords []DNSRecord `json:"dns_records"`
}

// DNSRecord is one record the group publishes.
type DNSRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// groupView is the group as Warmbly applies it, from the stored group.
func (s *Service) groupView(ctx context.Context, po *models.PartnerOrganization) *GroupView {
	if po.GroupExternalID == nil {
		return nil
	}
	v := &GroupView{ExternalID: *po.GroupExternalID, PoweredBy: true}
	if !s.mailboxesOn() {
		return v
	}
	g, err := s.mb.Repo.GetPartnerGroup(ctx, po.PartnerID, po.Mode, *po.GroupExternalID)
	if err != nil {
		errs.CaptureException(err)
		return v
	}
	if g == nil {
		return v
	}
	v.Branded = g.Branded()
	v.PoweredBy = !(v.Branded && g.HidePoweredBy)
	if g.PortalDomain != "" {
		p := &PortalView{Domain: g.PortalDomain, Status: g.PortalStatus, DNSRecords: []DNSRecord{}}
		if p.Status == "none" {
			p.Status = "pending"
		}
		if target := config.PortalCNAMETarget(); target != "" {
			p.DNSRecords = append(p.DNSRecords, DNSRecord{Type: "CNAME", Name: g.PortalDomain, Value: target})
		}
		if s.mb.Portals != nil {
			if name, value := s.mb.Portals.Record(g.ID, g.PortalDomain); name != "" {
				p.DNSRecords = append(p.DNSRecords, DNSRecord{Type: "TXT", Name: name, Value: value})
			}
		}
		if p.Status == "verified" {
			u := "https://" + g.PortalDomain
			p.URL = &u
		}
		v.Portal = p
	}
	return v
}

// GroupBrandForOrganization implements brandkit.GroupBrands: the brand of the group a managed workspace was sent with.
func (s *Service) GroupBrandForOrganization(ctx context.Context, orgID uuid.UUID) (*models.BrandSettings, error) {
	if s == nil || !s.mailboxesOn() {
		return nil, nil
	}
	g, err := s.mb.Repo.PartnerGroupForOrganization(ctx, orgID)
	if err != nil || g == nil || !g.Branded() {
		return nil, err
	}
	return &models.BrandSettings{DisplayName: g.ProductName, LogoURL: g.LogoURL, AccentColor: g.AccentColor, WebsiteURL: g.WebsiteURL,
		HidePoweredBy: g.HidePoweredBy}, nil
}

// CheckPortals looks up group portal hosts not verified yet: a CNAME to the portal address and the group's TXT proof.
func (s *Service) CheckPortals(ctx context.Context) error {
	if !s.mailboxesOn() || s.mb.Portals == nil {
		return nil
	}
	due, err := s.mb.Repo.DuePartnerPortals(ctx, s.now().Add(-portalRecheck), 20)
	if err != nil {
		return err
	}
	for _, g := range due {
		res := s.mb.Portals.Gate(ctx, customhost.VerifyPortal(ctx, trackdns.Verify, g.PortalDomain), g.ID, false)
		status, at := "pending", (*time.Time)(nil)
		if res.Verified {
			now := s.now().UTC()
			status, at = "verified", &now
		}
		if err := s.mb.Repo.SetPartnerPortalStatus(ctx, g.ID, status, at); err != nil {
			errs.CaptureException(err)
		}
	}
	return nil
}
