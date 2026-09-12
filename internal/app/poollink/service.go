// Package poollink is the cloud side of the self-hosted warmup pool link.
package poollink

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
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
	ErrCodeNotFound      = errx.NewWithIdentifier(errx.NotFound, "pool_link_code_not_found", "That code is unknown or has expired. Start the connection again from your instance.")
	ErrCodeNotPending    = errx.NewWithIdentifier(errx.Conflict, "pool_link_code_used", "That code has already been used.")
	ErrCodeDenied        = errx.NewWithIdentifier(errx.Forbidden, "pool_link_denied", "The connection was declined.")
	ErrInstanceNotFound  = errx.NewWithIdentifier(errx.NotFound, "pool_link_instance_not_found", "Linked instance not found.")
	ErrInstanceRevoked   = errx.NewWithIdentifier(errx.Unauthorized, "pool_link_revoked", "This instance's link has been revoked. Reconnect from the instance settings.")
	ErrMailboxLimit      = errx.NewWithIdentifier(errx.PaymentRequired, "pool_link_mailbox_limit", "This workspace has reached the free allowance of linked mailboxes. Upgrade to the pool plan for unlimited mailboxes.")
	ErrMailboxNotFound   = errx.NewWithIdentifier(errx.NotFound, "pool_link_mailbox_not_found", "That mailbox is not enrolled.")
	ErrWarmupNotEntitled = errx.NewWithIdentifier(errx.Forbidden, "pool_link_warmup_unavailable", "Warmup is not available on this workspace.")
	ErrBadCredential     = errx.NewWithIdentifier(errx.BadRequest, "pool_link_credential", "A credential matching the provider is required.")
	ErrBadRequest        = errx.NewWithIdentifier(errx.BadRequest, "pool_link_request", "Instance name is required.")
)

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
	// RevokeInstance ends a link and removes every mailbox it enrolled.
	RevokeInstance(ctx context.Context, orgID, instanceID uuid.UUID) *errx.Error

	Enroll(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkEnrollRequest) (*models.PoolLinkMailboxState, *errx.Error)
	ListMailboxes(ctx context.Context, inst *models.PoolLinkInstance) ([]models.PoolLinkMailboxState, *errx.Error)
	GetMailbox(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID) (*models.PoolLinkMailboxState, *errx.Error)
	PatchMailbox(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID, patch models.PoolLinkMailboxPatch) (*models.PoolLinkMailboxState, *errx.Error)
	Unenroll(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID) *errx.Error

	// Cloud-managed mailboxes for linked instances (oauth.go).
	StartOAuth(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkOAuthStartRequest) (*models.PoolLinkOAuthStartResponse, *errx.Error)
	// CompleteOAuthCallback finishes a brokered consent; returns "" when the state is not brokered.
	CompleteOAuthCallback(ctx context.Context, provider, code, state, providerErr string) string
	FinishOAuth(ctx context.Context, inst *models.PoolLinkInstance, session string) (*models.PoolLinkMailboxState, *errx.Error)
	AccessToken(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID) (*models.PoolLinkAccessToken, *errx.Error)
	ListWorkspaceMailboxes(ctx context.Context, inst *models.PoolLinkInstance) ([]models.PoolLinkWorkspaceMailbox, *errx.Error)
	Adopt(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkAdoptRequest) (*models.PoolLinkMailboxState, *errx.Error)
	VerifyWarmupToken(ctx context.Context, inst *models.PoolLinkInstance, remoteID, token uuid.UUID) (bool, *errx.Error)

	// IsLinkedMailbox is the consumer's hot-path warmup-only check.
	IsLinkedMailbox(ctx context.Context, accountID uuid.UUID) bool
	// HasActiveLink entitles a workspace to warm its linked mailboxes.
	HasActiveLink(ctx context.Context, orgID uuid.UUID) bool
	// Plan is the allowance the workspace currently has.
	Plan(ctx context.Context, orgID uuid.UUID) (models.PoolLinkPlan, *errx.Error)
}

type service struct {
	repo      repository.PoolLinkRepository
	emails    repository.EmailRepository
	emailSvc  email.EmailService
	analytics analytics.AnalyticsService
	warmup    repository.WarmupRepository
	gate      feature.FeatureGateService
	orgs      organization.OrganizationService
	scheduler WarmupScheduler
	planRepo  repository.PlanRepository
	cache     *cache.Cache
}

