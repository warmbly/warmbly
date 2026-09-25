// Package sendingdomain is the workspace's view of the domains it sends from:
// their custom tracking host, and an optional redirect of the bare domain to
// the company's main website, served by this instance once DNS proves the
// workspace controls the domain.
package sendingdomain

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/domainproof"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/pkg/trackdns"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

// Stable codes for refusals a client branches on.
const (
	ErrIDNotYours       = "sending_domain_not_in_workspace"
	ErrIDConsumerDomain = "sending_domain_shared_provider"
	ErrIDTarget         = "domain_redirect_invalid_target"
	ErrIDTaken          = "domain_redirect_taken"
	ErrIDNoTracking     = "tracking_host_not_configured"
)

// Mailboxes is the mailbox store's side.
type Mailboxes interface {
	DomainsOverview(ctx context.Context, orgID uuid.UUID) ([]models.SendingDomain, *errx.Error)
	CountDomainMailboxes(ctx context.Context, orgID uuid.UUID, domain string) (int, *errx.Error)
	SetDomainTracking(ctx context.Context, orgID uuid.UUID, domain, host string, verified bool, verifiedAt *time.Time) (int, *errx.Error)
}

// Resolver is the DNS the checks read; net.DefaultResolver in production.
type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupCNAME(ctx context.Context, host string) (string, error)
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Auditor records a redirect changing state, which also refreshes every teammate's dashboard.
type Auditor interface {
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction, entityType models.AuditEntityType, entityID *uuid.UUID, ipAddress, userAgent string, changes, metadata map[string]string)
}

// WireAuditor attaches the audit log; optional.
func (s *Service) WireAuditor(a Auditor) { s.auditor = a }

type Service struct {
	auditor   Auditor
	vendors   VendorDomains
	redirects repository.DomainRedirectRepository
	mailboxes Mailboxes
	resolver  Resolver
	proof     *domainproof.Prover
	// target is this instance's tracking host; "" when tracking is not set up.
	target func() string
}

func NewService(redirects repository.DomainRedirectRepository, mailboxes Mailboxes, resolver Resolver, proof *domainproof.Prover) *Service {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &Service{redirects: redirects, mailboxes: mailboxes, resolver: resolver, proof: proof, target: config.TrackingHostname}
}

func normalizeDomain(d string) string {
	return mailhost.NormalizeDomain(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(d)), "www."))
}

