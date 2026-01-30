package wmail

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/client/goog"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

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
}

// SendResult contains the result of a send operation
type SendResult struct {
	Success       bool
	MessageID     string
	ProviderMsgID string
	SentAt        time.Time
	Error         *errx.MailError
}

// Send attempts to send an email - NO retries, failures returned immediately
func (w *WMail) Send(ctx context.Context, req *SendRequest) *SendResult {
	result := &SendResult{
		Success: false,
		SentAt:  time.Now(),
	}

	// For warmup emails, ensure HTML is empty
	bodyHTML := req.BodyHTML
	if req.IsWarmup {
		bodyHTML = ""
	}

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

	// Build parent reference for replies
	var parent *models.EmailMessageData
	if req.InReplyTo != "" && req.Parent != nil {
		parent = &models.EmailMessageData{
			MessageID: req.Parent.MessageID,
			ThreadID:  req.Parent.ThreadID,
		}
	}

	// Build custom headers for warmup token
	var customHeaders map[string]string
	if req.WarmupToken != "" {
		customHeaders = map[string]string{
			"X-Warmbly-Token": req.WarmupToken,
		}
	}

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

	// Build custom headers for warmup token
	var smtpCustomHeaders map[string]string
	if req.WarmupToken != "" {
		smtpCustomHeaders = map[string]string{
			"X-Warmbly-Token": req.WarmupToken,
		}
	}

	// Send via SMTP
	var merr *errx.MailError
	if smtpCustomHeaders != nil {
		merr = w.SmtpImapData.SmtpClient.Send(
			ctx,
			req.To,
			req.Cc,
			req.Bcc,
			req.Subject,
			req.BodyPlain,
			bodyHTML,
			req.InReplyTo,
			smtpCustomHeaders,
		)
	} else {
		merr = w.SmtpImapData.SmtpClient.Send(
			ctx,
			req.To,
			req.Cc,
			req.Bcc,
			req.Subject,
			req.BodyPlain,
			bodyHTML,
			req.InReplyTo,
		)
	}
	if merr != nil {
		result.Error = merr
		return result
	}

	result.Success = true
	result.MessageID = req.MessageID
	result.ProviderMsgID = req.MessageID // SMTP uses the same message ID
	return result
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
