package notify

import (
	"context"
	"log"
	"strings"
)

// logEmailNotificationService writes messages to stdout instead of delivering
// them. It exists so a fresh install can complete first login before any relay
// is configured: the login code is in the container logs rather than lost in a
// sink the operator may not know is there.
//
// Gitea calls the same idea PROTOCOL=dummy and Zulip uses a file-based backend.
// The banner is deliberately loud because this transport must never be mistaken
// for working mail.
type logEmailNotificationService struct {
	Name    string
	Address string
}

func NewLogEmailNotificationService(name, address string) EmailNotificationService {
	return &logEmailNotificationService{Name: name, Address: address}
}

func (s *logEmailNotificationService) Send(ctx context.Context, to, cc, bcc []string, subject, message string) error {
	return s.write(to, cc, bcc, "", subject, message)
}

func (s *logEmailNotificationService) SendOutreach(ctx context.Context, to []string, replyTo, subject, message string) error {
	return s.write(to, nil, nil, replyTo, subject, message)
}

func (s *logEmailNotificationService) write(to, cc, bcc []string, replyTo, subject, html string) error {
	recipients := append(append(append([]string{}, to...), cc...), bcc...)

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("┌──────────────────────────────────────────────────────────────────────\n")
	b.WriteString("│ MAIL_TRANSPORT=log — this message was NOT delivered\n")
	b.WriteString("│ To:      " + strings.Join(recipients, ", ") + "\n")
	b.WriteString("│ From:    " + formatAddress(s.Name, s.Address) + "\n")
	if replyTo != "" {
		b.WriteString("│ Reply-To: " + replyTo + "\n")
	}
	b.WriteString("│ Subject: " + subject + "\n")
	b.WriteString("├──────────────────────────────────────────────────────────────────────\n")
	for _, line := range strings.Split(htmlToPlainText(html), "\n") {
		b.WriteString("│ " + line + "\n")
	}
	b.WriteString("└──────────────────────────────────────────────────────────────────────\n")

	log.Print(b.String())
	return nil
}
