package email

import (
	"context"
	"errors"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *emailService) ValidateCredentials(ctx context.Context, orgID uuid.UUID, workerID string, credentials *models.SmtpImap) *errx.Error {
	processID := uuid.New()

	cipher, err := s.cipherService.Cipher(ctx, orgID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	credentials.IMAP.Password, err = cipher.Encrypt(ctx, credentials.IMAP.Password)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	credentials.SMTP.Password, err = cipher.Encrypt(ctx, credentials.SMTP.Password)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	eventData := models.WorkerEvent{
		Type: models.WorkerEventTypeEmailValidation,
		Body: models.EventWorkerEmailValidation{
			OrgID:       orgID,
			ProcessID:   processID,
			Credentials: credentials,
		},
	}

	topic := kafka.GetWorkerTopic(workerID)

	msgBytes, err := s.producer.Avrov2.Ser.Serialize(topic, eventData)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if err := s.producer.Produce(topic, []byte(orgID.String()), msgBytes); err != nil {
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
