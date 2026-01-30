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
	PoolID         uuid.UUID
	EmailAccountID uuid.UUID
	JoinedAt       time.Time
	BlockedAt      *time.Time
	BlockedReason  *string
	SpamScore      int
}

// SpamReport represents a spam report
type SpamReport struct {
	ID                uuid.UUID
	ReporterAccountID uuid.UUID
	ReportedAccountID uuid.UUID
	MessageID         string
	ReportType        string
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

// WarmupRepository defines methods for warmup data access
type WarmupRepository interface {
	// Pool management
	GetPoolByType(ctx context.Context, poolType string) (*WarmupPool, error)
	GetPoolParticipants(ctx context.Context, poolType string, excludeBlocked bool) ([]uuid.UUID, error)
	JoinPool(ctx context.Context, poolID, accountID uuid.UUID) error
	LeavePool(ctx context.Context, poolID, accountID uuid.UUID) error
	BlockFromPool(ctx context.Context, accountID uuid.UUID, reason string) error
	UnblockFromPool(ctx context.Context, accountID uuid.UUID) error
	IsInPool(ctx context.Context, accountID uuid.UUID, poolType string) (bool, error)

	// Spam tracking
	RecordSpamReport(ctx context.Context, report *SpamReport) error
	GetSpamScore(ctx context.Context, accountID uuid.UUID) (int, error)
	IncrementSpamScore(ctx context.Context, accountID uuid.UUID, amount int) (int, error)
	ResetSpamScore(ctx context.Context, accountID uuid.UUID) error

	// Statistics
	IncrementDailyCount(ctx context.Context, accountID uuid.UUID, date time.Time) error
	GetWarmupStatistics(ctx context.Context, accountID uuid.UUID, from, to time.Time) ([]WarmupStatistic, error)
	GetOrCreateDailyStats(ctx context.Context, accountID uuid.UUID, date time.Time, targetVolume int) (*WarmupStatistic, error)

	// Warmup token management
	CreateWarmupToken(ctx context.Context, token *models.WarmupToken) error
	GetWarmupToken(ctx context.Context, tokenID uuid.UUID) (*models.WarmupToken, error)
	ConsumeWarmupToken(ctx context.Context, tokenID uuid.UUID) error
	RecordInvalidTokenAttempt(ctx context.Context, accountID uuid.UUID, attemptedToken string) error
	CountRecentInvalidAttempts(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error)
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
		WHERE wp.pool_type = $1
	`

	if excludeBlocked {
		query += ` AND wpp.blocked_at IS NULL`
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
		INSERT INTO warmup_pool_participants (pool_id, email_account_id, joined_at, spam_score)
		VALUES ($1, $2, NOW(), 0)
		ON CONFLICT (pool_id, email_account_id) DO NOTHING
	`

	_, err := r.db.Exec(ctx, query, poolID, accountID)
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
		    blocked_reason = $1
		WHERE email_account_id = $2
		  AND blocked_at IS NULL
	`

	_, err := r.db.Exec(ctx, query, reason, accountID)
	return err
}

// UnblockFromPool unblocks an account from all warmup pools
func (r *warmupRepository) UnblockFromPool(ctx context.Context, accountID uuid.UUID) error {
	query := `
		UPDATE warmup_pool_participants
		SET blocked_at = NULL,
		    blocked_reason = NULL
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
			  AND wpp.blocked_at IS NULL
		)
	`

	var exists bool
	err := r.db.QueryRow(ctx, query, accountID, poolType).Scan(&exists)
	return exists, err
}

// RecordSpamReport records a spam report
func (r *warmupRepository) RecordSpamReport(ctx context.Context, report *SpamReport) error {
	query := `
		INSERT INTO warmup_spam_reports (id, reporter_account_id, reported_account_id, message_id, report_type, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (reporter_account_id, message_id) DO NOTHING
	`

	_, err := r.db.Exec(ctx, query,
		report.ID,
		report.ReporterAccountID,
		report.ReportedAccountID,
		report.MessageID,
		report.ReportType,
	)

	return err
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
		INSERT INTO warmup_tokens (token, task_id, sender_account_id, recipient_account_id, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := r.db.Exec(ctx, query,
		token.Token,
		token.TaskID,
		token.SenderAccountID,
		token.RecipientAccountID,
		token.ExpiresAt,
	)
	return err
}

// GetWarmupToken retrieves a valid (unconsumed, unexpired) warmup token
func (r *warmupRepository) GetWarmupToken(ctx context.Context, tokenID uuid.UUID) (*models.WarmupToken, error) {
	query := `
		SELECT token, task_id, sender_account_id, recipient_account_id, created_at, consumed_at, expires_at
		FROM warmup_tokens
		WHERE token = $1 AND consumed_at IS NULL AND expires_at > NOW()
	`

	t := &models.WarmupToken{}
	err := r.db.QueryRow(ctx, query, tokenID).Scan(
		&t.Token,
		&t.TaskID,
		&t.SenderAccountID,
		&t.RecipientAccountID,
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
