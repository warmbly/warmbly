package goog

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhdr"
	"google.golang.org/api/gmail/v1"
)

// Attachment is a fully-resolved file ready to be MIME-encoded into an outbound
// message. Data is the raw bytes; MimeType drives the Content-Type.
type Attachment struct {
	Filename string
	MimeType string
	Data     []byte
}

func (c *Client) SendMessage(
	ctx context.Context,
	to, cc, bcc []string,
	messageID,
	subject, bodyPlain, bodyHTML string,
	parent *models.EmailMessageData,
	attachments []Attachment,
	customHeaders ...map[string]string,
) (*gmail.Message, error) {
	// Gmail's users.messages.send only ever accepts the base64url raw RFC 5322
	// form. The structured gmail.MessagePart/Payload tree is what the API
	// returns when you READ a parsed message; submitting one to Send is
	// rejected outright with "'raw' RFC822 payload message string or uploading
	// message via /upload/* URL required". Every send goes through raw.
	return c.sendRaw(to, cc, bcc, messageID, subject, bodyPlain, bodyHTML, parent, attachments, customHeaders...)
}

// sendRaw builds an RFC 5322 message and submits it as base64url-encoded Raw,
// which is the only body Gmail's Send endpoint accepts.
func (c *Client) sendRaw(
	to, cc, bcc []string,
	messageID,
	subject, bodyPlain, bodyHTML string,
	parent *models.EmailMessageData,
	attachments []Attachment,
	customHeaders ...map[string]string,
) (*gmail.Message, error) {
	var hdrs []header
	hdrs = append(hdrs,
		header{"From", c.GetAddress()},
		header{"To", mailhdr.AddressList(to)},
		// We now own header encoding, so a non-ASCII subject (or recipient
		// display name) has to be RFC 2047-encoded here. Encoding is a no-op
		// on plain ASCII.
		header{"Subject", mailhdr.Subject(subject)},
		header{"Message-ID", messageID},
		header{"MIME-Version", "1.0"},
	)
	if len(cc) > 0 {
		hdrs = append(hdrs, header{"Cc", mailhdr.AddressList(cc)})
	}
	if len(bcc) > 0 {
		hdrs = append(hdrs, header{"Bcc", mailhdr.AddressList(bcc)})
	}
	if parent != nil && parent.MessageID != "" {
		mid := "<" + strings.Trim(parent.MessageID, "<>") + ">"
		hdrs = append(hdrs, header{"In-Reply-To", mid}, header{"References", mid})
	}
	if len(customHeaders) > 0 {
		for k, v := range customHeaders[0] {
			hdrs = append(hdrs, header{k, v})
		}
	}

	raw, err := buildMIME(hdrs, bodyPlain, bodyHTML, attachments)
	if err != nil {
		return nil, fmt.Errorf("build mime: %w", err)
	}

	msg := &gmail.Message{
		Raw: base64.URLEncoding.EncodeToString(raw),
	}
	if parent != nil && parent.ThreadID != "" {
		msg.ThreadId = parent.ThreadID
	}

	sent, err := c.srv.Users.Messages.Send("me", msg).Do()
	if err != nil {
		return nil, fmt.Errorf("send message failed: %w", err)
	}
	return sent, nil
}

type header struct{ name, value string }

// buildMIME assembles the message body using the narrowest structure the
// content actually needs: a bare text/plain when that is all there is, a
// multipart/alternative once there is an HTML body, and a multipart/mixed
// wrapper only when there are attachments. Wrapping every message in
// multipart/mixed would be valid but is not what a mail client produces, and
// cold outreach has no room for gratuitous structural differences.
func buildMIME(hdrs []header, bodyPlain, bodyHTML string, attachments []Attachment) ([]byte, error) {
	switch {
	case len(attachments) > 0:
		return buildMixedMIME(hdrs, bodyPlain, bodyHTML, attachments)
	case bodyHTML == "":
		return buildPlainMIME(hdrs, bodyPlain)
	default:
		return buildAlternativeMIME(hdrs, bodyPlain, bodyHTML)
	}
}

// writeHeaders emits the top-level headers. They must precede the body and any
// multipart boundary, so this always runs before a part is created.
func writeHeaders(buf *bytes.Buffer, hdrs []header) {
	for _, h := range hdrs {
		fmt.Fprintf(buf, "%s: %s\r\n", h.name, h.value)
	}
}

