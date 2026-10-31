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
	GetAccountStatusDetailsPage(ctx context.Context, orgID uuid.UUID, emailIDs []uuid.UUID, cursor string, limit int32) (*models.EmailAccountStatusesResult, *errx.Error)

	// Usage overview
	GetUsageOverview(ctx context.Context, orgID, userID uuid.UUID, period string) (*models.UsageOverview, *errx.Error)

	// Dashboard analytics
	// GetDashboardAnalytics narrows the campaign-derived sections to filter
	// when it names anything; account health stays workspace-wide.
	GetDashboardAnalytics(ctx context.Context, orgID uuid.UUID, period string, filter models.DashboardFilter) (*models.DashboardAnalytics, *errx.Error)
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
	var participant *models.WarmupParticipantHealth
	if s.warmupRepo != nil {
		if p, err := s.warmupRepo.GetParticipantHealthForAccount(ctx, accountID); err == nil {
			participant = p
		}
	}

	inCampaign := false
	if s.campaignRepo != nil {
		if n, err := s.campaignRepo.CountActiveCampaignsForAccount(ctx, accountID); err == nil {
			inCampaign = n > 0
		}
	}

	reads := s.loadStatusReads(ctx, []models.Email{*email}, map[uuid.UUID]*models.WarmupParticipantHealth{accountID: participant})
	warmupHealth := s.warmupHealthFromParticipant(accountID, participant, reads)
	return s.assembleAccountStatus(ctx, email, usage, errs, warmupHealth, inCampaign, rates, withPartners, reads), nil
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
	type errorKey struct {
		code, message, action string
		operation             models.MailTransportOperation
	}
	seen := make(map[errorKey]int, len(dbErrors))
	for _, e := range dbErrors {
		message := e.Message
		if e.UserMessage != nil && *e.UserMessage != "" {
			message = *e.UserMessage
		}
		action := ""
		if e.ActionRequired != nil {
			action = *e.ActionRequired
		}
		key := errorKey{e.ErrorCode, message, action, e.Operation.Conservative()}
		record := models.AccountError{
			ID:             e.ID,
			ErrorCode:      e.ErrorCode,
			Severity:       e.Severity,
			Title:          e.Title,
			Message:        message,
			ActionRequired: e.ActionRequired,
			CreatedAt:      e.CreatedAt,
			Operation:      key.operation,
		}
		if index, ok := seen[key]; ok {
			previous := errs[index]
			if record.CreatedAt.After(previous.CreatedAt) {
				errs[index] = record
			}
			if accountErrorPenalty(record.Severity) > accountErrorPenalty(previous.Severity) {
				errs[index].Severity = record.Severity
			} else {
				errs[index].Severity = previous.Severity
			}
			continue
		}
		seen[key] = len(errs)
		errs = append(errs, record)
	}
	return errs
}

func connectionProviderName(provider string) string {
	switch provider {
	case "gmail":
		return "Gmail"
	case "outlook":
		return "Microsoft Outlook"
	default:
		return "Mail server"
	}
}

func describeAccountErrors(email *models.Email, errs []models.AccountError) {
	for i := range errs {
		e := &errs[i]
		if e.ErrorCode != string(errx.MailErrorCodeServerUnreachable) && e.ErrorCode != string(errx.MailErrorCodeConnectionLost) {
			continue
		}
		operation := "connection attempt failed"
		switch e.Operation {
		case models.MailTransportSend:
			operation = "sending connection failed"
		case models.MailTransportSync:
			operation = "sync connection failed"
		}
		e.Title = connectionProviderName(email.Provider) + " " + operation
		action := "Warmbly retries automatically while this mailbox is switched on. This is the last reported failure, not proof that it is still occurring. A successful retry for the affected operation confirms recovery. Reconnecting is only needed if a sign-in error is also reported."
		if email.Status != "active" {
			action = "This mailbox is switched off, so automatic retries are paused. Check any sign-in errors, then switch the mailbox back on to retry."
		}
		e.ActionRequired = &action
	}
}

