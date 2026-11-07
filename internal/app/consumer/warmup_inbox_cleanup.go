package jobs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/arrivaldiag"
	"github.com/warmbly/warmbly/internal/repository"
)

// StartWarmupInboxCleanup repairs old leaks in bounded passes, retrying verification failures per mailbox.
func (s *JobsService) StartWarmupInboxCleanup(ctx context.Context) {
	if s.UniboxRepository == nil {
		return
	}
	sweep := &warmupInboxSweep{}
	var nextPass time.Time
	jobrun.Loop(ctx, "warmup_inbox_cleanup", time.Minute, true, func(ctx context.Context) error {
		if time.Now().Before(nextPass) {
			return jobrun.Quiet(sweep.heldError())
		}
		passCtx, cancel := context.WithTimeout(ctx, warmupCleanupPassBudget)
		defer cancel()
		done, err := s.cleanWarmupInboxBatch(passCtx, sweep)
		if done {
			nextPass = sweep.finish(time.Now())
		}
		if err != nil {
			return err
		}
		// Already reported when it was held; the panel keeps showing it until the mailbox clears.
		return jobrun.Quiet(sweep.heldError())
	})
}

const (
	warmupCleanupInterval   = 24 * time.Hour
	warmupCleanupRetryDelay = 15 * time.Minute
	warmupCleanupPassBudget = 45 * time.Second
	// A candidate can run both Cloud checks to their timeout, so none starts this close to the pass deadline.
	warmupCleanupCandidateReserve = 2*cloudWarmupCheckTimeout + 5*time.Second
	warmupCleanupPageSize         = 100
	warmupCleanupPassChecks       = 100
	warmupCleanupPassPages        = 10
	// A row that outruns the pass this many times in a row holds its mailbox instead of stalling the sweep.
	warmupCleanupMaxInterruptions = 2
)

// heldMailbox is a mailbox whose check failed, waiting to be retried from where it stopped.
type heldMailbox struct {
	resume  uuid.UUID // cursor before its first unfinished row
	retries int       // retry sweeps in a row that finished none of its rows
	due     time.Time
	err     error
	seq     int // order of holds, so the summary names the latest one
}

// warmupInboxSweep is the cleanup's position, carried across one-minute passes.
type warmupInboxSweep struct {
	afterID uuid.UUID
	// held are the mailboxes this sweep stopped checking after a failure.
	held map[uuid.UUID]heldMailbox
	// only limits a retry sweep to the mailboxes due, each past its own resume point.
	only map[uuid.UUID]heldMailbox
	// waiting are held mailboxes whose retry is not due yet.
	waiting map[uuid.UUID]heldMailbox
	// deferred are held mailboxes left for the next daily sweep, which comes before their next retry would.
	deferred map[uuid.UUID]heldMailbox
	// progressed marks the mailboxes with a row finished this sweep.
	progressed    map[uuid.UUID]bool
	interruptedAt uuid.UUID
	interruptions int
	holds         int
	fullAt        time.Time
	now           func() time.Time
}

func (w *warmupInboxSweep) clock() time.Time {
	if w.now != nil {
		return w.now()
	}
	return time.Now()
}

func (w *warmupInboxSweep) wants(mailbox, id uuid.UUID) bool {
	if _, held := w.held[mailbox]; held {
		return false
	}
	if w.only == nil {
		return true
	}
	h, ok := w.only[mailbox]
	return ok && bytes.Compare(id[:], h.resume[:]) > 0
}

func (w *warmupInboxSweep) hold(mailbox uuid.UUID, err error) {
	if w.held == nil {
		w.held = map[uuid.UUID]heldMailbox{}
	}
	w.holds++
	h := heldMailbox{resume: w.afterID, err: err, seq: w.holds}
	if prev, ok := w.only[mailbox]; ok {
		h.retries = prev.retries
	}
	w.held[mailbox] = h
}

// finished records a row that ran to completion, which counts as progress for its mailbox.
func (w *warmupInboxSweep) finished(mailbox uuid.UUID) {
	if w.progressed == nil {
		w.progressed = map[uuid.UUID]bool{}
	}
	w.progressed[mailbox] = true
	w.interruptions = 0
}

// interrupted notes a pass cut short on id and reports whether that row has now done it too often in a row.
func (w *warmupInboxSweep) interrupted(id uuid.UUID) bool {
	if w.interruptedAt == id {
		w.interruptions++
	} else {
		w.interruptedAt, w.interruptions = id, 1
	}
	return w.interruptions >= warmupCleanupMaxInterruptions
}

