package worker

import (
	"context"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/warmbly/warmbly/internal/email"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (w *WorkerService) HandleEmailValidation(ctx context.Context, body any) error {
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(5*time.Second))
	defer cancel()

	data, ok := body.(models.EventWorkerEmailValidation)
	if !ok {
		err := errx.ErrInvalidEventFormat
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("event_type", string(models.WorkerEventTypeEmailValidation))
			scope.SetTag("process_id", data.ProcessID.String())
			scope.SetTag("org_id", data.OrgID.String())
			sentry.CaptureException(err)
		})
		return err
	}

	cipher, err := w.CipherService.Cipher(ctx, data.OrgID)
	if err != nil {
		sentry.CaptureException(err)
		return nil
	}

	data.Credentials.IMAP.Password, err = cipher.Decrypt(ctx, data.Credentials.IMAP.Password)
	if err != nil {
		sentry.CaptureException(err)
		return nil
	}

	data.Credentials.SMTP.Password, err = cipher.Decrypt(ctx, data.Credentials.SMTP.Password)
	if err != nil {
		sentry.CaptureException(err)
		return nil
	}

	var group sync.WaitGroup
	results := make(chan bool, 2)
	group.Add(2)
	go func() {
		results <- email.VerifyImap(ctx, data.Credentials.IMAP.Host, data.Credentials.IMAP.Port, data.Credentials.IMAP.Username, data.Credentials.IMAP.Password)
	}()
	go func() {
		results <- email.VerifySMTP(ctx, data.Credentials.SMTP.Host, data.Credentials.SMTP.Port, data.Credentials.SMTP.Username, data.Credentials.SMTP.Password)
	}()

	result1 := <-results
	result2 := <-results

	var msg string
	if result1 && result2 {
		msg = "1"
	} else {
		msg = "0"
	}

	if err := w.Cache.Publish(ctx, "email_validation:"+data.ProcessID.String(), msg).Err(); err != nil {
		sentry.CaptureException(err)
		return nil
	}

	return nil
}