// assembleAccountStatus turns a mailbox and its already-fetched signals into
// the status shape. Everything SQL-bound arrives in reads, so the single
// mailbox and the batched list build a status the same way and a list costs no
// query per row; only the drawer's partner cap reads here.
func (s *analyticsService) assembleAccountStatus(ctx context.Context, email *models.Email, usage *models.AccountDailyUsage, errs []models.AccountError, warmupHealth *models.WarmupHealthInfo, inCampaign bool, rates map[uuid.UUID]models.WarmupPlacementRate, withPartners bool, reads *statusReads) *models.EmailAccountStatus {
	if errs == nil {
		errs = make([]models.AccountError, 0)
	}
	describeAccountErrors(email, errs)
	hold, held := reads.sendHolds[email.ID]
	if held {
		for i := range errs {
			if errs[i].Operation == models.MailTransportSync || (errs[i].ErrorCode != string(errx.MailErrorCodeServerUnreachable) && errs[i].ErrorCode != string(errx.MailErrorCodeConnectionLost)) {
				continue
			}
			action := sendRecoveryHoldMessage(hold)
			errs[i].ActionRequired = &action
		}
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
	if held {
		health.Status = "error"
		health.Score = min(health.Score, 50)
		health.Issues = append(health.Issues, sendRecoveryHoldMessage(hold))
	} else if reads.sendHoldsUnavailable {
		if health.Status == "healthy" {
			health.Status = "warning"
		}
		health.Issues = append(health.Issues, "Sending status is unavailable; mailbox send safety holds could not be checked.")
	}

	// Build warmup status if warmup has ever been enabled (active or paused).
	var warmupStatus *models.WarmupStatusInfo
	if email.Warmup != nil {
		target, hold := s.warmupTargetAndHold(ctx, email, warmupHealthState(warmupHealth), inCampaign, reads)
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
		// A day of refusals is what an owner can still act on; older ones were fixed or are stale.
		warmupStatus.SendFailure = reads.failures[email.ID]
		if at, ok := reads.nextWarmup[email.ID]; ok {
			warmupStatus.NextSendAt = &at
		}
	}

	coldRamp := coldRampNotice(email, reads.coldRamp, reads.now)
	holdReason := ""
	if held {
		holdReason = hold.Reason
	}
	lifecycle := sendLifecycleNotice(reads.lifecycles, email.ID)

	return &models.EmailAccountStatus{
		ID:                 email.ID,
		Email:              email.Email,
		Provider:           email.Provider,
		Status:             email.Status,
		LastSyncedAt:       &email.LastSyncedAt,
		Health:             health,
		Errors:             errs,
		DailyUsage:         *usage,
		WarmupStatus:       warmupStatus,
		WarmupHealth:       warmupHealth,
		Placement:          placement,
		InCampaign:         inCampaign,
		ColdRamp:           coldRamp,
		SendLifecycle:      lifecycle,
		SendRecoveryReason: holdReason,
	}
}

func sendRecoveryHoldMessage(hold repository.SendRecoveryHold) string {
	switch hold.Reason {
	case "unknown":
		if hold.Campaign {
			return "Warmbly is checking an unconfirmed campaign send automatically. When safe, it isolates that email so this mailbox can continue sending to other contacts. The affected contact waits for confirmation, and the uncertain email is never retried. No action is needed."
		}
		if hold.Warmup {
			return "Sending is paused after an unconfirmed warmup send. Warmbly checks it automatically and, when safe, retires that message so new sends can resume. The uncertain message is never retried."
		}
		return "Sending is briefly paused while Warmbly confirms whether an earlier email went out, so nothing is sent twice. This clears on its own, usually within 30 minutes. No action is needed."
	case "authentication":
		return "Sending is paused because the mail server refused this mailbox's login when it last sent. Reconnect the mailbox (re-authorize it, or enter its current password or app password); once the server accepts the login, sending resumes unless another send safety hold is still pending."
	default:
		return "Sending is paused by a send safety hold. Resolve the held send before this mailbox can send again."
	}
}

// warmupHealthState reads the band off the API shape, defaulting to healthy
// for a mailbox in no pool — the same default resolveHealthState uses.
// Unknown cloud evidence is no band at all, so it does not dampen the ramp.
func warmupHealthState(h *models.WarmupHealthInfo) models.WarmupHealthState {
	if h.CloudEvidenceUnavailable() {
		return ""
	}
	if h == nil || h.State == "" {
		return models.WarmupHealthHealthy
	}
	return models.WarmupHealthState(h.State)
}

// warmupDiversityWindow matches the selector's domain-history window.
const warmupDiversityWindow = 7 * 24 * time.Hour

// warmupHealthFromParticipant maps a resolved participant row into the API
// shape, adding the partner-diversity read-out, and falls back to the Warmbly
// Cloud standing for a mailbox in no pool. Returns nil when there is neither;
// health surfacing must never break status. A mailbox is in at most one pool
// (migration 000097), so the single participant row resolves its reputation.
func (s *analyticsService) warmupHealthFromParticipant(accountID uuid.UUID, participant *models.WarmupParticipantHealth, reads *statusReads) *models.WarmupHealthInfo {
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
		if d, ok := reads.diversity[accountID]; ok {
			info.PartnerMailboxes7d = d.Mailboxes
			info.PartnerDomains7d = d.Domains
			info.PartnerOrganizations7d = d.Organizations
			info.Received7d = d.Received
			info.Senders7d = d.Senders
		}
		return info
	}
	// A mailbox Warmbly Cloud warms has no pool row here; its standing is the cloud's.
	return reads.cloud[accountID]
}

