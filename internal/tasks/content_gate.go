package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/pkg/warmlint"
	"github.com/warmbly/warmbly/internal/repository"
)

// contentWarningWindow is how long one warning covers a step, so a campaign
// with 500 recipients writes one feed entry rather than 500.
const contentWarningWindow = 24 * time.Hour

// warnOnWeakContent scores the RENDERED copy for this send and, when it is
// below the org's floor, writes one warning to the campaign feed.
//
// The editor check scores the template; this scores what the recipient will
// actually receive, after merge fields, spintax and AI blocks have resolved.
// Those are exactly where a clean template turns into "Hi ," or a wall of
// trigger words. Advisory only: it never blocks or delays the send.
func (s *tasksService) warnOnWeakContent(ctx context.Context, orgID, campaignID, sequenceID uuid.UUID, step int, subject, bodyHTML, bodyPlain string, attachments int) {
	if s.advanced == nil || s.campaignLogRepo == nil {
		return
	}
	settings, xerr := s.advanced.GetOrganizationSettings(ctx, orgID)
	if xerr != nil || settings == nil || !settings.Preflight.Enabled || !settings.Preflight.CheckContentScore {
		return
	}
	floor := settings.Preflight.MinContentScore
	if floor <= 0 {
		floor = 60
	}

	res := warmlint.ScoreWithAttachments(subject, bodyHTML, bodyPlain, attachments)
	if res.Score >= floor {
		return
	}

	seq := sequenceID.String()
	if seen, err := s.campaignLogRepo.HasRecentLogFor(ctx, campaignID, "content_warning", "sequence_id", seq,
		time.Now().Add(-contentWarningWindow)); err != nil || seen {
		return
	}

	detail := ""
	for _, issue := range res.Issues {
		if issue.Severity == "high" {
			detail = " " + issue.Message
			break
		}
	}
	if detail == "" && len(res.Issues) > 0 {
		detail = " " + res.Issues[0].Message
	}

	codes := make([]string, 0, len(res.Issues))
	for _, issue := range res.Issues {
		codes = append(codes, issue.Code)
	}

	if err := s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
		CampaignID: campaignID,
		EventType:  "content_warning",
		Message: fmt.Sprintf("Step %d's copy scores %d/100 for spam signals as sent (floor %d).%s",
			step, res.Score, floor, detail),
		Metadata: map[string]interface{}{
			"level":       "warning",
			"sequence_id": seq,
			"score":       res.Score,
			"floor":       floor,
			"issues":      codes,
		},
	}); err != nil {
		log.Warn().Err(err).Str("campaign_id", campaignID.String()).Msg("could not record the content warning")
	}
}
