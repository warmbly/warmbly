package analytics

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type AnalyticsService interface {
	// Warmup analytics
	GetWarmupAnalytics(ctx context.Context, orgID uuid.UUID, emailAccountID *uuid.UUID, from, to time.Time) (*models.WarmupAnalytics, *errx.Error)
	// GetWarmupPlacement is where warmup mail landed, for one mailbox or the workspace.
	GetWarmupPlacement(ctx context.Context, orgID uuid.UUID, emailAccountID *uuid.UUID, from, to time.Time) (*models.WarmupPlacementReport, *errx.Error)

	// Campaign analytics
	GetCampaignAnalytics(ctx context.Context, orgID, campaignID uuid.UUID) (*models.CampaignAnalytics, *errx.Error)
	GetCampaignDailyStats(ctx context.Context, orgID, campaignID uuid.UUID, from, to time.Time) ([]models.CampaignDailyStats, *errx.Error)

	// Email account status
	GetAccountStatus(ctx context.Context, orgID, accountID uuid.UUID) (*models.EmailAccountStatus, *errx.Error)
	// GetAccountStatusDetail adds what is too costly to read per mailbox on a
	// list: the partner cap on today's warmup target.
	GetAccountStatusDetail(ctx context.Context, orgID, accountID uuid.UUID) (*models.EmailAccountStatus, *errx.Error)
	GetAllAccountStatuses(ctx context.Context, orgID uuid.UUID) ([]models.EmailAccountStatus, *errx.Error)

	// Usage overview
	GetUsageOverview(ctx context.Context, orgID, userID uuid.UUID, period string) (*models.UsageOverview, *errx.Error)

	// Dashboard analytics
	GetDashboardAnalytics(ctx context.Context, orgID uuid.UUID, period string) (*models.DashboardAnalytics, *errx.Error)
	GetDirectMailAnalytics(ctx context.Context, orgID uuid.UUID, period string) (*models.DirectMailAnalytics, *errx.Error)
	GetCampaignHourlyStats(ctx context.Context, orgID, campaignID uuid.UUID, date time.Time) ([]models.CampaignHourlyStats, *errx.Error)
	CompareCampaigns(ctx context.Context, orgID uuid.UUID, campaignIDs []uuid.UUID, from, to time.Time) (*models.CampaignComparison, *errx.Error)
}

type analyticsService struct {
	analyticsRepo          repository.AnalyticsRepository
	emailRepo              repository.EmailRepository
	campaignRepo           repository.CampaignRepository
	emailAccountErrorsRepo repository.EmailAccountErrorRepository
	warmupRepo             repository.WarmupRepository
	// lifecycleRepo reads whether the mailbox is in cold rotation.
	// Optional/nil-safe.
	lifecycleRepo repository.SendLifecycleRepository
	// placementRepo reads warmup placement history. Optional/nil-safe.
	placementRepo repository.WarmupPlacementRepository
}

func NewService(
	analyticsRepo repository.AnalyticsRepository,
	emailRepo repository.EmailRepository,
	campaignRepo repository.CampaignRepository,
	emailAccountErrorsRepo repository.EmailAccountErrorRepository,
	warmupRepo repository.WarmupRepository,
) AnalyticsService {
	return &analyticsService{
		analyticsRepo:          analyticsRepo,
		emailRepo:              emailRepo,
		campaignRepo:           campaignRepo,
		emailAccountErrorsRepo: emailAccountErrorsRepo,
		warmupRepo:             warmupRepo,
	}
}

// warmupHealthPoolLookup lists the pools to check for a participant's health,
// premium first so paid orgs reflect their premium-pool reputation.
var warmupHealthPoolLookup = []string{"premium", "free"}

