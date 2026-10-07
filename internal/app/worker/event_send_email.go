package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emsg"
	"github.com/warmbly/warmbly/internal/repository"
)

// errMailboxNotLoaded is what an owner reads when the worker never connected the mailbox.
const errMailboxNotLoaded = "The sending worker has not loaded this mailbox yet, so nothing was sent. A mail server the worker cannot reach, or one that refuses its login, keeps a mailbox from loading."

func (w *WorkerService) HandleSendEmail(ctx context.Context, sendEmail models.SendEmail) error {
	workerID, workerIDErr := uuid.Parse(w.ID)
	var dispatch repository.WorkerWarmupDispatch
	var requiresNonce bool
	{
		if d, ok := w.SyncContextRepository.(repository.WorkerWarmupDispatch); ok {
			if workerIDErr != nil || workerID == uuid.Nil {
				return errors.New("invalid warmup worker identity")
			}
			dispatch = d
			state, err := d.WarmupDispatch(ctx, sendEmail.TaskID, sendEmail.EmailID, workerID, uuid.Nil, false, nil)
			if err != nil {
				return err
			}
			if state == nil {
				return errors.New("warmup dispatch authority unavailable")
			}
			requiresNonce = state.State == "authorized"
			if requiresNonce && state.Recipients != nil {
				recipients := append(append(append([]string{}, sendEmail.To...), sendEmail.Cc...), sendEmail.Bcc...)
				if state.OrganizationID == nil || *state.OrganizationID != sendEmail.OrgID || !repository.MatchOutboundRecipients(state.Recipients, recipients) {
					return errors.New("send envelope does not match durable authorization")
				}
			}
			if state.State == "started" || state.State == "denied" {
				return nil
			}
			if state.State == "finished" {
				return w.replayWarmupResult(state.Result)
			}
			if !requiresNonce && state.State != "legacy" {
				return errors.New("invalid warmup dispatch state")
			}
		}
	}
	log.Info().
		Str("task_id", sendEmail.TaskID.String()).
		Str("email_id", sendEmail.EmailID.String()).
		Bool("is_warmup", sendEmail.IsWarmup).
		Msg("Processing send email event")

	// Get the email account from MailManager
	mail, exists := w.loadedMailbox(ctx, sendEmail.EmailID)

	if !exists {
		// The mailbox is not loaded here: it is still being added (its
		// ADD_EMAIL is queued behind this send), or this worker restarted and
		// the reconciler has not re-shipped it yet. Leave the send for
		// redelivery a few times so a queued ADD_EMAIL gets processed first,
		// then report the failure so the control plane retries the step.
		return w.failSend(ctx, sendEmail, errMailboxNotLoaded, true)
	}

	if sendEmail.OrgID != uuid.Nil && (mail.OrgID == nil || *mail.OrgID != sendEmail.OrgID) {
		return errors.New("send organization does not own loaded mailbox")
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
	body, err := w.fetchEmailBody(ctx, sendEmail.OrgID, sendEmail.BodyS3Key)
	if err != nil {
		log.Error().Err(err).Str("s3_key", sendEmail.BodyS3Key).Msg("Failed to fetch email body from S3")
		return w.failSend(ctx, sendEmail, fmt.Sprintf("failed to fetch email body: %v", err), true)
	}

	// Fetch each attachment's bytes from object storage by key. A fetch failure
	// fails the send rather than silently dropping a file the user expects.
	attachments, err := w.fetchAttachments(ctx, body.Attachments)
	if err != nil {
		log.Error().Err(err).Str("task_id", sendEmail.TaskID.String()).Msg("Failed to fetch attachment bytes from S3")
		return w.failSend(ctx, sendEmail, fmt.Sprintf("failed to fetch attachment: %v", err), true)
	}

	// Use unified Send method
	if requiresNonce && body.DispatchNonce == "" {
		return errors.New("warmup dispatch nonce missing")
	}
	if body.DispatchNonce != "" {
		if dispatch == nil {
			return errors.New("warmup dispatch authority unavailable")
		}
		nonce, err := uuid.Parse(body.DispatchNonce)
		if err != nil {
			return err
		}
		state, err := dispatch.WarmupDispatch(ctx, sendEmail.TaskID, sendEmail.EmailID, workerID, nonce, true, nil)
		if err != nil {
			return err
		}
		if state.State == "finished" {
			return w.replayWarmupResult(state.Result)
		}
		if state.State != "execute" {
			return nil
		}
	}
	w.recordSendAttempt()
	sendStart := time.Now()
	result := mail.Send(ctx, &wmail.SendRequest{
		TaskID:         sendEmail.TaskID,
		To:             sendEmail.To,
		Cc:             sendEmail.Cc,
		Bcc:            sendEmail.Bcc,
		MessageID:      sendEmail.MessageID,
		Subject:        subject,
		BodyPlain:      body.Plain,
		BodyHTML:       body.HTML,
		InReplyTo:      sendEmail.InReplyTo,
		References:     body.References,
		Parent:         sendEmail.Parent,
		IsWarmup:       sendEmail.IsWarmup,
		WarmupToken:    sendEmail.WarmupToken,
		UnsubscribeURL: sendEmail.UnsubscribeURL,
		Attachments:    attachments,
		FromName:       body.FromName,
		FromEmail:      body.FromEmail,
		ReplyTo:        body.ReplyTo,
	})
	w.recordSendLatency(time.Since(sendStart))
	w.recordSendOutcome(result)
	if body.DispatchNonce != "" {
		stored := models.SendEmailResult{TaskID: sendEmail.TaskID, Success: result.Success, MessageID: result.MessageID, ProviderMsgID: result.ProviderMsgID, ThreadID: result.ThreadID, SentAt: time.Now().UTC()}
		if result.Error != nil {
			stored.Error = wmail.MailErrorToSendError(result.Error)
		}
		if _, err := dispatch.WarmupDispatch(ctx, sendEmail.TaskID, sendEmail.EmailID, workerID, uuid.Nil, false, &stored); err != nil {
			return err
		}
		if err := w.replayWarmupResult(&stored); err != nil {
			return err
		}
		if result.Success {
			w.deleteTransportEmailBody(ctx, sendEmail.TaskID, sendEmail.BodyS3Key)
			return nil
		}
	}

	if result.Success {
		log.Info().
			Str("task_id", sendEmail.TaskID.String()).
			Str("message_id", result.MessageID).
			Str("provider_msg_id", result.ProviderMsgID).
			Str("thread_id", result.ThreadID).
			Msg("Email sent successfully")

		w.deleteTransportEmailBody(ctx, sendEmail.TaskID, sendEmail.BodyS3Key)

		w.sendEmailSuccess(sendEmail.TaskID, result.MessageID, result.ProviderMsgID, result.ThreadID)
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

func (w *WorkerService) replayWarmupResult(result *models.SendEmailResult) error {
	if result == nil {
		return errors.New("warmup result unavailable")
	}
	kind := models.JobEventTypeEmailFailed
	if result.Success {
		kind = models.JobEventTypeEmailSent
	}
	return w.Produce(kind, result.TaskID.String(), *result)
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

// sendBody is what one send needs out of the transport blob: the decrypted
// bodies, the attachment refs, and the sender identity resolved at publish
// time. The identity fields are empty when the publisher predates them, which
// means "use the mailbox's own".
type sendBody struct {
	DispatchNonce string
	References    string
	Plain         string
	HTML          string
	Attachments   []emsg.Attachment
	FromName      string
	FromEmail     string
	ReplyTo       string
}

// fetchEmailBody fetches and decodes the email body from S3.
func (w *WorkerService) fetchEmailBody(ctx context.Context, orgID uuid.UUID, s3Key string) (*sendBody, error) {
	if w.Storage == nil {
		return nil, fmt.Errorf("storage client not configured")
	}

	// Get object from storage
	body, err := w.Storage.Get(ctx, s3Key)
	if err != nil {
		return nil, fmt.Errorf("failed to get S3 object: %w", err)
	}
	defer body.Close()

	// Read the body
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("failed to read S3 object: %w", err)
	}

	// Decode using emsg
	blob, err := emsg.DecodeBinary(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode emsg blob: %w", err)
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

	return &sendBody{
		Plain:         bodyPlain,
		HTML:          bodyHTML,
		Attachments:   blob.Attachments,
		FromName:      blob.FromName,
		FromEmail:     blob.FromEmail,
		ReplyTo:       blob.ReplyTo,
		References:    blob.References,
		DispatchNonce: blob.DispatchNonce,
	}, nil
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

// sendEmailSuccess sends a success result back to the jobs service. threadID is
// the provider conversation handle (Gmail only, empty elsewhere) the control
// plane needs to append the next step of a sequence to the same thread.
func (w *WorkerService) sendEmailSuccess(taskID uuid.UUID, messageID, providerMsgID, threadID string) {
	result := models.SendEmailResult{
		TaskID:        taskID,
		Success:       true,
		MessageID:     messageID,
		ProviderMsgID: providerMsgID,
		ThreadID:      threadID,
		SentAt:        time.Now(),
	}

	if err := w.Produce(models.JobEventTypeEmailSent, taskID.String(), result); err != nil {
		log.Error().Err(err).Str("task_id", taskID.String()).Msg("Failed to produce email sent event")
	}
}

// sendNotLoadedRedeliveries is how many bus deliveries a send may burn waiting
// for a transient condition (mailbox not loaded yet, object storage blip)
// before the worker reports it failed. Each redelivery is a second apart, and
// the bus stops redelivering at ten, so this stays well inside that.
const sendNotLoadedRedeliveries = 5

// failSend reports a send the worker could not attempt. A retryable condition
// is first left for bus redelivery (returning an error naks the message) so a
// queued ADD_EMAIL or a storage blip can clear; once the redeliveries are
// spent, when the bus does not redeliver, or when the condition is not
// retryable, the failure result is produced and the message is acked, handing
// the retry to the control plane.
func (w *WorkerService) failSend(ctx context.Context, sendEmail models.SendEmail, reason string, retryable bool) error {
	if d := deliveryOf(ctx); retryable && d.redelivers && d.attempt < sendNotLoadedRedeliveries {
		return errors.New(reason)
	}
	w.sendEmailFailure(sendEmail.TaskID, sendEmail.EmailID, nil, reason)
	return nil
}

// sendEmailError reports a failed send attempt. The per-task result is always
// an EMAIL_FAILED so the control plane has one result channel to walk the
// send back on; account-level conditions (auth, disabled, rate limit, server
// error) additionally raise their own typed event carrying the full context.
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

	if err := w.Produce(models.JobEventTypeEmailFailed, taskID.String(), result); err != nil {
		log.Error().Err(err).Str("task_id", taskID.String()).Msg("Failed to produce email failed event")
	}

	// Account-level conditions also raise their typed event with full context
	if eventType == models.JobEventTypeEmailAuthError ||
		eventType == models.JobEventTypeEmailDisabled ||
		eventType == models.JobEventTypeEmailRateLimited ||
		eventType == models.JobEventTypeEmailServerError {

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
	now := time.Now().UTC()
	result := models.SendEmailResult{
		TaskID:         taskID,
		Success:        false,
		LegacyErrorMsg: errorMsg,
		SentAt:         now,
		Error: &models.EmailSendError{Code: string(errx.MailErrorCodeServerUnreachable), Message: errorMsg, Failure: &errx.SendFailure{
			Protocol: "internal", Stage: "prepare", Disposition: errx.SendRetry, Scope: "mailbox", ObservedAt: now,
		}},
	}

	if err := w.Produce(models.JobEventTypeEmailFailed, taskID.String(), result); err != nil {
		log.Error().Err(err).Str("task_id", taskID.String()).Msg("Failed to produce email failure event")
	}
}
