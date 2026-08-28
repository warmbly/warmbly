package analytics

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type AnalyticsService interface {
	// Warmup analytics
	GetWarmupAnalytics(ctx context.Context, userID uuid.UUID, emailAccountID *uuid.UUID, from, to time.Time) (*models.WarmupAnalytics, *errx.Error)

	// Campaign analytics
	GetCampaignAnalytics(ctx context.Context, userID, campaignID uuid.UUID) (*models.CampaignAnalytics, *errx.Error)
	GetCampaignDailyStats(ctx context.Context, userID, campaignID uuid.UUID, from, to time.Time) ([]models.CampaignDailyStats, *errx.Error)

	// Email account status
	GetAccountStatus(ctx context.Context, orgID, accountID uuid.UUID) (*models.EmailAccountStatus, *errx.Error)
	GetAllAccountStatuses(ctx context.Context, orgID uuid.UUID) ([]models.EmailAccountStatus, *errx.Error)

	// Usage overview
	GetUsageOverview(ctx context.Context, userID uuid.UUID, period string) (*models.UsageOverview, *errx.Error)

	// Dashboard analytics
	GetDashboardAnalytics(ctx context.Context, userID uuid.UUID, period string) (*models.DashboardAnalytics, *errx.Error)
	GetCampaignHourlyStats(ctx context.Context, userID, campaignID uuid.UUID, date time.Time) ([]models.CampaignHourlyStats, *errx.Error)
	CompareCampaigns(ctx context.Context, userID uuid.UUID, campaignIDs []uuid.UUID, from, to time.Time) (*models.CampaignComparison, *errx.Error)
}

