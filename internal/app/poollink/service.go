// Package poollink is the cloud side of the self-hosted warmup pool link.
package poollink

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/analytics"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

var (
	ErrCodeNotFound        = errx.NewWithIdentifier(errx.NotFound, "pool_link_code_not_found", "That code is unknown or has expired. Start the connection again from your instance.")
	ErrCodeNotPending      = errx.NewWithIdentifier(errx.Conflict, "pool_link_code_used", "That code has already been used.")
	ErrCodeDenied          = errx.NewWithIdentifier(errx.Forbidden, "pool_link_denied", "The connection was declined.")
	ErrInstanceNotFound    = errx.NewWithIdentifier(errx.NotFound, "pool_link_instance_not_found", "Linked instance not found.")
	ErrInstanceRevoked     = errx.NewWithIdentifier(errx.Unauthorized, "pool_link_revoked", "This instance's link has been revoked. Warmbly Cloud links no new instances, so it cannot be reconnected.")
	ErrLinkClosed          = errx.NewWithIdentifier(errx.Forbidden, "pool_link_closed", "Warmbly Cloud no longer links new self-hosted instances. Existing connections keep working.")
	ErrMailboxSubscription = errx.NewWithIdentifier(errx.PaymentRequired, "pool_link_subscription_required", "Adding mailboxes to Warmbly Cloud from a self-hosted instance needs a paid Warmbly Cloud plan. Choose a plan, or keep warming this mailbox on your instance. Mailboxes already enrolled keep warming.")
	ErrMailboxNotFound     = errx.NewWithIdentifier(errx.NotFound, "pool_link_mailbox_not_found", "That mailbox is not enrolled.")
	ErrMailboxConflict     = errx.NewWithIdentifier(errx.Conflict, "pool_link_mailbox_conflict", "This address already exists in the linked Cloud workspace. An instance administrator can connect the existing mailbox from Add account > In your Warmbly Cloud workspace when it is available there. Otherwise, contact Warmbly support to check the association. Do not delete or re-enroll the Cloud mailbox.")
	ErrWarmupNotEntitled   = errx.NewWithIdentifier(errx.Forbidden, "pool_link_warmup_unavailable", "Warmup is not available on this workspace.")
	ErrBadCredential       = errx.NewWithIdentifier(errx.BadRequest, "pool_link_credential", "A credential matching the provider is required.")
	ErrBadRequest          = errx.NewWithIdentifier(errx.BadRequest, "pool_link_request", "Instance name is required.")
	ErrCleartextMailbox    = errx.NewWithIdentifier(errx.Unprocessable, "pool_link_cleartext_mailbox", "Warmbly Cloud reaches your mail server over the internet, so it needs TLS or STARTTLS on both SMTP and IMAP. This mailbox uses security \"none\", which only works for a mail server on your instance's own machine. Switch it to TLS, or keep warming it on your instance.")
	ErrExecutorInvalid     = errx.NewWithIdentifier(errx.BadRequest, "pool_link_executor", "Warmbly Cloud runs the warmup of every mailbox it holds, so executor must be cloud.")
	ErrExecutorConflict    = errx.NewWithIdentifier(errx.Conflict, "pool_link_executor_conflict", "This mailbox's credential is still on your instance. Enroll it again with its credential to hand it to Warmbly Cloud.")
)

// CredentialRestorer writes the credential of a mailbox handed back to Cloud.
type CredentialRestorer interface {
	RestoreMailboxCredential(ctx context.Context, id uuid.UUID, oauth *models.PoolLinkOAuthCredential, creds *models.SmtpImap) error
}

// cloudReachable refuses an SMTP/IMAP credential this instance's workers cannot use, before anything is stored.
func cloudReachable(c *models.SmtpImap) *errx.Error {
	for _, svc := range []*models.Service{c.SMTP, c.IMAP} {
		if svc != nil && svc.Security == models.MailSecurityNone && !models.CleartextMailAllowed(models.NormalizeMailHost(svc.Host)) {
			return ErrCleartextMailbox
		}
	}
	return nil
}

// WarmupScheduler seeds the first warmup task after enrollment.
type WarmupScheduler interface {
	EnsureWarmupScheduled(ctx context.Context, accountID uuid.UUID) error
}