func (s *analyticsService) GetWarmupAnalytics(ctx context.Context, orgID uuid.UUID, emailAccountID *uuid.UUID, from, to time.Time) (*models.WarmupAnalytics, *errx.Error) {
	// Get daily stats
	dailyStats, xerr := s.analyticsRepo.GetWarmupStats(ctx, orgID, emailAccountID, from, to)
	if xerr != nil {
		return nil, xerr
	}

	// Calculate summary
	var totalSent, totalReplied, totalReceived, totalTarget, daysActive int
	for _, day := range dailyStats {
		totalSent += day.EmailsSent
		totalReplied += day.EmailsReplied
		totalReceived += day.EmailsReceived
		totalTarget += day.TargetVolume
		if day.Active {
			daysActive++
		}
	}
	var averageDaily, replyRate, targetProgress float64
	if daysActive > 0 {
		averageDaily = float64(totalSent) / float64(daysActive)
	}
	if totalSent > 0 {
		replyRate = float64(totalReplied) / float64(totalSent) * 100
	}
	if totalTarget > 0 {
		targetProgress = float64(totalSent) / float64(totalTarget) * 100
	}

	analytics := &models.WarmupAnalytics{
		DateRange: models.DateRange{
			From: from,
			To:   to,
		},
		Summary: models.WarmupSummary{
			TotalSent:      totalSent,
			TotalReplied:   totalReplied,
			TotalReceived:  totalReceived,
			AverageDaily:   averageDaily,
			ReplyRate:      replyRate,
			TargetProgress: targetProgress,
			DaysActive:     daysActive,
		},
		DailyStats: dailyStats,
	}

	if emailAccountID != nil {
		analytics.EmailAccountID = *emailAccountID
	}

	return analytics, nil
}

func (s *analyticsService) GetCampaignAnalytics(ctx context.Context, orgID, campaignID uuid.UUID) (*models.CampaignAnalytics, *errx.Error) {
	// Get campaign details
	campaign, err := s.campaignForOrg(ctx, orgID, campaignID)
	if err != nil {
		return nil, err
	}

	// Get summary
	summary, xerr := s.analyticsRepo.GetCampaignSummary(ctx, orgID, campaignID)
	if xerr != nil {
		return nil, xerr
	}

	// Get sequence stats
	sequences, xerr := s.analyticsRepo.GetSequenceStats(ctx, campaignID)
	if xerr != nil {
		return nil, xerr
	}

	// Where and on what people engaged; best-effort, the totals stand alone.
	engagement, xerr := s.analyticsRepo.GetCampaignEngagementBreakdown(ctx, campaignID, 8)
	if xerr != nil {
		engagement = nil
	}

	return &models.CampaignAnalytics{
		CampaignID: campaignID,
		Name:       campaign.Name,
		Status:     campaign.Status,
		Summary:    *summary,
		Sequences:  sequences,
		Engagement: engagement,
	}, nil
}

func (s *analyticsService) GetCampaignDailyStats(ctx context.Context, orgID, campaignID uuid.UUID, from, to time.Time) ([]models.CampaignDailyStats, *errx.Error) {
	// Verify campaign workspace
	_, err := s.campaignForOrg(ctx, orgID, campaignID)
	if err != nil {
		return nil, err
	}

	return s.analyticsRepo.GetCampaignDailyStats(ctx, campaignID, from, to)
}

func (s *analyticsService) GetAccountStatus(ctx context.Context, orgID, accountID uuid.UUID) (*models.EmailAccountStatus, *errx.Error) {
	return s.accountStatus(ctx, orgID, accountID, s.placementRates(ctx, orgID, &accountID), false)
}

func (s *analyticsService) GetAccountStatusDetail(ctx context.Context, orgID, accountID uuid.UUID) (*models.EmailAccountStatus, *errx.Error) {
	return s.accountStatus(ctx, orgID, accountID, s.placementRates(ctx, orgID, &accountID), true)
}

