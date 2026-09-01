package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"

	"github.com/warmbly/warmbly/internal/client/netbind"
)

func VerifySMTP(ctx context.Context, host string, port int, user, pass string) bool {
	addr := fmt.Sprintf("%s:%d", host, port)

	var conn net.Conn
	var err error

	// Matches the send client's TLS policy: MAIL_TLS_INSECURE is a dev-only
	// knob for the local self-signed sandbox, never set in production.
	tlsConf := &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: netbind.InsecureTLS(),
	}

	// netbind dialers so validation probes leave from WORKER_BIND_IP exactly
	// like the sends they are vouching for.
	switch port {
	case 465:
		conn, err = netbind.TLSDialer(nil, tlsConf).DialContext(ctx, "tcp", addr)

	case 587:
		conn, err = netbind.Dialer(nil).DialContext(ctx, "tcp", addr)

	default:
		return false
	}
	// A bad host is ordinary user input, not an exceptional case: dial failed
	// means conn is nil, and closing it would panic this goroutine and take
	// the whole worker down with it.
	if err != nil || conn == nil {
		return false
	}
	defer conn.Close()

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return false
	}
	defer c.Close()

	if port == 587 {
		if err := c.StartTLS(tlsConf); err != nil {
			return false
		}
	}

	auth := smtp.PlainAuth("", user, pass, host)

	done := make(chan error, 1)
	go func() { done <- c.Auth(auth) }()

	select {
	case err := <-done:
		return err == nil
	case <-ctx.Done():
		return false
	}
}
