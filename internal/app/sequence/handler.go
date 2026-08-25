package sequence

import (
	"context"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/warmlint"
)

func (s *sequenceService) Create(ctx context.Context, userID, campaignID string) (*models.Sequence, *errx.Error) {
	sequence, xerr := s.sequenceRepository.Create(ctx, userID, campaignID)
	return scoreSequence(sequence), xerr
}

func (s *sequenceService) Get(ctx context.Context, userID, campaignID string) ([]models.Sequence, *errx.Error) {
	sequences, xerr := s.sequenceRepository.Get(ctx, userID, campaignID)
	for i := range sequences {
		scoreSequence(&sequences[i])
	}
	return sequences, xerr
}

func (s *sequenceService) Update(ctx context.Context, userID, campaignID, sequenceID string, data *models.UpdateSequence) (*models.Sequence, *errx.Error) {
	// Branch routing is resolved (and made safe against deleted/dangling targets
	// and loops) at schedule time in the repository's finder; the repository also
	// validates branch shape before persisting. No cross-step write validation is
	// needed here — the canvas only ever points a branch at a real step or stop.
	sequence, xerr := s.sequenceRepository.Update(ctx, userID, campaignID, sequenceID, data)
	return scoreSequence(sequence), xerr
}

func scoreSequence(sequence *models.Sequence) *models.Sequence {
	if sequence == nil || sequence.Kind != "email" {
		return sequence
	}
	score := warmlint.Score(sequence.Subject, sequence.BodyHTML, sequence.BodyPlain)
	issues := make([]models.ContentSafetyIssue, len(score.Issues))
	for i := range score.Issues {
		issues[i] = models.ContentSafetyIssue(score.Issues[i])
	}
	sequence.ContentScore = &models.ContentSafetyScore{Score: score.Score, Issues: issues, Hard: score.Hard}
	return sequence
}

// UpdateLayout persists only step canvas coordinates (drag-to-stick). Cosmetic
// and high-churn, so it stays out of the audited content-update path.
func (s *sequenceService) UpdateLayout(ctx context.Context, userID, campaignID string, positions []models.SequencePosition) *errx.Error {
	return s.sequenceRepository.UpdateLayout(ctx, userID, campaignID, positions)
}

func (s *sequenceService) Delete(ctx context.Context, userID, campaignID, sequenceID string) *errx.Error {
	return s.sequenceRepository.Delete(ctx, userID, campaignID, sequenceID)
}