// Overview is every sending domain with its tracking hosts and redirect.
func (s *Service) Overview(ctx context.Context, orgID uuid.UUID) ([]models.SendingDomain, *errx.Error) {
	domains, xerr := s.mailboxes.DomainsOverview(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}
	redirects, err := s.redirects.List(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	byDomain := map[string]*models.DomainRedirect{}
	for i := range redirects {
		r := redirects[i]
		r.Records = s.records(&r, nil)
		byDomain[r.Domain] = &r
	}
	var links map[string]models.VendorDomainLink
	if s.vendors != nil {
		links, _ = s.vendors.DomainLinks(ctx, orgID)
	}
	for i := range domains {
		domains[i].Redirect = byDomain[domains[i].Domain]
		if l, ok := links[domains[i].Domain]; ok {
			domains[i].VendorDomain = &l
		}
	}
	return domains, nil
}

// trackingLabels are the names people and vendors usually give a tracking host.
var trackingLabels = []string{config.DefaultTrackingLabel, "links", "track", "t", "click", "go", "email", "trk"}

// TrackingSuggestion is the tracking host to offer a domain: one already in
// use and verified, one whose DNS already points here, or link.<domain>.
func (s *Service) TrackingSuggestion(ctx context.Context, domain string, inUse []models.TrackingDomainUse) *models.TrackingSuggestion {
	target := s.target()
	if target == "" {
		return nil
	}
	for _, t := range inUse {
		if t.Verified {
			return &models.TrackingSuggestion{Host: t.Host, Status: "active", CNAMETarget: target}
		}
	}
	for _, label := range trackingLabels {
		host := label + "." + domain
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		res := trackdns.VerifyWith(cctx, s.resolver, host, target)
		cancel()
		if res.Verified {
			return &models.TrackingSuggestion{Host: host, Status: "found", CNAMETarget: target}
		}
	}
	return &models.TrackingSuggestion{Host: config.DefaultTrackingHost(domain), Status: "suggested", CNAMETarget: target}
}

// ApplyTracking puts one tracking host on every workspace mailbox on the domain.
// It is saved even before DNS is in place: the tracking sweep verifies it and
// links switch to it the moment the CNAME resolves.
func (s *Service) ApplyTracking(ctx context.Context, orgID uuid.UUID, domain, host string) (*models.TrackingDomainStatus, int, *errx.Error) {
	domain = normalizeDomain(domain)
	if n, xerr := s.mailboxes.CountDomainMailboxes(ctx, orgID, domain); xerr != nil {
		return nil, 0, xerr
	} else if n == 0 {
		return nil, 0, errx.NewWithIdentifier(errx.NotFound, ErrIDNotYours, "The workspace has no mailbox on this domain.")
	}
	host = config.NormalizeTrackingHost(host)
	if host != "" {
		if s.target() == "" {
			return nil, 0, errx.NewWithIdentifier(errx.BadRequest, ErrIDNoTracking, "Open and click tracking is not set up on this instance.")
		}
		if xerr := validate.ValidateTrackingDomain(host); xerr != nil {
			return nil, 0, xerr
		}
	}
	status := &models.TrackingDomainStatus{TrackingDomain: host, CNAMETarget: s.target()}
	if host != "" {
		res := trackdns.VerifyWith(ctx, s.resolver, host, s.target())
		status.TrackingDomainVerified, status.Status, status.Message, status.Observed = res.Verified, res.Code, res.Reason, res.Observed
		status.TrackingHostUnresolvable = res.TargetUnresolvable
		if res.Verified {
			now := time.Now().UTC()
			status.TrackingDomainVerifiedAt = &now
		}
	}
	n, xerr := s.mailboxes.SetDomainTracking(ctx, orgID, domain, host, status.TrackingDomainVerified, status.TrackingDomainVerifiedAt)
	if xerr != nil {
		return nil, 0, xerr
	}
	return status, n, nil
}

// RedirectInput sets a domain's redirect.
type RedirectInput struct {
	TargetURL  string `json:"target_url"`
	IncludeWWW *bool  `json:"include_www"`
}

// SetRedirect records (or updates) the redirect and checks DNS once.
func (s *Service) SetRedirect(ctx context.Context, orgID, userID uuid.UUID, domain string, in RedirectInput) (*models.DomainRedirect, *errx.Error) {
	domain = normalizeDomain(domain)
	if xerr := s.ownDomain(ctx, orgID, domain); xerr != nil {
		return nil, xerr
	}
	target, xerr := cleanTarget(in.TargetURL, domain)
	if xerr != nil {
		return nil, xerr
	}
	if s.target() == "" {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDNoTracking, "Domain redirects are served by the tracking service, which is not set up on this instance.")
	}
	www := true
	if in.IncludeWWW != nil {
		www = *in.IncludeWWW
	}
	r := &models.DomainRedirect{ID: uuid.New(), OrganizationID: orgID, Domain: domain, TargetURL: target, IncludeWWW: www, VerifyToken: s.proof.Value(orgID, domain)}
	if err := s.redirects.Upsert(ctx, r, userID); err != nil {
		return nil, errx.InternalError()
	}
	return s.check(ctx, r)
}

// ownDomain refuses a domain the workspace sends nothing from, and the shared providers anyone has an address on.
func (s *Service) ownDomain(ctx context.Context, orgID uuid.UUID, domain string) *errx.Error {
	if domain == "" {
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDNotYours, "Enter a domain.")
	}
	if mailhost.SharedProvider(domain) {
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDConsumerDomain, "A shared email provider's domain cannot be redirected.")
	}
	n, xerr := s.mailboxes.CountDomainMailboxes(ctx, orgID, domain)
	if xerr != nil {
		return xerr
	}
	if n == 0 {
		return errx.NewWithIdentifier(errx.NotFound, ErrIDNotYours, "The workspace has no mailbox on this domain.")
	}
	return nil
}

