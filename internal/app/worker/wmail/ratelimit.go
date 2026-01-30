package wmail

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Rate limiting constants for IMAP sync (anti-abuse)
const (
	// SyncRateLimitPerHour - Max new emails synced per hour
	SyncRateLimitPerHour = 500

	// SyncRateLimitPer5Min - Max new emails in 5 minute window (burst protection)
	SyncRateLimitPer5Min = 100

	// Window durations
	hourWindow    = time.Hour
	fiveMinWindow = 5 * time.Minute

	// Key TTL (slightly longer than window for cleanup)
	syncKeyTTL = 2 * time.Hour
)

// getSyncRateLimitKey returns the Redis key for sync rate limiting
func (w *WMail) getSyncRateLimitKey(window string) string {
	return fmt.Sprintf("sync:%s:%s", w.ID.String(), window)
}

// checkSyncRateLimit checks if the email account has exceeded sync rate limits
// Returns a MailError if rate limit is exceeded, nil otherwise
func (w *WMail) checkSyncRateLimit(ctx context.Context) *errx.MailError {
	if w.Cache == nil {
		// No cache client, skip rate limiting
		return nil
	}

	now := time.Now()

	// Check 5-minute burst limit
	burstKey := w.getSyncRateLimitKey("5min")
	burstCount, err := w.getSyncCount(ctx, burstKey, fiveMinWindow, now)
	if err != nil {
		log.Warn().Err(err).Str("email_id", w.ID.String()).Msg("Failed to check burst rate limit")
		// Don't block on Redis errors
		return nil
	}

	if burstCount >= SyncRateLimitPer5Min {
		log.Warn().
			Str("email_id", w.ID.String()).
			Int64("count", burstCount).
			Int("limit", SyncRateLimitPer5Min).
			Msg("Sync burst rate limit exceeded")
		return errx.ErrMailRateLimitExceeded
	}

	// Check hourly limit
	hourlyKey := w.getSyncRateLimitKey("hourly")
	hourlyCount, err := w.getSyncCount(ctx, hourlyKey, hourWindow, now)
	if err != nil {
		log.Warn().Err(err).Str("email_id", w.ID.String()).Msg("Failed to check hourly rate limit")
		return nil
	}

	if hourlyCount >= SyncRateLimitPerHour {
		log.Warn().
			Str("email_id", w.ID.String()).
			Int64("count", hourlyCount).
			Int("limit", SyncRateLimitPerHour).
			Msg("Sync hourly rate limit exceeded")
		return errx.ErrMailRateLimitExceeded
	}

	return nil
}

// getSyncCount returns the number of sync events in the given time window
func (w *WMail) getSyncCount(ctx context.Context, key string, window time.Duration, now time.Time) (int64, error) {
	// Remove old entries outside the window
	minScore := fmt.Sprintf("%d", now.Add(-window).UnixNano())
	if err := w.Cache.ZRemRangeByScore(ctx, key, "-inf", minScore).Err(); err != nil {
		return 0, err
	}

	// Count current entries
	count, err := w.Cache.ZCard(ctx, key).Result()
	if err != nil {
		return 0, err
	}

	return count, nil
}

// recordSyncEvent records sync events for rate limiting
// count is the number of new emails synced in this batch
func (w *WMail) recordSyncEvent(ctx context.Context, count int) error {
	if w.Cache == nil || count <= 0 {
		return nil
	}

	now := time.Now()
	score := float64(now.UnixNano())

	// Record events with unique members (timestamp + index)
	members := make([]redis.Z, count)
	for i := 0; i < count; i++ {
		members[i] = redis.Z{
			Score:  score,
			Member: fmt.Sprintf("%d:%d", now.UnixNano(), i),
		}
	}

	// Add to both windows
	pipe := w.Cache.Pipeline()

	burstKey := w.getSyncRateLimitKey("5min")
	hourlyKey := w.getSyncRateLimitKey("hourly")

	pipe.ZAdd(ctx, burstKey, members...)
	pipe.Expire(ctx, burstKey, syncKeyTTL)

	pipe.ZAdd(ctx, hourlyKey, members...)
	pipe.Expire(ctx, hourlyKey, syncKeyTTL)

	_, err := pipe.Exec(ctx)
	if err != nil {
		log.Warn().Err(err).Str("email_id", w.ID.String()).Msg("Failed to record sync events")
		return err
	}

	return nil
}

// onRateLimitExceeded handles rate limit exceeded - terminates account and sends event
func (w *WMail) onRateLimitExceeded(ctx context.Context, err *errx.MailError) {
	log.Error().
		Str("email_id", w.ID.String()).
		Str("user_id", w.UserID.String()).
		Str("error_code", string(err.Code)).
		Msg("Rate limit exceeded - terminating email account")

	// Send rate limit event to jobsService via Kafka
	userInfo := err.GetUserErrorInfo()
	errorEvent := models.EmailErrorEvent{
		TaskID:         "",
		EmailAccountID: w.ID.String(),
		UserID:         w.UserID.String(),
		ErrorCode:      string(err.Code),
		ErrorType:      string(err.Type),
		ResolveMethod:  string(err.ResolveMethod),
		Message:        err.Message,
		UserVisible:    err.IsUserVisible(),
		UserTitle:      userInfo.Title,
		UserMessage:    userInfo.Message,
		ActionRequired: userInfo.ActionRequired,
		Timestamp:      time.Now().Unix(),
	}

	if sendErr := w.onEvent(models.JobEventTypeEmailRateLimited, errorEvent); sendErr != nil {
		log.Error().Err(sendErr).Msg("Failed to send rate limit event")
	}

	// Terminate the email account
	if w.TerminateFunc != nil {
		w.TerminateFunc()
	}
}

// CheckAndRecordSync checks rate limits and records sync events
// Returns MailError if rate limit exceeded (and triggers termination)
func (w *WMail) CheckAndRecordSync(ctx context.Context, newEmailCount int) *errx.MailError {
	// Check rate limit first
	if err := w.checkSyncRateLimit(ctx); err != nil {
		w.onRateLimitExceeded(ctx, err)
		return err
	}

	// Record the sync event
	if err := w.recordSyncEvent(ctx, newEmailCount); err != nil {
		// Log but don't fail
		log.Warn().Err(err).Msg("Failed to record sync event")
	}

	return nil
}
