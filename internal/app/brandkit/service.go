package brandkit

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/customhost"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/trackdns"
	"github.com/warmbly/warmbly/internal/repository"
)

var (
	ErrAgencyNotFound = errx.NewWithIdentifier(errx.NotFound, "agency_not_found", "Agency not found.")
	ErrAgencyRole     = errx.NewWithIdentifier(errx.Forbidden, "agency_forbidden",
		"Only an agency owner or admin can change the agency's branding.")
	ErrPortalDomainInvalid = errx.NewWithIdentifier(errx.BadRequest, "invalid_portal_domain",
		"That does not look like a domain name. Use a subdomain you control, like portal.youragency.com.")
	ErrPortalDomainTaken = errx.NewWithIdentifier(errx.Conflict, "portal_domain_taken",
		"Another workspace or agency on this instance already uses this domain. Choose a host on a domain your agency owns.")
	ErrPortalSignInInvalid = errx.NewWithIdentifier(errx.BadRequest, "invalid_portal_sign_in",
		"portal_sign_in must be \"portal\" or \"warmbly\".")
)

// Service manages stored brands and answers which host a workspace's public pages live on.
type Service struct {
	repo     repository.BrandRepository
	resolver *DBResolver
	plans    PlanReader
	hosts    *customhost.Checker
	verify   func(ctx context.Context, domain, target string) trackdns.Result
	portals  *Portals
	edge     EdgeHosts
}

// EdgeHosts is the edge zone agency portal domains are served from; satisfied by *edgehost.Service.
type EdgeHosts interface {
	Register(ctx context.Context, hosts ...string)
	Records(ctx context.Context, hosts ...string) []models.DNSRecord
}

// WireEdge registers a verified portal domain on the portal edge zone; optional.
func (s *Service) WireEdge(e EdgeHosts) { s.edge = e }

// edgeRegisterTimeout bounds the edge call a verify makes inline; a slow edge is left to its sweep.
const edgeRegisterTimeout = 5 * time.Second

// NewService wires the brand settings service; resolver is the one public pages read.
func NewService(repo repository.BrandRepository, resolver *DBResolver, plans PlanReader, hosts *customhost.Checker) *Service {
	return &Service{repo: repo, resolver: resolver, plans: plans, hosts: hosts, verify: trackdns.Verify}
}

// Resolver is the DB-backed resolver this service invalidates on write; nil when the service is.
func (s *Service) Resolver() *DBResolver {
	if s == nil {
		return nil
	}
	return s.resolver
}

// WorkspaceView is GET /organization/brand.
type WorkspaceView struct {
	// Brand is what the workspace stored; empty fields mean it stored nothing.
	Brand models.BrandSettings `json:"brand"`
	// Inherited is the agency's brand the workspace shows when it sets none.
	Inherited *models.BrandSettings `json:"inherited,omitempty"`
	Effective Resolution            `json:"effective"`
	// PortalHost is where connect links and reports are built; PortalSource says why.
	PortalHost   string `json:"portal_host"`
	PortalSource string `json:"portal_source"`
	// PortalBaseURL is that host as an origin, for embed snippets.
	PortalBaseURL string `json:"portal_base_url"`
	// PoweredBy is the deployment's attribution whether or not this brand hides it; nil when it configured none.
	PoweredBy *PoweredBy `json:"powered_by,omitempty"`
}

