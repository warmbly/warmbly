package leadreview

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	writeAheadTick = 30 * time.Second
	// writeAheadPasses bounds one campaign's passes per tick; campaigns take turns, one pass each.
	writeAheadPasses   = 5
	writeAheadCampaign = 100
)

// StartAIWriteAhead writes the first email's AI copy for every lead of an active
// campaign before it is due, so the Review tab shows it and the send reuses it.
// It runs on one backend (leader loop) and keeps each campaign's cursor in memory.
func (s *Service) StartAIWriteAhead(ctx context.Context) {
	cursors := map[uuid.UUID]*models.CampaignReviewCursor{}
	t := time.NewTicker(writeAheadTick)
	defer t.Stop()
	for {
		s.writeAheadTick(ctx, cursors)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) writeAheadTick(ctx context.Context, cursors map[uuid.UUID]*models.CampaignReviewCursor) {
	if !s.Emails.AIAvailable() {
		return
	}
	var active []repository.CampaignOrg
	var after uuid.UUID
	for ctx.Err() == nil {
		page, err := s.Repo.ActiveAICampaigns(ctx, after, writeAheadCampaign)
		if err != nil {
			log.Warn().Err(err).Msg("AI write-ahead: list campaigns")
			return
		}
		active = append(active, page...)
		if len(page) < writeAheadCampaign {
			break
		}
		after = page[len(page)-1].CampaignID
	}
	seen := map[uuid.UUID]bool{}
	for _, c := range active {
		seen[c.CampaignID] = true
	}
	for id := range cursors {
		if !seen[id] {
			delete(cursors, id)
		}
	}

	// Round-robin, so a slow campaign delays each other one by a single pass.
	stopped := map[uuid.UUID]bool{}
	for pass := 0; pass < writeAheadPasses && len(active) > 0; pass++ {
		next := active[:0]
		for _, c := range active {
			if ctx.Err() != nil {
				return
			}
			if stopped[c.OrgID] {
				continue
			}
			more, ok := s.writeAheadPass(ctx, c.OrgID, c.CampaignID, cursors)
			if !ok {
				stopped[c.OrgID] = true
			}
			if more && ok {
				next = append(next, c)
			}
		}
		active = next
	}
}

// writeAheadPass runs one pass and reports whether the campaign has more to write
// this tick, and false for ok when the organization's balance or cap stopped it.
func (s *Service) writeAheadPass(ctx context.Context, orgID, campaignID uuid.UUID, cursors map[uuid.UUID]*models.CampaignReviewCursor) (more, ok bool) {
	res, xerr := s.WriteAIBatch(ctx, orgID, campaignID, nil, nil, false, cursors[campaignID])
	if xerr != nil {
		log.Warn().Str("campaign_id", campaignID.String()).Str("code", xerr.ResponseCode()).Msg("AI write-ahead: pass failed")
		return false, true
	}
	if res.Stopped != "" {
		return false, false
	}
	// The end of the list wraps to the start, so a lead that failed is tried again next tick.
	if res.Remaining == 0 || res.Next == "" {
		delete(cursors, campaignID)
		return false, true
	}
	next, err := DecodeCursor(res.Next)
	if err != nil {
		delete(cursors, campaignID)
		return false, true
	}
	cursors[campaignID] = next
	return true, true
}