// accountStatus builds one mailbox's status; rates is the org's rolling
// placement, read once by the caller. withPartners adds the partner cap, a
// pool-wide read that lists must not repeat per mailbox.
func (s *analyticsService) accountStatus(ctx context.Context, orgID, accountID uuid.UUID, rates map[uuid.UUID]models.WarmupPlacementRate, withPartners bool) (*models.EmailAccountStatus, *errx.Error) {
	// Get email account (org-scoped lookup)
	email, xerr := s.emailRepo.Get(ctx, orgID.String(), accountID.String())
	if xerr != nil {
		return nil, xerr
	}

	// Get daily usage
	now := time.Now().UTC()
	usage, xerr := s.analyticsRepo.GetAccountDailyUsage(ctx, accountID, now)
	if xerr != nil {
		return nil, xerr
	}

	// Get errors
	var errors []models.AccountError
	if s.emailAccountErrorsRepo != nil {
		dbErrors, err := s.emailAccountErrorsRepo.GetByAccountID(ctx, accountID, true)
		if err == nil {
			for _, e := range dbErrors {
				errors = append(errors, models.AccountError{
					ID:             e.ID,
					ErrorCode:      e.ErrorCode,
					Severity:       e.Severity,
					Title:          e.Title,
					Message:        e.Message,
					ActionRequired: e.ActionRequired,
					CreatedAt:      e.CreatedAt,
				})
			}
		}
	}
	if errors == nil {
		errors = make([]models.AccountError, 0)
	}

	// Calculate health, then fold in warmup-pool reputation so the single
	// score the user sees reflects spam placement / complaints / throttling.
	health := calculateAccountHealth(email, errors)
	warmupHealth := s.buildWarmupHealth(ctx, accountID)
	applyWarmupHealth(&health, warmupHealth)
	var placement *models.WarmupPlacementRate
	if r, ok := rates[accountID]; ok {
		placement = &r
	}
	applyWarmupPlacement(&health, placement)

	inCampaign := false
	if s.campaignRepo != nil {
		if n, err := s.campaignRepo.CountActiveCampaignsForAccount(ctx, accountID); err == nil {
			inCampaign = n > 0
		}
	}

	// Build warmup status if warmup has ever been enabled (active or paused).
	var warmupStatus *models.WarmupStatusInfo
	if email.Warmup != nil {
		target, hold := s.warmupTargetAndHold(ctx, email, warmupHealthState(warmupHealth), inCampaign)
		var limit *models.WarmupPartnerLimit
		if withPartners {
			limit = s.warmupPartnerLimit(ctx, email, warmupHealth, target)
		}
		if limit != nil {
			target = max(limit.Reachable, usage.WarmupSent)
		}
		warmupStatus = &models.WarmupStatusInfo{
			Enabled:       true,
			Paused:        email.WarmupPausedAt != nil,
			PausedAt:      email.WarmupPausedAt,
			StartedAt:     *email.Warmup,
			CurrentVolume: usage.WarmupSent,
			TargetVolume:  target,
			MaxVolume:     email.WarmupMax,
			ReplyRate:     email.WarmupReplyRate,
			DaysActive:    int(time.Since(*email.Warmup).Hours() / 24),
			RampHold:      hold,
			PartnerLimit:  limit,
		}
	}

	coldRamp := s.coldRampInfo(ctx, email)
	lifecycle := s.sendLifecycleInfo(ctx, email.ID)

	return &models.EmailAccountStatus{
		ID:            email.ID,
		Email:         email.Email,
		Provider:      email.Provider,
		Status:        email.Status,
		LastSyncedAt:  &email.LastSyncedAt,
		Health:        health,
		Errors:        errors,
		DailyUsage:    *usage,
		WarmupStatus:  warmupStatus,
		WarmupHealth:  warmupHealth,
		Placement:     placement,
		InCampaign:    inCampaign,
		ColdRamp:      coldRamp,
		SendLifecycle: lifecycle,
	}, nil
}

// warmupHealthState reads the band off the API shape, defaulting to healthy
// for a mailbox in no pool — the same default resolveHealthState uses.
func warmupHealthState(h *models.WarmupHealthInfo) models.WarmupHealthState {
	if h == nil || h.State == "" {
		return models.WarmupHealthHealthy
	}
	return models.WarmupHealthState(h.State)
}

// warmupDiversityWindow matches the selector's domain-history window.
const warmupDiversityWindow = 7 * 24 * time.Hour

// buildWarmupHealth looks up the mailbox's warmup-pool health (premium pool
// first) and maps it into the API shape. Returns nil when the mailbox is not
// in a pool or the lookup fails — health surfacing must never break status.
func (s *analyticsService) buildWarmupHealth(ctx context.Context, accountID uuid.UUID) *models.WarmupHealthInfo {
	if s.warmupRepo == nil {
		return nil
	}
	for _, poolType := range warmupHealthPoolLookup {
		h, err := s.warmupRepo.GetParticipantHealth(ctx, accountID, poolType)
		if err != nil || h == nil {
			continue
		}
		info := &models.WarmupHealthInfo{
			PoolType:     poolType,
			State:        string(h.HealthState),
			Score:        h.LastHealthScore,
			BlockedUntil: h.BlockedUntil,
			EvaluatedAt:  h.LastHealthEvaluatedAt,
		}
		if h.LastHealthReason != nil {
			info.Reason = *h.LastHealthReason
		}
		// Best-effort: the counts are a read-out, never a reason to fail status.
		if d, derr := s.warmupRepo.GetPartnerDiversity(ctx, accountID, time.Now().Add(-warmupDiversityWindow)); derr == nil {
			info.PartnerMailboxes7d = d.Mailboxes
			info.PartnerDomains7d = d.Domains
			info.PartnerOrganizations7d = d.Organizations
			info.Received7d = d.Received
			info.Senders7d = d.Senders
		}
		return info
	}
	return nil
}

