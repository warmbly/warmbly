// Package credits implements the AI writing-assistant credit ledger: balance
// reads, atomic consumption with idempotency, and grants. Hard billing
// correctness (no negative balances, no double-charge on retry) lives in the
// repository's atomic SQL; this service layer adds the abuse caps (a short
// rolling window plus a daily window) on top.
package credits

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// Rolling-window cap: max writing-assistant generations per org in a 5-hour
	// window. Catches burst abuse independent of credit balance.
	WindowShort       = 5 * time.Hour
	DefaultShortLimit = 60

	// Daily cap: max writing-assistant generations per org per UTC day.
	WindowDaily       = 24 * time.Hour
	DefaultDailyLimit = 300

	keyPrefixShort = "credits:cap:5h:"
	keyPrefixDaily = "credits:cap:day:"
)

// ErrInsufficientCredits signals the org has fewer credits than requested. The
// handler maps this to HTTP 402. Distinct, exported sentinels are used (rather
// than *errx.Error) because errx has no 402 code; the handler emits the 402
// envelope itself.
var ErrInsufficientCredits = errors.New("insufficient credits")

// ErrCapExceeded signals the org hit a rolling/daily generation cap. The
// handler maps this to HTTP 429.
var ErrCapExceeded = errors.New("generation rate cap exceeded")

// CreditService is the application-facing API for AI credits.
type CreditService interface {
	// GetBalance returns the org's current spendable balance across both pools
	// (0 if no ledger yet).
	GetBalance(ctx context.Context, orgID uuid.UUID) (int, *errx.Error)

	// GetLedger returns the org's full ledger (monthly + purchased pools, reset
	// date, lifetime purchased). Returns a zero-value ledger (not nil) when the
	// org has no ledger row yet.
	GetLedger(ctx context.Context, orgID uuid.UUID) (*models.CreditLedger, *errx.Error)

	// Consume enforces the abuse caps, then atomically debits `amount` credits,
	// draining the monthly pool first then purchased. On success it returns the
	// resulting combined balance. idempotencyKey may be empty; when set, a retry
	// with the same key does not double-charge and does not re-count against the
	// caps.
	//
	// Returns ErrInsufficientCredits (→402) or ErrCapExceeded (→429) as
	// sentinel errors the handler maps; any other error is an internal failure.
	Consume(ctx context.Context, orgID uuid.UUID, amount int, reason, model string, tokens int, idempotencyKey string) (int, error)

	// Grant adds credits to an org's monthly pool (e.g. a provider-failure
	// refund). Returns the resulting combined balance.
	Grant(ctx context.Context, orgID uuid.UUID, amount int, reason string) (int, *errx.Error)

	// GrantPurchased adds top-up credits to the purchased pool (never expire,
	// survive resets) and bumps lifetime total_purchased. idempotencyKey (the
	// Stripe event id) makes webhook retries safe. Plain error return so the
	// stripe package's CreditGranter can satisfy it without importing errx.
	GrantPurchased(ctx context.Context, orgID uuid.UUID, amount int, reason, idempotencyKey string) (int, error)

	// ResetMonthlyAllowance sets the monthly pool to `allowance` (set-to-N;
	// purchased pool untouched) and stamps the reset time. idempotencyKey (the
	// Stripe event id) makes webhook retries safe.
	ResetMonthlyAllowance(ctx context.Context, orgID uuid.UUID, allowance int, idempotencyKey string) error

	// CheckUsageCaps returns ErrCapExceeded when the org has hit a rolling 5h
	// or daily generation cap, WITHOUT consuming. Lets a caller gate expensive
	// AI work (e.g. a research run) before doing it, so a cap trip at charge
	// time does not waste a full run.
	CheckUsageCaps(ctx context.Context, orgID uuid.UUID) error

	// ListTransactions returns recent ledger transactions, newest first.
	ListTransactions(ctx context.Context, orgID uuid.UUID, limit int) ([]models.CreditTransaction, *errx.Error)

	// ListTransactionsBefore keyset-paginates the history (rows older than the
	// cursor). Pass zero values for the first page.
	ListTransactionsBefore(ctx context.Context, orgID uuid.UUID, limit int, beforeCreatedAt time.Time, beforeID uuid.UUID) ([]models.CreditTransaction, *errx.Error)
}

type creditService struct {
	repo       repository.CreditRepository
	cache      *cache.Cache
	shortLimit int
	dailyLimit int
}

func NewService(repo repository.CreditRepository, c *cache.Cache) CreditService {
	return &creditService{
		repo:       repo,
		cache:      c,
		shortLimit: DefaultShortLimit,
		dailyLimit: DefaultDailyLimit,
	}
}

func (s *creditService) GetBalance(ctx context.Context, orgID uuid.UUID) (int, *errx.Error) {
	ledger, err := s.repo.GetBalance(ctx, orgID)
	if err != nil {
		return 0, errx.New(errx.Internal, "failed to read credit balance")
	}
	if ledger == nil {
		return 0, nil
	}
	return ledger.Total(), nil
}

func (s *creditService) GetLedger(ctx context.Context, orgID uuid.UUID) (*models.CreditLedger, *errx.Error) {
	ledger, err := s.repo.GetBalance(ctx, orgID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to read credit ledger")
	}
	if ledger == nil {
		// No ledger yet: report an empty one so callers get a stable shape.
		return &models.CreditLedger{OrgID: orgID}, nil
	}
	return ledger, nil
}

