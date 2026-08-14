package config

import (
	"context"
	"os"
	"strings"
)

// SMTP security modes. The enum is explicit rather than a bare "secure" boolean
// because that boolean means implicit TLS in some products and STARTTLS in
// others, and operators get it wrong in both directions.
const (
	SMTPSecurityStartTLS = "starttls" // submission on 587, upgrade in-band
	SMTPSecurityTLS      = "tls"      // implicit TLS on 465
	SMTPSecurityNone     = "none"     // cleartext; only legitimate for a local sink
)

// SMTP auth mechanisms. "auto" picks the strongest mechanism the server
// advertises, which is what an operator copying credentials out of a provider
// dashboard expects.
const (
	SMTPAuthAuto    = "auto"
	SMTPAuthPlain   = "plain"
	SMTPAuthLogin   = "login"
	SMTPAuthCRAMMD5 = "cram-md5"
	SMTPAuthNone    = "none"
)

type SMTPConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	// Security is one of the SMTPSecurity* constants.
	Security string
	// Auth is one of the SMTPAuth* constants.
	Auth string
	// EHLOName is the name announced to the relay. Defaults to the sender
	// domain, because net/smtp otherwise announces "localhost" and relays
	// score that as suspicious.
	EHLOName string
	// InsecureSkipVerify disables certificate verification. Only for a relay
	// with a private CA; never for a public provider.
	InsecureSkipVerify bool
}

func (c *Config) LoadSMTPConfig(ctx context.Context) *SMTPConfig {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return nil
	}

	security := strings.ToLower(strings.TrimSpace(os.Getenv("SMTP_SECURITY")))
	port := strings.TrimSpace(os.Getenv("SMTP_PORT"))

	// Security and port each infer the other so an operator can set either one
	// alone and get the conventional pairing. Explicit values always win.
	if security == "" {
		switch port {
		case "465":
			security = SMTPSecurityTLS
		// 1025 and 11025 are the Mailpit and MailHog defaults; a sink offers
		// no STARTTLS, so inferring starttls there would fail every send.
		case "25", "1025", "11025", "2525":
			security = SMTPSecurityNone
		default:
			security = SMTPSecurityStartTLS
		}
	}
	if port == "" {
		switch security {
		case SMTPSecurityTLS:
			port = "465"
		case SMTPSecurityNone:
			port = "25"
		default:
			port = "587"
		}
	}

	auth := strings.ToLower(strings.TrimSpace(os.Getenv("SMTP_AUTH")))
	if auth == "" {
		auth = SMTPAuthAuto
	}

	return &SMTPConfig{
		Host:               host,
		Port:               port,
		Username:           os.Getenv("SMTP_USERNAME"),
		Password:           os.Getenv("SMTP_PASSWORD"),
		Security:           security,
		Auth:               auth,
		EHLOName:           os.Getenv("SMTP_EHLO_NAME"),
		InsecureSkipVerify: isTrue(os.Getenv("SMTP_TLS_INSECURE_SKIP_VERIFY")),
	}
}

func isTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
