package confenge

import (
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// deliverApprovedSMTP sends the exact approved touchpoint payload over SMTP.
// Used in local/dev/CI when SMTP_HOST is set so queue→transport can hit Mailpit
// without a separate worker process. Never regenerates commercial copy.
func deliverApprovedSMTP(from, to, subject, body string) error {
	host := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	if host == "" {
		return fmt.Errorf("SMTP_HOST not set")
	}
	port := strings.TrimSpace(os.Getenv("SMTP_PORT"))
	if port == "" {
		port = "1025"
	}
	if from == "" {
		from = strings.TrimSpace(os.Getenv("EMAIL_ADDRESS"))
	}
	if from == "" {
		from = "confenge@warmbly.local"
	}
	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("empty recipient")
	}
	// Optional sink rewrite for controlled pilot (never invents content).
	if sink := strings.TrimSpace(os.Getenv("CONFENGE_SMTP_SINK_EMAIL")); sink != "" {
		to = sink
	}
	addr := net.JoinHostPort(host, port)
	msg := strings.Builder{}
	msg.WriteString("From: " + from + "\r\n")
	msg.WriteString("To: " + to + "\r\n")
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	// Bound dial for CI/local Mailpit.
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return smtp.SendMail(addr, nil, from, []string{to}, []byte(msg.String()))
}

// localSMTPDeliveryEnabled is an explicit test-only Mailpit path.
func localSMTPDeliveryEnabled() bool {
	if strings.TrimSpace(os.Getenv("SMTP_HOST")) == "" {
		return false
	}
	appEnv := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	if appEnv == "prod" || appEnv == "production" {
		return false
	}
	v := strings.ToLower(strings.TrimSpace(os.Getenv("CONFENGE_LOCAL_SMTP_DELIVERY")))
	return v == "1" || v == "true" || v == "on"
}
