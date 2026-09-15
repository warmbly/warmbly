package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify/templates"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

func (s *authService) RegistrationStart(ctx context.Context, data *AuthData, origin SignupOrigin) (*models.AuthSession, *errx.Error) {
	ipaddr := origin.IP
	if s.policy.DisablePasswordLogin {
		return nil, errx.New(errx.Forbidden, "password sign-up is disabled on this deployment")
	}

	if err := s.signupAllowed(ctx, data.Email, data.Invite); err != nil {
		return nil, err
	}

	// The caller's 400, not an incident. See LoginStart.
	if xerr := s.captcha.Verify(ctx, data.Turnstile, ipaddr); xerr != nil {
		return nil, xerr
	}

	if !crypt.ValidatePassword(data.Password) {
		return nil, errx.ErrPassword
	}

	passwordHash, xerr := argon2.Hash(data.Password)
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	// With verification off, or with a transport that cannot deliver, there is
	// nothing to confirm: create the account now rather than issuing a code
	// nobody can receive. Every product surveyed defaults self-host to this.
	if !s.policy.RequireEmailVerification || !s.mailDelivers {
		u, err := s.createAccount(ctx, data.Email, passwordHash, SignupAttribution{
			ReferralCode: data.ReferralCode,
			Invite:       data.Invite,
			Acquisition:  data.Acquisition,
		}, origin)
		if err != nil {
			return nil, err
		}
		return s.sessionForNewAccount(ctx, u, origin)
	}

	if err := s.canSendEmail(ctx, emailFlowRegistration, data.Email); err != nil {
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

	text, xerr := templates.GenerateRegistrationCodeHTML(code)
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	// Reported by the transport; see LoginStart.
	if xerr := s.sendAuthEmail(ctx, data.Email, "Your Verification Code", text); xerr != nil {
		return nil, errx.ErrMailUndeliverable
	}

	codeHash, xerr := argon2.Hash(code)
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	session := &models.RegistrationSession{
		CodeHash:     codeHash,
		PasswordHash: passwordHash,
		Nonce:        nonce,
		ReferralCode: data.ReferralCode,
		Invite:       data.Invite,
	}
	// Held across the emailed code so the org created at confirm still knows
	// which link brought the person here. Normalized now, so the session never
	// holds an unclamped value a caller supplied.
	if acq := data.Acquisition.Normalize(); !acq.Empty() {
		session.Acquisition = &acq
	}

	if err := s.saveRegistrationSession(ctx, sessionID, session, expiresAt); err != nil {
		return nil, err
	}

	sessionToken, xerr := s.tokenService.GenerateToken(uuid.Nil, sessionID, data.Email, nonce, issuedAt, expiresAt)
	if xerr != nil {
		errs.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	return &models.AuthSession{
		Session:      sessionToken,
		CodeRequired: true,
	}, nil
}

func (s *authService) RegistrationConfirm(ctx context.Context, data *ConfirmData, session string, origin SignupOrigin) (*models.AuthSession, *errx.Error) {
	token, err := s.tokenService.VerifyToken(session)
	if err != nil {
		return nil, err
	}
	if token.ExpiresAt.Before(time.Now()) {
		return nil, errx.ErrSession
	}
	sess, err := s.getRegistrationSession(ctx, token.SessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil || sess.Nonce != token.Nonce {
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
		_ = s.saveRegistrationSession(ctx, token.SessionID, sess, token.ExpiresAt.Time)
		return nil, errx.ErrCode
	}

	// Re-check the policy: a session minted while signups were open must not
	// outlive a lockdown applied before the code came back.
	if err := s.signupAllowed(ctx, token.Email, sess.Invite); err != nil {
		return nil, err
	}

	attr := SignupAttribution{ReferralCode: sess.ReferralCode, Invite: sess.Invite}
	if sess.Acquisition != nil {
		attr.Acquisition = *sess.Acquisition
	}
	u, cerr := s.createAccount(ctx, token.Email, sess.PasswordHash, attr, origin)
	if cerr != nil {
		return nil, cerr
	}
	return s.sessionForNewAccount(ctx, u, origin)
}

// sessionForNewAccount signs the fresh account in, so registering lands in
// the dashboard instead of on the sign-in form.
func (s *authService) sessionForNewAccount(ctx context.Context, u *models.User, origin SignupOrigin) (*models.AuthSession, *errx.Error) {
	if u == nil {
		return &models.AuthSession{CodeRequired: false}, nil
	}
	result, xerr := s.finishLoginAs(ctx, u.ID, origin.IP, origin.UserAgent, "password")
	if xerr != nil {
		// The account exists; a sign-in hiccup must not read as a failed signup.
		return &models.AuthSession{CodeRequired: false}, nil
	}
	return &models.AuthSession{CodeRequired: false, Token: result.Token, TwoFARequired: result.TwoFARequired, PendingToken: result.PendingToken, ExpiresIn: result.ExpiresIn}, nil
}
