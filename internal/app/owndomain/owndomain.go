// Package owndomain decides which hosts belong to a workspace, so a redirect
// the workspace configures (a form's after-submit page, a sending domain's
// root) only ever sends a visitor to a domain the workspace has proved it controls.
package owndomain

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/publicsuffix"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/domainproof"
	"github.com/warmbly/warmbly/internal/pkg/mailhost"
	"github.com/warmbly/warmbly/internal/repository"
)

// ErrIDNotOwn refuses a redirect destination outside the workspace's own domains.
const ErrIDNotOwn = "redirect_target_not_own_domain"

// Refusal is the save-time error for a destination outside the workspace's own domains.
func Refusal() *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, ErrIDNotOwn, RefusalMessage)
}

// RefusalMessage names the kinds of domain a redirect may point at.
const RefusalMessage = "The redirect has to go to a page on one of this workspace's own domains: a domain it has a mailbox on (not a shared provider such as gmail.com), its verified tracking domain, its verified forms domain, a website it verified with its TXT record, or a subdomain of one of them. Verify the website under Sending domains > Redirect, or use an address on one of those."

// Mailboxes lists the domains the workspace has a mailbox on and its verified tracking hosts.
type Mailboxes interface {
	OwnedDomains(ctx context.Context, orgID uuid.UUID) ([]string, *errx.Error)
}

// FormsDomains is the workspace's custom forms domain and whether it verified.
type FormsDomains interface {
	GetFormsDomain(ctx context.Context, orgID uuid.UUID) (string, bool, error)
}

// Resolver is the one definition of a workspace's own domains, read at save time and at serve time.
type Resolver struct {
	mailboxes Mailboxes
	forms     FormsDomains
	websites  repository.WebsiteDomainRepository
	proof     *domainproof.Prover
	txt       domainproof.TXTResolver
}

// WireWebsites lets a workspace add a website by publishing its TXT proof; a nil resolver uses the system's DNS.
func (r *Resolver) WireWebsites(websites repository.WebsiteDomainRepository, proof *domainproof.Prover, txt domainproof.TXTResolver) {
	if txt == nil {
		txt = net.DefaultResolver
	}
	r.websites, r.proof, r.txt = websites, proof, txt
}

// New builds the resolver; forms may be nil.
func New(mailboxes Mailboxes, forms FormsDomains) *Resolver {
	return &Resolver{mailboxes: mailboxes, forms: forms}
}

// Set is a workspace's own domains, split by how each was proved.
type Set struct {
	// Base are its mailbox domains, verified tracking hosts and verified forms domain.
	Base []string
	// Websites are the domains it proved with its TXT record.
	Websites []string
}

// Allows reports whether host is one of the set's domains or under one.
func (s Set) Allows(host string) bool { return Within(host, s.Base) || Within(host, s.Websites) }

// All is every domain in the set.
func (s Set) All() []string { return append(append([]string{}, s.Base...), s.Websites...) }

// Domains are the workspace's own domains: every domain it has a mailbox on
// except shared providers, its verified tracking hosts, its custom forms
// domain once verified, and the websites it proved. Each is normalized; a host under one is also the workspace's.
func (r *Resolver) Domains(ctx context.Context, orgID uuid.UUID) ([]string, *errx.Error) {
	set, xerr := r.Set(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}
	return set.All(), nil
}

// Set is Domains split into the base domains and the proved websites.
func (r *Resolver) Set(ctx context.Context, orgID uuid.UUID) (Set, *errx.Error) {
	base, xerr := r.base(ctx, orgID)
	if xerr != nil {
		return Set{}, xerr
	}
	set := Set{Base: base}
	if r.websites != nil {
		list, err := r.websites.Verified(ctx, orgID)
		if err != nil {
			return Set{}, errx.InternalError()
		}
		set.Websites = clean(list)
	}
	return set, nil
}

func (r *Resolver) base(ctx context.Context, orgID uuid.UUID) ([]string, *errx.Error) {
	var raw []string
	if r.mailboxes != nil {
		list, xerr := r.mailboxes.OwnedDomains(ctx, orgID)
		if xerr != nil {
			return nil, xerr
		}
		raw = append(raw, list...)
	}
	if r.forms != nil {
		domain, verified, err := r.forms.GetFormsDomain(ctx, orgID)
		if err != nil {
			return nil, errx.InternalError()
		}
		if verified && domain != "" {
			raw = append(raw, domain)
		}
	}
	return clean(raw), nil
}

