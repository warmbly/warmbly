package notify

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strings"

	"github.com/getsentry/sentry-go"
)

type smtpEmailNotificationService struct {
	Name    string
	Address string
	Host    string
	Port    string
}

func NewSMTPEmailNotificationService(name, address, host, port string) EmailNotificationService {
	return &smtpEmailNotificationService{
		Name:    name,
		Address: address,
		Host:    host,
		Port:    port,
	}
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

	if err := smtp.SendMail(addr, nil, s.Address, allRecipients, msg); err != nil {
		sentry.CaptureException(err)
		return err
	}

	return nil
}
