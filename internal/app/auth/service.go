package auth

import (
	"context"
	"github.com/warmbly/warmbly/internal/app/orgrisk"
	"github.com/warmbly/warmbly/internal/pkg/geo"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/appattest"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/app/trial"
	"github.com/warmbly/warmbly/internal/app/user"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify"
	"github.com/warmbly/warmbly/internal/observability/analytics"
	"github.com/warmbly/warmbly/internal/pkg/captcha"
	"github.com/warmbly/warmbly/internal/repository"
)

// TwoFAChallenger issues + checks the 2FA login challenge after the email-code
// step. Satisfied by *twofa.Service; injected post-construction (WireTwoFA) so
// the auth package needs no import of twofa (no cycle).
type TwoFAChallenger interface {
	IsEnabled(ctx context.Context, userID uuid.UUID) (bool, error)
	CreatePendingChallenge(ctx context.Context, userID uuid.UUID, authProvider string, link *models.UserIdentity) (string, int, *errx.Error)
}

// ReferralAttributor links a brand-new org to the referrer behind its signup
// code. Satisfied by *referral.Service; injected post-construction (WireReferral)
// so the auth package needs no import of referral (no cycle).
type ReferralAttributor interface {
	AttributeSignup(ctx context.Context, code string, inviteeOrgID, inviteeUserID uuid.UUID) *errx.Error
}

// AcquisitionRecorder stores where a signup came from on the new workspace.
// Satisfied by repository.OrganizationRepository; injected post-construction
// (WireAcquisition) and nil-safe, so a deployment that never wires it simply
// records no attribution.
type AcquisitionRecorder interface {
	RecordOrganizationAcquisition(ctx context.Context, acq *models.OrgAcquisition) error
}

// ProductAnalytics counts a finished signup. Satisfied by
// *analytics.Client; injected post-construction (WireAnalytics) and nil-safe,
// so a deployment with no POSTHOG_KEY counts nothing and calls nothing.
//
// The signup is counted server-side because that is the only place it is
// exact: an ad blocker, a closed tab or a failed beacon all lose the browser's
// version of the event that matters most.
type ProductAnalytics interface {
	Capture(name string, req analytics.Request, properties map[string]any)
}

// InstanceSettings is the operator-editable half of the signup policy.
// Satisfied by instancesettings.Service; injected post-construction
// (WireInstanceSettings) so the auth package needs no import of it (no cycle).
type InstanceSettings interface {
	AllowInvitedSignup(ctx context.Context) bool
}