// Workspace returns the workspace's stored brand, its inheritance and the result.
func (s *Service) Workspace(ctx context.Context, orgID uuid.UUID) (*WorkspaceView, *errx.Error) {
	ws, err := s.repo.GetWorkspaceBrand(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	agency, err := s.repo.AgencyBrandForOrganization(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	s.resolver.Invalidate(orgID)
	res, err := s.resolver.Resolve(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	out := &WorkspaceView{Effective: res}
	if ws != nil {
		out.Brand = *ws
	}
	if agency != nil && agency.BrandSettings.Set() {
		inherited := agency.BrandSettings
		out.Inherited = &inherited
	}
	out.PortalHost, out.PortalSource = portalOf(res)
	out.PortalBaseURL = config.PortalOriginOn(out.PortalHost)
	out.PoweredBy = Default().PoweredBy
	return out, nil
}

// UpdateWorkspace stores the workspace's brand after validating every field.
func (s *Service) UpdateWorkspace(ctx context.Context, orgID, userID uuid.UUID, in models.BrandInput) (*WorkspaceView, *errx.Error) {
	in, xerr := NormalizeInput(in)
	if xerr != nil {
		return nil, xerr
	}
	stored, err := s.repo.GetWorkspaceBrand(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	res, err := s.resolver.Resolve(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if xerr := CheckHidePoweredBy(in.HidePoweredBy, stored != nil && stored.HidePoweredBy, res.FullWhiteLabel); xerr != nil {
		return nil, xerr
	}
	if _, err := s.repo.UpsertWorkspaceBrand(ctx, orgID, in, userID); err != nil {
		return nil, errx.InternalError()
	}
	return s.Workspace(ctx, orgID)
}

// SetWorkspaceLogo points the brand at an uploaded logo and returns the URL it replaced.
func (s *Service) SetWorkspaceLogo(ctx context.Context, orgID, userID uuid.UUID, url string) (string, *errx.Error) {
	previous, err := s.repo.SetWorkspaceLogo(ctx, orgID, url, userID)
	if err != nil {
		return "", errx.InternalError()
	}
	s.resolver.Invalidate(orgID)
	return previous, nil
}

// ResetWorkspace removes the workspace's brand so it inherits again; returns the logo URL it named.
func (s *Service) ResetWorkspace(ctx context.Context, orgID uuid.UUID) (string, *errx.Error) {
	previous, err := s.repo.DeleteWorkspaceBrand(ctx, orgID)
	if err != nil {
		return "", errx.InternalError()
	}
	s.resolver.Invalidate(orgID)
	return previous, nil
}

// PortalHost is the host a workspace's connect links and reports are built on, with its port on the shared address.
func (s *Service) PortalHost(ctx context.Context, orgID uuid.UUID) string {
	if s == nil {
		return config.PortalURLHost()
	}
	res, _ := s.resolver.Resolve(ctx, orgID)
	host, _ := portalOf(res)
	return host
}

// PortalBaseURL is the origin connect links and reports are built on, for the dashboard to hand out; empty without PORTAL_DOMAIN.
func (s *Service) PortalBaseURL(ctx context.Context, orgID uuid.UUID) string {
	return config.PortalOriginOn(s.PortalHost(ctx, orgID))
}

// portalOf is the agency's verified portal domain, else the shared portal address; nothing without a portal service.
func portalOf(res Resolution) (string, string) {
	if config.PortalURLHost() == "" {
		return "", "shared"
	}
	if host := config.NormalizeTrackingHost(res.PortalHost); host != "" {
		return host, "agency"
	}
	return config.PortalURLHost(), "shared"
}

// AgencyView is GET /agency/:agencyId/brand.
type AgencyView struct {
	Brand          models.BrandSettings     `json:"brand"`
	FullWhiteLabel bool                     `json:"full_white_label"`
	CanManage      bool                     `json:"can_manage"`
	PortalDomain   models.FormsDomainStatus `json:"portal_domain"`
	// PortalBaseURL is the verified portal domain as an origin, else the shared portal address; empty without PORTAL_DOMAIN.
	PortalBaseURL string `json:"portal_base_url"`
	// PoweredBy is the deployment's attribution; nil when it configured none.
	PoweredBy *PoweredBy `json:"powered_by,omitempty"`
	// PortalSignIn is where clients sign in to the portal domain.
	PortalSignIn models.PortalSignIn `json:"portal_sign_in"`
	// IconURL and SignInImageURL are the uploaded tab icon and sign-in page image; empty when none.
	IconURL        string `json:"icon_url"`
	SignInImageURL string `json:"sign_in_image_url"`
	// Theme is the portal theme, {} when the agency set none.
	Theme models.PortalTheme `json:"theme"`
}

// Agency returns the agency's brand for any of its staff.
func (s *Service) Agency(ctx context.Context, agencyID, userID uuid.UUID) (*AgencyView, *errx.Error) {
	role, xerr := s.agencyRole(ctx, agencyID, userID)
	if xerr != nil {
		return nil, xerr
	}
	return s.agencyView(ctx, agencyID, role)
}

func (s *Service) agencyView(ctx context.Context, agencyID uuid.UUID, role models.AgencyRole) (*AgencyView, *errx.Error) {
	b, err := s.repo.GetAgencyBrand(ctx, agencyID)
	if err != nil {
		return nil, errx.InternalError()
	}
	entitled, xerr := s.agencyEntitled(ctx, agencyID)
	if xerr != nil {
		return nil, xerr
	}
	out := &AgencyView{FullWhiteLabel: entitled, CanManage: role.CanManage(), PoweredBy: Default().PoweredBy, PortalSignIn: models.PortalSignInPortal}
	if b != nil {
		out.Brand = b.BrandSettings
		out.PortalSignIn = signInOrDefault(b.PortalSignIn)
		out.IconURL, out.SignInImageURL, out.Theme = b.IconURL, b.SignInImageURL, CleanTheme(b.Theme)
	}
	out.PortalDomain = s.portalStatus(ctx, agencyID, b)
	out.PortalBaseURL = config.PortalBaseURL()
	if b != nil && b.PortalDomainVerified && out.PortalBaseURL != "" {
		out.PortalBaseURL = config.PortalOriginOn(b.PortalDomain)
	}
	return out, nil
}

// UpdateAgency stores the agency's brand; every client without its own sees it.
func (s *Service) UpdateAgency(ctx context.Context, agencyID, userID uuid.UUID, in models.BrandInput) (*AgencyView, *errx.Error) {
	role, xerr := s.manager(ctx, agencyID, userID)
	if xerr != nil {
		return nil, xerr
	}
	in, xerr = NormalizeInput(in)
	if xerr != nil {
		return nil, xerr
	}
	if in.PortalSignIn != nil && !in.PortalSignIn.Valid() {
		return nil, ErrPortalSignInInvalid
	}
	if in.Theme != nil {
		theme, xerr := NormalizeTheme(*in.Theme)
		if xerr != nil {
			return nil, xerr
		}
		in.Theme = &theme
	}
	stored, err := s.repo.GetAgencyBrand(ctx, agencyID)
	if err != nil {
		return nil, errx.InternalError()
	}
	entitled, xerr := s.agencyEntitled(ctx, agencyID)
	if xerr != nil {
		return nil, xerr
	}
	if xerr := CheckHidePoweredBy(in.HidePoweredBy, stored != nil && stored.HidePoweredBy, entitled); xerr != nil {
		return nil, xerr
	}
	if _, err := s.repo.UpsertAgencyBrand(ctx, agencyID, in, userID); err != nil {
		return nil, errx.InternalError()
	}
	s.resolver.InvalidateAll()
	s.portals.Invalidate()
	return s.agencyView(ctx, agencyID, role)
}

// signInOrDefault reads an unknown stored value as the default.
func signInOrDefault(p models.PortalSignIn) models.PortalSignIn {
	if p.Valid() {
		return p
	}
	return models.PortalSignInPortal
}

// SetAgencyImage points the agency's logo, tab icon or sign-in image at an uploaded object and returns the URL it replaced.
func (s *Service) SetAgencyImage(ctx context.Context, agencyID, userID uuid.UUID, image models.AgencyBrandImage, url string) (string, *errx.Error) {
	if _, xerr := s.manager(ctx, agencyID, userID); xerr != nil {
		return "", xerr
	}
	previous, err := s.repo.SetAgencyImage(ctx, agencyID, image, url, userID)
	if err != nil {
		return "", errx.InternalError()
	}
	s.resolver.InvalidateAll()
	s.portals.Invalidate()
	return previous, nil
}

// CanManageAgency refuses anyone but an agency owner or admin, before an upload is stored.
func (s *Service) CanManageAgency(ctx context.Context, agencyID, userID uuid.UUID) *errx.Error {
	_, xerr := s.manager(ctx, agencyID, userID)
	return xerr
}

// SetPortalDomain stores the agency's portal host and resolves it at once; empty clears it.
func (s *Service) SetPortalDomain(ctx context.Context, agencyID, userID uuid.UUID, raw string) (*AgencyView, *errx.Error) {
	role, xerr := s.manager(ctx, agencyID, userID)
	if xerr != nil {
		return nil, xerr
	}
	domain := config.NormalizeTrackingHost(raw)
	if domain != "" && (!customhost.ValidHost(domain) || customhost.Platform(domain)) {
		return nil, ErrPortalDomainInvalid
	}
	if held, err := s.repo.PortalHostHeldElsewhere(ctx, agencyID, domain); err != nil {
		return nil, errx.InternalError()
	} else if held {
		return nil, ErrPortalDomainTaken
	}
	if err := s.repo.SetAgencyPortalDomain(ctx, agencyID, domain, userID); err != nil {
		return nil, errx.InternalError()
	}
	s.resolver.InvalidateAll()
	s.portals.Invalidate()
	if domain == "" {
		return s.agencyView(ctx, agencyID, role)
	}
	return s.VerifyPortalDomain(ctx, agencyID, userID)
}

// VerifyPortalDomain re-resolves the agency's portal host: a CNAME to PORTAL_DOMAIN plus the agency's TXT proof.
func (s *Service) VerifyPortalDomain(ctx context.Context, agencyID, userID uuid.UUID) (*AgencyView, *errx.Error) {
	role, xerr := s.manager(ctx, agencyID, userID)
	if xerr != nil {
		return nil, xerr
	}
	b, err := s.repo.GetAgencyBrand(ctx, agencyID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if b == nil || b.PortalDomain == "" {
		return s.agencyView(ctx, agencyID, role)
	}
	res := s.CheckPortal(ctx, agencyID, b.PortalDomain, b.PortalDomainVerified)
	if res.Verified && !b.PortalDomainVerified {
		if held, err := s.repo.PortalHostHeldElsewhere(ctx, agencyID, res.Domain); err != nil {
			return nil, errx.InternalError()
		} else if held {
			return nil, ErrPortalDomainTaken
		}
	}
	// A resolver failure never un-verifies a host that was working.
	if res.Code != trackdns.CodeLookupError || !b.PortalDomainVerified {
		var at *time.Time
		if res.Verified {
			now := time.Now().UTC()
			at = &now
		}
		if err := s.repo.SetAgencyPortalDomainVerified(ctx, agencyID, b.PortalDomain, res.Verified, at); err != nil {
			if errors.Is(err, repository.ErrPortalDomainTaken) {
				return nil, ErrPortalDomainTaken
			}
			return nil, errx.InternalError()
		}
	}
	s.resolver.InvalidateAll()
	s.portals.Invalidate()
	// The edge has to know the name before a visit over HTTPS can reach the portal.
	if res.Verified && s.edge != nil {
		ectx, cancel := context.WithTimeout(ctx, edgeRegisterTimeout)
		s.edge.Register(ectx, b.PortalDomain)
		cancel()
	}
	view, xerr := s.agencyView(ctx, agencyID, role)
	if xerr != nil {
		return nil, xerr
	}
	view.PortalDomain.Status, view.PortalDomain.Message, view.PortalDomain.Observed = res.Code, res.Reason, res.Observed
	view.PortalDomain.FormsHostUnresolvable = res.TargetUnresolvable
	return view, nil
}

// CheckPortal is the DNS verdict on an agency's portal host; the sweep calls it too.
func (s *Service) CheckPortal(ctx context.Context, agencyID uuid.UUID, domain string, held bool) trackdns.Result {
	return s.hosts.Gate(ctx, customhost.VerifyPortal(ctx, s.verify, domain), agencyID, held)
}

func (s *Service) portalStatus(ctx context.Context, agencyID uuid.UUID, b *models.AgencyBrand) models.FormsDomainStatus {
	out := models.FormsDomainStatus{CNAMETarget: config.PortalCNAMETarget()}
	if b != nil {
		out.FormsDomain, out.FormsDomainVerified, out.FormsDomainVerifiedAt = b.PortalDomain, b.PortalDomainVerified, b.PortalDomainVerifiedAt
	}
	switch {
	case out.FormsDomain == "":
		out.Status = trackdns.CodeUnset
		out.Message = "No portal domain is set; client pages use the shared portal address."
	case out.CNAMETarget == "":
		out.Status = trackdns.CodeNoTarget
		out.Message = "This install has no portal address (PORTAL_DOMAIN) configured, so there is nothing to point a record at."
	case out.FormsDomainVerified:
		out.Status = trackdns.CodeVerified
		out.Message = "Verified. Clients can sign in to their dashboard here, and every client's connect links and reports are built on this domain."
	default:
		out.Status = "pending"
		out.Message = "Not verified yet; client pages use the shared portal address until it resolves."
	}
	if out.FormsDomain != "" {
		out.TXTName, out.TXTValue = s.hosts.Record(agencyID, out.FormsDomain)
	}
	if out.FormsDomainVerified && s.edge != nil {
		out.EdgeRecords = s.edge.Records(ctx, out.FormsDomain)
	}
	return out
}

func (s *Service) agencyRole(ctx context.Context, agencyID, userID uuid.UUID) (models.AgencyRole, *errx.Error) {
	role, err := s.repo.AgencyRole(ctx, agencyID, userID)
	if err != nil {
		return "", errx.InternalError()
	}
	if role == "" {
		return "", ErrAgencyNotFound
	}
	return role, nil
}

func (s *Service) manager(ctx context.Context, agencyID, userID uuid.UUID) (models.AgencyRole, *errx.Error) {
	role, xerr := s.agencyRole(ctx, agencyID, userID)
	if xerr != nil {
		return "", xerr
	}
	if !role.CanManage() {
		return "", ErrAgencyRole
	}
	return role, nil
}

// agencyEntitled reads the agency's own plan: its brand reaches clients under that plan's grant.
func (s *Service) agencyEntitled(ctx context.Context, agencyID uuid.UUID) (bool, *errx.Error) {
	if config.BillingProvider() == "none" {
		return true, nil
	}
	planID, status, err := s.repo.AgencyPlan(ctx, agencyID)
	if err != nil {
		return false, errx.InternalError()
	}
	if planID == nil || !agencyStatusEntitles(status) || s.plans == nil {
		return false, nil
	}
	plan, err := s.plans.GetByID(ctx, *planID)
	if err != nil {
		return false, errx.InternalError()
	}
	return plan.IsolatedEgress(), nil
}

// agencyStatusEntitles mirrors the statuses models.AgencyGrantUntil grants on.
func agencyStatusEntitles(status models.SubscriptionStatus) bool {
	switch status {
	case models.SubscriptionStatusActive, models.SubscriptionStatusTrialing, models.SubscriptionStatusPastDue:
		return true
	}
	return false
}
