package email

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/client/mailtls"
	"github.com/warmbly/warmbly/internal/client/netbind"
	wsmtp "github.com/warmbly/warmbly/internal/client/smtpimap/smtp"
	"github.com/warmbly/warmbly/internal/models"
)

// probeConversation bounds the SMTP conversation when the context carries no
// deadline of its own. Some hosted Microsoft relays (sending.ac) take up to 15
// seconds to sign a mailbox in, so it leaves room for that. The IMAP probe
// uses the same bound.
const probeConversation = 25 * time.Second

// VerifySMTP probes a mailbox's SMTP credentials the same way the send path
// connects: the caller's security mode decides implicit TLS versus STARTTLS,
// and any port is accepted. security may be empty, in which case the port
// convention decides. The result says why a probe failed, so a refused
// password and an unreachable host are not reported as the same thing.
func VerifySMTP(ctx context.Context, host string, port int, user, pass, security string, policy mailtls.Policy) ProbeResult {
	// Brackets belong to the address, not to the host, and JoinHostPort is
	// what puts them back for an IPv6 literal.
	host = models.NormalizeMailHost(host)

	// The send client's TLS policy, with the presented certificate captured
	// so the owner can be shown what to confirm.
	capture := &mailtls.Capture{}
	tlsConf := mailtls.Config(host, policy, capture)

	// The unencrypted mode only ever addresses this machine. Refusing it here
	// as well as at send time means a mailbox that could never be dialled
	// safely fails at connect, where the user is standing in front of the
	// form, rather than at the first send.
	resolved := models.ResolveSMTPSecurity(security, port)
	if resolved == models.MailSecurityNone && !models.CleartextMailAllowed(host) {
		return probeFailText(models.MailProbeCleartext, "unencrypted SMTP is only allowed to a loopback host on a self-hosted instance")
	}
	// TCP first and TLS second, like the IMAP probe, so a port that answers
	// and then fails the handshake reads as a TLS problem, not as unreachable.
	// The dial leaves from WORKER_BIND_IP like the sends it vouches for, and
	// comes back holding 587 when the mailbox's 465 never answered.
	dialed, err := wsmtp.DialSubmission(ctx, netbind.FromEnv(), host, port, security)
	if errors.Is(err, wsmtp.ErrSMTPSEgressBlocked) {
		return probeFailText(models.MailProbeUnreachable, "the worker's network blocks outbound port 465 and the server did not answer on 587, so the server itself was never reached")
	}
	// A bad host is ordinary user input, not an exceptional case: dial failed
	// means conn is nil, and closing it would panic this goroutine and take
	// the whole worker down with it.
	if err != nil || dialed.Conn == nil {
		if err == nil {
			err = errors.New("dial returned no connection")
		}
		return probeFail(ctx, models.MailProbeUnreachable, err)
	}
	conn := dialed.Conn
	defer conn.Close()
	if dialed.Security == models.MailSecurityNone && !netbind.LoopbackPeer(conn) {
		return probeFailText(models.MailProbeCleartext, "the host did not resolve to this machine")
	}
	// The greeting, EHLO and STARTTLS read with no deadline of their own, so a
	// server that accepts and never speaks would park this goroutine, and the
	// handler waiting on it, for good.
	deadline := time.Now().Add(probeConversation)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return probeFail(ctx, models.MailProbeProtocol, err)
	}
	res := verifySMTPSession(ctx, conn, dialed.Security, host, user, pass, tlsConf, capture)
	if dialed.FellBack {
		res.Port, res.Security = dialed.Port, dialed.Security
	}
	return res
}

