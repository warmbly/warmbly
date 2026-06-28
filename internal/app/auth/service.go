package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/app/trial"
	"github.com/warmbly/warmbly/internal/app/user"
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
	LoginStart(ctx context.Context, data *AuthData, ipaddr string) (*models.AuthSession, *errx.Error)
	LoginConfirm(ctx context.Context, data *ConfirmData, session, ipaddr, userAgent string) (*models.LoginResult, *errx.Error)
	// WireTwoFA attaches the 2FA challenger (post-construction; nil = 2FA off).
	WireTwoFA(t TwoFAChallenger)

	RegistrationStart(ctx context.Context, data *AuthData, ipaddr string) (*models.AuthSession, *errx.Error)
	RegistrationConfirm(ctx context.Context, data *ConfirmData, session, ipaddr string) *errx.Error
	// WireReferral attaches the referral attributor (post-construction; nil = no
	// referral attribution at signup).
	WireReferral(r ReferralAttributor)

	ResetPasswordStart(ctx context.Context, data *ResetPasswordStart, ipaddr string) *errx.Error
	ResetPasswordConfirm(ctx context.Context, data *ResetPasswordConfirm, session, ipaddr string) *errx.Error

	// ChangePassword updates a logged-in user's password after verifying the
	// current one.
	ChangePassword(ctx context.Context, userID, currentSessionID uuid.UUID, data *ChangePassword) *errx.Error
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
	twofa                    TwoFAChallenger
	referral                 ReferralAttributor
}

func (s *authService) WireTwoFA(t TwoFAChallenger) { s.twofa = t }

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
	}
}
