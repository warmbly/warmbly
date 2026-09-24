package email

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// checkCredentials runs a credential check on the best worker that takes it.
// prefer is the mailbox's own worker on a reconnect, nil on a first connect.
func (s *emailService) checkCredentials(ctx context.Context, orgID uuid.UUID, prefer *uuid.UUID, creds *models.SmtpImap) *errx.Error {
	if s.workerAssignment == nil {
		return errx.ErrEmailOnboardNoWorker
	}
	workers, err := s.workerAssignment.ValidationWorkers(ctx, orgID, prefer)
	if err != nil || len(workers) == 0 {
		return errx.ErrEmailOnboardNoWorker
	}
	return s.ValidateCredentials(ctx, orgID, workers, creds)
}

// ValidateCredentials asks the workers in order to try the credentials, over
// each one's Redis request channel so no queued command delays the check. A
// worker not listening is skipped, a silent one costs one wait, and the bus is
// used only when none listens. Only the SMTP port may be rewritten (adoptProbedPort).
func (s *emailService) ValidateCredentials(ctx context.Context, orgID uuid.UUID, workers []models.Worker, credentials *models.SmtpImap) *errx.Error {
	if credentials == nil || credentials.SMTP == nil || credentials.IMAP == nil {
		return errx.ErrEmailCredentialsRequired
	}
	if len(workers) == 0 {
		return errx.ErrEmailOnboardNoWorker
	}

	cipher, err := s.cipherService.Cipher(ctx, orgID)
	if err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	sealedIMAP := *credentials.IMAP
	sealedIMAP.Password, err = cipher.Encrypt(ctx, credentials.IMAP.Password)
	if err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	sealedSMTP := *credentials.SMTP
	sealedSMTP.Password, err = cipher.Encrypt(ctx, credentials.SMTP.Password)
	if err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	sealed := &models.SmtpImap{SMTP: &sealedSMTP, IMAP: &sealedIMAP}

	silent := 0
	for _, w := range workers {
		if silent >= validationAttempts || ctx.Err() != nil {
			break
		}
		xerr, outcome := s.validateOn(ctx, orgID, w.ID, sealed, credentials, false)
		switch outcome {
		case validationAnswered:
			return xerr
		case validationSilent:
			silent++
		}
	}
	if silent > 0 || ctx.Err() != nil {
		return errx.ErrEmailValidation
	}
	xerr, outcome := s.validateOn(ctx, orgID, workers[0].ID, sealed, credentials, true)
	if outcome == validationSilent {
		return errx.ErrEmailValidation
	}
	return xerr
}

type validationOutcome int

const (
	validationAnswered validationOutcome = iota
	// validationNotListening: nobody took the request, so nothing was tried.
	validationNotListening
	// validationSilent: a worker took the request and never answered.
	validationSilent
)

// validationAttempts bounds how many silent workers one check waits on.
const validationAttempts = 2

// validateOn sends one check to one worker and waits for its verdict.
func (s *emailService) validateOn(ctx context.Context, orgID, workerID uuid.UUID, sealed, credentials *models.SmtpImap, overBus bool) (*errx.Error, validationOutcome) {
	processID := uuid.New()

	// Longer than the worker's own budget, so a verdict at its limit is still heard.
	waitCtx, cancel := context.WithTimeout(ctx, validationWait)
	defer cancel()

	// Confirmed before the request goes out: Redis keeps nothing for a channel nobody holds.
	sub := s.r.Subscribe(waitCtx, models.EmailValidationReplyChannel(processID))
	defer sub.Close()
	if _, err := sub.Receive(waitCtx); err != nil {
		if waitCtx.Err() != nil {
			return nil, validationSilent
		}
		errs.CaptureException(err)
		return errx.InternalError(), validationAnswered
	}

	req := models.EventWorkerEmailValidation{OrgID: orgID, ProcessID: processID, Credentials: sealed}
	if overBus {
		if err := s.publisher.PublishEmailValidation(ctx, workerID.String(), req); err != nil {
			errs.CaptureException(err)
			return errx.InternalError(), validationAnswered
		}
	} else {
		body, err := json.Marshal(req)
		if err != nil {
			return errx.InternalError(), validationAnswered
		}
		receivers, err := s.r.Publish(ctx, models.EmailValidationRequestChannel(workerID), body).Result()
		if err != nil {
			errs.CaptureException(err)
			return errx.InternalError(), validationAnswered
		}
		if receivers == 0 {
			return nil, validationNotListening
		}
	}

	xerr, answered := awaitVerdict(waitCtx, sub, workerID, processID, credentials)
	if !answered {
		log.Warn().Str("worker_id", workerID.String()).Bool("over_bus", overBus).Msg("mailbox validation: worker took the check and did not answer")
		return nil, validationSilent
	}
	return xerr, validationAnswered
}

// awaitVerdict reads the worker's answer; answered is false when none came in time.
func awaitVerdict(ctx context.Context, sub *redis.PubSub, workerID, processID uuid.UUID, credentials *models.SmtpImap) (*errx.Error, bool) {
	for {
		msg, err := sub.ReceiveMessage(ctx)
		if err != nil {
			// Ask the context, not the error: go-redis pushes the deadline onto
			// the socket as a net timeout. One while the context is live is Redis failing.
			if ctx.Err() != nil {
				return nil, false
			}
			errs.CaptureException(err)
			return errx.InternalError(), true
		}

		// A worker answers with a JSON verdict followed by the legacy digit.
		// Either alone is enough; a worker that predates verdicts sends only
		// the digit, and a payload that is neither is skipped.
		switch msg.Payload {
		case "1":
			return nil, true
		case "0":
			return errx.ErrEmailCredentials, true
		}
		var verdict models.EmailValidationVerdict
		if err := json.Unmarshal([]byte(msg.Payload), &verdict); err != nil {
			continue
		}
		if verdict.Error != "" {
			// The worker answered but never reached the mail server; that is
			// this side's failure, not the customer's host or port.
			err := fmt.Errorf("mailbox validation on worker %s did not run: %s", workerID, verdict.Error)
			log.Error().Str("worker_id", workerID.String()).Str("process_id", processID.String()).Msg(err.Error())
			errs.CaptureException(err)
			return errx.InternalError(), true
		}
		if verdict.OK {
			adoptProbedPort(credentials.SMTP, verdict.SMTP)
			return nil, true
		}
		return validationError(verdict, credentials), true
	}
}

// validationWait is how long the caller waits for a worker's verdict. It
// covers worker.validationBudget plus worker.replyBudget with room for the
// bus both ways, so a verdict produced at the worker's limit is still heard.
const validationWait = 14 * time.Second

// adoptProbedPort stores the port the worker actually signed in on. It is the
// one field a verdict may correct: a mailbox that kept naming a port the fleet
// cannot reach would fail on every send.
func adoptProbedPort(svc *models.Service, leg models.EmailValidationLeg) {
	if svc == nil || leg.Port == 0 || leg.Port == svc.Port {
		return
	}
	svc.Port = leg.Port
	svc.Security = leg.Security
}
