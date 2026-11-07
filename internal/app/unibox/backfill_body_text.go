package unibox

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
	"github.com/warmbly/warmbly/internal/repository"
)

// Backfill pacing. A pass is one query plus one object-storage read per row, so
// the batch is small and the interval generous: this is catching up on history,
// and nothing waits on it.
const (
	bodyBackfillBatch    = 100
	bodyBackfillInterval = 30 * time.Second
	// bodyBackfillRetries is how many ticks one message is retried before the sweep moves past it.
	bodyBackfillRetries = 3
)

// StartBodyTextBackfill fills in the searchable text of messages synced before
// bodies were indexed, so unibox search covers the whole archive rather than
// only mail that arrives from now on.
//
// The sweep walks the table once per instance: it pages by id, stores its
// place after every page, and records itself done when a page comes back
// empty, so a restart resumes rather than re-reading rows whose stored body
// really is empty. New mail is written with its text, so finishing leaves
// nothing behind.
func (s *uniboxService) StartBodyTextBackfill(ctx context.Context) {
	progress, err := s.uniboxRepository.BodyBackfillProgress(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("unibox body backfill: progress unreadable, starting over")
	}
	if progress.Done {
		return
	}
	ticker := time.NewTicker(bodyBackfillInterval)
	defer ticker.Stop()

	cursor := progress.After
	var filled int
	var stuck uuid.UUID
	var tries int
	// retry reports whether a failed message is tried again next tick, so the saved place never passes it.
	retry := func(id uuid.UUID, err error) bool {
		if id != stuck {
			stuck, tries = id, 0
		}
		tries++
		log.Warn().Err(err).Str("message_id", id.String()).Int("attempt", tries).Msg("unibox body backfill: message failed")
		return tries < bodyBackfillRetries
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		targets, err := s.uniboxRepository.ListMissingBodyText(ctx, cursor, bodyBackfillBatch)
		if err != nil {
			log.Warn().Err(err).Msg("unibox body backfill: list failed")
			continue
		}
		if len(targets) == 0 {
			if serr := s.uniboxRepository.SaveBodyBackfillProgress(ctx, repository.UniboxBodyBackfill{After: cursor, Done: true}); serr != nil {
				log.Warn().Err(serr).Msg("unibox body backfill: could not record completion")
			}
			if filled > 0 {
				log.Info().Int("messages", filled).Msg("unibox body backfill: complete")
			}
			return
		}

		for _, t := range targets {
			body, gerr := s.GetBody(ctx, t.UserID, t.EmailID, t.ID)
			// A missing blob is expected here: fixtures and mail synced before body storage existed have no object to read.
			if gerr != nil && !errors.Is(gerr, storage.ErrNotFound) && retry(t.ID, gerr) {
				break
			}
			if gerr == nil {
				text := mailhtml.SearchText(string(body.PlainText), string(body.HTMLBody), config.MaxSearchBodyText)
				if text != "" {
					if serr := s.uniboxRepository.SetBodyText(ctx, t.ID, text); serr != nil {
						if retry(t.ID, serr) {
							break
						}
					} else {
						filled++
					}
				}
			}
			cursor = t.ID
		}
		if serr := s.uniboxRepository.SaveBodyBackfillProgress(ctx, repository.UniboxBodyBackfill{After: cursor}); serr != nil {
			log.Warn().Err(serr).Msg("unibox body backfill: could not record progress")
		}
	}
}
