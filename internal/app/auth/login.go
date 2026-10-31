package auth

import (
	"context"
	"errors"
	"github.com/warmbly/warmbly/internal/app/appattest"
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

	// A failed challenge is the caller's 400, not an incident: reporting it
	// filed an issue for every bot and every reloaded sign-in page.
	if xerr := s.verifyHuman(ctx, appattest.ActionLogin, data.Turnstile, data.AppAttest, ipaddr); xerr != nil {
		return nil, xerr
	}

	// The credential lookup, the send budget and the emailed code all key on
	// this string, so it is folded once here rather than at each of them.
	data.Email = normalizeEmail(data.Email)

	// The attempt is charged before the hash comparison, so parallel guesses
	// cannot share one slot and a guesser past the limit cannot even measure
	// argon2's timing.
	if ok, xerr := s.reserveLoginAttempt(ctx, data.Email); xerr != nil {
		return nil, xerr
	} else if !ok {
		return nil, errx.ErrAuthLimit
	}

	uid, err := s.authRepository.IsValidCredentials(ctx, data.Email, data.Password)
	if err != nil {
		if !errors.Is(err, errx.ErrCredentials) {
			s.releaseLoginAttempt(ctx, data.Email)
		}
		return nil, err
	}
	s.clearLoginFailures(ctx, data.Email)

	// Assessed once here: the verdict that decides the challenge is the same
	// one recorded when the sign-in completes.
	verdict := s.assessLogin(ctx, uid, ipaddr)

	// An issued initial password signs nobody in; its holder chooses their own first.
	pending, xerr := s.authRepository.InitialPasswordPending(ctx, uid)
	if xerr != nil {
		return nil, xerr
	}
	if pending {
		return s.createInitialPasswordChallenge(ctx, uid, data.Email, verdict)
	}

	return s.continuePasswordLogin(ctx, uid, data.Email, data.DeviceToken, verdict, ipaddr, userAgent)
}

// continuePasswordLogin is everything after a password is accepted: the
// emailed code unless excused, then completeLogin with its ban and 2FA gates.
func (s *authService) continuePasswordLogin(ctx context.Context, uid uuid.UUID, email, deviceToken string, verdict authrisk.Verdict, ipaddr, userAgent string) (*models.AuthSession, *errx.Error) {
	// The emailed code is a step in the login, not a second factor: NIST
	// SP 800-63B and OWASP ASVS both decline to count email as one. When it is
	// off, or the device is already known, the login completes here and never
	// touches the mail transport.
	if !s.loginCodeRequired(ctx, uid, deviceToken, verdict) {
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

	if err := s.canSendEmail(ctx, emailFlowLogin, email); err != nil {
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

	// The transport reports the send failure itself; capturing it again here
	// filed the same rejection as a second issue under a second file.
	if xerr := s.sendAuthEmail(ctx, email, "Your Login Code", text); xerr != nil {
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

	sessionToken, xerr := s.tokenService.GenerateTokenFor(token.PurposeLoginCode, uid, sessionID, "", nonce, issuedAt, expiresAt)
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
func (s *authService) loginCodeRequired(ctx context.Context, userID uuid.UUID, deviceToken string, verdict authrisk.Verdict) bool {
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
		// A trusted browser in an impossible place is exactly the case the
		// device token cannot catch: the attacker has the token.
		return !s.isTrustedDevice(ctx, userID, deviceToken) || verdict.Flagged
	default:
		return true
	}
}

func (s *authService) LoginConfirm(ctx context.Context, data *ConfirmData, session, ipaddr string, userAgent string) (*models.LoginResult, *errx.Error) {
	atoken, err := s.tokenService.VerifyTokenFor(token.PurposeLoginCode, session)
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

	sessKey := getLoginSessionKey(atoken.SessionID)
	if ok, xerr := s.reserveCodeAttempt(ctx, sessKey, time.Until(atoken.ExpiresAt.Time)); xerr != nil {
		return nil, xerr
	} else if !ok {
		return nil, errx.ErrCodeLimit
	}

	v, xerr := argon2.Verify(data.Code, sess.CodeHash)
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	if !v {
		return nil, errx.ErrCode
	}

	// Consumed on success, and only the request that deletes it may continue,
	// so one emailed code completes exactly one sign-in.
	if !s.consumeCodeSession(ctx, sessKey) {
		return nil, errx.ErrSession
	}

	result, ferr := s.finishLoginWith(ctx, atoken.UserID, ipaddr, userAgent, challengeVerdict(sess))
	if ferr != nil {
		return nil, ferr
	}
	// Issued once the code is proved, even ahead of a TOTP step: the token
	// stands in for this emailed code and never for the second factor.
	if data.RememberDevice {
		result.DeviceToken = s.trustDevice(ctx, atoken.UserID)
	}
	return result, nil
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
// Every login path goes through it, so the ban check and the 2FA gate hold for
// password, Apple, Google, passkey and browser OAuth sign-ins alike.
func (s *authService) finishLoginAs(ctx context.Context, userID uuid.UUID, ipaddr, userAgent, provider string) (*models.LoginResult, *errx.Error) {
	return s.finishLoginAsWith(ctx, userID, ipaddr, userAgent, provider, nil)
}

// refuseSuspended is the ban-scope check (migration 000045): BanScopeLogin
// means the account cannot authenticate, whatever it presents. A ban state
// that cannot be read refuses the sign-in.
func (s *authService) refuseSuspended(ctx context.Context, userID uuid.UUID) *errx.Error {
	scope, err := s.userRepository.GetBanState(ctx, userID)
	if err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}
	if models.BanScope(scope).Has(models.BanScopeLogin) {
		return errx.ErrAccountSuspended
	}
	return nil
}

// finishLoginAsWith takes the verdict the caller already reached, if it has
// one. A nil verdict is assessed here, which is right for the paths that
// authenticate and complete in the same request.
func (s *authService) finishLoginAsWith(ctx context.Context, userID uuid.UUID, ipaddr, userAgent, provider string, verdict *authrisk.Verdict) (*models.LoginResult, *errx.Error) {
	return s.completeLogin(ctx, userID, ipaddr, userAgent, provider, verdict, nil)
}

// completeLogin is the one path behind every sign-in: ban check, 2FA gate,
// then the session. link, when set, is a federated identity attached only
// once every gate has passed (inside the 2FA verify on a 2FA account).
func (s *authService) completeLogin(ctx context.Context, userID uuid.UUID, ipaddr, userAgent, provider string, verdict *authrisk.Verdict, link *models.UserIdentity) (*models.LoginResult, *errx.Error) {
	if xerr := s.refuseSuspended(ctx, userID); xerr != nil {
		return nil, xerr
	}

	// 2FA gate: if the user has TOTP enabled, issue a single-use pending
	// challenge instead of a full session. The FE distinguishes on
	// two_fa_required and POSTs /auth/2fa/verify next.
	if s.twofa != nil {
		enabled, err := s.twofa.IsEnabled(ctx, userID)
		if err != nil {
			errs.CaptureException(err)
			return nil, errx.InternalError()
		}
		if enabled {
			pendTok, expiresIn, perr := s.twofa.CreatePendingChallenge(ctx, userID, provider, link)
			if perr != nil {
				return nil, perr
			}
			return &models.LoginResult{TwoFARequired: true, PendingToken: pendTok, ExpiresIn: expiresIn}, nil
		}
	}

	if link != nil {
		if xerr := s.linkIdentity(ctx, userID, *link); xerr != nil {
			return nil, xerr
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

	return &models.LoginResult{Token: newToken}, nil
}
