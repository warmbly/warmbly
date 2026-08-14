package auth

import (
	"context"
	"net/mail"

	"github.com/getsentry/sentry-go"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// createAccount provisions a user, their organization and their trial. Both
// registration paths share it: the emailed-code confirm, and the direct create
// used when email verification is off.
func (s *authService) createAccount(ctx context.Context, address, passwordHash, referralCode string) *errx.Error {
	email, perr := mail.ParseAddress(address)
	if perr != nil {
		return errx.ErrEmail
	}

	u, xerr := s.userRepository.CreateUser(ctx, email, passwordHash)
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
	if s.referral != nil && org != nil && referralCode != "" {
		if xerr := s.referral.AttributeSignup(ctx, referralCode, org.ID, u.ID); xerr != nil {
			sentry.CaptureException(xerr)
		}
	}

	return nil
}

// signupAllowed enforces DISABLE_REGISTRATION.
//
// The first-launch exemption is what makes invite_only safe as a default: an
// instance with no users yet always accepts the first signup, so there is no
// chicken-and-egg where the lockdown blocks the account that would issue the
// invitations. Plausible's MaybeDisableRegistration plug works the same way.
func (s *authService) signupAllowed(ctx context.Context) *errx.Error {
	if s.policy.PublicSignupsAllowed() {
		return nil
	}

	empty, err := s.userRepository.IsEmpty(ctx)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}
	if empty {
		return nil
	}

	return errx.ErrRegistrationClosed
}

// RegistrationMode reports the effective mode for the /auth/config endpoint,
// resolving the first-launch exemption so the login screen can show a signup
// form on a brand new instance.
func (s *authService) RegistrationMode(ctx context.Context) string {
	if s.policy.PublicSignupsAllowed() {
		return config.RegistrationOpen
	}
	if empty, err := s.userRepository.IsEmpty(ctx); err == nil && empty {
		return config.RegistrationOpen
	}
	return s.policy.Registration
}
