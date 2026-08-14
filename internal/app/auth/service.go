package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/app/trial"
	"github.com/warmbly/warmbly/internal/app/user"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify"
	"github.com/warmbly/warmbly/internal/pkg/captcha"
	"github.com/warmbly/warmbly/internal/repository"
)

// TwoFAChallenger issues + checks the 2FA login challenge after the email-code
// step. Satisfied by *twofa.Service; injected post-construction (WireTwoFA) so
// the auth package needs no import of twofa (no cycle).
type TwoFAChallenger interface {
	IsEnabled(ctx context.Context, userID uuid.UUID) (bool, error)
	CreatePendingChallenge(ctx context.Context, userID uuid.UUID) (string, int, *errx.Error)
}

// ReferralAttributor links a brand-new org to the referrer behind its signup
// code. Satisfied by *referral.Service; injected post-construction (WireReferral)
// so the auth package needs no import of referral (no cycle).
type ReferralAttributor interface {
	AttributeSignup(ctx context.Context, code string, inviteeOrgID, inviteeUserID uuid.UUID) *errx.Error
}

type AuthService interface {
	LoginStart(ctx context.Context, data *AuthData, ipaddr, userAgent string) (*models.AuthSession, *errx.Error)
	LoginConfirm(ctx context.Context, data *ConfirmData, session, ipaddr, userAgent string) (*models.LoginResult, *errx.Error)
	// WireTwoFA attaches the 2FA challenger (post-construction; nil = 2FA off).
	WireTwoFA(t TwoFAChallenger)

	RegistrationStart(ctx context.Context, data *AuthData, ipaddr string) (*models.AuthSession, *errx.Error)
	RegistrationConfirm(ctx context.Context, data *ConfirmData, session, ipaddr string) *errx.Error
	// WireReferral attaches the referral attributor (post-construction; nil = no
	// referral attribution at signup).
	WireReferral(r ReferralAttributor)

	// Native-app social sign-in: the client authenticates with the provider on
	// device and exchanges the resulting ID token for a session. First sign-in
	// provisions the account (org + trial) like password registration.
	AppleIDTokenAuth(ctx context.Context, rawToken, firstName, lastName, ipaddr, userAgent string) (*models.LoginResult, *errx.Error)
	GoogleIDTokenAuth(ctx context.Context, rawToken, ipaddr, userAgent string) (*models.LoginResult, *errx.Error)
	// WireExternalIDTokens attaches the ID-token verifiers (post-construction;
	// a nil verifier disables that provider).
	WireExternalIDTokens(apple, google IDTokenVerifier)

	ResetPasswordStart(ctx context.Context, data *ResetPasswordStart, ipaddr string) *errx.Error
	ResetPasswordConfirm(ctx context.Context, data *ResetPasswordConfirm, session, ipaddr string) *errx.Error

	// ChangePassword updates a logged-in user's password after verifying the
	// current one.
	ChangePassword(ctx context.Context, userID, currentSessionID uuid.UUID, data *ChangePassword) *errx.Error

	// Policy is the resolved per-deployment auth behavior, exposed so the
	// public /auth/config endpoint can report it to the login screen.
	Policy() *config.AuthPolicy

	// RegistrationMode is Policy().Registration with the first-launch
	// exemption applied, so a brand new instance advertises open signups.
	RegistrationMode(ctx context.Context) string

	// WireDeployment attaches the deployment-level facts auth decisions depend
	// on: the resolved policy and whether the mail transport actually delivers.
	WireDeployment(policy *config.AuthPolicy, mailDelivers bool)

	// WireIdentities attaches the federated-identity store, so external
	// sign-in resolves accounts by (issuer, subject) instead of email.
	WireIdentities(r repository.IdentityRepository)

	// Generic OIDC. WireOIDC attaches the provider (nil = OIDC disabled).
	WireOIDC(p OIDCProvider)
	OIDCBegin(ctx context.Context) (*OIDCRedirect, *errx.Error)
	// OIDCCallback returns a single-use handoff code, not a session: the
	// provider redirects a browser here, so the response must be a redirect.
	OIDCCallback(ctx context.Context, code, state, ipaddr, userAgent string) (string, *errx.Error)
	OIDCExchange(ctx context.Context, code string) (*models.LoginResult, *errx.Error)
}

type authService struct {
	authRepository           repository.AuthRepository
	userRepository           repository.UserRepository
	tokenService             token.TokenService
	userService              user.UserService
	trialService             trial.TrialService
	organizationService      organization.OrganizationService
	emailNotificationService notify.EmailNotificationService
	cache                    *cache.Cache
	captcha                  *captcha.Turnstile
	externalAuth             *models.ExternalAuth
	appleIDTokens            IDTokenVerifier
	googleIDTokens           IDTokenVerifier
	twofa                    TwoFAChallenger
	referral                 ReferralAttributor
	// identities binds federated logins to (issuer, subject). Nil-safe: an
	// unwired repository falls back to the historic email-only matching, which
	// is only safe for Apple and Google.
	identities repository.IdentityRepository
	// oidc is the generic OpenID Connect provider, nil unless configured.
	oidc OIDCProvider

	// policy and mailDelivers govern whether an emailed code gates a login and
	// whether public signups are open. Defaults are set in NewService so a
	// caller that never wires them still behaves like the historic flow.
	policy       *config.AuthPolicy
	mailDelivers bool
}

func (s *authService) WireTwoFA(t TwoFAChallenger) { s.twofa = t }

func (s *authService) WireDeployment(policy *config.AuthPolicy, mailDelivers bool) {
	if policy != nil {
		s.policy = policy
	}
	s.mailDelivers = mailDelivers
}

func (s *authService) Policy() *config.AuthPolicy { return s.policy }

func (s *authService) WireIdentities(r repository.IdentityRepository) { s.identities = r }

func (s *authService) WireOIDC(p OIDCProvider) { s.oidc = p }

func (s *authService) WireReferral(r ReferralAttributor) { s.referral = r }

func NewService(
	authRepository repository.AuthRepository,
	cache *cache.Cache,
	captcha *captcha.Turnstile,
	tokenService token.TokenService,
	emailNotificationService notify.EmailNotificationService,
	externalAuthData *models.ExternalAuth,
	trialService trial.TrialService,
	organizationService organization.OrganizationService,
	userRepository repository.UserRepository,
	userService user.UserService,
) AuthService {
	return &authService{
		authRepository:           authRepository,
		tokenService:             tokenService,
		emailNotificationService: emailNotificationService,
		cache:                    cache,
		captcha:                  captcha,
		externalAuth:             externalAuthData,
		trialService:             trialService,
		organizationService:      organizationService,
		userRepository:           userRepository,
		userService:              userService,
		// Overwritten by WireDeployment during boot. The default assumes a
		// delivering transport so an unwired service keeps the historic
		// always-email-a-code behavior rather than silently relaxing it.
		policy:       config.LoadAuthPolicy(true),
		mailDelivers: true,
	}
}
