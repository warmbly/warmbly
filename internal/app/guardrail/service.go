package guardrail

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/notification"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Service evaluates every active campaign with guardrails switched on and
// pauses the ones that have left their band.
type Service interface {
	// Sweep evaluates all guardrail-enabled campaigns once and returns how many
	// it paused. Safe to run concurrently with itself: the pause is a
	// conditional UPDATE on status = 'active'.
	Sweep(ctx context.Context) (int, error)
	// Clear wipes a campaign's tripped marker. Called when it is started again.
	Clear(ctx context.Context, campaignID uuid.UUID) error
}

type service struct {
	repo     repository.GuardrailRepository
	logRepo  repository.CampaignLogRepository
	audit    audit.AuditService
	notifier notification.Service
}

func NewService(
	repo repository.GuardrailRepository,
	logRepo repository.CampaignLogRepository,
	auditSvc audit.AuditService,
	notifier notification.Service,
) Service {
	return &service{repo: repo, logRepo: logRepo, audit: auditSvc, notifier: notifier}
}

// sweepLimit bounds one pass. Guardrails are opt-in per campaign, so this is
// far above any realistic count; it exists so a runaway install cannot turn one
// tick into an unbounded scan.
const sweepLimit = 1000

func (s *service) Sweep(ctx context.Context) (int, error) {
	campaigns, err := s.repo.ListGuardrailCampaigns(ctx, sweepLimit)
	if err != nil {
		return 0, err
	}

	paused := 0
	for _, c := range campaigns {
		breach := Evaluate(c)
		if breach == nil {
			continue
		}

		tripped, terr := s.repo.TripGuardrail(ctx, c.ID, breach.Reason)
		if terr != nil {
			log.Warn().Err(terr).Str("campaign_id", c.ID.String()).Msg("guardrail: pause failed")
			continue
		}
		if !tripped {
			// Someone paused or completed the campaign between the read and
			// the write. Nothing to announce.
			continue
		}

		paused++
		s.announce(ctx, c, breach)
	}
	return paused, nil
}

func (s *service) Clear(ctx context.Context, campaignID uuid.UUID) error {
	return s.repo.ClearGuardrail(ctx, campaignID)
}

// announce records the pause everywhere a person might look for it: the
// campaign's own activity log, the org audit trail (which also drives the
// realtime spine, so every teammate's campaign list refreshes), and an in-app
// notification for the members who can act on it.
//
// All three are best-effort. A campaign that is paused but whose notification
// failed is still safely paused, which is the part that matters.
func (s *service) announce(ctx context.Context, c repository.GuardrailCampaign, b *Breach) {
	log.Info().
		Str("campaign_id", c.ID.String()).
		Str("rule", string(b.Rule)).
		Float64("observed", b.Observed).
		Float64("threshold", b.Threshold).
		Int("sample", b.Sample).
		Msg("guardrail: campaign paused")

	if s.logRepo != nil {
		_ = s.logRepo.CreateLog(ctx, &repository.CampaignLogEntry{
			CampaignID: c.ID,
			EventType:  "guardrail_paused",
			Message:    b.Reason,
			Metadata: map[string]interface{}{
				"rule":      string(b.Rule),
				"observed":  b.Observed,
				"threshold": b.Threshold,
				"sample":    b.Sample,
				"window":    c.WindowDays,
			},
		})
	}

	if c.OrganizationID == nil {
		return
	}
	orgID := *c.OrganizationID
	campaignID := c.ID

	if s.audit != nil {
		// A zero actor marks this as the platform acting rather than a member.
		s.audit.LogAction(ctx, orgID, uuid.Nil,
			models.AuditActionPause, models.AuditEntityCampaign, &campaignID, "", "",
			map[string]string{"status": "active -> paused_guardrail"},
			map[string]string{
				"reason":    string(b.Rule),
				"observed":  fmt.Sprintf("%.2f", b.Observed),
				"threshold": fmt.Sprintf("%.2f", b.Threshold),
				"sample":    fmt.Sprintf("%d", b.Sample),
			})
	}

	if s.notifier != nil {
		s.notifier.NotifyOrg(ctx, orgID, models.PermViewCampaigns, uuid.Nil,
			models.NotifCampaignPaused,
			fmt.Sprintf("%s was paused automatically", c.Name),
			b.Reason,
			"/app/campaigns/"+campaignID.String(),
			map[string]any{
				"campaign_id": campaignID.String(),
				"rule":        string(b.Rule),
				"observed":    b.Observed,
				"threshold":   b.Threshold,
				"sample":      b.Sample,
			},
			"guardrail:"+campaignID.String(),
		)
	}
}
