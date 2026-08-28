package scheduler

import (
	"context"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/behavior"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// SchedulerService provides task scheduling functionality
type SchedulerService interface {
	// Warmup scheduling
	CalculateNextWarmupTime(ctx context.Context, accountID uuid.UUID) (time.Time, error)

	// Campaign scheduling
	CalculateNextCampaignTime(ctx context.Context, campaignID uuid.UUID) (time.Time, *repository.ContactSequencePair, uuid.UUID, error)

	// Email scheduling (smart send)
	CalculateNextEmailTime(ctx context.Context, accountID uuid.UUID) (time.Time, error)
}

type schedulerService struct {
	taskRepo             repository.TaskRepository
	warmupRepo           repository.WarmupRepository
	campaignProgressRepo repository.CampaignProgressRepository
	emailRepo            repository.EmailRepository
	campaignRepo         repository.CampaignRepository
	// contactRepo is used only to read/cache the recipient ESP/provider for
	// ESP matching (no MX dial on the hot path). nil-safe.
	contactRepo repository.ContactRepository
	// campaignLogRepo records send-path decision logs (e.g. ESP defer,
	// new-lead cap). Optional/nil-safe so the scheduler keeps working without it.
	campaignLogRepo repository.CampaignLogRepository
	// behaviorSvc supplies each mailbox's rolled workday (hours, lunch, daily
	// and hourly ceilings, send spacing). Optional/nil-safe: without it every
	// mailbox keeps the legacy fixed cap and min-gap path.
	behaviorSvc behavior.Service
	// domainAuth resolves the operator's sending-domain authentication gate.
	// Optional/nil-safe: without it the persisted auth state stays
	// observe-only and never blocks a send.
	domainAuth DomainAuthPolicy
	// outreachRepo reads the org's send-time-optimization settings. Optional/
	// nil-safe: without it sends stay on the sending mailbox's clock.
	outreachRepo repository.AdvancedOutreachRepository
	// orgRiskRepo reads the organization's fused abuse posture. Optional/
	// nil-safe: without it no organization is ever risk-capped.
	orgRiskRepo repository.OrgRiskRepository
}

// DomainAuthPolicy resolves whether the sending-domain authentication gate is
// enforced, and how long a domain must stay failing before it applies. Narrow
// and primitive-typed so the scheduler does not depend on the settings package;
// instancesettings.Service satisfies it.
type DomainAuthPolicy interface {
	DomainAuth(ctx context.Context) (enforce bool, grace time.Duration)
}

// domainAuthGate resolves the gate for one scheduling pass. A nil policy, or an
// operator who turned enforcement off, means no mailbox is ever gated.
func (s *schedulerService) domainAuthGate(ctx context.Context) (bool, time.Duration) {
	if s.domainAuth == nil {
		return false, 0
	}
	return s.domainAuth.DomainAuth(ctx)
}

// WireBehavior attaches the sending-behaviour engine. Kept off the constructor
// so the scheduler stays constructible in tests and in any deployment that has
// not enabled the feature.
func (s *schedulerService) WireBehavior(b behavior.Service) {
	s.behaviorSvc = b
}

// BehaviorAware is the optional capability the caller uses to attach the
// behaviour engine after construction.
type BehaviorAware interface {
	WireBehavior(b behavior.Service)
}

// WireDomainAuth attaches the sending-domain authentication policy. Kept off
// the constructor for the same reason as WireBehavior: the scheduler must stay
// constructible in tests and in deployments that leave the gate off.
func (s *schedulerService) WireDomainAuth(p DomainAuthPolicy) {
	s.domainAuth = p
}

// DomainAuthAware is the optional capability the caller uses to attach the
// authentication gate after construction.
type DomainAuthAware interface {
	WireDomainAuth(p DomainAuthPolicy)
}

// WireOutreach attaches the advanced-outreach settings repository, which
// carries the org's recipient-timezone send-time policy. Kept off the
// constructor for the same reason as WireBehavior.
func (s *schedulerService) WireOutreach(r repository.AdvancedOutreachRepository) {
	s.outreachRepo = r
}

// OutreachAware is the optional capability the caller uses to attach the
// outreach settings after construction.
type OutreachAware interface {
	WireOutreach(r repository.AdvancedOutreachRepository)
}

// WireOrgRisk attaches the organization risk posture.
func (s *schedulerService) WireOrgRisk(r repository.OrgRiskRepository) {
	s.orgRiskRepo = r
}

// OrgRiskAware is the optional capability the caller uses to attach org risk
// after construction.
type OrgRiskAware interface {
	WireOrgRisk(r repository.OrgRiskRepository)
}

// orgRiskState is the organization's posture for one pass. Fails open to
// trusted: a lookup error must never restrict a customer on a verdict nothing
// established.
func (s *schedulerService) orgRiskState(ctx context.Context, orgID *uuid.UUID) models.OrgRiskState {
	if s.orgRiskRepo == nil || orgID == nil {
		return models.OrgRiskTrusted
	}
	states, err := s.orgRiskRepo.GetOrgRiskStates(ctx, []uuid.UUID{*orgID})
	if err != nil {
		return models.OrgRiskTrusted
	}
	if state, ok := states[*orgID]; ok && state.Valid() {
		return state
	}
	return models.OrgRiskTrusted
}

// sendTimePreference resolves the org's recipient-timezone policy for one
// scheduling pass. No repository, no organization, or an unreadable settings
// row all mean "not enabled", which leaves the slot on the mailbox's clock.
func (s *schedulerService) sendTimePreference(ctx context.Context, orgID *uuid.UUID) sendTimePreference {
	if s.outreachRepo == nil || orgID == nil {
		return sendTimePreference{}
	}
	settings, err := s.outreachRepo.GetOutreachSettings(ctx, *orgID)
	if err != nil {
		return sendTimePreference{}
	}
	return sendTimePreferenceFrom(settings)
}

// NewSchedulerService creates a new scheduler service
func NewSchedulerService(
	taskRepo repository.TaskRepository,
	warmupRepo repository.WarmupRepository,
	campaignProgressRepo repository.CampaignProgressRepository,
	emailRepo repository.EmailRepository,
	campaignRepo repository.CampaignRepository,
	contactRepo repository.ContactRepository,
	campaignLogRepo repository.CampaignLogRepository,
) SchedulerService {
	return &schedulerService{
		taskRepo:             taskRepo,
		warmupRepo:           warmupRepo,
		campaignProgressRepo: campaignProgressRepo,
		emailRepo:            emailRepo,
		campaignRepo:         campaignRepo,
		contactRepo:          contactRepo,
		campaignLogRepo:      campaignLogRepo,
	}
}

// Timezone utilities

// loadLocation safely loads a timezone location with fallback to UTC
func loadLocation(tz string) *time.Location {
	if tz == "" {
		return time.UTC
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// convertToAccountTimezone converts a time from campaign timezone to account timezone
func convertToAccountTimezone(t time.Time, campaignTZ, accountTZ string) time.Time {
	campaignLoc := loadLocation(campaignTZ)
	accountLoc := loadLocation(accountTZ)

	// Get the wall clock time in campaign timezone
	year, month, day := t.In(campaignLoc).Date()
	hour, min, sec := t.In(campaignLoc).Clock()

	// Reconstruct in account timezone
	return time.Date(year, month, day, hour, min, sec, 0, accountLoc)
}

// Time calculation helpers

// parseTimeOfDay parses a time string like "08:00" and returns minutes since midnight
// parseTimeOfDay reads "HH:MM" into minutes since midnight. Delegates to
// models.ClockMinutes so the Postgres `time` rendering is handled in one place.
func parseTimeOfDay(timeStr string) int {
	return models.ClockMinutes(timeStr, 0)
}

// calculateBusinessHoursRemaining calculates hours remaining in business day
func calculateBusinessHoursRemaining(timezone string) float64 {
	loc := loadLocation(timezone)
	now := time.Now().In(loc)

	// Business hours: 8am to 8pm (12 hours)
	endOfDay := time.Date(now.Year(), now.Month(), now.Day(), 20, 0, 0, 0, loc)

	if now.After(endOfDay) {
		return 0
	}

	remaining := endOfDay.Sub(now).Hours()
	return max(0, remaining)
}

// calculateFirstSlotTomorrow calculates first business hour slot tomorrow
func calculateFirstSlotTomorrow(timezone string) time.Time {
	loc := loadLocation(timezone)
	now := time.Now().In(loc)

	// Tomorrow at 8am + random jitter (0-60 minutes)
	tomorrow := now.Add(24 * time.Hour)
	firstSlot := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 8, 0, 0, 0, loc)

	// Add random jitter
	jitter := randomJitter(0, 60)
	return finalSlot(firstSlot.Add(time.Minute * time.Duration(jitter)))
}

// randomJitter generates random jitter between min and max minutes
func randomJitter(min, max int) int {
	if min >= max {
		return min
	}
	return min + rand.Intn(max-min)
}
