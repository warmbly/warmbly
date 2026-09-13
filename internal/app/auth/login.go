package auth

import (
	"context"
	"github.com/warmbly/warmbly/internal/app/authrisk"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify/templates"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

func (s *authService) LoginStart(ctx context.Context, data *AuthData, ipaddr, userAgent string) (*models.AuthSession, *errx.Error) {
	if s.policy.DisablePasswordLogin {
		return nil, errx.New(errx.Forbidden, "password sign-in is disabled on this deployment")
	}

	if xerr := s.captcha.Verify(ctx, data.Turnstile, ipaddr); xerr != nil {
		errs.CaptureException(xerr)
		return nil, xerr
	}

	uid, err := s.authRepository.IsValidCredentials(ctx, data.Email, data.Password)
	if err != nil {
		return nil, err
	}

	// The emailed code is a step in the login, not a second factor: NIST
	// SP 800-63B and OWASP ASVS both decline to count email as one. When it is
	// off, or the device is already known, the login completes here and never
	// touches the mail transport.
	// Assessed once here: the verdict that decides the challenge is the same
	// one recorded when the sign-in completes.
	verdict := s.assessLogin(ctx, uid, ipaddr)
	if !s.loginCodeRequired(ctx, uid, userAgent, verdict) {
		result, ferr := s.finishLoginWith(ctx, uid, ipaddr, userAgent, &verdict)
		if ferr != nil {
			return nil, ferr
		}
		return &models.AuthSession{
			CodeRequired:  false,
			Token:         result.Token,
			TwoFARequired: result.TwoFARequired,
			PendingToken:  result.PendingToken,
			ExpiresIn:     result.ExpiresIn,
		}, nil
	}

	if err := s.canSendEmail(ctx, emailFlowLogin, data.Email); err != nil {
		return nil, err
	}

	issuedAt := time.Now()
	expiresAt := issuedAt.Add(AuthSessionTTL)

	sessionID := uuid.New()
	nonce, xerr := crypt.Nonce()
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	code, xerr := crypt.VerificationCode()
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	text, xerr := templates.GenerateLoginCodeHTML(code)
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	if xerr := s.sendAuthEmail(ctx, data.Email, "Your Login Code", text); xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.ErrMailUndeliverable
	}

	codeHash, xerr := argon2.Hash(code)
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	session := &models.LoginSession{
		CodeHash:      codeHash,
		Nonce:         nonce,
		AnomalyReason: verdict.Reason,
	}

	sessionToken, xerr := s.tokenService.GenerateToken(uid, sessionID, "", nonce, issuedAt, expiresAt)
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	if err := s.saveLoginSession(ctx, sessionID, session, expiresAt); err != nil {
		return nil, err
	}

	return &models.AuthSession{
		Session:      sessionToken,
		CodeRequired: true,
	}, nil
}

// loginCodeRequired applies AUTH_LOGIN_CODE. A transport that cannot deliver
// never demands a code, because there would be no way to complete the login.
func (s *authService) loginCodeRequired(ctx context.Context, userID uuid.UUID, userAgent string, verdict authrisk.Verdict) bool {
	if !s.mailDelivers {
		return false
	}
	// A named account an operator excused, for a reviewer or auditor who has
	// to sign in and cannot read this instance's mail. Checked before the
	// policy so it holds under "always" too, which is the mode that would
	// otherwise make such a review impossible. A read failure is not an
	// exemption: the code is still demanded.
	if exempt, err := s.userRepository.IsLoginCodeExempt(ctx, userID); err != nil {
		errs.CaptureExceptionContext(ctx, err, errs.Tag("area", "login_code_exempt"))
	} else if exempt {
		return false
	}
	switch s.policy.LoginCode {
	case config.LoginCodeOff:
		// An operator who turned codes off has made that choice explicitly,
		// and overriding it would mean this deployment can never sign in
		// without mail. Anomalies are still recorded for review.
		return false
	case config.LoginCodeNewDevice:
		// A familiar browser in an impossible place is exactly the case the
		// device fingerprint cannot catch: the attacker has the cookie.
		return !s.isKnownDevice(ctx, userID, userAgent) || verdict.Flagged
	default:
		return true
	}
}