// applyWarmupHealth folds warmup-pool reputation into the unified account
// health score so a throttled/quarantined mailbox reads as degraded even when
// its connection and sync are fine. Healthy/empty states leave health intact.
func applyWarmupHealth(health *models.AccountHealth, wh *models.WarmupHealthInfo) {
	if wh == nil {
		return
	}

	var penalty int
	var issue string
	switch models.WarmupHealthState(wh.State) {
	case models.WarmupHealthWatch:
		penalty, issue = 10, "Warmup reputation needs watching"
	case models.WarmupHealthThrottled:
		penalty, issue = 25, "Warmup throttled — spam placement elevated"
	case models.WarmupHealthQuarantined:
		penalty, issue = 50, "Warmup quarantined — mailbox temporarily removed from the pool"
	case models.WarmupHealthBlocked:
		penalty, issue = 70, "Warmup blocked — reputation requires review"
	default:
		return
	}

	if wh.Reason != "" {
		issue = issue + " (" + wh.Reason + ")"
	}

	health.Score -= penalty
	if health.Score < 0 {
		health.Score = 0
	}
	switch models.WarmupHealthState(wh.State) {
	case models.WarmupHealthWatch:
		if health.Status == "healthy" {
			health.Status = "warning"
		}
	default:
		if health.Status != "error" {
			health.Status = "error"
		}
	}
	health.Issues = append(health.Issues, issue)
}

func (s *analyticsService) GetAllAccountStatuses(ctx context.Context, orgID uuid.UUID) ([]models.EmailAccountStatus, *errx.Error) {
	// Get all email accounts for the organization
	emailsResult, xerr := s.emailRepo.Search(ctx, orgID.String(), "", nil, nil, 1000, nil)
	if xerr != nil {
		return nil, xerr
	}

	rates := s.placementRates(ctx, orgID, nil)
	statuses := make([]models.EmailAccountStatus, 0, len(emailsResult.Data))
	for _, email := range emailsResult.Data {
		status, xerr := s.accountStatus(ctx, orgID, email.ID, rates, false)
		if xerr != nil {
			return nil, xerr
		}
		statuses = append(statuses, *status)
	}

	return statuses, nil
}

func (s *analyticsService) GetUsageOverview(ctx context.Context, orgID, userID uuid.UUID, period string) (*models.UsageOverview, *errx.Error) {
	now := time.Now().UTC()
	var from time.Time
	switch period {
	case "week":
		from = now.AddDate(0, 0, -7)
	case "month":
		from = now.AddDate(0, -1, 0)
	default:
		period = "day"
		from = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	}

	// Get email account counts
	accountsUsage, xerr := s.analyticsRepo.GetEmailAccountCounts(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}

	// Get campaign counts
	campaignsUsage, xerr := s.analyticsRepo.GetCampaignCounts(ctx, orgID, from, now)
	if xerr != nil {
		return nil, xerr
	}

	// Get contact counts
	contactsUsage, xerr := s.analyticsRepo.GetContactCounts(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}

	// API usage would come from rate limit service
	apiUsage := models.APIUsage{
		TotalCalls:   0, // Would be populated from rate limit tracking
		DailyLimit:   50000,
		TopEndpoints: make([]models.EndpointUsage, 0),
	}

	return &models.UsageOverview{
		UserID:        userID,
		Period:        period,
		EmailAccounts: *accountsUsage,
		Campaigns:     *campaignsUsage,
		Contacts:      *contactsUsage,
		API:           apiUsage,
	}, nil
}

// Helper functions

func calculateAccountHealth(email *models.Email, errors []models.AccountError) models.AccountHealth {
	health := models.AccountHealth{
		Status: "healthy",
		Score:  100,
		Issues: make([]string, 0),
	}

	// Check status
	if email.Status != "active" {
		health.Status = "error"
		health.Score -= 50
		health.Issues = append(health.Issues, "Account is not active")
	}

	// Check for critical errors
	for _, e := range errors {
		if e.Severity == "CRITICAL" {
			health.Status = "error"
			health.Score -= 30
			health.Issues = append(health.Issues, e.Title)
		} else if e.Severity == "WARNING" {
			if health.Status == "healthy" {
				health.Status = "warning"
			}
			health.Score -= 10
			health.Issues = append(health.Issues, e.Title)
		}
	}

	// Ensure score doesn't go below 0
	if health.Score < 0 {
		health.Score = 0
	}

	return health
}

