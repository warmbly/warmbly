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

// Result is the outcome of verifying one address. It round-trips into the
// contacts table (verification_status / verification_reason / is_catch_all /
// verification_checked_at).
type Result struct {
	Email      string    `json:"email"`
	Status     Status    `json:"status"`
	Reason     string    `json:"reason"`
	IsCatchAll bool      `json:"is_catch_all"`
	HasMX      bool      `json:"has_mx"`
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
	if c.HeloHost == "" {
		c.HeloHost = "localhost"
	}
	if c.MailFrom == "" {
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

// SMTPVerifier is the in-house Verifier: syntax -> MX -> SMTP RCPT probe ->
// catch-all detection. It opens exactly one connection to the lowest-preference
// MX and probes both the real address and a random localpart on the same
// session, so catch-all detection costs no extra connection.
type SMTPVerifier struct {
	cfg      Config
	resolver *net.Resolver
}

// New constructs the in-house SMTP verifier. The resolver mirrors dnsauth's
// net.Resolver usage (context-bounded lookups, no custom dialer needed).
func New(cfg Config) *SMTPVerifier {
	return &SMTPVerifier{
		cfg:      cfg.withDefaults(),
		resolver: &net.Resolver{},
	}
}

// Verify runs the full pipeline for one address. It never returns an error; an
// undeterminable result is encoded as StatusUnknown so callers have a single
// code path.
func (v *SMTPVerifier) Verify(ctx context.Context, email string) Result {
	now := time.Now().UTC()
	res := Result{Email: email, CheckedAt: now, Status: StatusUnknown}

	// 1. Syntax (RFC 5322-ish via net/mail). A parse failure is a hard invalid.
	addr, err := mail.ParseAddress(email)
	if err != nil {
		res.Status = StatusInvalid
		res.Reason = "invalid syntax"
		return res
	}
	normalized := strings.ToLower(strings.TrimSpace(addr.Address))
	res.Email = normalized
	at := strings.LastIndex(normalized, "@")
	if at <= 0 || at == len(normalized)-1 {
		res.Status = StatusInvalid
		res.Reason = "invalid syntax"
		return res
	}
	localpart := normalized[:at]
	domain := normalized[at+1:]

	// 2. MX lookup. No MX (and no usable fallback) is a hard invalid: nowhere to
	// deliver. A lookup *error* (timeout/SERVFAIL) is unknown, not invalid.
	hosts, mxErr := v.lookupMXHosts(ctx, domain)
	if mxErr != nil {
		res.Status = StatusUnknown
		res.Reason = "mx lookup failed: " + mxErr.Error()
		return res
	}
	if len(hosts) == 0 {
		res.Status = StatusInvalid
		res.Reason = "no MX records"
		return res
	}
	res.HasMX = true

	// 3. SMTP RCPT probe against the lowest-preference (highest priority) MX.
	probe := v.probe(ctx, hosts[0], localpart, domain)
	switch probe.outcome {
	case probeAccepted:
		// 4. Catch-all check already folded into probe(): if the random control
		// localpart was also accepted, the 250 on the real address is meaningless.
		if probe.catchAll {
			res.IsCatchAll = true
			res.Status = StatusRisky
			res.Reason = "catch-all domain; acceptance is not conclusive"
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
}

// probe opens one SMTP session to host:25, greets it, sets the envelope sender,
// and issues RCPT TO for the real address. If the real address is accepted it
// also issues RCPT TO for a random control localpart on the same domain to
// detect catch-all behaviour. Interpretation:
//
//	250          -> accepted
//	550 (5xx)    -> rejected (hard invalid)
//	4xx / timeout/ dial error -> unknown (greylist, blocked :25, transient)
func (v *SMTPVerifier) probe(ctx context.Context, host, localpart, domain string) probeResult {
	dialer := net.Dialer{Timeout: v.cfg.DialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, "25"))
	if err != nil {
		// Most commonly: outbound :25 blocked by the cloud provider, or the MX is
		// firewalled/tarpitting. Either way we cannot conclude invalid.
		return probeResult{outcome: probeUnknown, reason: "smtp dial failed (port 25 may be blocked): " + err.Error()}
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
		return probeResult{outcome: probeUnknown, reason: "smtp handshake failed: " + err.Error()}
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
// stdlib surfaces the SMTP reply code on *textproto.Error; 5xx is a hard
// rejection, 4xx is transient (unknown), and a nil error is acceptance.
func classifyRcpt(err error) (probeOutcome, string) {
	if err == nil {
		return probeAccepted, "recipient accepted"
	}
	var protoErr *textproto.Error
	if errors.As(err, &protoErr) {
		code := protoErr.Code
		switch {
		case code >= 500 && code < 600:
			return probeRejected, "recipient rejected (" + strconv.Itoa(code) + "): " + protoErr.Msg
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
