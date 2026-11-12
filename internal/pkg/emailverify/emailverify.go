// Package emailverify performs pre-send email verification so the platform can
// drop addresses that would hard-bounce *before* a worker ever sends to them.
// Today suppression is purely reactive (we only suppress after a bounce/complaint
// lands), which means every bad address costs us one real bounce against our
// sending reputation. Verifying up front turns that into a zero-bounce drop.
//
// CONTROL-PLANE ONLY. This package dials remote MX hosts on :25 to run an SMTP
// RCPT probe. That probe MUST run from the backend/consumer (a dedicated,
// non-sending IP), never from a worker. Workers are our sending IPs, and running
// RCPT probes from them pollutes the exact reputation this feature exists to
// protect — recipient servers treat probe traffic from a sending IP as
// suspicious, and a probe that gets greylisted/tarpitted ties up a sending
// connection. Keep all probing on the control plane.
//
// Operational caveats baked into the design:
//   - Many cloud providers (AWS, GCP, most consumer ISPs) block *outbound* :25.
//     On such a host the SMTP probe will always time out and every address
//     degrades to Status "unknown". Run the prober from a host with open
//     outbound :25, or plug in a paid backend (see below).
//   - Greylisting is normal and correct server behaviour: a first-contact RCPT
//     often gets a 4xx "try again later". We deliberately map 4xx -> "unknown"
//     (never "invalid") so greylisting can't cause us to drop a real contact.
//   - Catch-all domains accept *every* localpart, so a 250 on the real address
//     proves nothing. We detect catch-all by probing a random localpart; if that
//     is also accepted the real address is downgraded to "risky", not "valid".
//
// For production-grade accuracy the Verifier interface is intentionally the only
// contract the rest of the platform depends on. A paid provider (ZeroBounce,
// NeverBounce, Bouncer, etc.) can be dropped in as an alternate Verifier
// implementation without touching the service, repo, scheduler, or handler.
package emailverify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/safehttp"
	"github.com/warmbly/warmbly/internal/pkg/signuprisk"
)

// Status is the verification outcome for a single address. It is a small closed
// set so callers (and the DB column) can reason about it without parsing free
// text.
type Status string

const (
	// StatusValid means syntax + MX + an accepted RCPT on a non-catch-all domain.
	StatusValid Status = "valid"
	// StatusRisky means deliverable-looking but unsafe to trust — primarily a
	// catch-all domain where acceptance proves nothing.
	StatusRisky Status = "risky"
	// StatusInvalid means a hard failure: bad syntax, no MX, or a 550-class RCPT
	// rejection. These are the addresses the pre-send gate should drop.
	StatusInvalid Status = "invalid"
	// StatusUnknown means we couldn't determine deliverability — timeout, 4xx
	// greylisting, blocked outbound :25, or any transient/ambiguous condition.
	// Unknown is never dropped; it is retried or sent cautiously.
	StatusUnknown Status = "unknown"
)

// SubStatus refines a Status with the reason class a paid provider or the
// in-house checks can name. Empty when nothing more specific is known.
type SubStatus string

const (
	SubStatusNone        SubStatus = ""
	SubStatusCatchAll    SubStatus = "catch_all"
	SubStatusDisposable  SubStatus = "disposable"
	SubStatusRole        SubStatus = "role"
	SubStatusSpamTrap    SubStatus = "spamtrap"
	SubStatusMailboxFull SubStatus = "mailbox_full"
	SubStatusNoMX        SubStatus = "no_mx"
	SubStatusSyntax      SubStatus = "syntax"
	// SubStatusUndisclosed marks a provider (Microsoft, Yahoo) that answers
	// every RCPT the same way, so an SMTP probe cannot judge the mailbox.
	SubStatusUndisclosed SubStatus = "undisclosed"
)

// Provider names for Result.Provider and contacts.verification_provider.
const (
	ProviderBuiltin         = "builtin"
	ProviderMillionVerifier = "millionverifier"
	ProviderCleanMyList     = "cleanmylist"
	ProviderMailVeri        = "mailveri"
)

