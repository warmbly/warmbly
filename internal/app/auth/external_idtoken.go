package auth

import (
	"context"
	"errors"
	"net/mail"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/pkg/idtoken"
	"github.com/warmbly/warmbly/internal/repository"
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
func (s *authService) AppleIDTokenAuth(ctx context.Context, rawToken, firstName, lastName, ipaddr, userAgent string) (*models.LoginResult, *errx.Error) {
	return s.externalIDTokenAuth(ctx, s.appleIDTokens, token.AuthProviderApple, rawToken, firstName, lastName, ipaddr, userAgent)
}

// GoogleIDTokenAuth signs a user in with a native Google Sign-In ID token.
func (s *authService) GoogleIDTokenAuth(ctx context.Context, rawToken, ipaddr, userAgent string) (*models.LoginResult, *errx.Error) {
	return s.externalIDTokenAuth(ctx, s.googleIDTokens, token.AuthProviderGoogle, rawToken, "", "", ipaddr, userAgent)
}

// externalIDTokenAuth is the shared native social sign-in flow: verify the
// token, find or create the account, and mint a session. Like passkeys, a
// provider-verified identity is already strong auth, so there is no email OTP
// or captcha step; first sign-in provisions the org and free trial exactly
// like password registration does.
func (s *authService) externalIDTokenAuth(ctx context.Context, verifier IDTokenVerifier, provider, rawToken, firstName, lastName, ipaddr, userAgent string) (*models.LoginResult, *errx.Error) {
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
	// A provider asserts whatever case it holds. Unfolded, the address lookup
	// below missed the local account and provisioned a second one alongside
	// it, because the unique index is on the raw column.
	email.Address = normalizeEmail(email.Address)

	res, rerr := s.resolveFederatedUser(ctx, provider, claims.Issuer, claims.Subject, email, firstName, lastName, models.OrgAcquisition{})
	if rerr != nil {
		return nil, rerr
	}

	// Ban check and the 2FA gate both live in finishLoginAs, so social
	// sign-in enforces exactly what password login does.
	return s.finishFederatedLogin(ctx, res, ipaddr, userAgent, provider)
}

// federatedResolution is what a verified external identity resolved to. With
// LinkRequired nothing was linked and no session may be issued yet.
type federatedResolution struct {
	UserID       uuid.UUID
	LinkRequired bool
	Identity     models.UserIdentity
}

// finishFederatedLogin: the password challenge while the account still has
// to claim the identity, the session otherwise. Both federated paths end here.
func (s *authService) finishFederatedLogin(ctx context.Context, res federatedResolution, ipaddr, userAgent, sessionProvider string) (*models.LoginResult, *errx.Error) {
	if res.LinkRequired {
		return s.createLinkChallenge(ctx, res.UserID, res.Identity)
	}
	return s.finishLoginAs(ctx, res.UserID, ipaddr, userAgent, sessionProvider)
}

// resolveFederatedUser maps a verified external identity to a local account.
//
// The lookup order is what keeps this safe. The (issuer, subject) pair is the
// only provider-controlled stable identifier, so it is checked first. Falling
// back to the email address is allowed exactly once, to link a pre-existing
// local account, and only when that account has no other identity from this
// issuer already: a second subject claiming an address that is already
// federated is an impersonation attempt, not a re-login. A password account
// is not linked on the address alone: the link waits for its password.
func (s *authService) resolveFederatedUser(ctx context.Context, provider, issuer, subject string, email *mail.Address, firstName, lastName string, acq models.OrgAcquisition) (federatedResolution, *errx.Error) {
	return s.resolveFederated(ctx, provider, issuer, subject, email, firstName, lastName, acq, true)
}

// resolveFederated is resolveFederatedUser; retry allows one re-resolution after losing a provisioning race.
func (s *authService) resolveFederated(ctx context.Context, provider, issuer, subject string, email *mail.Address, firstName, lastName string, acq models.OrgAcquisition, retry bool) (federatedResolution, *errx.Error) {
	identity := models.UserIdentity{
		Provider: provider,
		Issuer:   issuer,
		Subject:  subject,
		Email:    email.Address,
	}

	if s.identities != nil && issuer != "" && subject != "" {
		existing, ierr := s.identities.FindUserByIdentity(ctx, issuer, subject)
		if ierr != nil {
			errs.CaptureException(ierr)
			return federatedResolution{}, errx.InternalError()
		}
		if existing != uuid.Nil {
			_ = s.identities.TouchLogin(ctx, issuer, subject)
			return federatedResolution{UserID: existing, Identity: identity}, nil
		}
	}

	u, uerr := s.userRepository.GetUserByEmail(ctx, email.Address)
	if uerr != nil && !errors.Is(uerr, errx.ErrUser) {
		errs.CaptureException(uerr)
		return federatedResolution{}, errx.InternalError()
	}

	if u == nil {
		// Just-in-time provisioning is a signup, so it answers to
		// DISABLE_REGISTRATION like every other one. Without this an instance
		// set to `true` is still open to anyone the IdP will assert.
		if xerr := s.federatedSignupAllowed(ctx, email.Address); xerr != nil {
			return federatedResolution{}, xerr
		}
		var cerr error
		u, cerr = s.createExternalUser(ctx, email, firstName, lastName, acq)
		if errors.Is(cerr, repository.ErrUserEmailTaken) {
			// A concurrent sign-in created the account first; resolve against it like any existing one.
			if retry {
				return s.resolveFederated(ctx, provider, issuer, subject, email, firstName, lastName, acq, false)
			}
			return federatedResolution{}, errx.ErrAccountExists
		}
		if cerr != nil {
			errs.CaptureException(cerr)
			return federatedResolution{}, errx.InternalError()
		}
	} else {
		if xerr := s.refuseSecondIdentity(ctx, u.ID, issuer); xerr != nil {
			return federatedResolution{}, xerr
		}
		// Only an identity that can actually be linked is worth a password;
		// with nothing to attach the prompt would gate nothing and recur.
		if s.identities != nil && issuer != "" && subject != "" {
			required, xerr := s.linkRequiresPassword(ctx, u.ID)
			if xerr != nil {
				return federatedResolution{}, xerr
			}
			if required {
				return federatedResolution{UserID: u.ID, LinkRequired: true, Identity: identity}, nil
			}
		}
	}

	if xerr := s.linkIdentity(ctx, u.ID, identity); xerr != nil {
		return federatedResolution{}, xerr
	}

	return federatedResolution{UserID: u.ID, Identity: identity}, nil
}

// refuseSecondIdentity refuses the email fallback for an account that already
// holds a different subject from this issuer.
func (s *authService) refuseSecondIdentity(ctx context.Context, userID uuid.UUID, issuer string) *errx.Error {
	if s.identities == nil || issuer == "" {
		return nil
	}
	linked, herr := s.identities.HasIdentityForIssuer(ctx, userID, issuer)
	if herr != nil {
		errs.CaptureException(herr)
		return errx.InternalError()
	}
	if linked {
		return errx.New(errx.Forbidden, "this account is already linked to a different identity from that provider")
	}
	return nil
}

// linkRequiresPassword: a password account waits for its password; with
// password sign-in off, or no password set, there is nothing to ask for.
func (s *authService) linkRequiresPassword(ctx context.Context, userID uuid.UUID) (bool, *errx.Error) {
	if s.policy != nil && s.policy.DisablePasswordLogin {
		return false, nil
	}
	if s.authRepository == nil {
		return false, nil
	}
	hash, xerr := s.authRepository.GetPasswordHash(ctx, userID)
	if xerr != nil {
		errs.CaptureException(xerr)
		return false, errx.InternalError()
	}
	return hash != "", nil
}

// linkIdentity binds the identity to the account; one another account owns
// is refused and nobody is signed in on it.
func (s *authService) linkIdentity(ctx context.Context, userID uuid.UUID, identity models.UserIdentity) *errx.Error {
	if s.identities == nil || identity.Issuer == "" || identity.Subject == "" {
		return nil
	}
	if lerr := s.identities.Link(ctx, userID, identity); lerr != nil {
		if errors.Is(lerr, repository.ErrIdentityTaken) {
			return errx.New(errx.Forbidden, "that identity is already linked to another account")
		}
		errs.CaptureException(lerr)
		return errx.InternalError()
	}
	_ = s.identities.TouchLogin(ctx, identity.Issuer, identity.Subject)
	return nil
}

// createExternalUser provisions a first-time social sign-in: a passwordless
// user row (they can set a password later via reset), the provider-asserted
// name when available, and the same invitation, org and trial bootstrap as
// RegistrationConfirm.
func (s *authService) createExternalUser(ctx context.Context, email *mail.Address, firstName, lastName string, acq models.OrgAcquisition) (*models.User, error) {
	u, err := s.userRepository.CreateUser(ctx, email, "")
	if err != nil {
		return nil, err
	}

	// A provider-asserted name the rules refuse is dropped, never a failed sign-in.
	firstName = displayname.Clean(firstName, displayname.Person)
	lastName = displayname.Clean(lastName, displayname.Person)
	if firstName != "" {
		// Provider-asserted name beats CreateUser's email local-part default.
		if perr := s.userRepository.UpdateProfile(ctx, u.ID, firstName, lastName); perr == nil {
			u.FirstName, u.LastName = firstName, lastName
		}
	}

	if serr := s.userService.SaveUser(ctx, u); serr != nil {
		return nil, serr
	}

	// An invited account joins the inviting organization and stops there, the
	// same as the password path. Only an address nobody invited gets its own
	// workspace and trial.
	if s.acceptPendingInvitation(ctx, u.ID, u.Email) {
		return u, nil
	}

	var org *models.Organization
	if s.organizationService != nil {
		orgName := displayname.DefaultWorkspace(u.FirstName)
		var orgErr *errx.Error
		org, orgErr = s.organizationService.Create(ctx, u.ID, orgName, "")
		if orgErr != nil {
			errs.CaptureException(orgErr)
			// Don't fail the sign-in if org creation fails.
		}
	}
	s.attributeWorkspace(ctx, u, org, acq)

	if s.trialService != nil && org != nil {
		if terr := s.trialService.StartFreeTrialWithOrg(ctx, u.ID, org.ID); terr != nil {
			errs.CaptureException(terr)
			// Don't fail the sign-in if trial creation fails.
		}
	}

	return u, nil
}
