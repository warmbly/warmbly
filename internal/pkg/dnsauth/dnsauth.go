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
	AllAligned    bool     `json:"all_aligned"`
	// LookupError is true when an authoritative lookup (SPF root or DMARC)
	// failed for a reason other than the record simply not existing (timeout,
	// SERVFAIL, network). Callers persisting state must treat this as "unknown"
	// rather than "failing" so a transient resolver hiccup never reads as a
	// domain misconfiguration.
	LookupError bool   `json:"lookup_error"`
	Summary     string `json:"summary"`
}

// State classifies the result for persistence:
//   - "unknown" when the domain is empty or an authoritative lookup errored
//     transiently (never treat that as misconfigured),
//   - "passing" when the two discoverable authoritative records (SPF + DMARC)
//     are present,
//   - "failing" otherwise.
//
// DKIM is advisory only: selectors are not discoverable from DNS, so a missing
// DKIM never forces a "failing" verdict on its own.
func (r Result) State() string {
	if r.Domain == "" || r.LookupError {
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

// Check validates SPF, DKIM and DMARC for the domain. dkimSelectors may be nil
// to probe a default selector set.
func Check(ctx context.Context, domain string, dkimSelectors []string) Result {
	domain = strings.ToLower(strings.TrimSpace(domain))
	res := Result{Domain: domain}
	if domain == "" {
		res.Summary = "no domain to check"
		return res
	}

	resolver := &net.Resolver{}
	// lookup returns the TXT records plus whether the failure was transient. A
	// DNS "not found" (NXDOMAIN/no such host) is authoritative: the record truly
	// is absent. Any other resolver error is uncertain and must not be read as a
	// real misconfiguration, so it is reported back as transient=true.
	lookup := func(name string) (txts []string, transientErr bool) {
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

	// SPF: a TXT record on the root domain beginning v=spf1.
	spfTxts, spfErr := lookup(domain)
	for _, t := range spfTxts {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t)), "v=spf1") {
			res.SPFFound = true
			res.SPFRecord = strings.TrimSpace(t)
			break
		}
	}

	// DMARC: a TXT record at _dmarc.<domain> containing v=DMARC1; capture p=.
	dmarcTxts, dmarcErr := lookup("_dmarc." + domain)
	for _, t := range dmarcTxts {
		if strings.Contains(strings.ToLower(t), "v=dmarc1") {
			res.DMARCFound = true
			res.DMARCPolicy = parseDMARCPolicy(t)
			break
		}
	}

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

func parseDMARCPolicy(record string) string {
	for _, part := range strings.Split(record, ";") {
		part = strings.TrimSpace(strings.ToLower(part))
		if strings.HasPrefix(part, "p=") {
			return strings.TrimSpace(strings.TrimPrefix(part, "p="))
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
		return "SPF, DKIM and DMARC all present (DMARC policy: " + policy + ")"
	}
	return "missing or unverifiable: " + strings.Join(missing, ", ")
}
