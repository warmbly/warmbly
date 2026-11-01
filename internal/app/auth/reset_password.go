package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/appattest"
	tokenpkg "github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify/templates"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

func (s *authService) ResetPasswordStart(ctx context.Context, data *ResetPasswordStart, ipaddr string) *errx.Error {
	// The caller's 400, not an incident. See LoginStart.
	if err := s.verifyHuman(ctx, appattest.ActionResetPassword, data.Turnstile, data.AppAttest, ipaddr); err != nil {
		return err
	}

	// Before the budget as well as the lookup, so the same address typed two
	// ways spends one budget rather than two.
	data.Email = normalizeEmail(data.Email)

	// Spend the budget before the lookup, and key it on the submitted address,
	// so an unknown address costs the attacker the same as a known one.
	if err := s.passwordResetLimit(ctx, data.Email); err != nil {
		return err
	}

	xerr := s.startPasswordReset(ctx, data)
	// A failure here produced no mail, so it does not spend the address's
	// allowance. The budget is two requests per four hours: charging our own
	// faults to it meant one bad afternoon locked a real person out of the
	// only self-service way back into their account, and the second attempt
	// failed for a different reason than the first. An unknown address
	// deliberately still pays, because it returns nil rather than an error.
	if xerr != nil {
		s.refundPasswordResetLimit(ctx, data.Email)
	}
	return xerr
}

func (s *authService) startPasswordReset(ctx context.Context, data *ResetPasswordStart) *errx.Error {
	user, uerr := s.userRepository.GetUserByEmail(ctx, data.Email)
	if uerr != nil {
		// Only "no such account" is answered 200. Returning ErrUser here was an
		// enumeration oracle, but swallowing EVERY error into one was worse: a
		// cache or database fault answered "Email successfully sent." and sent
		// nothing, which is indistinguishable to the person from a mail that
		// was delivered to a folder they cannot find. Anything that is not the
		// address being unknown is ours, and says so.
		if !errors.Is(uerr, errx.ErrUser) {
			errs.CaptureException(uerr)
			return errx.InternalError()
		}
		// Logged because this one really does answer 200: without a line here
		// a reset that reached nobody left no trace anywhere, so a mistyped
		// address and a broken transport looked identical from support.
		log.Info().Str("email", data.Email).Msg("password reset requested for an address with no account")
		return nil
	}

	u, xerr := s.userService.GetUser(ctx, user.ID)
	if xerr != nil {
		// Same reasoning: the account exists, so this is a cache or database
		// failure and never an unknown address. Reported rather than hidden
		// behind a success — this is the path a Redis quota outage took.
		errs.CaptureException(xerr)
		return errx.InternalError()
	}
	return s.emailResetLink(ctx, u, data.Email)
}

// ErrPasswordLoginDisabled refuses a password link on a deployment that turned
// password sign-in off, where the password could never be used.
var ErrPasswordLoginDisabled = errx.NewWithIdentifier(errx.Forbidden, "password_login_disabled",
	"Password sign-in is turned off on this server, so a password cannot be set.")

// SendPasswordLink emails the signed-in user a link to set a password, so an
// account created through a provider can move to email sign-in in place.
func (s *authService) SendPasswordLink(ctx context.Context, userID uuid.UUID) *errx.Error {
	if s.policy != nil && s.policy.DisablePasswordLogin {
		return ErrPasswordLoginDisabled
	}
	u, xerr := s.userService.GetUser(ctx, userID)
	if xerr != nil {
		return xerr
	}
	// Same budget as Forgot password, keyed on the same address.
	address := normalizeEmail(u.Email)
	if err := s.passwordResetLimit(ctx, address); err != nil {
		return err
	}
	if err := s.emailResetLink(ctx, u, address); err != nil {
		s.refundPasswordResetLimit(ctx, address)
		return err
	}
	return nil
}

