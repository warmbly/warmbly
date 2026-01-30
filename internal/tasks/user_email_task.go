package tasks

import (
	"context"
	"fmt"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

func (s *tasksService) HandleUserEmailTask(task *proto.ProcessTask) *errx.Error {
	ctx := context.Background()

	// STEP 1: Parse task ID
	taskID, err := uuid.Parse(task.TaskId)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.BadRequest, "invalid task ID")
	}

	// STEP 2: Load task record
	taskRecord, err := s.taskRepo.GetTask(ctx, taskID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if taskRecord == nil {
		return errx.ErrNotFound
	}

	// STEP 3: Load email_task record (with new columns)
	emailTask, err := s.taskRepo.GetEmailTask(ctx, taskID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if emailTask == nil {
		return errx.ErrNotFound
	}

	// STEP 4: Load email account
	account, xerr := s.emailRepo.GetByID(ctx, taskRecord.EmailAccountID)
	if xerr != nil {
		return xerr
	}

	// STEP 5: Mark task as active (with advisory lock)
	if err := s.taskRepo.UpdateTaskStatusWithLock(ctx, taskID, "active"); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	// STEP 6: Add signature if SignatureSync
	bodyHTML := emailTask.BodyHTML
	bodyPlain := emailTask.BodyPlain

	if account.SignatureSync {
		if bodyHTML != "" {
			bodyHTML = AddSignature(bodyHTML, account.SignatureHTML, true)
		}
		if bodyPlain != "" {
			bodyPlain = AddSignature(bodyPlain, account.SignaturePlain, false)
		}
	}

	// STEP 7: Generate Message-ID
	messageID := generateMessageID(account.Email)

	// STEP 8: Build InReplyTo string
	var inReplyTo string
	if len(emailTask.InReplyTo) > 0 {
		inReplyTo = emailTask.InReplyTo[0]
	}

	// STEP 9: Build EmailMessage and send via worker
	emailMsg := EmailMessage{
		From:      account.Email,
		To:        emailTask.To,
		CC:        emailTask.CC,
		BCC:       emailTask.BCC,
		Subject:   emailTask.Subject,
		BodyHTML:  bodyHTML,
		BodyPlain: bodyPlain,
		InReplyTo: inReplyTo,
		MessageID: messageID,
		IsWarmup:  false,
		Tracking:  nil,
	}

	if err := s.emailSender.Send(ctx, taskID, emailMsg, *account); err != nil {
		s.taskRepo.RecordTaskFailure(ctx, taskID, "Send failed", err.Error())
		return nil
	}

	// STEP 10: Update task record
	taskRecord.MessageID = messageID
	taskRecord.Status = "completed"

	// STEP 11: Mark task completed (with advisory lock)
	if err := s.taskRepo.UpdateTaskStatusWithLock(ctx, taskID, "completed"); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	// STEP 12: Publish events
	fmt.Printf("User email sent: task=%s, account=%s\n", task.TaskId, account.ID)

	return nil
}