// applyWarmupHealth keeps reduced volume a warning and pool exclusion an error.
func applyWarmupHealth(health *models.AccountHealth, wh *models.WarmupHealthInfo) {
	if wh == nil {
		return
	}
	// Sends wait for a fresh report, but a missing report says nothing about reputation.
	if wh.CloudEvidenceUnavailable() {
		if health.Status == "healthy" {
			health.Status = "warning"
		}
		health.Issues = append(health.Issues, "Warmbly Cloud status unavailable")
		return
	}

	var penalty int
	var issue string
	switch models.WarmupHealthState(wh.State) {
	case models.WarmupHealthWatch:
		issue = "Warmup reputation needs watching"
	case models.WarmupHealthThrottled:
		issue = "Warmup sending reduced"
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
	case models.WarmupHealthWatch, models.WarmupHealthThrottled:
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
	return s.accountStatusesPage(ctx, orgID, emailIDs, cursor, limit)
}

func (s *analyticsService) accountStatusesPage(ctx context.Context, orgID uuid.UUID, emailIDs []uuid.UUID, cursor string, limit int32) (*models.EmailAccountStatusesResult, *errx.Error) {
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

	reads := s.loadStatusReads(ctx, emails, healthByID)
	if ctx.Err() != nil {
		return nil, errx.InternalError()
	}

	day := now.Format("2006-01-02")
	result.Data = make([]models.EmailAccountStatus, len(emails))
	for i := range emails {
		email := &emails[i]
		u := usage[email.ID]
		if u == nil {
			// Deletion between the page and usage reads leaves an empty day, not a dropped row.
			u = &models.AccountDailyUsage{Date: day, CampaignLimit: email.CampaignLimit, WarmupLimit: email.WarmupMax}
		}
		warmupHealth := s.warmupHealthFromParticipant(email.ID, healthByID[email.ID], reads)
		result.Data[i] = *s.assembleAccountStatus(ctx, email, u, errsByID[email.ID], warmupHealth, campByID[email.ID] > 0, rates, false, reads)
	}
	return result, nil
}

// GetAccountStatusDetailsPage retains the detail target without scanning partner cohorts per mailbox.
func (s *analyticsService) GetAccountStatusDetailsPage(ctx context.Context, orgID uuid.UUID, emailIDs []uuid.UUID, cursor string, limit int32) (*models.EmailAccountStatusesResult, *errx.Error) {
	result, xerr := s.accountStatusesPage(ctx, orgID, emailIDs, cursor, limit)
	if xerr != nil || s.warmupRepo == nil {
		return result, xerr
	}
	byPool := make(map[string][]uuid.UUID)
	for _, status := range result.Data {
		if w, h := status.WarmupStatus, status.WarmupHealth; w != nil && !w.Paused && w.TargetVolume > 0 && h != nil && h.PoolType != "" {
			byPool[h.PoolType] = append(byPool[h.PoolType], status.ID)
		}
	}
	for pool, ids := range byPool {
		counts, err := s.warmupRepo.WarmupPartnerCounts(ctx, pool, ids)
		if err != nil {
			return nil, errx.InternalError()
		}
		for i := range result.Data {
			status := &result.Data[i]
			if reachable, ok := counts[status.ID]; ok && status.WarmupStatus != nil && reachable < status.WarmupStatus.TargetVolume {
				w := status.WarmupStatus
				w.PartnerLimit = &models.WarmupPartnerLimit{Reachable: reachable, RampTarget: w.TargetVolume}
				w.TargetVolume = max(reachable, status.DailyUsage.WarmupSent)
			}
		}
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

	penalties := make(map[string]int)
	for _, e := range errors {
		penalty := 0
		if e.Severity == "CRITICAL" {
			health.Status = "error"
			penalty = 30
		} else if e.Severity == "WARNING" {
			if health.Status == "healthy" {
				health.Status = "warning"
			}
			penalty = 10
		}
		key := e.ErrorCode
		if key == "" {
			key = e.Title
		}
		previous := penalties[key]
		if penalty <= previous {
			continue
		}
		penalties[key] = penalty
		health.Score -= penalty - previous
		if previous == 0 {
			title := e.Title
			if e.ErrorCode == string(errx.MailErrorCodeServerUnreachable) || e.ErrorCode == string(errx.MailErrorCodeConnectionLost) {
				title = connectionProviderName(email.Provider) + " connection attempt failed"
				if e.ErrorCode == string(errx.MailErrorCodeConnectionLost) {
					title = connectionProviderName(email.Provider) + " connection was interrupted"
				}
			}
			health.Issues = append(health.Issues, title)
		}
	}

	// Ensure score doesn't go below 0
	if health.Score < 0 {
		health.Score = 0
	}

	return health
}

func accountErrorPenalty(severity string) int {
	switch severity {
	case "CRITICAL":
		return 30
	case "WARNING":
		return 10
	default:
		return 0
	}
}

// warmupTargetAndHold reports the target the SCHEDULER will act on, plus what
// is holding it down. It runs the shared warmupramp policy rather than its own
// arithmetic: a drawer saying "target 25" for a mailbox sending 18 is worse
// than no number.
func (s *analyticsService) warmupTargetAndHold(ctx context.Context, email *models.Email, health models.WarmupHealthState, inCampaign bool, reads *statusReads) (int, *models.WarmupRampHold) {
	if email.Warmup == nil {
		return 0, nil
	}
	var reader warmupramp.Reader
	if reads.ramp != nil {
		reader = reads.ramp
	} else if s.warmupRepo != nil {
		reader = s.warmupRepo
	}
	plan := warmupramp.Resolve(ctx, reader, warmupramp.Input{
		AccountID:       email.ID,
		WarmupStart:     *email.Warmup,
		ActivelyWarming: email.IsWarmingActive(),
		Base:            email.WarmupBase,
		Increase:        email.WarmupIncrease,
		Max:             email.WarmupMax,
		InCampaign:      inCampaign,
		Health:          health,
		Now:             reads.now,
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

func (s *analyticsService) GetDashboardAnalytics(ctx context.Context, orgID uuid.UUID, period string, filter models.DashboardFilter) (*models.DashboardAnalytics, *errx.Error) {
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

	// The one place a filter becomes a campaign set, so every section reads the same one.
	var view *models.DashboardScope
	var scope *models.CampaignScope
	if !filter.Empty() {
		var xerr *errx.Error
		view, scope, xerr = s.analyticsRepo.ResolveDashboardScope(ctx, orgID, filter)
		if xerr != nil {
			return nil, xerr
		}
	}
	if filter.AllowedCampaigns != nil {
		scope = withinCampaigns(scope, filter.AllowedCampaigns)
		if view != nil {
			view.CampaignCount = len(scope.CampaignIDs)
		}
	}

	// Get overall stats
	overallStats, xerr := s.analyticsRepo.GetDashboardOverallStats(ctx, orgID, from, to, scope, filter.AllowedMailboxes)
	if xerr != nil {
		return nil, xerr
	}

	// Get recent activity
	recentActivity, xerr := s.analyticsRepo.GetRecentActivity(ctx, orgID, 20, scope)
	if xerr != nil {
		recentActivity = make([]models.RecentActivityItem, 0)
	}

	// Get top campaigns
	topCampaigns, xerr := s.analyticsRepo.GetTopCampaigns(ctx, orgID, from, to, 5, "emails_sent", scope)
	if xerr != nil {
		topCampaigns = make([]models.TopCampaignStats, 0)
	}

	// Get account health summary
	accountHealth, xerr := s.analyticsRepo.GetAccountHealthSummary(ctx, orgID, filter.AllowedMailboxes)
	if xerr != nil {
		accountHealth = &models.AccountHealthSummary{}
	}

	// Get daily trend
	dailyTrend, xerr := s.analyticsRepo.GetDashboardDailyTrend(ctx, orgID, from, to, scope)
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
		Scope:          view,
	}, nil
}

// withinCampaigns narrows scope to allowed; a nil scope becomes allowed itself.
func withinCampaigns(scope *models.CampaignScope, allowed []uuid.UUID) *models.CampaignScope {
	if scope == nil {
		return &models.CampaignScope{CampaignIDs: append([]uuid.UUID{}, allowed...)}
	}
	keep := make(map[uuid.UUID]bool, len(allowed))
	for _, id := range allowed {
		keep[id] = true
	}
	out := &models.CampaignScope{CampaignIDs: []uuid.UUID{}}
	for _, id := range scope.CampaignIDs {
		if keep[id] {
			out.CampaignIDs = append(out.CampaignIDs, id)
		}
	}
	return out
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

// coldRampNotice explains a graduation ceiling holding this mailbox below its
// own cold cap. Nil when nothing is holding it, so the drawer stays quiet. A
// failed or missing read is a zero state, which can only make it stricter.
func coldRampNotice(email *models.Email, states map[uuid.UUID]repository.ColdRampState, now time.Time) *models.ColdRampInfo {
	if email.Warmup == nil || email.CampaignLimit <= 0 {
		return nil
	}
	state := states[email.ID].WithKnownWarmup(email.Warmup)
	info := warmupramp.Notice(state.WarmupStartedAt, state.ColdRampStartedAt, state.Placements, email.CampaignLimit, now, state.ConfirmedReplies)
	if info == nil || info.Ceiling >= email.CampaignLimit {
		return nil
	}
	return info
}

// sendLifecycleNotice reports the mailbox's cold-rotation state, but only when
// it is not active: an active mailbox is the normal case and needs no notice.
func sendLifecycleNotice(states map[uuid.UUID]models.SendLifecycleState, accountID uuid.UUID) *models.SendLifecycleState {
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
