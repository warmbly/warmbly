package auth

import (
	"context"
	"net/mail"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify/templates"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

func (s *authService) RegistrationStart(ctx context.Context, data *AuthData, ipaddr string) (*models.AuthSession, *errx.Error) {
	if xerr := s.captcha.Verify(ctx, data.Turnstile, ipaddr); xerr != nil {
		sentry.CaptureException(xerr)
		return nil, xerr
	}

	if !crypt.ValidatePassword(data.Password) {
		return nil, errx.ErrPassword
	}

	if err := s.canSendEmail(ctx, data.Email); err != nil {
		return nil, err
	}

	passwordHash, xerr := argon2.Hash(data.Password)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	issuedAt := time.Now()
	expiresAt := issuedAt.Add(AuthSessionTTL)
	sessionID := uuid.New()
	nonce, xerr := crypt.Nonce()
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	code, xerr := crypt.VerificationCode()
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	text, xerr := templates.GenerateRegistrationCodeHTML(code)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	if xerr := s.sendAuthEmail(ctx, data.Email, "Your Verification Code", text); xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	codeHash, xerr := argon2.Hash(code)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	session := &models.RegistrationSession{
		CodeHash:     codeHash,
		PasswordHash: passwordHash,
		Nonce:        nonce,
		ReferralCode: data.ReferralCode,
	}

	if err := s.saveRegistrationSession(ctx, sessionID, session, expiresAt); err != nil {
		return nil, err
	}

	sessionToken, xerr := s.tokenService.GenerateToken(uuid.Nil, sessionID, data.Email, nonce, issuedAt, expiresAt)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	return &models.AuthSession{
		Session: sessionToken,
	}, nil
}

func (s *authService) RegistrationConfirm(ctx context.Context, data *ConfirmData, session, ipaddr string) *errx.Error {
	token, err := s.tokenService.VerifyToken(session)
	if err != nil {
		return err
	}
	if token.ExpiresAt.Before(time.Now()) {
		return errx.ErrSession
	}
	sess, err := s.getRegistrationSession(ctx, token.SessionID)
	if err != nil {
		return err
	}
	if sess == nil || sess.Nonce != token.Nonce {
		return errx.ErrSession
	}

	if sess.Tries >= AuthAttempts {
		return errx.ErrCodeLimit
	}

	v, xerr := argon2.Verify(data.Code, sess.CodeHash)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return errx.InternalError()
	}

	if !v {
		sess.Tries++
		_ = s.saveRegistrationSession(ctx, token.SessionID, sess, token.ExpiresAt.Time)
		return errx.ErrCode
	}

	email, xerr := mail.ParseAddress(token.Email)
	if xerr != nil {
		return errx.ErrEmail
	}

	u, xerr := s.userRepository.CreateUser(ctx, email, sess.PasswordHash)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return errx.InternalError()
	}

	if err := s.userService.SaveUser(ctx, u); err != nil {
		return err
	}

	// Auto-create organization for new user
	var org *models.Organization
	if s.organizationService != nil {
		orgName := u.FirstName + "'s Organization"
		if u.FirstName == "" {
			orgName = "My Organization"
		}
		var orgErr *errx.Error
		org, orgErr = s.organizationService.Create(ctx, u.ID, orgName)
		if orgErr != nil {
			sentry.CaptureException(orgErr)
			// Don't fail registration if org creation fails
		}
	}

	// Start 2-week free trial for new user (linked to organization)
	if s.trialService != nil && org != nil {
		if err := s.trialService.StartFreeTrialWithOrg(ctx, u.ID, org.ID); err != nil {
			sentry.CaptureException(err)
			// Don't fail registration if trial creation fails
		}
	}

	// Attribute the signup to a referrer if a referral code rode along.
	// Best-effort: a bad or self-referral code never fails registration.
	if s.referral != nil && org != nil && sess.ReferralCode != "" {
		if xerr := s.referral.AttributeSignup(ctx, sess.ReferralCode, org.ID, u.ID); xerr != nil {
			sentry.CaptureException(xerr)
		}
	}

	return nil
}