// finish closes a sweep and returns when the next starts: a retry of the mailboxes due, else the next full sweep.
func (w *warmupInboxSweep) finish(now time.Time) time.Time {
	if w.only == nil {
		// A full sweep revisits every mailbox, so nothing held before it is still owed a check.
		w.fullAt, w.waiting, w.deferred = now.Add(warmupCleanupInterval), nil, nil
	}
	if w.waiting == nil {
		w.waiting = map[uuid.UUID]heldMailbox{}
	}
	if w.deferred == nil {
		w.deferred = map[uuid.UUID]heldMailbox{}
	}
	for m, h := range w.held {
		if w.progressed[m] {
			// It finished rows before failing again, so it carries on rather than backing off.
			h.retries, h.due = 0, now
		} else {
			h.due = now.Add(warmupCleanupRetryDelay << h.retries)
			h.retries++
		}
		if h.due.Before(w.fullAt) {
			w.waiting[m] = h
		} else {
			w.deferred[m] = h
		}
	}
	w.afterID, w.held, w.only, w.progressed = uuid.Nil, nil, nil, nil
	if len(w.waiting) == 0 {
		return w.fullAt
	}
	next := w.fullAt
	for _, h := range w.waiting {
		if h.due.Before(next) {
			next = h.due
		}
	}
	w.only = map[uuid.UUID]heldMailbox{}
	first := true
	for m, h := range w.waiting {
		if h.due.After(next) {
			continue
		}
		w.only[m] = h
		delete(w.waiting, m)
		if first || bytes.Compare(h.resume[:], w.afterID[:]) < 0 {
			w.afterID, first = h.resume, false
		}
	}
	return next
}

// heldError names the mailboxes still owed a check and the latest failure among them, nil when there are none.
func (w *warmupInboxSweep) heldError() error {
	ids := map[uuid.UUID]bool{}
	var latest heldMailbox
	for _, set := range []map[uuid.UUID]heldMailbox{w.held, w.only, w.waiting, w.deferred} {
		for id, h := range set {
			ids[id] = true
			if h.seq > latest.seq {
				latest = h
			}
		}
	}
	if len(ids) == 0 || latest.err == nil {
		return nil
	}
	return fmt.Errorf("warmup inbox cleanup holds %d mailbox(es) for retry: %w", len(ids), latest.err)
}

func (w *warmupInboxSweep) hasTime(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	deadline, ok := ctx.Deadline()
	return !ok || deadline.Sub(w.clock()) >= warmupCleanupCandidateReserve
}

// fileWarmupOutOfMailbox persists filing separately from best-effort engagement.
func (s *JobsService) fileWarmupOutOfMailbox(ctx context.Context, e *models.JobEventNewEmail) error {
	defer arrivaldiag.Begin(ctx, arrivaldiag.WarmupFile)()
	if s.Publisher == nil || s.EmailRepository == nil || e.Message == nil {
		return nil
	}
	if models.NormalizeFolder(e.Message.Folder, e.Message.Flags) == models.FolderTrash || e.Message.ProviderFolder == models.FolderTrash {
		return nil
	}
	account, xerr := s.EmailRepository.GetByID(ctx, e.Message.EmailID)
	if xerr != nil {
		if xerr.Code == errx.NotFound {
			return nil
		}
		return fmt.Errorf("%w: warmup mailbox lookup: %w", errWarmupVerification, xerr)
	}
	if account == nil {
		return nil
	}
	if s.WarmupRecoveryRepo != nil {
		if err := s.WarmupRecoveryRepo.RememberMessage(ctx, e.Message.EmailID, e.Message.MessageID, e.Message.InternalDate); err != nil {
			return fmt.Errorf("%w: remember warmup message: %w", errWarmupVerification, err)
		}
	}
	placement, folder := account.WarmupFiling()
	if placement == models.WarmupPlacementInbox {
		return nil
	}
	action := models.WarmupEmailAction{
		UserID:             e.UserID,
		EmailID:            e.Message.EmailID,
		GmailID:            e.Message.GmailID,
		UID:                e.Message.UID,
		MailboxUIDValidity: e.Message.Mailbox,
		MailboxFolder:      e.Message.FolderPath,
		RFCMessageID:       e.Message.MessageID,
		InternalID:         e.Message.ID.String(),
		Actions:            []string{models.WarmupActionFile},
		Placement:          placement,
		TargetFolder:       folder,
	}
	if s.WarmupRecoveryRepo != nil {
		id, err := s.WarmupRecoveryRepo.EnqueueFiling(ctx, action)
		if err != nil {
			return fmt.Errorf("%w: persist warmup filing: %w", errWarmupVerification, err)
		}
		action.FilingID = id.String()
	}
	workerID, routable := s.warmupActionWorker(ctx, account)
	if !routable {
		if s.WarmupRecoveryRepo != nil {
			return nil
		}
		return fmt.Errorf("warmup filing: mailbox has no assigned worker")
	}
	if s.WarmupRecoveryRepo == nil {
		s.markSelfMove(ctx, e.Message.EmailID, e.Message.MessageID)
	}
	// Durable filings use presence verification instead of assuming every attempt moves mail.
	err := s.Publisher.PublishWarmupAction(ctx, workerID, &action)
	if s.WarmupRecoveryRepo != nil {
		return nil // The persisted filing survives a failed publish.
	}
	if err != nil {
		return fmt.Errorf("%w: warmup file: %w", errWarmupVerification, err)
	}
	return nil
}

