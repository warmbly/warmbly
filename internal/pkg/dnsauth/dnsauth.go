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
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

// DKIM status values. A DKIM key lives at a selector its owner chose and DNS
// offers no way to enumerate the selectors under a domain, so a probe that
// finds nothing proves nothing: the only honest negative is "undetermined".
const (
	DKIMStatusFound        = "found"
	DKIMStatusUndetermined = "undetermined"
)

// Result is the outcome of an authentication check for one domain.
type Result struct {
	Domain        string   `json:"domain"`
	SPFFound      bool     `json:"spf_found"`
	SPFRecord     string   `json:"spf_record,omitempty"`
	DKIMFound     bool     `json:"dkim_found"`
	DKIMSelectors []string `json:"dkim_selectors,omitempty"`
	// DKIMStatus is the tri-state DKIMFound cannot express. DKIMFound is only
	// ever a positive: false means the probed selectors did not answer, which
	// is not evidence the domain has no DKIM. Present a DKIMStatusUndetermined
	// result as unverified, never as missing.
	DKIMStatus  string `json:"dkim_status"`
	DMARCFound  bool   `json:"dmarc_found"`
	DMARCPolicy string `json:"dmarc_policy,omitempty"`
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

// defaultSelectors are the selectors probed when neither the caller nor the
// domain's own records name one. They are the generic names mail providers
// hand out, ordered roughly by how often they are seen, and they are a guess:
// nothing here answering means the selector is one we did not try.
var defaultSelectors = []string{
	"google", "selector1", "selector2", "default", "dkim", "mail", "email",
	"k1", "k2", "s1", "s2", "key1", "key2", "smtp", "x", "zoho", "fm1",
	"protonmail", "pm", "sig1", "mandrill",
}

// providerSelectors maps a fragment of a mail provider's hostname, as it
// appears in an MX record or an SPF mechanism, to the selectors that provider
// publishes. Who handles a domain's mail IS discoverable from DNS, and a
// provider's selector is fixed, which turns most of the unguessable lookup
// below into a known one.
//
// Amazon SES, SparkPost and HubSpot are deliberately absent: their selectors
// are per-account tokens, so there is nothing to guess and pretending
// otherwise would only cost a lookup.
var providerSelectors = []struct {
	host      string
	selectors []string
}{
	{"google.com", []string{"google"}},
	{"googlemail.com", []string{"google"}},
	{"outlook.com", []string{"selector1", "selector2"}},
	{"microsoft.com", []string{"selector1", "selector2"}},
	{"zoho", []string{"zoho", "zmail"}},
	{"messagingengine.com", []string{"fm1", "fm2", "fm3", "mesmtp"}},
	{"fastmail.com", []string{"fm1", "fm2", "fm3"}},
	{"protonmail.ch", []string{"protonmail", "protonmail2", "protonmail3"}},
	{"proton.me", []string{"protonmail", "protonmail2", "protonmail3"}},
	{"yandex", []string{"mail"}},
	{"mailgun.org", []string{"mailo", "smtp", "k1", "mg", "pic"}},
	{"sendgrid.net", []string{"s1", "s2", "smtpapi"}},
	{"mcsv.net", []string{"k1", "k2", "k3"}},
	{"mandrillapp.com", []string{"mandrill"}},
	{"mailjet.com", []string{"mailjet"}},
	{"mtasv.net", []string{"pm"}},
	{"postmarkapp.com", []string{"pm"}},
	{"icloud.com", []string{"sig1"}},
	{"migadu.com", []string{"key1", "key2", "key3"}},
	{"mxroute", []string{"x"}},
	{"mxrouting.net", []string{"x"}},
	{"titan.email", []string{"titan1", "titan2"}},
	{"secureserver.net", []string{"default", "dkim"}},
	{"zendesk.com", []string{"zendesk1", "zendesk2"}},
	{"freshemail.io", []string{"fd1", "fd2"}},
	{"klaviyomail.com", []string{"kl", "kl2"}},
	{"mlsend.com", []string{"ml"}},
	{"createsend.com", []string{"cm"}},
	{"elasticemail.com", []string{"api"}},
	{"resend.com", []string{"resend"}},
	{"brevo.com", []string{"mail"}},
	{"sendinblue.com", []string{"mail"}},
	{"mailbox.org", []string{"mbo0001"}},
	{"ionos", []string{"ionos1"}},
	{"hostinger", []string{"hostingermail1", "hostingermail2"}},
}

const (
	lookupTimeout = 5 * time.Second
	// maxSelectorProbes bounds one domain's DKIM probing. The sweep walks its
	// domains one at a time inside a five-minute budget, so the ceiling that
	// matters is four rounds of lookupTimeout, not the query count. Hinted
	// selectors are probed first, so trimming here only ever drops the tail of
	// the generic guesses.
	maxSelectorProbes = 32
	// selectorBatch is how many selectors are probed at once. A batch that
	// finds a key ends the search, so a domain on a mainstream provider costs
	// one round of lookups.
	selectorBatch = 8
)

// lookupFunc returns the TXT records for a name plus whether the failure was
// transient. A DNS "not found" (NXDOMAIN/no such host) is authoritative: the
// record truly is absent. Any other resolver error is uncertain and must not be
// read as a real misconfiguration, so it is reported back as transient=true.
type lookupFunc func(name string) (txts []string, transientErr bool)

// mxLookupFunc returns the MX hostnames for a name. Its failures are never
// authoritative for anything: MX is only read to guess DKIM selectors, which is
// advisory, so the caller ignores the error.
type mxLookupFunc func(name string) (hosts []string, transientErr bool)

// lookups is the resolver the check runs against, injected so the record logic
// is unit-testable without DNS.
type lookups struct {
	txt lookupFunc
	mx  mxLookupFunc
}

// Check validates SPF, DKIM and DMARC for the domain. dkimSelectors may be nil
// to probe the selectors the domain's own SPF and MX records imply, plus a
// default set.
func Check(ctx context.Context, domain string, dkimSelectors []string) Result {
	resolver := &net.Resolver{}
	txt := func(name string) ([]string, bool) {
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
	mx := func(name string) ([]string, bool) {
		c, cancel := context.WithTimeout(ctx, lookupTimeout)
		defer cancel()
		recs, err := resolver.LookupMX(c, name)
		if err != nil {
			return nil, true
		}
		hosts := make([]string, 0, len(recs))
		for _, r := range recs {
			hosts = append(hosts, strings.TrimSuffix(r.Host, "."))
		}
		return hosts, false
	}
	return checkWith(domain, dkimSelectors, lookups{txt: txt, mx: mx})
}

// checkWith is Check with the resolver injected, so the record logic (including
// the organizational-domain DMARC fallback) is unit-testable without DNS.
func checkWith(domain string, dkimSelectors []string, l lookups) Result {
	domain = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(domain), ".")))
	res := Result{Domain: domain, DKIMStatus: DKIMStatusUndetermined}
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
	spfTxts, spfErr := l.txt(domain)
	for _, t := range spfTxts {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t)), "v=spf1") {
			res.SPFFound = true
			res.SPFRecord = strings.TrimSpace(t)
			break
		}
	}

	dmarcErr := lookupDMARC(&res, domain, l.txt)

	// Only the SPF and DMARC lookups gate the persisted verdict; DKIM is advisory
	// so its lookups don't influence LookupError.
	res.LookupError = spfErr || dmarcErr

	// DKIM: a TXT record at <selector>._domainkey.<domain>.
	if len(dkimSelectors) == 0 {
		var mxHosts []string
		if l.mx != nil {
			mxHosts, _ = l.mx(domain)
		}
		dkimSelectors = dedupe(append(selectorHints(res.SPFRecord, mxHosts), defaultSelectors...))
	} else {
		dkimSelectors = dedupe(dkimSelectors)
	}
	if len(dkimSelectors) > maxSelectorProbes {
		dkimSelectors = dkimSelectors[:maxSelectorProbes]
	}
	res.DKIMSelectors = probeSelectors(domain, dkimSelectors, l.txt)
	if len(res.DKIMSelectors) > 0 {
		res.DKIMFound = true
		res.DKIMStatus = DKIMStatusFound
	}

	res.AllAligned = res.SPFFound && res.DKIMFound && res.DMARCFound
	res.Summary = summarize(res)
	return res
}

