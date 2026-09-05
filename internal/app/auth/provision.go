package auth

import (
	"context"
	"net/mail"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/orgrisk"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/signuprisk"
)

// createAccount provisions a user, their organization and their trial. Both
// registration paths share it: the emailed-code confirm, and the direct create
// used when email verification is off.
//
// invite, when it resolves, puts the account straight into the inviting
// organization instead of a fresh empty one.
// SignupOrigin is where a signup came from. Captured so accounts opened by one
// actor can be correlated later; RegistrationStart used to take the address
// only for the CAPTCHA check and then drop it.
type SignupOrigin struct {
	IP        string
	UserAgent string
}

func (s *authService) createAccount(ctx context.Context, address, passwordHash, referralCode, invite string, origin SignupOrigin) (*models.User, *errx.Error) {
	email, perr := mail.ParseAddress(address)
	if perr != nil {
		return nil, errx.ErrEmail
	}

	// Whether the invitation is the only thing that permitted this signup.
	// When it is, a failed accept cannot fall through to a personal workspace:
	// that would turn an invite-gated signup into an unrelated account.
	inviteRequired := s.inviteIsLoadBearing(ctx, invite)

	u, xerr := s.userRepository.CreateUser(ctx, email, passwordHash)
	if xerr != nil {
		sentry.CaptureException(xerr)
		return nil, errx.InternalError()
	}

	if err := s.userService.SaveUser(ctx, u); err != nil {
		return nil, err
	}

	s.recordSignupOrigin(ctx, u.ID, u.Email, origin)

	// An invited account joins the inviting org and stops there: no second
	// workspace, no trial of its own. A token that died between start and
	// confirm only falls through to a personal org when open registration
	// would have accepted the signup anyway.
	if invite != "" && s.organizationService != nil {
		if _, err := s.organizationService.AcceptInvitation(ctx, invite, u.ID, u.Email); err == nil {
			// An invited account finished signing up just as much as a
			// self-serve one; it simply joined an existing workspace.
			s.notifyOperatorSignup(u, "")
			return u, nil
		}
		if inviteRequired {
			return nil, errx.ErrInvitationInvalid
		}
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

	// Fold the signup's own risk into the new workspace's posture. A signup
	// signal alone can only ever reach `watch`, which by definition changes
	// nothing a customer can feel; it takes several detectors agreeing to
	// restrict anything.
	if org != nil {
		s.recordSignupRisk(ctx, org.ID, u.Email, origin)
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

	workspace := ""
	if org != nil {
		workspace = org.Name
	}
	s.notifyOperatorSignup(u, workspace)

	return u, nil
}

// notifyOperatorSignup raises the operator alert for a finished signup. Both
// the invited and the self-serve path go through it so neither can be missed.
func (s *authService) notifyOperatorSignup(u *models.User, workspace string) {
	if s.opsNotify == nil || u == nil {
		return
	}
	s.opsNotify.NotifyOperator(
		"user.registered",
		"New signup: "+u.Email,
		"A new account finished signing up.",
		map[string]string{
			"Email":     u.Email,
			"Name":      strings.TrimSpace(u.FirstName + " " + u.LastName),
			"Workspace": workspace,
		},
	)
}

// signupAllowed enforces DISABLE_REGISTRATION.
//
// The first-launch exemption is what makes invite_only safe as a default: an
// instance with no users yet always accepts the first signup, so there is no
// chicken-and-egg where the lockdown blocks the account that would issue the
// invitations. Plausible's MaybeDisableRegistration plug works the same way.
//
// invite holds the invitation token the caller signed up with, if any. Without
// it invite_only would be indistinguishable from fully closed, because
// accepting an invitation requires an account and the invitation is what would
// create one.
func (s *authService) signupAllowed(ctx context.Context, address, invite string) *errx.Error {
	if s.policy.PublicSignupsAllowed() {
		return nil
	}

	// The exemption exists so invite_only cannot deadlock a fresh install with
	// nobody to issue the first invitation. An operator who asked for `true`
	// asked for closed, and the setup link plus warmblyctl already cover the
	// first account, so the exemption does not extend to them.
	if s.policy.Registration != config.RegistrationClosed {
		empty, err := s.userRepository.IsEmpty(ctx)
		if err != nil {
			sentry.CaptureException(err)
			return errx.InternalError()
		}
		if empty {
			return nil
		}
	}

	if s.policy.Registration == config.RegistrationInviteOnly {
		// The operator can require that an admin creates the account instead,
		// so the invitation stops being a self-service signup capability. With
		// it off nobody can self-register, which is what closed reports.
		if !s.invitedSignupAllowed(ctx) {
			return errx.ErrRegistrationClosed
		}
		if invite == "" {
			return errx.ErrRegistrationInviteOnly
		}
		if s.inviteAllows(ctx, invite, address) {
			return nil
		}
		return errx.ErrInvitationInvalid
	}

	return errx.ErrRegistrationClosed
}

// invitedSignupAllowed reports whether an invitee may create their own account.
// Defaults to true, so an unwired settings service keeps the invite flow working.
func (s *authService) invitedSignupAllowed(ctx context.Context) bool {
	if s.settings == nil {
		return true
	}
	return s.settings.AllowInvitedSignup(ctx)
}

// inviteIsLoadBearing reports whether the invitation is the only reason this
// signup was permitted. Open registration and the first-launch exemption both
// stand on their own, so a failed accept may fall through to a personal org.
func (s *authService) inviteIsLoadBearing(ctx context.Context, invite string) bool {
	if invite == "" || s.policy == nil || s.policy.Registration != config.RegistrationInviteOnly {
		return false
	}
	if empty, err := s.userRepository.IsEmpty(ctx); err == nil && empty {
		return false
	}
	return true
}

// inviteAllows reports whether a live invitation for this exact address backs
// the signup. The token is the capability, so this is not an enumeration
// oracle: a caller without one learns nothing.
func (s *authService) inviteAllows(ctx context.Context, invite, address string) bool {
	if s.organizationService == nil {
		return false
	}
	preview, err := s.organizationService.PreviewInvitation(ctx, invite)
	if err != nil || preview == nil || preview.Expired {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(preview.Email), strings.TrimSpace(address))
}

// federatedSignupAllowed gates just-in-time provisioning from an identity
// provider. The address is already verified by the IdP, so an invitation is
// matched by address rather than by token here.
//
// SSO_AUTO_PROVISION is the explicit opt-out for operators whose position is
// "my IdP is the gate": Grafana's per-provider allow_sign_up and Miniflux's
// OAUTH2_USER_CREATION are the same switch.
func (s *authService) federatedSignupAllowed(ctx context.Context, address string) *errx.Error {
	if s.policy.SSOAutoProvision || s.policy.PublicSignupsAllowed() {
		return nil
	}

	if s.policy.Registration != config.RegistrationClosed {
		empty, err := s.userRepository.IsEmpty(ctx)
		if err != nil {
			sentry.CaptureException(err)
			return errx.InternalError()
		}
		if empty {
			return nil
		}
	}

	if s.policy.Registration == config.RegistrationInviteOnly {
		// Same knob as the password path: an operator can require that an
		// admin creates the account rather than the invitee signing in first.
		if !s.invitedSignupAllowed(ctx) {
			return errx.ErrRegistrationClosed
		}
		if s.hasPendingInvitation(ctx, address) {
			return nil
		}
		return errx.ErrRegistrationInviteOnly
	}

	return errx.ErrRegistrationClosed
}

// hasPendingInvitation reports whether a live invitation is waiting for this
// address. Only ever called with an address the IdP asserted, so it answers
// nothing for an anonymous caller.
func (s *authService) hasPendingInvitation(ctx context.Context, address string) bool {
	if s.organizationService == nil {
		return false
	}
	invitations, err := s.organizationService.GetUserPendingInvitations(ctx, address)
	if err != nil {
		return false
	}
	for i := range invitations {
		if !invitations[i].IsExpired() {
			return true
		}
	}
	return false
}

// acceptPendingInvitation redeems the first live invitation waiting for this
// address, so a federated signup lands in the inviting organization instead of
// leaving the invitation pending. Reported, so the caller knows whether the
// personal-workspace fallback is still needed.
func (s *authService) acceptPendingInvitation(ctx context.Context, userID uuid.UUID, address string) bool {
	if s.organizationService == nil {
		return false
	}
	invitations, err := s.organizationService.GetUserPendingInvitations(ctx, address)
	if err != nil {
		return false
	}
	for i := range invitations {
		if invitations[i].IsExpired() {
			continue
		}
		if _, aerr := s.organizationService.AcceptInvitationByID(ctx, invitations[i].ID, userID, address); aerr == nil {
			return true
		}
	}
	return false
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
	// With invited self-signup turned off, invite_only accepts nobody, so
	// reporting it would render a signup form that always refuses.
	if s.policy.Registration == config.RegistrationInviteOnly && !s.invitedSignupAllowed(ctx) {
		return config.RegistrationClosed
	}
	return s.policy.Registration
}

// recordSignupRisk files the signup's finding as evidence on the new
// workspace. Best-effort and never a reason to refuse an account.
func (s *authService) recordSignupRisk(ctx context.Context, orgID uuid.UUID, email string, origin SignupOrigin) {
	if s.orgRisk == nil {
		return
	}
	// Two signals, not one score: a throwaway domain is evidence about the
	// account, a tagged address only describes how the signup looked.
	risk := signuprisk.Score(email, origin.IP)
	s.fileSignupFindings(ctx, orgID, "signup_disposable", orgrisk.ClassSubstantive, risk.Substantive())
	s.fileSignupFindings(ctx, orgID, "signup", orgrisk.ClassCircumstantial, risk.Circumstantial())
}

// fileSignupFindings records one class of a signup's findings as a single
// signal. Nothing is filed when that class found nothing.
func (s *authService) fileSignupFindings(ctx context.Context, orgID uuid.UUID, key string,
	class orgrisk.SignalClass, findings []signuprisk.Finding) {
	weight, reasons := 0, make([]string, 0, len(findings))
	for _, f := range findings {
		weight += f.Weight
		reasons = append(reasons, f.Reason)
	}
	if weight == 0 {
		return
	}
	if _, err := s.orgRisk.RecordSignal(ctx, orgID, orgrisk.Signal{
		Key:    key,
		Weight: weight,
		Detail: strings.Join(reasons, "; "),
		Class:  class,
		TTL:    orgrisk.DefaultSignalTTL,
	}); err != nil {
		log.Warn().Str("organization_id", orgID.String()).Str("signal", key).
			Msg("could not record the signup risk signal")
	}
}

// recordSignupOrigin persists where a signup came from and what its address
// scored. Best-effort: an account is never refused because the evidence could
// not be written, since that would turn a bookkeeping failure into a lost
// customer.
func (s *authService) recordSignupOrigin(ctx context.Context, userID uuid.UUID, email string, origin SignupOrigin) {
	if s.userRepository == nil {
		return
	}
	risk := signuprisk.Score(email, origin.IP)
	if err := s.userRepository.RecordSignupMetadata(ctx, userID,
		origin.IP, origin.UserAgent, risk.Score, signuprisk.Normalize(email)); err != nil {
		log.Warn().Err(err).Str("user_id", userID.String()).Msg("could not record signup origin")
	}
}
