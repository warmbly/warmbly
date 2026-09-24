package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/email"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// validationBudget bounds unsealing plus both dials. With replyBudget it must
// sit well inside the backend's validationWait, which starts before publish.
const validationBudget = 7 * time.Second

// probeBudget bounds the two dials, and starts only once the credentials are
// unsealed so a cold key cache cannot turn a slow host into a refusal.
const probeBudget = 5 * time.Second

// replyBudget publishes the verdict on a context the probe deadline cannot cancel.
const replyBudget = 3 * time.Second

// What the worker says when it could not run the probes at all. Closed words:
// the cause is logged here and never shown to the person at the form.
const (
	verdictErrNoCredentials = "the check arrived without both legs' credentials"
	verdictErrUnseal        = "the worker could not unseal the credentials"
)

// HandleEmailValidation runs a credential check off the bus loop. The bus
// hands a worker one message at a time, so a check run inline waits behind
// every send and mailbox load queued ahead of it while the backend's clock is
// already running. The verdict travels over Redis, so the message itself is
// done the moment it has been read.
func (w *WorkerService) HandleEmailValidation(parent context.Context, data models.EventWorkerEmailValidation) error {
	ctx := context.WithoutCancel(parent)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				errs.Recover(r)
			}
		}()
		w.runEmailValidation(ctx, data)
	}()
	return nil
}

// ListenValidations takes credential checks from this worker's Redis request
// channel until ctx ends. go-redis resubscribes by itself after a dropped link.
func (w *WorkerService) ListenValidations(ctx context.Context) {
	id, err := uuid.Parse(w.ID)
	if err != nil || w.Cache == nil {
		return
	}
	sub := w.Cache.Subscribe(ctx, models.EmailValidationRequestChannel(id))
	defer sub.Close()
	msgs := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			var data models.EventWorkerEmailValidation
			if err := json.Unmarshal([]byte(msg.Payload), &data); err != nil || data.ProcessID == uuid.Nil {
				continue
			}
			_ = w.HandleEmailValidation(ctx, data)
		}
	}
}

// runEmailValidation unseals the credentials, probes both legs and always
// answers: a check the worker could not run is reported as such, because a
// silent worker reads at the backend as a mail server that never replied.
func (w *WorkerService) runEmailValidation(parent context.Context, data models.EventWorkerEmailValidation) {
	ctx, cancelAll := context.WithTimeout(parent, validationBudget)
	defer cancelAll()

	reply := func(v models.EmailValidationVerdict) {
		w.publishValidationVerdict(parent, data.ProcessID, v)
	}

	creds := data.Credentials
	if creds == nil || creds.IMAP == nil || creds.SMTP == nil {
		reply(models.EmailValidationVerdict{Error: verdictErrNoCredentials})
		return
	}

	cipher, err := w.CipherService.Cipher(ctx, data.OrgID)
	if err != nil {
		errs.CaptureException(err)
		reply(models.EmailValidationVerdict{Error: verdictErrUnseal})
		return
	}

	imapCreds, smtpCreds := *creds.IMAP, *creds.SMTP
	imapCreds.Password, err = cipher.Decrypt(ctx, imapCreds.Password)
	if err != nil {
		errs.CaptureException(err)
		reply(models.EmailValidationVerdict{Error: verdictErrUnseal})
		return
	}
	smtpCreds.Password, err = cipher.Decrypt(ctx, smtpCreds.Password)
	if err != nil {
		errs.CaptureException(err)
		reply(models.EmailValidationVerdict{Error: verdictErrUnseal})
		return
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeBudget)
	defer cancel()

	imapDone := make(chan email.ProbeResult, 1)
	smtpDone := make(chan email.ProbeResult, 1)
	// Credentials are untrusted user input. A panic in a bare goroutine cannot
	// be recovered by the caller and would take down the whole worker along
	// with every mailbox assigned to it, so each probe recovers its own.
	probe := func(out chan<- email.ProbeResult, fn func() email.ProbeResult) {
		go func() {
			res := email.ProbeResult{Reason: models.MailProbeProtocol, Detail: "the probe failed unexpectedly"}
			defer func() {
				if r := recover(); r != nil {
					errs.Recover(r)
				}
				out <- res
			}()
			res = fn()
		}()
	}
	probe(imapDone, func() email.ProbeResult {
		return email.VerifyImap(probeCtx, imapCreds.Host, imapCreds.Port, imapCreds.Username, imapCreds.Password, imapCreds.Security)
	})
	probe(smtpDone, func() email.ProbeResult {
		return email.VerifySMTP(probeCtx, smtpCreds.Host, smtpCreds.Port, smtpCreds.Username, smtpCreds.Password, smtpCreds.Security)
	})

	verdict := validationVerdict(<-smtpDone, <-imapDone)
	logProbe("smtp", &smtpCreds, verdict.SMTP)
	logProbe("imap", &imapCreds, verdict.IMAP)
	reply(verdict)
}

// publishValidationVerdict answers the backend on the process's channel.
func (w *WorkerService) publishValidationVerdict(parent context.Context, processID uuid.UUID, verdict models.EmailValidationVerdict) {
	replyCtx, replyCancel := context.WithTimeout(context.WithoutCancel(parent), replyBudget)
	defer replyCancel()
	channel := models.EmailValidationReplyChannel(processID)
	// The verdict goes first and the legacy digit after it: a backend that
	// reads verdicts returns on the first message, and one that predates them
	// skips what it cannot parse and takes the digit, so a fleet mid-update
	// keeps answering either way.
	if body, err := json.Marshal(verdict); err == nil {
		if err := w.Cache.Publish(replyCtx, channel, string(body)).Err(); err != nil {
			errs.CaptureException(err)
			return
		}
	}
	legacy := "0"
	if verdict.OK {
		legacy = "1"
	}
	if err := w.Cache.Publish(replyCtx, channel, legacy).Err(); err != nil {
		errs.CaptureException(err)
	}
}

// validationVerdict folds the two probes into the reply the backend reads.
func validationVerdict(smtp, imap email.ProbeResult) models.EmailValidationVerdict {
	return models.EmailValidationVerdict{
		OK:   smtp.OK && imap.OK,
		SMTP: smtp.Leg(),
		IMAP: imap.Leg(),
	}
}

// logProbe records a failed leg with what the server said, and never the
// password: this is the only trace a refused connect leaves on the worker.
func logProbe(leg string, svc *models.Service, res models.EmailValidationLeg) {
	if res.OK {
		return
	}
	log.Info().
		Str("leg", leg).
		Str("host", svc.Host).
		Int("port", svc.Port).
		Str("username", svc.Username).
		Str("reason", res.Reason).
		Str("detail", res.Detail).
		Msg("mailbox credential probe failed")
}
