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
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	issuer        = "Warmbly"
	pendingTTL    = 5 * time.Minute
	maxTries      = 5  // per pending-session code attempts
	ipLimit       = 20 // verify attempts per IP per window
	ipWindow      = 15 * time.Minute
	userLimit     = 10 // sign-in code attempts per account per window, across challenges
	userWindow    = 15 * time.Minute
	recoveryCount = 10
)

// InvalidCodeID is the response code for a TOTP or recovery code that did not match.
const InvalidCodeID = "two_fa_invalid_code"

const UsedCodeID = "two_fa_code_used"

// ErrInvalidCode is the answer to a TOTP or recovery code that did not match.
// It carries a stable identifier so a client can show an inline "try again"
// instead of a generic failure.
func ErrInvalidCode() *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, InvalidCodeID, "That code didn't match. Check your authenticator and try again.")
}

// EnrollStart is the one-time secret + provisioning URI shown during enrollment.
// The parameters are spelled out for the manual-entry path, so a client never
// has to parse them back out of the URI.
type EnrollStart struct {
	Secret     string `json:"secret"`
	OtpauthURI string `json:"otpauth_uri"`
	Issuer     string `json:"issuer"`
	Account    string `json:"account"`
	Algorithm  string `json:"algorithm"`
	Digits     int    `json:"digits"`
	Period     int    `json:"period"`
}

// Status is what the security settings page shows about the user's 2FA.
type Status struct {
	Enabled                bool       `json:"enabled"`
	ConfirmedAt            *time.Time `json:"confirmed_at,omitempty"`
	RecoveryCodesRemaining int        `json:"recovery_codes_remaining"`
	RecoveryCodesTotal     int        `json:"recovery_codes_total"`
}

type Service interface {
	IsEnabled(ctx context.Context, userID uuid.UUID) (bool, error)
	Status(ctx context.Context, userID uuid.UUID) (*Status, error)
	EnrollStart(ctx context.Context, userID uuid.UUID) (*EnrollStart, *errx.Error)
	EnrollConfirm(ctx context.Context, userID uuid.UUID, code string) ([]string, *errx.Error)
	Disable(ctx context.Context, userID uuid.UUID, code string) *errx.Error
	// RegenerateRecoveryCodes replaces every recovery code with a fresh set,
	// returned in plaintext once. Requires a current TOTP or recovery code.
	RegenerateRecoveryCodes(ctx context.Context, userID uuid.UUID, code string) ([]string, *errx.Error)
	// VerifyCurrentCode checks a TOTP or recovery code for a user who is
	// already signed in, without changing anything. Used by the re-auth
	// endpoint so someone with 2FA on can confirm with their authenticator
	// rather than retyping a password they may not have (passkey and SSO
	// accounts often have none).
	VerifyCurrentCode(ctx context.Context, userID uuid.UUID, code string) (bool, *errx.Error)
	// CreatePendingChallenge mints a short-lived single-use pending token for a
	// 2FA login challenge. authProvider is what the session will record; link,
	// when set, is a federated identity attached only once the code passes.
	CreatePendingChallenge(ctx context.Context, userID uuid.UUID, authProvider string, link *models.UserIdentity) (string, int, *errx.Error)
	// VerifyLogin exchanges a pending token + code (TOTP or recovery) for a
	// real session.
	VerifyLogin(ctx context.Context, pendingToken, code, ipaddr, userAgent string) (*models.Token, *errx.Error)
	// WireIdentityLinker attaches the store a linking challenge writes to.
	WireIdentityLinker(l IdentityLinker)
}

// IdentityLinker binds a federated identity to an account. Satisfied by
// repository.IdentityRepository.
type IdentityLinker interface {
	Link(ctx context.Context, userID uuid.UUID, identity models.UserIdentity) error
}

type service struct {
	repo     repository.TOTPRepository
	users    repository.UserRepository
	tokens   token.TokenService
	cache    *cache.Cache
	sealKey  [32]byte
	readKeys [][32]byte
	linker   IdentityLinker
}

func (s *service) WireIdentityLinker(l IdentityLinker) { s.linker = l }

func NewService(repo repository.TOTPRepository, users repository.UserRepository, tokens token.TokenService, c *cache.Cache, sealKey [32]byte, readKeys ...[32]byte) Service {
	return &service{repo: repo, users: users, tokens: tokens, cache: c, sealKey: sealKey, readKeys: readKeys}
}

func (s *service) openSecret(sealed string) (string, error) {
	secret, err := Open(s.sealKey, sealed)
	if err == nil {
		return secret, nil
	}
	for _, key := range s.readKeys {
		if secret, readErr := Open(key, sealed); readErr == nil {
			return secret, nil
		}
	}
	return "", err
}

func (s *service) IsEnabled(ctx context.Context, userID uuid.UUID) (bool, error) {
	return s.repo.IsEnabled(ctx, userID)
}

