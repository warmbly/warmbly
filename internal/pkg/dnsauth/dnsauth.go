// Package dnsauth validates a sending domain's email authentication records
// (SPF, DKIM, DMARC) via DNS TXT lookups. Authentication alignment is a hard
// Google/Yahoo bulk-sender requirement and the most common silent deliverability
// failure, so this lets the platform surface missing/misconfigured records.
//
// Control-plane only: this performs outbound DNS lookups and is meant to run in
// the backend (on demand or on a schedule), never in the worker.
package dnsauth

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

// Result is the outcome of an authentication check for one domain.
type Result struct {
	Domain        string   `json:"domain"`
	SPFFound      bool     `json:"spf_found"`
	SPFRecord     string   `json:"spf_record,omitempty"`
	DKIMFound     bool     `json:"dkim_found"`
	DKIMSelectors []string `json:"dkim_selectors,omitempty"`
	DMARCFound    bool     `json:"dmarc_found"`
	DMARCPolicy   string   `json:"dmarc_policy,omitempty"`
	// DMARCDomain is where the record was actually found. It differs from
	// Domain when the policy is inherited from the organizational domain.
	DMARCDomain string `json:"dmarc_domain,omitempty"`
	// DMARCInherited reports that this subdomain has no record of its own and
	// is covered by its organizational domain's policy (RFC 7489 section 6.6.3).
	DMARCInherited bool `json:"dmarc_inherited"`
	// Reserved marks a special-use domain that is defined never to resolve
	// (.test, .invalid, .localhost, .example, .local). It cannot be evaluated
	// rather than failing evaluation, so it classifies as "unknown".
	Reserved   bool `json:"reserved"`
	AllAligned bool `json:"all_aligned"`
	// LookupError is true when an authoritative lookup (SPF root or DMARC)
	// failed for a reason other than the record simply not existing (timeout,
	// SERVFAIL, network). Callers persisting state must treat this as "unknown"
	// rather than "failing" so a transient resolver hiccup never reads as a
	// domain misconfiguration.
	LookupError bool   `json:"lookup_error"`
	Summary     string `json:"summary"`
}

// State classifies the result for persistence:
//   - "unknown" when the domain is empty, is a special-use domain that cannot
//     resolve by definition, or an authoritative lookup errored transiently
//     (never treat any of those as misconfigured),
//   - "passing" when the two discoverable authoritative records (SPF + DMARC)
//     are present,
//   - "failing" otherwise.
//
// DKIM is advisory only: selectors are not discoverable from DNS, so a missing
// DKIM never forces a "failing" verdict on its own. DMARC policy strength is
// advisory too: Google's bulk-sender rules require a record with at least
// p=none, so p=none is compliant and must not read as failing.
func (r Result) State() string {
	if r.Domain == "" || r.LookupError || r.Reserved {
		return "unknown"
	}
	if r.SPFFound && r.DMARCFound {
		return "passing"
	}
	return "failing"
}

// defaultSelectors are common DKIM selectors to probe when the caller doesn't
// know the domain's selector. DKIM selectors aren't discoverable from DNS, so a
// "not found" only means none of these matched, not that DKIM is absent.
var defaultSelectors = []string{"google", "default", "selector1", "selector2", "k1", "mail", "dkim", "s1", "s2"}

const lookupTimeout = 5 * time.Second

// lookupFunc returns the TXT records for a name plus whether the failure was
// transient. A DNS "not found" (NXDOMAIN/no such host) is authoritative: the
// record truly is absent. Any other resolver error is uncertain and must not be
// read as a real misconfiguration, so it is reported back as transient=true.
type lookupFunc func(name string) (txts []string, transientErr bool)

// Check validates SPF, DKIM and DMARC for the domain. dkimSelectors may be nil
// to probe a default selector set.
func Check(ctx context.Context, domain string, dkimSelectors []string) Result {
	resolver := &net.Resolver{}
	lookup := func(name string) ([]string, bool) {
		c, cancel := context.WithTimeout(ctx, lookupTimeout)
		defer cancel()
		txts, err := resolver.LookupTXT(c, name)
		if err != nil {
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
				return nil, false
			}
			return nil, true
		}
		return txts, false
	}
	return checkWith(domain, dkimSelectors, lookup)
}

