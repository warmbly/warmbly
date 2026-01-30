package smtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

type Client struct {
	FirstName string
	LastName  string
	Email     string

	AuthType    models.AuthType
	Credentials *models.Service
	Oauth2      *models.Oauth2Service
}

func (c *Client) Send(
	ctx context.Context,
	to, cc, bcc []string,
	subject, bodyPlain, bodyHTML,
	inReplyTo string,
	customHeaders ...map[string]string,
) *errx.MailError {
	from := mail.Address{Address: c.Email, Name: fmt.Sprintf("%s %s", c.FirstName, c.LastName)}

	// ----- Headers -----
	headers := map[string]string{
		"From":         from.String(),
		"To":           strings.Join(to, ", "),
		"Subject":      subject,
		"Date":         time.Now().Format(time.RFC1123Z),
		"MIME-Version": "1.0",
	}
	if len(cc) > 0 {
		headers["Cc"] = strings.Join(cc, ", ")
	}
	if inReplyTo != "" {
		headers["In-Reply-To"] = fmt.Sprintf("<%s>", inReplyTo)
		headers["References"] = fmt.Sprintf("<%s>", inReplyTo)
	}

	// Add custom headers (e.g., X-Warmbly-Token for warmup)
	if len(customHeaders) > 0 {
		for k, v := range customHeaders[0] {
			headers[k] = v
		}
	}

	// ----- Multipart body -----
	var msg bytes.Buffer
	writer := multipart.NewWriter(&msg)
	boundary := writer.Boundary()
	headers["Content-Type"] = fmt.Sprintf("multipart/alternative; boundary=%s", boundary)

	for k, v := range headers {
		fmt.Fprintf(&msg, "%s: %s\r\n", k, v)
	}
	fmt.Fprint(&msg, "\r\n")

	// text/plain
	if bodyPlain != "" {
		part, _ := writer.CreatePart(map[string][]string{
			"Content-Type":              {"text/plain; charset=UTF-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		qp := quotedprintable.NewWriter(part)
		qp.Write([]byte(bodyPlain))
		qp.Close()
	}

	// text/html
	if bodyHTML != "" {
		part, _ := writer.CreatePart(map[string][]string{
			"Content-Type":              {"text/html; charset=UTF-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		qp := quotedprintable.NewWriter(part)
		qp.Write([]byte(bodyHTML))
		qp.Close()
	}
	writer.Close()

	recipients := append(append([]string{}, to...), cc...)
	recipients = append(recipients, bcc...)

	return c.sendRaw(ctx, from.Address, recipients, msg.Bytes())
}

// ---------- Internal helpers ----------

func (c *Client) sendRaw(ctx context.Context, from string, to []string, data []byte) *errx.MailError {
	var host string
	var port int

	switch c.AuthType {
	case models.AuthPlain:
		host = c.Credentials.Host
		port = c.Credentials.Port
	case models.AuthOAuth2:
		host = c.Oauth2.Host
		port = c.Oauth2.Port
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return errx.ErrMailServerUnreachable
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, c.Credentials.Host)
	if err != nil {
		return errx.ErrMailServerUnreachable
	}
	defer client.Quit()

	tlsConf := &tls.Config{
		ServerName: host,
	}
	if err := client.StartTLS(tlsConf); err != nil {
		return errx.ErrMailServerUnreachable
	}

	// --- Auth ---
	switch c.AuthType {
	case models.AuthPlain:
		auth := smtp.PlainAuth("", c.Credentials.Username, c.Credentials.Password, c.Credentials.Host)
		if err := client.Auth(auth); err != nil {
			return errx.ErrMailInvalidCredentials
		}
	case models.AuthOAuth2:
		tk, err := c.Oauth2.Token.Token()
		if err != nil {
			var rErr *oauth2.RetrieveError
			if errors.As(err, &rErr) {
				if rErr.Response.StatusCode >= 500 {
					return errx.ErrMailServerUnreachable
				}
			}
			return errx.ErrMailAuthenticationFailed
		}

		auth := newOAuth2Auth(c.Email, tk.AccessToken)
		if err := client.Auth(auth); err != nil {
			return errx.ErrMailAuthenticationFailed
		}
	}

	if err := client.Mail(from); err != nil {
		return errx.ErrMailServerUnreachable
	}
	for _, r := range to {
		if err := client.Rcpt(r); err != nil {
			return errx.ErrMailServerUnreachable
		}
	}
	w, err := client.Data()
	if err != nil {
		return errx.ErrMailServerUnreachable
	}
	if _, err := w.Write(data); err != nil {
		return errx.ErrMailServerUnreachable
	}
	if err := w.Close(); err != nil {
		return errx.ErrMailServerUnreachable
	}

	return nil
}
