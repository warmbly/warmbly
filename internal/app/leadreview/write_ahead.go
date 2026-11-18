package leadreview

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	writeAheadTick = 30 * time.Second
	// writeAheadPasses bounds one campaign's passes per tick, so one big list cannot starve the rest.
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
	seen := map[uuid.UUID]bool{}
	stopped := map[uuid.UUID]bool{}
	var after uuid.UUID
	for ctx.Err() == nil {
		page, err := s.Repo.ActiveAICampaigns(ctx, after, writeAheadCampaign)
		if err != nil {
			log.Warn().Err(err).Msg("AI write-ahead: list campaigns")
			return
		}
		for _, c := range page {
			seen[c.CampaignID] = true
			if !stopped[c.OrgID] && !s.writeAheadCampaign(ctx, c.OrgID, c.CampaignID, cursors) {
				stopped[c.OrgID] = true
			}
		}
		if len(page) < writeAheadCampaign {
			break
		}
		after = page[len(page)-1].CampaignID
	}
	for id := range cursors {
		if !seen[id] {
			delete(cursors, id)
		}
	}
}

// writeAheadCampaign runs a few passes and reports false when the organization's balance or cap stopped it.
func (s *Service) writeAheadCampaign(ctx context.Context, orgID, campaignID uuid.UUID, cursors map[uuid.UUID]*models.CampaignReviewCursor) bool {
	for i := 0; i < writeAheadPasses && ctx.Err() == nil; i++ {
		res, xerr := s.WriteAIBatch(ctx, orgID, campaignID, nil, nil, false, cursors[campaignID])
		if xerr != nil {
			log.Warn().Str("campaign_id", campaignID.String()).Str("code", xerr.ResponseCode()).Msg("AI write-ahead: pass failed")
			return true
		}
		if res.Stopped != "" {
			return false
		}
		// The end of the list wraps to the start, so a lead that failed is tried again next tick.
		if res.Remaining == 0 || res.Next == "" {
			delete(cursors, campaignID)
			return true
		}
		next, err := DecodeCursor(res.Next)
		if err != nil {
			delete(cursors, campaignID)
			return true
		}
		cursors[campaignID] = next
	}
	return true
}