// checkWith is Check with the resolver injected, so the record logic (including
// the organizational-domain DMARC fallback) is unit-testable without DNS.
func checkWith(domain string, dkimSelectors []string, lookup lookupFunc) Result {
	domain = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(domain), ".")))
	res := Result{Domain: domain}
	if domain == "" {
		res.Summary = "no domain to check"
		return res
	}

	// A special-use domain is defined never to resolve, so every lookup below
	// would return an authoritative "not found" and the domain would read as
	// misconfigured. It is not: it is unevaluatable. Saying so keeps a
	// development or demo instance (whose mailboxes sit on .test / .local)
	// out of the send gate, and costs nothing in production, where a mailbox
	// on one of these cannot deliver mail anyway.
	if reservedDomain(domain) {
		res.Reserved = true
		res.Summary = "special-use domain, cannot be checked"
		return res
	}

	// SPF: a TXT record on the root domain beginning v=spf1. SPF does NOT
	// inherit from a parent domain, so this must be published on the exact
	// sending domain and there is no fallback to try.
	spfTxts, spfErr := lookup(domain)
	for _, t := range spfTxts {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t)), "v=spf1") {
			res.SPFFound = true
			res.SPFRecord = strings.TrimSpace(t)
			break
		}
	}

	dmarcErr := lookupDMARC(&res, domain, lookup)

	// Only the SPF and DMARC lookups gate the persisted verdict; DKIM is advisory
	// so its lookups don't influence LookupError.
	res.LookupError = spfErr || dmarcErr

	// DKIM: a TXT record at <selector>._domainkey.<domain>.
	if len(dkimSelectors) == 0 {
		dkimSelectors = defaultSelectors
	}
	for _, sel := range dkimSelectors {
		txts, _ := lookup(sel + "._domainkey." + domain)
		for _, t := range txts {
			lt := strings.ToLower(t)
			if strings.Contains(lt, "v=dkim1") || strings.Contains(lt, "k=rsa") || strings.Contains(lt, "p=") {
				res.DKIMFound = true
				res.DKIMSelectors = append(res.DKIMSelectors, sel)
				break
			}
		}
	}

	res.AllAligned = res.SPFFound && res.DKIMFound && res.DMARCFound
	res.Summary = summarize(res)
	return res
}

// lookupDMARC resolves the DMARC policy covering domain and records it on res,
// returning whether a lookup failed transiently.
//
// A subdomain with no record of its own is covered by its organizational
// domain's policy (RFC 7489 section 6.6.3), and the applicable policy there is
// sp= when present, else p=. Dedicated sending subdomains (mail.acme.com,
// go.acme.com) are the norm in cold outreach, so without this fallback every
// correctly-configured one of them reads as missing DMARC.
func lookupDMARC(res *Result, domain string, lookup lookupFunc) bool {
	txts, transient := lookup("_dmarc." + domain)
	if transient {
		return true
	}
	for _, t := range txts {
		if strings.Contains(strings.ToLower(t), "v=dmarc1") {
			res.DMARCFound = true
			res.DMARCDomain = domain
			res.DMARCPolicy = dmarcTag(t, "p")
			return false
		}
	}

	org := organizationalDomain(domain)
	if org == "" || org == domain {
		return false
	}
	orgTxts, orgTransient := lookup("_dmarc." + org)
	if orgTransient {
		return true
	}
	for _, t := range orgTxts {
		if strings.Contains(strings.ToLower(t), "v=dmarc1") {
			res.DMARCFound = true
			res.DMARCInherited = true
			res.DMARCDomain = org
			// sp= is the policy the organizational domain publishes FOR its
			// subdomains; p= applies only when sp= is absent.
			if sp := dmarcTag(t, "sp"); sp != "" {
				res.DMARCPolicy = sp
			} else {
				res.DMARCPolicy = dmarcTag(t, "p")
			}
			return false
		}
	}
	return false
}

// reservedSuffixes are the special-use top-level domains that are guaranteed
// never to resolve on the public internet: RFC 2606 and RFC 6761 (.test,
// .example, .invalid, .localhost), RFC 6762 (.local) and RFC 8375 (.home.arpa).
var reservedSuffixes = []string{"test", "example", "invalid", "localhost", "local", "home.arpa"}

// reservedDomain reports whether the domain sits under a special-use suffix, or
// is one itself.
func reservedDomain(domain string) bool {
	for _, suffix := range reservedSuffixes {
		if domain == suffix || strings.HasSuffix(domain, "."+suffix) {
			return true
		}
	}
	return false
}

// organizationalDomain is the registrable domain (eTLD+1) for a hostname, which
// is where DMARC inheritance stops. Returns "" when it cannot be derived (an
// input that is itself a public suffix, or malformed).
func organizationalDomain(domain string) string {
	org, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return ""
	}
	return org
}

// dmarcTag reads one tag out of a DMARC record ("p", "sp", ...). Tags are
// semicolon-separated name=value pairs and are case-insensitive.
func dmarcTag(record, tag string) string {
	prefix := tag + "="
	for _, part := range strings.Split(record, ";") {
		part = strings.TrimSpace(strings.ToLower(part))
		if strings.HasPrefix(part, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(part, prefix))
		}
	}
	return ""
}

func summarize(r Result) string {
	var missing []string
	if !r.SPFFound {
		missing = append(missing, "SPF")
	}
	if !r.DKIMFound {
		missing = append(missing, "DKIM")
	}
	if !r.DMARCFound {
		missing = append(missing, "DMARC")
	}
	if len(missing) == 0 {
		policy := r.DMARCPolicy
		if policy == "" {
			policy = "none"
		}
		s := "SPF, DKIM and DMARC all present (DMARC policy: " + policy + ")"
		if r.DMARCInherited {
			s += ", inherited from " + r.DMARCDomain
		}
		return s
	}
	return "missing or unverifiable: " + strings.Join(missing, ", ")
}