// probeSelectors resolves the candidates in bounded parallel batches and stops
// at the first batch that finds a key: one published selector already proves
// the domain signs, and a fleet-wide sweep should not pay for the rest. Hits
// are collected in candidate order so the result does not depend on which
// goroutine answered first.
func probeSelectors(domain string, candidates []string, lookup lookupFunc) []string {
	var found []string
	for start := 0; start < len(candidates) && len(found) == 0; start += selectorBatch {
		end := min(start+selectorBatch, len(candidates))
		hits := make([]bool, end-start)
		var wg sync.WaitGroup
		for i := start; i < end; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				txts, _ := lookup(candidates[i] + "._domainkey." + domain)
				for _, t := range txts {
					if dkimKey(t) {
						hits[i-start] = true
						return
					}
				}
			}(i)
		}
		wg.Wait()
		for i, hit := range hits {
			if hit {
				found = append(found, candidates[start+i])
			}
		}
	}
	return found
}

// dkimKey reports whether a TXT record is a DKIM key that can actually sign:
// a v=DKIM1 record (or the bare k=/p= pair some providers still publish) with a
// non-empty p=. An empty p= is a REVOKED key, which signs nothing, so counting
// it would report a dead selector as working authentication.
func dkimKey(txt string) bool {
	var version, keyType, public string
	var hasPublic bool
	for _, part := range strings.Split(txt, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "v":
			version = strings.ToLower(strings.TrimSpace(value))
		case "k":
			keyType = strings.TrimSpace(value)
		case "p":
			public, hasPublic = strings.TrimSpace(value), true
		}
	}
	if version != "" && version != "dkim1" {
		return false
	}
	if version == "" && keyType == "" {
		return false
	}
	return hasPublic && public != ""
}