func NewService(
	repo repository.PoolLinkRepository,
	emails repository.EmailRepository,
	emailSvc email.EmailService,
	analyticsSvc analytics.AnalyticsService,
	warmup repository.WarmupRepository,
	gate feature.FeatureGateService,
	orgs organization.OrganizationService,
	planRepo repository.PlanRepository,
) Service {
	return &service{repo: repo, emails: emails, emailSvc: emailSvc, analytics: analyticsSvc, warmup: warmup, gate: gate, orgs: orgs, planRepo: planRepo}
}

// WireScheduler attaches the warmup task seeder after construction.
func (s *service) WireScheduler(w WarmupScheduler) { s.scheduler = w }

// Unambiguous alphabet: no 0/O, 1/I/L.
const userCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func randomUserCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 0, 9)
	for i, v := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, userCodeAlphabet[int(v)%len(userCodeAlphabet)])
	}
	return string(out), nil
}

func randomToken(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

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

func (s *service) StartCode(ctx context.Context, req models.PoolLinkStartRequest) (*models.PoolLinkStartResponse, *errx.Error) {
	req.InstanceName = strings.TrimSpace(req.InstanceName)
	if req.InstanceName == "" {
		return nil, ErrBadRequest
	}
	if len(req.InstanceName) > 80 {
		req.InstanceName = req.InstanceName[:80]
	}
	if u, err := url.Parse(strings.TrimSpace(req.InstanceURL)); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		req.InstanceURL = ""
	}
	if len(req.InstanceVersion) > 40 {
		req.InstanceVersion = req.InstanceVersion[:40]
	}

	deviceCode, err := randomToken("")
	if err != nil {
		return nil, errx.InternalError()
	}
	_ = s.repo.DeleteExpiredCodes(ctx)
	var code *models.PoolLinkCode
	for attempt := 0; attempt < 3; attempt++ {
		userCode, err := randomUserCode()
		if err != nil {
			return nil, errx.InternalError()
		}
		code, err = s.repo.CreateCode(ctx, hashToken(deviceCode), userCode, req, time.Now().Add(config.PoolLinkCodeTTLMinutes*time.Minute))
		if err == nil {
			break
		}
		// A user-code collision is the only expected failure; retry with a new one.
		code = nil
	}
	if code == nil {
		return nil, errx.InternalError()
	}
	return &models.PoolLinkStartResponse{
		DeviceCode:      deviceCode,
		UserCode:        code.UserCode,
		VerificationURL: config.AppBaseURL() + "/connect?code=" + url.QueryEscape(code.UserCode),
		ExpiresIn:       config.PoolLinkCodeTTLMinutes * 60,
		Interval:        config.PoolLinkPollIntervalSeconds,
	}, nil
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

func (s *service) ApproveCode(ctx context.Context, userCode string, orgID, userID uuid.UUID) (*models.PoolLinkInstance, *errx.Error) {
	code, xerr := s.DescribeCode(ctx, userCode)
	if xerr != nil {
		return nil, xerr
	}
	if code.Status != models.PoolLinkCodePending {
		return nil, ErrCodeNotPending
	}
	token, err := randomToken("wpl_")
	if err != nil {
		return nil, errx.InternalError()
	}
	inst := &models.PoolLinkInstance{
		ID:             uuid.New(),
		OrganizationID: orgID,
		Name:           code.InstanceName,
		URL:            code.InstanceURL,
		Version:        code.InstanceVersion,
		CreatedBy:      &userID,
	}
	if err := s.repo.CreateInstance(ctx, inst, hashToken(token)); err != nil {
		return nil, errx.InternalError()
	}
	ok, err := s.repo.ApproveCode(ctx, code.UserCode, orgID, userID, inst.ID, token)
	if err != nil {
		return nil, errx.InternalError()
	}
	if !ok {
		_ = s.repo.RevokeInstance(ctx, inst.ID)
		return nil, ErrCodeNotPending
	}
	return inst, nil
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
	plan := models.PoolLinkPlan{Tier: "free", Enrolled: enrolled, PriceUSD: config.PoolLinkPlanPriceUSD, WarmupEntitled: true}
	if config.BillingProvider() == "none" {
		plan.Tier = "paid"
		return plan, nil
	}
	paid, xerr := s.gate.IsPaidOrganization(ctx, orgID)
	if xerr != nil {
		return plan, xerr
	}
	if paid {
		plan.Tier = "paid"
		return plan, nil
	}
	limit := models.FreeWorkspaceMailboxLimit
	plan.MailboxLimit = &limit
	if s.planRepo != nil {
		if p, err := s.planRepo.GetByID(ctx, uuid.MustParse(config.PoolLinkPlanID)); err == nil && p != nil && p.StripePriceID != nil && *p.StripePriceID != "" {
			plan.UpgradeURL = config.AppBaseURL() + "/app/settings/billing?pool=1"
		}
	}
	return plan, nil
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
		Instance:     *inst,
		Organization: models.PoolLinkOrgInfo{ID: org.ID, Name: org.Name},
		Plan:         plan,
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
	inst, err := s.repo.GetInstance(ctx, instanceID)
	if err != nil {
		return errx.InternalError()
	}
	if inst == nil || inst.OrganizationID != orgID {
		return ErrInstanceNotFound
	}
	mailboxes, err := s.repo.ListMailboxes(ctx, inst.ID)
	if err != nil {
		return errx.InternalError()
	}
	for _, m := range mailboxes {
		if xerr := s.Unenroll(ctx, inst, m.RemoteID); xerr != nil {
			log.Warn().Str("account_id", m.EmailAccountID.String()).Msg("pool link: mailbox removal failed during revoke; continuing")
		}
	}
	if err := s.repo.RevokeInstance(ctx, inst.ID); err != nil {
		return errx.InternalError()
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

func (s *service) Enroll(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkEnrollRequest) (*models.PoolLinkMailboxState, *errx.Error) {
	if req.RemoteID == uuid.Nil || strings.TrimSpace(req.Email) == "" {
		return nil, ErrBadRequest
	}
	if existing, err := s.repo.GetMailboxByRemote(ctx, inst.ID, req.RemoteID); err != nil {
		return nil, errx.InternalError()
	} else if existing != nil {
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
	if plan.MailboxLimit != nil && plan.Enrolled >= *plan.MailboxLimit {
		return nil, ErrMailboxLimit
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
			OrganizationID: &orgID,
			Provider:       req.Provider,
			Name:           name,
			Email:          strings.TrimSpace(req.Email),
			AccessToken:    req.OAuth.AccessToken,
			RefreshToken:   req.OAuth.RefreshToken,
			ExpiresAt:      req.OAuth.ExpiresAt,
		})
	case models.InboxProviderSMTPIMAP:
		if req.SMTPIMAP == nil || req.SMTPIMAP.SMTP == nil || req.SMTPIMAP.IMAP == nil {
			return nil, ErrBadCredential
		}
		acc, xerr = s.emails.NewSMTPIMAPAccount(ctx, userID, models.NewSMTPIMAPAccount{
			OrganizationID: &orgID,
			Name:           name,
			Email:          strings.TrimSpace(req.Email),
			SMTP:           req.SMTPIMAP.SMTP,
			IMAP:           req.SMTPIMAP.IMAP,
		})
	default:
		return nil, ErrBadCredential
	}
	if xerr != nil {
		return nil, xerr
	}

	if err := s.repo.EnrollMailbox(ctx, &models.PoolLinkMailbox{InstanceID: inst.ID, RemoteID: req.RemoteID, EmailAccountID: acc.ID}); err != nil {
		_ = s.emailSvc.Delete(ctx, userID, acc.ID.String())
		return nil, errx.InternalError()
	}

	s.applyWarmupSettings(ctx, orgID, userID, acc.ID, req.Warmup)
	if _, xerr := s.emailSvc.SetWarmupLifecycle(ctx, userID, acc.ID.String(), "start"); xerr != nil {
		log.Warn().Str("account_id", acc.ID.String()).Msg("pool link: warmup start failed after enrollment")
	}
	if err := s.emailSvc.LoadAccountOntoWorker(ctx, acc.ID); err != nil {
		log.Warn().Err(err).Str("account_id", acc.ID.String()).Msg("pool link: worker load failed; reconciler will retry")
	}
	if s.scheduler != nil {
		_ = s.scheduler.EnsureWarmupScheduled(ctx, acc.ID)
	}
	return s.GetMailbox(ctx, inst, req.RemoteID)
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
	if w.StartTime != "" {
		upd.WarmupStartTime, set = &w.StartTime, true
	}
	if w.EndTime != "" {
		upd.WarmupEndTime, set = &w.EndTime, true
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
		log.Warn().Str("account_id", accountID.String()).Msg("pool link: warmup settings update failed")
	}
}

func (s *service) ListMailboxes(ctx context.Context, inst *models.PoolLinkInstance) ([]models.PoolLinkMailboxState, *errx.Error) {
	rows, err := s.repo.ListMailboxes(ctx, inst.ID)
	if err != nil {
		return nil, errx.InternalError()
	}
	out := make([]models.PoolLinkMailboxState, 0, len(rows))
	for _, m := range rows {
		state, xerr := s.state(ctx, inst, &m)
		if xerr != nil {
			continue
		}
		out = append(out, *state)
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
	st := &models.PoolLinkMailboxState{
		RemoteID:       m.RemoteID,
		EmailAccountID: acc.ID,
		Email:          acc.Email,
		Name:           acc.Name,
		Provider:       acc.Provider,
		Status:         acc.Status,
		EnrolledAt:     m.EnrolledAt,
		Managed:        m.Managed,
		AuthState:      acc.AuthState,
		Settings: models.PoolLinkWarmupSettings{
			Base: acc.WarmupBase, Max: acc.WarmupMax, Increase: acc.WarmupIncrease, ReplyRate: acc.WarmupReplyRate,
			StartTime: acc.WarmupStartTime, EndTime: acc.WarmupEndTime, Days: acc.WarmupDays, Timezone: acc.Timezone,
		},
	}
	if s.analytics != nil {
		status, xerr := s.analytics.GetAccountStatus(ctx, inst.OrganizationID, acc.ID)
		if xerr == nil && status != nil {
			st.Warmup = status.WarmupStatus
			st.Health = status.WarmupHealth
			st.Errors = status.Errors
			st.SentToday = status.DailyUsage.WarmupSent
		} else if xerr != nil {
			log.Warn().Str("account_id", acc.ID.String()).Str("error", xerr.Message).Msg("pool link: account status unavailable")
		}
	}
	if s.warmup != nil {
		since := time.Now().Add(-7 * 24 * time.Hour)
		if n, err := s.warmup.SumWarmupSentSince(ctx, acc.ID, since); err == nil {
			st.Sent7d = n
		}
		if n, err := s.warmup.CountSpamPlacementsSince(ctx, acc.ID, since); err == nil {
			st.SpamPlaced7d = n
		}
		if stats, err := s.warmup.GetWarmupStatistics(ctx, acc.ID, since, time.Now()); err == nil {
			for _, d := range stats {
				st.Replied7d += d.EmailsReplied
			}
		}
	}
	return st, nil
}

func (s *service) PatchMailbox(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID, patch models.PoolLinkMailboxPatch) (*models.PoolLinkMailboxState, *errx.Error) {
	m, err := s.repo.GetMailboxByRemote(ctx, inst.ID, remoteID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if m == nil {
		return nil, ErrMailboxNotFound
	}
	userID, xerr := s.ownerUserID(ctx, inst)
	if xerr != nil {
		return nil, xerr
	}
	reload := false
	if patch.OAuth != nil && patch.OAuth.RefreshToken != "" {
		if err := s.emails.RefreshBoxToken(ctx, m.EmailAccountID, patch.OAuth.AccessToken, patch.OAuth.RefreshToken, patch.OAuth.ExpiresAt); err != nil {
			return nil, errx.InternalError()
		}
		reload = true
	}
	if patch.SMTPIMAP != nil {
		if err := s.emails.ReplaceSMTPIMAPCredentials(ctx, m.EmailAccountID, patch.SMTPIMAP); err != nil {
			return nil, errx.InternalError()
		}
		reload = true
	}
	if patch.Warmup != nil {
		s.applyWarmupSettings(ctx, inst.OrganizationID, userID, m.EmailAccountID, *patch.Warmup)
	}
	switch patch.Lifecycle {
	case "pause", "resume":
		if _, xerr := s.emailSvc.SetWarmupLifecycle(ctx, userID, m.EmailAccountID.String(), patch.Lifecycle); xerr != nil {
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
	m, err := s.repo.GetMailboxByRemote(ctx, inst.ID, remoteID)
	if err != nil {
		return errx.InternalError()
	}
	if m == nil {
		return ErrMailboxNotFound
	}
	// A managed mailbox belongs to the workspace; only the link goes.
	if !m.Managed {
		userID, xerr := s.ownerUserID(ctx, inst)
		if xerr != nil {
			return xerr
		}
		if xerr := s.emailSvc.Delete(ctx, userID, m.EmailAccountID.String()); xerr != nil && xerr != errx.ErrNotFound {
			return xerr
		}
	}
	if err := s.repo.DeleteMailbox(ctx, inst.ID, remoteID); err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) IsLinkedMailbox(ctx context.Context, accountID uuid.UUID) bool {
	m, err := s.repo.GetMailboxByAccount(ctx, accountID)
	return err == nil && m != nil
}

func (s *service) HasActiveLink(ctx context.Context, orgID uuid.UUID) bool {
	ok, err := s.repo.HasActiveInstance(ctx, orgID)
	return err == nil && ok
}
