package unibox

import (
	"context"

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
		s.relaySeen(ctx, orgID, changed, data.Seen)
		return data, nil
	}

	changed, err := s.uniboxRepository.MarkSeenBulk(ctx, orgID, data.EmailIDs, data.Seen)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	s.relaySeen(ctx, orgID, changed, data.Seen)

	return data, nil
}

// relaySeen carries a read/unread change out to the mailboxes themselves, so
// a thread read in Warmbly is read in Gmail too.
//
// Best-effort and after the fact: the store is the customer's view and has
// already been written, so a worker that cannot be reached must not fail the
// request. Nothing here retries either, because the provider's own state is
// what the next sync brings back regardless.
func (s *uniboxService) relaySeen(ctx context.Context, orgID uuid.UUID, changed []uuid.UUID, seen bool) {
	if s.publisher == nil || len(changed) == 0 {
		return
	}

	targets, err := s.uniboxRepository.SeenRelayTargets(ctx, orgID, changed)
	if err != nil {
		errs.CaptureException(err)
		return
	}

	// One event per mailbox, chunked: "mark all as read" on a busy folder is
	// one press over thousands of messages, and a mailbox is the unit a
	// worker holds.
	byMailbox := make(map[uuid.UUID]*models.MessageSeenAction)
	order := make([]uuid.UUID, 0, len(targets))
	workers := make(map[uuid.UUID]uuid.UUID, len(targets))
	for _, t := range targets {
		act, ok := byMailbox[t.EmailID]
		if !ok {
			act = &models.MessageSeenAction{EmailID: t.EmailID, Seen: seen}
			byMailbox[t.EmailID] = act
			workers[t.EmailID] = t.WorkerID
			order = append(order, t.EmailID)
		}
		act.Messages = append(act.Messages, t.Ref)
	}

	for _, emailID := range order {
		act := byMailbox[emailID]
		for start := 0; start < len(act.Messages); start += models.SeenRelayChunk {
			end := start + models.SeenRelayChunk
			if end > len(act.Messages) {
				end = len(act.Messages)
			}
			batch := &models.MessageSeenAction{
				EmailID:  emailID,
				Seen:     seen,
				Messages: act.Messages[start:end],
			}
			if err := s.publisher.PublishMessageSeen(ctx, workers[emailID], batch); err != nil {
				log.Warn().Err(err).
					Str("email_account_id", emailID.String()).
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
