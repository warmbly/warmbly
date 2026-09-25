package sendingdomain

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// MaxBulkDomains bounds one bulk setup; the dashboard sends larger selections in chunks.
const MaxBulkDomains = 100

// ErrIDBulkInvalid refuses a bulk setup that names no domain, too many, or nothing to do.
const ErrIDBulkInvalid = "sending_domain_bulk_invalid"

// bulkConcurrency bounds the vendor calls and DNS probes one bulk setup runs at once.
const bulkConcurrency = 4

// bulkDomainTimeout bounds one domain, so a slow vendor or resolver cannot hold a slot for the whole request.
const bulkDomainTimeout = 20 * time.Second

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// BulkInput applies one tracking subdomain and one redirect target to many domains.
type BulkInput struct {
	Domains []string `json:"domains"`
	// TrackingLabel sets <label>.<domain> as every listed domain's tracking host.
	TrackingLabel string `json:"tracking_label"`
	// RedirectURL sends every listed domain's root to one website.
	RedirectURL string `json:"redirect_url"`
	// TrackingHosts and RedirectURLs override the shared value for the domains they name.
	TrackingHosts map[string]string `json:"tracking_hosts"`
	RedirectURLs  map[string]string `json:"redirect_urls"`
}

// BulkResult is what one domain got.
type BulkResult struct {
	Domain   string          `json:"domain"`
	Tracking *BulkTracking   `json:"tracking,omitempty"`
	Redirect *BulkRedirectTo `json:"redirect,omitempty"`
}

// BulkTracking is one domain's tracking host after a bulk setup.
type BulkTracking struct {
	Host string `json:"host"`
	// Via is "vendor" when the domain's vendor wrote the CNAME, "dns" when it is the customer's to add.
	Via         string `json:"via"`
	Verified    bool   `json:"verified"`
	Mailboxes   int    `json:"mailboxes"`
	CNAMETarget string `json:"cname_target,omitempty"`
	// Note says why the vendor did not write the record, when it was asked and refused.
	Note  string `json:"note,omitempty"`
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
}

// BulkRedirectTo is one domain's root redirect after a bulk setup.
type BulkRedirectTo struct {
	TargetURL string `json:"target_url,omitempty"`
	// Via is "vendor" when the vendor forwards the root, "dns" when this instance serves it.
	Via      string `json:"via"`
	Verified bool   `json:"verified"`
	// Reviewed means the vendor's staff apply the forwarding later.
	Reviewed bool   `json:"reviewed,omitempty"`
	Error    string `json:"error,omitempty"`
	Code     string `json:"code,omitempty"`
}

