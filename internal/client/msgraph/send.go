package msgraph

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhdr"
)

// SendMessage sends a message through Graph and returns the RFC 5322
// Message-ID the recipient will actually see.
//
// It creates the message as a draft and then sends the draft, rather than
// posting to /me/sendMail, because Exchange stamps its own internetMessageId
// when the item is created and discards the Message-ID we supply. Mail sent
// with sendMail therefore reaches recipients under an id Warmbly has never
// seen, which leaves warmup tokens unverifiable and campaign replies
// unthreadable. Creating the draft first lets us read that id before the
// message leaves, so the draft MIME deliberately carries no Message-ID of our
// own. messageID is only used by the sendMail fallback below.
//
// The body is still built as RFC 5322 MIME, which is the only Graph shape that
// can carry In-Reply-To/References and arbitrary headers. Note that Exchange
// drops custom headers in transit even so: the verify header rides along for
// the providers that keep it, and is not what verification depends on.
// customHeaders is variadic to mirror goog.Client.SendMessage.
func (c *Client) SendMessage(
	ctx context.Context,
	fromName string,
	to, cc, bcc []string,
	messageID,
	subject, bodyPlain, bodyHTML string,
	parent *models.EmailMessageData,
	attachments []Attachment,
	customHeaders ...map[string]string,
) (string, error) {
	raw, err := buildMIME(sendHeaders(c.FromAddress(fromName), to, cc, bcc, "", subject, parent, customHeaders...), bodyPlain, bodyHTML, attachments)
	if err != nil {
		return "", fmt.Errorf("build mime: %w", err)
	}

	draftID, assignedID, err := c.createDraft(ctx, raw)
	if err != nil {
		var mailErr *errx.MailError
		if errors.As(err, &mailErr) && mailErr.Failure != nil {
			mailErr.Failure.Stage = "create_draft"
		}
		if errors.As(err, &mailErr) && (mailErr.Type == errx.MailErrorCritical || mailErr.Failure != nil) &&
			!(mailErr.Failure != nil && mailErr.Failure.Status == http.StatusForbidden && mailErr.Failure.RetryAt == nil) {
			return "", err
		}
		// Nothing was created, so nothing can be double-sent: fall back to the
		// single-shot path with our own Message-ID. The send still lands; only
		// the id we learn is lost.
		log.Warn().Err(err).Str("email", c.Email).Msg("graph draft creation failed; sending without a readable message id")
		fallback, berr := buildMIME(sendHeaders(c.FromAddress(fromName), to, cc, bcc, messageID, subject, parent, customHeaders...), bodyPlain, bodyHTML, attachments)
		if berr != nil {
			return "", fmt.Errorf("build mime: %w", berr)
		}
		if serr := c.sendMIME(ctx, fallback); serr != nil {
			return "", serr
		}
		return messageID, nil
	}

	if assignedID == "" {
		assignedID = c.draftMessageID(ctx, draftID)
	}

	if err := c.sendDraft(ctx, draftID); err != nil {
		// The draft is still sitting in Drafts; leaving it there would show up
		// in the customer's own mail client as an unsent message.
		var mailErr *errx.MailError
		if !errors.As(err, &mailErr) || mailErr.Failure == nil || mailErr.Failure.Disposition != errx.SendAmbiguous {
			c.discardDraft(ctx, draftID)
		}
		return "", err
	}
	return assignedID, nil
}

// sendHeaders assembles the top-level RFC 5322 headers for an outbound
// message. A blank messageID omits the header entirely, which is what the
// draft path wants: Exchange then assigns the id and we read it back.
func sendHeaders(from string, to, cc, bcc []string, messageID, subject string, parent *models.EmailMessageData, customHeaders ...map[string]string) []hdr {
	hdrs := []hdr{
		{"From", from},
		{"To", mailhdr.AddressList(to)},
		// RFC 2047: a non-ASCII subject or display name has to be encoded or
		// it reaches the recipient as mojibake. No-op for plain ASCII.
		{"Subject", mailhdr.Subject(subject)},
		{"MIME-Version", "1.0"},
	}
	if messageID != "" {
		hdrs = append(hdrs, hdr{"Message-ID", messageID})
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
			if strings.EqualFold(k, "References") {
				for i := range hdrs {
					if strings.EqualFold(hdrs[i].name, "References") {
						hdrs = append(hdrs[:i], hdrs[i+1:]...)
						break
					}
				}
			}
			hdrs = append(hdrs, hdr{k, v})
		}
	}
	return hdrs
}

