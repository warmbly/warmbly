package repository

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/config"
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
	References        []string
	ScenarioVersion   string
	RenderingVersion  string
	MaxTurns          int
	MessageID         string
	Subject           string
	ThreadID          *string
	ConversationTheme string
	ContentSource     string
	ConversationID    *uuid.UUID
	ConversationTurn  int
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
	// RetiredAt is when the retention sweep sent the deletion for this
	// message. A removal observed after that is the platform's own.
	RetiredAt *time.Time
	// LandedSpam is set when the message arrived in the spam folder, so a
	// spam label on it is the provider's filter, not the owner.
	LandedSpam bool
}

// WarmupMailToRetire is one warmup message whose retention window has passed,
// with what the worker needs to find it: the provider's key from the message
// map (a Gmail id, a Graph id, or the RFC Message-ID on IMAP), the immutable
// Message-ID, and where the mailbox files warmup so the search starts there.
// Token is set for the sender's own copy of a send, InternalID for the copy a
// recipient received.
type WarmupMailToRetire struct {
	UserID         uuid.UUID
	EmailAccountID uuid.UUID
	WorkerID       uuid.UUID
	InternalID     uuid.UUID
	Token          uuid.UUID
	MessageID      string
	ProviderKey    string
	Placement      string
	Folder         string
}

// WarmupRepository defines methods for warmup data access
type WarmupRepository interface {
	// Pool management
	IsPoolEligible(ctx context.Context, accountID uuid.UUID, poolType string, sending bool) (bool, error)
	GetPoolParticipants(ctx context.Context, poolType string, excludeBlocked bool) ([]uuid.UUID, error)
	// MoveToPool joins this pool, or moves an existing membership over. A new
	// member starts from the standing mirrored for its address (see migration
	// 000152), so re-adding a mailbox is not a reset.
	MoveToPool(ctx context.Context, poolID, accountID uuid.UUID, role string) error
	// PurgeExpiredReputationLedger forgets the mirrored standing of addresses
	// with no live pool row once the retention window has lapsed, and reports
	// how many.
	PurgeExpiredReputationLedger(ctx context.Context) (int64, error)
	// MoveExistingToPool moves an existing member, keeping its role; a mailbox in no pool stays out.
	MoveExistingToPool(ctx context.Context, poolID, accountID uuid.UUID) (bool, error)
	// LeaveAllPools removes the mailbox from warmup. Removal is never pool-scoped: the caller
	// cannot know which pool a mailbox whose entitlement just changed is sitting in (issue #211).
	LeaveAllPools(ctx context.Context, accountID uuid.UUID) error
	BlockFromPool(ctx context.Context, accountID uuid.UUID, reason string) error
	// GetHealthState returns the account's warmup health state plus its
	// blocked_until, so non-warmup callers (e.g. the campaign scheduler) can
	// gate cold sends on warmup health without needing a pool type. A mailbox
	// Warmbly Cloud warms reads the standing the cloud last reported. Returns
	// ("healthy", nil) when the account has neither.
	GetHealthState(ctx context.Context, accountID uuid.UUID) (models.WarmupHealthState, *time.Time, error)
	// GetHealthStates is GetHealthState for a pool in one read. A mailbox in
	// no pool is healthy, as the single read reports it.
	GetHealthStates(ctx context.Context, accountIDs []uuid.UUID) (map[uuid.UUID]WarmupHealthRead, error)
	UnblockFromPool(ctx context.Context, accountID uuid.UUID) error
	IsInPool(ctx context.Context, accountID uuid.UUID, poolType string) (bool, error)
	GetParticipantHealth(ctx context.Context, accountID uuid.UUID, poolType string) (*models.WarmupParticipantHealth, error)
	// GetParticipantHealthForAccount returns the participant row whatever pool it is in.
	GetParticipantHealthForAccount(ctx context.Context, accountID uuid.UUID) (*models.WarmupParticipantHealth, error)
	// GetParticipantHealthForAccounts is the batched form: one pool row per
	// mailbox in the set, keyed by account id.
	GetParticipantHealthForAccounts(ctx context.Context, accountIDs []uuid.UUID) (map[uuid.UUID]*models.WarmupParticipantHealth, error)
	// GetKnownHealthState is GetHealthState over real evidence, plus whether Cloud's evidence is unknown.
	GetKnownHealthState(ctx context.Context, accountID uuid.UUID) (models.WarmupHealthState, bool, error)
	// GetCloudStanding is the standing Warmbly Cloud last reported for a
	// mailbox it warms; nil for any other mailbox.
	GetCloudStanding(ctx context.Context, accountID uuid.UUID) (*models.WarmupHealthInfo, error)
	// UpdateParticipantHealth returns the row as written, or nil when the
	// review-required hold kept it (or the mailbox is in no pool). PoolType is
	// not on the returned row; the caller has it.
	UpdateParticipantHealth(ctx context.Context, accountID uuid.UUID, state models.WarmupHealthState, blockedUntil *time.Time, reason string, score float64) (*models.WarmupParticipantHealth, error)
	// ListParticipantHealth is every participant row, stalest evaluation first,
	// so a sweep cut off by its deadline resumes where it left off.
	ListParticipantHealth(ctx context.Context) ([]models.WarmupParticipantHealth, error)
	// HealthMetricCounts is every count behind a health decision in one round trip.
	HealthMetricCounts(ctx context.Context, accountID uuid.UUID, sincePlacement, since7d, since30d time.Time) (models.WarmupHealthCounts, error)
	// CountWarmupSpamReportsSince: one scan; placements (the provider filed it) and complaints (the recipient did) apart.
	CountWarmupSpamReportsSince(ctx context.Context, accountID uuid.UUID, since time.Time) (placements, complaints int, err error)
	// ColdRampStateForAccounts returns a whole candidate pool's graduation
	// inputs in one round trip. The scheduler reads this per pass, so it must
	// not be per-account.
	ColdRampStateForAccounts(ctx context.Context, accountIDs []uuid.UUID, since time.Time) (map[uuid.UUID]ColdRampState, error)
	// StampColdRampStart records a mailbox's first cold send. Idempotent: a
	// mailbox that already has an anchor keeps it.
	StampColdRampStart(ctx context.Context, accountID uuid.UUID) error
	// SpamPlacementsSince lists when this sender's warmup mail was found in the
	// junk folder of a Google, Microsoft or Yahoo recipient; another host's
	// junk folder is never held against a sender. The ramp subtracts a freeze
	// window per placement, so it needs all of them, not just the newest.
	SpamPlacementsSince(ctx context.Context, accountID uuid.UUID, since time.Time) ([]time.Time, error)
	SumWarmupSentSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error)

	// Health sweep
	ListParticipantMemberships(ctx context.Context) ([]WarmupParticipantMembership, error)
	GetPoolHealthCounts(ctx context.Context) (map[string]int, float64, error)

	// Spam tracking
	RecordSpamReport(ctx context.Context, report *SpamReport) (bool, error)

	// Statistics
	IncrementDailyCount(ctx context.Context, accountID uuid.UUID, date time.Time) error
	IncrementReplyCount(ctx context.Context, accountID uuid.UUID, date time.Time) error
	// FailWarmupSend atomically records the failure and refunds its daily counters.
	FailWarmupSend(ctx context.Context, accountID, taskID uuid.UUID, date time.Time, title, message string) error
	// LastWarmupSendFailure is the newest warmup send the worker could not deliver since `since`, when no later send was confirmed delivered.
	LastWarmupSendFailure(ctx context.Context, accountID uuid.UUID, since time.Time) (*models.WarmupSendFailure, error)
	ListMailboxLoadingIncidents(ctx context.Context, now time.Time) ([]MailboxLoadingIncident, error)
	GetWarmupStatistics(ctx context.Context, accountID uuid.UUID, from, to time.Time) ([]WarmupStatistic, error)
	GetWarmupSummaries(ctx context.Context, accountIDs []uuid.UUID, from, to time.Time) (map[uuid.UUID]WarmupSummary, error)
	GetOrCreateDailyStats(ctx context.Context, accountID uuid.UUID, date time.Time, targetVolume int) (*WarmupStatistic, error)

	// Pool-wide placement analytics (admin overview)
	PoolSpamPlacementRate(ctx context.Context, since time.Time) (float64, error)
	PoolSpamPlacementsByProvider(ctx context.Context, since time.Time) (map[string]int, error)
	// SenderPlacementByHost is one SENDER's verified placement keyed by the
	// recipient's mail host (mailhost.Host), not by how that mailbox connects
	// or its domain: a custom domain can be run by anyone.
	SenderPlacementByHost(ctx context.Context, senderAccountID uuid.UUID, since time.Time) (map[string]HostPlacementStat, error)

	// Warmup token management
	CreateWarmupToken(ctx context.Context, token *models.WarmupToken) error
	GetWarmupToken(ctx context.Context, tokenID uuid.UUID) (*models.WarmupToken, error)
	FindWarmupToken(ctx context.Context, tokenID uuid.UUID) (*models.WarmupToken, error)
	ConsumeWarmupToken(ctx context.Context, tokenID uuid.UUID) error
	// RecordWarmupTokenDelivery stamps the Message-ID the provider actually put
	// on the wire onto the send's token, which is what the recipient can match
	// when the verify header did not survive delivery.
	RecordWarmupTokenDelivery(ctx context.Context, taskID uuid.UUID, messageID string) error
	// FindDeliveredWarmupToken resolves the pending token for an inbound
	// message that carries no verify header.
	FindDeliveredWarmupToken(ctx context.Context, recipientAccountID uuid.UUID, senderAddress, messageID, subject string) (*models.WarmupToken, error)
	// IsWarmupDelivery answers the same question for a second reader of the
	// same mailbox, which must not depend on who consumed the token first.
	IsWarmupDelivery(ctx context.Context, accountID uuid.UUID, senderAddress, messageID, subject string) (bool, error)
	// IsWarmupThreadReply recognises a message by what it answers: any of the
	// ids in its In-Reply-To naming a warmup send or receipt of this mailbox,
	// or an earlier turn already recognised this way.
	IsWarmupThreadReply(ctx context.Context, accountID uuid.UUID, parentIDs []string) (bool, error)
	// RecordWarmupThreadMessage remembers a recognised turn so the turn
	// answering it is recognised too.
	RecordWarmupThreadMessage(ctx context.Context, accountID uuid.UUID, messageID string) error

	// Warmup conversation support
	GetRecentlyUsedPartners(ctx context.Context, accountID uuid.UUID, since time.Time) ([]uuid.UUID, error)
	GetRecentPartnerCounts(ctx context.Context, accountID uuid.UUID, since time.Time) (map[uuid.UUID]int, error)
	GetLatestReplyCandidate(ctx context.Context, senderAccountID, recipientAccountID uuid.UUID) (*WarmupReplyCandidate, error)

	// GetPoolParticipantProviders maps each participant to its provider bucket
	// (models.ClassifyProvider on the address).
	GetPoolParticipantProviders(ctx context.Context, poolType string, excludeBlocked bool) (map[uuid.UUID]string, error)
	// WarmupPartnerCandidates is everyone a sender may be paired with: its own
	// tier, plus proven free mailboxes when a premium tier is thin. The
	// scheduler caps volume on the same set the selector draws from.
	WarmupPartnerCandidates(ctx context.Context, poolType string, senderID uuid.UUID) ([]models.WarmupPartnerCandidate, error)
	WarmupPartnerCounts(ctx context.Context, poolType string, senderIDs []uuid.UUID) (map[uuid.UUID]int, error)
	// Batched per-mailbox reads for a status page; see pg_warmup_status_batch.go.
	GetPartnerDiversityForAccounts(ctx context.Context, accountIDs []uuid.UUID, since time.Time) (map[uuid.UUID]WarmupPartnerDiversity, error)
	GetCloudStandings(ctx context.Context, accountIDs []uuid.UUID) (map[uuid.UUID]*models.WarmupHealthInfo, error)
	SpamPlacementsSinceForAccounts(ctx context.Context, accountIDs []uuid.UUID, since time.Time) (map[uuid.UUID][]time.Time, error)
	SumWarmupSentSinceForAccounts(ctx context.Context, accountIDs []uuid.UUID, since time.Time) (map[uuid.UUID]int, error)
	LastWarmupSendFailures(ctx context.Context, accountIDs []uuid.UUID, since time.Time) (map[uuid.UUID]*models.WarmupSendFailure, error)
	GetRecentPartnerDomainCounts(ctx context.Context, accountID uuid.UUID, since time.Time) (map[string]int, error)
	// GetPartnerDiversity counts confirmed partners reached in the window.
	GetPartnerDiversity(ctx context.Context, accountID uuid.UUID, since time.Time) (WarmupPartnerDiversity, error)

	// Tampering protection: track delivered warmup mail so a later deletion or
	// spam-flag can be attributed, and count "harm" events per mailbox.
	RecordWarmupReceived(ctx context.Context, accountID, internalID uuid.UUID, messageID string, senderAccountID uuid.UUID, landedSpam bool) error
	GetWarmupReceived(ctx context.Context, accountID, internalID uuid.UUID) (*WarmupReceived, error)
	// Spam moves after arrival are held and attributed on the activity around them.
	RecordWarmupSpamMove(ctx context.Context, m WarmupSpamMove) (bool, error)
	ListSettledWarmupSpamMoves(ctx context.Context, settledBefore time.Time, limit int) ([]WarmupSpamMove, error)
	WarmupSpamMoveEvidence(ctx context.Context, m WarmupSpamMove) (WarmupSpamMoveEvidence, error)
	ClaimWarmupSpamMove(ctx context.Context, accountID uuid.UUID, messageID string, lease time.Duration) (bool, error)
	FixWarmupSpamMoveVerdict(ctx context.Context, accountID uuid.UUID, messageID, verdict string, signals []string) (bool, error)
	CompleteWarmupSpamMove(ctx context.Context, accountID uuid.UUID, messageID string) error
	CorrelatedOwnerSpamMoves(ctx context.Context, senderID, exceptAccountID uuid.UUID, at time.Time) ([]WarmupSpamMove, error)
	ReattributeOwnerSpamMoves(ctx context.Context, senderID, exceptAccountID uuid.UUID, at time.Time) error
	RecordOwnerActivity(ctx context.Context, accountID uuid.UUID, at time.Time) error
	RecordWarmupTampering(ctx context.Context, accountID uuid.UUID, messageID, kind string) (bool, error)
	CountWarmupTamperingSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error)
	// WithdrawWarmupTampering deletes a strike the mailbox did not earn,
	// reporting whether there was one.
	WithdrawWarmupTampering(ctx context.Context, accountID uuid.UUID, messageID, kind string) (bool, error)
	HasWarmupTampering(ctx context.Context, accountID uuid.UUID, messageID, kind string) (bool, error)
	// CountWarmupTamperingBetween is the strikes a hold decided at `to` read,
	// less one message; byAddress counts every mailbox sharing the address.
	CountWarmupTamperingBetween(ctx context.Context, accountID uuid.UUID, from, to time.Time, excludeMessageID string, byAddress bool) (deletions, spamFlags int, err error)
	// WarmupReceiptRetired reports whether retention has retired the receipt.
	WarmupReceiptRetired(ctx context.Context, accountID uuid.UUID, messageID string) (bool, error)
	// ListUnverifiedDeletions is pre-search deletion strikes nobody asked
	// about within retryAfter, on active mailboxes with a worker.
	ListUnverifiedDeletions(ctx context.Context, since time.Time, retryAfter time.Duration, limit int) ([]WarmupTamperingToVerify, error)
	MarkTamperingVerifyRequested(ctx context.Context, accountID uuid.UUID, messageID string) error
	MarkTamperingVerified(ctx context.Context, accountID uuid.UUID, messageID, kind string) error
	// GetWarmupHold is the pool row's standing, else the address's ledger row
	// when no mailbox with the address is in a pool; nil when neither exists.
	GetWarmupHold(ctx context.Context, accountID uuid.UUID) (*WarmupHold, error)
	// ReviseWarmupHold replaces the hold read by GetWarmupHold if it is
	// unchanged; a state that holds nothing clears it.
	ReviseWarmupHold(ctx context.Context, accountID uuid.UUID, hold *WarmupHold, state models.WarmupHealthState, until *time.Time, newReason string) (bool, error)

	// Retention: warmup mail is deleted from the mailbox once its window has
	// passed, the platform's own copy of the body with it, and the
	// per-message records are pruned after theirs.
	//
	// ListWarmupMailToRetire returns received copies whose window (the
	// mailbox's own, else defaultDays) has passed, oldest first, on active
	// mailboxes that have a worker to act. ListWarmupSentCopiesToRetire is
	// the same for the sender's own copy of each send. RetireWarmupReceived
	// and RetireWarmupSentCopy stamp the row once the deletion is on the bus.
	ListWarmupMailToRetire(ctx context.Context, defaultDays, limit int, skip []uuid.UUID) ([]WarmupMailToRetire, error)
	RetireWarmupReceived(ctx context.Context, accountID, internalID uuid.UUID) error
	ListWarmupSentCopiesToRetire(ctx context.Context, defaultDays, limit int, skip []uuid.UUID) ([]WarmupMailToRetire, error)
	RetireWarmupSentCopy(ctx context.Context, token uuid.UUID) error
	// PruneWarmupEventsBefore drops per-message records older than before:
	// tampering events, spam reports, retired receipts, and tokens whose sent
	// copy is gone. A receipt or token whose mail is still in the mailbox is
	// kept, so a removal seen later can still be told apart from tampering.
	// mailDays is the instance mail window, which a thread turn also outlives.
	PruneWarmupEventsBefore(ctx context.Context, before time.Time, mailDays int) (int64, error)

	// Appeals (user-facing submission; admin review lives in the admin repo).
	CreateWarmupAppeal(ctx context.Context, accountID, userID uuid.UUID, reason string) (uuid.UUID, error)
	HasPendingWarmupAppeal(ctx context.Context, accountID uuid.UUID) (bool, error)
}

