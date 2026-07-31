package email

import (
	"context"
	"errors"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ValidateCredentials seals a copy of the credentials with the org DEK and asks
// a worker to try them against the live servers. The caller's credentials are
// never mutated: they go on to be stored under the credentials key, and sealing
// them in place here would double-encrypt the stored password.
func (s *emailService) ValidateCredentials(ctx context.Context, orgID uuid.UUID, workerID string, credentials *models.SmtpImap) *errx.Error {
	processID := uuid.New()

	if credentials == nil || credentials.SMTP == nil || credentials.IMAP == nil {
		return errx.ErrEmailCredentialsRequired
	}

	cipher, err := s.cipherService.Cipher(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	sealedIMAP := *credentials.IMAP
	sealedIMAP.Password, err = cipher.Encrypt(ctx, credentials.IMAP.Password)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	sealedSMTP := *credentials.SMTP
	sealedSMTP.Password, err = cipher.Encrypt(ctx, credentials.SMTP.Password)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if err := s.publisher.PublishEmailValidation(ctx, workerID, models.EventWorkerEmailValidation{
		OrgID:       orgID,
		ProcessID:   processID,
		Credentials: &models.SmtpImap{SMTP: &sealedSMTP, IMAP: &sealedIMAP},
	}); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	subscribeContext, cancel := context.WithDeadline(ctx, time.Now().Add(5*time.Second))
	defer cancel()

	r := s.r.Subscribe(ctx, "email_validation:"+processID.String())
	defer r.Close()

	for {
		msg, err := r.ReceiveMessage(subscribeContext)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return errx.ErrEmailValidation
			}
			sentry.CaptureException(err)
			return errx.InternalError()
		}

		switch msg.Payload {
		case "1":
			return nil
		case "0":
			return errx.ErrEmailCredentials
		}
	}
}