// cleanTarget accepts a website address a person pastes and refuses one that would loop.
func cleanTarget(raw, domain string) (string, *errx.Error) {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || len(raw) > 2048 {
		return "", errx.NewWithIdentifier(errx.BadRequest, ErrIDTarget, "Enter the website address to send visitors to, like https://yourcompany.com.")
	}
	host := strings.ToLower(u.Hostname())
	if host == domain || host == "www."+domain {
		return "", errx.NewWithIdentifier(errx.BadRequest, ErrIDTarget, "The redirect cannot point back at the same domain.")
	}
	u.Fragment = ""
	return u.String(), nil
}

// VerifyRedirect checks DNS now.
func (s *Service) VerifyRedirect(ctx context.Context, orgID uuid.UUID, domain string) (*models.DomainRedirect, *errx.Error) {
	domain = normalizeDomain(domain)
	if xerr := s.ownDomain(ctx, orgID, domain); xerr != nil {
		return nil, xerr
	}
	r, err := s.redirects.Get(ctx, orgID, domain)
	if err != nil {
		return nil, errx.InternalError()
	}
	if r == nil {
		return nil, errx.ErrNotFound
	}
	return s.check(ctx, r)
}

// DeleteRedirect stops serving the redirect.
func (s *Service) DeleteRedirect(ctx context.Context, orgID uuid.UUID, domain string) *errx.Error {
	ok, err := s.redirects.Delete(ctx, orgID, normalizeDomain(domain))
	if err != nil {
		return errx.InternalError()
	}
	if !ok {
		return errx.ErrNotFound
	}
	return nil
}

// probe is one DNS check's findings.
type probe struct {
	txt, apex, www           bool
	txtMissing, apexMissing  bool
	apexReason, targetReason string
}

func (s *Service) probe(ctx context.Context, r *models.DomainRedirect) probe {
	var p probe
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	txts, err := s.resolver.LookupTXT(cctx, domainproof.Name(r.Domain))
	p.txt = s.proof.Matches(txts, r.OrganizationID, r.Domain)
	var dnsErr *net.DNSError
	p.txtMissing = !p.txt && (err == nil || (errors.As(err, &dnsErr) && dnsErr.IsNotFound))
	target := s.target()
	apex := trackdns.VerifyWith(cctx, s.resolver, r.Domain, target)
	p.apex = apex.Verified
	p.apexMissing = !p.apex && (apex.Code == trackdns.CodeNotFound || apex.Code == trackdns.CodeWrongTarget)
	p.apexReason = apex.Reason
	if apex.TargetUnresolvable {
		p.targetReason = "The tracking host " + target + " does not resolve, so no address can be compared."
	}
	if r.IncludeWWW {
		p.www = trackdns.VerifyWith(cctx, s.resolver, "www."+r.Domain, target).Verified
	}
	return p
}

// check verifies one redirect and records the verdict. A verified redirect
// only drops when DNS says so definitively, never on a lookup that failed.
func (s *Service) check(ctx context.Context, r *models.DomainRedirect) (*models.DomainRedirect, *errx.Error) {
	p := s.probe(ctx, r)
	verified := p.txt && p.apex
	msg := ""
	switch {
	case p.targetReason != "":
		msg = p.targetReason
	case !p.txt:
		msg = "The TXT record " + domainproof.Name(r.Domain) + " is not there yet. DNS changes can take a while to appear."
	case !p.apex:
		msg = p.apexReason
	}
	if r.Verified && !verified && !(p.txtMissing || p.apexMissing) {
		verified, msg = true, ""
	}
	if err := s.redirects.SetCheck(ctx, r.ID, verified, msg); err != nil {
		if errors.Is(err, repository.ErrRedirectTaken) {
			return nil, errx.NewWithIdentifier(errx.Conflict, ErrIDTaken, "Another workspace on this instance already redirects this domain.")
		}
		return nil, errx.InternalError()
	}
	fresh, err := s.redirects.Get(ctx, r.OrganizationID, r.Domain)
	if err != nil || fresh == nil {
		return nil, errx.InternalError()
	}
	if fresh.Verified != r.Verified && s.auditor != nil && fresh.CreatedBy != nil {
		s.auditor.LogAction(ctx, fresh.OrganizationID, *fresh.CreatedBy, models.AuditActionUpdate, models.AuditEntityDomainRedirect, &fresh.ID, "", "",
			map[string]string{"verified": strconv.FormatBool(fresh.Verified)}, map[string]string{"domain": fresh.Domain})
	}
	fresh.Records = s.records(fresh, &p)
	return fresh, nil
}

