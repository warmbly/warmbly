package handler

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/config"
)

// AuthorizeTLSDomain answers Caddy's on-demand TLS `ask` probe.
//
//	GET /tls/authorize?domain=<host>  ->  204 issue | 403 refuse
//
// A self-hosted instance's reverse proxy has a site block per hostname it was
// installed with, so a workspace that verifies a custom tracking domain
// (t.acme.com CNAME -> track.selfhost.com) had no certificate for it and every
// tracked link, and every opt-out link riding the same host, failed TLS.
// Verification proves DNS, not that the host can terminate it. This is the
// other half: Caddy asks here before obtaining a certificate for a name it has
// never seen, and the answer is yes exactly for the names this instance
// actually serves.
//
// Unauthenticated on purpose. Caddy has no credential to present, and the
// answer discloses nothing: the CNAME that makes a domain interesting already
// points at this machine in public DNS.
//
// Deliberately gated on *verified* only. An unverified domain is never built
// into a link, so nothing legitimate ever asks for it, and verification is a
// DNS check that completes without a certificate, so gating this way cannot
// deadlock against issuance.
func (h *Handler) AuthorizeTLSDomain(c *gin.Context) {
	host := config.NormalizeTrackingHost(c.Query("domain"))
	if host == "" || !plausibleHostname(host) {
		c.Status(http.StatusForbidden)
		return
	}

	// This instance's own recipient-facing hosts. They normally have their own
	// site block, but answering for them keeps the endpoint's answer equal to
	// "does this instance serve that name" rather than a subset of it.
	if host == config.TrackingHostname() || host == config.FormsHostname() || host == config.PortalCNAMETarget() {
		c.Status(http.StatusNoContent)
		return
	}

	if allow, ok := tlsDomainCache.get(host); ok {
		writeTLSDecision(c, allow)
		return
	}

	if h.CustomDomains == nil {
		c.Status(http.StatusForbidden)
		return
	}
	allow, err := h.CustomDomains.IsVerified(c.Request.Context(), host)
	if err != nil {
		// Refusing is the safe direction and is not sticky: Caddy retries on
		// the next handshake, and a cached "no" from a transient database
		// error would outlive the error itself.
		c.Status(http.StatusForbidden)
		return
	}

	tlsDomainCache.put(host, allow)
	writeTLSDecision(c, allow)
}

func writeTLSDecision(c *gin.Context, allow bool) {
	if allow {
		c.Status(http.StatusNoContent)
		return
	}
	c.Status(http.StatusForbidden)
}

// plausibleHostname rejects what cannot be a domain name before any database
// work. Most of what a scanner puts in SNI dies here.
func plausibleHostname(host string) bool {
	if len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				continue
			}
			// A hyphen is legal inside a label, never at either end.
			if r == '-' && i > 0 && i < len(label)-1 {
				continue
			}
			return false
		}
	}
	return true
}

// tlsDomainCache keeps the gate off the database on the repeat handshakes a
// single name produces. There is deliberately no miss budget on top of it: the
// TLS handshake Caddy is already performing costs more than this indexed
// lookup, so a budget would not be the thing that saves the instance, and it
// would give anyone spraying SNI a way to stop a real customer's first
// certificate from ever being issued.
var tlsDomainCache = &domainCache{
	entries: map[string]domainDecision{},
	allowed: 10 * time.Minute,
	refused: time.Minute,
	max:     8192,
}

type domainDecision struct {
	allow   bool
	expires time.Time
}

type domainCache struct {
	mu      sync.Mutex
	entries map[string]domainDecision

	// A refusal is held briefly and an approval for longer: a domain that has
	// just verified should get its certificate on the next handshake rather
	// than after the customer waits out a cached no.
	allowed time.Duration
	refused time.Duration
	max     int
}

func (d *domainCache) get(host string) (bool, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.entries[host]
	if !ok || time.Now().After(e.expires) {
		return false, false
	}
	return e.allow, true
}

func (d *domainCache) put(host string, allow bool) {
	ttl := d.refused
	if allow {
		ttl = d.allowed
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.entries) >= d.max {
		now := time.Now()
		for k, e := range d.entries {
			if now.After(e.expires) {
				delete(d.entries, k)
			}
		}
		// Still full means the cache is being sprayed rather than used. Drop
		// it whole: bounded memory matters more than the hit rate, and the
		// names worth keeping are re-learned on their next handshake.
		if len(d.entries) >= d.max {
			d.entries = map[string]domainDecision{}
		}
	}
	d.entries[host] = domainDecision{allow: allow, expires: time.Now().Add(ttl)}
}
