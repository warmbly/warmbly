package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

const smtpSendTimeout = 10 * time.Second

type smtpEmailNotificationService struct {
	Name     string
	Address  string
	Host     string
	Port     string
	Username string
	Password string
}

func NewSMTPEmailNotificationService(name, address, host, port string, credentials ...string) EmailNotificationService {
	service := &smtpEmailNotificationService{
		Name:    name,
		Address: address,
		Host:    host,
		Port:    port,
	}
	if len(credentials) >= 2 {
		service.Username = credentials[0]
		service.Password = credentials[1]
	}
	return service
}

func (s *smtpEmailNotificationService) Send(ctx context.Context, to, cc, bcc []string, subject, message string) error {
	from := fmt.Sprintf("%s <%s>", s.Name, s.Address)
	addr := net.JoinHostPort(s.Host, s.Port)

	allRecipients := make([]string, 0, len(to)+len(cc)+len(bcc))
	allRecipients = append(allRecipients, to...)
	allRecipients = append(allRecipients, cc...)
	allRecipients = append(allRecipients, bcc...)

	headers := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=\"UTF-8\"\r\n\r\n",
		from,
		strings.Join(to, ", "),
		subject,
	)

	msg := []byte(headers + message)

	if err := sendSMTP(ctx, addr, s.Host, s.Address, allRecipients, msg, s.Username, s.Password); err != nil {
		sentry.CaptureException(err)
		return err
	}

	return nil
}

// SendOutreach is Send with an explicit Reply-To. The SMTP transport
// doesn't have a structured envelope for this so we forge the header
// directly into the message.
func (s *smtpEmailNotificationService) SendOutreach(ctx context.Context, to []string, replyTo, subject, message string) error {
	from := fmt.Sprintf("%s <%s>", s.Name, s.Address)
	addr := net.JoinHostPort(s.Host, s.Port)

	headers := "From: " + from + "\r\n" +
		"To: " + strings.Join(to, ", ") + "\r\n" +
		"Subject: " + subject + "\r\n"
	if replyTo != "" {
		headers += "Reply-To: " + replyTo + "\r\n"
	}
	headers += "MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=\"UTF-8\"\r\n\r\n"

	msg := []byte(headers + message)
	if err := sendSMTP(ctx, addr, s.Host, s.Address, to, msg, s.Username, s.Password); err != nil {
		sentry.CaptureException(err)
		return err
	}
	return nil
}

func sendSMTP(ctx context.Context, addr, host, from string, recipients []string, msg []byte, username, password string) error {
	if len(recipients) == 0 {
		return errors.New("smtp send requires at least one recipient")
	}

	ctx, cancel := context.WithTimeout(ctx, smtpSendTimeout)
	defer cancel()

	dialer := net.Dialer{Timeout: smtpSendTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}

	if username != "" && password != "" {
		if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			return err
		}
	}

	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}

	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(msg); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	if err := client.Quit(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