type Service interface {
	// StartCode opens a handshake for an instance that has no token yet.
	StartCode(ctx context.Context, req models.PoolLinkStartRequest) (*models.PoolLinkStartResponse, *errx.Error)
	// PollCode is what the instance calls until the code is approved.
	PollCode(ctx context.Context, deviceCode string) (*models.PoolLinkPollResponse, *errx.Error)
	// DescribeCode is what the approving member sees before deciding.
	DescribeCode(ctx context.Context, userCode string) (*models.PoolLinkCode, *errx.Error)
	ApproveCode(ctx context.Context, userCode string, orgID, userID uuid.UUID) (*models.PoolLinkInstance, *errx.Error)
	DenyCode(ctx context.Context, userCode string) *errx.Error

	// AuthenticateInstance resolves a bearer token to a live instance.
	AuthenticateInstance(ctx context.Context, token, version string) (*models.PoolLinkInstance, *errx.Error)
	InstanceInfo(ctx context.Context, inst *models.PoolLinkInstance) (*models.PoolLinkInstanceInfo, *errx.Error)
	ListInstances(ctx context.Context, orgID uuid.UUID) ([]models.PoolLinkInstance, *errx.Error)
	// RevokeInstance stops and releases every mailbox the link enrolled, then ends the link.
	RevokeInstance(ctx context.Context, orgID, instanceID uuid.UUID) *errx.Error

	Enroll(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkEnrollRequest) (*models.PoolLinkMailboxState, *errx.Error)
	AssociateSMTPIMAP(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkSMTPAssociationRequest) (*models.PoolLinkMailboxState, *errx.Error)
	ListMailboxes(ctx context.Context, inst *models.PoolLinkInstance) ([]models.PoolLinkMailboxState, *errx.Error)
	WarmupStats(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkWarmupReportRequest) ([]models.WarmupDailyStats, *errx.Error)
	WarmupPlacementData(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkWarmupReportRequest) (*models.WarmupPlacementData, *errx.Error)
	// ListStanding is the warmup standing of every enrolled mailbox, which the
	// instance polls so its own send gates hold the cloud's verdict.
	ListStanding(ctx context.Context, inst *models.PoolLinkInstance) ([]models.PoolLinkMailboxStanding, *errx.Error)
	GetMailbox(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID) (*models.PoolLinkMailboxState, *errx.Error)
	PatchMailbox(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID, patch models.PoolLinkMailboxPatch) (*models.PoolLinkMailboxState, *errx.Error)
	Unenroll(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID) *errx.Error

	// Cloud-managed mailboxes for linked instances (oauth.go).
	StartOAuth(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkOAuthStartRequest) (*models.PoolLinkOAuthStartResponse, *errx.Error)
	// DescribeOAuthConsent and ContinueOAuth back the cloud page shown before the provider opens.
	DescribeOAuthConsent(ctx context.Context, state string) (*models.PoolLinkOAuthConsent, *errx.Error)
	ContinueOAuth(ctx context.Context, state, binding string) (string, *errx.Error)
	// CompleteOAuthCallback finishes a brokered consent and returns where to send the browser.
	CompleteOAuthCallback(ctx context.Context, provider, code, state, providerErr, binding string) (string, *errx.Error)
	FinishOAuth(ctx context.Context, inst *models.PoolLinkInstance, session string) (*models.PoolLinkMailboxState, *errx.Error)
	FinishManagedOAuth(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkOAuthFinishRequest) (*models.PoolLinkMailboxState, *errx.Error)
	AccessToken(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID) (*models.PoolLinkAccessToken, *errx.Error)
	ListWorkspaceMailboxes(ctx context.Context, inst *models.PoolLinkInstance, includeSMTPIMAP bool) ([]models.PoolLinkWorkspaceMailbox, *errx.Error)
	Adopt(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkAdoptRequest) (*models.PoolLinkMailboxState, *errx.Error)
	VerifyWarmupToken(ctx context.Context, inst *models.PoolLinkInstance, remoteID, token uuid.UUID) (bool, *errx.Error)
	// VerifyWarmupDelivery is the same answer for warmup mail that arrived without its verify header.
	VerifyWarmupDelivery(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID, q models.PoolLinkWarmupDeliveryQuery) (bool, *errx.Error)

	// HasActiveLink entitles a workspace to warm its linked mailboxes.
	HasActiveLink(ctx context.Context, orgID uuid.UUID) bool
	// Plan is the allowance the workspace currently has.
	Plan(ctx context.Context, orgID uuid.UUID) (models.PoolLinkPlan, *errx.Error)
}

type service struct {
	repo       repository.PoolLinkRepository
	emails     repository.EmailRepository
	emailSvc   email.EmailService
	analytics  analytics.AnalyticsService
	warmup     repository.WarmupRepository
	gate       feature.FeatureGateService
	orgs       organization.OrganizationService
	scheduler  WarmupScheduler
	cache      *cache.Cache
	campaigns  CampaignUsage
	delegation DelegatedTokenBroker
	restorer   CredentialRestorer
}

// DelegatedTokenBroker mints tokens without moving the administrator's grant to an instance.
type DelegatedTokenBroker interface {
	AccessToken(context.Context, uuid.UUID) (*models.PoolLinkAccessToken, bool, *errx.Error)
}

func (s *service) WireDelegation(b DelegatedTokenBroker) { s.delegation = b }

// CampaignUsage says whether Cloud campaigns send from a mailbox.
type CampaignUsage interface {
	AccountHasActiveCampaign(ctx context.Context, accountID uuid.UUID) (bool, error)
}

func NewService(
	repo repository.PoolLinkRepository,
	emails repository.EmailRepository,
	emailSvc email.EmailService,
	analyticsSvc analytics.AnalyticsService,
	warmup repository.WarmupRepository,
	gate feature.FeatureGateService,
	orgs organization.OrganizationService,
) Service {
	s := &service{repo: repo, emails: emails, emailSvc: emailSvc, analytics: analyticsSvc, warmup: warmup, gate: gate, orgs: orgs}
	s.restorer, _ = emails.(CredentialRestorer)
	return s
}

// WireScheduler attaches the warmup task seeder after construction.
func (s *service) WireScheduler(w WarmupScheduler) { s.scheduler = w }

// WireCampaigns lets enrollment adopt only mailboxes no active campaign sends from.
func (s *service) WireCampaigns(c CampaignUsage) { s.campaigns = c }

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// NormalizeUserCode accepts any case, with or without the dash.
func NormalizeUserCode(raw string) string {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, "-", "")
	raw = strings.ReplaceAll(raw, " ", "")
	if len(raw) != 8 {
		return raw
	}
	return raw[:4] + "-" + raw[4:]
}

// StartCode refuses: Warmbly Cloud links no new self-hosted instances, on any plan.
func (s *service) StartCode(context.Context, models.PoolLinkStartRequest) (*models.PoolLinkStartResponse, *errx.Error) {
	return nil, ErrLinkClosed
}

