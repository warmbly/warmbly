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
	"github.com/warmbly/warmbly/internal/config"
)

const smtpSendTimeout = 30 * time.Second

// ErrSMTPCleartextAuth is returned rather than sending credentials over an
// unencrypted link. Go's own PlainAuth refuses the same thing, but silently
// enough that operators read it as a credentials problem.
var ErrSMTPCleartextAuth = errors.New("smtp: refusing to send credentials over an unencrypted connection (set SMTP_SECURITY=starttls or tls)")

type smtpEmailNotificationService struct {
	Name    string
	Address string
	cfg     *config.SMTPConfig
}

func NewSMTPEmailNotificationService(name, address string, cfg *config.SMTPConfig) EmailNotificationService {
	return &smtpEmailNotificationService{
		Name:    name,
		Address: address,
		cfg:     cfg,
	}
}

func (s *smtpEmailNotificationService) Send(ctx context.Context, to, cc, bcc []string, subject, message string) error {
	return s.send(ctx, to, cc, bcc, "", subject, message)
}

// SendOutreach is Send with an explicit Reply-To so a noreply From can still
// funnel replies into a real inbox.
func (s *smtpEmailNotificationService) SendOutreach(ctx context.Context, to []string, replyTo, subject, message string) error {
	return s.send(ctx, to, nil, nil, replyTo, subject, message)
}

func (s *smtpEmailNotificationService) send(ctx context.Context, to, cc, bcc []string, replyTo, subject, message string) error {
	recipients := make([]string, 0, len(to)+len(cc)+len(bcc))
	recipients = append(recipients, to...)
	recipients = append(recipients, cc...)
	recipients = append(recipients, bcc...)

	msg, err := buildMessage(s.Name, s.Address, to, cc, replyTo, subject, message)
	if err != nil {
		return err
	}

	if err := s.deliver(ctx, recipients, msg); err != nil {
		sentry.CaptureException(err)
		return err
	}
	return nil
}

func (s *smtpEmailNotificationService) deliver(ctx context.Context, recipients []string, msg []byte) error {
	if len(recipients) == 0 {
		return errors.New("smtp send requires at least one recipient")
	}

	client, err := s.dial(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := s.authenticate(client); err != nil {
		return err
	}

	if err := client.Mail(s.Address); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("smtp RCPT TO: %w", err)
		}
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
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

// dial returns a connected client with TLS already established according to
// the configured security mode.
func (s *smtpEmailNotificationService) dial(ctx context.Context) (*smtp.Client, error) {
	addr := net.JoinHostPort(s.cfg.Host, s.cfg.Port)

	ctx, cancel := context.WithTimeout(ctx, smtpSendTimeout)
	defer cancel()

	tlsConf := &tls.Config{
		ServerName:         s.cfg.Host,
		InsecureSkipVerify: s.cfg.InsecureSkipVerify, //nolint:gosec // operator opt-in for a private-CA relay
		MinVersion:         tls.VersionTLS12,
	}

	var conn net.Conn
	var err error
	if s.cfg.Security == config.SMTPSecurityTLS {
		dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: smtpSendTimeout}, Config: tlsConf}
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	} else {
		dialer := &net.Dialer{Timeout: smtpSendTimeout}
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("smtp greeting: %w", err)
	}

	if name := s.ehloName(); name != "" {
		if err := client.Hello(name); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("smtp EHLO: %w", err)
		}
	}

	if s.cfg.Security == config.SMTPSecurityStartTLS {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			_ = client.Close()
			return nil, fmt.Errorf("smtp: %s does not offer STARTTLS (set SMTP_SECURITY=tls for port 465, or none for a local sink)", addr)
		}
		if err := client.StartTLS(tlsConf); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("smtp STARTTLS: %w", err)
		}
	}

	return client, nil
}

func (s *smtpEmailNotificationService) authenticate(client *smtp.Client) error {
	if s.cfg.Auth == config.SMTPAuthNone || s.cfg.Username == "" {
		return nil
	}

	// Credentials only travel over an encrypted link. Loopback is exempt so a
	// sidecar relay on the same host still works.
	if _, isTLS := client.TLSConnectionState(); !isTLS && !isLoopback(s.cfg.Host) {
		return ErrSMTPCleartextAuth
	}

	auth, err := s.authMechanism(client)
	if err != nil {
		return err
	}
	if auth == nil {
		return nil
	}
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp AUTH: %w", err)
	}
	return nil
}

func (s *smtpEmailNotificationService) authMechanism(client *smtp.Client) (smtp.Auth, error) {
	switch s.cfg.Auth {
	case config.SMTPAuthPlain:
		return smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host), nil
	case config.SMTPAuthLogin:
		return newLoginAuth(s.cfg.Username, s.cfg.Password, s.cfg.Host), nil
	case config.SMTPAuthCRAMMD5:
		return smtp.CRAMMD5Auth(s.cfg.Username, s.cfg.Password), nil
	}

	// auto: pick from what the server advertises. LOGIN is listed before PLAIN
	// because the relays that only speak one of them (Exchange Online and most
	// appliances) speak LOGIN.
	ok, ext := client.Extension("AUTH")
	if !ok {
		return nil, fmt.Errorf("smtp: %s advertises no AUTH extension but SMTP_USERNAME is set", s.cfg.Host)
	}
	mechs := strings.ToUpper(ext)
	switch {
	case strings.Contains(mechs, "CRAM-MD5"):
		return smtp.CRAMMD5Auth(s.cfg.Username, s.cfg.Password), nil
	case strings.Contains(mechs, "LOGIN"):
		return newLoginAuth(s.cfg.Username, s.cfg.Password, s.cfg.Host), nil
	case strings.Contains(mechs, "PLAIN"):
		return smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host), nil
	default:
		return nil, fmt.Errorf("smtp: no supported AUTH mechanism in %q", ext)
	}
}

func (s *smtpEmailNotificationService) ehloName() string {
	if s.cfg.EHLOName != "" {
		return s.cfg.EHLOName
	}
	// Announce the sender domain rather than net/smtp's "localhost", which
	// relays treat as a spam signal.
	if at := strings.LastIndex(s.Address, "@"); at >= 0 && at+1 < len(s.Address) {
		return s.Address[at+1:]
	}
	return ""
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// loginAuth implements the non-standard but widely required AUTH LOGIN
// mechanism, which net/smtp does not ship. Exchange Online and many appliance
// relays advertise it exclusively.
type loginAuth struct {
	username string
	password string
	host     string
}

func newLoginAuth(username, password, host string) smtp.Auth {
	return &loginAuth{username: username, password: password, host: host}
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS && !isLoopback(server.Name) {
		return "", nil, ErrSMTPCleartextAuth
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	// Servers vary in how they word the prompts, so match on the decoded
	// challenge rather than expecting an exact string.
	switch strings.ToLower(string(fromServer)) {
	case "username:", "user name":
		return []byte(a.username), nil
	case "password:":
		return []byte(a.password), nil
	}
	if strings.Contains(strings.ToLower(string(fromServer)), "user") {
		return []byte(a.username), nil
	}
	if strings.Contains(strings.ToLower(string(fromServer)), "pass") {
		return []byte(a.password), nil
	}
	return nil, fmt.Errorf("smtp: unexpected LOGIN challenge %q", string(fromServer))
}
