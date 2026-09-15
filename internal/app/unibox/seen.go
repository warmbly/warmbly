package unibox

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

func (s *uniboxService) MarkSeen(ctx context.Context, userID, emailID uuid.UUID, seen bool) *errx.Error {
	if err := s.uniboxRepository.MarkSeen(ctx, userID, emailID, seen); err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}

	return nil
}

func (s *uniboxService) MarkSeenBulk(ctx context.Context, orgID uuid.UUID, data *models.MarkSeen) (*models.MarkSeen, *errx.Error) {
	if len(data.EmailIDs) > 500 {
		return nil, errx.ErrSeenMax
	}

	// A folder sweep and an id list are different requests; refuse the
	// ambiguous combination instead of guessing which one was meant.
	if data.Folder != "" {
		if len(data.EmailIDs) > 0 {
			return nil, errx.ErrSeenFolderAndIDs
		}
		if !models.ValidFolder(data.Folder) {
			return nil, errx.ErrUniboxFolder
		}
		changed, err := s.uniboxRepository.MarkSeenByFolder(ctx, orgID, data.Folder, data.Seen)
		if err != nil {
			errs.CaptureException(err)
			return nil, errx.InternalError()
		}
		s.relaySeen(ctx, orgID, changed)
		return data, nil
	}

	changed, err := s.uniboxRepository.MarkSeenBulk(ctx, orgID, data.EmailIDs, data.Seen)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	s.relaySeen(ctx, orgID, changed)

	return data, nil
}

// relaySeen carries a read/unread change out to the mailboxes themselves, so
// a thread read in Warmbly is read in Gmail too.
//
// Detached and best-effort. The store is the customer's view and has already
// been written, so this must not hold the response open behind a slow broker,
// and a worker that cannot be reached must not fail the request. Nothing
// retries: the provider's own state is what the next sync brings back anyway.
//
// What is relayed is the state the ROW now holds, read back inside the
// lookup, not the state the request asked for. Two people toggling the same
// conversation in opposite directions at the same moment can still race, but
// the loser then relays the winner's answer rather than its own.
func (s *uniboxService) relaySeen(ctx context.Context, orgID uuid.UUID, changed []uuid.UUID) {
	if s.publisher == nil || len(changed) == 0 {
		return
	}

	go func() {
		// Detached from the request, bounded so a wedged broker cannot leak a
		// goroutine per press.
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), seenRelayTimeout)
		defer cancel()
		s.publishSeenRelay(bg, orgID, changed)
	}()
}

// seenRelayTimeout bounds one relay. Generous, because "mark all as read" on
// a busy folder is many events, and it exists to end a wedged publish rather
// than to pace a healthy one.
const seenRelayTimeout = 2 * time.Minute

func (s *uniboxService) publishSeenRelay(ctx context.Context, orgID uuid.UUID, changed []uuid.UUID) {
	targets, err := s.uniboxRepository.SeenRelayTargets(ctx, orgID, changed)
	if err != nil {
		errs.CaptureException(err)
		return
	}

	// One event per mailbox and read state: "mark all as read" on a busy
	// folder is one press over thousands of messages, a mailbox is the unit a
	// worker holds, and a batch carries one state for all of it.
	type relayKey struct {
		emailID uuid.UUID
		seen    bool
	}
	batches := make(map[relayKey]*models.MessageSeenAction)
	workers := make(map[uuid.UUID]uuid.UUID, len(targets))
	order := make([]relayKey, 0, len(targets))
	for _, t := range targets {
		key := relayKey{emailID: t.EmailID, seen: t.Seen}
		act, ok := batches[key]
		if !ok {
			act = &models.MessageSeenAction{EmailID: t.EmailID, Seen: t.Seen}
			batches[key] = act
			workers[t.EmailID] = t.WorkerID
			order = append(order, key)
		}
		act.Messages = append(act.Messages, t.Ref)
	}

	for _, key := range order {
		act := batches[key]
		for start := 0; start < len(act.Messages); start += models.SeenRelayChunk {
			end := start + models.SeenRelayChunk
			if end > len(act.Messages) {
				end = len(act.Messages)
			}
			batch := &models.MessageSeenAction{
				EmailID:  key.emailID,
				Seen:     key.seen,
				Messages: act.Messages[start:end],
			}
			if err := s.publisher.PublishMessageSeen(ctx, workers[key.emailID], batch); err != nil {
				log.Warn().Err(err).
					Str("email_account_id", key.emailID.String()).
					Int("messages", len(batch.Messages)).
					Msg("could not relay the unibox read state to the mailbox provider")
			}
		}
	}
}

// MoveFolderBulk backs Archive, Delete and Move to inbox in the thread header.
// Store-side only: the provider copy stays where it is, and provider_folder is
// left alone so the sync can still tell a real provider move from a flag scan.
func (s *uniboxService) MoveFolderBulk(ctx context.Context, orgID uuid.UUID, data *models.MoveFolder) (*models.MoveFolder, *errx.Error) {
	if len(data.EmailIDs) > 500 {
		return nil, errx.ErrSeenMax
	}
	// Only the three a user can file into. sent/drafts/spam are verdicts the
	// provider reaches, and accepting them here would let a caller forge one.
	if !models.FilableFolder(data.Folder) {
		return nil, errx.ErrUniboxFilableFolder
	}
	if err := s.uniboxRepository.MoveToFolderBulk(ctx, orgID, data.EmailIDs, data.Folder); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	return data, nil
}