type analyticsService struct {
	analyticsRepo          repository.AnalyticsRepository
	emailRepo              repository.EmailRepository
	campaignRepo           repository.CampaignRepository
	emailAccountErrorsRepo repository.EmailAccountErrorRepository
	warmupRepo             repository.WarmupRepository
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

func (s *analyticsService) GetWarmupAnalytics(ctx context.Context, userID uuid.UUID, emailAccountID *uuid.UUID, from, to time.Time) (*models.WarmupAnalytics, *errx.Error) {
	// Get daily stats
	dailyStats, xerr := s.analyticsRepo.GetWarmupStats(ctx, userID, emailAccountID, from, to)
	if xerr != nil {
		return nil, xerr
	}

	// Calculate summary
	var totalSent, totalReplied int
	for _, day := range dailyStats {
		totalSent += day.EmailsSent
		totalReplied += day.EmailsReplied
	}

	daysActive := len(dailyStats)
	var averageDaily, replyRate float64
	if daysActive > 0 {
		averageDaily = float64(totalSent) / float64(daysActive)
	}
	if totalSent > 0 {
		replyRate = float64(totalReplied) / float64(totalSent) * 100
	}

	analytics := &models.WarmupAnalytics{
		DateRange: models.DateRange{
			From: from,
			To:   to,
		},
		Summary: models.WarmupSummary{
			TotalSent:    totalSent,
			TotalReplied: totalReplied,
			AverageDaily: averageDaily,
			ReplyRate:    replyRate,
			DaysActive:   daysActive,
		},
		DailyStats: dailyStats,
	}

	if emailAccountID != nil {
		analytics.EmailAccountID = *emailAccountID
	}

	return analytics, nil
}

func (s *analyticsService) GetCampaignAnalytics(ctx context.Context, userID, campaignID uuid.UUID) (*models.CampaignAnalytics, *errx.Error) {
	// Get campaign details
	campaign, err := s.campaignRepo.GetByID(ctx, campaignID)
	if err != nil {
		return nil, errx.ErrNotFound
	}

	// Verify ownership
	if campaign.UserID != userID.String() {
		return nil, errx.ErrForbidden
	}

	// Get summary
	summary, xerr := s.analyticsRepo.GetCampaignSummary(ctx, userID, campaignID)
	if xerr != nil {
		return nil, xerr
	}

	// Get sequence stats
	sequences, xerr := s.analyticsRepo.GetSequenceStats(ctx, campaignID)
	if xerr != nil {
		return nil, xerr
	}

	return &models.CampaignAnalytics{
		CampaignID: campaignID,
		Name:       campaign.Name,
		Status:     campaign.Status,
		Summary:    *summary,
		Sequences:  sequences,
	}, nil
}

func (s *analyticsService) GetCampaignDailyStats(ctx context.Context, userID, campaignID uuid.UUID, from, to time.Time) ([]models.CampaignDailyStats, *errx.Error) {
	// Verify campaign ownership
	campaign, err := s.campaignRepo.GetByID(ctx, campaignID)
	if err != nil {
		return nil, errx.ErrNotFound
	}
	if campaign.UserID != userID.String() {
		return nil, errx.ErrForbidden
	}

	return s.analyticsRepo.GetCampaignDailyStats(ctx, campaignID, from, to)
}

func (s *analyticsService) GetAccountStatus(ctx context.Context, orgID, accountID uuid.UUID) (*models.EmailAccountStatus, *errx.Error) {
	// Get email account (org-scoped lookup)
	email, xerr := s.emailRepo.Get(ctx, orgID.String(), accountID.String())
	if xerr != nil {
		return nil, xerr
	}

	// Get daily usage
	usage, xerr := s.analyticsRepo.GetAccountDailyUsage(ctx, accountID, time.Now())
	if xerr != nil {
		// Non-fatal, use empty usage
		usage = &models.AccountDailyUsage{
			Date: time.Now().Format("2006-01-02"),
		}
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

	// Build warmup status if warmup has ever been enabled (active or paused).
	var warmupStatus *models.WarmupStatusInfo
	if email.Warmup != nil {
		target, hold := s.warmupTargetAndHold(ctx, email, warmupHealthState(warmupHealth))
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
		}
	}

	inCampaign := false
	if s.campaignRepo != nil {
		if n, err := s.campaignRepo.CountActiveCampaignsForAccount(ctx, accountID); err == nil {
			inCampaign = n > 0
		}
	}

	coldRamp := s.coldRampInfo(ctx, email)

	return &models.EmailAccountStatus{
		ID:           email.ID,
		Email:        email.Email,
		Provider:     email.Provider,
		Status:       email.Status,
		LastSyncedAt: &email.LastSyncedAt,
		Health:       health,
		Errors:       errors,
		DailyUsage:   *usage,
		WarmupStatus: warmupStatus,
		WarmupHealth: warmupHealth,
		InCampaign:   inCampaign,
		ColdRamp:     coldRamp,
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
			State:        string(h.HealthState),
			Score:        h.LastHealthScore,
			SpamScore:    h.SpamScore,
			BlockedUntil: h.BlockedUntil,
			EvaluatedAt:  h.LastHealthEvaluatedAt,
		}
		if h.LastHealthReason != nil {
			info.Reason = *h.LastHealthReason
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

	statuses := make([]models.EmailAccountStatus, 0, len(emailsResult.Data))
	for _, email := range emailsResult.Data {
		status, xerr := s.GetAccountStatus(ctx, orgID, email.ID)
		if xerr != nil {
			continue // Skip failed accounts
		}
		statuses = append(statuses, *status)
	}

	return statuses, nil
}

func (s *analyticsService) GetUsageOverview(ctx context.Context, userID uuid.UUID, period string) (*models.UsageOverview, *errx.Error) {
	// Get email account counts
	accountsUsage, xerr := s.analyticsRepo.GetEmailAccountCounts(ctx, userID)
	if xerr != nil {
		return nil, xerr
	}

	// Get campaign counts
	campaignsUsage, xerr := s.analyticsRepo.GetCampaignCounts(ctx, userID)
	if xerr != nil {
		return nil, xerr
	}

	// Get contact counts
	contactsUsage, xerr := s.analyticsRepo.GetContactCounts(ctx, userID)
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
func (s *analyticsService) warmupTargetAndHold(ctx context.Context, email *models.Email, health models.WarmupHealthState) (int, *models.WarmupRampHold) {
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

// Dashboard Analytics implementations

func (s *analyticsService) GetDashboardAnalytics(ctx context.Context, orgID uuid.UUID, period string) (*models.DashboardAnalytics, *errx.Error) {
	// Calculate date range from period
	var from, to time.Time
	to = time.Now()

	switch period {
	case "7d":
		from = to.AddDate(0, 0, -7)
	case "30d":
		from = to.AddDate(0, 0, -30)
	case "90d":
		from = to.AddDate(0, 0, -90)
	default:
		from = to.AddDate(0, 0, -7) // Default to 7 days
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

func (s *analyticsService) GetCampaignHourlyStats(ctx context.Context, userID, campaignID uuid.UUID, date time.Time) ([]models.CampaignHourlyStats, *errx.Error) {
	// Verify campaign ownership
	campaign, err := s.campaignRepo.GetByID(ctx, campaignID)
	if err != nil {
		return nil, errx.ErrNotFound
	}
	if campaign.UserID != userID.String() {
		return nil, errx.ErrForbidden
	}

	return s.analyticsRepo.GetCampaignHourlyStats(ctx, campaignID, date)
}

func (s *analyticsService) CompareCampaigns(ctx context.Context, userID uuid.UUID, campaignIDs []uuid.UUID, from, to time.Time) (*models.CampaignComparison, *errx.Error) {
	// Validate that all campaigns belong to user
	for _, campaignID := range campaignIDs {
		campaign, err := s.campaignRepo.GetByID(ctx, campaignID)
		if err != nil {
			return nil, errx.ErrNotFound
		}
		if campaign.UserID != userID.String() {
			return nil, errx.ErrForbidden
		}
	}

	return s.analyticsRepo.CompareCampaigns(ctx, userID, campaignIDs, from, to)
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
	if !ok || state.WarmupStartedAt == nil {
		return nil
	}

	now := time.Now()
	warmupDays := int(now.Sub(*state.WarmupStartedAt).Hours() / 24)
	if warmupDays < 0 {
		warmupDays = 0
	}
	var rampStart time.Time
	if state.ColdRampStartedAt != nil {
		rampStart = *state.ColdRampStartedAt
	}

	ceiling := warmupramp.ColdCeiling(warmupDays, rampStart, state.Placements, now, email.CampaignLimit)
	if ceiling >= email.CampaignLimit {
		return nil
	}

	remaining := email.CampaignLimit - ceiling
	days := remaining / warmupramp.ColdRampIncrement
	if remaining%warmupramp.ColdRampIncrement != 0 {
		days++
	}
	return &models.ColdRampInfo{
		Ceiling:       ceiling,
		MailboxCap:    email.CampaignLimit,
		DaysToFullCap: days,
		Held:          warmupramp.ColdHeldUntil(rampStart, state.Placements, now, warmupramp.FreezeWindow) != nil,
	}
}
