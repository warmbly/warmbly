package auth

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/app/authrisk"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

// initialPasswordTTL is how long the choose-your-password form has: the login-code window.
const initialPasswordTTL = AuthSessionTTL

func getInitialPasswordKey(sessionID uuid.UUID) string {
	return "initial_pw:" + sessionID.String()
}

// createInitialPasswordChallenge parks a password sign-in made with an issued
// initial password behind a single-use token only CompleteInitialPassword takes.
func (s *authService) createInitialPasswordChallenge(ctx context.Context, userID uuid.UUID, email string, verdict authrisk.Verdict) (*models.AuthSession, *errx.Error) {
	sid := uuid.New()
	nonce, err := crypt.Nonce()
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	now := time.Now()
	pendTok, xerr := s.tokenService.GenerateTokenFor(token.PurposeInitialPassword, userID, sid, "", nonce, now, now.Add(initialPasswordTTL))
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}
	pending := &models.InitialPasswordPending{UserID: userID, Nonce: nonce, Email: email, AnomalyReason: verdict.Reason}
	data, merr := json.Marshal(pending)
	if merr != nil {
		errs.CaptureException(merr)
		return nil, errx.InternalError()
	}
	if err := s.cache.Set(ctx, getInitialPasswordKey(sid), data, initialPasswordTTL).Err(); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	return &models.AuthSession{
		PasswordChangeRequired: true,
		PendingToken:           pendTok,
		ExpiresIn:              int(initialPasswordTTL.Seconds()),
	}, nil
}

func (s *authService) getInitialPasswordPending(ctx context.Context, sessionID uuid.UUID) (*models.InitialPasswordPending, *errx.Error) {
	data, err := s.cache.Get(ctx, getInitialPasswordKey(sessionID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	var pending models.InitialPasswordPending
	if err := json.Unmarshal(data, &pending); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	return &pending, nil
}

// CompleteInitialPassword replaces an issued initial password with the
// holder's own, then continues the sign-in exactly as LoginStart would have.
func (s *authService) CompleteInitialPassword(ctx context.Context, data *InitialPasswordData, ipaddr, userAgent string) (*models.AuthSession, *errx.Error) {
	if data == nil || data.PendingToken == "" {
		return nil, errx.ErrPasswordChangeExpired
	}
	if s.policy != nil && s.policy.DisablePasswordLogin {
		return nil, errx.New(errx.Forbidden, "password sign-in is disabled on this deployment")
	}
	// VerifyTokenFor requires an expiry and refuses an expired token.
	claims, xerr := s.tokenService.VerifyTokenFor(token.PurposeInitialPassword, data.PendingToken)
	if xerr != nil {
		return nil, errx.ErrPasswordChangeExpired
	}
	pend, xerr := s.getInitialPasswordPending(ctx, claims.SessionID)
	if xerr != nil {
		return nil, xerr
	}
	if pend == nil || pend.Nonce != claims.Nonce || pend.UserID != claims.UserID {
		return nil, errx.ErrPasswordChangeExpired
	}

	if perr := crypt.PasswordError(data.Password); perr != nil {
		return nil, perr
	}
	// Empty once the issued password has run out or been replaced.
	oldHash, xerr := s.authRepository.GetPasswordHash(ctx, pend.UserID)
	if xerr != nil {
		return nil, xerr
	}
	if oldHash == "" {
		return nil, errx.ErrPasswordChangeExpired
	}
	same, verr := argon2.Verify(data.Password, oldHash)
	if verr != nil {
		errs.CaptureException(verr)
		return nil, errx.InternalError()
	}
	if same {
		return nil, errx.ErrPasswordReused
	}
	newHash, herr := argon2.Hash(data.Password)
	if herr != nil {
		errs.CaptureException(herr)
		return nil, errx.InternalError()
	}

	// Sessions end before the write, and a failure leaves the challenge for a retry.
	if s.tokenService != nil {
		if err := s.tokenService.RevokeOtherSessions(ctx, pend.UserID, uuid.Nil); err != nil {
			errs.CaptureException(err)
			return nil, ErrSessionsNotEnded
		}
	}
	if err := s.ForgetTrustedDevices(ctx, pend.UserID); err != nil {
		return nil, err
	}
	// Only the request that deletes the challenge may write, so it is spent once.
	if !s.consumeCodeSession(ctx, getInitialPasswordKey(claims.SessionID)) {
		return nil, errx.ErrPasswordChangeExpired
	}
	replaced, xerr := s.authRepository.ReplaceInitialPassword(ctx, pend.UserID, oldHash, newHash)
	if xerr != nil {
		return nil, xerr
	}
	if !replaced {
		return nil, errx.ErrPasswordChangeExpired
	}

	verdict := authrisk.Verdict{Flagged: pend.AnomalyReason != "", Reason: pend.AnomalyReason}
	return s.continuePasswordLogin(ctx, pend.UserID, pend.Email, data.DeviceToken, verdict, ipaddr, userAgent)
}
