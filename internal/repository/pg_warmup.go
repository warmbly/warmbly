package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// WarmupPool represents a warmup pool
type WarmupPool struct {
	ID              uuid.UUID
	PoolType        string
	Name            string
	Description     string
	MaxParticipants *int
	CreatedAt       time.Time
}

// WarmupPoolParticipant represents a participant in a warmup pool
type WarmupPoolParticipant struct {
	PoolID                uuid.UUID
	EmailAccountID        uuid.UUID
	ParticipantRole       string
	JoinedAt              time.Time
	BlockedAt             *time.Time
	BlockedUntil          *time.Time
	BlockedReason         *string
	SpamScore             int
	HealthState           models.WarmupHealthState
	LastHealthScore       float64
	LastHealthReason      *string
	LastHealthEvaluatedAt *time.Time
}

// SpamReport represents a spam report
type SpamReport struct {
	ID                uuid.UUID
	ReporterAccountID uuid.UUID
	ReportedAccountID uuid.UUID
	MessageID         string
	ReportType        string
	// ContentSource is the content cohort ("static"/"ai") of the warmup send
	// that landed in spam, denormalised here from the warmup token so the A/B
	// harness can aggregate spam-placement rate by cohort.
	ContentSource string
	// RecipientProvider / RecipientDomain record where the message was filtered
	// into spam (the recipient mailbox's provider, e.g. "google"/"smtp_imap",
	// and domain, e.g. "outlook.com") so placement can be segmented per provider
	// instead of one flat rate. Empty when the dimension isn't known.
	RecipientProvider string
	RecipientDomain   string
	CreatedAt         time.Time
}

// WarmupStatistic represents daily warmup statistics
type WarmupStatistic struct {
	EmailAccountID uuid.UUID
	Date           time.Time
	EmailsSent     int
	EmailsReplied  int
	TargetVolume   int
}

// WarmupReplyCandidate describes a previously sent warmup message that can be replied to.
type WarmupReplyCandidate struct {
	MessageID         string
	Subject           string
	ThreadID          *string
	ConversationTheme string
}

// WarmupReceived records a verified warmup email delivered to a participant
// mailbox, so a later deletion/flag event (which carries only the internal
// message id) can be matched back to warmup and to the sender.
type WarmupReceived struct {
	EmailAccountID  uuid.UUID
	InternalID      uuid.UUID
	MessageID       string
	SenderAccountID uuid.UUID
	CreatedAt       time.Time
}

// WarmupRepository defines methods for warmup data access
type WarmupRepository interface {
	// Pool management
	GetPoolByType(ctx context.Context, poolType string) (*WarmupPool, error)
	GetPoolParticipants(ctx context.Context, poolType string, excludeBlocked bool) ([]uuid.UUID, error)
	GetPoolRecipientParticipants(ctx context.Context, poolType string, excludeBlocked bool) ([]uuid.UUID, error)
	JoinPool(ctx context.Context, poolID, accountID uuid.UUID) error
	SetParticipantRole(ctx context.Context, poolID, accountID uuid.UUID, role string) error
	LeavePool(ctx context.Context, poolID, accountID uuid.UUID) error
	BlockFromPool(ctx context.Context, accountID uuid.UUID, reason string) error
	// GetHealthState returns the WORST current warmup health state across the
	// account's pool memberships plus its blocked_until, so non-warmup callers
	// (e.g. the campaign scheduler) can gate cold sends on warmup health without
	// needing a pool type. Returns ("healthy", nil) when the account is in no pool.
	GetHealthState(ctx context.Context, accountID uuid.UUID) (models.WarmupHealthState, *time.Time, error)
	UnblockFromPool(ctx context.Context, accountID uuid.UUID) error
	IsInPool(ctx context.Context, accountID uuid.UUID, poolType string) (bool, error)
	GetParticipantHealth(ctx context.Context, accountID uuid.UUID, poolType string) (*models.WarmupParticipantHealth, error)
	UpdateParticipantHealth(ctx context.Context, accountID uuid.UUID, state models.WarmupHealthState, blockedUntil *time.Time, reason string, score float64) error
	CountSpamReportsSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error)
	CountUserComplaintsSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error)
	CountSpamPlacementsSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error)
	SumWarmupSentSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error)
	CountDeliverabilityEventsByAccount(ctx context.Context, accountID uuid.UUID, eventType string, since time.Time) (int, error)
	CountDeliveredByAccount(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error)

	// Health sweep
	GetAllParticipantAccountIDs(ctx context.Context) ([]uuid.UUID, error)
	GetPoolHealthCounts(ctx context.Context) (map[string]int, float64, error)

	// Spam tracking
	RecordSpamReport(ctx context.Context, report *SpamReport) (bool, error)
	GetSpamScore(ctx context.Context, accountID uuid.UUID) (int, error)
	IncrementSpamScore(ctx context.Context, accountID uuid.UUID, amount int) (int, error)
	ResetSpamScore(ctx context.Context, accountID uuid.UUID) error

	// Statistics
	IncrementDailyCount(ctx context.Context, accountID uuid.UUID, date time.Time) error
	IncrementReplyCount(ctx context.Context, accountID uuid.UUID, date time.Time) error
	GetWarmupStatistics(ctx context.Context, accountID uuid.UUID, from, to time.Time) ([]WarmupStatistic, error)
	GetOrCreateDailyStats(ctx context.Context, accountID uuid.UUID, date time.Time, targetVolume int) (*WarmupStatistic, error)

	// Pool-wide placement analytics (admin overview)
	PoolSpamPlacementRate(ctx context.Context, since time.Time) (float64, error)
	PoolSpamPlacementsByProvider(ctx context.Context, since time.Time) (map[string]int, error)

	// Warmup token management
	CreateWarmupToken(ctx context.Context, token *models.WarmupToken) error
	GetWarmupToken(ctx context.Context, tokenID uuid.UUID) (*models.WarmupToken, error)
	FindWarmupToken(ctx context.Context, tokenID uuid.UUID) (*models.WarmupToken, error)
	ConsumeWarmupToken(ctx context.Context, tokenID uuid.UUID) error
	RecordInvalidTokenAttempt(ctx context.Context, accountID uuid.UUID, attemptedToken string) error
	CountRecentInvalidAttempts(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error)

	// Warmup conversation support
	GetRecentlyUsedPartners(ctx context.Context, accountID uuid.UUID, since time.Time) ([]uuid.UUID, error)
	GetRecentPartnerCounts(ctx context.Context, accountID uuid.UUID, since time.Time) (map[uuid.UUID]int, error)
	GetLatestReplyCandidate(ctx context.Context, senderAccountID, recipientAccountID uuid.UUID) (*WarmupReplyCandidate, error)

	// Partner diversity support
	GetPoolParticipantDomains(ctx context.Context, poolType string, excludeBlocked bool) (map[uuid.UUID]string, error)
	GetPoolParticipantEmails(ctx context.Context, poolType string, excludeBlocked bool) (map[uuid.UUID]string, error)
	CountEligibleRecipients(ctx context.Context, poolType string, excludeAccountID uuid.UUID) (int, error)
	GetRecentPartnerDomainCounts(ctx context.Context, accountID uuid.UUID, since time.Time) (map[string]int, error)

	// Tampering protection: track delivered warmup mail so a later deletion or
	// spam-flag can be attributed, and count "harm" events per mailbox.
	RecordWarmupReceived(ctx context.Context, accountID, internalID uuid.UUID, messageID string, senderAccountID uuid.UUID) error
	GetWarmupReceived(ctx context.Context, accountID, internalID uuid.UUID) (*WarmupReceived, error)
	RecordWarmupTampering(ctx context.Context, accountID uuid.UUID, messageID, kind string) (bool, error)
	CountWarmupTamperingSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error)

	// Appeals (user-facing submission; admin review lives in the admin repo).
	CreateWarmupAppeal(ctx context.Context, accountID, userID uuid.UUID, reason string) (uuid.UUID, error)
	HasPendingWarmupAppeal(ctx context.Context, accountID uuid.UUID) (bool, error)
}