// emailResetLink mints a single-use reset link for the account and mails it to
// address, which must be the account's own.
func (s *authService) emailResetLink(ctx context.Context, u *models.User, address string) *errx.Error {
	sessionID := uuid.New()
	nonce, err := crypt.Nonce()
	if err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}

	issuedAt := time.Now()
	expiresAt := issuedAt.Add(PasswordResetTTL)

	token, err := s.tokenService.GenerateTokenFor(tokenpkg.PurposePasswordReset, u.ID, sessionID, address, nonce, issuedAt, expiresAt)
	if err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}

	if err := s.saveResetPasswordSession(ctx, sessionID, nonce); err != nil {
		return err
	}

	url := config.GetPasswordResetURL(token, config.DashboardOriginFromContext(ctx))

	text, err := templates.GenerateResetPasswordHTML(u.FirstName, url, PasswordResetTTL)
	if err != nil {
		errs.CaptureException(err)
		return errx.InternalError()
	}

	// Reported by the transport; see LoginStart.
	if err := s.sendResetEmailWithRetry(ctx, u.Email, "Password Reset Confirmation", text); err != nil {
		return errx.ErrMailUndeliverable
	}

	return nil
}

// sendResetEmailWithRetry makes one transient failure survivable rather than
// final. The reset mail is the only self-service way back into an account, so
// a single refused connection or throttled SES call should cost a second of
// latency, not the whole attempt. Bounded to one retry and a short pause: the
// caller is a person holding an HTTP request open, and a rejection that is
// going to be permanent (an unverified identity, a suppressed address) repeats
// identically, so there is nothing to gain from trying harder.
func (s *authService) sendResetEmailWithRetry(ctx context.Context, to, subject, message string) error {
	err := s.sendAuthEmail(ctx, to, subject, message)
	if err == nil {
		return nil
	}
	// Nothing left to retry into: the caller gave up or the deadline passed.
	if ctx.Err() != nil {
		return err
	}
	select {
	case <-time.After(authEmailRetryDelay):
	case <-ctx.Done():
		return err
	}
	return s.sendAuthEmail(ctx, to, subject, message)
}

// ErrResetLinkInvalid answers a reset link or reset session that is expired,
// already used, or older than the account's current password.
var ErrResetLinkInvalid = errx.NewWithIdentifier(errx.Unauthorized, "reset_link_invalid",
	"This password reset link has expired or was already used. Request a new one with Forgot password on the sign-in page.")

// ErrSessionsNotEnded refuses a password write whose other sessions could not
// all be ended; the old password stays in place and the request can be retried.
var ErrSessionsNotEnded = &errx.Error{Code: errx.ServiceUnavailable, Identifier: "sessions_not_ended", RetryAfter: 5,
	Message: "Your password was not changed because the devices signed in to this account could not all be signed out. Wait a few seconds and try again."}

// ErrCredentialRevokeUnsupported refuses a password write that asked to end app
// grants and API keys on an instance that has no way to end them.
var ErrCredentialRevokeUnsupported = errx.NewWithIdentifier(errx.BadRequest, "api_credentials_revoke_unsupported",
	"This server cannot sign out connected apps and API keys, so your password was not changed. Untick that option and try again, then remove apps and keys from Settings.")

// verifyResetLink checks an emailed reset link token without spending it.
func (s *authService) verifyResetLink(ctx context.Context, linkToken string) (*tokenpkg.TokenClaims, *errx.Error) {
	sess, err := s.verifyResetLinkToken(ctx, linkToken)
	if err != nil && err.Code == errx.Unauthorized {
		return nil, ErrResetLinkInvalid
	}
	return sess, err
}

func (s *authService) verifyResetLinkToken(ctx context.Context, linkToken string) (*tokenpkg.TokenClaims, *errx.Error) {
	sess, err := s.tokenService.VerifyTokenFor(tokenpkg.PurposePasswordReset, linkToken)
	if err != nil {
		return nil, err
	}

	if sess.ExpiresAt == nil || sess.ExpiresAt.Before(time.Now()) {
		return nil, errx.ErrToken
	}

	nonce, err := s.getResetPasswordSession(ctx, sess.SessionID)
	if err != nil {
		return nil, err
	}

	if nonce != sess.Nonce {
		return nil, errx.ErrToken
	}

	// A link is only good for the password it was requested against. Once the
	// password has been written by any path (this flow, the signed-in change,
	// the operator CLI), every link issued before that write is dead, however
	// long its own expiry has left.
	changedAt, err := s.authRepository.PasswordChangedAt(ctx, sess.UserID)
	if err != nil {
		return nil, err
	}
	if resetLinkPredatesPassword(sess.IssuedAt, changedAt) {
		return nil, errx.ErrToken
	}
	return sess, nil
}

