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
	// Attachments require a multipart/mixed MIME tree, which the structured
	// gmail.MessagePart API does not express well (no per-part raw bytes with
	// Content-Disposition). Build a raw RFC 5322 message and submit it as
	// base64url Raw. The no-attachment path keeps the existing structured form
	// so threading/back-compat behavior is unchanged.
	if len(attachments) > 0 {
		return c.sendRawWithAttachments(to, cc, bcc, messageID, subject, bodyPlain, bodyHTML, parent, attachments, customHeaders...)
	}

	// Compose headers
	headers := []*gmail.MessagePartHeader{
		{Name: "From", Value: c.GetAddress()},
		{Name: "To", Value: strings.Join(to, ", ")},
		{Name: "Subject", Value: subject},
		{Name: "Message-ID", Value: messageID},
	}

	if len(cc) > 0 {
		headers = append(headers, &gmail.MessagePartHeader{
			Name:  "Cc",
			Value: strings.Join(cc, ", "),
		})
	}

	if len(bcc) > 0 {
		headers = append(headers, &gmail.MessagePartHeader{
			Name:  "Bcc",
			Value: strings.Join(bcc, ", "),
		})
	}

	if parent != nil && parent.MessageID != "" {
		// Trim any existing <...> before re-wrapping so we don't emit <<id>>,
		// which won't match the original Message-ID header and breaks threading.
		mid := "<" + strings.Trim(parent.MessageID, "<>") + ">"
		headers = append(headers,
			&gmail.MessagePartHeader{Name: "In-Reply-To", Value: mid},
			&gmail.MessagePartHeader{Name: "References", Value: mid},
		)
	}

	// Add custom headers (e.g., X-Warmbly-Token for warmup)
	if len(customHeaders) > 0 {
		for k, v := range customHeaders[0] {
			headers = append(headers, &gmail.MessagePartHeader{Name: k, Value: v})
		}
	}

	// Compose parts
	var parts []*gmail.MessagePart

	// Plain text part
	parts = append(parts, &gmail.MessagePart{
		MimeType: "text/plain",
		Body: &gmail.MessagePartBody{
			Data: base64.URLEncoding.EncodeToString([]byte(bodyPlain)),
		},
	})

	// HTML part (optional)
	if bodyHTML != "" {
		parts = append(parts, &gmail.MessagePart{
			MimeType: "text/html",
			Body: &gmail.MessagePartBody{
				Data: base64.URLEncoding.EncodeToString([]byte(bodyHTML)),
			},
		})
	}

	// Full message
	msg := &gmail.Message{
		Payload: &gmail.MessagePart{
			MimeType: "multipart/alternative",
			Headers:  headers,
			Parts:    parts,
		},
	}

	// Threading
	if parent != nil && parent.ThreadID != "" {
		msg.ThreadId = parent.ThreadID
	}

	// Send via Gmail API
	sent, err := c.srv.Users.Messages.Send("me", msg).Do()
	if err != nil {
		return nil, fmt.Errorf("send message failed: %w", err)
	}

	return sent, nil
}

// sendRawWithAttachments builds a multipart/mixed RFC 5322 message
// (multipart/alternative for text+html, then one application/* part per
// attachment) and submits it via the Gmail API as base64url-encoded Raw.
func (c *Client) sendRawWithAttachments(
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
		header{"To", strings.Join(to, ", ")},
		header{"Subject", subject},
		header{"Message-ID", messageID},
		header{"MIME-Version", "1.0"},
	)
	if len(cc) > 0 {
		hdrs = append(hdrs, header{"Cc", strings.Join(cc, ", ")})
	}
	if len(bcc) > 0 {
		hdrs = append(hdrs, header{"Bcc", strings.Join(bcc, ", ")})
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

	raw, err := buildMixedMIME(hdrs, bodyPlain, bodyHTML, attachments)
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

// buildMixedMIME assembles a multipart/mixed message: a multipart/alternative
// (text/plain + optional text/html) followed by one attachment part each. Text
// parts use quoted-printable; attachment parts use base64 with a
// Content-Disposition: attachment header. Shared by the Gmail raw path.
func buildMixedMIME(hdrs []header, bodyPlain, bodyHTML string, attachments []Attachment) ([]byte, error) {
	var buf bytes.Buffer

	mixed := multipart.NewWriter(&buf)

	// Top-level headers + the multipart/mixed Content-Type. These must precede
	// the first boundary, so write them before any part is created.
	for _, h := range hdrs {
		fmt.Fprintf(&buf, "%s: %s\r\n", h.name, h.value)
	}
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