type warmupRepository struct {
	db *pgxpool.Pool
}

// NewWarmupRepository creates a new warmup repository
func NewWarmupRepository(db *pgxpool.Pool) WarmupRepository {
	return &warmupRepository{db: db}
}

// GetPoolByType retrieves a pool by type
func (r *warmupRepository) GetPoolByType(ctx context.Context, poolType string) (*WarmupPool, error) {
	query := `
		SELECT id, pool_type, name, description, max_participants, created_at
		FROM warmup_pools
		WHERE pool_type = $1
		LIMIT 1
	`

	pool := &WarmupPool{}
	err := r.db.QueryRow(ctx, query, poolType).Scan(
		&pool.ID,
		&pool.PoolType,
		&pool.Name,
		&pool.Description,
		&pool.MaxParticipants,
		&pool.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return pool, err
}

// GetPoolParticipants retrieves all participant account IDs from a pool
func (r *warmupRepository) GetPoolParticipants(ctx context.Context, poolType string, excludeBlocked bool) ([]uuid.UUID, error) {
	query := `
		SELECT wpp.email_account_id
		FROM warmup_pool_participants wpp
		JOIN warmup_pools wp ON wpp.pool_id = wp.id
		JOIN email_accounts ea ON ea.id = wpp.email_account_id
		WHERE wp.pool_type = $1
		  AND wpp.participant_role = 'sender_receiver'
		  AND ea.status = 'active'
	`

	if excludeBlocked {
		query += `
		 AND (
		  wpp.health_state IN ('healthy', 'watch', 'throttled')
		  OR (
		   wpp.health_state IN ('quarantined', 'blocked')
		   AND wpp.blocked_until IS NOT NULL
		   AND wpp.blocked_until <= NOW()
		  )
		 )
		 AND (
		  wpp.blocked_at IS NULL
		  OR (wpp.blocked_until IS NOT NULL AND wpp.blocked_until <= NOW())
		 )
		`
	}

	rows, err := r.db.Query(ctx, query, poolType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accountIDs []uuid.UUID
	for rows.Next() {
		var accountID uuid.UUID
		if err := rows.Scan(&accountID); err != nil {
			return nil, err
		}
		accountIDs = append(accountIDs, accountID)
	}

	return accountIDs, rows.Err()
}

// GetPoolRecipientParticipants retrieves participant account IDs that can
// receive warmup mail. Recipient-only rows increase safe inbound capacity
// without scheduling outbound warmup sends from those mailboxes.
func (r *warmupRepository) GetPoolRecipientParticipants(ctx context.Context, poolType string, excludeBlocked bool) ([]uuid.UUID, error) {
	query := `
		SELECT wpp.email_account_id
		FROM warmup_pool_participants wpp
		JOIN warmup_pools wp ON wpp.pool_id = wp.id
		JOIN email_accounts ea ON ea.id = wpp.email_account_id
		WHERE wp.pool_type = $1
		  AND wpp.participant_role IN ('sender_receiver', 'recipient_only')
		  AND ea.status = 'active'
	`

	if excludeBlocked {
		query += `
		 AND (
		  wpp.health_state IN ('healthy', 'watch', 'throttled')
		  OR (
		   wpp.health_state IN ('quarantined', 'blocked')
		   AND wpp.blocked_until IS NOT NULL
		   AND wpp.blocked_until <= NOW()
		  )
		 )
		 AND (
		  wpp.blocked_at IS NULL
		  OR (wpp.blocked_until IS NOT NULL AND wpp.blocked_until <= NOW())
		 )
		`
	}

	rows, err := r.db.Query(ctx, query, poolType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accountIDs []uuid.UUID
	for rows.Next() {
		var accountID uuid.UUID
		if err := rows.Scan(&accountID); err != nil {
			return nil, err
		}
		accountIDs = append(accountIDs, accountID)
	}

	return accountIDs, rows.Err()
}

// JoinPool adds an account to a warmup pool
func (r *warmupRepository) JoinPool(ctx context.Context, poolID, accountID uuid.UUID) error {
	query := `
		INSERT INTO warmup_pool_participants (pool_id, email_account_id, joined_at, spam_score, participant_role)
		VALUES ($1, $2, NOW(), 0, 'sender_receiver')
		ON CONFLICT (pool_id, email_account_id) DO UPDATE
		SET joined_at = warmup_pool_participants.joined_at
	`

	_, err := r.db.Exec(ctx, query, poolID, accountID)
	return err
}

func (r *warmupRepository) SetParticipantRole(ctx context.Context, poolID, accountID uuid.UUID, role string) error {
	query := `
		UPDATE warmup_pool_participants
		SET participant_role = $1
		WHERE pool_id = $2 AND email_account_id = $3
	`
	_, err := r.db.Exec(ctx, query, role, poolID, accountID)
	return err
}

// LeavePool removes an account from a warmup pool
func (r *warmupRepository) LeavePool(ctx context.Context, poolID, accountID uuid.UUID) error {
	query := `
		DELETE FROM warmup_pool_participants
		WHERE pool_id = $1 AND email_account_id = $2
	`

	_, err := r.db.Exec(ctx, query, poolID, accountID)
	return err
}

// BlockFromPool blocks an account from all warmup pools
func (r *warmupRepository) BlockFromPool(ctx context.Context, accountID uuid.UUID, reason string) error {
	query := `
		UPDATE warmup_pool_participants
		SET blocked_at = NOW(),
		    blocked_until = NULL,
		    blocked_reason = $1,
		    health_state = 'blocked',
		    last_health_reason = $1,
		    last_health_evaluated_at = NOW()
		WHERE email_account_id = $2
		  AND blocked_at IS NULL
	`

	_, err := r.db.Exec(ctx, query, reason, accountID)
	return err
}

// GetHealthState returns the worst current health state across the account's
// pool memberships and that row's blocked_until. Worst-wins so a mailbox that's
// blocked in any pool is treated as blocked for cold-send gating.
func (r *warmupRepository) GetHealthState(ctx context.Context, accountID uuid.UUID) (models.WarmupHealthState, *time.Time, error) {
	query := `
		SELECT health_state, blocked_until
		FROM warmup_pool_participants
		WHERE email_account_id = $1
		ORDER BY CASE health_state
			WHEN 'blocked' THEN 5
			WHEN 'quarantined' THEN 4
			WHEN 'throttled' THEN 3
			WHEN 'watch' THEN 2
			WHEN 'healthy' THEN 1
			ELSE 0
		END DESC
		LIMIT 1
	`
	var state string
	var blockedUntil *time.Time
	err := r.db.QueryRow(ctx, query, accountID).Scan(&state, &blockedUntil)
	if err == sql.ErrNoRows {
		return models.WarmupHealthHealthy, nil, nil
	}
	if err != nil {
		return models.WarmupHealthHealthy, nil, err
	}
	return models.WarmupHealthState(state), blockedUntil, nil
}

// UnblockFromPool unblocks an account from all warmup pools
func (r *warmupRepository) UnblockFromPool(ctx context.Context, accountID uuid.UUID) error {
	query := `
		UPDATE warmup_pool_participants
		SET blocked_at = NULL,
		    blocked_until = NULL,
		    blocked_reason = NULL,
		    health_state = 'healthy',
		    last_health_reason = NULL,
		    last_health_score = 0,
		    last_health_evaluated_at = NOW()
		WHERE email_account_id = $1
	`

	_, err := r.db.Exec(ctx, query, accountID)
	return err
}

// IsInPool checks if an account is in a specific pool type
func (r *warmupRepository) IsInPool(ctx context.Context, accountID uuid.UUID, poolType string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1
			FROM warmup_pool_participants wpp
			JOIN warmup_pools wp ON wpp.pool_id = wp.id
			WHERE wpp.email_account_id = $1
			  AND wp.pool_type = $2
			  AND (
			   wpp.health_state IN ('healthy', 'watch')
			   OR (
			    wpp.health_state IN ('quarantined', 'blocked')
			    AND wpp.blocked_until IS NOT NULL
			    AND wpp.blocked_until <= NOW()
			   )
			  )
			  AND (
			   wpp.blocked_at IS NULL
			   OR (wpp.blocked_until IS NOT NULL AND wpp.blocked_until <= NOW())
			  )
		)
	`

	var exists bool
	err := r.db.QueryRow(ctx, query, accountID, poolType).Scan(&exists)
	return exists, err
}

// RecordSpamReport records a spam report
func (r *warmupRepository) RecordSpamReport(ctx context.Context, report *SpamReport) (bool, error) {
	query := `
		INSERT INTO warmup_spam_reports (id, reporter_account_id, reported_account_id, message_id, report_type, content_source, recipient_provider, recipient_domain, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (reporter_account_id, message_id) DO NOTHING
	`

	cmd, err := r.db.Exec(ctx, query,
		report.ID,
		report.ReporterAccountID,
		report.ReportedAccountID,
		report.MessageID,
		report.ReportType,
		report.ContentSource,
		report.RecipientProvider,
		report.RecipientDomain,
	)

	if err != nil {
		return false, err
	}

	return cmd.RowsAffected() > 0, nil
}

// GetSpamScore retrieves the spam score for an account
func (r *warmupRepository) GetSpamScore(ctx context.Context, accountID uuid.UUID) (int, error) {
	query := `
		SELECT COALESCE(SUM(spam_score), 0)
		FROM warmup_pool_participants
		WHERE email_account_id = $1
	`

	var score int
	err := r.db.QueryRow(ctx, query, accountID).Scan(&score)
	return score, err
}

func (r *warmupRepository) GetParticipantHealth(ctx context.Context, accountID uuid.UUID, poolType string) (*models.WarmupParticipantHealth, error) {
	query := `
		SELECT
			wpp.pool_id,
			wp.pool_type,
			wpp.email_account_id,
			wpp.joined_at,
			wpp.blocked_at,
			wpp.blocked_until,
			wpp.blocked_reason,
			wpp.spam_score,
			wpp.health_state,
			wpp.last_health_score,
			wpp.last_health_reason,
			wpp.last_health_evaluated_at
		FROM warmup_pool_participants wpp
		JOIN warmup_pools wp ON wp.id = wpp.pool_id
		WHERE wpp.email_account_id = $1
		  AND wp.pool_type = $2
		LIMIT 1
	`

	var out models.WarmupParticipantHealth
	var state string
	if err := r.db.QueryRow(ctx, query, accountID, poolType).Scan(
		&out.PoolID,
		&out.PoolType,
		&out.EmailAccountID,
		&out.JoinedAt,
		&out.BlockedAt,
		&out.BlockedUntil,
		&out.BlockedReason,
		&out.SpamScore,
		&state,
		&out.LastHealthScore,
		&out.LastHealthReason,
		&out.LastHealthEvaluatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	out.HealthState = models.WarmupHealthState(state)
	return &out, nil
}

func (r *warmupRepository) UpdateParticipantHealth(ctx context.Context, accountID uuid.UUID, state models.WarmupHealthState, blockedUntil *time.Time, reason string, score float64) error {
	query := `
		UPDATE warmup_pool_participants
		SET
			health_state = $1,
			blocked_until = $2,
			blocked_at = CASE
				WHEN $2 IS NOT NULL AND (blocked_at IS NULL OR blocked_until IS DISTINCT FROM $2) THEN NOW()
				WHEN $2 IS NULL AND blocked_until IS NOT NULL THEN NULL
				ELSE blocked_at
			END,
			blocked_reason = CASE
				WHEN $2 IS NOT NULL OR $1 = 'blocked' THEN $3
				WHEN $1 = 'healthy' THEN NULL
				ELSE COALESCE($3, blocked_reason)
			END,
			last_health_score = $4,
			last_health_reason = NULLIF($3, ''),
			last_health_evaluated_at = NOW()
		WHERE email_account_id = $5
		  AND NOT (blocked_at IS NOT NULL AND blocked_until IS NULL AND health_state = 'blocked')
	`
	_, err := r.db.Exec(ctx, query, state, blockedUntil, reason, score, accountID)
	return err
}

// CountSpamReportsSince returns the total count of any warmup spam-related
// event against the account. Retained for backward compatibility with code
// that wants the combined signal; new code should prefer the split
// CountUserComplaintsSince / CountSpamPlacementsSince methods so the two
// fundamentally different signals can be threshold-checked independently.
func (r *warmupRepository) CountSpamReportsSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM warmup_spam_reports
		WHERE reported_account_id = $1
		  AND created_at >= $2
		  AND report_type IN ('spam', 'spam_folder', 'user_complaint', 'spam_placement')
	`
	var count int
	err := r.db.QueryRow(ctx, query, accountID, since).Scan(&count)
	return count, err
}

// CountUserComplaintsSince counts warmup events where the recipient
// explicitly marked the message as spam. Strong negative signal because
// the user actively rejected the content.
func (r *warmupRepository) CountUserComplaintsSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM warmup_spam_reports
		WHERE reported_account_id = $1
		  AND created_at >= $2
		  AND report_type IN ('user_complaint', 'spam', 'spam_folder')
	`
	var count int
	err := r.db.QueryRow(ctx, query, accountID, since).Scan(&count)
	return count, err
}

// CountSpamPlacementsSince counts warmup events where the message landed
// in the recipient's Junk/Spam folder on delivery. Distinct from a user
// complaint — the user took no action; provider classifier put it there.
func (r *warmupRepository) CountSpamPlacementsSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM warmup_spam_reports
		WHERE reported_account_id = $1
		  AND created_at >= $2
		  AND report_type = 'spam_placement'
	`
	var count int
	err := r.db.QueryRow(ctx, query, accountID, since).Scan(&count)
	return count, err
}

func (r *warmupRepository) SumWarmupSentSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error) {
	query := `
		SELECT COALESCE(SUM(emails_sent), 0)
		FROM warmup_statistics
		WHERE email_account_id = $1
		  AND date >= DATE($2)
	`
	var total int
	err := r.db.QueryRow(ctx, query, accountID, since).Scan(&total)
	return total, err
}

// CountDeliverabilityEventsByAccount counts deliverability events (bounce, complaint, etc.)
// for a specific email account by joining through the tasks table.
func (r *warmupRepository) CountDeliverabilityEventsByAccount(ctx context.Context, accountID uuid.UUID, eventType string, since time.Time) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM deliverability_events de
		JOIN tasks t ON t.id = de.task_id
		WHERE t.email_account_id = $1
		  AND de.event_type = $2
		  AND de.created_at >= $3
	`
	var count int
	err := r.db.QueryRow(ctx, query, accountID, eventType, since).Scan(&count)
	return count, err
}

// CountDeliveredByAccount counts completed tasks (sent emails) for an account since a given time.
func (r *warmupRepository) CountDeliveredByAccount(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM tasks
		WHERE email_account_id = $1
		  AND status = 'completed'
		  AND completed_at >= $2
	`
	var count int
	err := r.db.QueryRow(ctx, query, accountID, since).Scan(&count)
	return count, err
}