type AuthService interface {
	LoginStart(ctx context.Context, data *AuthData, ipaddr, userAgent string) (*models.AuthSession, *errx.Error)
	// PasswordHashFor returns the stored argon2 hash, or empty for an account
	// that has no password (passkey or SSO only). Used by the re-auth endpoint
	// to confirm the account holder without starting a new login.
	PasswordHashFor(ctx context.Context, userID uuid.UUID) (string, *errx.Error)
	// The per-account budget for signed-in proofs (re-auth, 2FA disable and
	// recovery-code regeneration). Reserve charges one attempt atomically and
	// reports whether it was within budget; Release refunds an attempt that
	// never checked a credential; Clear forgives the count on success.
	ReserveReauthAttempt(ctx context.Context, userID uuid.UUID) (bool, *errx.Error)
	ReleaseReauthAttempt(ctx context.Context, userID uuid.UUID)
	ClearReauthFailures(ctx context.Context, userID uuid.UUID)
	LoginConfirm(ctx context.Context, data *ConfirmData, session, ipaddr, userAgent string) (*models.LoginResult, *errx.Error)
	// CompleteInitialPassword takes the password_change_required challenge and
	// the holder's own password, then continues the sign-in like LoginStart.
	CompleteInitialPassword(ctx context.Context, data *InitialPasswordData, ipaddr, userAgent string) (*models.AuthSession, *errx.Error)
	// WireTwoFA attaches the 2FA challenger (post-construction; nil = 2FA off).
	WireTwoFA(t TwoFAChallenger)
	// WireAppAttest accepts native app proofs on the captcha-gated paths (nil = off).
	WireAppAttest(a *appattest.Service)

	RegistrationStart(ctx context.Context, data *AuthData, origin SignupOrigin) (*models.AuthSession, *errx.Error)
	RegistrationConfirm(ctx context.Context, data *ConfirmData, session string, origin SignupOrigin) (*models.AuthSession, *errx.Error)
	// WireReferral attaches the referral attributor (post-construction; nil = no
	// referral attribution at signup).
	WireReferral(r ReferralAttributor)
	WireAcquisition(r AcquisitionRecorder)
	WireAnalytics(a ProductAnalytics, host string)

	// WireOperatorNotifier attaches the instance-wide operator alert channel
	// (post-construction; nil = no alerts).
	WireOperatorNotifier(n OperatorNotifier)

	// WireInstanceSettings attaches the database-backed instance settings
	// (post-construction; nil keeps the permissive defaults).
	WireInstanceSettings(s InstanceSettings)

	// Native-app social sign-in: the client authenticates with the provider on
	// device and exchanges the resulting ID token for a session. First sign-in
	// provisions the account (org + trial) like password registration.
	AppleIDTokenAuth(ctx context.Context, rawToken, firstName, lastName, ipaddr, userAgent string) (*models.LoginResult, *errx.Error)
	GoogleIDTokenAuth(ctx context.Context, rawToken, ipaddr, userAgent string) (*models.LoginResult, *errx.Error)
	// WireExternalIDTokens attaches the ID-token verifiers (post-construction;
	// a nil verifier disables that provider).
	WireExternalIDTokens(apple, google IDTokenVerifier)

	ResetPasswordStart(ctx context.Context, data *ResetPasswordStart, ipaddr string) *errx.Error
	// ResetPasswordSession opens a reset session for an emailed link token,
	// which the confirm step takes in place of the link token itself.
	ResetPasswordSession(ctx context.Context, linkToken string) (*ResetPasswordSession, *errx.Error)
	ResetPasswordConfirm(ctx context.Context, data *ResetPasswordConfirm, session, ipaddr string) *errx.Error
	// LinkedIdentities lists the providers the user can sign in with.
	LinkedIdentities(ctx context.Context, userID uuid.UUID) ([]LinkedIdentity, *errx.Error)
	// UnlinkIdentity removes a provider from the user's sign-in methods and
	// ends the other sessions it signed in, refusing when it is the last one.
	UnlinkIdentity(ctx context.Context, userID, currentSessionID uuid.UUID, provider string) *errx.Error
	// SendPasswordLink emails the signed-in user a set-password link on the
	// same budget and confirm step as Forgot password.
	SendPasswordLink(ctx context.Context, userID uuid.UUID) *errx.Error
	// WireCredentialRevokers attaches what ends a user's API credentials when
	// a password change or reset asks for it.
	WireCredentialRevokers(r ...UserCredentialRevoker)

	// ChangePassword updates a logged-in user's password after verifying the
	// current one. Every session ends with it and the caller gets the token
	// pair of a new one for its device; current is the caller's session, whose
	// workspace, sign-in method and MFA status the new one keeps.
	ChangePassword(ctx context.Context, userID uuid.UUID, current *models.Session, ipaddr, userAgent string, data *ChangePassword) (*models.Token, *errx.Error)
	// ForgetTrustedDevices drops every browser the user asked to remember, so
	// each asks for an emailed login code again.
	ForgetTrustedDevices(ctx context.Context, userID uuid.UUID) *errx.Error
	// ForgetTrustedDevice drops the remembered devices whose tokens a signing-out browser presents.
	ForgetTrustedDevice(ctx context.Context, userID uuid.UUID, tokens []string) *errx.Error

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

	// Browser sign-in: generic OIDC, Sign in with Google, Sign in with Apple.
	// WireFederatedProvider attaches one under its identity-provider key
	// ("oidc", "google", "apple"); a provider that is never wired stays
	// unavailable and its button is never advertised.
	WireFederatedProvider(name string, p FederatedProvider)
	FederatedProviders() []string
	FederatedProviderLabels() map[string]string
	SSOBegin(ctx context.Context, provider string) (*SSORedirect, *errx.Error)
	// SSOCallbackComplete returns a single-use handoff code, not a session: the
	// provider redirects a browser here, so the response must be a redirect.
	SSOCallbackComplete(ctx context.Context, in SSOCallback) (*SSOCallbackResult, *errx.Error)
	SSOExchange(ctx context.Context, code, binding string) (*models.LoginResult, *errx.Error)
	// SSOLinkConfirm completes a federated sign-in that came back with
	// link_required: the address already belongs to a password account, and
	// the identity is attached only once that password is presented.
	SSOLinkConfirm(ctx context.Context, data *SSOLinkData, ipaddr, userAgent string) (*models.LoginResult, *errx.Error)

	// Re-authentication of a signed-in session through an identity linked to
	// the account. Each succeeds only for (issuer, subject) already bound to
	// userID, and only for the session that began it; the caller stamps it.
	ReauthIdentities(ctx context.Context, userID uuid.UUID) (browser, native []string, err *errx.Error)
	ReauthSSOBegin(ctx context.Context, provider string, userID, sessionID uuid.UUID) (*SSORedirect, *errx.Error)
	ReauthSSOFinish(ctx context.Context, code, binding string, userID, sessionID uuid.UUID) *errx.Error
	ReauthNativeBegin(ctx context.Context, sessionID uuid.UUID) (string, *errx.Error)
	ReauthNativeVerify(ctx context.Context, provider, rawToken string, userID, sessionID uuid.UUID) *errx.Error
}

