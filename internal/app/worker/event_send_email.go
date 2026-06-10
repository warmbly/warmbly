package worker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emsg"
)

func (w *WorkerService) HandleSendEmail(ctx context.Context, body any) error {
	sendEmail, ok := body.(models.SendEmail)
	if !ok {
		log.Debug().Msg("Invalid HandleSendEmail body type")
		return fmt.Errorf("invalid body type")
	}

	log.Info().
		Str("task_id", sendEmail.TaskID.String()).
		Str("email_id", sendEmail.EmailID.String()).
		Strs("to", sendEmail.To).
		Bool("is_warmup", sendEmail.IsWarmup).
		Msg("Processing send email event")

	// Get the email account from MailManager
	w.mailManager.RLock()
	mail, exists := w.mailManager.Emails[sendEmail.EmailID]
	w.mailManager.RUnlock()

	if !exists {
		err := fmt.Errorf("email account %s not found in worker", sendEmail.EmailID.String())
		w.sendEmailFailure(sendEmail.TaskID, sendEmail.EmailID, mail, err.Error())
		return err
	}

	// Decrypt subject
	subject := sendEmail.Subject
	if w.CipherService != nil {
		c, cerr := w.CipherService.Cipher(ctx, sendEmail.OrgID)
		if cerr == nil {
			decSubject, cerr := c.Decrypt(ctx, sendEmail.Subject)
			if cerr == nil {
				subject = decSubject
			}
		}
	}

	// Fetch email body from S3 (attachment refs ride inside the emsg blob).
	bodyPlain, bodyHTML, attachmentRefs, err := w.fetchEmailBody(ctx, sendEmail.OrgID, sendEmail.BodyS3Key)
	if err != nil {
		log.Error().Err(err).Str("s3_key", sendEmail.BodyS3Key).Msg("Failed to fetch email body from S3")
		w.sendEmailFailure(sendEmail.TaskID, sendEmail.EmailID, mail, fmt.Sprintf("failed to fetch email body: %v", err))
		return err
	}

	// Fetch each attachment's bytes from object storage by key. A fetch failure
	// fails the send rather than silently dropping a file the user expects.
	attachments, err := w.fetchAttachments(ctx, attachmentRefs)
	if err != nil {
		log.Error().Err(err).Str("task_id", sendEmail.TaskID.String()).Msg("Failed to fetch attachment bytes from S3")
		w.sendEmailFailure(sendEmail.TaskID, sendEmail.EmailID, mail, fmt.Sprintf("failed to fetch attachment: %v", err))
		return err
	}

	// Use unified Send method
	w.recordSendAttempt()
	sendStart := time.Now()
	result := mail.Send(ctx, &wmail.SendRequest{
		TaskID:         sendEmail.TaskID,
		To:             sendEmail.To,
		Cc:             sendEmail.Cc,
		Bcc:            sendEmail.Bcc,
		MessageID:      sendEmail.MessageID,
		Subject:        subject,
		BodyPlain:      bodyPlain,
		BodyHTML:       bodyHTML,
		InReplyTo:      sendEmail.InReplyTo,
		Parent:         sendEmail.Parent,
		IsWarmup:       sendEmail.IsWarmup,
		WarmupToken:    sendEmail.WarmupToken,
		UnsubscribeURL: sendEmail.UnsubscribeURL,
		Attachments:    attachments,
	})
	w.recordSendLatency(time.Since(sendStart))
	w.recordSendOutcome(result)

	if result.Success {
		log.Info().
			Str("task_id", sendEmail.TaskID.String()).
			Str("message_id", result.MessageID).
			Str("provider_msg_id", result.ProviderMsgID).
			Msg("Email sent successfully")

		w.deleteTransportEmailBody(ctx, sendEmail.TaskID, sendEmail.BodyS3Key)

		w.sendEmailSuccess(sendEmail.TaskID, result.MessageID, result.ProviderMsgID)
	} else {
		log.Error().
			Str("task_id", sendEmail.TaskID.String()).
			Str("error_code", string(result.Error.Code)).
			Str("error_message", result.Error.Message).
			Msg("Email send failed")

		w.sendEmailError(sendEmail.TaskID, sendEmail.EmailID, mail, result.Error)
	}

	return nil
}

func (w *WorkerService) deleteTransportEmailBody(ctx context.Context, taskID uuid.UUID, s3Key string) {
	if w.Storage == nil || s3Key == "" {
		return
	}

	if err := w.Storage.Delete(ctx, s3Key); err != nil {
		log.Warn().
			Err(err).
			Str("task_id", taskID.String()).
			Str("s3_key", s3Key).
			Msg("Failed to delete transport email body from S3")
	}
}