// IncrementSpamScore increments the spam score for an account
func (r *warmupRepository) IncrementSpamScore(ctx context.Context, accountID uuid.UUID, amount int) (int, error) {
	query := `
		UPDATE warmup_pool_participants
		SET spam_score = spam_score + $1
		WHERE email_account_id = $2
		RETURNING spam_score
	`

	var newScore int
	err := r.db.QueryRow(ctx, query, amount, accountID).Scan(&newScore)
	return newScore, err
}

// ResetSpamScore resets the spam score for an account
func (r *warmupRepository) ResetSpamScore(ctx context.Context, accountID uuid.UUID) error {
	query := `
		UPDATE warmup_pool_participants
		SET spam_score = 0
		WHERE email_account_id = $1
	`

	_, err := r.db.Exec(ctx, query, accountID)
	return err
}

// IncrementDailyCount increments the daily email count for warmup
func (r *warmupRepository) IncrementDailyCount(ctx context.Context, accountID uuid.UUID, date time.Time) error {
	query := `
		INSERT INTO warmup_statistics (email_account_id, date, emails_sent, target_volume)
		VALUES ($1, DATE($2), 1, 0)
		ON CONFLICT (email_account_id, date)
		DO UPDATE SET emails_sent = warmup_statistics.emails_sent + 1
	`

	_, err := r.db.Exec(ctx, query, accountID, date)
	return err
}