// buildPlainMIME is the single-part form used by warmup mail and any campaign
// with no HTML body.
func buildPlainMIME(hdrs []header, bodyPlain string) ([]byte, error) {
	var buf bytes.Buffer
	writeHeaders(&buf, hdrs)
	fmt.Fprint(&buf, "Content-Type: text/plain; charset=UTF-8\r\n")
	fmt.Fprint(&buf, "Content-Transfer-Encoding: quoted-printable\r\n\r\n")

	qp := quotedprintable.NewWriter(&buf)
	if _, err := qp.Write([]byte(bodyPlain)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// buildAlternativeMIME is the text + HTML form used by a normal campaign send.
func buildAlternativeMIME(hdrs []header, bodyPlain, bodyHTML string) ([]byte, error) {
	var buf bytes.Buffer
	alt := multipart.NewWriter(&buf)

	writeHeaders(&buf, hdrs)
	fmt.Fprintf(&buf, "Content-Type: multipart/alternative; boundary=%s\r\n\r\n", alt.Boundary())

	if err := writeTextPart(alt, "text/plain; charset=UTF-8", bodyPlain); err != nil {
		return nil, err
	}
	if err := writeTextPart(alt, "text/html; charset=UTF-8", bodyHTML); err != nil {
		return nil, err
	}
	if err := alt.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// buildMixedMIME assembles a multipart/mixed message: a multipart/alternative
// (text/plain + optional text/html) followed by one attachment part each. Text
// parts use quoted-printable; attachment parts use base64 with a
// Content-Disposition: attachment header. Shared by the Gmail raw path.
func buildMixedMIME(hdrs []header, bodyPlain, bodyHTML string, attachments []Attachment) ([]byte, error) {
	var buf bytes.Buffer

	mixed := multipart.NewWriter(&buf)

	// Top-level headers + the multipart/mixed Content-Type. These must precede
	// the first boundary, so write them before any part is created.
	writeHeaders(&buf, hdrs)
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%s\r\n\r\n", mixed.Boundary())

	// --- multipart/alternative sub-tree for the text bodies ---
	var altBuf bytes.Buffer
	alt := multipart.NewWriter(&altBuf)

	if err := writeTextPart(alt, "text/plain; charset=UTF-8", bodyPlain); err != nil {
		return nil, err
	}
	if bodyHTML != "" {
		if err := writeTextPart(alt, "text/html; charset=UTF-8", bodyHTML); err != nil {
			return nil, err
		}
	}
	if err := alt.Close(); err != nil {
		return nil, err
	}

	altPart, err := mixed.CreatePart(textproto.MIMEHeader{
		"Content-Type": {fmt.Sprintf("multipart/alternative; boundary=%s", alt.Boundary())},
	})
	if err != nil {
		return nil, err
	}
	if _, err := altPart.Write(altBuf.Bytes()); err != nil {
		return nil, err
	}

	// --- one attachment part per file ---
	for _, a := range attachments {
		mimeType := a.MimeType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		// RFC 2047-encode the filename for non-ASCII safety.
		fn := mime.QEncoding.Encode("utf-8", a.Filename)
		part, perr := mixed.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {fmt.Sprintf("%s; name=%q", mimeType, fn)},
			"Content-Transfer-Encoding": {"base64"},
			"Content-Disposition":       {fmt.Sprintf("attachment; filename=%q", fn)},
		})
		if perr != nil {
			return nil, perr
		}
		if werr := writeBase64Wrapped(part, a.Data); werr != nil {
			return nil, werr
		}
	}

	if err := mixed.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// writeTextPart writes a quoted-printable text body part with the given
// Content-Type into the multipart writer.
func writeTextPart(w *multipart.Writer, contentType, body string) error {
	part, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {contentType},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return err
	}
	qp := quotedprintable.NewWriter(part)
	if _, err := qp.Write([]byte(body)); err != nil {
		return err
	}
	return qp.Close()
}

// writeBase64Wrapped writes data as base64, hard-wrapped at 76 columns per
// RFC 2045 so strict MTAs accept the message.
func writeBase64Wrapped(w io.Writer, data []byte) error {
	encoded := base64.StdEncoding.EncodeToString(data)
	const lineLen = 76
	for i := 0; i < len(encoded); i += lineLen {
		end := i + lineLen
		if end > len(encoded) {
			end = len(encoded)
		}
		if _, err := w.Write([]byte(encoded[i:end] + "\r\n")); err != nil {
			return err
		}
	}
	return nil
}
