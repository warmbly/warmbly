package notify

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/warmbly/warmbly/internal/config"
)

// Transport is a constructed platform-mail sender plus enough metadata for the
// boot preflight, the admin diagnostics endpoint, and the login screen to tell
// the operator what is actually happening to their mail.
type Transport struct {
	EmailNotificationService

	// Kind is one of the config.MailTransport* constants.
	Kind string
	// Description is a one-line human summary, e.g. "smtp starttls
	// smtp.example.com:587 (authenticated)".
	Description string
	// Delivers reports whether this transport actually puts mail on the wire.
	// False for the log transport, which is the signal every caller uses to
	// decide whether email-dependent flows can be relied on.
	Delivers bool

	smtp *config.SMTPConfig
}

// NewTransport builds the platform mail sender selected by MAIL_TRANSPORT.
func NewTransport(ctx context.Context, cfg *config.Config, name, address string) (*Transport, error) {
	kind := config.MailTransport()

	switch kind {
	case config.MailTransportLog:
		return &Transport{
			EmailNotificationService: NewLogEmailNotificationService(name, address),
			Kind:                     kind,
			Description:              "log (messages are written to stdout, not delivered)",
			Delivers:                 false,
		}, nil

	case config.MailTransportSMTP:
		smtpCfg := cfg.LoadSMTPConfig(ctx)
		if smtpCfg == nil {
			return nil, fmt.Errorf("MAIL_TRANSPORT=smtp requires SMTP_HOST")
		}
		auth := "no auth"
		if smtpCfg.Username != "" {
			auth = "authenticated as " + smtpCfg.Username
		}
		return &Transport{
			EmailNotificationService: NewSMTPEmailNotificationService(name, address, smtpCfg),
			Kind:                     kind,
			Description: fmt.Sprintf("smtp %s %s (%s)",
				smtpCfg.Security, net.JoinHostPort(smtpCfg.Host, smtpCfg.Port), auth),
			Delivers: true,
			smtp:     smtpCfg,
		}, nil

	case config.MailTransportSES:
		ses, err := NewEmailNotficiationService(ctx, name, address)
		if err != nil {
			return nil, err
		}
		return &Transport{
			EmailNotificationService: ses,
			Kind:                     kind,
			Description:              "ses (AWS credentials and a verified identity required)",
			Delivers:                 true,
		}, nil
	}

	return nil, fmt.Errorf("unknown MAIL_TRANSPORT %q (want smtp, log or ses)", kind)
}

// Preflight checks the transport without sending a message. It dials and
// authenticates the SMTP relay, which is the failure every self-hoster hits and
// the one that previously only surfaced as a 500 on the login screen.
func (t *Transport) Preflight(ctx context.Context) error {
	if t.Kind != config.MailTransportSMTP || t.smtp == nil {
		return nil
	}
	svc, ok := t.EmailNotificationService.(*smtpEmailNotificationService)
	if !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	client, err := svc.dial(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := svc.authenticate(client); err != nil {
		return err
	}
	return client.Quit()
}