type warmupRepository struct {
	db           *pgxpool.Pool
	receiptOnce  sync.Once
	receiptSlots chan struct{}
	// sessionLocks is set behind a transaction pooler; see UseSessionLocks.
	sessionLocks *pgxpool.Pool
	// partnerWeeks7d is nil in tests that build the struct directly, which
	// then read every candidate's counts exactly.
	partnerWeeks7d *partnerWeekCache
}

// NewWarmupRepository creates a new warmup repository
func NewWarmupRepository(db *pgxpool.Pool) WarmupRepository {
	return &warmupRepository{db: db, partnerWeeks7d: newPartnerWeekCache(partnerWeekTTL)}
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
		  AND ` + poolAuthoritySQL + `
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

	rows, err := resultDB(ctx, r.db).Query(ctx, query, poolType)
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

// MoveToPool conflicts on the account, not the pool (unique index, migration 000097), so a
// mailbox in the other pool moves and keeps every reputation column: changing pool cannot
// launder a penalty, and no mailbox can hold two memberships.
func (r *warmupRepository) MoveToPool(ctx context.Context, poolID, accountID uuid.UUID, role string) error {
	tx, err := beginResultTx(ctx, r.db)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var admitted bool
	err = tx.QueryRow(ctx, `SELECT ea.status = 'active' AND ea.seed_scope IS NULL
          AND (ea.test_mode IS NULL OR ea.test_mode='legacy' OR ea.test_mode='diagnostic' AND (ea.test_receive_enabled OR ea.test_send_enabled))
	AND EXISTS (SELECT 1 FROM organizations o WHERE o.id = ea.organization_id AND o.risk_state IN ('trusted', 'watch'))
	AND NOT EXISTS (SELECT 1 FROM cloud_link_mailboxes clm WHERE clm.email_account_id = ea.id)
	FROM email_accounts ea WHERE ea.id = $1 FOR UPDATE`, accountID).Scan(&admitted)
	if err != nil {
		return err
	}
	if !admitted {
		return errors.New("warmup pool authority unavailable")
	}

	// An existing member keeps everything it has; only its pool and role move,
	// which the mirror trigger ignores (000156), so its retention window holds.
	moved, err := tx.Exec(ctx, `
		UPDATE warmup_pool_participants
		   SET pool_id = $1::uuid, participant_role = $3::text
		 WHERE email_account_id = $2::uuid
	`, poolID, accountID, role)
	if err != nil {
		return err
	}
	if moved.RowsAffected() == 0 {
		// A new member starts from whatever standing its address holds and is
		// still within the retention window. health_signals_from is
		// deliberately left at its default: the history behind the old standing
		// is gone, so new signals count from now, while the verdict is kept.
		// The insert is itself a write, so the trigger re-mirrors it.
		_, err = tx.Exec(ctx, `
			INSERT INTO warmup_pool_participants
			    (pool_id, email_account_id, joined_at, participant_role,
			     health_state, blocked_at, blocked_until, blocked_reason, last_health_score, last_health_reason)
			SELECT $1::uuid, $2::uuid, NOW(), $3::text,
			       COALESCE(l.health_state, 'healthy'), l.blocked_at, l.blocked_until, l.blocked_reason,
			       COALESCE(l.last_health_score, 0), l.last_health_reason
			  FROM email_accounts a
			  LEFT JOIN warmup_reputation_ledger l
			    ON l.organization_id = a.organization_id
			   AND l.email = lower(btrim(a.email))
			   AND (l.standing_until IS NULL
			        OR GREATEST(l.standing_until, l.recorded_at) + ($4::int * interval '1 day') > now())
			 WHERE a.id = $2::uuid
			ON CONFLICT (email_account_id) DO UPDATE
			SET pool_id = EXCLUDED.pool_id,
			    participant_role = EXCLUDED.participant_role
		`, poolID, accountID, role, config.WarmupReputationLedgerDays)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *warmupRepository) PurgeExpiredReputationLedger(ctx context.Context) (int64, error) {
	// A standing that requires review (standing_until NULL) never lapses, and
	// nothing is forgotten while a live pool row still backs it.
	tag, err := resultDB(ctx, r.db).Exec(ctx, `
		DELETE FROM warmup_reputation_ledger l
		 WHERE l.standing_until IS NOT NULL
		   AND (l.cloud_health_state IS NULL OR (l.cloud_blocked_until IS NOT NULL AND l.cloud_blocked_until + ($1::int * interval '1 day') <= now()))
		   AND GREATEST(l.standing_until, l.recorded_at) + ($1::int * interval '1 day') <= now()
		   AND NOT EXISTS (
		       SELECT 1
		         FROM warmup_pool_participants p
		         JOIN email_accounts a ON a.id = p.email_account_id
		        WHERE a.organization_id = l.organization_id
		          AND lower(btrim(a.email)) = l.email)
	`, config.WarmupReputationLedgerDays)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// MoveExistingToPool corrects a member's pool and leaves its role alone, so the reconciler
// cannot promote a mailbox that was deliberately demoted to recipient_only.
func (r *warmupRepository) MoveExistingToPool(ctx context.Context, poolID, accountID uuid.UUID) (bool, error) {
	query := `
		UPDATE warmup_pool_participants
		SET pool_id = $1::uuid
		WHERE email_account_id = $2::uuid
		  AND pool_id <> $1::uuid
	`

	cmd, err := resultDB(ctx, r.db).Exec(ctx, query, poolID, accountID)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

// LeaveAllPools removes the mailbox from warmup entirely.
func (r *warmupRepository) LeaveAllPools(ctx context.Context, accountID uuid.UUID) error {
	// The standing is already mirrored by address; leaving only restarts its
	// retention window, so a mailbox that leaves on an auth error or a lapsed
	// plan and rejoins weeks later still meets the standing it left with.
	query := `
		WITH bumped AS (
			UPDATE warmup_reputation_ledger l
			   SET recorded_at = now()
			  FROM email_accounts a
			 WHERE a.id = $1
			   AND l.organization_id = a.organization_id
			   AND l.email = lower(btrim(a.email))
		)
		DELETE FROM warmup_pool_participants
		WHERE email_account_id = $1
	`

	_, err := resultDB(ctx, r.db).Exec(ctx, query, accountID)
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

	_, err := resultDB(ctx, r.db).Exec(ctx, query, reason, accountID)
	return err
}

// WarmupHealthRead is one mailbox's worst live standing across its pools.
type WarmupHealthRead struct {
	State        models.WarmupHealthState
	BlockedUntil *time.Time
	// CloudStatusOnly: the hold is only a missing Warmbly Cloud report, with no real hold beside it.
	CloudStatusOnly bool
	// CloudReportMissing: Warmbly Cloud has no current report, so a hold has no end whatever BlockedUntil says.
	CloudReportMissing bool
}

// IsPoolEligible checks current role, authority and health independently of price.
func (r *warmupRepository) IsPoolEligible(ctx context.Context, accountID uuid.UUID, poolType string, sending bool) (bool, error) {
	var ok bool
	err := resultDB(ctx, r.db).QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM warmup_pool_participants wpp
 JOIN warmup_pools wp ON wp.id = wpp.pool_id
 JOIN email_accounts ea ON ea.id = wpp.email_account_id
 WHERE ea.id = $1 AND wp.pool_type = $2
	 AND CASE WHEN $3 THEN wpp.participant_role='sender_receiver' AND `+testSenderSQL+` ELSE `+testRecipientSQL+` END
	AND `+poolEligibleSQL+`)`, accountID, poolType, sending).Scan(&ok)
	return ok, err
}

// GetHealthStates is GetHealthState over a pool: the worst standing per
// mailbox, in one query.
func (r *warmupRepository) GetHealthStates(ctx context.Context, accountIDs []uuid.UUID) (map[uuid.UUID]WarmupHealthRead, error) {
	out := make(map[uuid.UUID]WarmupHealthRead, len(accountIDs))
	for _, id := range accountIDs {
		out[id] = WarmupHealthRead{State: models.WarmupHealthHealthy}
	}
	if len(accountIDs) == 0 {
		return out, nil
	}
	query := `
		SELECT a.id, h.health_state, h.blocked_until, h.cloud_only, h.cloud_missing
		  FROM unnest($1::uuid[]) AS a(id)
		  CROSS JOIN LATERAL (` + warmupGateStandingSQL("a.id") + `
		  ) h
	`
	rows, err := resultDB(ctx, r.db).Query(ctx, query, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var state string
		var until *time.Time
		var cloudOnly, cloudMissing bool
		if err := rows.Scan(&id, &state, &until, &cloudOnly, &cloudMissing); err != nil {
			return nil, err
		}
		out[id] = WarmupHealthRead{State: models.WarmupHealthState(state), BlockedUntil: until, CloudStatusOnly: cloudOnly, CloudReportMissing: cloudMissing}
	}
	return out, rows.Err()
}

// GetHealthState returns the account's warmup health state and blocked_until without the
// caller naming a pool. The ordering keeps the worst state winning.
func (r *warmupRepository) GetHealthState(ctx context.Context, accountID uuid.UUID) (models.WarmupHealthState, *time.Time, error) {
	query := `SELECT health_state, blocked_until FROM (` + warmupGateStandingSQL("$1::uuid") + `
	) h`
	var state string
	var blockedUntil *time.Time
	err := resultDB(ctx, r.db).QueryRow(ctx, query, accountID).Scan(&state, &blockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return models.WarmupHealthHealthy, nil, nil
	}
	if err != nil {
		return models.WarmupHealthHealthy, nil, err
	}
	return models.WarmupHealthState(state), blockedUntil, nil
}

func (r *warmupRepository) GetKnownHealthState(ctx context.Context, accountID uuid.UUID) (models.WarmupHealthState, bool, error) {
	var state *string
	var unknown bool
	err := resultDB(ctx, r.db).QueryRow(ctx, `SELECT (SELECT k.health_state FROM (`+warmupKnownStandingSQL("$1::uuid")+`) k), `+cloudStandingUnknownSQL("$1::uuid"), accountID).Scan(&state, &unknown)
	if err != nil {
		return models.WarmupHealthHealthy, false, err
	}
	if state == nil {
		return models.WarmupHealthHealthy, unknown, nil
	}
	return models.WarmupHealthState(*state), unknown, nil
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

	_, err := resultDB(ctx, r.db).Exec(ctx, query, accountID)
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
	err := resultDB(ctx, r.db).QueryRow(ctx, query, accountID, poolType).Scan(&exists)
	return exists, err
}

// RecordSpamReport records a spam report
func (r *warmupRepository) RecordSpamReport(ctx context.Context, report *SpamReport) (bool, error) {
	query := `
		INSERT INTO warmup_spam_reports (id, reporter_account_id, reported_account_id, message_id, report_type, content_source, recipient_provider, recipient_domain, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (reporter_account_id, message_id) DO NOTHING
	`

	cmd, err := resultDB(ctx, r.db).Exec(ctx, query,
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

// participantHealthColumns is the row every reader below scans; the account
// readers pin it with participantHealthWhere, the listing orders it instead.
const participantHealthColumns = `
		SELECT
			wpp.pool_id,
			wp.pool_type,
			wpp.email_account_id,
			wpp.joined_at,
			wpp.blocked_at,
			wpp.blocked_until,
			wpp.blocked_reason,
			wpp.health_state,
			wpp.last_health_score,
			wpp.last_health_reason,
			wpp.last_health_evaluated_at,
			wpp.health_signals_from
		FROM warmup_pool_participants wpp
		JOIN warmup_pools wp ON wp.id = wpp.pool_id`

const participantHealthSelect = participantHealthColumns + `
		WHERE wpp.email_account_id = $1`

func (r *warmupRepository) GetCloudStanding(ctx context.Context, accountID uuid.UUID) (*models.WarmupHealthInfo, error) {
	m, err := scanCloudLinkMailbox(resultDB(ctx, r.db).QueryRow(ctx,
		`SELECT `+cloudLinkMailboxColumns+` FROM cloud_link_mailboxes WHERE email_account_id = $1`, accountID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c cloudStatusRow
	if err := resultDB(ctx, r.db).QueryRow(ctx, `SELECT `+cloudStatusSelect("$1::uuid")+`
		  FROM (SELECT 1) one LEFT JOIN LATERAL (`+warmupKnownStandingSQL("$1::uuid")+`) k ON true`, accountID).Scan(c.dest()...); err != nil {
		return nil, err
	}
	return c.status(m, time.Now()), nil
}

// GetParticipantHealthForAccount returns the participant row from whichever pool the mailbox
// is in. Exact because a mailbox is in at most one (migration 000097).
func (r *warmupRepository) GetParticipantHealthForAccount(ctx context.Context, accountID uuid.UUID) (*models.WarmupParticipantHealth, error) {
	return r.scanParticipantHealth(resultDB(ctx, r.db).QueryRow(ctx, participantHealthSelect, accountID))
}

// GetParticipantHealthForAccounts is the batched form of
// GetParticipantHealthForAccount: the single pool row for each mailbox in the
// set, keyed by account id, in one query. A mailbox in no pool is absent.
func (r *warmupRepository) GetParticipantHealthForAccounts(ctx context.Context, accountIDs []uuid.UUID) (map[uuid.UUID]*models.WarmupParticipantHealth, error) {
	out := make(map[uuid.UUID]*models.WarmupParticipantHealth, len(accountIDs))
	if len(accountIDs) == 0 {
		return out, nil
	}

	query := participantHealthColumns + `
			WHERE wpp.email_account_id = ANY($1::uuid[])`

	rows, err := resultDB(ctx, r.db).Query(ctx, query, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		h, err := r.scanParticipantHealth(rows)
		if err != nil {
			return nil, err
		}
		if h != nil {
			out[h.EmailAccountID] = h
		}
	}
	return out, rows.Err()
}

func (r *warmupRepository) GetParticipantHealth(ctx context.Context, accountID uuid.UUID, poolType string) (*models.WarmupParticipantHealth, error) {
	query := participantHealthSelect + `
		  AND wp.pool_type = $2
		LIMIT 1
	`

	return r.scanParticipantHealth(resultDB(ctx, r.db).QueryRow(ctx, query, accountID, poolType))
}

func (r *warmupRepository) scanParticipantHealth(row pgx.Row) (*models.WarmupParticipantHealth, error) {
	var out models.WarmupParticipantHealth
	var state string
	if err := row.Scan(
		&out.PoolID,
		&out.PoolType,
		&out.EmailAccountID,
		&out.JoinedAt,
		&out.BlockedAt,
		&out.BlockedUntil,
		&out.BlockedReason,
		&state,
		&out.LastHealthScore,
		&out.LastHealthReason,
		&out.LastHealthEvaluatedAt,
		&out.HealthSignalsFrom,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	out.HealthState = models.WarmupHealthState(state)
	return &out, nil
}

func (r *warmupRepository) UpdateParticipantHealth(ctx context.Context, accountID uuid.UUID, state models.WarmupHealthState, blockedUntil *time.Time, reason string, score float64) (*models.WarmupParticipantHealth, error) {
	// Every parameter is cast explicitly. Left bare, Postgres deduced $1 as
	// `character varying` from the health_state assignment and as `text` from the
	// equality tests, and could not deduce $2 at all from IS NULL / IS DISTINCT
	// FROM. It refused the whole statement with 42P08, so this UPDATE never ran
	// for any account in any pool and no warmup health state was ever persisted
	// (issue #195). Keep the casts.
	//
	// A block is a sentence, not a reading. The bands read windows far shorter
	// than the terms they hand out (seven days of placement against a 30-day
	// block), and a re-added mailbox arrives with no history at all, so a
	// decision from fresh metrics must not lower a quarantine or a block while
	// its blocked_until is in the future; a decision at least as severe applies,
	// and one of equal severity keeps the later end so a 90-day term is not cut
	// to 30 by a milder reading. A held sentence also keeps the reading that
	// produced it: a blocked mailbox stops warming, so the next sweep sees an
	// empty sample, and overwriting the score and reason left the only
	// explanation of the block blank. Throttled is not floored: the docs
	// promise it lifts on recovery. Deciding it here, against the row as it is
	// at write time, is what keeps an admin unblock that lands mid-sweep from
	// being overwritten by the block the sweep read a moment earlier.
	query := `
		WITH cur AS (
			SELECT email_account_id, health_state, blocked_until,
			       CASE health_state
			           WHEN 'blocked' THEN 5 WHEN 'quarantined' THEN 4 WHEN 'throttled' THEN 3
			           WHEN 'watch' THEN 2 WHEN 'healthy' THEN 1 ELSE 0 END AS cur_rank,
			       CASE $1::text
			           WHEN 'blocked' THEN 5 WHEN 'quarantined' THEN 4 WHEN 'throttled' THEN 3
			           WHEN 'watch' THEN 2 WHEN 'healthy' THEN 1 ELSE 0 END AS new_rank
			  FROM warmup_pool_participants
			 WHERE email_account_id = $5::uuid
		),
		eff AS (
			SELECT email_account_id,
			       (blocked_until IS NOT NULL AND blocked_until > now() AND cur_rank >= 4 AND new_rank < cur_rank) AS held,
			       CASE WHEN blocked_until IS NOT NULL AND blocked_until > now() AND cur_rank >= 4 AND new_rank < cur_rank
			            THEN health_state ELSE $1::text END AS state,
			       CASE WHEN blocked_until IS NOT NULL AND blocked_until > now() AND cur_rank >= 4 AND new_rank < cur_rank
			            THEN blocked_until
			            WHEN new_rank = cur_rank AND cur_rank >= 4 AND blocked_until IS NOT NULL AND $2::timestamptz IS NOT NULL
			            THEN GREATEST(blocked_until, $2::timestamptz)
			            ELSE $2::timestamptz END AS until
			  FROM cur
		)
		UPDATE warmup_pool_participants p
		SET
			health_state = eff.state,
			blocked_until = eff.until,
			-- Only a quarantine or block takes a mailbox out of the pool; a throttle's term is not one.
			blocked_at = CASE
				WHEN eff.state NOT IN ('quarantined', 'blocked') THEN NULL
				WHEN eff.until IS NOT NULL AND (p.blocked_at IS NULL OR p.blocked_until IS DISTINCT FROM eff.until) THEN NOW()
				WHEN eff.until IS NULL AND p.blocked_until IS NOT NULL THEN NULL
				ELSE p.blocked_at
			END,
			blocked_reason = CASE
				WHEN eff.held THEN p.blocked_reason
				WHEN eff.until IS NOT NULL OR eff.state = 'blocked' THEN $3::text
				WHEN eff.state = 'healthy' THEN NULL
				ELSE COALESCE($3::text, p.blocked_reason)
			END,
			last_health_score = CASE WHEN eff.held THEN p.last_health_score ELSE $4::double precision END,
			last_health_reason = CASE WHEN eff.held THEN p.last_health_reason ELSE NULLIF($3::text, '') END,
			last_health_evaluated_at = NOW()
		FROM eff
		WHERE p.email_account_id = eff.email_account_id
		  AND NOT (p.blocked_at IS NOT NULL AND p.blocked_until IS NULL AND p.health_state = 'blocked')
		RETURNING p.pool_id, '', p.email_account_id, p.joined_at, p.blocked_at, p.blocked_until, p.blocked_reason,
		          p.health_state, p.last_health_score, p.last_health_reason,
		          p.last_health_evaluated_at, p.health_signals_from
	`
	// The RETURNING list is participantHealthSelect's shape with an empty pool
	// type, so the standing the floor decided comes back in the write's trip.
	return r.scanParticipantHealth(resultDB(ctx, r.db).QueryRow(ctx, query, state, blockedUntil, reason, score, accountID))
}

// ListParticipantHealth: stalest first, so a deadline is pacing, not a blind spot.
func (r *warmupRepository) ListParticipantHealth(ctx context.Context) ([]models.WarmupParticipantHealth, error) {
	rows, err := resultDB(ctx, r.db).Query(ctx, participantHealthColumns+`
		ORDER BY wpp.last_health_evaluated_at ASC NULLS FIRST, wpp.email_account_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.WarmupParticipantHealth
	for rows.Next() {
		h, err := r.scanParticipantHealth(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *h)
	}
	return out, rows.Err()
}

// Bounces here are hard bounces: one recorded as a full mailbox, a block or a
// temporary failure is not about the address and stays out of the rate.
const healthFeedbackCountsSQL = `SELECT
	COUNT(*) FILTER (WHERE de.event_type = 'complaint') AS complaints,
	COUNT(*) FILTER (WHERE de.event_type = 'bounce'
		AND COALESCE(de.metadata->>'bounce_cause', '') NOT IN ('mailbox_full', 'reputation_block', 'transient')) AS bounces
	FROM deliverability_events de JOIN tasks t ON t.id = de.task_id
	WHERE t.email_account_id = $1 AND de.created_at >= $3 AND de.event_type IN ('complaint', 'bounce')`

const healthTamperingCountsSQL = `SELECT
	COUNT(*) FILTER (WHERE kind = 'deletion') AS deletions,
	COUNT(*) FILTER (WHERE kind = 'spam_flag') AS spam_flags
	FROM warmup_tampering_events WHERE email_account_id = $1 AND created_at >= $2`

// HealthMetricCounts shares scans only between signals with identical predicates.
func (r *warmupRepository) HealthMetricCounts(ctx context.Context, accountID uuid.UUID, sincePlacement, since7d, since30d time.Time) (models.WarmupHealthCounts, error) {
	query := `
		SELECT
			(SELECT COALESCE(SUM(emails_sent), 0) FROM warmup_statistics
			  WHERE email_account_id = $1 AND date >= DATE($2)),
			(SELECT COUNT(*) FROM warmup_spam_reports
			  WHERE reported_account_id = $1 AND created_at >= $2 AND report_type = 'spam_placement'),
			(SELECT COUNT(*) FROM warmup_spam_reports
			  WHERE reported_account_id = $1 AND created_at >= $2 AND report_type IN ('user_complaint', 'spam', 'spam_folder')),
			feedback.complaints, feedback.bounces,
			(SELECT COUNT(*) FROM tasks
			  WHERE email_account_id = $1 AND status = 'completed' AND completed_at >= $3),
			tampering.deletions, tampering.spam_flags,
			placed.major, placed.major_spam, placed.other, placed.other_spam
		FROM (` + placementEvidenceSQL("$1", "$4") + `) placed
		CROSS JOIN (` + healthFeedbackCountsSQL + `) feedback
		CROSS JOIN (` + healthTamperingCountsSQL + `) tampering
	`
	var c models.WarmupHealthCounts
	p := &c.Placement
	err := resultDB(ctx, r.db).QueryRow(ctx, query, accountID, since7d, since30d, sincePlacement).Scan(
		&c.SentLast7d, &c.SpamPlacementsLast7d, &c.UserComplaintsLast7d,
		&c.ComplaintsLast30d, &c.BouncesLast30d, &c.DeliveredLast30d,
		&c.DeletionsLast7d, &c.SpamFlagsLast7d,
		&p.MajorDelivered, &p.MajorSpam, &p.OtherDelivered, &p.OtherSpam)
	return c, err
}

// CountWarmupSpamReportsSince: the two signals have their own thresholds, so they come back apart.
func (r *warmupRepository) CountWarmupSpamReportsSince(ctx context.Context, accountID uuid.UUID, since time.Time) (placements, complaints int, err error) {
	query := `
		SELECT
			COUNT(*) FILTER (WHERE report_type = 'spam_placement'),
			COUNT(*) FILTER (WHERE report_type IN ('user_complaint', 'spam', 'spam_folder'))
		FROM warmup_spam_reports
		WHERE reported_account_id = $1
		  AND created_at >= $2
		  AND report_type IN ('spam_placement', 'user_complaint', 'spam', 'spam_folder')
	`
	err = resultDB(ctx, r.db).QueryRow(ctx, query, accountID, since).Scan(&placements, &complaints)
	return placements, complaints, err
}

// ColdRampState is one mailbox's warmup-to-cold graduation inputs.
type ColdRampState struct {
	ConfirmedReplies  int
	WarmupStartedAt   *time.Time
	ColdRampStartedAt *time.Time
	// WarmupStoppedAt is set only in the Stop state (test_mode off); a pause,
	// even of a stopped mailbox, or a status-only stamp does not count.
	WarmupStoppedAt *time.Time
	Placements      []time.Time
}

// CeilingApplies is the one gate every reader of the cold ceiling asks: a
// mailbox that warmed, unless its latest warmup lifecycle event is a stop.
func (s ColdRampState) CeilingApplies() bool {
	if s.WarmupStartedAt == nil {
		return false
	}
	return s.WarmupStoppedAt == nil || s.WarmupStoppedAt.Before(*s.WarmupStartedAt)
}

func (s ColdRampState) WithKnownWarmup(startedAt *time.Time) ColdRampState {
	if s.WarmupStartedAt == nil {
		s.WarmupStartedAt = startedAt
	}
	return s
}

func (r *warmupRepository) ColdRampStateForAccounts(ctx context.Context, accountIDs []uuid.UUID, since time.Time) (map[uuid.UUID]ColdRampState, error) {
	out := make(map[uuid.UUID]ColdRampState, len(accountIDs))
	if len(accountIDs) == 0 {
		return out, nil
	}

	rows, err := resultDB(ctx, r.db).Query(ctx, `
		SELECT id, warmup, cold_ramp_started_at,
		       CASE WHEN test_mode='off' THEN warmup_stopped_at END
		FROM email_accounts
		WHERE id = ANY($1::uuid[])
	`, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var state ColdRampState
		if err := rows.Scan(&id, &state.WarmupStartedAt, &state.ColdRampStartedAt, &state.WarmupStoppedAt); err != nil {
			return nil, err
		}
		out[id] = state
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	placementRows, err := resultDB(ctx, r.db).Query(ctx, `
		SELECT reported_account_id, created_at
		FROM warmup_spam_reports sr
		WHERE reported_account_id = ANY($1::uuid[])
		  AND report_type = 'spam_placement'
		  AND created_at >= $2
		  AND `+majorRecipientSQL+`
		ORDER BY created_at
	`, accountIDs, since)
	if err != nil {
		return nil, err
	}
	defer placementRows.Close()
	for placementRows.Next() {
		var id uuid.UUID
		var at time.Time
		if err := placementRows.Scan(&id, &at); err != nil {
			return nil, err
		}
		state := out[id]
		state.Placements = append(state.Placements, at)
		out[id] = state
	}
	if err := placementRows.Err(); err != nil {
		return nil, err
	}
	placementRows.Close()
	feedback, err := resultDB(ctx, r.db).Query(ctx, `SELECT t.email_account_id,COUNT(DISTINCT p.contact_id) FILTER(WHERE p.replied_at>NOW()-INTERVAL '7 days' AND p.bounced_at IS NULL AND p.complained_at IS NULL),
	 MAX(p.bounced_at),MAX(p.complained_at)
	 FROM campaign_contact_progress p JOIN tasks t ON t.id=p.dispatch_task_id
	 WHERE t.email_account_id=ANY($1::uuid[]) AND t.task_type='campaign' AND p.sent_at IS NOT NULL
	 AND p.sent_at>NOW()-INTERVAL '7 days' GROUP BY t.email_account_id`, accountIDs)
	if err != nil {
		return nil, err
	}
	defer feedback.Close()
	for feedback.Next() {
		var id uuid.UUID
		var replies int
		var bounced, complained *time.Time
		if err := feedback.Scan(&id, &replies, &bounced, &complained); err != nil {
			return nil, err
		}
		state := out[id]
		state.ConfirmedReplies = replies
		if bounced != nil {
			state.Placements = append(state.Placements, *bounced)
		}
		if complained != nil {
			state.Placements = append(state.Placements, *complained)
			state.ConfirmedReplies = 0
		}
		out[id] = state
	}
	return out, feedback.Err()
}

func (r *warmupRepository) StampColdRampStart(ctx context.Context, accountID uuid.UUID) error {
	_, err := resultDB(ctx, r.db).Exec(ctx, `
		UPDATE email_accounts
		   SET cold_ramp_started_at = NOW()
		 WHERE id = $1 AND cold_ramp_started_at IS NULL
	`, accountID)
	return err
}

func (r *warmupRepository) SpamPlacementsSince(ctx context.Context, accountID uuid.UUID, since time.Time) ([]time.Time, error) {
	rows, err := resultDB(ctx, r.db).Query(ctx, `
		SELECT created_at
		FROM warmup_spam_reports sr
		WHERE reported_account_id = $1
		  AND report_type = 'spam_placement'
		  AND created_at >= $2
		  AND `+majorRecipientSQL+`
		ORDER BY created_at
	`, accountID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []time.Time
	for rows.Next() {
		var at time.Time
		if err := rows.Scan(&at); err != nil {
			return nil, err
		}
		out = append(out, at)
	}
	return out, rows.Err()
}

func (r *warmupRepository) SumWarmupSentSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error) {
	query := `
		SELECT COALESCE(SUM(emails_sent), 0)
		FROM warmup_statistics
		WHERE email_account_id = $1
		  AND date >= DATE($2)
	`
	var total int
	err := resultDB(ctx, r.db).QueryRow(ctx, query, accountID, since).Scan(&total)
	return total, err
}

// IncrementDailyCount increments the daily email count for warmup
func (r *warmupRepository) IncrementDailyCount(ctx context.Context, accountID uuid.UUID, date time.Time) error {
	query := `
		INSERT INTO warmup_statistics (email_account_id, date, emails_sent, target_volume)
		VALUES ($1, ($2::timestamptz AT TIME ZONE 'UTC')::date, 1, 0)
		ON CONFLICT (email_account_id, date)
		DO UPDATE SET emails_sent = warmup_statistics.emails_sent + 1
	`

	_, err := resultDB(ctx, r.db).Exec(ctx, query, accountID, date)
	return err
}

// IncrementReplyCount increments the daily warmup reply count. Upserts the row
// so it is order-independent with IncrementDailyCount (either may run first).
func (r *warmupRepository) IncrementReplyCount(ctx context.Context, accountID uuid.UUID, date time.Time) error {
	query := `
		INSERT INTO warmup_statistics (email_account_id, date, emails_sent, emails_replied, target_volume)
		VALUES ($1, ($2::timestamptz AT TIME ZONE 'UTC')::date, 0, 1, 0)
		ON CONFLICT (email_account_id, date)
		DO UPDATE SET emails_replied = warmup_statistics.emails_replied + 1
	`
	_, err := resultDB(ctx, r.db).Exec(ctx, query, accountID, date)
	return err
}

// FailWarmupSend makes the task transition and counter refund one retryable write.
func (r *warmupRepository) FailWarmupSend(ctx context.Context, accountID, taskID uuid.UUID, _ time.Time, title, message string) error {
	tx, err := beginResultTx(ctx, r.db)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockSend(ctx, tx, taskID, accountID); err != nil {
		return err
	}

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status::text
		FROM tasks
		WHERE id = $1 AND email_account_id = $2 AND task_type = 'warmup'
		FOR UPDATE
	`, taskID, accountID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status != "completed" && status != "dead_lettered" {
		return tx.Commit(ctx)
	}

	if err = refundWarmupCharge(ctx, tx, taskID, accountID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE tasks SET status = 'failed', updated_at = NOW() WHERE id = $1`, taskID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO task_failures (task_id, title, message)
		VALUES ($1, $2, $3)
		ON CONFLICT (task_id) DO UPDATE
		SET title = EXCLUDED.title, message = EXCLUDED.message
	`, taskID, title, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func refundWarmupCharge(ctx context.Context, tx pgx.Tx, taskID, accountID uuid.UUID) error {
	_, err := tx.Exec(ctx, `WITH refunded AS (UPDATE warmup_tasks w SET warmup_refunded_at=NOW()
		FROM tasks t WHERE w.task_id=t.id AND t.id=$1 AND t.email_account_id=$2 AND t.task_type='warmup'
		AND w.warmup_charged_date IS NOT NULL AND w.warmup_reply_charged IS NOT NULL AND w.warmup_refunded_at IS NULL
		RETURNING w.warmup_charged_date,w.warmup_reply_charged)
		UPDATE warmup_statistics s SET emails_sent=GREATEST(s.emails_sent-1,0),
			emails_replied=GREATEST(s.emails_replied-CASE WHEN r.warmup_reply_charged THEN 1 ELSE 0 END,0)
		FROM refunded r WHERE s.email_account_id=$2 AND s.date=r.warmup_charged_date`, taskID, accountID)
	return err
}

func (r *warmupRepository) LastWarmupSendFailure(ctx context.Context, accountID uuid.UUID, since time.Time) (*models.WarmupSendFailure, error) {
	f := &models.WarmupSendFailure{}
	var first time.Time
	err := resultDB(ctx, r.db).QueryRow(ctx, `
		SELECT message, at, first_at FROM (`+warmupSendFailuresSQL+`) f
		WHERE (message LIKE $2 AND first_at <= $3 - INTERVAL '1 hour' AND at > $3 - INTERVAL '1 hour'
		       AND status = 'active' AND warming AND sending)
		   OR (message NOT LIKE $2 AND at >= $4)
	`, accountID, mailboxLoadingPattern, time.Now(), since).
		Scan(&f.Message, &f.At, &first)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(f.Message, models.MailboxNotLoadedPrefix) {
		f.Kind = models.WarmupFailureLoading
		f.FirstFailureAt = &first
	}
	return f, nil
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
	if err := resultDB(ctx, r.db).QueryRow(ctx, query, since).Scan(&placements, &sent); err != nil {
		return 0, err
	}
	if sent == 0 {
		return 0, nil
	}
	return float64(placements) / float64(sent) * 100, nil
}

// PoolSpamPlacementsByProvider counts spam placements in the window keyed by who
// runs the recipient's mail, not the stored connect method.
func (r *warmupRepository) PoolSpamPlacementsByProvider(ctx context.Context, since time.Time) (map[string]int, error) {
	query := `
		SELECT recipient_domain, COUNT(*)
		FROM warmup_spam_reports
		WHERE report_type = 'spam_placement' AND created_at >= $1
		GROUP BY 1
	`
	rows, err := resultDB(ctx, r.db).Query(ctx, query, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var domain string
		var n int
		if err := rows.Scan(&domain, &n); err != nil {
			return nil, err
		}
		// A domainless row belongs to no provider, so it stays out of custom.
		key := "unknown"
		if domain != "" {
			key = string(models.ClassifyProvider(domain))
		}
		out[key] += n
	}
	return out, rows.Err()
}

// HostPlacementStat is a sender's warmup record at one recipient mail host:
// how many arrivals were verified there, and how many of those landed in junk.
type HostPlacementStat struct {
	Delivered int
	Spam      int
}

// Rate is the share of deliveries that landed in junk, 0 when nothing arrived.
func (p HostPlacementStat) Rate() float64 {
	if p.Delivered <= 0 {
		return 0
	}
	return float64(p.Spam) / float64(p.Delivered)
}

func (r *warmupRepository) SenderPlacementByHost(ctx context.Context, senderAccountID uuid.UUID, since time.Time) (map[string]HostPlacementStat, error) {
	// Read from the rollup the placement dashboard reads, so the selector and
	// the charts cannot disagree. A row with no host is unattributable.
	rows, err := resultDB(ctx, r.db).Query(ctx, `
		SELECT recipient_host, SUM(inbox + tabs + spam)::int, SUM(spam)::int
		FROM warmup_placement_daily
		WHERE sender_account_id = $1 AND date >= ($2::timestamptz AT TIME ZONE 'UTC')::date
		  AND recipient_host <> ''
		GROUP BY 1
	`, senderAccountID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]HostPlacementStat)
	for rows.Next() {
		var host string
		var stat HostPlacementStat
		if err := rows.Scan(&host, &stat.Delivered, &stat.Spam); err != nil {
			return nil, err
		}
		out[host] = stat
	}
	return out, rows.Err()
}

// The inbound cap's three numbers as SQL literals, for send admission's copy
// of the rule partnerInboundCap applies to candidates.
var (
	inboundDailyFloorSQL    = strconv.Itoa(config.WarmupInboundDailyFloor)
	inboundDailyCeilingSQL  = strconv.Itoa(config.WarmupInboundDailyCeiling)
	inboundDailyMultipleSQL = strconv.Itoa(config.WarmupInboundDailyMultiple)
)

// partnerEligibleSQL is the predicate for a recipient the draw may reach: an
// active mailbox that receives, in a standing that is live, or an expired
// quarantine or block that the gate re-evaluates.
var poolAuthoritySQL = `
          ea.status = 'active' AND ea.seed_scope IS NULL
          AND wpp.participant_role IN ('sender_receiver', 'recipient_only')
          AND EXISTS (SELECT 1 FROM organizations o WHERE o.id = ea.organization_id
                      AND o.risk_state IN ('trusted', 'watch'))
          AND NOT EXISTS (SELECT 1 FROM cloud_link_mailboxes clm WHERE clm.email_account_id = ea.id)
          AND NOT EXISTS (SELECT 1 FROM warmup_reputation_ledger l
              WHERE l.organization_id = ea.organization_id AND l.email = lower(btrim(ea.email))
              AND l.cloud_health_state IN ('quarantined', 'blocked')
              AND (l.cloud_blocked_until IS NULL OR l.cloud_blocked_until > NOW()))
          AND ` + selfExecutionGateSQL("ea.id")

const testSenderSQL = `(ea.test_mode IS NULL OR ea.test_mode='legacy' OR ea.test_mode='diagnostic' AND ea.test_send_enabled)`
const testReceiveFlagsSQL = `(ea.test_mode IS NULL OR ea.test_mode='legacy' OR ea.test_mode='diagnostic' AND ea.test_receive_enabled)`

// testRecipientSQL is models.Email.WarmupReceivingAllowed: a mailbox whose warmup is off receives no warmup mail.
const testRecipientSQL = `(` + testReceiveFlagsSQL + ` AND ea.warmup IS NOT NULL AND ea.warmup_paused_at IS NULL)`

var partnerEligibleSQL = poolEligibleSQL + ` AND ` + testRecipientSQL

// arrivedRecipientSQL vouches for mail already delivered, so it reads the receive flags alone.
var arrivedRecipientSQL = poolEligibleSQL + ` AND ` + testReceiveFlagsSQL

var poolEligibleSQL = poolAuthoritySQL + ` AND
		  wpp.participant_role IN ('sender_receiver', 'recipient_only')
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
		  )`

// partnerProvenSQL is what "proven" means on both sides of a cross-tier
// exchange: healthy now, never blocked, a member for the minimum age, and in a
// workspace in good standing. A pool move keeps joined_at, so the risk check
// is what keeps a demoted mailbox out.
const partnerProvenSQL = `
		  wpp.health_state = 'healthy'
		  AND wpp.blocked_at IS NULL
		  AND wpp.joined_at <= NOW() - make_interval(days => $2)
		  AND o.risk_state NOT IN ('restricted', 'suspended')`

// partnerCandidateColumns is what every cohort query selects, in the order
// partnerCohort scans it.
const partnerCandidateColumns = `
		SELECT wpp.email_account_id AS id, ea.email, ea.organization_id, ea.provider::text AS provider, ea.mail_host`

// WarmupPartnerCandidates includes qualified cross-tier partners, already inbound-capped.
func (r *warmupRepository) WarmupPartnerCandidates(ctx context.Context, poolType string, senderID uuid.UUID) ([]models.WarmupPartnerCandidate, error) {
	// Eligibility and the sender's workspace in one round trip.
	var eligible, senderExecuted bool
	var senderOrg *uuid.UUID
	err := resultDB(ctx, r.db).QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM warmup_pool_participants wpp
 JOIN warmup_pools wp ON wp.id = wpp.pool_id
 JOIN email_accounts ea ON ea.id = wpp.email_account_id
 WHERE ea.id = $1 AND wp.pool_type = $2
	 AND wpp.participant_role='sender_receiver' AND `+testSenderSQL+`
	AND `+poolEligibleSQL+`), (SELECT organization_id FROM email_accounts WHERE id = $1), `+instanceExecutedSQL("$1::uuid"), senderID, poolType).
		Scan(&eligible, &senderOrg, &senderExecuted)
	if err != nil || !eligible {
		return nil, err
	}
	// Two instance-executed mailboxes never pair: neither side of the exchange would be observed by Cloud.
	recipientSQL := partnerEligibleSQL
	if senderExecuted {
		recipientSQL += ` AND NOT ` + instanceExecutedSQL("ea.id")
	}

	cands, err := r.partnerCohort(ctx, partnerCandidateColumns+`
		FROM warmup_pool_participants wpp
		JOIN warmup_pools wp ON wpp.pool_id = wp.id
		JOIN email_accounts ea ON ea.id = wpp.email_account_id
		WHERE wp.pool_type = $1
		  AND wpp.email_account_id <> $2
		  AND `+recipientSQL,
		poolType, models.WarmupPartnerOwnTier, poolType, senderID)
	if err != nil {
		return nil, err
	}

	if borrowFrom, ok := models.WarmupPoolBorrowsFrom(poolType); ok {
		borrowed, err := r.partnerCohort(ctx, partnerCandidateColumns+`
			FROM warmup_pool_participants wpp
			JOIN warmup_pools wp ON wpp.pool_id = wp.id
			JOIN email_accounts ea ON ea.id = wpp.email_account_id
			JOIN organizations o ON o.id = ea.organization_id
			WHERE wp.pool_type = $1
			  AND ea.organization_id IS DISTINCT FROM $3
			  AND `+recipientSQL+`
			  AND `+partnerProvenSQL,
			borrowFrom, models.WarmupPartnerBorrowed, borrowFrom, config.WarmupPoolFallbackMinAgeDays, senderOrg)
		if err != nil {
			return nil, err
		}
		cands = append(cands, borrowed...)
	}

	if returnTo, ok := models.WarmupPoolReturnsTo(poolType); ok {
		// Only a paying mailbox that verifiably reached this sender's inbox in
		// the window, and only while the sender itself is proven: a mailbox on
		// watch keeps warming in its own tier but stops calling on paying ones.
		returns, err := r.partnerCohort(ctx, partnerCandidateColumns+`
			FROM warmup_pool_participants wpp
			JOIN warmup_pools wp ON wpp.pool_id = wp.id
			JOIN email_accounts ea ON ea.id = wpp.email_account_id
			WHERE wp.pool_type = $1
			  AND `+recipientSQL+`
			  AND wpp.email_account_id IN (
			      SELECT wr.sender_account_id
			      FROM warmup_received wr
			      WHERE wr.email_account_id = $3
			        AND wr.created_at >= NOW() - make_interval(days => $4)
			  )
			  AND EXISTS (
			      SELECT 1
			      FROM warmup_pool_participants wpp
			      JOIN warmup_pools wp ON wpp.pool_id = wp.id
			      JOIN email_accounts ea ON ea.id = wpp.email_account_id
			      JOIN organizations o ON o.id = ea.organization_id
			      WHERE wpp.email_account_id = $3
			        AND wp.pool_type = $5
			        AND ea.status = 'active'
			        AND `+partnerProvenSQL+`
			  )`,
			returnTo, models.WarmupPartnerReturn, returnTo, config.WarmupPoolFallbackMinAgeDays, senderID, config.WarmupPoolReturnVisitDays, poolType)
		if err != nil {
			return nil, err
		}
		cands = append(cands, returns...)
	}
	return r.capPartnerCandidates(ctx, poolType, cands)
}

type warmupExecutionExclusion struct{}

// A reserved send must not consume its own recipient capacity during revalidation.
func (r *warmupRepository) WarmupExecutionCandidates(ctx context.Context, pool string, sender, task uuid.UUID) ([]models.WarmupPartnerCandidate, error) {
	return r.WarmupPartnerCandidates(context.WithValue(ctx, warmupExecutionExclusion{}, task), pool, sender)
}

// partnerCohort reads one cohort's members, uncapped; capPartnerCandidates
// applies the counts and the inbound cap to the cohorts together.
func (r *warmupRepository) partnerCohort(ctx context.Context, candidateSQL, poolType string, origin models.WarmupPartnerOrigin, args ...any) ([]models.WarmupPartnerCandidate, error) {
	rows, err := resultDB(ctx, r.db).Query(ctx, candidateSQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.WarmupPartnerCandidate
	for rows.Next() {
		c := models.WarmupPartnerCandidate{PoolType: poolType, Origin: origin}
		if err := rows.Scan(&c.ID, &c.Email, &c.OrganizationID, &c.Provider, &c.MailHost); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *warmupRepository) GetPoolParticipantProviders(ctx context.Context, poolType string, excludeBlocked bool) (map[uuid.UUID]string, error) {
	// Deliberately NOT health-filtered by default: this map only resolves a
	// candidate's provider, it never decides eligibility. A candidate missing
	// from it scores an unpenalized 1.0, so a just-unblocked partner the
	// selector re-admits would sidestep the sender's provider penalty.
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

	rows, err := resultDB(ctx, r.db).Query(ctx, query, poolType)
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
		out[id] = string(models.ClassifyProvider(domain))
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

	rows, err := resultDB(ctx, r.db).Query(ctx, query, accountID, from, to)
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
	err := resultDB(ctx, r.db).QueryRow(ctx, query, accountID, date, targetVolume).Scan(
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
		INSERT INTO warmup_tokens (token, task_id, sender_account_id, recipient_account_id, conversation_theme, content_source, conversation_id, conversation_turn, subject, expires_at, sequence_id, variant_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	_, err := resultDB(ctx, r.db).Exec(ctx, query,
		token.Token,
		token.TaskID,
		token.SenderAccountID,
		token.RecipientAccountID,
		token.ConversationTheme,
		token.ContentSource,
		token.ConversationID,
		token.ConversationTurn,
		token.Subject,
		token.ExpiresAt,
		token.SequenceID,
		token.VariantID,
	)
	return err
}

// GetWarmupToken retrieves a valid (unconsumed, unexpired) warmup token
func (r *warmupRepository) GetWarmupToken(ctx context.Context, tokenID uuid.UUID) (*models.WarmupToken, error) {
	query := `SELECT ` + warmupTokenColumns + `
		FROM warmup_tokens
		WHERE token = $1 AND consumed_at IS NULL AND expires_at > NOW()`
	return scanWarmupToken(resultDB(ctx, r.db).QueryRow(ctx, query, tokenID))
}

func (r *warmupRepository) FindWarmupToken(ctx context.Context, tokenID uuid.UUID) (*models.WarmupToken, error) {
	query := `SELECT ` + warmupTokenColumns + `
		FROM warmup_tokens
		WHERE token = $1`
	return scanWarmupToken(resultDB(ctx, r.db).QueryRow(ctx, query, tokenID))
}

// WarmupReceiptProcessor serializes recipient engagement across delivery retries.
type WarmupReceiptProcessor interface {
	ProcessWarmupReceipt(context.Context, uuid.UUID, uuid.UUID, WarmupReceiptProof, func(*models.WarmupToken) error) (bool, error)
}

var ErrWarmupReceiptBusy = errors.New("warmup receipt is already being processed")

// WarmupSessionLocker moves receipt locks onto direct sessions when the pool is a transaction pooler.
type WarmupSessionLocker interface {
	UseSessionLocks(*pgxpool.Pool)
}

// UseSessionLocks holds each receipt's lock on a direct connection from locks instead of a pooled transaction.
func (r *warmupRepository) UseSessionLocks(locks *pgxpool.Pool) { r.sessionLocks = locks }

func (r *warmupRepository) ProcessWarmupReceipt(ctx context.Context, tokenID, recipient uuid.UUID, proof WarmupReceiptProof, accept func(*models.WarmupToken) error) (bool, error) {
	if r.sessionLocks != nil {
		return r.processReceiptUnderSessionLock(ctx, tokenID, recipient, proof, accept)
	}
	if r.db.Config().MaxConns < 2 {
		return false, errors.New("warmup receipt processing requires at least two database connections")
	}
	// Leave capacity for acceptance's repository calls while the receipt lock is held.
	r.receiptOnce.Do(func() { r.receiptSlots = make(chan struct{}, r.db.Config().MaxConns/2) })
	select {
	case r.receiptSlots <- struct{}{}:
		defer func() { <-r.receiptSlots }()
	default:
		return false, ErrWarmupReceiptBusy
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	// The lock spans acceptance, including consumption, but not the callback's writes.
	var locked bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('warmup_receipt_' || $1::text))`, tokenID).Scan(&locked); err != nil {
		return false, err
	}
	if !locked {
		return false, ErrWarmupReceiptBusy
	}
	return r.acceptReceipt(ctx, tx, tokenID, recipient, proof, accept)
}

// processReceiptUnderSessionLock takes the same advisory key as the transaction path, so the two exclude each other
// during a rolling deploy, but on a direct session: a pooler connection is never held while acceptance needs another.
func (r *warmupRepository) processReceiptUnderSessionLock(ctx context.Context, tokenID, recipient uuid.UUID, proof WarmupReceiptProof, accept func(*models.WarmupToken) error) (bool, error) {
	conn, err := r.sessionLocks.Acquire(ctx)
	if err != nil {
		return false, err
	}
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('warmup_receipt_' || $1::text))`, tokenID).Scan(&locked); err != nil {
		// The session's state is unknown, and closing it drops any lock it took.
		_ = conn.Hijack().Close(context.WithoutCancel(ctx))
		return false, err
	}
	if !locked {
		conn.Release()
		return false, ErrWarmupReceiptBusy
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var released bool
		if err := conn.QueryRow(unlockCtx, `SELECT pg_advisory_unlock(hashtext('warmup_receipt_' || $1::text))`, tokenID).Scan(&released); err != nil || !released {
			_ = conn.Hijack().Close(unlockCtx)
			return
		}
		conn.Release()
	}()
	return r.acceptReceipt(ctx, r.db, tokenID, recipient, proof, accept)
}

// acceptReceipt decides a receipt while its lock is held and runs acceptance when the token still applies.
func (r *warmupRepository) acceptReceipt(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, tokenID, recipient uuid.UUID, proof WarmupReceiptProof, accept func(*models.WarmupToken) error) (bool, error) {
	token, err := scanWarmupToken(q.QueryRow(ctx, `SELECT `+warmupTokenColumns+` FROM warmup_tokens wt
		WHERE wt.token=$1 AND wt.recipient_account_id=$2
		AND EXISTS(SELECT 1 FROM email_accounts sender WHERE sender.id=wt.sender_account_id AND lower(sender.email)=lower($3))`,
		tokenID, recipient, strings.TrimSpace(proof.SenderAddress)))
	if err != nil || token == nil {
		return false, err
	}
	if token.ConsumedAt != nil || !token.ExpiresAt.After(time.Now()) {
		return true, nil
	}
	var lineage, awaitingSend, retired, settled bool
	if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM warmup_tasks WHERE task_id=$1 AND lineage_version=1),
		EXISTS(SELECT 1 FROM warmup_tasks w JOIN tasks sent ON sent.id=w.task_id
		WHERE w.task_id=$1 AND w.lineage_version=1 AND (sent.status<>'completed' OR $2='')),
		EXISTS(SELECT 1 FROM warmup_tokens WHERE token=$3 AND sent_retired_at IS NOT NULL)
		OR EXISTS(SELECT 1 FROM tasks WHERE id=$1 AND warmup_send_retired_at IS NOT NULL),
		EXISTS(SELECT 1 FROM tasks WHERE id=$1 AND status IN ('failed','cancelled'))`, token.TaskID, token.SentMessageID, token.Token).Scan(&lineage, &awaitingSend, &retired, &settled); err != nil {
		return false, err
	}
	if retired {
		return true, nil
	}
	if awaitingSend {
		if settled {
			// A failed or cancelled send is never confirmed later, so it gets now what expiry would give it.
			return true, nil
		}
		return false, ErrWarmupDeliveryPending
	}
	if lineage && (strings.Trim(proof.MessageID, "<> \t") == "" ||
		proof.Token != token.Token && strings.Trim(token.SentMessageID, "<> \t") != strings.Trim(proof.MessageID, "<> \t")) {
		return false, nil
	}
	return true, accept(token)
}

// ConsumeWarmupToken marks a warmup token as consumed
func (r *warmupRepository) ConsumeWarmupToken(ctx context.Context, tokenID uuid.UUID) error {
	query := `UPDATE warmup_tokens SET consumed_at = NOW() WHERE token = $1 AND consumed_at IS NULL`
	_, err := resultDB(ctx, r.db).Exec(ctx, query, tokenID)
	return err
}

// warmupTokenColumns is the shared select list for a warmup token row.
const warmupTokenColumns = `token, task_id, sender_account_id, recipient_account_id,
	COALESCE(conversation_theme, ''), COALESCE(content_source, ''), conversation_id,
	conversation_turn, COALESCE(subject, ''), COALESCE(sent_message_id, ''),
	created_at, consumed_at, expires_at`

func scanWarmupToken(row pgx.Row) (*models.WarmupToken, error) {
	t := &models.WarmupToken{}
	err := row.Scan(
		&t.Token,
		&t.TaskID,
		&t.SenderAccountID,
		&t.RecipientAccountID,
		&t.ConversationTheme,
		&t.ContentSource,
		&t.ConversationID,
		&t.ConversationTurn,
		&t.Subject,
		&t.SentMessageID,
		&t.CreatedAt,
		&t.ConsumedAt,
		&t.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// RecordWarmupTokenDelivery stamps the delivered Message-ID onto the send's
// token. Graph re-stamps the Message-ID we mint, so on Outlook this is the
// only value a recipient can match the send against.
func (r *warmupRepository) RecordWarmupTokenDelivery(ctx context.Context, taskID uuid.UUID, messageID string) error {
	if messageID == "" {
		return nil
	}
	_, err := resultDB(ctx, r.db).Exec(ctx,
		`UPDATE warmup_tokens SET sent_message_id = $2 WHERE task_id = $1`,
		taskID, messageID)
	return err
}

// FindDeliveredWarmupToken resolves the token for warmup mail that arrived
// without its verify header, which is every send from a Microsoft mailbox
// (Graph strips custom headers in transit).
//
// Two keys: the Message-ID the provider stamped on the send, and the pending
// token for this exact sender/recipient pair whose subject matches. Both are
// scoped to unconsumed, unexpired tokens addressed to this recipient, of which
// there are only ever a handful, so this is one indexed lookup on a path every
// inbound message takes.
//
// The pair key is deliberately narrow. Warmup partners are other Warmbly
// mailboxes, so without the subject and the two-day window a real email
// between two pool members could claim a pending token and vanish from the
// recipient's unibox.
func (r *warmupRepository) FindDeliveredWarmupToken(ctx context.Context, recipientAccountID uuid.UUID, senderAddress, messageID, subject string) (*models.WarmupToken, error) {
	messageID = strings.Trim(strings.TrimSpace(messageID), "<>")
	senderAddress = strings.TrimSpace(senderAddress)
	subject = strings.TrimSpace(subject)
	if messageID == "" && (senderAddress == "" || subject == "") {
		return nil, nil
	}

	const matchesMessageID = `($2 <> '' AND wt.sent_message_id <> '' AND btrim(wt.sent_message_id, '<>') = $2)`
	query := `SELECT ` + warmupTokenColumns + `
		FROM warmup_tokens wt
		WHERE wt.recipient_account_id = $1
		  AND wt.consumed_at IS NULL
		  AND wt.expires_at > NOW()
		  AND (
		    ` + matchesMessageID + `
		    OR (
		      $3 <> '' AND $4 <> ''
		      AND ($2 = '' OR wt.sent_message_id = '')
		      AND wt.created_at > NOW() - INTERVAL '2 days'
		      AND wt.subject <> ''
		      AND lower(btrim(wt.subject)) = lower($4)
		      AND EXISTS (
		          SELECT 1 FROM email_accounts ea
		          WHERE ea.id = wt.sender_account_id AND lower(ea.email) = lower($3)
		      )
		    )
		  )
		ORDER BY ` + matchesMessageID + ` DESC, wt.created_at DESC
		LIMIT 1`
	return scanWarmupToken(resultDB(ctx, r.db).QueryRow(ctx, query, recipientAccountID, messageID, senderAddress, subject))
}

var ErrWarmupDeliveryPending = errors.New("warmup send is awaiting its provider message identifier")

// IsWarmupDelivery recognizes either mailbox's copy without consuming a token or repeating engagement.
func (r *warmupRepository) IsWarmupDelivery(ctx context.Context, accountID uuid.UUID, senderAddress, messageID, subject string) (bool, error) {
	messageID = strings.Trim(strings.TrimSpace(messageID), "<>")
	senderAddress = strings.TrimSpace(senderAddress)
	subject = strings.TrimSpace(subject)
	if messageID == "" && (senderAddress == "" || subject == "") {
		return false, nil
	}
	query := `
		SELECT EXISTS (
		    SELECT 1 FROM warmup_received wr
		    WHERE (wr.email_account_id = $1 OR wr.sender_account_id = $1)
		      AND $2 <> ''
		      AND btrim(wr.message_id, '<>') = $2
		  ) OR EXISTS (
		    SELECT 1 FROM warmup_tokens wt
		    WHERE (wt.recipient_account_id = $1 OR wt.sender_account_id = $1)
		      AND $2 <> '' AND wt.sent_message_id <> '' AND btrim(wt.sent_message_id, '<>') = $2
		  ) OR EXISTS (
		    SELECT 1 FROM warmup_tokens wt
		    JOIN tasks t ON t.id = wt.task_id
		    WHERE (wt.recipient_account_id = $1 OR wt.sender_account_id = $1)
		      AND $2 <> '' AND wt.sent_message_id = '' AND btrim(t.message_id, '<>') = $2
		  ) OR EXISTS (
		    SELECT 1 FROM warmup_tokens wt
		    JOIN email_accounts ea ON ea.id = wt.sender_account_id
		    WHERE wt.recipient_account_id = $1 AND $3 <> '' AND $4 <> ''
		      AND ($2 = '' OR wt.sent_message_id = '')
		      AND wt.created_at > NOW() - INTERVAL '2 days'
		      AND wt.subject <> '' AND lower(btrim(wt.subject)) = lower($4)
		      AND lower(ea.email) = lower($3)
		  ), EXISTS (
		    SELECT 1 FROM warmup_tokens wt
		    JOIN email_accounts ea ON ea.id = wt.sender_account_id
		    JOIN tasks t ON t.id = wt.task_id
		    WHERE wt.sender_account_id = $1 AND $3 <> '' AND $4 <> ''
		      AND lower(ea.email) = lower($3) AND lower(btrim(wt.subject)) = lower($4)
		      AND wt.sent_message_id = '' AND wt.expires_at > NOW()
		      AND t.status IN ('active', 'completed', 'dead_lettered')
		  ) OR EXISTS (
		    -- A live arrival from the sender of a send not yet confirmed: its
		    -- subject may not survive delivery, the confirmed id will. The
		    -- historical sweep passes no subject and is never held.
		    SELECT 1 FROM warmup_tokens wt
		    JOIN email_accounts ea ON ea.id = wt.sender_account_id
		    JOIN tasks t ON t.id = wt.task_id
		    WHERE wt.recipient_account_id = $1 AND $3 <> '' AND $4 <> ''
		      AND lower(ea.email) = lower($3)
		      AND wt.sent_message_id = '' AND wt.consumed_at IS NULL
		      AND wt.created_at > NOW() - INTERVAL '30 minutes'
		      AND t.status IN ('active', 'completed')
		  )`
	// One snapshot ensures a send confirmation cannot fall between known and pending checks.
	var known, pending bool
	if err := resultDB(ctx, r.db).QueryRow(ctx, query, accountID, messageID, senderAddress, subject).Scan(&known, &pending); err != nil {
		return false, err
	}
	if !known && pending {
		return false, ErrWarmupDeliveryPending
	}
	return known, nil
}

// IsWarmupThreadReply answers for mail that carries no token and no known id
// of its own: a reply typed by hand at a partner mailbox. It is warmup when
// what it answers is a warmup send or receipt of this mailbox, or a turn
// recognised the same way before (the record is keyed by Message-ID alone,
// because whichever mailbox syncs a copy of the next turn asks about it).
func (r *warmupRepository) IsWarmupThreadReply(ctx context.Context, accountID uuid.UUID, parentIDs []string) (bool, error) {
	parents := normalizeMessageIDs(parentIDs)
	if len(parents) == 0 {
		return false, nil
	}
	query := `
		SELECT EXISTS (
		    SELECT 1 FROM warmup_received wr
		    WHERE (wr.email_account_id = $1 OR wr.sender_account_id = $1)
		      AND btrim(wr.message_id, '<>') = ANY($2)
		  ) OR EXISTS (
		    SELECT 1 FROM warmup_tokens wt
		    WHERE (wt.recipient_account_id = $1 OR wt.sender_account_id = $1)
		      AND wt.sent_message_id <> '' AND btrim(wt.sent_message_id, '<>') = ANY($2)
		  ) OR EXISTS (
		    SELECT 1 FROM warmup_tokens wt
		    JOIN tasks t ON t.id = wt.task_id
		    WHERE (wt.recipient_account_id = $1 OR wt.sender_account_id = $1)
		      AND btrim(t.message_id, '<>') = ANY($2)
		  ) OR EXISTS (
		    SELECT 1 FROM warmup_thread_messages m WHERE m.message_id = ANY($2)
		  )`
	var known bool
	if err := resultDB(ctx, r.db).QueryRow(ctx, query, accountID, parents).Scan(&known); err != nil {
		return false, err
	}
	return known, nil
}

// RecordWarmupThreadMessage is idempotent on a re-sync of the same turn.
func (r *warmupRepository) RecordWarmupThreadMessage(ctx context.Context, accountID uuid.UUID, messageID string) error {
	ids := normalizeMessageIDs([]string{messageID})
	if len(ids) == 0 || accountID == uuid.Nil {
		return nil
	}
	_, err := resultDB(ctx, r.db).Exec(ctx,
		`INSERT INTO warmup_thread_messages (message_id, email_account_id) VALUES ($1, $2)
		 ON CONFLICT DO NOTHING`,
		ids[0], accountID)
	return err
}

// normalizeMessageIDs trims each id to the bare form every lookup compares on
// and drops blanks.
func normalizeMessageIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id = strings.Trim(strings.TrimSpace(id), "<>"); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// GetRecentlyUsedPartners returns partner account IDs the sender has targeted since the provided timestamp.
func (r *warmupRepository) GetRecentlyUsedPartners(ctx context.Context, accountID uuid.UUID, since time.Time) ([]uuid.UUID, error) {
	query := `
		SELECT DISTINCT recipient_account_id
		FROM warmup_tokens
		WHERE sender_account_id = $1
		  AND created_at >= $2
	`

	rows, err := resultDB(ctx, r.db).Query(ctx, query, accountID, since)
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

	rows, err := resultDB(ctx, r.db).Query(ctx, query, accountID, since)
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
func (r *warmupRepository) RecordWarmupReceived(ctx context.Context, accountID, internalID uuid.UUID, messageID string, senderAccountID uuid.UUID, landedSpam bool) error {
	query := `
		INSERT INTO warmup_received (email_account_id, internal_id, message_id, sender_account_id, landed_spam)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (email_account_id, internal_id) DO NOTHING
	`
	_, err := resultDB(ctx, r.db).Exec(ctx, query, accountID, internalID, messageID, senderAccountID, landedSpam)
	return err
}

// GetWarmupReceived looks up a delivered warmup email by recipient + internal
// message id. Returns nil when the message was not a warmup email.
func (r *warmupRepository) GetWarmupReceived(ctx context.Context, accountID, internalID uuid.UUID) (*WarmupReceived, error) {
	query := `
		SELECT email_account_id, internal_id, message_id, sender_account_id, created_at, retired_at, landed_spam
		FROM warmup_received
		WHERE email_account_id = $1 AND internal_id = $2
	`
	var w WarmupReceived
	err := resultDB(ctx, r.db).QueryRow(ctx, query, accountID, internalID).Scan(
		&w.EmailAccountID, &w.InternalID, &w.MessageID, &w.SenderAccountID, &w.CreatedAt, &w.RetiredAt, &w.LandedSpam,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// WarmupRetirePerMailbox bounds one mailbox's share of a retirement run; the sweep skips a mailbox it already served that run.
const WarmupRetirePerMailbox = 50

// ListWarmupMailToRetire lists received warmup copies past their window:
// the mailbox's own when it set one, else the instance default handed in as
// $1, on active mailboxes that have a worker to act. The provider key comes
// from the message map by the internal id, which is what the worker's remove
// and flag events are keyed on. It starts from the mailboxes, so receipts
// held by a mailbox that cannot act are never walked.
func (r *warmupRepository) ListWarmupMailToRetire(ctx context.Context, defaultDays, limit int, skip []uuid.UUID) ([]WarmupMailToRetire, error) {
	query := `
		SELECT ea.user_id, ea.id, ` + actionWorkerSQL("ea") + `, wr.internal_id, wr.message_id,
		       COALESCE(m.message_id, ''), ea.warmup_placement, ea.warmup_folder
		FROM email_accounts ea
		CROSS JOIN LATERAL (
			SELECT r.internal_id, r.message_id, r.created_at FROM warmup_received r
			WHERE r.email_account_id = ea.id AND r.retired_at IS NULL
			  AND r.created_at < NOW() - make_interval(days => COALESCE(ea.warmup_retention_days, $1))
			ORDER BY r.created_at
			LIMIT LEAST($2, ` + strconv.Itoa(WarmupRetirePerMailbox) + `)
		) wr
		LEFT JOIN LATERAL (
			SELECT em.message_id FROM email_message_map em
			WHERE em.email_id = ea.id AND em.id = wr.internal_id
			LIMIT 1
		) m ON true
		WHERE ea.status = 'active' AND NOT (ea.id = ANY($3::uuid[]))
		  AND ` + actionRoutableSQL("ea") + `
		ORDER BY wr.created_at
		LIMIT $2`
	return r.scanMailToRetire(ctx, query, defaultDays, limit, skip, false)
}

// ListWarmupSentCopiesToRetire lists the sender's own copies past their
// window. Gmail and Outlook file that copy themselves; an SMTP mailbox never
// gets one, because the worker does not append warmup to Sent, so those
// senders are left out rather than searched for a message that does not
// exist. The map is keyed by the provider's id on Gmail and Graph, so the key
// is usually empty there and the worker searches by Message-ID instead.
// Like the received side it starts from the mailboxes that can act.
func (r *warmupRepository) ListWarmupSentCopiesToRetire(ctx context.Context, defaultDays, limit int, skip []uuid.UUID) ([]WarmupMailToRetire, error) {
	query := `
		SELECT ea.user_id, ea.id, ` + actionWorkerSQL("ea") + `, wt.token, wt.sent_message_id,
		       COALESCE(m.message_id, ''), ea.warmup_placement, ea.warmup_folder
		FROM email_accounts ea
		CROSS JOIN LATERAL (
			SELECT t.token, t.sent_message_id, t.created_at FROM warmup_tokens t
			WHERE t.sender_account_id = ea.id AND t.sent_message_id <> '' AND t.sent_retired_at IS NULL
			  AND t.created_at < NOW() - make_interval(days => COALESCE(ea.warmup_retention_days, $1))
			ORDER BY t.created_at
			LIMIT LEAST($2, ` + strconv.Itoa(WarmupRetirePerMailbox) + `)
		) wt
		LEFT JOIN LATERAL (
			SELECT em.message_id FROM email_message_map em
			WHERE em.email_id = ea.id AND em.message_id = wt.sent_message_id
			LIMIT 1
		) m ON true
		WHERE ea.status = 'active' AND NOT (ea.id = ANY($3::uuid[]))
		  AND ` + actionRoutableSQL("ea") + `
		  AND ea.provider <> 'smtp_imap'
		ORDER BY wt.created_at
		LIMIT $2`
	return r.scanMailToRetire(ctx, query, defaultDays, limit, skip, true)
}

func (r *warmupRepository) scanMailToRetire(ctx context.Context, query string, defaultDays, limit int, skip []uuid.UUID, sent bool) ([]WarmupMailToRetire, error) {
	if skip == nil {
		skip = []uuid.UUID{}
	}
	rows, err := resultDB(ctx, r.db).Query(ctx, query, defaultDays, limit, skip)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WarmupMailToRetire
	for rows.Next() {
		var m WarmupMailToRetire
		var id uuid.UUID
		if err := rows.Scan(&m.UserID, &m.EmailAccountID, &m.WorkerID, &id, &m.MessageID, &m.ProviderKey, &m.Placement, &m.Folder); err != nil {
			return nil, err
		}
		if sent {
			m.Token = id
		} else {
			m.InternalID = id
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// RetireWarmupReceived stamps the receipt once its deletion is on the bus.
func (r *warmupRepository) RetireWarmupReceived(ctx context.Context, accountID, internalID uuid.UUID) error {
	_, err := resultDB(ctx, r.db).Exec(ctx, `
		UPDATE warmup_received SET retired_at = NOW()
		WHERE email_account_id = $1 AND internal_id = $2 AND retired_at IS NULL`, accountID, internalID)
	return err
}

// RetireWarmupSentCopy stamps the token once the deletion of the sender's own
// copy is on the bus.
func (r *warmupRepository) RetireWarmupSentCopy(ctx context.Context, token uuid.UUID) error {
	_, err := resultDB(ctx, r.db).Exec(ctx, `
		UPDATE warmup_tokens SET sent_retired_at = NOW()
		WHERE token = $1 AND sent_retired_at IS NULL`, token)
	return err
}

// warmupRecoveryIdentifierDays matches the expiry remember_warmup_identifier gives (migration 000265).
const warmupRecoveryIdentifierDays = 90

// PruneWarmupEventsBefore drops the per-message warmup records older than
// before. Receipts and tokens are kept while their mail may still be in the
// mailbox (not yet retired), so the retention sweep can still reach it and a
// removal seen later is still recognised as warmup rather than tampering.
func (r *warmupRepository) PruneWarmupEventsBefore(ctx context.Context, before time.Time, mailDays int) (int64, error) {
	var total int64
	// Each statement gets only the arguments it references; pgx refuses an extra one.
	for _, st := range []struct {
		query string
		args  []any
	}{
		{`DELETE FROM warmup_tampering_events
		 WHERE created_at < LEAST($1, NOW() - make_interval(days => ` + strconv.Itoa(config.WarmupTamperingKeepDays) + `))`, []any{before}},
		{`DELETE FROM warmup_spam_reports WHERE created_at < $1`, []any{before}},
		{`DELETE FROM warmup_spam_moves
		 WHERE decided_at IS NOT NULL
		   AND observed_at < LEAST($1, NOW() - make_interval(days => ` + strconv.Itoa(config.WarmupTamperingKeepDays) + `))`, []any{before}},
		{`DELETE FROM mailbox_owner_activity
		 WHERE bucket < NOW() - make_interval(days => ` + strconv.Itoa(config.WarmupOwnerActivityKeepDays) + `)`, nil},
		{`DELETE FROM warmup_received WHERE created_at < $1 AND retired_at IS NOT NULL`, []any{before}},
		{`DELETE FROM warmup_tokens
		 WHERE created_at < $1
		   AND (sent_message_id = '' OR sent_retired_at IS NOT NULL
		        OR sender_account_id IN (SELECT id FROM email_accounts WHERE provider = 'smtp_imap'))`, []any{before}},
		{`DELETE FROM pool_link_commands WHERE status IN ('answered', 'expired', 'cancelled') AND created_at < $1`, []any{before}},
		// An arrival's handle is how a retiring delete reaches the instance, so it outlives the copy it names.
		{`DELETE FROM pool_link_arrivals a WHERE a.reported_at < $1
		 AND NOT EXISTS (SELECT 1 FROM warmup_received wr WHERE wr.email_account_id = a.email_account_id
		                 AND wr.internal_id = a.internal_id AND wr.retired_at IS NULL)`, []any{before}},
	} {
		cmd, err := resultDB(ctx, r.db).Exec(ctx, st.query, st.args...)
		if err != nil {
			return total, err
		}
		total += cmd.RowsAffected()
	}
	// Replies are matched against a thread turn, so it outlives the event window, its mailbox's mail window and the recovery identifiers.
	n, err := DeleteInBatches(ctx, r.db, "warmup_thread_messages",
		`SELECT m.ctid FROM warmup_thread_messages m JOIN email_accounts ea ON ea.id = m.email_account_id
		  WHERE m.created_at < LEAST($2, NOW() - make_interval(days => GREATEST(`+strconv.Itoa(warmupRecoveryIdentifierDays)+`, COALESCE(ea.warmup_retention_days, $3))))
		  LIMIT $1`, before, mailDays)
	total += n
	if err != nil {
		return total, err
	}
	return total, nil
}

// RecordWarmupTampering records one "harm" a participant did to a warmup email.
// Returns whether a new row was inserted (deduped per account+message+kind).
func (r *warmupRepository) RecordWarmupTampering(ctx context.Context, accountID uuid.UUID, messageID, kind string) (bool, error) {
	query := `
		INSERT INTO warmup_tampering_events (email_account_id, message_id, kind)
		VALUES ($1, $2, $3)
		ON CONFLICT (email_account_id, message_id, kind) DO NOTHING
	`
	cmd, err := resultDB(ctx, r.db).Exec(ctx, query, accountID, messageID, kind)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func (r *warmupRepository) WithdrawWarmupTampering(ctx context.Context, accountID uuid.UUID, messageID, kind string) (bool, error) {
	cmd, err := resultDB(ctx, r.db).Exec(ctx, `
		DELETE FROM warmup_tampering_events
		WHERE email_account_id = $1 AND message_id = $2 AND kind = $3`,
		accountID, messageID, kind)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func (r *warmupRepository) HasWarmupTampering(ctx context.Context, accountID uuid.UUID, messageID, kind string) (bool, error) {
	var exists bool
	err := resultDB(ctx, r.db).QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM warmup_tampering_events
		              WHERE email_account_id = $1 AND message_id = $2 AND kind = $3)`,
		accountID, messageID, kind).Scan(&exists)
	return exists, err
}

func (r *warmupRepository) CountWarmupTamperingBetween(ctx context.Context, accountID uuid.UUID, from, to time.Time, excludeMessageID string, byAddress bool) (int, int, error) {
	var deletions, spamFlags int
	err := resultDB(ctx, r.db).QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE kind = 'deletion'), COUNT(*) FILTER (WHERE kind = 'spam_flag')
		FROM warmup_tampering_events
		WHERE email_account_id IN (
		        SELECT $1::uuid
		        UNION
		        SELECT sib.id FROM email_accounts me
		        JOIN email_accounts sib ON sib.organization_id = me.organization_id
		         AND lower(btrim(sib.email)) = lower(btrim(me.email))
		        WHERE me.id = $1 AND $5::bool)
		  AND created_at >= $2 AND created_at <= $3
		  AND message_id <> $4`,
		accountID, from, to, excludeMessageID, byAddress).Scan(&deletions, &spamFlags)
	return deletions, spamFlags, err
}

func (r *warmupRepository) WarmupReceiptRetired(ctx context.Context, accountID uuid.UUID, messageID string) (bool, error) {
	var retired bool
	err := resultDB(ctx, r.db).QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM warmup_received
		              WHERE email_account_id = $1 AND message_id = $2 AND retired_at IS NOT NULL)`,
		accountID, messageID).Scan(&retired)
	return retired, err
}

// WarmupTamperingToVerify is one unverified deletion strike and where to ask.
type WarmupTamperingToVerify struct {
	EmailAccountID uuid.UUID
	UserID         uuid.UUID
	WorkerID       uuid.UUID
	MessageID      string
}

func (r *warmupRepository) ListUnverifiedDeletions(ctx context.Context, since time.Time, retryAfter time.Duration, limit int) ([]WarmupTamperingToVerify, error) {
	rows, err := resultDB(ctx, r.db).Query(ctx, `
		SELECT t.email_account_id, ea.user_id, `+actionWorkerSQL("ea")+`, t.message_id
		FROM warmup_tampering_events t
		JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE t.verified_at IS NULL
		  AND t.kind = 'deletion'
		  AND t.created_at >= $1
		  AND (t.verify_requested_at IS NULL OR t.verify_requested_at < NOW() - make_interval(secs => $2))
		  AND t.message_id <> ''
		  AND ea.status = 'active'
		  AND `+actionRoutableSQL("ea")+`
		ORDER BY t.created_at
		LIMIT $3`, since, retryAfter.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WarmupTamperingToVerify
	for rows.Next() {
		var v WarmupTamperingToVerify
		if err := rows.Scan(&v.EmailAccountID, &v.UserID, &v.WorkerID, &v.MessageID); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *warmupRepository) MarkTamperingVerifyRequested(ctx context.Context, accountID uuid.UUID, messageID string) error {
	_, err := resultDB(ctx, r.db).Exec(ctx, `
		UPDATE warmup_tampering_events SET verify_requested_at = NOW()
		WHERE email_account_id = $1 AND message_id = $2 AND kind = 'deletion' AND verified_at IS NULL`,
		accountID, messageID)
	return err
}

func (r *warmupRepository) MarkTamperingVerified(ctx context.Context, accountID uuid.UUID, messageID, kind string) error {
	_, err := resultDB(ctx, r.db).Exec(ctx, `
		UPDATE warmup_tampering_events SET verified_at = NOW()
		WHERE email_account_id = $1 AND message_id = $2 AND kind = $3 AND verified_at IS NULL`,
		accountID, messageID, kind)
	return err
}

// WarmupHold is a mailbox's standing as a pool row or ledger row holds it.
type WarmupHold struct {
	State        models.WarmupHealthState
	BlockedAt    *time.Time
	BlockedUntil *time.Time
	Reason       string
	// InPool is false when the standing is only on the ledger.
	InPool bool
}

// noSiblingInPoolSQL is true when no mailbox sharing $1's address is in a
// pool, which is when the ledger is the standing.
const noSiblingInPoolSQL = `NOT EXISTS (
	SELECT 1 FROM warmup_pool_participants p
	JOIN email_accounts sib ON sib.id = p.email_account_id
	WHERE sib.organization_id = a.organization_id AND lower(btrim(sib.email)) = lower(btrim(a.email)))`

func (r *warmupRepository) GetWarmupHold(ctx context.Context, accountID uuid.UUID) (*WarmupHold, error) {
	var h WarmupHold
	var state string
	err := resultDB(ctx, r.db).QueryRow(ctx, `
		SELECT health_state, blocked_at, blocked_until, COALESCE(blocked_reason, ''), true
		FROM warmup_pool_participants WHERE email_account_id = $1
		UNION ALL
		SELECT l.health_state, l.blocked_at, l.blocked_until, COALESCE(l.blocked_reason, ''), false
		FROM warmup_reputation_ledger l
		JOIN email_accounts a ON a.organization_id = l.organization_id AND lower(btrim(a.email)) = l.email
		WHERE a.id = $1 AND `+noSiblingInPoolSQL+`
		LIMIT 1`, accountID).Scan(&state, &h.BlockedAt, &h.BlockedUntil, &h.Reason, &h.InPool)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	h.State = models.WarmupHealthState(state)
	return &h, nil
}

func (r *warmupRepository) ReviseWarmupHold(ctx context.Context, accountID uuid.UUID, hold *WarmupHold, state models.WarmupHealthState, until *time.Time, newReason string) (bool, error) {
	if hold == nil || hold.BlockedUntil == nil {
		return false, nil
	}
	held := state == models.WarmupHealthQuarantined || state == models.WarmupHealthBlocked
	if !held {
		until, newReason = nil, ""
	}
	if hold.InPool {
		cmd, err := resultDB(ctx, r.db).Exec(ctx, `
			UPDATE warmup_pool_participants
			SET health_state = $4::text,
			    blocked_until = $5::timestamptz,
			    blocked_reason = NULLIF($6::text, ''),
			    blocked_at = CASE WHEN $5::timestamptz IS NULL THEN NULL ELSE blocked_at END
			WHERE email_account_id = $1 AND blocked_reason = $2 AND blocked_until = $3`,
			accountID, hold.Reason, *hold.BlockedUntil, string(state), until, newReason)
		if err != nil {
			return false, err
		}
		return cmd.RowsAffected() > 0, nil
	}
	var sqlText string
	args := []any{accountID, hold.Reason, *hold.BlockedUntil}
	if held {
		sqlText = `
			UPDATE warmup_reputation_ledger l
			SET health_state = $4::text, blocked_until = $5::timestamptz, blocked_reason = $6::text,
			    standing_until = GREATEST($5::timestamptz, NOW())
			FROM email_accounts a
			WHERE a.id = $1 AND l.organization_id = a.organization_id AND l.email = lower(btrim(a.email))
			  AND l.blocked_reason = $2 AND l.blocked_until = $3 AND ` + noSiblingInPoolSQL
		args = append(args, string(state), until, newReason)
	} else {
		sqlText = `
			DELETE FROM warmup_reputation_ledger l
			USING email_accounts a
			WHERE a.id = $1 AND l.organization_id = a.organization_id AND l.email = lower(btrim(a.email))
			  AND l.blocked_reason = $2 AND l.blocked_until = $3 AND ` + noSiblingInPoolSQL
	}
	cmd, err := resultDB(ctx, r.db).Exec(ctx, sqlText, args...)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

// CountWarmupTamperingSince counts distinct tampering events for a mailbox.
func (r *warmupRepository) CountWarmupTamperingSince(ctx context.Context, accountID uuid.UUID, since time.Time) (int, error) {
	query := `SELECT COUNT(*) FROM warmup_tampering_events WHERE email_account_id = $1 AND created_at >= $2`
	var n int
	err := resultDB(ctx, r.db).QueryRow(ctx, query, accountID, since).Scan(&n)
	return n, err
}

// CreateWarmupAppeal inserts a pending appeal for a blocked mailbox.
func (r *warmupRepository) CreateWarmupAppeal(ctx context.Context, accountID, userID uuid.UUID, reason string) (uuid.UUID, error) {
	id := uuid.New()
	query := `
		INSERT INTO warmup_appeals (id, email_account_id, user_id, reason, status)
		VALUES ($1, $2, $3, $4, 'pending')
	`
	_, err := resultDB(ctx, r.db).Exec(ctx, query, id, accountID, userID, reason)
	return id, err
}

// HasPendingWarmupAppeal reports whether the mailbox already has an open appeal.
func (r *warmupRepository) HasPendingWarmupAppeal(ctx context.Context, accountID uuid.UUID) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM warmup_appeals WHERE email_account_id = $1 AND status = 'pending')`
	var exists bool
	err := resultDB(ctx, r.db).QueryRow(ctx, query, accountID).Scan(&exists)
	return exists, err
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
	rows, err := resultDB(ctx, r.db).Query(ctx, query, accountID, since)
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

// WarmupPartnerDiversity is the distinct confirmed reach across three
// dimensions, and the other direction: what verifiably arrived from the pool
// and from how many senders. A mailbox that sends to nineteen partners and
// hears back from one is starving, and only the second pair of numbers shows it.
type WarmupPartnerDiversity struct {
	Mailboxes     int
	Domains       int
	Organizations int
	Received      int
	Senders       int
}

// GetPartnerDiversity returns zeros when no confirmed send falls in the window.
func (r *warmupRepository) GetPartnerDiversity(ctx context.Context, accountID uuid.UUID, since time.Time) (WarmupPartnerDiversity, error) {
	query := `
		SELECT
			COUNT(DISTINCT wt.recipient_account_id),
			COUNT(DISTINCT lower(split_part(ea.email, '@', 2))),
			COUNT(DISTINCT ea.organization_id)
		FROM warmup_tokens wt
		JOIN tasks t ON t.id = wt.task_id
		JOIN email_accounts ea ON ea.id = wt.recipient_account_id
		WHERE wt.sender_account_id = $1
		  AND wt.created_at >= $2
		  AND t.status = 'completed'
		  AND wt.sent_message_id <> ''
	`
	var out WarmupPartnerDiversity
	if err := resultDB(ctx, r.db).QueryRow(ctx, query, accountID, since).Scan(&out.Mailboxes, &out.Domains, &out.Organizations); err != nil {
		return out, err
	}
	// Arrivals are what the recipient's own sync verified, so a partner whose
	// mail never landed does not count as one heard from.
	err := resultDB(ctx, r.db).QueryRow(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT sender_account_id)
		FROM warmup_received
		WHERE email_account_id = $1
		  AND created_at >= $2
	`, accountID, since).Scan(&out.Received, &out.Senders)
	return out, err
}

// GetLatestReplyCandidate finds the latest completed warmup email from sender to recipient.
// It also returns the generated conversation and turn so the next reply can
// continue the exact offline-generated thread.
func (r *warmupRepository) GetLatestReplyCandidate(ctx context.Context, senderAccountID, recipientAccountID uuid.UUID) (*WarmupReplyCandidate, error) {
	query := `
		SELECT t.message_id, COALESCE(et.subject, ''), et.thread_id,
		       COALESCE(wt.conversation_theme, ''), COALESCE(wt.content_source, ''),
		       wt.conversation_id, wt.conversation_turn
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
		  AND NOT EXISTS (
		      SELECT 1
		      FROM tasks response_task
		      JOIN email_tasks response ON response.task_id = response_task.id
		      WHERE response_task.email_account_id = $2
		        AND response_task.status = 'completed'
		        AND t.message_id = ANY(COALESCE(response.in_reply_to, '{}'::text[]))
		  )
		ORDER BY t.completed_at DESC
		LIMIT 1
	`

	candidate := &WarmupReplyCandidate{}
	err := resultDB(ctx, r.db).QueryRow(ctx, query, senderAccountID, recipientAccountID).Scan(
		&candidate.MessageID,
		&candidate.Subject,
		&candidate.ThreadID,
		&candidate.ConversationTheme,
		&candidate.ContentSource,
		&candidate.ConversationID,
		&candidate.ConversationTurn,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return candidate, nil
}

// WarmupParticipantMembership is one participating mailbox, what decides its pool, and the pools it sits in.
type WarmupParticipantMembership struct {
	EmailAccountID uuid.UUID
	Status         string
	OrganizationID *uuid.UUID
	WarmupPoolType string
	PoolTypes      []string
}

// ListParticipantMemberships reads every participant with its mailbox in one query.
func (r *warmupRepository) ListParticipantMemberships(ctx context.Context) ([]WarmupParticipantMembership, error) {
	rows, err := resultDB(ctx, r.db).Query(ctx, `
		SELECT p.email_account_id, ea.status::text, ea.organization_id, COALESCE(ea.warmup_pool_type, ''),
		       array_agg(DISTINCT wp.pool_type::text)
		  FROM warmup_pool_participants p
		  JOIN warmup_pools wp ON wp.id = p.pool_id
		  JOIN email_accounts ea ON ea.id = p.email_account_id
		 GROUP BY p.email_account_id, ea.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []WarmupParticipantMembership
	for rows.Next() {
		var m WarmupParticipantMembership
		if err := rows.Scan(&m.EmailAccountID, &m.Status, &m.OrganizationID, &m.WarmupPoolType, &m.PoolTypes); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetPoolHealthCounts returns counts per health state and the average band score
func (r *warmupRepository) GetPoolHealthCounts(ctx context.Context) (map[string]int, float64, error) {
	query := `
		SELECT health_state, COUNT(*), AVG(last_health_score)
		FROM warmup_pool_participants
		GROUP BY health_state
	`
	rows, err := resultDB(ctx, r.db).Query(ctx, query)
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