func (s *authService) LoginConfirm(ctx context.Context, data *ConfirmData, session, ipaddr string, userAgent string) (*models.LoginResult, *errx.Error) {
	atoken, err := s.tokenService.VerifyToken(session)
	if err != nil {
		return nil, err
	}
	if atoken.ExpiresAt.Before(time.Now()) {
		return nil, errx.ErrSession
	}
	sess, err := s.getLoginSession(ctx, atoken.SessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil || sess.Nonce != atoken.Nonce {
		return nil, errx.ErrSession
	}

	if sess.Tries >= AuthAttempts {
		return nil, errx.ErrCodeLimit
	}

	v, xerr := argon2.Verify(data.Code, sess.CodeHash)
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	if !v {
		sess.Tries++
		_ = s.saveLoginSession(ctx, atoken.SessionID, sess, atoken.ExpiresAt.Time)
		return nil, errx.ErrCode
	}

	// Consume the session on success so it cannot be re-confirmed to mint fresh
	// 2FA pending tokens, which would reset the per-pending attempt counter.
	// One email confirmation means exactly one challenge.
	_ = s.cache.Del(ctx, getLoginSessionKey(atoken.SessionID)).Err()

	return s.finishLoginWith(ctx, atoken.UserID, ipaddr, userAgent, challengeVerdict(sess))
}

// challengeVerdict recovers the verdict that issued this challenge rather than
// reaching a fresh one: history has moved on since, and the confirming request
// can arrive from a different address, so re-judging could store a challenged
// sign-in as clean.
func challengeVerdict(sess *models.LoginSession) *authrisk.Verdict {
	return &authrisk.Verdict{Flagged: sess.AnomalyReason != "", Reason: sess.AnomalyReason}
}

func (s *authService) finishLogin(ctx context.Context, userID uuid.UUID, ipaddr, userAgent string) (*models.LoginResult, *errx.Error) {
	return s.finishLoginWith(ctx, userID, ipaddr, userAgent, nil)
}

func (s *authService) finishLoginWith(ctx context.Context, userID uuid.UUID, ipaddr, userAgent string, verdict *authrisk.Verdict) (*models.LoginResult, *errx.Error) {
	return s.finishLoginAsWith(ctx, userID, ipaddr, userAgent, token.AuthProviderEmail, verdict)
}

// finishLoginAs is everything that happens once a caller has proved they own
// the account: ban enforcement, the 2FA gate, and session issuance.
//
// Every login path goes through it. Previously only the password path checked
// 2FA, so a user who enrolled TOTP could still be signed in without it through
// Apple, Google or a passkey, and the browser OAuth paths skipped the ban check
// as well.
func (s *authService) finishLoginAs(ctx context.Context, userID uuid.UUID, ipaddr, userAgent, provider string) (*models.LoginResult, *errx.Error) {
	return s.finishLoginAsWith(ctx, userID, ipaddr, userAgent, provider, nil)
}

// finishLoginAsWith takes the verdict the caller already reached, if it has
// one. A nil verdict is assessed here, which is right for the paths that
// authenticate and complete in the same request.
func (s *authService) finishLoginAsWith(ctx context.Context, userID uuid.UUID, ipaddr, userAgent, provider string, verdict *authrisk.Verdict) (*models.LoginResult, *errx.Error) {
	// Ban-scope enforcement (migration 000045). The runtime treats
	// BanScopeLogin as "this account cannot authenticate" — the row's
	// banned_at is set in tandem so legacy callers still see the user
	// as banned, but the bit makes the rule auditable.
	if scope, scopeErr := s.userRepository.GetBanState(ctx, userID); scopeErr == nil {
		if models.BanScope(scope).Has(models.BanScopeLogin) {
			return nil, errx.New(errx.Forbidden, "this account has been suspended")
		}
	}

	// 2FA gate: if the user has TOTP enabled, issue a single-use pending
	// challenge instead of a full session. The FE distinguishes on
	// two_fa_required and POSTs /auth/2fa/verify next.
	if s.twofa != nil {
		if enabled, _ := s.twofa.IsEnabled(ctx, userID); enabled {
			pendTok, expiresIn, perr := s.twofa.CreatePendingChallenge(ctx, userID)
			if perr != nil {
				return nil, perr
			}
			return &models.LoginResult{TwoFARequired: true, PendingToken: pendTok, ExpiresIn: expiresIn}, nil
		}
	}

	// Record where this sign-in came from, whatever route reached here, so the
	// next one has something to compare against.
	if verdict == nil {
		v := s.assessLogin(ctx, userID, ipaddr)
		verdict = &v
	}
	s.recordLogin(ctx, userID, ipaddr, userAgent, *verdict)

	newToken, err := s.tokenService.GenerateSession(ctx, userID, "", ipaddr, userAgent, provider)
	if err != nil {
		return nil, err
	}

	s.rememberDevice(ctx, userID, userAgent)

	return &models.LoginResult{Token: newToken}, nil
}
