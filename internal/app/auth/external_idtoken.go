package auth

import (
	"context"
	"errors"
	"net/mail"

	"github.com/getsentry/sentry-go"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/idtoken"
)

// IDTokenVerifier checks a provider-signed ID token (signature, issuer,
// audience, expiry) and returns the identity it asserts. Satisfied by
// *idtoken.Verifier; an interface so tests can stub it.
type IDTokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (*idtoken.Claims, error)
}

// WireExternalIDTokens attaches the native-app ID-token verifiers
// (post-construction; a nil verifier disables that provider).
func (s *authService) WireExternalIDTokens(apple, google IDTokenVerifier) {
	s.appleIDTokens = apple
	s.googleIDTokens = google
}

// AppleIDTokenAuth signs a user in with a native Sign in with Apple identity
// token. Apple only shares the user's name with the app (never in the token),
// so the client forwards it for first-sign-in profile prefill.
func (s *authService) AppleIDTokenAuth(ctx context.Context, rawToken, firstName, lastName, ipaddr, userAgent string) (*models.Token, *errx.Error) {
	return s.externalIDTokenAuth(ctx, s.appleIDTokens, token.AuthProviderApple, rawToken, firstName, lastName, ipaddr, userAgent)
}

// GoogleIDTokenAuth signs a user in with a native Google Sign-In ID token.
func (s *authService) GoogleIDTokenAuth(ctx context.Context, rawToken, ipaddr, userAgent string) (*models.Token, *errx.Error) {
	return s.externalIDTokenAuth(ctx, s.googleIDTokens, token.AuthProviderGoogle, rawToken, "", "", ipaddr, userAgent)
}

// externalIDTokenAuth is the shared native social sign-in flow: verify the
// token, find or create the account, and mint a session. Like passkeys, a
// provider-verified identity is already strong auth, so there is no email OTP
// or captcha step; first sign-in provisions the org and free trial exactly
// like password registration does.
func (s *authService) externalIDTokenAuth(ctx context.Context, verifier IDTokenVerifier, provider, rawToken, firstName, lastName, ipaddr, userAgent string) (*models.Token, *errx.Error) {
	if verifier == nil {
		return nil, errx.ErrExternalProvider
	}

	claims, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, errx.ErrExternalCode
	}
	if claims.Email == "" || !claims.EmailVerified {
		return nil, errx.ErrExternalEmail
	}
	if firstName == "" {
		firstName = claims.GivenName
	}
	if lastName == "" {
		lastName = claims.FamilyName
	}

	email, perr := mail.ParseAddress(claims.Email)
	if perr != nil {
		return nil, errx.ErrEmail
	}

	u, uerr := s.userRepository.GetUserByEmail(ctx, email.Address)
	if uerr != nil && !errors.Is(uerr, errx.ErrUser) {
		sentry.CaptureException(uerr)
		return nil, errx.InternalError()
	}

	if u == nil {
		u, uerr = s.createExternalUser(ctx, email, firstName, lastName)
		if uerr != nil {
			sentry.CaptureException(uerr)
			return nil, errx.InternalError()
		}
	}

	// Ban-scope enforcement — parity with password and passkey login.
	if scope, scopeErr := s.userRepository.GetBanState(ctx, u.ID); scopeErr == nil {
		if models.BanScope(scope).Has(models.BanScopeLogin) {
			return nil, errx.New(errx.Forbidden, "this account has been suspended")
		}
	}

	session, xerr := s.tokenService.GenerateSession(ctx, u.ID, "", ipaddr, userAgent, provider)
	if xerr != nil {
		return nil, xerr
	}
	return session, nil
}

// createExternalUser provisions a first-time social sign-in: a passwordless
// user row (they can set a password later via reset), the provider-asserted
// name when available, and the same org + trial bootstrap as RegistrationConfirm.
func (s *authService) createExternalUser(ctx context.Context, email *mail.Address, firstName, lastName string) (*models.User, error) {
	u, err := s.userRepository.CreateUser(ctx, email, "")
	if err != nil {
		return nil, err
	}

	if firstName != "" {
		// Provider-asserted name beats CreateUser's email local-part default.
		if perr := s.userRepository.UpdateProfile(ctx, u.ID, firstName, lastName); perr == nil {
			u.FirstName, u.LastName = firstName, lastName
		}
	}

	if serr := s.userService.SaveUser(ctx, u); serr != nil {
		return nil, serr
	}

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
			// Don't fail the sign-in if org creation fails.
		}
	}

	if s.trialService != nil && org != nil {
		if terr := s.trialService.StartFreeTrialWithOrg(ctx, u.ID, org.ID); terr != nil {
			sentry.CaptureException(terr)
			// Don't fail the sign-in if trial creation fails.
		}
	}

	return u, nil
}