// StartPendingWarmupVerification drains arrivals held during verification outages.
func (s *JobsService) StartPendingWarmupVerification(ctx context.Context) {
	jobrun.Loop(ctx, "pending_warmup_verification", time.Minute, true, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		return s.retryPendingWarmupVerification(ctx)
	})
}

func (s *JobsService) retryPendingWarmupVerification(ctx context.Context) error {
	events, err := s.UniboxRepository.ClaimPendingWarmupVerification(ctx, 25)
	if err != nil {
		return err
	}
	var failures, waiting []error
	for _, e := range events {
		err := s.UniboxRepository.ProcessPendingWarmupVerification(ctx, e.Message.ID, func(current *models.JobEventNewEmail) error {
			return s.ingestNewEmail(ctx, current)
		})
		switch {
		case err == nil:
		case errors.Is(err, repository.ErrWarmupDeliveryPending):
			// The hold this loop exists for: the send's provider id is not recorded yet, so the lease brings it back.
			waiting = append(waiting, err)
		default:
			failures = append(failures, err)
		}
	}
	if len(failures) == 0 {
		return jobrun.Quiet(errors.Join(waiting...))
	}
	return errors.Join(failures...)
}

// cleanWarmupInboxBatch runs one pass and reports whether the sweep reached the end.
func (s *JobsService) cleanWarmupInboxBatch(ctx context.Context, sweep *warmupInboxSweep) (bool, error) {
	var failures []error
	checked := 0
	for range warmupCleanupPassPages {
		if !sweep.hasTime(ctx) {
			return false, warmupCleanupFailures(failures)
		}
		events, err := s.UniboxRepository.ListWarmupReviewCandidates(ctx, sweep.afterID, warmupCleanupPageSize)
		if err != nil {
			return false, errors.Join(warmupCleanupFailures(failures), err)
		}
		for _, e := range events {
			if !sweep.wants(e.Message.EmailID, e.Message.ID) {
				sweep.afterID = e.Message.ID
				continue
			}
			if checked == warmupCleanupPassChecks || !sweep.hasTime(ctx) {
				return false, warmupCleanupFailures(failures)
			}
			checked++
			err := s.cleanWarmupCandidate(ctx, e)
			if err == nil {
				sweep.finished(e.Message.EmailID)
				sweep.afterID = e.Message.ID
				continue
			}
			if ctx.Err() != nil && !sweep.interrupted(e.Message.ID) {
				// The pass ran out, not the mailbox: this row is the first one retried.
				return false, errors.Join(warmupCleanupFailures(failures), err)
			}
			// One mailbox's failing checks, or a row that keeps outrunning the pass, must not stall every mailbox after it.
			sweep.hold(e.Message.EmailID, err)
			sweep.afterID = e.Message.ID
			log.Warn().Err(err).Str("email_account_id", e.Message.EmailID.String()).
				Msg("warmup inbox cleanup held a mailbox for retry")
			failures = append(failures, err)
			if ctx.Err() != nil {
				return false, warmupCleanupFailures(failures)
			}
		}
		if len(events) < warmupCleanupPageSize {
			return true, warmupCleanupFailures(failures)
		}
	}
	return false, warmupCleanupFailures(failures)
}

func warmupCleanupFailures(failures []error) error {
	if len(failures) == 0 {
		return nil
	}
	return fmt.Errorf("warmup inbox cleanup held %d mailbox(es) for retry: %w", len(failures), failures[0])
}

func (s *JobsService) cleanWarmupCandidate(ctx context.Context, e models.JobEventNewEmail) error {
	// Historical cleanup requires an exact identifier, never a reused subject.
	candidate, message := e, *e.Message
	message.Subject = ""
	candidate.Message = &message
	warmup, err := s.isKnownWarmupEmail(ctx, &candidate)
	if err != nil {
		return err
	}
	if !warmup {
		// A reply in a warmup thread that got in before ancestry was
		// checked, or while the turn it answers was still unknown.
		if warmup, err = s.isWarmupThreadReply(ctx, &candidate); err != nil {
			return err
		}
	}
	if !warmup {
		return nil
	}
	// Deleting the Unibox row only takes it out of OUR inbox. The copy
	// in the customer's own mailbox is what they are looking at, and
	// nothing else ever goes back for it, so file it here too (#583).
	// The Unibox row is the retry record: it stays until the filing
	// action is on the bus, so a publish or lookup failure is
	// re-offered when the mailbox is retried instead of hiding the leak.
	if err := s.fileWarmupOutOfMailbox(ctx, &candidate); err != nil {
		return err
	}
	if err := s.UniboxRepository.Delete(ctx, e.UserID, e.Message.ID); err != nil {
		return err
	}
	if s.StreamingPublisher != nil {
		s.StreamingPublisher.PublishEmailDeleted(ctx, s.emailInboxEvent(ctx, e.UserID, e.Message))
	}
	return nil
}
