package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Warmup mail retention.
//
// Warmup mail is real mail in the customer's mailbox, and once its engagement
// has been recorded there is nothing left to keep it for. A mailbox on a fixed
// quota, which is every mailbox outside Google Workspace, would otherwise fill
// with it, and a full mailbox receives nothing at all. So the platform deletes
// it: every received copy and every sender's own copy past the mailbox's
// window (its own, else retention.warmup_mail_days) is retired here and a
// delete action goes to the worker holding the mailbox, which removes it from
// the provider and drops the stored body.
//
// The row is retired only after the action is on the bus, so a publish that
// fails leaves the message to be offered again next pass. The removal the
// sync then observes is outside config.WarmupDeletionStrikeHours by
// construction (the window floor is days), and the retired stamp says whose
// deletion it was, so it can never be a strike.
//
// The same loop prunes the per-message warmup records past
// retention.warmup_event_days once a full pass has completed.

const (
	warmupRetentionBatch = 500
	// warmupRetentionRunBudget is how long one run keeps taking batches, so a backlog clears in hours, not weeks.
	warmupRetentionRunBudget = 40 * time.Second
	// warmupRetentionPassEvery is how often a completed pass is repeated. The
	// windows are whole days, so nothing is gained by walking the tables
	// more often than this.
	warmupRetentionPassEvery = 6 * time.Hour
)

// StartWarmupMailRetention runs the retention sweep in bounded batches, and
// the record prune after each complete pass.
func (s *JobsService) StartWarmupMailRetention(ctx context.Context) {
	if s.WarmupRepo == nil || s.Publisher == nil {
		return
	}
	var nextPass time.Time
	jobrun.Loop(ctx, "warmup_mail_retention", time.Minute, true, func(ctx context.Context) error {
		if time.Now().Before(nextPass) {
			return nil
		}
		batchCtx, cancel := context.WithTimeout(ctx, 55*time.Second)
		defer cancel()
		started, run := time.Now(), newRetireRun()
		for {
			done, err := s.retireWarmupMailBatch(batchCtx, run)
			if err != nil {
				return err
			}
			if done {
				break
			}
			if time.Since(started) > warmupRetentionRunBudget {
				return nil
			}
		}
		if run.full {
			// A mailbox took its whole share this run and may hold more; the next run continues it.
			return nil
		}
		// The record prune gets its own budget, so a long retirement run cannot starve it.
		pruneCtx, cancelPrune := context.WithTimeout(ctx, 5*time.Minute)
		defer cancelPrune()
		if err := s.pruneWarmupEvents(pruneCtx); err != nil {
			return err
		}
		nextPass = time.Now().Add(warmupRetentionPassEvery)
		return nil
	})
}

// warmupRetentionDays is the instance window every mailbox without one of its
// own follows, read on every batch so an admin edit applies to the next one.
func (s *JobsService) warmupRetentionDays(ctx context.Context) (mail, events int) {
	mail, events = config.WarmupMailRetentionDaysDefault, config.WarmupEventRetentionDaysDefault
	if s.Retention != nil {
		r := s.Retention.RetentionWindows(ctx)
		if r.WarmupMailDays >= config.WarmupMailRetentionDaysMin {
			mail = r.WarmupMailDays
		}
		if r.WarmupEventDays >= config.WarmupEventRetentionDaysMin {
			events = r.WarmupEventDays
		}
	}
	return mail, events
}

// retireRun is what one run has handed each mailbox, so no mailbox gets more than its share a run.
type retireRun struct {
	received, sent map[uuid.UUID]bool
	// full is set when a mailbox took its whole share, so the pass is not complete.
	full bool
}

func newRetireRun() *retireRun {
	return &retireRun{received: map[uuid.UUID]bool{}, sent: map[uuid.UUID]bool{}}
}

// take records a batch against the run and reports whether the listing ran out; a mailbox it skips next is marked by full.
func (r *retireRun) take(served map[uuid.UUID]bool, batch []repository.WarmupMailToRetire) bool {
	for _, m := range batch {
		served[m.EmailAccountID] = true
	}
	if atShare(batch) {
		r.full = true
	}
	return len(batch) < warmupRetentionBatch
}

