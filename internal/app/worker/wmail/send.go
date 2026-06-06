package wmail

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/client/goog"
	"github.com/warmbly/warmbly/internal/client/smtpimap/smtp"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Attachment is a fully-resolved attachment ready to be MIME-encoded: the
// worker has already fetched Data from object storage.
type Attachment struct {
	Filename string
	MimeType string
	Data     []byte
}

// SendRequest contains all parameters needed to send an email
type SendRequest struct {
	TaskID      uuid.UUID
	To          []string
	Cc          []string
	Bcc         []string
	MessageID   string
	Subject     string
	BodyPlain   string
	BodyHTML    string
	InReplyTo   string
	Parent      *models.EmailParent
	IsWarmup    bool
	WarmupToken string
	// UnsubscribeURL, when set (campaign sends with the unsubscribe header
	// enabled), produces RFC 8058 one-click unsubscribe headers.
	UnsubscribeURL string
	// Attachments, when present, are encoded as multipart/mixed parts after the
	// multipart/alternative text body. Warmup sends never carry attachments.
	Attachments []Attachment
}

// buildSendHeaders assembles the outbound custom headers: the warmup
// verification token (warmup sends) and RFC 8058 one-click unsubscribe headers
// (campaign sends). Returns nil when there are none so callers can branch.
func buildSendHeaders(req *SendRequest) map[string]string {
	h := map[string]string{}
	if req.WarmupToken != "" {
		h[config.WarmupVerifyHeader] = req.WarmupToken
	}
	if req.UnsubscribeURL != "" {
		// RFC 8058: the HTTPS URI in List-Unsubscribe plus the one-click marker
		// tells Gmail/Yahoo/Microsoft to POST List-Unsubscribe=One-Click here.
		h["List-Unsubscribe"] = "<" + req.UnsubscribeURL + ">"
		h["List-Unsubscribe-Post"] = "List-Unsubscribe=One-Click"
	}
	if len(h) == 0 {
		return nil
	}
	return h
}

// SendResult contains the result of a send operation
type SendResult struct {
	Success       bool
	MessageID     string
	ProviderMsgID string
	SentAt        time.Time
	Error         *errx.MailError
}

const maxSendRetries = 3

// Send attempts to send an email with retry for transient failures
func (w *WMail) Send(ctx context.Context, req *SendRequest) *SendResult {
	// For warmup emails, ensure HTML is empty
	bodyHTML := req.BodyHTML
	if req.IsWarmup {
		bodyHTML = ""
	}

	var result *SendResult
	for attempt := 0; attempt <= maxSendRetries; attempt++ {
		result = &SendResult{Success: false, SentAt: time.Now()}

		switch w.EmailType {
		case models.InboxProviderGoogle:
			result = w.sendViaGmail(ctx, req, bodyHTML)
		case models.InboxProviderOutlook, models.InboxProviderSMTPIMAP:
			result = w.sendViaSMTP(ctx, req, bodyHTML)
		default:
			result.Error = errx.MError(
				errx.MailErrorCritical,
				errx.MailErrorCodeUnsupported,
				"Unsupported email provider",
				errx.MailErrorResolveMethodNone,
			)
			return result
		}

		if result.Success {
			return result
		}

		// Don't retry critical/auth errors - only transient ones
		if result.Error != nil && result.Error.Type == errx.MailErrorCritical {
			return result
		}

		if attempt < maxSendRetries {
			backoff := time.Duration(1<<uint(attempt)) * time.Second // 1s, 2s, 4s
			select {
			case <-ctx.Done():
				return result
			case <-time.After(backoff):
			}
		}
	}

	return result
}

// sendViaGmail sends an email using the Gmail API
func (w *WMail) sendViaGmail(ctx context.Context, req *SendRequest, bodyHTML string) *SendResult {
	result := &SendResult{
		Success: false,
		SentAt:  time.Now(),
	}

	if w.GoogleData == nil || w.GoogleData.Client == nil {
		result.Error = errx.MError(
			errx.MailErrorCritical,
			errx.MailErrorCodeAuthenticationFailed,
			"Gmail client not initialized",
			errx.MailErrorResolveMethodReload,
		)
		return result
	}

	// Build parent reference for replies. Warmup replies often only have the
	// RFC Message-ID from the token flow, not a local provider thread record.
	var parent *models.EmailMessageData
	if req.InReplyTo != "" && req.Parent != nil {
		parent = &models.EmailMessageData{
			MessageID: req.Parent.MessageID,
			ThreadID:  req.Parent.ThreadID,
		}
	} else if req.InReplyTo != "" {
		parent = &models.EmailMessageData{
			MessageID: strings.Trim(req.InReplyTo, "<>"),
		}
	}

	// Build custom headers (warmup token + RFC 8058 one-click unsubscribe).
	customHeaders := buildSendHeaders(req)

	// Convert resolved attachments to the goog transport shape (warmup sends
	// carry none, but threading req.Attachments is harmless when empty).
	attachments := toGoogAttachments(req.Attachments)

	// Send via Gmail API
	gmailMsg, err := w.GoogleData.Client.SendMessage(
		ctx,
		req.To,
		req.Cc,
		req.Bcc,
		req.MessageID,
		req.Subject,
		req.BodyPlain,
		bodyHTML,
		parent,
		attachments,
		customHeaders,
	)
	if err != nil {
		// Convert to MailError using goog.HandleError
		if mailErr := goog.HandleError(err); mailErr != nil {
			result.Error = mailErr
		} else {
			// Generic error
			result.Error = errx.MError(
				errx.MailErrorWarning,
				errx.MailErrorCodeServerUnreachable,
				err.Error(),
				errx.MailErrorResolveMethodRetry,
			)
		}
		return result
	}

	result.Success = true
	result.MessageID = req.MessageID
	result.ProviderMsgID = gmailMsg.Id
	return result
}