func (s *service) PollCode(ctx context.Context, deviceCode string) (*models.PoolLinkPollResponse, *errx.Error) {
	code, token, err := s.repo.ClaimCode(ctx, hashToken(strings.TrimSpace(deviceCode)))
	if err != nil {
		return nil, errx.InternalError()
	}
	if code == nil {
		return nil, ErrCodeNotFound
	}
	switch code.Status {
	case models.PoolLinkCodeDenied:
		return nil, ErrCodeDenied
	case models.PoolLinkCodeClaimed:
		return nil, ErrCodeNotPending
	case models.PoolLinkCodeApproved:
		if code.InstanceID == nil || code.OrganizationID == nil {
			return nil, errx.InternalError()
		}
		org, xerr := s.orgs.Get(ctx, *code.OrganizationID)
		if xerr != nil {
			return nil, xerr
		}
		return &models.PoolLinkPollResponse{
			Status:        models.PoolLinkCodeApproved,
			InstanceID:    code.InstanceID,
			InstanceToken: token,
			Organization:  &models.PoolLinkOrgInfo{ID: org.ID, Name: org.Name},
		}, nil
	default:
		return &models.PoolLinkPollResponse{Status: models.PoolLinkCodePending}, nil
	}
}

func (s *service) DescribeCode(ctx context.Context, userCode string) (*models.PoolLinkCode, *errx.Error) {
	code, err := s.repo.GetCodeByUserCode(ctx, NormalizeUserCode(userCode))
	if err != nil {
		return nil, errx.InternalError()
	}
	if code == nil {
		return nil, ErrCodeNotFound
	}
	return code, nil
}

// ApproveCode refuses every code, including one issued before linking closed.
func (s *service) ApproveCode(context.Context, string, uuid.UUID, uuid.UUID) (*models.PoolLinkInstance, *errx.Error) {
	return nil, ErrLinkClosed
}

func (s *service) DenyCode(ctx context.Context, userCode string) *errx.Error {
	ok, err := s.repo.DenyCode(ctx, NormalizeUserCode(userCode))
	if err != nil {
		return errx.InternalError()
	}
	if !ok {
		return ErrCodeNotPending
	}
	return nil
}

func (s *service) AuthenticateInstance(ctx context.Context, token, version string) (*models.PoolLinkInstance, *errx.Error) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, "wpl_") {
		return nil, errx.ErrUnauthorized
	}
	inst, err := s.repo.GetInstanceByTokenHash(ctx, hashToken(token))
	if err != nil {
		return nil, errx.InternalError()
	}
	if inst == nil {
		return nil, ErrInstanceRevoked
	}
	_ = s.repo.TouchInstance(ctx, inst.ID, version)
	return inst, nil
}

func (s *service) Plan(ctx context.Context, orgID uuid.UUID) (models.PoolLinkPlan, *errx.Error) {
	// The free allowance is per workspace: mailboxes connected directly and
	// through linked instances share it.
	enrolled, xerr := s.emails.CountForOrganization(ctx, orgID)
	if xerr != nil {
		return models.PoolLinkPlan{}, xerr
	}
	warming, xerr := s.emails.CountWarmingForOrganization(ctx, orgID)
	if xerr != nil {
		return models.PoolLinkPlan{}, xerr
	}
	plan := models.PoolLinkPlan{Tier: "free", Enrolled: enrolled, Warming: warming, PriceUSD: config.PoolLinkPlanPriceUSD, WarmupEntitled: true}
	if config.BillingProvider() != "none" {
		plan.ManageURL = config.AppBaseURL() + "/app/settings/billing"
	}
	paid, xerr := s.paidWorkspace(ctx, orgID)
	if xerr != nil {
		return plan, xerr
	}
	if paid {
		plan.Tier = "paid"
		return plan, nil
	}
	limit := models.FreeWorkspaceMailboxLimit
	plan.MailboxLimit = &limit
	plan.SubscriptionRequired = true
	if plan.ManageURL != "" {
		// ?pool=1 opens the plan chooser on arrival.
		plan.UpgradeURL = plan.ManageURL + "?pool=1"
	}
	return plan, nil
}

// paidWorkspace reports whether orgID pays for a plan; with billing off every workspace does.
func (s *service) paidWorkspace(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error) {
	if config.BillingProvider() == "none" {
		return true, nil
	}
	return s.gate.HasPremiumWarmup(ctx, orgID)
}

// admitNewMailbox refuses a mailbox the workspace's plan lets no linked instance add; enrolled ones are untouched.
func admitNewMailbox(plan models.PoolLinkPlan) *errx.Error {
	if plan.SubscriptionRequired {
		return ErrMailboxSubscription
	}
	return nil
}

// newMailboxAdmitted is admitNewMailbox without loading the plan: only a paid workspace adds, and it has no cap.
func (s *service) newMailboxAdmitted(ctx context.Context, orgID uuid.UUID) *errx.Error {
	paid, xerr := s.paidWorkspace(ctx, orgID)
	if xerr != nil {
		return xerr
	}
	if !paid {
		return ErrMailboxSubscription
	}
	return nil
}

func (s *service) InstanceInfo(ctx context.Context, inst *models.PoolLinkInstance) (*models.PoolLinkInstanceInfo, *errx.Error) {
	org, xerr := s.orgs.Get(ctx, inst.OrganizationID)
	if xerr != nil {
		return nil, xerr
	}
	plan, xerr := s.Plan(ctx, inst.OrganizationID)
	if xerr != nil {
		return nil, xerr
	}
	mailboxes, err := s.repo.ListMailboxes(ctx, inst.ID)
	if err != nil {
		return nil, errx.InternalError()
	}
	inst.MailboxCount = len(mailboxes)
	return &models.PoolLinkInstanceInfo{
		SMTPAssociationProtocol: models.SMTPAssociationProtocol,
		ManagedConsentProtocol:  models.ManagedConsentProtocol,
		Instance:                *inst,
		Organization:            models.PoolLinkOrgInfo{ID: org.ID, Name: org.Name},
		Plan:                    plan,
	}, nil
}

