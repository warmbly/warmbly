package unibox

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
)

// Backfill pacing. A pass is one query plus one object-storage read per row, so
// the batch is small and the interval generous: this is catching up on history,
// and nothing waits on it.
const (
	bodyBackfillBatch    = 100
	bodyBackfillInterval = 30 * time.Second
)

// StartBodyTextBackfill fills in the searchable text of messages synced before
// bodies were indexed, so unibox search covers the whole archive rather than
// only mail that arrives from now on.
//
// The sweep walks the table once per process: it pages by id, and stops for
// good when a pass turns up no rows left to visit. Rows whose stored body
// really is empty stay at body_text = ” and are simply re-visited after the
// next restart, which is cheaper than inventing a "tried and failed" marker.
func (s *uniboxService) StartBodyTextBackfill(ctx context.Context) {
	ticker := time.NewTicker(bodyBackfillInterval)
	defer ticker.Stop()

	var cursor uuid.UUID
	var filled int
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
			if filled > 0 {
				log.Info().Int("messages", filled).Msg("unibox body backfill: complete")
			}
			return
		}

		for _, t := range targets {
			cursor = t.ID
			body, gerr := s.GetBody(ctx, t.UserID, t.ID)
			if gerr != nil {
				// A missing blob is expected here: fixtures and mail synced
				// before body storage existed have no object to read.
				continue
			}
			text := mailhtml.SearchText(string(body.PlainText), string(body.HTMLBody), config.MaxSearchBodyText)
			if text == "" {
				continue
			}
			if serr := s.uniboxRepository.SetBodyText(ctx, t.ID, text); serr != nil {
				log.Warn().Err(serr).Str("message_id", t.ID.String()).Msg("unibox body backfill: write failed")
				continue
			}
			filled++
		}
	}
}