func (s *creditService) Consume(ctx context.Context, orgID uuid.UUID, amount int, reason, model string, tokens int, idempotencyKey string) (int, error) {
	if amount <= 0 {
		return 0, errors.New("credit amount must be positive")
	}

	// Enforce the abuse caps first. The repo's Consume then handles the atomic
	// debit and idempotent replay; we only count a cap hit against a *fresh*
	// debit (replayed == false) so a legitimate client retry with the same
	// Idempotency-Key is never penalized against the 5h/daily window.
	if err := s.checkCaps(ctx, orgID, idempotencyKey); err != nil {
		return 0, err
	}

	bal, _, replayed, err := s.repo.Consume(ctx, orgID, amount, reason, model, tokens, idempotencyKey)
	if errors.Is(err, repository.ErrInsufficientCredits) {
		return 0, ErrInsufficientCredits
	}
	if err != nil {
		return 0, err
	}

	// Persist the cap increment only for fresh debits. checkCaps used a
	// reserve/peek so a replay does not advance the window.
	if !replayed {
		s.commitCaps(ctx, orgID)
	}
	return bal, nil
}

func (s *creditService) Grant(ctx context.Context, orgID uuid.UUID, amount int, reason string) (int, *errx.Error) {
	if amount <= 0 {
		return 0, errx.New(errx.BadRequest, "grant amount must be positive")
	}
	bal, _, err := s.repo.Grant(ctx, orgID, amount, reason, "")
	if err != nil {
		return 0, errx.New(errx.Internal, "failed to grant credits")
	}
	return bal, nil
}

func (s *creditService) GrantPurchased(ctx context.Context, orgID uuid.UUID, amount int, reason, idempotencyKey string) (int, error) {
	if amount <= 0 {
		return 0, errors.New("grant amount must be positive")
	}
	bal, _, err := s.repo.GrantPurchased(ctx, orgID, amount, reason, idempotencyKey)
	if err != nil {
		return 0, err
	}
	return bal, nil
}

func (s *creditService) ResetMonthlyAllowance(ctx context.Context, orgID uuid.UUID, allowance int, idempotencyKey string) error {
	if allowance < 0 {
		return errors.New("allowance must be non-negative")
	}
	_, err := s.repo.ResetMonthly(ctx, orgID, allowance, idempotencyKey)
	return err
}

func (s *creditService) CheckUsageCaps(ctx context.Context, orgID uuid.UUID) error {
	return s.checkCaps(ctx, orgID, "")
}

func (s *creditService) ListTransactions(ctx context.Context, orgID uuid.UUID, limit int) ([]models.CreditTransaction, *errx.Error) {
	txns, err := s.repo.ListTransactions(ctx, orgID, limit)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to list credit transactions")
	}
	return txns, nil
}

func (s *creditService) ListTransactionsBefore(ctx context.Context, orgID uuid.UUID, limit int, beforeCreatedAt time.Time, beforeID uuid.UUID) ([]models.CreditTransaction, *errx.Error) {
	txns, err := s.repo.ListTransactionsBefore(ctx, orgID, limit, beforeCreatedAt, beforeID)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to list credit transactions")
	}
	return txns, nil
}

// shortKey / dailyKey are the per-org Redis keys for the two abuse windows.
func (s *creditService) shortKey(orgID uuid.UUID) string {
	return keyPrefixShort + orgID.String()
}

func (s *creditService) dailyKey(orgID uuid.UUID) string {
	day := time.Now().UTC().Format("2006-01-02")
	return fmt.Sprintf("%s%s:%s", keyPrefixDaily, orgID.String(), day)
}

// checkCaps rejects when the org has already reached the rolling 5h or daily
// generation cap. It only *reads* the counters (commitCaps does the increment
// after a successful fresh debit), so an idempotent replay never advances the
// window. On Redis errors it fails open so a cache outage never blocks
// legitimate generation. It is a thin wrapper over Redis (the same key shape the
// rate-limit service uses), not a reimplementation of that service.
//
// idempotencyKey is reserved for future per-key suppression; today a replay is
// distinguished after the fact via the repo's replayed flag.
func (s *creditService) checkCaps(ctx context.Context, orgID uuid.UUID, _ string) error {
	if s.cache == nil {
		return nil
	}
	if s.atOrOverLimit(ctx, s.shortKey(orgID), s.shortLimit) {
		return ErrCapExceeded
	}
	if s.atOrOverLimit(ctx, s.dailyKey(orgID), s.dailyLimit) {
		return ErrCapExceeded
	}
	return nil
}

func (s *creditService) atOrOverLimit(ctx context.Context, key string, limit int) bool {
	n, err := s.cache.Get(ctx, key).Int()
	if err == redis.Nil {
		return false
	}
	if err != nil {
		// Fail open on cache errors.
		return false
	}
	return n >= limit
}

// commitCaps increments both windows after a successful fresh debit, setting the
// TTLs on first write. Best-effort: a Redis failure here never fails the
// already-completed generation.
func (s *creditService) commitCaps(ctx context.Context, orgID uuid.UUID) {
	if s.cache == nil {
		return
	}
	s.bump(ctx, s.shortKey(orgID), WindowShort)
	s.bump(ctx, s.dailyKey(orgID), WindowDaily)
}

func (s *creditService) bump(ctx context.Context, key string, window time.Duration) {
	pipe := s.cache.Pipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, window)
	_, _ = pipe.Exec(ctx)
}