// warmupTargetAndHold reports the target the SCHEDULER will act on, plus what
// is holding it down. It runs the shared warmupramp policy rather than its own
// arithmetic: a drawer saying "target 25" for a mailbox sending 18 is worse
// than no number.
func (s *analyticsService) warmupTargetAndHold(ctx context.Context, email *models.Email, health models.WarmupHealthState, inCampaign bool) (int, *models.WarmupRampHold) {
	if email.Warmup == nil {
		return 0, nil
	}
	plan := warmupramp.Resolve(ctx, s.warmupRepo, warmupramp.Input{
		AccountID:       email.ID,
		WarmupStart:     *email.Warmup,
		ActivelyWarming: email.IsWarmingActive(),
		Base:            email.WarmupBase,
		Increase:        email.WarmupIncrease,
		Max:             email.WarmupMax,
		InCampaign:      inCampaign,
		Health:          health,
		Now:             time.Now(),
	})
	if plan.FrozenUntil == nil {
		return plan.Target, nil
	}
	// Reported for the whole freeze, not just the shorter window that also
	// cuts volume: otherwise a mailbox between 48 and 72 hours after a
	// placement shows a ramp that is not climbing and no reason why.
	return plan.Target, &models.WarmupRampHold{
		Placements: plan.Placements,
		Sends:      plan.Sends,
		VolumeCut:  plan.Cut(),
		ResumesAt:  *plan.FrozenUntil,
	}
}

// warmupPartnerLimit applies the scheduler's partner cap to the drawer's
// target: a mailbox never writes to one partner twice in a day, so it cannot
// send more than the partners it can still reach.
func (s *analyticsService) warmupPartnerLimit(ctx context.Context, email *models.Email, wh *models.WarmupHealthInfo, target int) *models.WarmupPartnerLimit {
	if s.warmupRepo == nil || wh == nil || wh.PoolType == "" || target <= 0 || !email.IsWarmingActive() {
		return nil
	}
	cands, err := s.warmupRepo.WarmupPartnerCandidates(ctx, wh.PoolType, email.ID)
	if err != nil || len(cands) >= target {
		return nil
	}
	return &models.WarmupPartnerLimit{Reachable: len(cands), RampTarget: target}
}

// Dashboard Analytics implementations

func (s *analyticsService) GetDashboardAnalytics(ctx context.Context, orgID uuid.UUID, period string) (*models.DashboardAnalytics, *errx.Error) {
	// Calculate date range from period
	to := time.Now().UTC()
	startOfToday := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	var from time.Time

	switch period {
	case "7d":
		from = startOfToday.AddDate(0, 0, -6)
	case "30d":
		from = startOfToday.AddDate(0, 0, -29)
	case "90d":
		from = startOfToday.AddDate(0, 0, -89)
	default:
		from = startOfToday.AddDate(0, 0, -6)
		period = "7d"
	}

	// Get overall stats
	overallStats, xerr := s.analyticsRepo.GetDashboardOverallStats(ctx, orgID, from, to)
	if xerr != nil {
		return nil, xerr
	}

	// Get recent activity
	recentActivity, xerr := s.analyticsRepo.GetRecentActivity(ctx, orgID, 20)
	if xerr != nil {
		recentActivity = make([]models.RecentActivityItem, 0)
	}

	// Get top campaigns
	topCampaigns, xerr := s.analyticsRepo.GetTopCampaigns(ctx, orgID, from, to, 5, "emails_sent")
	if xerr != nil {
		topCampaigns = make([]models.TopCampaignStats, 0)
	}

	// Get account health summary
	accountHealth, xerr := s.analyticsRepo.GetAccountHealthSummary(ctx, orgID)
	if xerr != nil {
		accountHealth = &models.AccountHealthSummary{}
	}

	// Get daily trend
	dailyTrend, xerr := s.analyticsRepo.GetDashboardDailyTrend(ctx, orgID, from, to)
	if xerr != nil {
		dailyTrend = make([]models.DashboardDailyStats, 0)
	}

	return &models.DashboardAnalytics{
		Period:         period,
		OverallStats:   *overallStats,
		RecentActivity: recentActivity,
		TopCampaigns:   topCampaigns,
		AccountHealth:  *accountHealth,
		DailyTrend:     dailyTrend,
	}, nil
}

