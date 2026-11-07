package analytics

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// statusReads are the per-mailbox signals a status needs beyond the mailbox
// row, read for a whole page in a fixed number of queries. Every read is
// best-effort, as it was per mailbox: a failed one leaves its map nil, which
// each consumer treats as no signal.
type statusReads struct {
	now                  time.Time
	diversity            map[uuid.UUID]repository.WarmupPartnerDiversity
	cloud                map[uuid.UUID]*models.WarmupHealthInfo
	ramp                 *rampReads
	failures             map[uuid.UUID]*models.WarmupSendFailure
	nextWarmup           map[uuid.UUID]time.Time
	coldRamp             map[uuid.UUID]repository.ColdRampState
	lifecycles           map[uuid.UUID]models.SendLifecycleState
	sendHolds            map[uuid.UUID]repository.SendRecoveryHold
	sendHoldsUnavailable bool
	sendPauses           map[uuid.UUID]models.SendPauseInfo
}

// loadStatusReads reads the signals for emails, asking each query only for the
// mailboxes that would have issued it on their own.
func (s *analyticsService) loadStatusReads(ctx context.Context, emails []models.Email, participants map[uuid.UUID]*models.WarmupParticipantHealth) *statusReads {
	reads := &statusReads{now: time.Now()}
	ids := make([]uuid.UUID, len(emails))
	for i := range emails {
		ids[i] = emails[i].ID
	}
	if reader, ok := s.emailRepo.(interface {
		GetSendRecoveryHolds(context.Context, []uuid.UUID) (map[uuid.UUID]repository.SendRecoveryHold, error)
	}); ok {
		if holds, err := reader.GetSendRecoveryHolds(ctx, ids); err == nil {
			reads.sendHolds = holds
		} else {
			reads.sendHoldsUnavailable = true
		}
	}
	if reader, ok := s.emailRepo.(interface {
		GetSendPauses(context.Context, []uuid.UUID) (map[uuid.UUID]models.SendPauseInfo, error)
	}); ok {
		if pauses, err := reader.GetSendPauses(ctx, ids); err == nil {
			reads.sendPauses = pauses
		}
	}
	if s.lifecycleRepo != nil {
		if m, err := s.lifecycleRepo.GetSendLifecycles(ctx, ids); err == nil {
			reads.lifecycles = m
		}
	}
	if s.warmupRepo == nil {
		return reads
	}

	var pooled, unpooled, warming, failing, coldRamp []uuid.UUID
	for i := range emails {
		e := &emails[i]
		if participants[e.ID] != nil {
			pooled = append(pooled, e.ID)
		} else {
			unpooled = append(unpooled, e.ID)
		}
		if e.Warmup == nil {
			continue
		}
		if e.IsWarmingActive() {
			warming = append(warming, e.ID)
			if e.TestSendingAllowed() {
				failing = append(failing, e.ID)
			}
		}
		if e.CampaignLimit > 0 {
			coldRamp = append(coldRamp, e.ID)
		}
	}

	if len(pooled) > 0 {
		if m, err := s.warmupRepo.GetPartnerDiversityForAccounts(ctx, pooled, reads.now.Add(-warmupDiversityWindow)); err == nil {
			reads.diversity = m
		}
	}
	if len(unpooled) > 0 {
		if m, err := s.warmupRepo.GetCloudStandings(ctx, unpooled); err == nil {
			reads.cloud = m
		}
	}
	reads.ramp = s.loadRampReads(ctx, warming, reads.now)
	if reader, ok := s.warmupRepo.(interface {
		NextWarmupSends(context.Context, []uuid.UUID) (map[uuid.UUID]time.Time, error)
	}); ok && len(warming) > 0 {
		if m, err := reader.NextWarmupSends(ctx, warming); err == nil {
			reads.nextWarmup = m
		}
	}
	if len(failing) > 0 {
		if m, err := s.warmupRepo.LastWarmupSendFailures(ctx, failing, reads.now.Add(-24*time.Hour)); err == nil {
			reads.failures = m
		}
	}
	if len(coldRamp) > 0 {
		if m, err := s.warmupRepo.ColdRampStateForAccounts(ctx, coldRamp, reads.now.Add(-warmupramp.LookbackWindow)); err == nil {
			reads.coldRamp = m
		}
	}
	return reads
}

// rampReads answers warmupramp.Resolve from a page's prefetch, and asks the
// repository for anything the prefetch did not cover.
type rampReads struct {
	fallback        warmupramp.Reader
	placementsSince time.Time
	placements      map[uuid.UUID][]time.Time
	sentSince       time.Time
	sent            map[uuid.UUID]int
}

func (s *analyticsService) loadRampReads(ctx context.Context, warming []uuid.UUID, now time.Time) *rampReads {
	r := &rampReads{
		fallback:        s.warmupRepo,
		placementsSince: now.Add(-warmupramp.LookbackWindow),
		sentSince:       now.Add(-warmupramp.SoftSignalWindow),
	}
	if len(warming) == 0 {
		return r
	}
	placements, err := s.warmupRepo.SpamPlacementsSinceForAccounts(ctx, warming, r.placementsSince)
	if err != nil {
		return r
	}
	r.placements = placements
	// Resolve reads sends only for a mailbox with a placement inside the soft window.
	var recent []uuid.UUID
	for id, at := range placements {
		for _, p := range at {
			if !p.Before(r.sentSince) {
				recent = append(recent, id)
				break
			}
		}
	}
	if len(recent) > 0 {
		if sent, err := s.warmupRepo.SumWarmupSentSinceForAccounts(ctx, recent, r.sentSince); err == nil {
			r.sent = sent
		}
	}
	return r
}

func (r *rampReads) SpamPlacementsSince(ctx context.Context, accountID uuid.UUID, since time.Time) ([]time.Time, error) {
	if at, ok := r.placements[accountID]; ok && since.Equal(r.placementsSince) {
		return at, nil
	}
	return r.fallback.SpamPlacementsSince(ctx, accountID, since)
}

func (r *rampReads) SumWarmupSentSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error) {
	if n, ok := r.sent[accountID]; ok && since.Equal(r.sentSince) {
		return n, nil
	}
	return r.fallback.SumWarmupSentSince(ctx, accountID, since)
}
