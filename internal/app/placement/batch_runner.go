package placement

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// senderOutcome is what one attempt to start a batch sender came to.
type senderOutcome int

const (
	outcomeStarted senderOutcome = iota
	// outcomeNotStarted: skipped or deferred, the batch goes on.
	outcomeNotStarted
	// outcomeStopBatch: no other sender could start either (the allowance or
	// the balance ran out, the campaign went away).
	outcomeStopBatch
)

// runBatches advances every active batch: resolve senders whose tests
// finished, start the next due ones within the workspace's concurrency and the
// start rate, and close a batch with nothing left to do.
func (s *service) runBatches(ctx context.Context) {
	if s.Batches == nil {
		return
	}
	if err := s.Batches.CloseInactiveBatches(ctx); err != nil {
		errs.CaptureException(err)
	}
	s.settleCancelledBatches(ctx)
	now := s.now()
	batches, err := s.Batches.ClaimBatches(ctx, now, 2*time.Minute, config.PlacementBatchRunnerBatchesPerTick)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	pol := s.policy(ctx)
	instance, err := s.Batches.CountSendingBatchSenders(ctx, nil)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	pace := batchPace{
		orgLimit: pol.BatchSenderConcurrency, instanceLimit: pol.BatchInstanceConcurrency,
		perMinute: pol.BatchStartsPerMinute, instance: instance, sending: map[uuid.UUID]int{},
	}
	for i := range batches {
		b := &batches[i]
		s.advanceBatch(ctx, b, &pace, now)
		if err := s.Batches.ReleaseBatch(ctx, b.ID); err != nil {
			errs.CaptureException(err)
		}
	}
}

// settleCancelledBatches finishes what a cancel leaves behind: a test a sender
// started while the batch was being cancelled is cancelled too, and senders
// whose tests have since finished are resolved.
func (s *service) settleCancelledBatches(ctx context.Context) {
	late, err := s.Batches.RunningTestsOfCancelledBatches(ctx, 100)
	if err != nil {
		errs.CaptureException(err)
	}
	for _, t := range late {
		if _, err := s.Repo.CancelTest(ctx, t.OrganizationID, t.TestID); err != nil {
			errs.CaptureException(err)
		}
	}
	if err := s.Batches.SyncClosedBatchSenders(ctx); err != nil {
		errs.CaptureException(err)
	}
}

// batchPace is one runner pass's view of how many batch senders are sending,
// per workspace and across the instance, against their limits.
type batchPace struct {
	orgLimit, instanceLimit, perMinute int
	instance                           int
	sending                            map[uuid.UUID]int
}

func (s *service) advanceBatch(ctx context.Context, b *models.PlacementBatch, pace *batchPace, now time.Time) {
	stale := now.Add(-time.Duration(config.PlacementBatchSenderStaleMinutes) * time.Minute)
	if err := s.Batches.SyncBatchSenders(ctx, b.ID, stale); err != nil {
		errs.CaptureException(err)
		return
	}
	if now.After(b.RetryUntil) {
		if err := s.Batches.CloseOpenBatchSenders(ctx, b.ID, []string{models.PlacementSenderDeferred}, models.PlacementSenderSkipped,
			"placement_batch_retry_expired", "Still could not run when the batch's retry window closed."); err != nil {
			errs.CaptureException(err)
		}
	}

	if _, ok := pace.sending[b.OrganizationID]; !ok {
		orgID := b.OrganizationID
		n, err := s.Batches.CountSendingBatchSenders(ctx, &orgID)
		if err != nil {
			errs.CaptureException(err)
			return
		}
		pace.sending[b.OrganizationID] = n
	}
	slots := min(pace.orgLimit-pace.sending[b.OrganizationID], pace.instanceLimit-pace.instance,
		startAllowance(pace.perMinute, b.LastTickAt, now))
	moved := false
	if slots > 0 {
		due, err := s.Batches.DueBatchSenders(ctx, b.ID, now, slots*4+10)
		if err != nil {
			errs.CaptureException(err)
			return
		}
		started := 0
		for _, snd := range due {
			if started >= slots {
				break
			}
			outcome, reason, detail := s.startBatchSender(ctx, b, snd, now)
			moved = true
			if outcome == outcomeStarted {
				started++
				pace.sending[b.OrganizationID]++
				pace.instance++
				continue
			}
			if outcome == outcomeStopBatch {
				if err := s.Batches.CloseOpenBatchSenders(ctx, b.ID,
					[]string{models.PlacementSenderQueued, models.PlacementSenderDeferred},
					models.PlacementSenderSkipped, reason, detail); err != nil {
					errs.CaptureException(err)
				}
				break
			}
		}
		if started > 0 && b.Status == models.PlacementBatchQueued {
			if err := s.Batches.MarkBatchStarted(ctx, b.ID); err != nil {
				errs.CaptureException(err)
			}
			b.Status = models.PlacementBatchRunning
		}
	}

	progress, err := s.Batches.BatchProgress(ctx, []uuid.UUID{b.ID})
	if err != nil {
		errs.CaptureException(err)
		return
	}
	p := progress[b.ID]
	if p.Open() > 0 {
		if moved {
			s.publishBatch(ctx, b)
		}
		return
	}
	s.finishBatch(ctx, b, p)
}