// IncrementReplyCount increments the daily warmup reply count. Upserts the row
// so it is order-independent with IncrementDailyCount (either may run first).
func (r *warmupRepository) IncrementReplyCount(ctx context.Context, accountID uuid.UUID, date time.Time) error {
	query := `
		INSERT INTO warmup_statistics (email_account_id, date, emails_sent, emails_replied, target_volume)
		VALUES ($1, DATE($2), 0, 1, 0)
		ON CONFLICT (email_account_id, date)
		DO UPDATE SET emails_replied = warmup_statistics.emails_replied + 1
	`
	_, err := r.db.Exec(ctx, query, accountID, date)
	return err
}

// PoolSpamPlacementRate returns the pool-wide warmup spam-placement rate (%)
// over the window: spam_placement events divided by total warmup sends. This is
// the number surfaced as avg_spam_placement_rate in the admin health summary.
func (r *warmupRepository) PoolSpamPlacementRate(ctx context.Context, since time.Time) (float64, error) {
	query := `
		SELECT
			(SELECT COUNT(*) FROM warmup_spam_reports WHERE report_type = 'spam_placement' AND created_at >= $1) AS placements,
			(SELECT COALESCE(SUM(emails_sent), 0) FROM warmup_statistics WHERE date >= DATE($1)) AS sent
	`
	var placements, sent int
	if err := r.db.QueryRow(ctx, query, since).Scan(&placements, &sent); err != nil {
		return 0, err
	}
	if sent == 0 {
		return 0, nil
	}
	return float64(placements) / float64(sent) * 100, nil
}