// records are what the customer adds at their DNS provider.
func (s *Service) records(r *models.DomainRedirect, p *probe) []models.DNSRecord {
	target := s.target()
	ok := func(v bool) bool { return p != nil && v }
	out := []models.DNSRecord{
		{Purpose: "ownership", Type: "TXT", Name: domainproof.Name(r.Domain), Value: s.proof.Value(r.OrganizationID, r.Domain), OK: ok(p != nil && p.txt) || r.Verified},
	}
	addrs := s.targetAddresses(target)
	if len(addrs) > 0 {
		for _, a := range addrs {
			kind := "A"
			if strings.Contains(a, ":") {
				kind = "AAAA"
			}
			out = append(out, models.DNSRecord{Purpose: "root", Type: kind, Name: r.Domain, Value: a, OK: ok(p != nil && p.apex) || r.Verified})
		}
	} else {
		out = append(out, models.DNSRecord{Purpose: "root", Type: "ALIAS", Name: r.Domain, Value: target, OK: ok(p != nil && p.apex) || r.Verified})
	}
	if r.IncludeWWW {
		out = append(out, models.DNSRecord{Purpose: "www", Type: "CNAME", Name: "www." + r.Domain, Value: target, OK: ok(p != nil && p.www), Optional: true})
	}
	return out
}

func (s *Service) targetAddresses(target string) []string {
	if target == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := s.resolver.LookupIPAddr(ctx, target)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip.IP.IsLoopback() || ip.IP.IsPrivate() || ip.IP.IsUnspecified() {
			continue
		}
		out = append(out, ip.IP.String())
	}
	return out
}

// Lookup is the redirect target for a host the tracking service was asked for.
func (s *Service) Lookup(ctx context.Context, host string) (string, bool, error) {
	return s.redirects.Lookup(ctx, host)
}

// StartSweep re-checks pending redirects often and verified ones a few times a
// day, so DNS added later verifies by itself and DNS removed stops the redirect.
func (s *Service) StartSweep(ctx context.Context) {
	jobrun.Loop(ctx, "domain_redirect_sweep", 10*time.Minute, false, func(ctx context.Context) error {
		due, err := s.redirects.Due(ctx, 100)
		if err != nil {
			return err
		}
		for i := range due {
			if _, xerr := s.check(ctx, &due[i]); xerr != nil && xerr.Identifier != ErrIDTaken {
				log.Warn().Str("domain", due[i].Domain).Str("error", xerr.Message).Msg("domain redirect sweep: check failed")
			}
		}
		return nil
	})
}

// VendorDomains is the inbox vendor side: domains a vendor account holds and what its API can do for them.
type VendorDomains interface {
	DomainLinks(ctx context.Context, orgID uuid.UUID) (map[string]models.VendorDomainLink, *errx.Error)
	SetForwarding(ctx context.Context, orgID uuid.UUID, domain, target string) (*models.VendorDomainLink, *errx.Error)
	UpsertDNS(ctx context.Context, orgID uuid.UUID, domain, typ, name, value string) *errx.Error
}

// WireVendors attaches the inbox vendor accounts; optional.
func (s *Service) WireVendors(v VendorDomains) { s.vendors = v }

// VendorLinks maps every domain the workspace's vendor accounts hold; empty when none is connected.
func (s *Service) VendorLinks(ctx context.Context, orgID uuid.UUID) map[string]models.VendorDomainLink {
	if s.vendors == nil {
		return nil
	}
	links, xerr := s.vendors.DomainLinks(ctx, orgID)
	if xerr != nil {
		return nil
	}
	return links
}

// VendorLink is the vendor account holding the domain, or nil.
func (s *Service) VendorLink(ctx context.Context, orgID uuid.UUID, domain string) *models.VendorDomainLink {
	if l, ok := s.VendorLinks(ctx, orgID)[normalizeDomain(domain)]; ok {
		return &l
	}
	return nil
}