// verifySMTPSession runs the sign-in on an open socket in the given mode.
func verifySMTPSession(ctx context.Context, conn net.Conn, resolved, host, user, pass string, tlsConf *tls.Config, capture *mailtls.Capture) (res ProbeResult) {
	defer func() {
		if res.Certificate == nil {
			res.Certificate = capture.Certificate(nil)
		}
	}()
	implicitTLS := resolved == models.MailSecurityTLS
	if implicitTLS {
		tlsConn := tls.Client(conn, tlsConf)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return tlsFail(ctx, err, capture)
		}
		conn = tlsConn
	}

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return probeFail(ctx, models.MailProbeProtocol, err)
	}
	defer c.Close()
	// Explicit EHLO: Extension() swallows a failed greeting exchange and then
	// reports no AUTH, which read as a mailbox with nothing to verify.
	if err := c.Hello(ehloName(user)); err != nil {
		return probeFail(ctx, models.MailProbeProtocol, err)
	}

	if !implicitTLS && resolved != models.MailSecurityNone {
		// TLS stays mandatory, with the same development-only escape hatch the
		// send path uses for the local no-STARTTLS sink.
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsConf); err != nil {
				return tlsFail(ctx, err, capture)
			}
		} else if !netbind.InsecureTLS() {
			return probeFailText(models.MailProbeTLS, "the server offers no STARTTLS on this port, and Warmbly only signs in over an encrypted connection. Use the server's SSL/TLS port (usually 465) or a port that offers STARTTLS (usually 587)")
		}
	}

	// Negotiated from what the server advertised and the link it is on, like
	// the send path: a server that offers only LOGIN refuses a blind AUTH
	// PLAIN, and no password mechanism runs outside TLS or loopback.
	auth, aerr := wsmtp.NegotiateAuth(c, user, pass, host)
	if errors.Is(aerr, wsmtp.ErrSMTPCleartextAuth) {
		return probeFailText(models.MailProbeCleartext, "the server only offers sign-in over an unencrypted connection, and Warmbly never sends a password unencrypted. Use the server's SSL/TLS port (usually 465) or a port that offers STARTTLS (usually 587)")
	}
	if errors.Is(aerr, wsmtp.ErrSMTPNoSupportedAuth) {
		return probeFailText(models.MailProbeProtocol, noSupportedAuthDetail(c))
	}
	if aerr != nil {
		return probeFail(ctx, models.MailProbeProtocol, aerr)
	}
	if auth == nil {
		// No AUTH offered at all: nothing to verify, and the send path will
		// not authenticate either.
		return probeOK()
	}

	done := make(chan error, 1)
	go func() { done <- c.Auth(auth) }()

	select {
	case err := <-done:
		if err == nil {
			return probeOK()
		}
		return probeFail(ctx, smtpAuthReason(err), err)
	case <-ctx.Done():
		return probeFail(ctx, models.MailProbeTimeout, ctx.Err())
	}
}

// smtpAuthReason reads an AUTH refusal by its reply code. Only the two codes
// that answer the credentials themselves are a refusal: 535 (RFC 4954) and
// 534, which Google uses for "application-specific password required" and
// "log in via your web browser". A 4xx asks to come back later; any other
// 5xx (504 mechanism unsupported, 530 must STARTTLS, 538 encryption
// required) is the conversation failing, not the password.
func smtpAuthReason(err error) string {
	var te *textproto.Error
	if !errors.As(err, &te) {
		if errors.Is(err, wsmtp.ErrSMTPCleartextAuth) {
			return models.MailProbeCleartext
		}
		return models.MailProbeProtocol
	}
	switch {
	case te.Code == 534 || te.Code == 535:
		return models.MailProbeAuthRefused
	case te.Code >= 500:
		return models.MailProbeProtocol
	default:
		return models.MailProbeTemporary
	}
}

// ehloName announces the sender's own domain, as the send path does.
func ehloName(user string) string {
	if at := strings.LastIndexByte(user, '@'); at >= 0 && at < len(user)-1 {
		return user[at+1:]
	}
	return "localhost"
}

// noSupportedAuthDetail names the sign-in methods the server offered when none is one Warmbly speaks.
func noSupportedAuthDetail(c *smtp.Client) string {
	detail := "the server offers no sign-in method allowed by this instance (use LOGIN or PLAIN over TLS)"
	if _, offered := c.Extension("AUTH"); offered != "" {
		detail = "the server offers no sign-in method allowed by this instance: it offers " + probeDetail(errors.New(offered)) + "; use LOGIN or PLAIN over TLS"
	}
	return detail + ". Use the provider's documented SMTP submission host, which usually offers one of them"
}