// PoolSpamPlacementsByProvider returns spam-placement counts grouped by the
// recipient provider over the window, so the admin overview can show where
// warmup mail is being filtered (e.g. mostly at Outlook vs Gmail).
func (r *warmupRepository) PoolSpamPlacementsByProvider(ctx context.Context, since time.Time) (map[string]int, error) {
	query := `
		SELECT COALESCE(NULLIF(recipient_provider, ''), 'unknown'), COUNT(*)
		FROM warmup_spam_reports
		WHERE report_type = 'spam_placement' AND created_at >= $1
		GROUP BY 1
	`
	rows, err := r.db.Query(ctx, query, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var provider string
		var n int
		if err := rows.Scan(&provider, &n); err != nil {
			return nil, err
		}
		out[provider] = n
	}
	return out, rows.Err()
}

// GetWarmupStatistics retrieves warmup statistics for a date range
func (r *warmupRepository) GetWarmupStatistics(ctx context.Context, accountID uuid.UUID, from, to time.Time) ([]WarmupStatistic, error) {
	query := `
		SELECT email_account_id, date, emails_sent, emails_replied, target_volume
		FROM warmup_statistics
		WHERE email_account_id = $1
		  AND date >= DATE($2)
		  AND date <= DATE($3)
		ORDER BY date ASC
	`

	rows, err := r.db.Query(ctx, query, accountID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []WarmupStatistic
	for rows.Next() {
		stat := WarmupStatistic{}
		err := rows.Scan(
			&stat.EmailAccountID,
			&stat.Date,
			&stat.EmailsSent,
			&stat.EmailsReplied,
			&stat.TargetVolume,
		)
		if err != nil {
			return nil, err
		}
		stats = append(stats, stat)
	}

	return stats, rows.Err()
}

// GetOrCreateDailyStats retrieves or creates daily warmup statistics
func (r *warmupRepository) GetOrCreateDailyStats(ctx context.Context, accountID uuid.UUID, date time.Time, targetVolume int) (*WarmupStatistic, error) {
	query := `
		INSERT INTO warmup_statistics (email_account_id, date, emails_sent, emails_replied, target_volume)
		VALUES ($1, DATE($2), 0, 0, $3)
		ON CONFLICT (email_account_id, date)
		DO UPDATE SET target_volume = EXCLUDED.target_volume
		RETURNING email_account_id, date, emails_sent, emails_replied, target_volume
	`

	stat := &WarmupStatistic{}
	err := r.db.QueryRow(ctx, query, accountID, date, targetVolume).Scan(
		&stat.EmailAccountID,
		&stat.Date,
		&stat.EmailsSent,
		&stat.EmailsReplied,
		&stat.TargetVolume,
	)

	return stat, err
}

// CreateWarmupToken creates a warmup verification token
func (r *warmupRepository) CreateWarmupToken(ctx context.Context, token *models.WarmupToken) error {
	query := `
		INSERT INTO warmup_tokens (token, task_id, sender_account_id, recipient_account_id, conversation_theme, content_source, conversation_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := r.db.Exec(ctx, query,
		token.Token,
		token.TaskID,
		token.SenderAccountID,
		token.RecipientAccountID,
		token.ConversationTheme,
		token.ContentSource,
		token.ConversationID,
		token.ExpiresAt,
	)
	return err
}

// GetWarmupToken retrieves a valid (unconsumed, unexpired) warmup token
func (r *warmupRepository) GetWarmupToken(ctx context.Context, tokenID uuid.UUID) (*models.WarmupToken, error) {
	query := `
		SELECT token, task_id, sender_account_id, recipient_account_id, COALESCE(conversation_theme, ''), COALESCE(content_source, ''), conversation_id, created_at, consumed_at, expires_at
		FROM warmup_tokens
		WHERE token = $1 AND consumed_at IS NULL AND expires_at > NOW()
	`

	t := &models.WarmupToken{}
	err := r.db.QueryRow(ctx, query, tokenID).Scan(
		&t.Token,
		&t.TaskID,
		&t.SenderAccountID,
		&t.RecipientAccountID,
		&t.ConversationTheme,
		&t.ContentSource,
		&t.ConversationID,
		&t.CreatedAt,
		&t.ConsumedAt,
		&t.ExpiresAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return t, err
}

func (r *warmupRepository) FindWarmupToken(ctx context.Context, tokenID uuid.UUID) (*models.WarmupToken, error) {
	query := `
		SELECT token, task_id, sender_account_id, recipient_account_id, COALESCE(conversation_theme, ''), COALESCE(content_source, ''), conversation_id, created_at, consumed_at, expires_at
		FROM warmup_tokens
		WHERE token = $1
	`

	t := &models.WarmupToken{}
	err := r.db.QueryRow(ctx, query, tokenID).Scan(
		&t.Token,
		&t.TaskID,
		&t.SenderAccountID,
		&t.RecipientAccountID,
		&t.ConversationTheme,
		&t.ContentSource,
		&t.ConversationID,
		&t.CreatedAt,
		&t.ConsumedAt,
		&t.ExpiresAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return t, err
}

// ConsumeWarmupToken marks a warmup token as consumed
func (r *warmupRepository) ConsumeWarmupToken(ctx context.Context, tokenID uuid.UUID) error {
	query := `UPDATE warmup_tokens SET consumed_at = NOW() WHERE token = $1`
	_, err := r.db.Exec(ctx, query, tokenID)
	return err
}

// RecordInvalidTokenAttempt records an invalid warmup token attempt
func (r *warmupRepository) RecordInvalidTokenAttempt(ctx context.Context, accountID uuid.UUID, attemptedToken string) error {
	query := `
		INSERT INTO warmup_invalid_token_attempts (email_account_id, attempted_token)
		VALUES ($1, $2)
	`
	_, err := r.db.Exec(ctx, query, accountID, attemptedToken)
	return err
}

// CountRecentInvalidAttempts counts invalid token attempts since a given time
func (r *warmupRepository) CountRecentInvalidAttempts(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM warmup_invalid_token_attempts
		WHERE email_account_id = $1 AND created_at > $2
	`

	var count int
	err := r.db.QueryRow(ctx, query, accountID, since).Scan(&count)
	return count, err
}

// GetRecentlyUsedPartners returns partner account IDs the sender has targeted since the provided timestamp.
func (r *warmupRepository) GetRecentlyUsedPartners(ctx context.Context, accountID uuid.UUID, since time.Time) ([]uuid.UUID, error) {
	query := `
		SELECT DISTINCT recipient_account_id
		FROM warmup_tokens
		WHERE sender_account_id = $1
		  AND created_at >= $2
	`

	rows, err := r.db.Query(ctx, query, accountID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var partnerIDs []uuid.UUID
	for rows.Next() {
		var partnerID uuid.UUID
		if err := rows.Scan(&partnerID); err != nil {
			return nil, err
		}
		partnerIDs = append(partnerIDs, partnerID)
	}

	return partnerIDs, rows.Err()
}

// GetRecentPartnerCounts returns how many times the sender has targeted each
// partner since the provided timestamp. Used to enforce an explicit
// partner-diversity target — no single partner should absorb a large share of
// one mailbox's warmup traffic (a reciprocal-graph detection signal).
func (r *warmupRepository) GetRecentPartnerCounts(ctx context.Context, accountID uuid.UUID, since time.Time) (map[uuid.UUID]int, error) {
	query := `
		SELECT recipient_account_id, COUNT(*)
		FROM warmup_tokens
		WHERE sender_account_id = $1
		  AND created_at >= $2
		GROUP BY recipient_account_id
	`

	rows, err := r.db.Query(ctx, query, accountID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[uuid.UUID]int)
	for rows.Next() {
		var partnerID uuid.UUID
		var n int
		if err := rows.Scan(&partnerID, &n); err != nil {
			return nil, err
		}
		counts[partnerID] = n
	}

	return counts, rows.Err()
}

// RecordWarmupReceived stores a delivered warmup email keyed by recipient +
// internal message id. Idempotent on re-delivery of the same message.
func (r *warmupRepository) RecordWarmupReceived(ctx context.Context, accountID, internalID uuid.UUID, messageID string, senderAccountID uuid.UUID) error {
	query := `
		INSERT INTO warmup_received (email_account_id, internal_id, message_id, sender_account_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (email_account_id, internal_id) DO NOTHING
	`
	_, err := r.db.Exec(ctx, query, accountID, internalID, messageID, senderAccountID)
	return err
}

// GetWarmupReceived looks up a delivered warmup email by recipient + internal
// message id. Returns nil when the message was not a warmup email.
func (r *warmupRepository) GetWarmupReceived(ctx context.Context, accountID, internalID uuid.UUID) (*WarmupReceived, error) {
	query := `
		SELECT email_account_id, internal_id, message_id, sender_account_id, created_at
		FROM warmup_received
		WHERE email_account_id = $1 AND internal_id = $2
	`
	var w WarmupReceived
	err := r.db.QueryRow(ctx, query, accountID, internalID).Scan(
		&w.EmailAccountID, &w.InternalID, &w.MessageID, &w.SenderAccountID, &w.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// RecordWarmupTampering records one "harm" a participant did to a warmup email.
// Returns whether a new row was inserted (deduped per account+message+kind).
func (r *warmupRepository) RecordWarmupTampering(ctx context.Context, accountID uuid.UUID, messageID, kind string) (bool, error) {
	query := `
		INSERT INTO warmup_tampering_events (email_account_id, message_id, kind)
		VALUES ($1, $2, $3)
		ON CONFLICT (email_account_id, message_id, kind) DO NOTHING
	`
	cmd, err := r.db.Exec(ctx, query, accountID, messageID, kind)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

// CountWarmupTamperingSince counts distinct tampering events for a mailbox.
func (r *warmupRepository) CountWarmupTamperingSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error) {
	query := `SELECT COUNT(*) FROM warmup_tampering_events WHERE email_account_id = $1 AND created_at >= $2`
	var n int
	err := r.db.QueryRow(ctx, query, accountID, since).Scan(&n)
	return n, err
}

// CreateWarmupAppeal inserts a pending appeal for a blocked mailbox.
func (r *warmupRepository) CreateWarmupAppeal(ctx context.Context, accountID, userID uuid.UUID, reason string) (uuid.UUID, error) {
	id := uuid.New()
	query := `
		INSERT INTO warmup_appeals (id, email_account_id, user_id, reason, status)
		VALUES ($1, $2, $3, $4, 'pending')
	`
	_, err := r.db.Exec(ctx, query, id, accountID, userID, reason)
	return id, err
}

// HasPendingWarmupAppeal reports whether the mailbox already has an open appeal.
func (r *warmupRepository) HasPendingWarmupAppeal(ctx context.Context, accountID uuid.UUID) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM warmup_appeals WHERE email_account_id = $1 AND status = 'pending')`
	var exists bool
	err := r.db.QueryRow(ctx, query, accountID).Scan(&exists)
	return exists, err
}

// GetPoolParticipantDomains returns a map from email_account_id to lowercased
// domain (the part after '@') for every active participant in the given pool.
// Used by the partner selector to weight selection toward under-represented
// recipient domains so a single mailbox provider does not dominate warmup
// traffic from a sender.
func (r *warmupRepository) GetPoolParticipantDomains(ctx context.Context, poolType string, excludeBlocked bool) (map[uuid.UUID]string, error) {
	query := `
		SELECT wpp.email_account_id, lower(split_part(ea.email, '@', 2))
		FROM warmup_pool_participants wpp
		JOIN warmup_pools wp ON wpp.pool_id = wp.id
		JOIN email_accounts ea ON ea.id = wpp.email_account_id
		WHERE wp.pool_type = $1
		  AND ea.status = 'active'
	`
	if excludeBlocked {
		query += " AND wpp.health_state IN ('healthy', 'watch', 'throttled')"
	}
	query += " AND wpp.participant_role IN ('sender_receiver', 'recipient_only')"

	rows, err := r.db.Query(ctx, query, poolType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[uuid.UUID]string)
	for rows.Next() {
		var id uuid.UUID
		var domain string
		if err := rows.Scan(&id, &domain); err != nil {
			return nil, err
		}
		out[id] = domain
	}
	return out, rows.Err()
}

// GetPoolParticipantEmails returns a map from email_account_id to full
// email address for every active participant in the given pool. Used by
// the routing-rule evaluator which needs the full address to classify
// providers and apply customer-defined rules.
func (r *warmupRepository) GetPoolParticipantEmails(ctx context.Context, poolType string, excludeBlocked bool) (map[uuid.UUID]string, error) {
	query := `
		SELECT wpp.email_account_id, ea.email
		FROM warmup_pool_participants wpp
		JOIN warmup_pools wp ON wpp.pool_id = wp.id
		JOIN email_accounts ea ON ea.id = wpp.email_account_id
		WHERE wp.pool_type = $1
		  AND ea.status = 'active'
	`
	if excludeBlocked {
		query += " AND wpp.health_state IN ('healthy', 'watch', 'throttled')"
	}
	query += " AND wpp.participant_role IN ('sender_receiver', 'recipient_only')"

	rows, err := r.db.Query(ctx, query, poolType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[uuid.UUID]string)
	for rows.Next() {
		var id uuid.UUID
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		out[id] = email
	}
	return out, rows.Err()
}

func (r *warmupRepository) CountEligibleRecipients(ctx context.Context, poolType string, excludeAccountID uuid.UUID) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM warmup_pool_participants wpp
		JOIN warmup_pools wp ON wpp.pool_id = wp.id
		JOIN email_accounts ea ON ea.id = wpp.email_account_id
		WHERE wp.pool_type = $1
		  AND wpp.email_account_id <> $2
		  AND wpp.participant_role IN ('sender_receiver', 'recipient_only')
		  AND ea.status = 'active'
		  AND (
		   wpp.health_state IN ('healthy', 'watch', 'throttled')
		   OR (
		    wpp.health_state IN ('quarantined', 'blocked')
		    AND wpp.blocked_until IS NOT NULL
		    AND wpp.blocked_until <= NOW()
		   )
		  )
		  AND (
		   wpp.blocked_at IS NULL
		   OR (wpp.blocked_until IS NOT NULL AND wpp.blocked_until <= NOW())
		  )
	`
	var count int
	err := r.db.QueryRow(ctx, query, poolType, excludeAccountID).Scan(&count)
	return count, err
}

// GetRecentPartnerDomainCounts returns a histogram of recipient domains the
// sender has targeted since the given timestamp. The selector uses this to
// downweight partners whose domain is over-represented in recent traffic.
func (r *warmupRepository) GetRecentPartnerDomainCounts(ctx context.Context, accountID uuid.UUID, since time.Time) (map[string]int, error) {
	query := `
		SELECT lower(split_part(ea.email, '@', 2)) AS domain, COUNT(*)
		FROM warmup_tokens wt
		JOIN email_accounts ea ON ea.id = wt.recipient_account_id
		WHERE wt.sender_account_id = $1
		  AND wt.created_at >= $2
		GROUP BY domain
	`
	rows, err := r.db.Query(ctx, query, accountID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var domain string
		var count int
		if err := rows.Scan(&domain, &count); err != nil {
			return nil, err
		}
		out[domain] = count
	}
	return out, rows.Err()
}

// GetLatestReplyCandidate finds the latest completed warmup email from sender to recipient.
// It also returns the original message's conversation_theme so the reply body can
// be drawn from the same topical bucket instead of a random conversation.
func (r *warmupRepository) GetLatestReplyCandidate(ctx context.Context, senderAccountID, recipientAccountID uuid.UUID) (*WarmupReplyCandidate, error) {
	query := `
		SELECT t.message_id, COALESCE(et.subject, ''), et.thread_id, COALESCE(wt.conversation_theme, '')
		FROM warmup_tokens wt
		JOIN tasks t ON t.id = wt.task_id
		LEFT JOIN email_tasks et ON et.task_id = t.id
		WHERE wt.sender_account_id = $1
		  AND wt.recipient_account_id = $2
		  AND t.status = 'completed'
		  AND t.message_id <> ''
		  AND t.completed_at IS NOT NULL
		  -- Human reply timing: never reply to a message that just arrived (the
		  -- recipient-side read engagement hasn't plausibly happened yet), and
		  -- don't necro-reply to threads older than a week.
		  AND t.completed_at < NOW() - INTERVAL '45 minutes'
		  AND t.completed_at > NOW() - INTERVAL '7 days'
		ORDER BY t.completed_at DESC
		LIMIT 1
	`

	candidate := &WarmupReplyCandidate{}
	err := r.db.QueryRow(ctx, query, senderAccountID, recipientAccountID).Scan(
		&candidate.MessageID,
		&candidate.Subject,
		&candidate.ThreadID,
		&candidate.ConversationTheme,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return candidate, nil
}

// GetAllParticipantAccountIDs returns all unique account IDs across all warmup pools
func (r *warmupRepository) GetAllParticipantAccountIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT DISTINCT email_account_id FROM warmup_pool_participants`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetPoolHealthCounts returns counts per health state and average spam score
func (r *warmupRepository) GetPoolHealthCounts(ctx context.Context) (map[string]int, float64, error) {
	query := `
		SELECT health_state, COUNT(*), AVG(spam_score)
		FROM warmup_pool_participants
		GROUP BY health_state
	`
	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	counts := map[string]int{}
	var totalScore float64
	var totalCount int
	for rows.Next() {
		var state string
		var count int
		var avgScore float64
		if err := rows.Scan(&state, &count, &avgScore); err != nil {
			return nil, 0, err
		}
		counts[state] = count
		totalScore += avgScore * float64(count)
		totalCount += count
	}

	avgScore := 0.0
	if totalCount > 0 {
		avgScore = totalScore / float64(totalCount)
	}
	return counts, avgScore, rows.Err()
}
