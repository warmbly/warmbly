// Package customhost is the one definition of who owns a workspace's custom
// domain: who may verify it, and whose forms and tracked links it serves.
package customhost

import (
	"context"
	"net"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/pkg/domainproof"
	"github.com/warmbly/warmbly/internal/pkg/trackdns"
)

// Owners lists the organizations holding a host verified as any kind of custom domain.
type Owners interface {
	Owners(ctx context.Context, host string) ([]uuid.UUID, error)
}

// Checker answers ownership questions about custom hosts.
type Checker struct {
	owners Owners
	proof  *domainproof.Prover
	txt    domainproof.TXTResolver
}

// New builds a checker; a nil resolver uses the system's DNS.
func New(owners Owners, proof *domainproof.Prover, txt domainproof.TXTResolver) *Checker {
	if txt == nil {
		txt = net.DefaultResolver
	}
	return &Checker{owners: owners, proof: proof, txt: txt}
}

// Normalize is the bare lower-case host without port or trailing dot.
func Normalize(raw string) string {
	h := strings.ToLower(strings.TrimSpace(raw))
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.TrimSuffix(h, ".")
}

// Platform reports whether host is one of this instance's own shared hosts.
func Platform(host string) bool {
	if host == "" {
		return false
	}
	return host == config.TrackingHostname() || host == config.FormsHostname() || host == config.PortalCNAMETarget()
}

// Verifier is one DNS check of domain against target; trackdns.Verify satisfies it.
type Verifier func(ctx context.Context, domain, target string) trackdns.Result

// VerifyPortal checks domain against the one host a portal domain points at; without PORTAL_DOMAIN nothing verifies.
func VerifyPortal(ctx context.Context, verify Verifier, domain string) trackdns.Result {
	target := config.PortalCNAMETarget()
	if target == "" {
		return trackdns.Result{Domain: Normalize(domain), Code: trackdns.CodeNoTarget,
			Reason: "This Warmbly install has no portal address configured, so there is nothing to point a CNAME at yet. Ask your administrator to set PORTAL_DOMAIN."}
	}
	return verify(ctx, domain, target)
}

// Holds reports whether orgID already has host verified as one of its custom domains.
func (c *Checker) Holds(ctx context.Context, orgID uuid.UUID, host string) bool {
	if c == nil || c.owners == nil || host == "" {
		return false
	}
	owners, err := c.owners.Owners(ctx, Normalize(host))
	if err != nil {
		return false
	}
	for _, o := range owners {
		if o == orgID {
			return true
		}
	}
	return false
}

// Gate keeps a CNAME verdict only when the workspace already holds the host or publishes its TXT proof.
func (c *Checker) Gate(ctx context.Context, res trackdns.Result, orgID uuid.UUID, held bool) trackdns.Result {
	if !res.Verified {
		return res
	}
	held = held || c.Holds(ctx, orgID, res.Domain)
	var proof *domainproof.Prover
	var txt domainproof.TXTResolver
	if c != nil {
		proof, txt = c.proof, c.txt
	}
	return proof.Gate(ctx, txt, res, orgID, held)
}

// Record is the TXT name and value the workspace publishes for host.
func (c *Checker) Record(orgID uuid.UUID, host string) (name, value string) {
	if c == nil {
		return "", ""
	}
	return c.proof.Record(orgID, Normalize(host))
}

// Serves reports whether content owned by orgID may be served on host: on another workspace's custom host it may not.
func (c *Checker) Serves(ctx context.Context, host string, orgID uuid.UUID) (bool, error) {
	owners, restricted, err := c.Guard(ctx, host)
	if err != nil || !restricted {
		return err == nil, err
	}
	return Allowed(owners, &orgID), nil
}

// Guard lists the workspaces a host is reserved to; restricted is false on a shared host or one nobody verified.
func (c *Checker) Guard(ctx context.Context, host string) (owners []uuid.UUID, restricted bool, err error) {
	host = Normalize(host)
	if c == nil || c.owners == nil || host == "" || Platform(host) {
		return nil, false, nil
	}
	owners, err = c.owners.Owners(ctx, host)
	if err != nil {
		return nil, true, err
	}
	return owners, len(owners) > 0, nil
}

// Allowed reports whether orgID is one of a restricted host's owners; an unknown organization never is.
func Allowed(owners []uuid.UUID, orgID *uuid.UUID) bool {
	if orgID == nil {
		return false
	}
	for _, o := range owners {
		if o == *orgID {
			return true
		}
	}
	return false
}

// ValidHost rejects the obvious non-domains before a DNS lookup: a
// bare label, an IP-looking value, or anything with characters a hostname
// cannot carry.
func ValidHost(host string) bool {
	if len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i, r := range label {
			isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
			if isAlnum {
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