// clean normalizes and dedupes, dropping shared providers.
func clean(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, d := range raw {
		d = Normalize(d)
		if d == "" || seen[d] || mailhost.SharedProvider(d) {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// Allows reports whether the destination URL's host is one of the workspace's own domains or under one.
func (r *Resolver) Allows(ctx context.Context, orgID uuid.UUID, target string) (bool, *errx.Error) {
	host := TargetHost(target)
	if host == "" {
		return false, nil
	}
	set, xerr := r.Set(ctx, orgID)
	if xerr != nil {
		return false, xerr
	}
	return set.Allows(host), nil
}

// Admit is Allows, and when the destination is not yet the workspace's, looks
// for its TXT proof on the website now and records the website when it is there.
func (r *Resolver) Admit(ctx context.Context, orgID uuid.UUID, target string) (bool, *errx.Error) {
	ok, xerr := r.Allows(ctx, orgID, target)
	if xerr != nil || ok {
		return ok, xerr
	}
	proof, xerr := r.Prove(ctx, orgID, target)
	if xerr != nil {
		return false, xerr
	}
	return proof != nil && proof.Verified, nil
}

// Proof is the TXT record that makes a website the workspace's, and whether it is in place.
type Proof struct {
	Domain   string
	Verified bool
	Name     string
	Value    string
}

// Record is the proof to publish for the destination's website, nil when it has none or websites are not wired.
func (r *Resolver) Record(orgID uuid.UUID, target string) *Proof {
	site := Website(TargetHost(target))
	if r.proof == nil || site == "" || mailhost.SharedProvider(site) {
		return nil
	}
	name, value := r.proof.Record(orgID, site)
	return &Proof{Domain: site, Name: name, Value: value}
}

// Prove looks for the destination website's TXT proof now and records the website when it is there.
func (r *Resolver) Prove(ctx context.Context, orgID uuid.UUID, target string) (*Proof, *errx.Error) {
	p := r.Record(orgID, target)
	if p == nil || r.websites == nil {
		return p, nil
	}
	present, err := r.published(ctx, orgID, p.Domain)
	if err != nil || !present {
		return p, nil
	}
	if err := r.websites.Mark(ctx, orgID, p.Domain); err != nil {
		return nil, errx.InternalError()
	}
	p.Verified = true
	return p, nil
}

// published reads the proof at the website itself, never a parent; an error means the lookup could not tell.
func (r *Resolver) published(ctx context.Context, orgID uuid.UUID, site string) (bool, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	txts, err := r.txt.LookupTXT(cctx, domainproof.Name(site))
	if r.proof.Matches(txts, orgID, site) {
		return true, nil
	}
	var dnsErr *net.DNSError
	if err != nil && !(errors.As(err, &dnsErr) && dnsErr.IsNotFound) {
		return false, err
	}
	return false, nil
}

// websiteRecheck is how often a proved website's TXT record is looked at again.
const websiteRecheck = 6 * time.Hour

// Recheck looks again at proofs older than websiteRecheck; one whose record is definitively gone is dropped.
func (r *Resolver) Recheck(ctx context.Context) {
	if r.websites == nil || r.proof == nil {
		return
	}
	due, err := r.websites.Due(ctx, time.Now().Add(-websiteRecheck), 50)
	if err != nil {
		return
	}
	for _, w := range due {
		present, err := r.published(ctx, w.OrganizationID, w.Domain)
		switch {
		case err != nil:
			// A lookup that could not tell keeps the proof and moves it to the back of the queue.
			log.Info().Str("domain", w.Domain).Msg("website proof: lookup failed, kept")
			_ = r.websites.Touch(ctx, w.OrganizationID, w.Domain)
		case present:
			_ = r.websites.Mark(ctx, w.OrganizationID, w.Domain)
		default:
			_ = r.websites.Drop(ctx, w.OrganizationID, w.Domain)
		}
	}
}

// Website is the registrable domain a host belongs to, which is where its proof lives.
func Website(host string) string {
	host = Normalize(host)
	if host == "" {
		return ""
	}
	site, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return ""
	}
	return site
}

// TargetHost is the normalized host of an absolute http(s) URL, or "" when it has none.
func TargetHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return ""
	}
	return Normalize(u.Hostname())
}

// Normalize lowercases a hostname, drops a trailing dot and converts it to
// its ASCII (punycode) form; an IP literal or anything that is not a
// hostname normalizes to "".
func Normalize(host string) string {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if host == "" || net.ParseIP(strings.Trim(host, "[]")) != nil {
		return ""
	}
	return mailhost.NormalizeDomain(host)
}

// Within reports whether host equals one of domains or is a subdomain of one, compared label by label.
func Within(host string, domains []string) bool {
	host = Normalize(host)
	if host == "" {
		return false
	}
	for _, d := range domains {
		d = Normalize(d)
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}