// fetchEmailBody fetches and decodes the email body from S3, returning the
// decrypted plain/HTML bodies and the attachment refs carried inside the blob.
func (w *WorkerService) fetchEmailBody(ctx context.Context, orgID uuid.UUID, s3Key string) (string, string, []emsg.Attachment, error) {
	if w.Storage == nil {
		return "", "", nil, fmt.Errorf("storage client not configured")
	}

	// Get object from storage
	body, err := w.Storage.Get(ctx, s3Key)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to get S3 object: %w", err)
	}
	defer body.Close()

	// Read the body
	data, err := io.ReadAll(body)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to read S3 object: %w", err)
	}

	// Decode using emsg
	blob, err := emsg.DecodeBinary(bytes.NewReader(data))
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to decode emsg blob: %w", err)
	}

	bodyPlain := string(blob.PlainText)
	bodyHTML := string(blob.HTMLBody)

	if w.CipherService != nil {
		if c, cerr := w.CipherService.Cipher(ctx, orgID); cerr == nil {
			if bodyPlain != "" {
				if decPlain, decErr := c.Decrypt(ctx, bodyPlain); decErr == nil {
					bodyPlain = decPlain
				}
			}
			if bodyHTML != "" {
				if decHTML, decErr := c.Decrypt(ctx, bodyHTML); decErr == nil {
					bodyHTML = decHTML
				}
			}
		}
	}

	return bodyPlain, bodyHTML, blob.Attachments, nil
}

// fetchAttachments downloads each attachment's bytes from object storage by
// key, returning wmail attachments ready to be MIME-encoded. The bytes are
// stored as-is (not user-encrypted), so no cipher pass is needed here.
func (w *WorkerService) fetchAttachments(ctx context.Context, refs []emsg.Attachment) ([]wmail.Attachment, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	if w.Storage == nil {
		return nil, fmt.Errorf("storage client not configured")
	}

	out := make([]wmail.Attachment, 0, len(refs))
	for _, ref := range refs {
		rc, err := w.Storage.Get(ctx, ref.S3Key)
		if err != nil {
			return nil, fmt.Errorf("get attachment %s: %w", ref.Filename, err)
		}
		data, readErr := io.ReadAll(rc)
		rc.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read attachment %s: %w", ref.Filename, readErr)
		}

		mimeType := ref.MimeType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		out = append(out, wmail.Attachment{
			Filename: ref.Filename,
			MimeType: mimeType,
			Data:     data,
		})
	}
	return out, nil
}

// sendEmailSuccess sends a success result back to the jobs service
func (w *WorkerService) sendEmailSuccess(taskID uuid.UUID, messageID, providerMsgID string) {
	result := models.SendEmailResult{
		TaskID:        taskID,
		Success:       true,
		MessageID:     messageID,
		ProviderMsgID: providerMsgID,
		SentAt:        time.Now(),
	}

	if err := w.Produce(models.JobEventTypeEmailSent, taskID.String(), result); err != nil {
		log.Error().Err(err).Str("task_id", taskID.String()).Msg("Failed to produce email sent event")
	}
}

// sendEmailError sends a structured error result back to the jobs service
func (w *WorkerService) sendEmailError(taskID uuid.UUID, emailID uuid.UUID, mail *wmail.WMail, mailErr *errx.MailError) {
	// Determine the appropriate event type based on error
	eventType := wmail.DetermineErrorEventType(mailErr)

	// Convert to transport format
	sendError := wmail.MailErrorToSendError(mailErr)

	result := models.SendEmailResult{
		TaskID:         taskID,
		Success:        false,
		Error:          sendError,
		LegacyErrorMsg: mailErr.Message,
		SentAt:         time.Now(),
	}

	if err := w.Produce(eventType, taskID.String(), result); err != nil {
		log.Error().Err(err).Str("task_id", taskID.String()).Msg("Failed to produce email error event")
	}

	// For critical auth/disabled errors, also send a separate error event with full context
	if eventType == models.JobEventTypeEmailAuthError ||
		eventType == models.JobEventTypeEmailDisabled ||
		eventType == models.JobEventTypeEmailRateLimited {

		userInfo := mailErr.GetUserErrorInfo()
		errorEvent := models.EmailErrorEvent{
			TaskID:         taskID.String(),
			EmailAccountID: emailID.String(),
			UserID:         mail.UserID.String(),
			ErrorCode:      string(mailErr.Code),
			ErrorType:      string(mailErr.Type),
			ResolveMethod:  string(mailErr.ResolveMethod),
			Message:        mailErr.Message,
			UserVisible:    mailErr.IsUserVisible(),
			UserTitle:      userInfo.Title,
			UserMessage:    userInfo.Message,
			ActionRequired: userInfo.ActionRequired,
			Timestamp:      time.Now().Unix(),
		}

		if err := w.Produce(eventType, emailID.String(), errorEvent); err != nil {
			log.Error().Err(err).Str("email_id", emailID.String()).Msg("Failed to produce email error event")
		}
	}
}

// sendEmailFailure sends a generic failure result (for non-MailError cases)
func (w *WorkerService) sendEmailFailure(taskID uuid.UUID, emailID uuid.UUID, mail *wmail.WMail, errorMsg string) {
	result := models.SendEmailResult{
		TaskID:         taskID,
		Success:        false,
		LegacyErrorMsg: errorMsg,
		SentAt:         time.Now(),
	}

	if err := w.Produce(models.JobEventTypeEmailFailed, taskID.String(), result); err != nil {
		log.Error().Err(err).Str("task_id", taskID.String()).Msg("Failed to produce email failure event")
	}
}
