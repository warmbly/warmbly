package msgraph

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhdr"
)

// SendMessage sends a message through Graph's MIME sendMail endpoint. We build a
// full RFC 5322 message ourselves and POST it base64-encoded with
// Content-Type: text/plain, because only the MIME path lets us set a custom
// Message-ID, In-Reply-To/References for threading, and arbitrary headers (the
// warmup verification token, RFC 8058 one-click unsubscribe) that the JSON
// message shape restricts. Graph auto-files the message in Sent Items.
//
// sendMail returns 202 with no id, so there is no provider message id to return;
// callers record the RFC Message-ID they supplied (inbound correlation keys on
// that anyway). customHeaders is variadic to mirror goog.Client.SendMessage.
func (c *Client) SendMessage(
	ctx context.Context,
	to, cc, bcc []string,
	messageID,
	subject, bodyPlain, bodyHTML string,
	parent *models.EmailMessageData,
	attachments []Attachment,
	customHeaders ...map[string]string,
) error {
	hdrs := []hdr{
		{"From", c.GetAddress()},
		{"To", mailhdr.AddressList(to)},
		// RFC 2047: a non-ASCII subject or display name has to be encoded or
		// it reaches the recipient as mojibake. No-op for plain ASCII.
		{"Subject", mailhdr.Subject(subject)},
		{"Message-ID", messageID},
		{"MIME-Version", "1.0"},
	}
	if len(cc) > 0 {
		hdrs = append(hdrs, hdr{"Cc", mailhdr.AddressList(cc)})
	}
	if len(bcc) > 0 {
		hdrs = append(hdrs, hdr{"Bcc", mailhdr.AddressList(bcc)})
	}
	if parent != nil && parent.MessageID != "" {
		// Trim any existing <...> before re-wrapping so we never emit <<id>>,
		// which won't match the original Message-ID and breaks threading.
		mid := "<" + strings.Trim(parent.MessageID, "<>") + ">"
		hdrs = append(hdrs, hdr{"In-Reply-To", mid}, hdr{"References", mid})
	}
	if len(customHeaders) > 0 {
		for k, v := range customHeaders[0] {
			hdrs = append(hdrs, hdr{k, v})
		}
	}

	raw, err := buildMIME(hdrs, bodyPlain, bodyHTML, attachments)
	if err != nil {
		return fmt.Errorf("build mime: %w", err)
	}

	// MIME sendMail: the request body is the base64 of the RFC 5322 message.
	encoded := base64.StdEncoding.EncodeToString(raw)
	resp, err := c.do(ctx, http.MethodPost, graphBase+"/me/sendMail", "text/plain", []byte(encoded))
	if err != nil {
		return errx.ErrMailServerUnreachable
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return HandleError(resp)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
