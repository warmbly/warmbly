package worker

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/warmbly/warmbly/internal/email"
	"github.com/warmbly/warmbly/internal/models"
)

func (w *WorkerService) HandleEmailValidation(ctx context.Context, data models.EventWorkerEmailValidation) error {
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(5*time.Second))
	defer cancel()

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

	results := make(chan bool, 2)
	// Credentials are untrusted user input. A panic in a bare goroutine cannot
	// be recovered by the caller and would take down the whole worker along
	// with every mailbox assigned to it, so each probe recovers its own.
	probe := func(fn func() bool) {
		go func() {
			ok := false
			defer func() {
				if r := recover(); r != nil {
					sentry.CurrentHub().Recover(r)
				}
				results <- ok
			}()
			ok = fn()
		}()
	}
	probe(func() bool {
		return email.VerifyImap(ctx, data.Credentials.IMAP.Host, data.Credentials.IMAP.Port, data.Credentials.IMAP.Username, data.Credentials.IMAP.Password, data.Credentials.IMAP.Security)
	})
	probe(func() bool {
		return email.VerifySMTP(ctx, data.Credentials.SMTP.Host, data.Credentials.SMTP.Port, data.Credentials.SMTP.Username, data.Credentials.SMTP.Password, data.Credentials.SMTP.Security)
	})

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
