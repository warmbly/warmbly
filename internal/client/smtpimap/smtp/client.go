package smtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/client/netbind"
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

	// BindIP optionally pins outbound TCP to a specific local source address.
	// When nil, WORKER_BIND_IP is consulted; when still unset, the OS default
	// route is used.
	BindIP *net.TCPAddr
}

// Attachment is a fully-resolved file to encode into an outbound message. Data
// is the raw bytes; MimeType drives the Content-Type.
type Attachment struct {
	Filename string
	MimeType string
	Data     []byte
}

func (c *Client) Send(
	ctx context.Context,
	to, cc, bcc []string,
	subject, bodyPlain, bodyHTML,
	inReplyTo string,
	attachments []Attachment,
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
		// The parent Message-ID may arrive already wrapped in <...>; trim before
		// re-wrapping so we don't emit <<id>>, which won't match the original
		// Message-ID header and breaks Gmail/Outlook threading.
		mid := "<" + strings.Trim(inReplyTo, "<>") + ">"
		headers["In-Reply-To"] = mid
		headers["References"] = mid
	}

	// Add custom headers (e.g., X-Warmbly-Token for warmup)
	if len(customHeaders) > 0 {
		for k, v := range customHeaders[0] {
			headers[k] = v
		}
	}

	var msg bytes.Buffer
	if len(attachments) > 0 {
		c.writeMixedBody(&msg, headers, bodyPlain, bodyHTML, attachments)
	} else {
		c.writeAlternativeBody(&msg, headers, bodyPlain, bodyHTML)
	}

	recipients := append(append([]string{}, to...), cc...)
	recipients = append(recipients, bcc...)

	return c.sendRaw(ctx, from.Address, recipients, msg.Bytes())
}

// writeAlternativeBody writes a multipart/alternative message (text/plain +
// optional text/html) including the top-level headers.
func (c *Client) writeAlternativeBody(msg *bytes.Buffer, headers map[string]string, bodyPlain, bodyHTML string) {
	writer := multipart.NewWriter(msg)
	headers["Content-Type"] = fmt.Sprintf("multipart/alternative; boundary=%s", writer.Boundary())

	for k, v := range headers {
		fmt.Fprintf(msg, "%s: %s\r\n", k, v)
	}
	fmt.Fprint(msg, "\r\n")

	writeTextParts(writer, bodyPlain, bodyHTML)
	writer.Close()
}

// writeMixedBody writes a multipart/mixed message: a multipart/alternative
// sub-tree for the text bodies, then one application/* part per attachment with
// a Content-Disposition: attachment header.
func (c *Client) writeMixedBody(msg *bytes.Buffer, headers map[string]string, bodyPlain, bodyHTML string, attachments []Attachment) {
	mixed := multipart.NewWriter(msg)
	headers["Content-Type"] = fmt.Sprintf("multipart/mixed; boundary=%s", mixed.Boundary())

	for k, v := range headers {
		fmt.Fprintf(msg, "%s: %s\r\n", k, v)
	}
	fmt.Fprint(msg, "\r\n")

	// multipart/alternative sub-tree for the text bodies.
	var altBuf bytes.Buffer
	alt := multipart.NewWriter(&altBuf)
	writeTextParts(alt, bodyPlain, bodyHTML)
	alt.Close()

	altPart, _ := mixed.CreatePart(textproto.MIMEHeader{
		"Content-Type": {fmt.Sprintf("multipart/alternative; boundary=%s", alt.Boundary())},
	})
	altPart.Write(altBuf.Bytes())

	// One attachment part per file.
	for _, a := range attachments {
		mimeType := a.MimeType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		fn := mime.QEncoding.Encode("utf-8", a.Filename)
		part, _ := mixed.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {fmt.Sprintf("%s; name=%q", mimeType, fn)},
			"Content-Transfer-Encoding": {"base64"},
			"Content-Disposition":       {fmt.Sprintf("attachment; filename=%q", fn)},
		})
		writeBase64Wrapped(part, a.Data)
	}

	mixed.Close()
}

// writeTextParts writes the text/plain and optional text/html quoted-printable
// parts into the given multipart writer.
func writeTextParts(writer *multipart.Writer, bodyPlain, bodyHTML string) {
	if bodyPlain != "" {
		part, _ := writer.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {"text/plain; charset=UTF-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		qp := quotedprintable.NewWriter(part)
		qp.Write([]byte(bodyPlain))
		qp.Close()
	}
	if bodyHTML != "" {
		part, _ := writer.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {"text/html; charset=UTF-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		qp := quotedprintable.NewWriter(part)
		qp.Write([]byte(bodyHTML))
		qp.Close()
	}
}

// writeBase64Wrapped writes data as base64, hard-wrapped at 76 columns per
// RFC 2045 so strict MTAs accept the message.
func writeBase64Wrapped(w io.Writer, data []byte) {
	encoded := base64.StdEncoding.EncodeToString(data)
	const lineLen = 76
	for i := 0; i < len(encoded); i += lineLen {
		end := i + lineLen
		if end > len(encoded) {
			end = len(encoded)
		}
		w.Write([]byte(encoded[i:end] + "\r\n"))
	}
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
	dialer := netbind.Dialer(c.BindIP)
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