// OperatorNotifier is the instance-wide operator alert surface, injected
// post-construction so this package needs no import of it. Nil disables it.
type OperatorNotifier interface {
	NotifyOperator(key, title, summary string, fields map[string]string)
}

type authService struct {
	// orgRisk files signup findings onto the new workspace's posture.
	// Optional/nil-safe: without it signups are scored but not fused.
	orgRisk orgrisk.Service
	// loginHistory and geo back the sign-in anomaly check. Either being nil
	// leaves every sign-in unjudged, which is the safe direction.
	loginHistory repository.LoginHistoryRepository
	geo          *geo.Client

	authRepository           repository.AuthRepository
	userRepository           repository.UserRepository
	tokenService             token.TokenService
	userService              user.UserService
	trialService             trial.TrialService
	organizationService      organization.OrganizationService
	emailNotificationService notify.EmailNotificationService
	// opsNotify raises instance-wide operator alerts. Nil is the default.
	opsNotify OperatorNotifier
	cache     *cache.Cache
	captcha   *captcha.Turnstile
	// appAttest takes the native app's proof in place of Turnstile. Nil turns it off.
	appAttest      *appattest.Service
	appleIDTokens  IDTokenVerifier
	googleIDTokens IDTokenVerifier
	twofa          TwoFAChallenger
	referral       ReferralAttributor
	// acquisition records the signup's channel on the new org. Nil-safe.
	acquisition AcquisitionRecorder
	// productAnalytics counts finished signups. Nil-safe.
	productAnalytics ProductAnalytics
	// analyticsHost is the dashboard's public hostname, forwarded so PostHog's
	// cookieless hash resolves to the same root domain the browser reported.
	analyticsHost string
	// settings is the operator-editable settings document, wired after
	// construction because it needs the database pool.
	settings InstanceSettings
	// identities binds federated logins to (issuer, subject). Nil-safe: an
	// unwired repository falls back to the historic email-only matching, which
	// is only safe for Apple and Google.
	identities repository.IdentityRepository
	// providers are the configured browser sign-in flows, keyed by identity
	// provider ("oidc", "google", "apple"). Empty unless one is configured.
	providers map[string]FederatedProvider
	// credentialRevokers end a user's app grants and API keys on request.
	credentialRevokers []UserCredentialRevoker

	// policy and mailDelivers govern whether an emailed code gates a login and
	// whether public signups are open. Defaults are set in NewService so a
	// caller that never wires them still behaves like the historic flow.
	policy       *config.AuthPolicy
	mailDelivers bool
}

