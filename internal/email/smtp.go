package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"time"
)

func VerifySMTP(ctx context.Context, host string, port int, user, pass string) bool {
	addr := fmt.Sprintf("%s:%d", host, port)

	var conn net.Conn
	var err error

	switch port {
	case 465:
		dialer := &tls.Dialer{
			NetDialer: &net.Dialer{Timeout: 5 * time.Second},
			Config:    &tls.Config{ServerName: host},
		}
		conn, err = dialer.DialContext(ctx, "tcp", addr)

	case 587:
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		conn, err = dialer.DialContext(ctx, "tcp", addr)

	default:
		return false
	}
	defer conn.Close()

	var c *smtp.Client
	switch port {
	case 465:
		c, err = smtp.NewClient(conn, host)
	case 587:
		c, err = smtp.NewClient(conn, host)
		if err == nil {
			if err = c.StartTLS(&tls.Config{ServerName: host}); err != nil {
				return false
			}
		}
	}
	if err != nil {
		return false
	}
	defer c.Close()

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