// createDraft posts the MIME message as a draft and returns the Graph item id
// plus the internetMessageId Exchange assigned to it.
func (c *Client) createDraft(ctx context.Context, raw []byte) (string, string, error) {
	encoded := base64.StdEncoding.EncodeToString(raw)
	resp, err := c.do(ctx, http.MethodPost, c.root()+"/messages", "text/plain", []byte(encoded))
	if err != nil {
		return "", "", transportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", HandleError(resp)
	}
	var created struct {
		ID                string `json:"id"`
		InternetMessageID string `json:"internetMessageId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", "", fmt.Errorf("decode draft: %w", err)
	}
	if created.ID == "" {
		return "", "", fmt.Errorf("graph created a draft without an id")
	}
	return created.ID, created.InternetMessageID, nil
}

// draftMessageID re-reads the assigned internetMessageId. Returns "" rather
// than an error: an unknown id costs verification accuracy, not the send.
func (c *Client) draftMessageID(ctx context.Context, draftID string) string {
	var msg struct {
		InternetMessageID string `json:"internetMessageId"`
	}
	if err := c.doJSON(ctx, http.MethodGet, c.messageURL(draftID)+"?$select=internetMessageId", nil, &msg); err != nil {
		log.Warn().Err(err).Str("email", c.Email).Msg("could not read the message id Graph assigned to the draft")
		return ""
	}
	return msg.InternetMessageID
}

// sendDraft submits a created draft.
func (c *Client) sendDraft(ctx context.Context, draftID string) error {
	resp, err := c.do(ctx, http.MethodPost, c.messageURL(draftID)+"/send", "", nil)
	if err != nil {
		return submissionFailure(transportError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return submissionFailure(HandleError(resp))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// discardDraft removes a draft whose send failed, so it does not sit in the
// customer's Drafts folder as a message they never wrote.
//
// It confirms the item is still a draft first. A send that failed ambiguously
// (a lost response) may in fact have gone out, and deleting the wrong item
// would take the customer's Sent Items copy with it. Best effort otherwise:
// the send has already failed and will be retried.
func (c *Client) discardDraft(ctx context.Context, draftID string) {
	var msg struct {
		IsDraft bool `json:"isDraft"`
	}
	if err := c.doJSON(ctx, http.MethodGet, c.messageURL(draftID)+"?$select=isDraft", nil, &msg); err != nil || !msg.IsDraft {
		return
	}
	resp, err := c.do(ctx, http.MethodDelete, c.messageURL(draftID), "", nil)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
}

// sendMIME is the single-shot /me/sendMail path, kept as the fallback for when
// a draft cannot be created. Graph auto-files the message in Sent Items and
// returns 202 with no id.
func (c *Client) sendMIME(ctx context.Context, raw []byte) error {
	encoded := base64.StdEncoding.EncodeToString(raw)
	resp, err := c.do(ctx, http.MethodPost, c.root()+"/sendMail", "text/plain", []byte(encoded))
	if err != nil {
		return submissionFailure(transportError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return submissionFailure(HandleError(resp))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func submissionFailure(mailErr *errx.MailError) *errx.MailError {
	if mailErr.Type == errx.MailErrorCritical {
		return mailErr
	}
	if mailErr.Failure == nil {
		return errx.WithSendFailure(mailErr, errx.SendFailure{Provider: "microsoft", Protocol: "http",
			Stage: "submit", Scope: "mailbox", Disposition: errx.SendAmbiguous, ObservedAt: time.Now().UTC()})
	}
	mailErr.Failure.Stage = "submit"
	if mailErr.Failure.Disposition == errx.SendPermanent && mailErr.Code == errx.MailErrorCodeServerUnreachable {
		mailErr.Code = errx.MailErrorCodeSendRejected
	}
	if mailErr.Failure.Status >= 500 {
		mailErr.Failure.Disposition = errx.SendAmbiguous
	}
	return mailErr
}