// selectorHints derives candidate selectors from who actually handles the
// domain's mail: SPF names the services allowed to send for it, MX names the
// mailbox host. Both are published, and each provider's selector is fixed.
func selectorHints(spfRecord string, mxHosts []string) []string {
	var out []string
	hosts := append(strings.Fields(strings.ToLower(spfRecord)), mxHosts...)
	for _, h := range hosts {
		h = strings.ToLower(h)
		for _, p := range providerSelectors {
			if strings.Contains(h, p.host) {
				out = append(out, p.selectors...)
			}
		}
	}
	return out
}

// dedupe keeps the first occurrence of each non-empty entry, so a hinted
// selector is probed before the generic ones rather than twice.
func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
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

// summarize is the one line stored as auth_reason and shown wherever the
// verdict is. Only SPF and DMARC can be reported missing: a DKIM probe that
// found nothing is unverified, so calling it missing sends an owner whose DKIM
// is fine hunting for a record that is already there.
func summarize(r Result) string {
	// A transient failure is not a verdict. This string is persisted as
	// auth_reason and shown in the drawer, the CLI and the notification body,
	// so saying "missing" over a resolver that never answered is the same
	// false alarm State() already refuses to record.
	if r.LookupError {
		return "could not be checked: DNS did not answer"
	}

	var missing []string
	if !r.SPFFound {
		missing = append(missing, "SPF")
	}
	if !r.DMARCFound {
		missing = append(missing, "DMARC")
	}
	if len(missing) > 0 {
		return "missing: " + joinAnd(missing)
	}

	policy := r.DMARCPolicy
	if policy == "" {
		policy = "none"
	}
	s := "SPF, DKIM and DMARC all present (DMARC policy: " + policy + ")"
	if !r.DKIMFound {
		s = "SPF and DMARC present (DMARC policy: " + policy + ")"
	}
	if r.DMARCInherited {
		s += ", inherited from " + r.DMARCDomain
	}
	if !r.DKIMFound {
		s += "; DKIM not verified, no key answered at the selectors we know"
	}
	return s
}

// joinAnd renders a list as "SPF and DMARC".
func joinAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
}