func keys(m map[uuid.UUID]bool) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	return out
}

// retireWarmupMailBatch offers one batch of received copies and one of sent
// copies to the workers, skipping mailboxes this run already served. It
// reports done when both listings came back drained.
func (s *JobsService) retireWarmupMailBatch(ctx context.Context, run *retireRun) (bool, error) {
	days, _ := s.warmupRetentionDays(ctx)

	received, err := s.WarmupRepo.ListWarmupMailToRetire(ctx, days, warmupRetentionBatch, keys(run.received))
	if err != nil {
		return false, err
	}
	var failures []error
	for i := range received {
		m := &received[i]
		if err := s.publishWarmupDelete(ctx, m); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := s.WarmupRepo.RetireWarmupReceived(ctx, m.EmailAccountID, m.InternalID); err != nil {
			failures = append(failures, err)
		}
	}

	sent, err := s.WarmupRepo.ListWarmupSentCopiesToRetire(ctx, days, warmupRetentionBatch, keys(run.sent))
	if err != nil {
		return false, errors.Join(append(failures, err)...)
	}
	for i := range sent {
		m := &sent[i]
		if err := s.publishWarmupDelete(ctx, m); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := s.WarmupRepo.RetireWarmupSentCopy(ctx, m.Token); err != nil {
			failures = append(failures, err)
		}
	}

	if len(failures) > 0 {
		// A row whose publish failed was not retired and is offered again;
		// the pass is not complete until every row of the batch went.
		return false, errors.Join(failures...)
	}
	doneReceived, doneSent := run.take(run.received, received), run.take(run.sent, sent)
	return doneReceived && doneSent, nil
}

// drained is a listing that came back short with no mailbox at its per-mailbox share, so nothing older is waiting.
func drained(batch []repository.WarmupMailToRetire) bool {
	return len(batch) < warmupRetentionBatch && !atShare(batch)
}

// atShare reports a mailbox that took its whole per-mailbox share and may hold more.
func atShare(batch []repository.WarmupMailToRetire) bool {
	per := map[uuid.UUID]int{}
	for _, m := range batch {
		if per[m.EmailAccountID]++; per[m.EmailAccountID] >= repository.WarmupRetirePerMailbox {
			return true
		}
	}
	return false
}

// publishWarmupDelete sends the delete for one message to the worker holding
// its mailbox. The action carries every key the worker can find the message
// by, and where the mailbox files warmup, so the search starts in the folder
// the message is most likely in.
func (s *JobsService) publishWarmupDelete(ctx context.Context, m *repository.WarmupMailToRetire) error {
	filing := models.Email{WarmupPlacement: m.Placement, WarmupFolder: m.Folder}
	placement, folder := filing.WarmupFiling()
	action := &models.WarmupEmailAction{
		UserID:       m.UserID,
		EmailID:      m.EmailAccountID,
		GmailID:      m.ProviderKey,
		RFCMessageID: m.MessageID,
		Actions:      []string{models.WarmupActionDelete},
		Placement:    placement,
		TargetFolder: folder,
	}
	if m.InternalID != [16]byte{} {
		action.InternalID = m.InternalID.String()
	}
	return s.Publisher.PublishWarmupAction(ctx, m.WorkerID, action)
}

// pruneWarmupEvents drops the per-message warmup records past the instance
// window. Runs after a complete retention pass, so a record is only ever
// pruned once the mail it describes has been retired.
func (s *JobsService) pruneWarmupEvents(ctx context.Context) error {
	mailDays, days := s.warmupRetentionDays(ctx)
	before := time.Now().AddDate(0, 0, -days)
	pruned, err := s.WarmupRepo.PruneWarmupEventsBefore(ctx, before, mailDays)
	if err != nil {
		return err
	}
	if pruned > 0 {
		log.Info().Int64("pruned", pruned).Int("days", days).Msg("warmup: per-message records past the retention window pruned")
	}
	return nil
}
