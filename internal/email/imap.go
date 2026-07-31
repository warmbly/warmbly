package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/warmbly/warmbly/internal/client/netbind"
)

func VerifyImap(ctx context.Context, host string, port int, user, pass string) bool {
	addr := fmt.Sprintf("%s:%d", host, port)

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	defer conn.Close()

	// Matches the sync client's TLS policy: MAIL_TLS_INSECURE is a dev-only
	// knob for the local self-signed sandbox, never set in production. Without
	// it, validation rejects mailboxes the worker would go on to sync fine.
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: netbind.InsecureTLS(),
	})
	if err := tlsConn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return false
	}
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return false
	}

	var c *imapclient.Client

	c = imapclient.New(tlsConn, nil)
	defer func() {
		_ = c.Logout().Wait()
		_ = c.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- c.Login(user, pass).Wait()
	}()

	select {
	case err := <-done:
		return err == nil
	case <-ctx.Done():
		return false
	}
}