func (s *authService) WireTwoFA(t TwoFAChallenger) { s.twofa = t }

// UserCredentialRevoker ends one kind of API credential a user holds or created.
type UserCredentialRevoker interface {
	RevokeUserCredentials(ctx context.Context, userID uuid.UUID) error
}

func (s *authService) WireCredentialRevokers(r ...UserCredentialRevoker) {
	s.credentialRevokers = append(s.credentialRevokers, r...)
}

// WireAppAttest accepts native app proofs on the captcha-gated paths.
func (s *authService) WireAppAttest(a *appattest.Service) { s.appAttest = a }

// verifyHuman is the anti-automation gate on every unauthenticated auth write:
// Turnstile for a browser, an App Attest assertion for the native app.
func (s *authService) verifyHuman(ctx context.Context, action, turnstile string, proof *appattest.Proof, ipaddr string) *errx.Error {
	if proof != nil && s.captcha.Required() && s.appAttest.Enabled() {
		return s.appAttest.Verify(ctx, action, proof)
	}
	return s.captcha.Verify(ctx, turnstile, ipaddr)
}

func (s *authService) WireDeployment(policy *config.AuthPolicy, mailDelivers bool) {
	if policy != nil {
		s.policy = policy
	}
	s.mailDelivers = mailDelivers
}

func (s *authService) Policy() *config.AuthPolicy { return s.policy }

func (s *authService) WireIdentities(r repository.IdentityRepository) { s.identities = r }

func (s *authService) WireReferral(r ReferralAttributor) { s.referral = r }

// WireAcquisition attaches the store for signup attribution.
func (s *authService) WireAcquisition(r AcquisitionRecorder) { s.acquisition = r }

// WireAnalytics attaches the product-analytics sink. host is the dashboard's
// public hostname, which is one of the three inputs to the cookieless hash.
func (s *authService) WireAnalytics(a ProductAnalytics, host string) {
	s.productAnalytics = a
	s.analyticsHost = host
}

// WireOperatorNotifier attaches the operator alert channel.
func (s *authService) WireOperatorNotifier(n OperatorNotifier) { s.opsNotify = n }

// WireInstanceSettings attaches the instance settings document, so the signup
// knobs on the admin page reach the registration paths.
func (s *authService) WireInstanceSettings(set InstanceSettings) { s.settings = set }

func NewService(
	authRepository repository.AuthRepository,
	cache *cache.Cache,
	captcha *captcha.Turnstile,
	tokenService token.TokenService,
	emailNotificationService notify.EmailNotificationService,
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

// WireOrgRisk attaches the organization risk posture. Kept off the constructor
// so auth stays constructible where risk is not wired.
func (s *authService) WireOrgRisk(r orgrisk.Service) { s.orgRisk = r }

// OrgRiskAware is the optional capability the caller uses to attach org risk.
type OrgRiskAware interface {
	WireOrgRisk(r orgrisk.Service)
}

// WireLoginRisk attaches the sign-in anomaly check. Kept off the constructor
// so auth stays constructible where geo or the history table is absent.
func (s *authService) WireLoginRisk(h repository.LoginHistoryRepository, g *geo.Client) {
	s.loginHistory, s.geo = h, g
}

// LoginRiskAware is the optional capability the caller uses to attach it.
type LoginRiskAware interface {
	WireLoginRisk(h repository.LoginHistoryRepository, g *geo.Client)
}
