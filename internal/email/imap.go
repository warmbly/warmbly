package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
)

func VerifyImap(ctx context.Context, host string, port int, user, pass string) bool {
	addr := fmt.Sprintf("%s:%d", host, port)

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	defer conn.Close()

	tlsConn := tls.Client(conn, &tls.Config{ServerName: host})
	if err := tlsConn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return false
	}
	if err := tlsConn.Handshake(); err != nil {
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
