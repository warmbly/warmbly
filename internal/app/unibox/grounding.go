package unibox

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// GroundingLimitMax bounds how many messages any AI surface may pull for
// context, whatever it asks for.
const GroundingLimitMax = 20

// ThreadGrounding returns a conversation as message text for an AI prompt,
// oldest first. Every AI surface used to ground on the preview snippet, so a
// draft answered the first line of an email and ignored the rest of it.
func (s *uniboxService) ThreadGrounding(ctx context.Context, orgID uuid.UUID, threadID string, limit int) ([]models.MessageGrounding, *errx.Error) {
	out, err := s.uniboxRepository.GroundingByThread(ctx, orgID, threadID, clampGroundingLimit(limit))
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	return out, nil
}

// AddressGrounding returns recent correspondence with one address, newest
// first, for grounding a brand-new email to that person.
func (s *uniboxService) AddressGrounding(ctx context.Context, orgID uuid.UUID, address string, limit int) ([]models.MessageGrounding, *errx.Error) {
	out, err := s.uniboxRepository.GroundingByAddress(ctx, orgID, address, clampGroundingLimit(limit))
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	return out, nil
}

func clampGroundingLimit(limit int) int {
	if limit <= 0 {
		return GroundingLimitMax
	}
	return min(limit, GroundingLimitMax)
}

// RenderGrounding turns grounding rows into the prompt block every AI surface
// sends, oldest first, with quoted history stripped and the budget spent on the
// newest messages.
func RenderGrounding(msgs []models.MessageGrounding) string {
	blocks := make([]generation.GroundingMessage, 0, len(msgs))
	for _, m := range msgs {
		from := ""
		if len(m.FromAddr) > 0 {
			from = m.FromAddr[0]
		}
		blocks = append(blocks, generation.GroundingMessage{
			From:    from,
			Subject: m.Subject,
			Body:    m.BodyText,
			Preview: m.Snippet,
		})
	}
	return generation.RenderThread(blocks, generation.GroundingPerMessageChars, generation.GroundingTotalChars)
}