// sendViaSMTP sends an email using SMTP
func (w *WMail) sendViaSMTP(ctx context.Context, req *SendRequest, bodyHTML string) *SendResult {
	result := &SendResult{
		Success: false,
		SentAt:  time.Now(),
	}

	if w.SmtpImapData == nil || w.SmtpImapData.SmtpClient == nil {
		result.Error = errx.MError(
			errx.MailErrorCritical,
			errx.MailErrorCodeAuthenticationFailed,
			"SMTP client not initialized",
			errx.MailErrorResolveMethodReload,
		)
		return result
	}

	// Build custom headers (warmup token + RFC 8058 one-click unsubscribe).
	smtpCustomHeaders := buildSendHeaders(req)

	// Convert resolved attachments to the SMTP transport shape.
	smtpAttachments := toSMTPAttachments(req.Attachments)

	// Send via SMTP. Attachments are passed explicitly (not variadic) so an
	// empty list still selects the same code path.
	merr := w.SmtpImapData.SmtpClient.Send(
		ctx,
		req.To,
		req.Cc,
		req.Bcc,
		req.Subject,
		req.BodyPlain,
		bodyHTML,
		req.InReplyTo,
		smtpAttachments,
		smtpCustomHeaders,
	)
	if merr != nil {
		result.Error = merr
		return result
	}

	result.Success = true
	result.MessageID = req.MessageID
	result.ProviderMsgID = req.MessageID // SMTP uses the same message ID
	return result
}

// toGoogAttachments maps wmail attachments to the Gmail transport shape.
func toGoogAttachments(in []Attachment) []goog.Attachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]goog.Attachment, 0, len(in))
	for _, a := range in {
		out = append(out, goog.Attachment{
			Filename: a.Filename,
			MimeType: a.MimeType,
			Data:     a.Data,
		})
	}
	return out
}

// toSMTPAttachments maps wmail attachments to the SMTP transport shape.
func toSMTPAttachments(in []Attachment) []smtp.Attachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]smtp.Attachment, 0, len(in))
	for _, a := range in {
		out = append(out, smtp.Attachment{
			Filename: a.Filename,
			MimeType: a.MimeType,
			Data:     a.Data,
		})
	}
	return out
}

// DetermineErrorEventType maps a MailError to the appropriate JobEventType
func DetermineErrorEventType(err *errx.MailError) models.JobEventType {
	if err == nil {
		return models.JobEventTypeEmailFailed
	}

	switch err.Code {
	case errx.MailErrorCodeGoogleAuth, errx.MailErrorCodeAuthenticationFailed:
		return models.JobEventTypeEmailAuthError

	case errx.MailErrorCodeAccountSuspended, errx.MailErrorCodeAuthorizationFailed:
		return models.JobEventTypeEmailDisabled

	case errx.MailErrorCodeRateLimitExceeded, errx.MailErrorCodeSendingTooFast, errx.MailErrorCodeQuotaExceeded:
		return models.JobEventTypeEmailRateLimited

	case errx.MailErrorCodeServerUnreachable, errx.MailErrorCodeConnectionLost:
		return models.JobEventTypeEmailServerError

	default:
		return models.JobEventTypeEmailFailed
	}
}

// MailErrorToSendError converts a MailError to an EmailSendError for transport
func MailErrorToSendError(err *errx.MailError) *models.EmailSendError {
	if err == nil {
		return nil
	}

	userInfo := err.GetUserErrorInfo()

	return &models.EmailSendError{
		Code:           string(err.Code),
		Type:           string(err.Type),
		Message:        err.Message,
		ResolveMethod:  string(err.ResolveMethod),
		UserVisible:    err.IsUserVisible(),
		UserTitle:      userInfo.Title,
		UserMessage:    userInfo.Message,
		ActionRequired: userInfo.ActionRequired,
	}
}