// ResetPasswordSession exchanges an emailed link token for an opaque reset
// session held server-side, so the confirm step carries no token in its URL.
// The link stays single-use: only the confirm step spends it.
func (s *authService) ResetPasswordSession(ctx context.Context, linkToken string) (*ResetPasswordSession, *errx.Error) {
	sess, err := s.verifyResetLink(ctx, linkToken)
	if err != nil {
		return nil, err
	}

	expiresAt := time.Now().Add(PasswordResetSessionTTL)
	if sess.ExpiresAt.Before(expiresAt) {
		expiresAt = sess.ExpiresAt.Time
	}

	a, nerr := crypt.Nonce()
	if nerr != nil {
		errs.CaptureException(nerr)
		return nil, errx.InternalError()
	}
	b, nerr := crypt.Nonce()
	if nerr != nil {
		errs.CaptureException(nerr)
		return nil, errx.InternalError()
	}
	handle := a + b

	if err := s.saveResetHandle(ctx, handle, linkToken, time.Until(expiresAt)); err != nil {
		return nil, err
	}
	return &ResetPasswordSession{Session: handle, ExpiresAt: expiresAt}, nil
}

// isResetLinkToken tells the emailed link token (a JWT) from a reset session handle.
func isResetLinkToken(session string) bool {
	return strings.Count(session, ".") == 2
}

func (s *authService) ResetPasswordConfirm(ctx context.Context, data *ResetPasswordConfirm, session, ipaddr string) *errx.Error {
	// The caller's 400, not an incident. See LoginStart.
	if err := s.verifyHuman(ctx, appattest.ActionResetPasswordConfirm, data.Turnstile, data.AppAttest, ipaddr); err != nil {
		return err
	}

	// A reset session resolves to its link token; a client that predates the
	// exchange still sends the link token itself.
	linkToken, handle := session, ""
	if !isResetLinkToken(session) {
		handle = session
		var err *errx.Error
		if linkToken, err = s.getResetHandle(ctx, handle); err != nil {
			if err.Code == errx.Unauthorized {
				return ErrResetLinkInvalid
			}
			return err
		}
	}

	sess, err := s.verifyResetLink(ctx, linkToken)
	if err != nil {
		return err
	}

	if perr := crypt.PasswordError(data.Password); perr != nil {
		return perr
	}

	// A reset may follow a compromise, so every session ends first and a
	// failure refuses the reset while the link is still good for a retry.
	if s.tokenService != nil {
		if err := s.tokenService.RevokeOtherSessions(ctx, sess.UserID, uuid.Nil); err != nil {
			errs.CaptureException(err)
			return ErrSessionsNotEnded
		}
	}
	if data.RevokeAPICredentials {
		if err := s.revokeAPICredentials(ctx, sess.UserID); err != nil {
			return err
		}
	}

	if err := s.ForgetTrustedDevices(ctx, sess.UserID); err != nil {
		return err
	}

	if err := s.deletePasswordResetSession(ctx, sess.SessionID); err != nil {
		return err
	}
	if handle != "" {
		s.deleteResetHandle(ctx, handle)
	}

	// Proving control of the mailbox clears any lockout that wrong passwords
	// accumulated, so a person who was locked out is not still locked out after
	// resetting, and an attacker cannot keep the lock on by guessing.
	s.clearLoginFailures(ctx, normalizeEmail(sess.Email))

	passwordHash, hashErr := argon2.Hash(data.Password)
	if hashErr != nil {
		errs.CaptureException(hashErr)
		return errx.InternalError()
	}

	if err := s.authRepository.ResetPassword(ctx, sess.UserID, passwordHash); err != nil {
		return err
	}

	return nil
}

// revokeAPICredentials ends the user's app authorizations and the API keys
// they created; any failure refuses the password write that asked for it.
func (s *authService) revokeAPICredentials(ctx context.Context, userID uuid.UUID) *errx.Error {
	if len(s.credentialRevokers) == 0 {
		return ErrCredentialRevokeUnsupported
	}
	for _, r := range s.credentialRevokers {
		if err := r.RevokeUserCredentials(ctx, userID); err != nil {
			errs.CaptureException(err)
			return errx.InternalError()
		}
	}
	return nil
}

