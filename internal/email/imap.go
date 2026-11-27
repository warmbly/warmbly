package email

import (
	"context"
	"crypto/tls"
	"errors"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/warmbly/warmbly/internal/client/mailtls"
	"github.com/warmbly/warmbly/internal/client/netbind"
	"github.com/warmbly/warmbly/internal/models"
)

// VerifyImap probes a mailbox's IMAP credentials the way the sync client
// connects: the caller's security mode decides implicit TLS versus STARTTLS.
// security may be empty, in which case the port convention decides. The
// result says why a probe failed, so a refused password and an unreachable
// host are not reported as the same thing.
func VerifyImap(ctx context.Context, host string, port int, user, pass, security string, policy mailtls.Policy) ProbeResult {
	// Brackets belong to the address, not to the host, and JoinHostPort is
	// what puts them back for an IPv6 literal.
	host = models.NormalizeMailHost(host)
	addr := models.MailDialAddress(host, port)

	resolved := models.ResolveIMAPSecurity(security, port)
	// The unencrypted mode only ever addresses this machine, checked here and
	// again against the peer below, so a mailbox that could never be dialled
	// safely fails at connect rather than on the first sync.
	if resolved == models.MailSecurityNone && !models.CleartextMailAllowed(host) {
		return probeFailText(models.MailProbeCleartext, "unencrypted IMAP is only allowed to a loopback host on a self-hosted instance")
	}

	// The send and sync clients bind WORKER_BIND_IP; a probe that left from
	// another address would vouch for a path the mailbox never uses.
	dialer := netbind.Dialer(netbind.FromEnv())
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return probeFail(ctx, models.MailProbeUnreachable, err)
	}
	defer conn.Close()
	if resolved == models.MailSecurityNone && !netbind.LoopbackPeer(conn) {
		return probeFailText(models.MailProbeCleartext, "the host did not resolve to this machine")
	}

	// The sync client's TLS policy, with the presented certificate captured
	// so the owner can be shown what to confirm.
	capture := &mailtls.Capture{}
	tlsConf := mailtls.Config(host, policy, capture)

	var c *imapclient.Client

	if resolved == models.MailSecurityNone {
		if err := conn.SetDeadline(time.Now().Add(probeConversation)); err != nil {
			return probeFail(ctx, models.MailProbeProtocol, err)
		}
		c = imapclient.New(conn, nil)
	} else if resolved == models.MailSecurityStartTLS {
		// The greeting arrives in cleartext and the upgrade happens in-band.
		if err := conn.SetDeadline(time.Now().Add(probeConversation)); err != nil {
			return probeFail(ctx, models.MailProbeProtocol, err)
		}
		c, err = imapclient.NewStartTLS(conn, &imapclient.Options{TLSConfig: tlsConf})
		if err != nil {
			return tlsFail(ctx, err, capture)
		}
	} else {
		tlsConn := tls.Client(conn, tlsConf)
		if err := tlsConn.SetDeadline(time.Now().Add(probeConversation)); err != nil {
			return probeFail(ctx, models.MailProbeProtocol, err)
		}
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return tlsFail(ctx, err, capture)
		}
		c = imapclient.New(tlsConn, nil)
	}

	defer c.Close()

	done := make(chan error, 1)
	go func() {
		done <- c.Login(user, pass).Wait()
	}()

	select {
	case err := <-done:
		// Classified before LOGOUT, which is not waited for: a server that
		// goes quiet after LOGIN would otherwise turn a refusal into a timeout.
		res := probeOK()
		if err != nil {
			res = probeFail(ctx, imapLoginReason(err), err)
		}
		res.Certificate = capture.Certificate(nil)
		c.Logout()
		return res
	case <-ctx.Done():
		return probeFail(ctx, models.MailProbeTimeout, ctx.Err())
	}
}

// imapLoginReason reads a LOGIN refusal. Only a tagged NO answers the
// credentials (AUTHENTICATIONFAILED, or Google's bare [ALERT] asking for an
// app password or a browser sign-in); a BAD is a command the server did not
// accept, and the codes that ask for a retry are temporary.
func imapLoginReason(err error) string {
	var ierr *imap.Error
	if !errors.As(err, &ierr) {
		return models.MailProbeProtocol
	}
	switch ierr.Type {
	case imap.StatusResponseTypeBad:
		return models.MailProbeProtocol
	case imap.StatusResponseTypeBye:
		return models.MailProbeTemporary
	}
	switch ierr.Code {
	case imap.ResponseCodeUnavailable, imap.ResponseCodeLimit, imap.ResponseCodeInUse:
		return models.MailProbeTemporary
	case imap.ResponseCodePrivacyRequired:
		return models.MailProbeProtocol
	}
	return models.MailProbeAuthRefused
}