// startAllowance is how many senders a batch may start this pass: its rate
// over the time since it was last advanced, at most a minute's worth, and at
// least one so a slow rate still moves.
func startAllowance(perMinute int, last *time.Time, now time.Time) int {
	if perMinute <= 0 {
		return 0
	}
	elapsed := time.Minute
	if last != nil {
		elapsed = min(max(now.Sub(*last), 0), time.Minute)
	}
	return max(1, int(float64(perMinute)*elapsed.Seconds()/60))
}

// startBatchSender starts one sender's test, or records why it did not start.
// The sender is claimed first, so a restart between the claim and the test
// can never start it twice; SyncBatchSenders returns a claim with no test.
func (s *service) startBatchSender(ctx context.Context, b *models.PlacementBatch, snd models.PlacementBatchSender, now time.Time) (senderOutcome, string, string) {
	record := func(status, reason, detail string, next time.Time) (senderOutcome, string, string) {
		if err := s.Batches.SetBatchSenderOutcome(ctx, snd.ID, status, reason, detail, next); err != nil {
			errs.CaptureException(err)
		}
		return outcomeNotStarted, reason, detail
	}
	if snd.EmailAccountID == nil {
		return record(models.PlacementSenderSkipped, "placement_sender_deleted", "The mailbox was removed from the workspace.", now)
	}
	// The mailbox is checked here so a not-found from the test below means
	// the batch's campaign or step went away, which stops every sender.
	acct, xerr := s.Emails.GetByID(ctx, *snd.EmailAccountID)
	if xerr != nil || acct == nil || acct.OrganizationID == nil || *acct.OrganizationID != b.OrganizationID {
		return record(models.PlacementSenderSkipped, "placement_sender_deleted", "The mailbox was removed from the workspace.", now)
	}
	ok, err := s.Batches.ClaimBatchSender(ctx, snd.ID)
	if err != nil {
		errs.CaptureException(err)
		return outcomeNotStarted, "", ""
	}
	if !ok {
		return outcomeNotStarted, "", ""
	}
	batchID, senderID := b.ID, snd.ID
	views, xerr := s.CreateTests(ctx, CreateInput{
		OrgID:           b.OrganizationID,
		UserID:          b.CreatedBy,
		SenderAccountID: *snd.EmailAccountID,
		CampaignID:      b.CampaignID,
		SequenceID:      b.SequenceID,
		ContactID:       b.ContactID,
		Subject:         b.Subject,
		BodyHTML:        b.BodyHTML,
		BodyPlain:       b.BodyPlain,
		Tracking:        b.Tracking,
		Panel:           b.Panel,
		SeedIDs:         b.SeedIDs,
		Families:        b.Families,
		Pace:            models.PlacementPaceSpaced,
		MaxSpend:        max(0, b.MaxMicro-b.SpentMicro),
		Origin:          models.PlacementOriginBatch,
		BatchID:         &batchID,
		BatchSenderID:   &senderID,
	})
	if xerr == nil {
		var charged models.Micro
		for _, v := range views {
			charged += v.ChargedMicro
		}
		if charged > 0 {
			b.SpentMicro += charged
			if err := s.Batches.AddBatchSpend(ctx, b.ID, charged); err != nil {
				errs.CaptureException(err)
			}
		}
		return outcomeStarted, "", ""
	}

	retry := b.OnUnavailable == models.PlacementUnavailableDefer
	unavailable := func(next time.Time) (senderOutcome, string, string) {
		if retry {
			return record(models.PlacementSenderDeferred, xerr.Identifier, xerr.Message, next)
		}
		return record(models.PlacementSenderSkipped, xerr.Identifier, xerr.Message, now)
	}
	switch xerr.Identifier {
	case "placement_sender_busy":
		// Another test is still sending from it; that is minutes, not a day.
		return record(models.PlacementSenderDeferred, xerr.Identifier, xerr.Message, now.Add(15*time.Minute))
	case "placement_daily_budget":
		return unavailable(nextSendingDay(now))
	case "placement_sender_unavailable", "placement_invalid_seeds":
		return unavailable(now.Add(time.Hour))
	case "placement_no_seeds":
		// Every seed is on this sender's own domain; no retry changes that.
		return record(models.PlacementSenderSkipped, xerr.Identifier, xerr.Message, now)
	case "placement_quota_exceeded", "insufficient_credits", "usage_cap_exceeded", "placement_not_entitled",
		"placement_panel_unavailable", "placement_invalid_tracking":
		record(models.PlacementSenderSkipped, xerr.Identifier, xerr.Message, now)
		return outcomeStopBatch, xerr.Identifier, xerr.Message
	}
	switch xerr.Code {
	case errx.NotFound, errx.BadRequest:
		record(models.PlacementSenderSkipped, "placement_batch_copy_unavailable", xerr.Message, now)
		return outcomeStopBatch, "placement_batch_copy_unavailable", "The batch's campaign or email is no longer available: " + xerr.Message
	}
	// Anything else is unexpected: try again later, then give up on the sender.
	if snd.Attempts+1 >= config.PlacementBatchSenderErrorAttemptsMax {
		if err := s.Batches.SetBatchSenderOutcome(ctx, snd.ID, models.PlacementSenderFailed, "placement_batch_start_failed",
			"The test could not be started.", now); err != nil {
			errs.CaptureException(err)
		}
		return outcomeNotStarted, "", ""
	}
	return record(models.PlacementSenderDeferred, "placement_batch_start_failed", "The test could not be started; it is retried shortly.",
		now.Add(time.Duration(snd.Attempts+1)*10*time.Minute))
}

