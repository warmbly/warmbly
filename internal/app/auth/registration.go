package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/appattest"
	"github.com/warmbly/warmbly/internal/app/token"
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

	// Folded before the duplicate check, or "Vincent@" and "vincent@" both get
	// past it and become two accounts for one person.
	data.Email = normalizeEmail(data.Email)

	if err := s.signupAllowed(ctx, data.Email, data.Invite); err != nil {
		return nil, err
	}

	// The caller's 400, not an incident. See LoginStart.
	if xerr := s.verifyHuman(ctx, appattest.ActionRegister, data.Turnstile, data.AppAttest, ipaddr); xerr != nil {
		return nil, xerr
	}

	if perr := crypt.PasswordError(data.Password); perr != nil {
		return nil, perr
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
			Invite:      data.Invite,
			Acquisition: data.Acquisition,
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

	sessionToken, xerr := s.tokenService.GenerateTokenFor(token.PurposeRegistration, uuid.Nil, sessionID, data.Email, nonce, issuedAt, expiresAt)
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
	claims, err := s.tokenService.VerifyTokenFor(token.PurposeRegistration, session)
	if err != nil {
		return nil, err
	}
	if claims.ExpiresAt.Before(time.Now()) {
		return nil, errx.ErrSession
	}
	sess, err := s.getRegistrationSession(ctx, claims.SessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil || sess.Nonce != claims.Nonce {
		return nil, errx.ErrSession
	}

	sessKey := getRegistrationSessionKey(claims.SessionID)
	if ok, xerr := s.reserveCodeAttempt(ctx, sessKey, time.Until(claims.ExpiresAt.Time)); xerr != nil {
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

	// Re-check the policy: a session minted while signups were open must not
	// outlive a lockdown applied before the code came back.
	if err := s.signupAllowed(ctx, claims.Email, sess.Invite); err != nil {
		return nil, err
	}

	// Single-use: only the request that consumes the session creates the account.
	if !s.consumeCodeSession(ctx, sessKey) {
		return nil, errx.ErrSession
	}

	attr := SignupAttribution{Invite: sess.Invite}
	if sess.Acquisition != nil {
		attr.Acquisition = *sess.Acquisition
	}
	u, cerr := s.createAccount(ctx, claims.Email, sess.PasswordHash, attr, origin)
	if cerr != nil {
		return nil, cerr
	}
	return s.sessionForNewAccount(ctx, u, origin)
}

const newAccountSignInTimeout = 10 * time.Second

// sessionForNewAccount signs the fresh account in, so registering lands in
// the dashboard instead of on the sign-in form.
func (s *authService) sessionForNewAccount(ctx context.Context, u *models.User, origin SignupOrigin) (*models.AuthSession, *errx.Error) {
	if u == nil {
		return &models.AuthSession{CodeRequired: false}, nil
	}
	// Detached from the request: provisioning the account can spend most of
	// its deadline, and a sign-in that times out here sends a person who just
	// signed up to the sign-in form.
	signInCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), newAccountSignInTimeout)
	defer cancel()
	result, xerr := s.finishLoginAs(signInCtx, u.ID, origin.IP, origin.UserAgent, token.AuthProviderEmail)
	if xerr != nil {
		// The account exists; a sign-in hiccup must not read as a failed signup.
		errs.CaptureException(xerr)
		return &models.AuthSession{CodeRequired: false}, nil
	}
	return &models.AuthSession{CodeRequired: false, Token: result.Token, TwoFARequired: result.TwoFARequired, PendingToken: result.PendingToken, ExpiresIn: result.ExpiresIn}, nil
}