func (s *service) ListInstances(ctx context.Context, orgID uuid.UUID) ([]models.PoolLinkInstance, *errx.Error) {
	list, err := s.repo.ListInstances(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return list, nil
}

func (s *service) RevokeInstance(ctx context.Context, orgID, instanceID uuid.UUID) *errx.Error {
	if _, ok := s.repo.(repository.PoolLinkManagedRepository); ok && !managedOperationLocked(ctx) {
		return s.withManagedOperationLock(ctx, instanceID, func(ctx context.Context) *errx.Error { return s.RevokeInstance(ctx, orgID, instanceID) })
	}
	inst, err := s.repo.GetInstance(ctx, instanceID)
	if err != nil {
		return errx.InternalError()
	}
	if inst == nil || inst.OrganizationID != orgID {
		return ErrInstanceNotFound
	}
	if r, ok := s.repo.(repository.PoolLinkManagedRepository); ok {
		if err := r.RevokeManagedOperations(ctx, inst.ID, nil); err != nil {
			return errx.InternalError()
		}
	}
	mailboxes, err := s.repo.ListMailboxes(ctx, inst.ID)
	if err != nil {
		return errx.InternalError()
	}
	// Stopped while the link still authenticates, so a failure leaves the instance's disconnect retryable.
	for _, m := range mailboxes {
		if xerr := s.stopParticipation(ctx, inst, m); xerr != nil {
			return xerr
		}
	}
	if err := s.repo.RevokeInstance(ctx, inst.ID); err != nil {
		return errx.InternalError()
	}
	// Enrollment commits under this lock and refuses a revoked link, so every mailbox listed here is already stopped.
	mailboxes, err = s.repo.ListMailboxes(ctx, inst.ID)
	if err != nil {
		return errx.InternalError()
	}
	for _, m := range mailboxes {
		if xerr := s.unenroll(ctx, inst, m.RemoteID); xerr != nil {
			log.Warn().Str("account_id", m.EmailAccountID.String()).Msg("pool link: mailbox removal failed during revoke; continuing")
		}
	}
	return nil
}

// stopParticipation ends a linked mailbox's Cloud warmup in both directions and retires its unstarted sends.
func (s *service) stopParticipation(ctx context.Context, inst *models.PoolLinkInstance, m models.PoolLinkMailbox) *errx.Error {
	if _, xerr := s.emailSvc.SetWarmupLifecycle(ctx, inst.OrganizationID.String(), m.EmailAccountID.String(), "stop"); xerr != nil && xerr != errx.ErrNotFound {
		return xerr
	}
	return nil
}

// ownerUserID is the approving member, falling back to the workspace owner.
func (s *service) ownerUserID(ctx context.Context, inst *models.PoolLinkInstance) (string, *errx.Error) {
	if inst.CreatedBy != nil {
		return inst.CreatedBy.String(), nil
	}
	org, xerr := s.orgs.Get(ctx, inst.OrganizationID)
	if xerr != nil {
		return "", xerr
	}
	return org.OwnerUserID.String(), nil
}

// enrollExecutor refuses instance execution and hands a legacy instance-run mailbox to Cloud once its credential arrives.
func (s *service) enrollExecutor(ctx context.Context, inst *models.PoolLinkInstance, existing *models.PoolLinkMailbox, req models.PoolLinkEnrollRequest) (*models.PoolLinkMailboxState, *errx.Error, bool) {
	if req.Executor.OrCloud() != models.PoolLinkExecutorCloud {
		return nil, ErrExecutorInvalid, true
	}
	if existing == nil || existing.Executor.OrCloud() != models.PoolLinkExecutorInstance {
		return nil, nil, false
	}
	if req.OAuth == nil && req.SMTPIMAP == nil {
		// Without a credential the mailbox stays out of the pool; only its ramp is kept current.
		out, xerr := s.PatchMailbox(ctx, inst, req.RemoteID, models.PoolLinkMailboxPatch{Warmup: &req.Warmup})
		return out, xerr, true
	}
	out, xerr := s.convertToCloud(ctx, inst, existing, req)
	return out, xerr, true
}

// convertToCloud returns an instance-run mailbox to Cloud with the credential the instance now shares.
func (s *service) convertToCloud(ctx context.Context, inst *models.PoolLinkInstance, existing *models.PoolLinkMailbox, req models.PoolLinkEnrollRequest) (*models.PoolLinkMailboxState, *errx.Error) {
	acc, xerr := s.emails.GetByID(ctx, existing.EmailAccountID)
	if xerr != nil {
		return nil, xerr
	}
	// Only SMTP/IMAP returns this way; Cloud cannot refresh a grant the instance's own app issued.
	switch models.InboxProvider(acc.Provider) {
	case models.InboxProviderSMTPIMAP:
		if req.OAuth != nil || req.SMTPIMAP == nil || req.SMTPIMAP.SMTP == nil || req.SMTPIMAP.IMAP == nil {
			return nil, ErrBadCredential
		}
		if xerr := cloudReachable(req.SMTPIMAP); xerr != nil {
			return nil, xerr
		}
	default:
		return nil, ErrBadCredential
	}
	converter, ok := s.repo.(repository.PoolLinkConversionRepository)
	if !ok || s.restorer == nil {
		return nil, errx.InternalError()
	}
	if _, ok := s.repo.(repository.PoolLinkManagedRepository); ok && !managedOperationLocked(ctx) {
		var out *models.PoolLinkMailboxState
		xerr := s.withManagedOperationLock(ctx, inst.ID, func(ctx context.Context) *errx.Error {
			var xerr *errx.Error
			out, xerr = s.convertToCloud(ctx, inst, existing, req)
			return xerr
		})
		return out, xerr
	}
	if err := s.restorer.RestoreMailboxCredential(ctx, existing.EmailAccountID, req.OAuth, req.SMTPIMAP); err != nil {
		return nil, errx.InternalError()
	}
	if err := converter.ConvertToCloud(ctx, inst.ID, req.RemoteID); err != nil {
		return nil, errx.InternalError()
	}
	if userID, xerr := s.ownerUserID(ctx, inst); xerr == nil {
		s.applyWarmupSettings(ctx, inst.OrganizationID, userID, existing.EmailAccountID, req.Warmup)
	}
	if err := s.emailSvc.LoadAccountOntoWorker(ctx, existing.EmailAccountID); err != nil {
		log.Warn().Err(err).Str("account_id", existing.EmailAccountID.String()).Msg("pool link: worker load failed after return to Cloud; reconciler will retry")
	}
	if s.scheduler != nil {
		_ = s.scheduler.EnsureWarmupScheduled(ctx, existing.EmailAccountID)
	}
	return s.GetMailbox(ctx, inst, req.RemoteID)
}

func (s *service) Enroll(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkEnrollRequest) (*models.PoolLinkMailboxState, *errx.Error) {
	if req.RemoteID == uuid.Nil || strings.TrimSpace(req.Email) == "" {
		return nil, ErrBadRequest
	}
	existing, err := s.repo.GetMailboxByRemote(ctx, inst.ID, req.RemoteID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if out, xerr, routed := s.enrollExecutor(ctx, inst, existing, req); routed {
		return out, xerr
	}
	if existing != nil {
		// Re-enrolling refreshes the credential and ramp, nothing else.
		return s.PatchMailbox(ctx, inst, req.RemoteID, models.PoolLinkMailboxPatch{Warmup: &req.Warmup, OAuth: req.OAuth, SMTPIMAP: req.SMTPIMAP})
	}

	plan, xerr := s.Plan(ctx, inst.OrganizationID)
	if xerr != nil {
		return nil, xerr
	}
	if !plan.WarmupEntitled {
		return nil, ErrWarmupNotEntitled
	}
	if xerr := admitNewMailbox(plan); xerr != nil {
		return nil, xerr
	}

	userID, xerr := s.ownerUserID(ctx, inst)
	if xerr != nil {
		return nil, xerr
	}
	orgID := inst.OrganizationID
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = req.Email
	}

	var acc *models.Email
	switch req.Provider {
	case models.InboxProviderGoogle, models.InboxProviderOutlook:
		if req.OAuth == nil || req.OAuth.RefreshToken == "" {
			return nil, ErrBadCredential
		}
		acc, xerr = s.emails.NewOauthAccount(ctx, userID, models.NewOauthAccount{
			OrganizationID:         &orgID,
			DisableSendingBehavior: true,
			Provider:               req.Provider,
			Name:                   name,
			Email:                  strings.TrimSpace(req.Email),
			AccessToken:            req.OAuth.AccessToken,
			RefreshToken:           req.OAuth.RefreshToken,
			ExpiresAt:              req.OAuth.ExpiresAt,
		})
	case models.InboxProviderSMTPIMAP:
		if req.SMTPIMAP == nil || req.SMTPIMAP.SMTP == nil || req.SMTPIMAP.IMAP == nil {
			return nil, ErrBadCredential
		}
		if xerr := cloudReachable(req.SMTPIMAP); xerr != nil {
			return nil, xerr
		}
		acc, xerr = s.emails.NewSMTPIMAPAccount(ctx, userID, models.NewSMTPIMAPAccount{
			OrganizationID:         &orgID,
			DisableSendingBehavior: true,
			Name:                   name,
			Email:                  strings.TrimSpace(req.Email),
			SMTP:                   req.SMTPIMAP.SMTP,
			IMAP:                   req.SMTPIMAP.IMAP,
		})
	default:
		return nil, ErrBadCredential
	}
	if xerr != nil {
		if errors.Is(xerr, errx.ErrEmailOnboardAlreadyExists) {
			if req.Provider == models.InboxProviderSMTPIMAP {
				return s.adoptHostedSMTPIMAP(ctx, inst, req, userID)
			}
			return nil, ErrMailboxConflict
		}
		return nil, xerr
	}

	recorded, xerr := s.commitEnrollment(ctx, inst, userID, &models.PoolLinkMailbox{InstanceID: inst.ID, RemoteID: req.RemoteID, EmailAccountID: acc.ID}, req)
	if !recorded {
		_ = s.emailSvc.Delete(ctx, orgID.String(), acc.ID.String())
	}
	if xerr != nil {
		return nil, xerr
	}
	return s.GetMailbox(ctx, inst, req.RemoteID)
}

// enrollError reports an enrollment refused on a revoked link as revoked, and anything else as internal.
func enrollError(err error) *errx.Error {
	if errors.Is(err, repository.ErrPoolLinkInstanceRevoked) {
		return ErrInstanceRevoked
	}
	return errx.InternalError()
}

// commitEnrollment records and starts a mailbox under the instance lock RevokeInstance holds, so a revoke either stops it or refuses it.
func (s *service) commitEnrollment(ctx context.Context, inst *models.PoolLinkInstance, userID string, m *models.PoolLinkMailbox, req models.PoolLinkEnrollRequest) (bool, *errx.Error) {
	if _, ok := s.repo.(repository.PoolLinkManagedRepository); ok && !managedOperationLocked(ctx) {
		var recorded bool
		xerr := s.withManagedOperationLock(ctx, inst.ID, func(ctx context.Context) *errx.Error {
			var xerr *errx.Error
			recorded, xerr = s.commitEnrollment(ctx, inst, userID, m, req)
			return xerr
		})
		return recorded, xerr
	}
	if err := s.repo.EnrollMailbox(ctx, m); err != nil {
		return false, enrollError(err)
	}
	s.activateEnrolled(ctx, inst, userID, m.EmailAccountID, req)
	return true, nil
}

// adoptHostedSMTPIMAP enrolls the workspace's own copy of the address, and only
// when the instance presents exactly the credentials Cloud holds for it and no
// active Cloud campaign sends from it, so an address alone never attaches.
func (s *service) adoptHostedSMTPIMAP(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkEnrollRequest, userID string) (*models.PoolLinkMailboxState, *errx.Error) {
	ref, xerr := s.emails.FindInOrganization(ctx, inst.OrganizationID, req.Email)
	if xerr != nil {
		return nil, xerr
	}
	if ref == nil || ref.Provider != string(models.InboxProviderSMTPIMAP) || ref.Status != "active" ||
		ref.AuthMethod == models.MailAuthDelegated || ref.Managed {
		return nil, ErrMailboxConflict
	}
	if existing, err := s.repo.GetMailboxByAccount(ctx, ref.ID); err != nil {
		return nil, errx.InternalError()
	} else if existing != nil {
		return nil, ErrMailboxConflict
	}
	stored, xerr := s.emails.GetSMTPCredentials(ctx, ref.ID)
	if xerr == errx.ErrNotFound {
		return nil, ErrMailboxConflict
	}
	if xerr != nil {
		return nil, xerr
	}
	if !sameSMTPIMAPCredentials(stored, req.SMTPIMAP) {
		return nil, ErrMailboxConflict
	}
	// An unenrolled mirror is deleted, so a mailbox Cloud campaigns send from never becomes one.
	if s.campaigns == nil {
		return nil, ErrMailboxConflict
	}
	if busy, err := s.campaigns.AccountHasActiveCampaign(ctx, ref.ID); err != nil {
		return nil, errx.InternalError()
	} else if busy {
		return nil, ErrMailboxConflict
	}
	// An existing Cloud mailbox keeps its ramp settings when the instance associates it.
	req.Warmup = models.PoolLinkWarmupSettings{}
	if _, xerr := s.commitEnrollment(ctx, inst, userID, &models.PoolLinkMailbox{InstanceID: inst.ID, RemoteID: req.RemoteID, EmailAccountID: ref.ID}, req); xerr != nil {
		return nil, xerr
	}
	log.Info().Str("instance_id", inst.ID.String()).Str("account_id", ref.ID.String()).Msg("pool link: adopted the workspace's existing mailbox on enrollment")
	return s.GetMailbox(ctx, inst, req.RemoteID)
}

func sameSMTPIMAPCredentials(stored *repository.SMTPCredentials, got *models.SmtpImap) bool {
	if stored == nil || got == nil || got.SMTP == nil || got.IMAP == nil {
		return false
	}
	same := func(host string, port int, user, password string, svc *models.Service) bool {
		return strings.EqualFold(strings.TrimSpace(host), strings.TrimSpace(svc.Host)) && port == svc.Port &&
			strings.TrimSpace(user) == strings.TrimSpace(svc.Username) &&
			subtle.ConstantTimeCompare([]byte(password), []byte(svc.Password)) == 1
	}
	return same(stored.SMTPHost, stored.SMTPPort, stored.SMTPUser, stored.SMTPPassword, got.SMTP) &&
		same(stored.IMAPHost, stored.IMAPPort, stored.IMAPUser, stored.IMAPPassword, got.IMAP)
}

func (s *service) activateEnrolled(ctx context.Context, inst *models.PoolLinkInstance, userID string, accountID uuid.UUID, req models.PoolLinkEnrollRequest) {
	orgID := inst.OrganizationID
	s.applyWarmupSettings(ctx, orgID, userID, accountID, req.Warmup)
	if _, xerr := s.emailSvc.SetWarmupLifecycle(ctx, orgID.String(), accountID.String(), "start"); xerr != nil {
		log.Warn().Str("account_id", accountID.String()).Str("code", xerr.Identifier).Str("error", xerr.Message).Msg("pool link: warmup start failed after enrollment")
	}
	if err := s.emailSvc.LoadAccountOntoWorker(ctx, accountID); err != nil {
		log.Warn().Err(err).Str("account_id", accountID.String()).Msg("pool link: worker load failed; reconciler will retry")
	}
	if s.scheduler != nil {
		_ = s.scheduler.EnsureWarmupScheduled(ctx, accountID)
	}
}

func (s *service) applyWarmupSettings(ctx context.Context, orgID uuid.UUID, userID string, accountID uuid.UUID, w models.PoolLinkWarmupSettings) {
	upd := &models.UpdateEmail{}
	set := false
	if w.Base > 0 {
		upd.WarmupBase, set = &w.Base, true
	}
	if w.Max > 0 {
		upd.WarmupMax, set = &w.Max, true
	}
	if w.Increase > 0 {
		upd.WarmupIncrease, set = &w.Increase, true
	}
	if w.ReplyRate > 0 {
		upd.WarmupReplyRate, set = &w.ReplyRate, true
	}
	// Instances send the column as read ("08:00:00.000000"); the write accepts "HH:MM" only.
	if start := models.ClockHHMM(w.StartTime); start != "" {
		upd.WarmupStartTime, set = &start, true
	}
	if end := models.ClockHHMM(w.EndTime); end != "" {
		upd.WarmupEndTime, set = &end, true
	}
	if w.Days > 0 {
		upd.WarmupDays, set = &w.Days, true
	}
	if w.Timezone != "" {
		upd.Timezone, set = &w.Timezone, true
	}
	if !set {
		return
	}
	if _, xerr := s.emailSvc.Update(ctx, orgID.String(), userID, accountID.String(), upd); xerr != nil {
		log.Warn().Str("account_id", accountID.String()).Str("code", xerr.Identifier).Str("error", xerr.Message).Msg("pool link: warmup settings update failed")
	}
}

func (s *service) ListMailboxes(ctx context.Context, inst *models.PoolLinkInstance) ([]models.PoolLinkMailboxState, *errx.Error) {
	rows, err := s.repo.ListMailboxes(ctx, inst.ID)
	if err != nil {
		return nil, errx.InternalError()
	}
	out := make([]models.PoolLinkMailboxState, 0, len(rows))
	for start := 0; start < len(rows); start += config.AccountStatusLimitMax {
		batch := rows[start:min(start+config.AccountStatusLimitMax, len(rows))]
		ids := make([]uuid.UUID, len(batch))
		for i, m := range batch {
			ids[i] = m.EmailAccountID
		}
		accounts, xerr := s.emails.Search(ctx, inst.OrganizationID.String(), "", nil, nil, config.AccountStatusLimitMax, ids)
		if xerr != nil {
			return nil, xerr
		}
		byID := make(map[uuid.UUID]models.Email, len(accounts.Data))
		for _, acc := range accounts.Data {
			byID[acc.ID] = acc
		}
		statuses := make(map[uuid.UUID]models.EmailAccountStatus)
		if s.analytics != nil {
			page, xerr := s.analytics.GetAccountStatusDetailsPage(ctx, inst.OrganizationID, ids, "", config.AccountStatusLimitMax)
			if xerr != nil {
				return nil, xerr
			}
			for _, status := range page.Data {
				statuses[status.ID] = status
			}
		}
		summaries := make(map[uuid.UUID]repository.WarmupSummary)
		if s.warmup != nil {
			now := time.Now()
			summaries, err = s.warmup.GetWarmupSummaries(ctx, ids, now.Add(-7*24*time.Hour), now)
			if err != nil {
				return nil, errx.InternalError()
			}
		}
		for _, m := range batch {
			acc, ok := byID[m.EmailAccountID]
			if !ok {
				return nil, errx.InternalError()
			}
			st := mailboxState(&acc, &m)
			if s.analytics != nil {
				status, ok := statuses[acc.ID]
				if !ok {
					return nil, errx.InternalError()
				}
				applyMailboxStatus(st, &status)
			}
			summary := summaries[acc.ID]
			st.Sent7d, st.Replied7d, st.SpamPlaced7d = summary.Sent, summary.Replied, summary.SpamPlaced
			out = append(out, *st)
		}
	}
	return out, nil
}

func (s *service) ListStanding(ctx context.Context, inst *models.PoolLinkInstance) ([]models.PoolLinkMailboxStanding, *errx.Error) {
	out, err := s.repo.ListStanding(ctx, inst.ID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return out, nil
}

func (s *service) GetMailbox(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID) (*models.PoolLinkMailboxState, *errx.Error) {
	m, err := s.repo.GetMailboxByRemote(ctx, inst.ID, remoteID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if m == nil {
		return nil, ErrMailboxNotFound
	}
	return s.state(ctx, inst, m)
}

func (s *service) state(ctx context.Context, inst *models.PoolLinkInstance, m *models.PoolLinkMailbox) (*models.PoolLinkMailboxState, *errx.Error) {
	acc, xerr := s.emails.GetByID(ctx, m.EmailAccountID)
	if xerr != nil {
		return nil, xerr
	}
	if acc.OrganizationID == nil || *acc.OrganizationID != inst.OrganizationID {
		return nil, ErrMailboxNotFound
	}
	st := mailboxState(acc, m)
	if s.analytics != nil {
		status, xerr := s.analytics.GetAccountStatusDetail(ctx, inst.OrganizationID, acc.ID)
		if xerr == nil && status != nil {
			applyMailboxStatus(st, status)
		} else if xerr != nil {
			log.Warn().Str("account_id", acc.ID.String()).Str("error", xerr.Message).Msg("pool link: account status unavailable")
		}
	}
	if s.warmup != nil {
		since := time.Now().Add(-7 * 24 * time.Hour)
		if n, err := s.warmup.SumWarmupSentSince(ctx, acc.ID, since); err == nil {
			st.Sent7d = n
		}
		if placed, _, err := s.warmup.CountWarmupSpamReportsSince(ctx, acc.ID, since); err == nil {
			st.SpamPlaced7d = placed
		}
		if stats, err := s.warmup.GetWarmupStatistics(ctx, acc.ID, since, time.Now()); err == nil {
			for _, d := range stats {
				st.Replied7d += d.EmailsReplied
			}
		}
	}
	return st, nil
}

func mailboxState(acc *models.Email, m *models.PoolLinkMailbox) *models.PoolLinkMailboxState {
	st := &models.PoolLinkMailboxState{
		Participation:  &models.DiagnosticParticipation{Mode: models.TestParticipationLegacy, Send: acc.TestSendingAllowed(), Receive: acc.TestReceivingAllowed(), SharedDailyLimit: acc.SharedDailyLimit, RollingRecipientLimit: acc.RollingRecipientLimit},
		RemoteID:       m.RemoteID,
		EmailAccountID: acc.ID,
		Email:          acc.Email,
		Name:           acc.Name,
		Provider:       acc.Provider,
		Status:         acc.Status,
		EnrolledAt:     m.EnrolledAt,
		Managed:        m.Managed,
		AuthState:      acc.AuthState,
		Executor:       m.Executor.OrCloud(),
		Settings: models.PoolLinkWarmupSettings{
			Base: acc.WarmupBase, Max: acc.WarmupMax, Increase: acc.WarmupIncrease, ReplyRate: acc.WarmupReplyRate,
			StartTime: acc.WarmupStartTime, EndTime: acc.WarmupEndTime, Days: acc.WarmupDays, Timezone: acc.ClockTimezone(),
		},
	}
	if acc.TestMode != nil {
		st.Participation.Mode = *acc.TestMode
	}
	return st
}

func applyMailboxStatus(st *models.PoolLinkMailboxState, status *models.EmailAccountStatus) {
	st.Warmup = status.WarmupStatus
	st.Health = status.WarmupHealth
	st.Errors = status.Errors
	st.SentToday = status.DailyUsage.WarmupSent
}

func (s *service) PatchMailbox(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID, patch models.PoolLinkMailboxPatch) (*models.PoolLinkMailboxState, *errx.Error) {
	if patch.Participation != nil && !patch.Participation.Valid() {
		return nil, errx.ErrInvalid
	}
	if r, ok := s.repo.(repository.PoolLinkManagedRepository); ok {
		if !managedOperationLocked(ctx) {
			var out *models.PoolLinkMailboxState
			xerr := s.withManagedOperationLock(ctx, inst.ID, func(ctx context.Context) *errx.Error {
				var xerr *errx.Error
				out, xerr = s.PatchMailbox(ctx, inst, remoteID, patch)
				return xerr
			})
			return out, xerr
		}
		op, xerr := s.managedAuthority(ctx, inst, remoteID)
		if xerr != nil {
			return nil, xerr
		}
		if op != nil && patch.Lifecycle != "" {
			if err := r.CompleteManagedActivation(ctx, inst.ID, remoteID); err != nil {
				return nil, errx.InternalError()
			}
		}
	}
	m, err := s.repo.GetMailboxByRemote(ctx, inst.ID, remoteID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if m == nil {
		return nil, ErrMailboxNotFound
	}
	if m.Executor.OrCloud() == models.PoolLinkExecutorInstance && (patch.OAuth != nil || patch.SMTPIMAP != nil) {
		return nil, ErrExecutorConflict
	}
	userID, xerr := s.ownerUserID(ctx, inst)
	if xerr != nil {
		return nil, xerr
	}
	reload := false
	if patch.OAuth != nil && patch.OAuth.RefreshToken != "" {
		if err := s.emails.RefreshBoxTokenForApp(ctx, m.EmailAccountID, patch.OAuth.AccessToken, patch.OAuth.RefreshToken, patch.OAuth.ExpiresAt, nil); err != nil {
			return nil, errx.InternalError()
		}
		reload = true
	}
	if patch.SMTPIMAP != nil {
		if xerr := cloudReachable(patch.SMTPIMAP); xerr != nil {
			return nil, xerr
		}
		if err := s.emails.ReplaceSMTPIMAPCredentials(ctx, m.EmailAccountID, patch.SMTPIMAP); err != nil {
			return nil, errx.InternalError()
		}
		reload = true
	}
	if patch.Warmup != nil {
		s.applyWarmupSettings(ctx, inst.OrganizationID, userID, m.EmailAccountID, *patch.Warmup)
	}
	if p := patch.Participation; p != nil {
		upd := &models.UpdateEmail{TestMode: &p.Mode, TestSendEnabled: &p.Send, TestReceiveEnabled: &p.Receive, SharedDailyLimit: p.SharedDailyLimit, RollingRecipientLimit: p.RollingRecipientLimit}
		if p.Send {
			upd.Warmup = &p.Send
		}
		if _, xerr := s.emailSvc.Update(ctx, inst.OrganizationID.String(), userID, m.EmailAccountID.String(), upd); xerr != nil {
			return nil, xerr
		}
		if p.Send && s.scheduler != nil {
			_ = s.scheduler.EnsureWarmupScheduled(ctx, m.EmailAccountID)
		}
	}
	switch patch.Lifecycle {
	case "pause", "resume":
		if _, xerr := s.emailSvc.SetWarmupLifecycle(ctx, inst.OrganizationID.String(), m.EmailAccountID.String(), patch.Lifecycle); xerr != nil {
			return nil, xerr
		}
		if patch.Lifecycle == "resume" && s.scheduler != nil {
			_ = s.scheduler.EnsureWarmupScheduled(ctx, m.EmailAccountID)
		}
	}
	if reload {
		if err := s.emailSvc.LoadAccountOntoWorker(ctx, m.EmailAccountID); err != nil {
			log.Warn().Err(err).Str("account_id", m.EmailAccountID.String()).Msg("pool link: worker reload after credential change failed")
		}
	}
	return s.GetMailbox(ctx, inst, remoteID)
}

func (s *service) Unenroll(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID) *errx.Error {
	return s.unenroll(ctx, inst, remoteID)
}

func (s *service) unenroll(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID) *errx.Error {
	if _, ok := s.repo.(repository.PoolLinkManagedRepository); ok && !managedOperationLocked(ctx) {
		return s.withManagedOperationLock(ctx, inst.ID, func(ctx context.Context) *errx.Error { return s.unenroll(ctx, inst, remoteID) })
	}
	if r, ok := s.repo.(repository.PoolLinkManagedRepository); ok {
		if err := r.RevokeManagedOperations(ctx, inst.ID, &remoteID); err != nil {
			return errx.InternalError()
		}
	}
	m, err := s.repo.GetMailboxByRemote(ctx, inst.ID, remoteID)
	if err != nil {
		return errx.InternalError()
	}
	if m == nil {
		return ErrMailboxNotFound
	}
	// A released mailbox sends and receives no Cloud warmup, whether Cloud keeps it or deletes it.
	if xerr := s.stopParticipation(ctx, inst, *m); xerr != nil {
		return xerr
	}
	if !m.Managed {
		if xerr := s.emailSvc.Delete(ctx, inst.OrganizationID.String(), m.EmailAccountID.String()); xerr != nil && xerr != errx.ErrNotFound {
			return xerr
		}
	}
	if err := s.repo.DeleteMailbox(ctx, inst.ID, remoteID); err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) HasActiveLink(ctx context.Context, orgID uuid.UUID) bool {
	ok, err := s.repo.HasActiveInstance(ctx, orgID)
	return err == nil && ok
}