// resetLinkPredatesPassword reports whether a reset token was issued no later
// than the last password write. JWT iat is whole seconds and the write is
// stamped by Postgres at microseconds, so a token minted in the same second as
// the change is refused too: fail closed, the person asks for a new link.
func resetLinkPredatesPassword(issuedAt *jwt.NumericDate, changedAt *time.Time) bool {
	if changedAt == nil {
		return false
	}
	if issuedAt == nil {
		return true
	}
	return !issuedAt.Time.After(*changedAt)
}

// ErrPasswordChangedSignInAgain answers a change whose password is already
// stored when the device could not be given a new session. The client must
// not keep its old tokens and must sign in again with the new password.
var ErrPasswordChangedSignInAgain = errx.NewWithIdentifier(errx.Conflict, "password_changed_sign_in_again",
	"Your password was changed, but this device could not be signed back in. Sign in again with your new password.")

// ChangePassword updates a logged-in user's password. It verifies the current
// password first (so a hijacked but unattended session can't silently change
// it), rejects OAuth-only accounts, and enforces the password policy. Every
// session ends with the change; the caller gets a new pair for its device.
func (s *authService) ChangePassword(ctx context.Context, userID uuid.UUID, current *models.Session, ipaddr, userAgent string, data *ChangePassword) (*models.Token, *errx.Error) {
	hash, xerr := s.authRepository.GetPasswordHash(ctx, userID)
	if xerr != nil {
		return nil, xerr
	}
	if hash == "" {
		return nil, errx.New(errx.BadRequest, "this account signs in without a password")
	}

	// The current password spends the account's re-authentication budget,
	// charged before the comparison.
	if ok, xerr := s.ReserveReauthAttempt(ctx, userID); xerr != nil {
		return nil, xerr
	} else if !ok {
		return nil, errx.ErrAuthLimit
	}
	ok, verr := argon2.Verify(data.CurrentPassword, hash)
	if verr != nil {
		s.ReleaseReauthAttempt(ctx, userID)
		errs.CaptureException(verr)
		return nil, errx.InternalError()
	}
	if !ok {
		return nil, errx.ErrCredentials
	}
	s.ClearReauthFailures(ctx, userID)

	if perr := crypt.PasswordError(data.NewPassword); perr != nil {
		return nil, perr
	}
	if data.NewPassword == data.CurrentPassword {
		return nil, errx.New(errx.BadRequest, "the new password must be different")
	}

	newHash, hashErr := argon2.Hash(data.NewPassword)
	if hashErr != nil {
		errs.CaptureException(hashErr)
		return nil, errx.InternalError()
	}
	// Forgotten before the new password is stored, so a failure leaves the old
	// password in place rather than a changed one with devices still trusted.
	if err := s.ForgetTrustedDevices(ctx, userID); err != nil {
		return nil, err
	}
	// Every other session ends before the password is written, and a failure
	// leaves the old password in place, so no session outlives the change.
	if s.tokenService != nil {
		keep := uuid.Nil
		if current != nil {
			keep = current.ID
		}
		if err := s.tokenService.RevokeOtherSessions(ctx, userID, keep); err != nil {
			errs.CaptureException(err)
			return nil, ErrSessionsNotEnded
		}
	}
	if data.RevokeAPICredentials {
		if err := s.revokeAPICredentials(ctx, userID); err != nil {
			return nil, err
		}
	}
	if err := s.authRepository.ResetPassword(ctx, userID, newHash); err != nil {
		return nil, err
	}

	if s.tokenService == nil {
		return nil, nil
	}
	// The password is stored by now, so this failure is its own outcome, not
	// an ordinary error: the caller must drop its tokens and sign in again.
	tok, err := s.tokenService.ReissueSession(ctx, userID, current, ipaddr, userAgent)
	if err != nil {
		errs.CaptureException(err)
		return nil, ErrPasswordChangedSignInAgain
	}
	return tok, nil
}

// PasswordHashFor returns the stored argon2 hash for a user.
func (s *authService) PasswordHashFor(ctx context.Context, userID uuid.UUID) (string, *errx.Error) {
	return s.authRepository.GetPasswordHash(ctx, userID)
}