// BulkSetup applies a tracking subdomain and a root redirect to every listed
// domain, each the easiest way available: through the vendor holding the
// domain when its API can, else with DNS the customer adds. One domain's
// refusal is reported on its row and never stops the others. Idempotent.
func (s *Service) BulkSetup(ctx context.Context, orgID, userID uuid.UUID, in BulkInput) ([]BulkResult, *errx.Error) {
	domains := make([]string, 0, len(in.Domains))
	seen := map[string]bool{}
	for _, d := range in.Domains {
		if d = normalizeDomain(d); d != "" && !seen[d] {
			seen[d] = true
			domains = append(domains, d)
		}
	}
	label := strings.ToLower(strings.TrimSpace(in.TrackingLabel))
	target := strings.TrimSpace(in.RedirectURL)
	hosts := map[string]string{}
	for d, h := range in.TrackingHosts {
		if h = config.NormalizeTrackingHost(h); h != "" {
			hosts[normalizeDomain(d)] = h
		}
	}
	targets := map[string]string{}
	for d, u := range in.RedirectURLs {
		if u = strings.TrimSpace(u); u != "" {
			targets[normalizeDomain(d)] = u
		}
	}
	switch {
	case len(domains) == 0:
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDBulkInvalid, "Pick at least one domain.")
	case len(domains) > MaxBulkDomains:
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDBulkInvalid, "One request sets up at most 100 domains.")
	case label == "" && target == "" && len(hosts) == 0 && len(targets) == 0:
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDBulkInvalid, "Give a tracking subdomain, a redirect website, or both.")
	case label != "" && !dnsLabel.MatchString(label):
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDBulkInvalid, "The tracking subdomain is one DNS label, like "+config.DefaultTrackingLabel+".")
	}
	if s.target() == "" {
		return nil, errx.NewWithIdentifier(errx.BadRequest, ErrIDNoTracking, "Open and click tracking is not set up on this instance.")
	}

	var links map[string]models.VendorDomainLink
	if s.vendors != nil {
		links, _ = s.vendors.DomainLinks(ctx, orgID)
	}
	out := make([]BulkResult, len(domains))
	sem := make(chan struct{}, bulkConcurrency)
	var wg sync.WaitGroup
	for i, d := range domains {
		out[i].Domain = d
		var link *models.VendorDomainLink
		if l, ok := links[d]; ok {
			link = &l
		}
		wg.Add(1)
		go func(r *BulkResult, link *models.VendorDomainLink) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(ctx, bulkDomainTimeout)
			defer cancel()
			if h := hosts[r.Domain]; h != "" {
				r.Tracking = s.bulkTracking(ctx, orgID, r.Domain, h, link)
			} else if label != "" {
				r.Tracking = s.bulkTracking(ctx, orgID, r.Domain, label+"."+r.Domain, link)
			}
			if u := targets[r.Domain]; u != "" {
				r.Redirect = s.bulkRedirect(ctx, orgID, userID, r.Domain, u, link)
			} else if target != "" {
				r.Redirect = s.bulkRedirect(ctx, orgID, userID, r.Domain, target, link)
			}
		}(&out[i], link)
	}
	wg.Wait()
	return out, nil
}

func (s *Service) bulkTracking(ctx context.Context, orgID uuid.UUID, domain, host string, link *models.VendorDomainLink) *BulkTracking {
	r := &BulkTracking{Host: host, Via: "dns", CNAMETarget: s.target()}
	if link != nil && link.CanDNS && hasType(link.DNSTypes, "CNAME") {
		if xerr := s.writeTrackingCNAME(ctx, orgID, domain, host); xerr != nil {
			r.Note = rowMessage(domain, xerr)
		} else {
			r.Via = "vendor"
		}
	}
	st, n, xerr := s.ApplyTracking(ctx, orgID, domain, host)
	if xerr != nil {
		r.Error, r.Code = rowMessage(domain, xerr), xerr.ResponseCode()
		return r
	}
	r.Verified, r.Mailboxes = st.TrackingDomainVerified, n
	return r
}

func (s *Service) bulkRedirect(ctx context.Context, orgID, userID uuid.UUID, domain, target string, link *models.VendorDomainLink) *BulkRedirectTo {
	if link != nil && link.CanForward {
		r := &BulkRedirectTo{Via: "vendor", Reviewed: link.ForwardingReviewed}
		l, xerr := s.VendorForward(ctx, orgID, userID, domain, target)
		if xerr != nil {
			r.Error, r.Code = rowMessage(domain, xerr), xerr.ResponseCode()
			return r
		}
		r.TargetURL, r.Verified = l.Forwarding, !link.ForwardingReviewed
		return r
	}
	r := &BulkRedirectTo{Via: "dns"}
	red, xerr := s.SetRedirect(ctx, orgID, userID, domain, RedirectInput{TargetURL: target})
	if xerr != nil {
		r.Error, r.Code = rowMessage(domain, xerr), xerr.ResponseCode()
		return r
	}
	r.TargetURL, r.Verified = red.TargetURL, red.Verified
	return r
}

// rowMessage is what a row may say about a refusal: an Internal error's detail is logged, never answered.
func rowMessage(domain string, xerr *errx.Error) string {
	if xerr.Code != errx.Internal || xerr.Public {
		return xerr.Message
	}
	log.Warn().Str("domain", domain).Str("detail", xerr.Message).Msg("sending domain bulk setup: row failed")
	return "Something went wrong."
}

func hasType(types []string, want string) bool {
	for _, t := range types {
		if strings.EqualFold(t, want) {
			return true
		}
	}
	return false
}