func (s *service) Status(ctx context.Context, userID uuid.UUID) (*Status, error) {
	row, err := s.repo.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if row == nil || !row.Enabled {
		return &Status{}, nil
	}
	unused, total, err := s.repo.CountRecoveryCodes(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &Status{Enabled: true, ConfirmedAt: row.ConfirmedAt, RecoveryCodesRemaining: unused, RecoveryCodesTotal: total}, nil
}

// RegenerateRecoveryCodes issues a new set and retires the old one in the same
// write, so a leaked sheet stops working the moment the new one is shown.
func (s *service) RegenerateRecoveryCodes(ctx context.Context, userID uuid.UUID, code string) ([]string, *errx.Error) {
	row, err := s.repo.Get(ctx, userID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if row == nil || !row.Enabled {
		return nil, errx.New(errx.BadRequest, "2FA is not enabled")
	}
	if xerr := s.checkCode(ctx, userID, row, code); xerr != nil {
		return nil, xerr
	}
	codes, hashes, err := generateRecoveryCodes()
	if err != nil {
		return nil, errx.InternalError()
	}
	if err := s.repo.InsertRecoveryCodes(ctx, userID, hashes); err != nil {
		return nil, errx.InternalError()
	}
	return codes, nil
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
	if xerr := s.checkCode(ctx, userID, row, code); xerr != nil {
		return xerr
	}
	if err := s.repo.Delete(ctx, userID); err != nil {
		return errx.InternalError()
	}
	return nil
}

// VerifyCurrentCode reports whether the code is a valid TOTP or recovery code
// for this user right now. A recovery code is consumed, and a TOTP step is
// retired, exactly as they are at sign-in: a code that has confirmed something
// must not confirm a second thing.
func (s *service) VerifyCurrentCode(ctx context.Context, userID uuid.UUID, code string) (bool, *errx.Error) {
	row, err := s.repo.Get(ctx, userID)
	if err != nil {
		errs.CaptureException(err)
		return false, errx.InternalError()
	}
	if row == nil || !row.Enabled {
		return false, nil
	}
	if xerr := s.checkCode(ctx, userID, row, code); xerr != nil {
		if xerr.Identifier == InvalidCodeID {
			return false, nil
		}
		return false, xerr
	}
	return true, nil
}

// checkCode accepts each authenticator step or recovery code once.
func (s *service) checkCode(ctx context.Context, userID uuid.UUID, row *models.UserTOTP, code string) *errx.Error {
	if isRecoveryFormat(code) {
		return s.tryConsumeRecoveryCode(ctx, userID, code)
	}
	secret, err := s.openSecret(row.SecretSealed)
	if err != nil {
		errs.CaptureException(ErrUnreadableSecret)
		return errx.InternalError()
	}
	step, ok := ValidateCodeStep(secret, code)
	if !ok {
		return ErrInvalidCode()
	}
	// Consume against the same enrollment whose code was verified.
	fresh, cerr := s.repo.ConsumeTOTPStep(ctx, userID, step, row.SecretSealed)
	if cerr != nil {
		if errors.Is(cerr, repository.ErrTOTPEnrollmentChanged) {
			return ErrInvalidCode()
		}
		errs.CaptureException(cerr)
		return errx.InternalError()
	}
	if !fresh {
		return errx.NewWithIdentifier(errx.BadRequest, UsedCodeID, "Wait for a new authenticator code, then try again.")
	}
	return nil
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

// deletePending drops the challenge and its try counter, reporting whether this
// call removed the challenge.
func (s *service) deletePending(ctx context.Context, sid uuid.UUID) bool {
	n, err := s.cache.Del(ctx, pendingKey(sid)).Result()
	_ = s.cache.Del(ctx, pendingTriesKey(sid)).Err()
	return err == nil && n == 1
}

func pendingTriesKey(sid uuid.UUID) string { return pendingKey(sid) + ":tries" }

// userTriesKey counts codes tried against one account across all its challenges.
func userTriesKey(uid uuid.UUID) string { return "2fa_verify_user:" + uid.String() }

// reserveAttempt charges one try before a code is compared. A cache error
// refuses with a retryable 503, so no code is compared uncharged.
func (s *service) reserveAttempt(ctx context.Context, key string, limit int64, window time.Duration) (bool, *errx.Error) {
	ok, err := s.cache.ReserveAttempt(ctx, key, limit, window)
	if err != nil {
		errs.CaptureException(err)
		return false, errx.ErrAttemptCheckUnavailable
	}
	return ok, nil
}

// releaseAttempt refunds a try that compared no code.
func (s *service) releaseAttempt(ctx context.Context, key string) {
	if err := s.cache.ReleaseAttempt(ctx, key); err != nil {
		errs.CaptureException(err)
	}
}

// ipAllowed is a coarse per-IP verify limiter (RateLimitMiddleware is a no-op
// pre-login). A cache error refuses like the per-account budget does.
func (s *service) ipAllowed(ctx context.Context, ip string) (bool, *errx.Error) {
	if ip == "" {
		return true, nil
	}
	return s.reserveAttempt(ctx, "2fa_verify_ip:"+ip, ipLimit, ipWindow)
}
