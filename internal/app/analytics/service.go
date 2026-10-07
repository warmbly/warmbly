package analytics

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

type AnalyticsService interface {
	// Warmup analytics
	GetWarmupAnalytics(ctx context.Context, orgID uuid.UUID, emailAccountID *uuid.UUID, from, to time.Time) (*models.WarmupAnalytics, *errx.Error)
	// GetWarmupPlacement is where warmup mail landed, for one mailbox or the workspace.
	GetWarmupPlacement(ctx context.Context, orgID uuid.UUID, emailAccountID *uuid.UUID, from, to time.Time) (*models.WarmupPlacementReport, *errx.Error)
	GetWarmupStatsForAccounts(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, from, to time.Time) ([]models.WarmupDailyStats, *errx.Error)
	GetWarmupPlacementDataForAccounts(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, from, to time.Time) (*models.WarmupPlacementData, *errx.Error)

	// Campaign analytics
	// GetCampaignAnalytics reads the performance of the sends inside period,
	// whole UTC days with To included, or of every send when period is nil.
	GetCampaignAnalytics(ctx context.Context, orgID, campaignID uuid.UUID, period *models.DateRange) (*models.CampaignAnalytics, *errx.Error)
	GetCampaignDailyStats(ctx context.Context, orgID, campaignID uuid.UUID, from, to time.Time) ([]models.CampaignDailyStats, *errx.Error)

	// Email account status
	GetAccountStatus(ctx context.Context, orgID, accountID uuid.UUID) (*models.EmailAccountStatus, *errx.Error)
	// GetAccountStatusDetail adds what is too costly to read per mailbox on a
	// list: the partner cap on today's warmup target.
	GetAccountStatusDetail(ctx context.Context, orgID, accountID uuid.UUID) (*models.EmailAccountStatus, *errx.Error)
	GetAllAccountStatuses(ctx context.Context, orgID uuid.UUID) ([]models.EmailAccountStatus, *errx.Error)
	// GetAccountStatusesPage is the bounded, batched form behind GET
	// /analytics/accounts: one page of statuses for the whole inventory (keyset
	// cursor) or for a named set of visible mailbox ids. Per-mailbox reads are
	// batched across the page, so its cost is bounded by the page, not by the
	// total inventory.
	GetAccountStatusesPage(ctx context.Context, orgID uuid.UUID, emailIDs []uuid.UUID, cursor string, limit int32) (*models.EmailAccountStatusesResult, *errx.Error)

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
	cloudReports  CloudWarmupReports
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

func (s *analyticsService) GetWarmupAnalytics(ctx context.Context, orgID uuid.UUID, emailAccountID *uuid.UUID, from, to time.Time) (*models.WarmupAnalytics, *errx.Error) {
	// Get daily stats
	dailyStats, xerr := s.analyticsRepo.GetWarmupStats(ctx, orgID, emailAccountID, from, to)
	if xerr != nil {
		return nil, xerr
	}

	if s.cloudReports != nil {
		if emailAccountID != nil {
			if _, xerr := s.emailRepo.Get(ctx, orgID.String(), emailAccountID.String()); xerr != nil {
				return nil, xerr
			}
		}
		cloud, xerr := s.cloudReports.WarmupStats(ctx, orgID, emailAccountID, from, to)
		if xerr != nil {
			return nil, xerr
		}
		dailyStats = models.MergeWarmupStats(dailyStats, cloud)
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

func (s *analyticsService) GetCampaignAnalytics(ctx context.Context, orgID, campaignID uuid.UUID, period *models.DateRange) (*models.CampaignAnalytics, *errx.Error) {
	// Get campaign details
	campaign, err := s.campaignForOrg(ctx, orgID, campaignID)
	if err != nil {
		return nil, err
	}

	// Get summary
	summary, xerr := s.analyticsRepo.GetCampaignSummary(ctx, orgID, campaignID, period)
	if xerr != nil {
		return nil, xerr
	}

	// Get sequence stats
	sequences, xerr := s.analyticsRepo.GetSequenceStats(ctx, campaignID, period)
	if xerr != nil {
		return nil, xerr
	}

	// Where and on what people engaged; best-effort, the totals stand alone.
	engagement, xerr := s.analyticsRepo.GetCampaignEngagementBreakdown(ctx, campaignID, period, 8)
	if xerr != nil {
		engagement = nil
	}

	return &models.CampaignAnalytics{
		CampaignID: campaignID,
		Name:       campaign.Name,
		Status:     campaign.Status,
		DateRange:  campaignPeriod(campaign, summary.FirstSentAt, period, time.Now()),
		Summary:    *summary,
		Sequences:  sequences,
		Engagement: engagement,
	}, nil
}

// campaignPeriod is the window the figures cover: the one asked for, or for
// all time the first send's UTC day (creation before one) through today.
func campaignPeriod(campaign *models.Campaign, firstSent *time.Time, period *models.DateRange, now time.Time) models.DateRange {
	if period != nil {
		return *period
	}
	start := campaign.CreatedAt
	if firstSent != nil {
		start = *firstSent
	}
	today := utcDay(now)
	from := utcDay(start)
	if from.After(today) {
		from = today
	}
	return models.DateRange{From: from, To: today}
}

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
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
// pool-wide read that lists must not repeat per mailbox. It reads the mailbox
// and its per-mailbox signals itself; the list path prefetches those in bulk
// and calls assembleAccountStatus directly.
func (s *analyticsService) accountStatus(ctx context.Context, orgID, accountID uuid.UUID, rates map[uuid.UUID]models.WarmupPlacementRate, withPartners bool) (*models.EmailAccountStatus, *errx.Error) {
	// Get email account (org-scoped lookup)
	email, xerr := s.emailRepo.Get(ctx, orgID.String(), accountID.String())
	if xerr != nil {
		return nil, xerr
	}

	// Get daily usage
	usage, xerr := s.analyticsRepo.GetAccountDailyUsage(ctx, accountID, time.Now().UTC())
	if xerr != nil {
		return nil, xerr
	}

	errs := s.accountErrors(ctx, accountID)
	warmupHealth := s.buildWarmupHealth(ctx, accountID)

	inCampaign := false
	if s.campaignRepo != nil {
		if n, err := s.campaignRepo.CountActiveCampaignsForAccount(ctx, accountID); err == nil {
			inCampaign = n > 0
		}
	}

	return s.assembleAccountStatus(ctx, email, usage, errs, warmupHealth, inCampaign, rates, withPartners), nil
}

// accountErrors reads a mailbox's active errors in the API shape, never nil.
// Error surfacing is best-effort: a failed read is an empty list, not a 500 on
// the whole status.
func (s *analyticsService) accountErrors(ctx context.Context, accountID uuid.UUID) []models.AccountError {
	if s.emailAccountErrorsRepo == nil {
		return make([]models.AccountError, 0)
	}
	dbErrors, err := s.emailAccountErrorsRepo.GetByAccountID(ctx, accountID, true)
	if err != nil {
		return make([]models.AccountError, 0)
	}
	return toAccountErrors(dbErrors)
}

// toAccountErrors maps stored error rows into the API shape, never nil.
func toAccountErrors(dbErrors []repository.EmailAccountError) []models.AccountError {
	errs := make([]models.AccountError, 0, len(dbErrors))
	for _, e := range dbErrors {
		errs = append(errs, models.AccountError{
			ID:             e.ID,
			ErrorCode:      e.ErrorCode,
			Severity:       e.Severity,
			Title:          e.Title,
			Message:        e.Message,
			ActionRequired: e.ActionRequired,
			CreatedAt:      e.CreatedAt,
		})
	}
	return errs
}

// assembleAccountStatus turns a mailbox and its already-fetched signals (usage,
// errors, warmup-pool health, campaign membership, placement rates) into the
// status shape. Everything SQL-bound is passed in so both the single-mailbox
// read and the batched list build a status the same way; the few remaining
// reads here (ramp target, partner cap, last send failure, cold ramp,
// lifecycle) are conditional and best-effort, and on a list are bounded by the
// page.
func (s *analyticsService) assembleAccountStatus(ctx context.Context, email *models.Email, usage *models.AccountDailyUsage, errs []models.AccountError, warmupHealth *models.WarmupHealthInfo, inCampaign bool, rates map[uuid.UUID]models.WarmupPlacementRate, withPartners bool) *models.EmailAccountStatus {
	if errs == nil {
		errs = make([]models.AccountError, 0)
	}

	// Calculate health, then fold in warmup-pool reputation so the single
	// score the user sees reflects spam placement / complaints / throttling.
	health := calculateAccountHealth(email, errs)
	applyWarmupHealth(&health, warmupHealth)
	var placement *models.WarmupPlacementRate
	if r, ok := rates[email.ID]; ok {
		placement = &r
	}
	applyWarmupPlacement(&health, placement)

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
		if s.warmupRepo != nil && email.IsWarmingActive() {
			// A day of refusals is what an owner can still act on; older ones were fixed or are stale.
			if f, err := s.warmupRepo.LastWarmupSendFailure(ctx, email.ID, time.Now().Add(-24*time.Hour)); err == nil {
				warmupStatus.SendFailure = f
			}
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
		Errors:        errs,
		DailyUsage:    *usage,
		WarmupStatus:  warmupStatus,
		WarmupHealth:  warmupHealth,
		Placement:     placement,
		InCampaign:    inCampaign,
		ColdRamp:      coldRamp,
		SendLifecycle: lifecycle,
	}
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

// buildWarmupHealth looks up the mailbox's warmup-pool health, or the standing
// Warmbly Cloud reported, and maps it into the API shape. Returns nil when
// there is neither or the lookup fails; health surfacing must never break
// status. A mailbox is in at most one pool (migration 000097), so the single
// participant row resolves its reputation whatever pool it sits in.
func (s *analyticsService) buildWarmupHealth(ctx context.Context, accountID uuid.UUID) *models.WarmupHealthInfo {
	if s.warmupRepo == nil {
		return nil
	}
	participant, err := s.warmupRepo.GetParticipantHealthForAccount(ctx, accountID)
	if err != nil {
		participant = nil
	}
	return s.warmupHealthFromParticipant(ctx, accountID, participant)
}

// warmupHealthFromParticipant maps a resolved participant row into the API
// shape, adding the partner-diversity read-out, and falls back to the Warmbly
// Cloud standing for a mailbox in no pool. Shared by the single-mailbox read
// and the batched list, which resolves the participant row for the page in one
// query rather than once per mailbox.
func (s *analyticsService) warmupHealthFromParticipant(ctx context.Context, accountID uuid.UUID, participant *models.WarmupParticipantHealth) *models.WarmupHealthInfo {
	if s.warmupRepo == nil {
		return nil
	}
	if participant != nil {
		info := &models.WarmupHealthInfo{
			PoolType:     participant.PoolType,
			State:        string(participant.HealthState),
			Score:        participant.LastHealthScore,
			BlockedUntil: participant.BlockedUntil,
			EvaluatedAt:  participant.LastHealthEvaluatedAt,
		}
		if participant.LastHealthReason != nil {
			info.Reason = *participant.LastHealthReason
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
	// A mailbox Warmbly Cloud warms has no pool row here; its standing is the cloud's.
	if cloud, err := s.warmupRepo.GetCloudStanding(ctx, accountID); err == nil && cloud != nil {
		return cloud
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

// GetAllAccountStatuses walks the whole inventory through the batched, keyset
// page builder. It no longer stops at a silent 1000-row cap: it follows the
// cursor to the end, so a caller that needs every status still gets every
// status, in batched pages rather than a sequential per-mailbox walk.
func (s *analyticsService) GetAllAccountStatuses(ctx context.Context, orgID uuid.UUID) ([]models.EmailAccountStatus, *errx.Error) {
	statuses := make([]models.EmailAccountStatus, 0)
	cursor := ""
	for {
		page, xerr := s.GetAccountStatusesPage(ctx, orgID, nil, cursor, config.AccountStatusLimitMax)
		if xerr != nil {
			return nil, xerr
		}
		statuses = append(statuses, page.Data...)
		if page.Pagination.NextCursor == nil {
			break
		}
		cursor = *page.Pagination.NextCursor
	}
	return statuses, nil
}

// GetAccountStatusesPage builds one page of account statuses. emailIDs scopes
// the page to a visible set (the dashboard asks only for what it shows);
// otherwise the keyset cursor walks the whole inventory. Every per-mailbox read
// is batched across the page, and the shared placement rate is read once, so
// the page's cost is bounded by the page size, not by the total inventory.
func (s *analyticsService) GetAccountStatusesPage(ctx context.Context, orgID uuid.UUID, emailIDs []uuid.UUID, cursor string, limit int32) (*models.EmailAccountStatusesResult, *errx.Error) {
	// The opaque cursor decodes to the keyset position; an invalid token is a
	// 400, never a silently ignored full-inventory scan.
	cursorID, xerr := paging.DecodeCursor(cursor)
	if xerr != nil {
		return nil, xerr
	}

	// emailRepo.Search scopes on organization_id and, when emailIDs is set,
	// intersects with it — a foreign-org id selects no row, so a page can never
	// disclose another tenant's status.
	emailsResult, xerr := s.emailRepo.Search(ctx, orgID.String(), "", cursorID, nil, limit, emailIDs)
	if xerr != nil {
		return nil, xerr
	}

	emails := emailsResult.Data
	result := &models.EmailAccountStatusesResult{
		Data:       make([]models.EmailAccountStatus, 0, len(emails)),
		Pagination: emailsResult.Pagination,
	}
	if len(emails) == 0 {
		return result, nil
	}

	ids := make([]uuid.UUID, len(emails))
	for i := range emails {
		ids[i] = emails[i].ID
	}

	now := time.Now().UTC()
	rates := s.placementRates(ctx, orgID, nil)

	// Batched per-mailbox reads: usage is required and propagates its error
	// (never a silently incomplete list), the rest are best-effort read-outs.
	usage, xerr := s.analyticsRepo.GetAccountDailyUsageBatch(ctx, ids, now)
	if xerr != nil {
		return nil, xerr
	}

	errsByID := make(map[uuid.UUID][]models.AccountError, len(ids))
	if s.emailAccountErrorsRepo != nil {
		raw, xerr := s.emailAccountErrorsRepo.GetByAccountIDs(ctx, ids, true)
		if xerr != nil {
			return nil, xerr
		}
		for id, rows := range raw {
			errsByID[id] = toAccountErrors(rows)
		}
	}

	campByID := map[uuid.UUID]int{}
	if s.campaignRepo != nil {
		if m, err := s.campaignRepo.CountActiveCampaignsForAccounts(ctx, ids); err == nil {
			campByID = m
		}
	}

	healthByID := map[uuid.UUID]*models.WarmupParticipantHealth{}
	if s.warmupRepo != nil {
		if m, err := s.warmupRepo.GetParticipantHealthForAccounts(ctx, ids); err == nil {
			healthByID = m
		}
	}

	day := now.Format("2006-01-02")
	for i := range emails {
		email := emails[i]
		u := usage[email.ID]
		if u == nil {
			// Deleted between the page read and the usage read: report an empty
			// day rather than drop the row or fail the whole page.
			u = &models.AccountDailyUsage{Date: day, CampaignLimit: email.CampaignLimit, WarmupLimit: email.WarmupMax}
		}
		warmupHealth := s.warmupHealthFromParticipant(ctx, email.ID, healthByID[email.ID])
		status := s.assembleAccountStatus(ctx, &email, u, errsByID[email.ID], warmupHealth, campByID[email.ID] > 0, rates, false)
		result.Data = append(result.Data, *status)
	}

	return result, nil
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
	if email.Warmup == nil || email.CampaignLimit <= 0 {
		return nil
	}
	state := repository.ColdRampState{}
	if s.warmupRepo != nil {
		states, err := s.warmupRepo.ColdRampStateForAccounts(ctx,
			[]uuid.UUID{email.ID}, time.Now().Add(-warmupramp.LookbackWindow))
		if err == nil {
			state = states[email.ID]
		}
	}
	state = state.WithKnownWarmup(email.Warmup)
	info := warmupramp.Notice(state.WarmupStartedAt, state.ColdRampStartedAt, state.Placements, email.CampaignLimit, time.Now(), state.ConfirmedReplies)
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