// nextSendingDay is a moment early in the next UTC day, when a mailbox's daily
// count starts over, spread over two hours so a deferred fleet does not all
// start at midnight.
func nextSendingDay(now time.Time) time.Time {
	day := now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	return day.Add(time.Duration(rand.Int64N(int64(2 * time.Hour))))
}

// finishBatch closes a batch with nothing left to run and tells its creator.
func (s *service) finishBatch(ctx context.Context, b *models.PlacementBatch, p models.PlacementBatchProgress) {
	status, msg := models.PlacementBatchCompleted, ""
	switch {
	case p.Completed == 0:
		status, msg = models.PlacementBatchFailed, "No sender finished a test."
	case p.Skipped+p.Failed+p.Cancelled > 0:
		status = models.PlacementBatchCompletedWithWarnings
	}
	ok, err := s.Batches.FinishBatch(ctx, b.ID, status, msg)
	if err != nil {
		errs.CaptureException(err)
		return
	}
	if !ok {
		return
	}
	b.Status = status
	s.publishBatch(ctx, b)
	if b.CreatedBy == nil || s.Notifier == nil {
		return
	}
	body := fmt.Sprintf("%d of %d mailboxes finished a test.", p.Completed, p.Total)
	if sums, err := s.Batches.BatchSummaries(ctx, []uuid.UUID{b.ID}); err == nil {
		c := sums[b.ID]
		c.Finish()
		body += " " + summaryLine(c)
	}
	orgID := b.OrganizationID
	s.Notifier.Notify(ctx, *b.CreatedBy, &orgID, models.NotifPlacementFinished, "Placement batch finished", body,
		"/app/placement/batches/"+b.ID.String(), map[string]any{"placement_batch_id": b.ID.String()})
}
