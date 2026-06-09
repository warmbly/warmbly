package twofa

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	issuer        = "Warmbly"
	pendingTTL    = 5 * time.Minute
	maxTries      = 5  // per pending-session code attempts
	ipLimit       = 20 // verify attempts per IP per window
	ipWindow      = 15 * time.Minute
	recoveryCount = 10
)

// EnrollStart is the one-time secret + provisioning URI shown during enrollment.
type EnrollStart struct {
	Secret     string `json:"secret"`
	OtpauthURI string `json:"otpauth_uri"`
}

type Service interface {
	IsEnabled(ctx context.Context, userID uuid.UUID) (bool, error)
	EnrollStart(ctx context.Context, userID uuid.UUID) (*EnrollStart, *errx.Error)
	EnrollConfirm(ctx context.Context, userID uuid.UUID, code string) ([]string, *errx.Error)
	Disable(ctx context.Context, userID uuid.UUID, code string) *errx.Error
	// CreatePendingChallenge mints a short-lived single-use pending token for a
	// 2FA login challenge (called from the login gate after the email code).
	CreatePendingChallenge(ctx context.Context, userID uuid.UUID) (string, int, *errx.Error)
	// VerifyLogin exchanges a pending token + code (TOTP or recovery) for a
	// real session.
	VerifyLogin(ctx context.Context, pendingToken, code, ipaddr, userAgent string) (*models.Token, *errx.Error)
}

type service struct {
	repo    repository.TOTPRepository
	users   repository.UserRepository
	tokens  token.TokenService
	cache   *cache.Cache
	sealKey [32]byte
}

func NewService(repo repository.TOTPRepository, users repository.UserRepository, tokens token.TokenService, c *cache.Cache, sealKey [32]byte) Service {
	return &service{repo: repo, users: users, tokens: tokens, cache: c, sealKey: sealKey}
}

func (s *service) IsEnabled(ctx context.Context, userID uuid.UUID) (bool, error) {
	return s.repo.IsEnabled(ctx, userID)
}

// Disable removes 2FA, requiring a valid current TOTP or recovery code (proof of
// possession) — a hijacked live session can't silently strip 2FA.
func (s *service) Disable(ctx context.Context, userID uuid.UUID, code string) *errx.Error {
	row, err := s.repo.Get(ctx, userID)
	if err != nil {
		return errx.InternalError()
	}
	if row == nil || !row.Enabled {
		return errx.New(errx.BadRequest, "2FA is not enabled")
	}
	if !s.validCode(ctx, userID, row, code) {
		return errx.New(errx.BadRequest, "Invalid code")
	}
	if err := s.repo.Delete(ctx, userID); err != nil {
		return errx.InternalError()
	}
	return nil
}

// validCode checks a code against the user's TOTP secret OR consumes a matching
// recovery code. Used by both Disable and VerifyLogin.
func (s *service) validCode(ctx context.Context, userID uuid.UUID, row *models.UserTOTP, code string) bool {
	if isRecoveryFormat(code) {
		return s.tryConsumeRecoveryCode(ctx, userID, code)
	}
	secret, err := Open(s.sealKey, row.SecretSealed)
	if err != nil {
		return false
	}
	return ValidateCode(secret, code)
}

// --- pending-challenge cache (Redis, mirrors the auth login_sess pattern) ---

func pendingKey(sid uuid.UUID) string { return "2fa_pending:" + sid.String() }

func (s *service) savePending(ctx context.Context, sid uuid.UUID, p *models.TwoFAPending, ttl time.Duration) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return s.cache.Set(ctx, pendingKey(sid), data, ttl).Err()
}

func (s *service) getPending(ctx context.Context, sid uuid.UUID) (*models.TwoFAPending, error) {
	data, err := s.cache.Get(ctx, pendingKey(sid)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}
	var p models.TwoFAPending
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *service) deletePending(ctx context.Context, sid uuid.UUID) {
	_ = s.cache.Del(ctx, pendingKey(sid)).Err()
}

// ipAllowed is a coarse per-IP verify limiter (RateLimitMiddleware is a no-op
// pre-login). Fail-open on a cache error so we never lock everyone out.
func (s *service) ipAllowed(ctx context.Context, ip string) bool {
	if ip == "" {
		return true
	}
	key := "2fa_verify_ip:" + ip
	n, err := s.cache.Incr(ctx, key).Result()
	if err != nil {
		return true
	}
	if n == 1 {
		_ = s.cache.Expire(ctx, key, ipWindow).Err()
	}
	return n <= ipLimit
}