// VendorForward points the domain's root at a website through the vendor that
// sold it, with no DNS for the customer to add. An empty target removes it where the vendor allows.
func (s *Service) VendorForward(ctx context.Context, orgID, userID uuid.UUID, domain, target string) (*models.VendorDomainLink, *errx.Error) {
	domain = normalizeDomain(domain)
	if xerr := s.ownDomain(ctx, orgID, domain); xerr != nil {
		return nil, xerr
	}
	if s.vendors == nil {
		return nil, errx.ErrNotFound
	}
	if target != "" {
		cleaned, xerr := cleanTarget(target, domain)
		if xerr != nil {
			return nil, xerr
		}
		target = cleaned
	}
	link, xerr := s.vendors.SetForwarding(ctx, orgID, domain, target)
	if xerr != nil {
		return nil, xerr
	}
	if s.auditor != nil {
		s.auditor.LogAction(ctx, orgID, userID, models.AuditActionUpdate, models.AuditEntityDomainRedirect, nil, "", "",
			map[string]string{"forwarding": target}, map[string]string{"domain": domain, "via": link.Vendor})
	}
	return link, nil
}

// VendorTracking writes the tracking CNAME through the vendor that holds the
// domain, then points every mailbox on the domain at it.
func (s *Service) VendorTracking(ctx context.Context, orgID uuid.UUID, domain, host string) (*models.TrackingDomainStatus, int, *errx.Error) {
	domain = normalizeDomain(domain)
	host = config.NormalizeTrackingHost(host)
	if xerr := s.writeTrackingCNAME(ctx, orgID, domain, host); xerr != nil {
		return nil, 0, xerr
	}
	return s.ApplyTracking(ctx, orgID, domain, host)
}

func (s *Service) writeTrackingCNAME(ctx context.Context, orgID uuid.UUID, domain, host string) *errx.Error {
	if xerr := s.ownDomain(ctx, orgID, domain); xerr != nil {
		return xerr
	}
	// Checked before any vendor write: the upsert replaces whatever record sits at the name.
	if xerr := validate.ValidateTrackingDomain(host); xerr != nil {
		return xerr
	}
	target := s.target()
	if target == "" {
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDNoTracking, "Open and click tracking is not set up on this instance.")
	}
	if !strings.Contains(target, ".") {
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDNoTracking, "This instance's tracking host ("+target+") is not a public name, so no DNS record can point at it.")
	}
	if !strings.HasSuffix(host, "."+domain) {
		return errx.NewWithIdentifier(errx.BadRequest, ErrIDTarget, "The tracking domain has to be a subdomain of "+domain+" to be written through its vendor.")
	}
	if s.vendors == nil {
		return errx.ErrNotFound
	}
	return s.vendors.UpsertDNS(ctx, orgID, domain, "CNAME", host, target)
}

// AutoRedirect sets a domain's root redirect the easiest way available: the
// vendor's own forwarding when a vendor account holds the domain, else a
// redirect served here once DNS is in place.
func (s *Service) AutoRedirect(ctx context.Context, orgID, userID uuid.UUID, domain, target string) *errx.Error {
	domain = normalizeDomain(domain)
	if l := s.VendorLink(ctx, orgID, domain); l != nil && l.CanForward {
		_, xerr := s.VendorForward(ctx, orgID, userID, domain, target)
		return xerr
	}
	_, xerr := s.SetRedirect(ctx, orgID, userID, domain, RedirectInput{TargetURL: target})
	return xerr
}

// AutoTracking writes the tracking CNAME through the domain's vendor when it
// can; otherwise the customer adds it and the tracking sweep picks it up.
func (s *Service) AutoTracking(ctx context.Context, orgID uuid.UUID, domain, host string) {
	domain = normalizeDomain(domain)
	l := s.VendorLink(ctx, orgID, domain)
	if l == nil || !l.CanDNS {
		return
	}
	if xerr := s.writeTrackingCNAME(ctx, orgID, domain, config.NormalizeTrackingHost(host)); xerr != nil {
		log.Info().Str("domain", domain).Str("code", xerr.ResponseCode()).Msg("sending domain: vendor could not write the tracking record")
	}
}