func (s *analyticsService) GetCampaignHourlyStats(ctx context.Context, orgID, campaignID uuid.UUID, date time.Time) ([]models.CampaignHourlyStats, *errx.Error) {
	// Verify campaign workspace
	_, err := s.campaignForOrg(ctx, orgID, campaignID)
	if err != nil {
		return nil, err
	}

	return s.analyticsRepo.GetCampaignHourlyStats(ctx, campaignID, date)
}

func (s *analyticsService) CompareCampaigns(ctx context.Context, orgID uuid.UUID, campaignIDs []uuid.UUID, from, to time.Time) (*models.CampaignComparison, *errx.Error) {
	// Validate that all campaigns belong to the selected workspace
	for _, campaignID := range campaignIDs {
		_, err := s.campaignForOrg(ctx, orgID, campaignID)
		if err != nil {
			return nil, err
		}
	}

	return s.analyticsRepo.CompareCampaigns(ctx, orgID, campaignIDs, from, to)
}

// coldRampInfo explains a graduation ceiling holding this mailbox below its own
// cold cap. Nil when nothing is holding it, so the drawer stays quiet.
func (s *analyticsService) coldRampInfo(ctx context.Context, email *models.Email) *models.ColdRampInfo {
	if email.Warmup == nil || s.warmupRepo == nil || email.CampaignLimit <= 0 {
		return nil
	}
	states, err := s.warmupRepo.ColdRampStateForAccounts(ctx,
		[]uuid.UUID{email.ID}, time.Now().Add(-warmupramp.LookbackWindow))
	if err != nil {
		return nil
	}
	state, ok := states[email.ID]
	if !ok {
		return nil
	}
	info := warmupramp.Notice(state.WarmupStartedAt, state.ColdRampStartedAt, state.Placements, email.CampaignLimit, time.Now())
	if info == nil || info.Ceiling >= email.CampaignLimit {
		return nil
	}
	return info
}

// sendLifecycleInfo reports the mailbox's cold-rotation state, but only when
// it is not active: an active mailbox is the normal case and needs no notice.
func (s *analyticsService) sendLifecycleInfo(ctx context.Context, accountID uuid.UUID) *models.SendLifecycleState {
	if s.lifecycleRepo == nil {
		return nil
	}
	states, err := s.lifecycleRepo.GetSendLifecycles(ctx, []uuid.UUID{accountID})
	if err != nil {
		return nil
	}
	state, ok := states[accountID]
	if !ok || state.State.SendsCold() {
		return nil
	}
	return &state
}

// WireLifecycle attaches the cold-sending lifecycle.
func (s *analyticsService) WireLifecycle(r repository.SendLifecycleRepository) {
	s.lifecycleRepo = r
}

// LifecycleAware is the optional capability the caller uses to attach it.
type LifecycleAware interface {
	WireLifecycle(r repository.SendLifecycleRepository)
}

// campaignForOrg applies the same workspace boundary as the campaign detail endpoint.
func (s *analyticsService) campaignForOrg(ctx context.Context, orgID, campaignID uuid.UUID) (*models.Campaign, *errx.Error) {
	campaign, err := s.campaignRepo.Get(ctx, orgID.String(), campaignID.String())
	if errors.Is(err, errx.ErrResourceNotFound) {
		return nil, errx.ErrNotFound
	}
	if err != nil {
		return nil, errx.InternalError()
	}
	return campaign, nil
}

// GetDirectMailAnalytics reports on mail written by hand rather than sent by a
// campaign. Same period vocabulary as the dashboard so the two views agree on
// what "last 7 days" means.
func (s *analyticsService) GetDirectMailAnalytics(ctx context.Context, orgID uuid.UUID, period string) (*models.DirectMailAnalytics, *errx.Error) {
	from, to, period := dashboardRange(period)

	out, xerr := s.analyticsRepo.GetDirectMailAnalytics(ctx, orgID, from, to)
	if xerr != nil {
		return nil, xerr
	}
	out.Period = period
	return out, nil
}

// dashboardRange turns a period name into a window, and hands back the name it
// actually used so a caller echoing it back never reports a period it did not
// measure.
func dashboardRange(period string) (time.Time, time.Time, string) {
	to := time.Now()
	switch period {
	case "30d":
		return to.AddDate(0, 0, -30), to, period
	case "90d":
		return to.AddDate(0, 0, -90), to, period
	case "7d":
		return to.AddDate(0, 0, -7), to, period
	default:
		return to.AddDate(0, 0, -7), to, "7d"
	}
}