// Result is the outcome of verifying one address. It round-trips into the
// contacts table (verification_status / verification_reason / is_catch_all /
// verification_checked_at / verification_sub_status / verification_provider).
type Result struct {
	Email      string    `json:"email"`
	Status     Status    `json:"status"`
	SubStatus  SubStatus `json:"sub_status,omitempty"`
	Reason     string    `json:"reason"`
	IsCatchAll bool      `json:"is_catch_all"`
	HasMX      bool      `json:"has_mx"`
	// Provider is who produced the verdict: ProviderBuiltin, a paid backend,
	// or the vocabulary an imported result was recognised as.
	Provider string `json:"provider,omitempty"`
	// Confidence is the scored certainty of Status once evidence is applied.
	Confidence int       `json:"confidence,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
}

// Verifier is the single contract the rest of the platform depends on. The
// in-house SMTPVerifier implements it; a paid provider can implement the same
// interface and be swapped in at wiring time with no other code changes.
type Verifier interface {
	Verify(ctx context.Context, email string) Result
}

// Config tunes the in-house SMTP prober. HeloHost is the hostname sent in
// EHLO/HELO and MailFrom is the envelope sender used in MAIL FROM. Both should
// be real, attributable values for the verifying (non-sending) host so remote
// servers see consistent, non-spoofed probe identity. Timeouts default to sane
// values when zero.
type Config struct {
	// HeloHost is the hostname announced in EHLO/HELO, e.g. "verify.warmbly.com".
	HeloHost string
	// MailFrom is the envelope sender for the probe, e.g. "verify@warmbly.com".
	// An empty MAIL FROM ("<>") is technically valid but more often filtered, so
	// a real address on the verifying host is preferred.
	MailFrom string
	// DialTimeout bounds the TCP connect to a single MX host. Default 5s.
	DialTimeout time.Duration
	// CommandTimeout bounds each SMTP command exchange. Default 5s.
	CommandTimeout time.Duration
	// MXTimeout bounds the MX DNS lookup. Default 5s.
	MXTimeout time.Duration
}

func (c Config) withDefaults() Config {
	// No fallback HELO name. A bare "localhost" is rejected by RFC-compliant
	// servers, and Postfix reports that rejection on RCPT, which the prober
	// used to read as "this recipient does not exist" (issue #200). An
	// unconfigured verifier declines to probe instead of guessing.
	c.HeloHost = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(c.HeloHost)), ".")
	if c.MailFrom == "" && isFQDN(c.HeloHost) {
		c.MailFrom = "verify@" + c.HeloHost
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = 5 * time.Second
	}
	if c.CommandTimeout <= 0 {
		c.CommandTimeout = 5 * time.Second
	}
	if c.MXTimeout <= 0 {
		c.MXTimeout = 5 * time.Second
	}
	return c
}

// ProbeReady reports whether the configured HELO host lets the SMTP probe run.
func (v *SMTPVerifier) ProbeReady() bool { return isFQDN(v.cfg.HeloHost) }

// SMTPVerifier is the in-house Verifier: syntax -> MX -> SMTP RCPT probe ->
// catch-all detection. It opens exactly one connection to the lowest-preference
// MX and probes both the real address and a random localpart on the same
// session, so catch-all detection costs no extra connection.
type SMTPVerifier struct {
	cfg      Config
	resolver *net.Resolver
	// smtpPort is always "25" in production (MX hosts listen nowhere else);
	// it exists so tests can point probe() at a local server.
	smtpPort string
	// allowPrivateMX lets probe() dial a non-public address. False everywhere
	// but in tests, which run their fake MX on loopback. In production an MX
	// that resolves to a private or link-local address is a user-controlled DNS
	// record pointing at our own network, which is the whole SSRF shape.
	allowPrivateMX bool
	// domains remembers per-domain facts (no MX, catch-all, undisclosing
	// provider) so a 50k list at 2k domains costs 2k probes, not 50k.
	domains *domainCache
}

// New constructs the in-house SMTP verifier. The resolver mirrors dnsauth's
// net.Resolver usage (context-bounded lookups, no custom dialer needed).
func New(cfg Config) *SMTPVerifier {
	return &SMTPVerifier{
		cfg:      cfg.withDefaults(),
		resolver: &net.Resolver{},
		smtpPort: "25",
		domains:  newDomainCache(domainCacheTTL),
	}
}

// Verify runs the full pipeline for one address. It never returns an error; an
// undeterminable result is encoded as StatusUnknown so callers have a single
// code path.
func (v *SMTPVerifier) Verify(ctx context.Context, email string) Result {
	now := time.Now().UTC()
	res := Result{Email: email, CheckedAt: now, Status: StatusUnknown, Provider: ProviderBuiltin}

	// 1. Syntax (RFC 5322-ish via net/mail). A parse failure is a hard invalid.
	addr, err := mail.ParseAddress(email)
	if err != nil {
		res.Status = StatusInvalid
		res.SubStatus = SubStatusSyntax
		res.Reason = "invalid syntax"
		return res
	}
	normalized := strings.ToLower(strings.TrimSpace(addr.Address))
	res.Email = normalized
	at := strings.LastIndex(normalized, "@")
	if at <= 0 || at == len(normalized)-1 {
		res.Status = StatusInvalid
		res.SubStatus = SubStatusSyntax
		res.Reason = "invalid syntax"
		return res
	}
	localpart := normalized[:at]
	domain := normalized[at+1:]

	// Address-level facts that need no network. A disposable domain is never
	// worth a send; a role address is deliverable but flagged, so the campaign's
	// risky toggle decides.
	if signuprisk.IsDisposable(normalized) {
		res.Status = StatusInvalid
		res.SubStatus = SubStatusDisposable
		res.Reason = "disposable email domain"
		return res
	}
	role := isRoleLocalpart(localpart)

	// 2. Domain facts, cached: no MX and catch-all are properties of the
	// domain, and providers that never disclose mailboxes are known by MX.
	if cached, ok := v.domains.get(domain); ok {
		switch cached.kind {
		case domainNoMX:
			res.Status = StatusInvalid
			res.SubStatus = SubStatusNoMX
			res.Reason = "no MX records"
			return res
		case domainCatchAll:
			res.HasMX = true
			res.IsCatchAll = true
			res.Status = StatusRisky
			res.SubStatus = SubStatusCatchAll
			res.Reason = "catch-all domain; acceptance is not conclusive"
			return res
		case domainUndisclosed:
			res.HasMX = true
			res.Status = StatusUnknown
			res.SubStatus = SubStatusUndisclosed
			res.Reason = cached.reason
			return res
		}
	}

	hosts, mxErr := v.lookupMXHosts(ctx, domain)
	if mxErr != nil {
		res.Status = StatusUnknown
		res.Reason = "mx lookup failed: " + mxErr.Error()
		return res
	}
	if len(hosts) == 0 {
		v.domains.put(domain, domainFact{kind: domainNoMX})
		res.Status = StatusInvalid
		res.SubStatus = SubStatusNoMX
		res.Reason = "no MX records"
		return res
	}
	res.HasMX = true

	// Providers that accept every RCPT and bounce later (Microsoft 365,
	// Yahoo) make a probe meaningless: say so instead of spending a
	// connection and calling the result "valid".
	if fp := fingerprintMX(hosts); fp.undisclosed {
		reason := fp.name + " does not disclose mailboxes to an SMTP probe"
		v.domains.put(domain, domainFact{kind: domainUndisclosed, reason: reason})
		res.Status = StatusUnknown
		res.SubStatus = SubStatusUndisclosed
		res.Reason = reason
		return res
	}

	// 3. SMTP RCPT probe against the lowest-preference (highest priority) MX.
	// Refused outright without a real HELO identity: a probe that announces a
	// bare or reserved name gets the SESSION rejected, and that rejection
	// arrives on RCPT looking exactly like a dead mailbox.
	if !isFQDN(v.cfg.HeloHost) {
		res.Status = StatusUnknown
		res.Reason = "smtp probe skipped: set EMAIL_VERIFY_HELO_HOST to a public fully-qualified hostname for this instance"
		return res
	}
	probe := v.probeHosts(ctx, hosts, localpart, domain)
	switch probe.outcome {
	case probeAccepted:
		// 4. Catch-all check already folded into probe(): if the random control
		// localpart was also accepted, the 250 on the real address is meaningless.
		if probe.catchAll {
			v.domains.put(domain, domainFact{kind: domainCatchAll})
			res.IsCatchAll = true
			res.Status = StatusRisky
			res.SubStatus = SubStatusCatchAll
			res.Reason = "catch-all domain; acceptance is not conclusive"
			return res
		}
		if role {
			res.Status = StatusRisky
			res.SubStatus = SubStatusRole
			res.Reason = "role address (shared inbox); recipient accepted"
			return res
		}
		res.Status = StatusValid
		res.Reason = "recipient accepted"
		return res
	case probeRejected:
		res.Status = StatusInvalid
		res.Reason = probe.reason
		return res
	default: // probeUnknown
		res.Status = StatusUnknown
		res.Reason = probe.reason
		return res
	}
}

// probeHosts tries the MX hosts in preference order until one answers the
// session. A dial or handshake failure on the primary is common (it is the
// busiest host) and says nothing about the mailbox; only a host that talked
// to us gets to decide.
func (v *SMTPVerifier) probeHosts(ctx context.Context, hosts []string, localpart, domain string) probeResult {
	var last probeResult
	for i, host := range hosts {
		if i >= maxMXAttempts {
			break
		}
		if err := ctx.Err(); err != nil {
			return probeResult{outcome: probeUnknown, reason: "cancelled: " + err.Error()}
		}
		last = v.probe(ctx, host, localpart, domain)
		if last.outcome != probeUnknown || !last.sessionFailed {
			return last
		}
	}
	return last
}

// lookupMXHosts returns MX hosts ordered by ascending preference (most-preferred
// first). When a domain publishes no MX, RFC 5321 permits implicit-MX fallback
// to the A/AAAA record of the domain itself; we honour that so apex-only mail
// domains aren't misjudged as invalid.
func (v *SMTPVerifier) lookupMXHosts(ctx context.Context, domain string) ([]string, error) {
	c, cancel := context.WithTimeout(ctx, v.cfg.MXTimeout)
	defer cancel()

	mxs, err := v.resolver.LookupMX(c, domain)
	if err != nil {
		// A "no such host" / "no MX" style miss is not a transport error; treat
		// it as "no MX" and let the implicit-MX fallback below decide.
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && (dnsErr.IsNotFound || dnsErr.Err == "no such host") {
			mxs = nil
		} else {
			return nil, err
		}
	}

	if len(mxs) > 0 {
		sort.SliceStable(mxs, func(i, j int) bool { return mxs[i].Pref < mxs[j].Pref })
		hosts := make([]string, 0, len(mxs))
		for _, mx := range mxs {
			h := strings.TrimSuffix(strings.TrimSpace(mx.Host), ".")
			if h != "" {
				hosts = append(hosts, h)
			}
		}
		if len(hosts) > 0 {
			return hosts, nil
		}
	}

	// Implicit MX: fall back to the domain's own A/AAAA if it resolves.
	ac, acancel := context.WithTimeout(ctx, v.cfg.MXTimeout)
	defer acancel()
	if addrs, aerr := v.resolver.LookupHost(ac, domain); aerr == nil && len(addrs) > 0 {
		return []string{domain}, nil
	}
	return nil, nil
}

type probeOutcome int

const (
	probeUnknown probeOutcome = iota
	probeAccepted
	probeRejected
)

type probeResult struct {
	outcome  probeOutcome
	catchAll bool
	reason   string
	// sessionFailed is set when the host never judged the address (dial or
	// handshake failure), so the next MX is worth trying.
	sessionFailed bool
}

// probe opens one SMTP session to host:25, greets it, sets the envelope sender,
// and issues RCPT TO for the real address. If the real address is accepted it
// also issues RCPT TO for a random control localpart on the same domain to
// detect catch-all behaviour. Interpretation:
//
//	250          -> accepted
//	5xx          -> rejected ONLY when the reply names the recipient; a 5xx
//	               that rejects our HELO / sender / IP / relay policy is unknown
//	4xx / timeout/ dial error -> unknown (greylist, blocked :25, transient)
func (v *SMTPVerifier) probe(ctx context.Context, host, localpart, domain string) probeResult {
	// The host is an MX name resolved from an address the user typed, so where
	// it points is the user's choice. Publishing `MX 169.254.169.254` or
	// `MX 127.0.0.1` for a domain you control turns "verify this address" into
	// a connect to the instance's own network. Resolve first, refuse anything
	// that is not a public address, then dial the address that was checked so
	// a second lookup cannot answer differently.
	ips, rerr := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if rerr != nil || len(ips) == 0 {
		return probeResult{outcome: probeUnknown, sessionFailed: true, reason: "mx lookup failed"}
	}
	if !v.allowPrivateMX {
		for _, ip := range ips {
			if safehttp.IsBlockedIP(ip) {
				return probeResult{outcome: probeUnknown, sessionFailed: true, reason: "mx resolves to a non-public address"}
			}
		}
	}

	// Try each address rather than only the first. LookupIP returns A and AAAA
	// mixed, so on an IPv4-only host a domain whose first record is AAAA would
	// always report unknown, which is a silent accuracy loss rather than an
	// error anyone would notice.
	dialer := net.Dialer{Timeout: v.cfg.DialTimeout}
	var conn net.Conn
	var err error
	for _, ip := range ips {
		conn, err = dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), v.smtpPort))
		if err == nil {
			break
		}
	}
	if err != nil {
		// Most commonly: outbound :25 blocked by the cloud provider, or the MX is
		// firewalled/tarpitting. Either way we cannot conclude invalid.
		return probeResult{outcome: probeUnknown, sessionFailed: true, reason: "smtp dial failed (port 25 may be blocked): " + err.Error()}
	}
	// Bound the whole session.
	deadline := time.Now().Add(v.cfg.CommandTimeout * 4)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return probeResult{outcome: probeUnknown, sessionFailed: true, reason: "smtp handshake failed: " + err.Error()}
	}
	defer func() { _ = client.Close() }()

	if err := client.Hello(v.cfg.HeloHost); err != nil {
		return probeResult{outcome: probeUnknown, reason: "EHLO/HELO rejected: " + err.Error()}
	}
	if err := client.Mail(v.cfg.MailFrom); err != nil {
		return probeResult{outcome: probeUnknown, reason: "MAIL FROM rejected: " + err.Error()}
	}

	realOutcome, realReason := classifyRcpt(client.Rcpt(localpart + "@" + domain))
	switch realOutcome {
	case probeRejected:
		return probeResult{outcome: probeRejected, reason: realReason}
	case probeUnknown:
		return probeResult{outcome: probeUnknown, reason: realReason}
	}

	// Real address accepted — probe a random control localpart to detect a
	// catch-all. If that is also accepted, the domain accepts everything and the
	// real 250 proves nothing.
	control := randomLocalpart()
	controlOutcome, _ := classifyRcpt(client.Rcpt(control + "@" + domain))

	return probeResult{outcome: probeAccepted, catchAll: controlOutcome == probeAccepted}
}

// classifyRcpt maps the error from smtp.Client.Rcpt to a probe outcome. The
// stdlib surfaces the SMTP reply code on *textproto.Error; 4xx is transient
// (unknown), a nil error is acceptance, and 5xx is handed to
// classifyPermanentRcpt, which only calls it a rejection when the reply talks
// about the recipient rather than about our probe.
func classifyRcpt(err error) (probeOutcome, string) {
	if err == nil {
		return probeAccepted, "recipient accepted"
	}
	var protoErr *textproto.Error
	if errors.As(err, &protoErr) {
		code := protoErr.Code
		switch {
		case code >= 500 && code < 600:
			// A 5xx here may reject the RECIPIENT or reject our probe; only the
			// former may become a permanent "invalid" verdict.
			return classifyPermanentRcpt(code, protoErr.Msg)
		case code >= 400 && code < 500:
			return probeUnknown, "transient/greylisted (" + strconv.Itoa(code) + "): " + protoErr.Msg
		}
	}
	// Connection reset, timeout mid-command, or an unparseable reply.
	return probeUnknown, "rcpt indeterminate: " + err.Error()
}

func randomLocalpart() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// Fall back to a fixed-but-unlikely localpart; the worst case is a
		// false-negative catch-all check, never a wrong invalid verdict.
		return "no-such-mailbox-warmbly-probe"
	}
	return "wb-verify-" + hex.EncodeToString(b)
}
