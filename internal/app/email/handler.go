package email

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/lifecycle"
	"github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/dnsauth"
	"github.com/warmbly/warmbly/internal/pkg/trackdns"
	"github.com/warmbly/warmbly/internal/utils/paging"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

func (s *emailService) Search(ctx context.Context, orgID, search, cursor, tag, limit string, allowedAccountIDs []uuid.UUID) (*models.EmailsResult, *errx.Error) {
	cursorId, err := paging.DecodeCursor(cursor)
	if err != nil {
		return nil, err
	}
	tagId, err := validate.Uuid(tag)
	if err != nil {
		return nil, err
	}

	if limit == "" {
		limit = "50"
	}

	limitN, err := validate.Limit(limit)
	if err != nil {
		return nil, err
	}

	return s.emailRepository.Search(ctx, orgID, search, cursorId, tagId, limitN, allowedAccountIDs)
}

func (s *emailService) Get(ctx context.Context, orgID, emailAccountID string) (*models.Email, *errx.Error) {
	return s.emailRepository.Get(ctx, orgID, emailAccountID)
}

func (s *emailService) Update(ctx context.Context, orgID, userID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error) {
	account, err := s.emailRepository.Update(ctx, orgID, emailAccountID, udata)
	if err != nil {
		return nil, err
	}

	s.syncWarmupPoolMembership(ctx, account)
	s.applyStatusToWorker(ctx, userID, account, udata.Status)
	s.publishAccountEvent(ctx, pubsub.EventAccountSynced, account)
	return account, nil
}

// applyStatusToWorker carries a status change through to the machine running
// the mailbox, which only the row recorded before. Best-effort both ways: no
// load path ships a mailbox that is not active, and the reconciler re-places an
// active one anyway.
func (s *emailService) applyStatusToWorker(ctx context.Context, userID string, account *models.Email, status *string) {
	if account == nil || status == nil {
		return
	}

	if *status == "active" {
		s.loadAccountBestEffort(ctx, account.ID)
		return
	}

	if xerr := s.dropFromWorker(ctx, userID, account.ID); xerr != nil {
		log.Warn().
			Str("error", xerr.Message).
			Str("email_id", account.ID.String()).
			Str("status", *status).
			Msg("could not tell the worker to drop the disabled mailbox; it stops syncing when that worker next restarts")
	}
}

// BulkUpdateTags is a pure tag-link rewrite: no warmup pool or worker state
// depends on tags, so no per-account fanout is needed (the caller audits
// once and the spine refreshes lists).
func (s *emailService) BulkUpdateTags(ctx context.Context, orgID string, emailIDs, addTags, removeTags []uuid.UUID) (int, *errx.Error) {
	if len(addTags) == 0 && len(removeTags) == 0 {
		return 0, errx.ErrNotEnough
	}
	return s.emailRepository.BulkUpdateTags(ctx, orgID, emailIDs, addTags, removeTags)
}

// SetWarmupLifecycle applies a warmup start/pause/resume/disable transition,
// then re-syncs pool membership and fans out the change so dashboards update
// live. Seeding the actual warmup task chain is the caller's responsibility
// (the API handler triggers EnsureWarmupScheduled) — this service has no
// Cloud Tasks client.
func (s *emailService) SetWarmupLifecycle(ctx context.Context, userID, emailAccountID, action string) (*models.Email, *errx.Error) {
	account, err := s.emailRepository.SetWarmupLifecycle(ctx, userID, emailAccountID, action)
	if err != nil {
		return nil, err
	}

	s.syncWarmupPoolMembership(ctx, account)
	s.publishAccountEvent(ctx, pubsub.EventAccountSynced, account)
	return account, nil
}

// SetSendHold: force is set because this is the owner's decision the rebalancer's guard protects.
// A release also serves as the manual exit from resting (issue #243).
func (s *emailService) SetSendHold(ctx context.Context, orgID, emailAccountID string, hold bool) (*models.SendLifecycleState, *errx.Error) {
	if s.lifecycleRepo == nil {
		return nil, errx.New(errx.Conflict, "Holding a mailbox is not available on this install.")
	}
	account, xerr := s.emailRepository.Get(ctx, orgID, emailAccountID)
	if xerr != nil {
		return nil, xerr
	}
	next, reason := models.SendLifecycleReserve, "held back by its owner"
	if !hold {
		// A release lands where the rebalancer would put the mailbox now.
		candidate, err := s.lifecycleRepo.GetLifecycleCandidate(ctx, account.ID)
		if err != nil {
			log.Error().Err(err).Str("email_account_id", account.ID.String()).Msg("could not read send lifecycle")
			return nil, errx.InternalError()
		}
		d := lifecycle.Decide(models.SendLifecycleActive, nil, candidate.HealthState, time.Now())
		next, reason = d.Next, "released by its owner"
		if candidate.Current == models.SendLifecycleResting {
			reason = "put back by its owner"
		}
		if d.Reason != "" {
			reason = "released by its owner; " + d.Reason
		}
	}
	if _, err := s.lifecycleRepo.SetSendLifecycle(ctx, account.ID, next, reason, true); err != nil {
		log.Error().Err(err).Str("email_account_id", account.ID.String()).Msg("could not set send hold")
		return nil, errx.InternalError()
	}
	states, err := s.lifecycleRepo.GetSendLifecycles(ctx, []uuid.UUID{account.ID})
	if err != nil {
		log.Error().Err(err).Str("email_account_id", account.ID.String()).Msg("could not read send lifecycle")
		return nil, errx.InternalError()
	}
	state := states[account.ID]
	s.publishAccountEvent(ctx, pubsub.EventAccountSynced, account)
	return &state, nil
}

// UpdateTrackingDomain sets or clears a mailbox's custom tracking domain and
// resolves it immediately. The host it has to point at is this install's
// TRACKING_DOMAIN, not a hardcoded warmbly.com name: a self-hosted deployment
// was previously told to CNAME at a host it has nothing to do with, so its
// customers could never verify.
func (s *emailService) UpdateTrackingDomain(ctx context.Context, orgID, emailAccountID, domain string) (*models.TrackingDomainStatus, *errx.Error) {
	// Accept what a human pastes (a full URL, a trailing dot, stray case) and
	// store the bare host. Anything still malformed after that is a real error
	// with a real message, not a domain that saves and then sits at "pending"
	// forever because it can never resolve.
	domain = config.NormalizeTrackingHost(domain)
	if domain != "" {
		if xerr := validate.ValidateTrackingDomain(domain); xerr != nil {
			return nil, xerr
		}
	}

	status := s.resolveTrackingDomain(ctx, domain)

	if err := s.emailRepository.UpdateTrackingDomain(ctx, orgID, emailAccountID, domain, status.TrackingDomainVerified, status.TrackingDomainVerifiedAt); err != nil {
		return nil, err
	}

	return status, nil
}

// GetTrackingDomain reports a mailbox's stored tracking-domain state plus the
// CNAME target for this install. It does no DNS work: resolving is a write
// (the verdict gates whether real links route through the custom host), so it
// lives behind VerifyTrackingDomain, the same split as CheckDomainAuth and
// RefreshDomainAuth.
func (s *emailService) GetTrackingDomain(ctx context.Context, orgID, emailAccountID string) (*models.TrackingDomainStatus, *errx.Error) {
	account, xerr := s.Get(ctx, orgID, emailAccountID)
	if xerr != nil {
		return nil, xerr
	}

	target := config.TrackingHostname()
	status := &models.TrackingDomainStatus{
		TrackingDomain:           account.TrackingDomain,
		TrackingDomainVerified:   account.TrackingDomainVerified,
		TrackingDomainVerifiedAt: account.TrackingDomainVerifiedAt,
		CNAMETarget:              target,
	}

	switch {
	case account.TrackingDomain == "":
		status.Status = trackdns.CodeUnset
		status.Message = "No custom tracking domain is set, so opens and clicks go through the shared tracking host and the unsubscribe link stays on this install's API address."
	case target == "":
		status.Status = trackdns.CodeNoTarget
		status.Message = "This Warmbly install has no tracking host configured, so there is nothing to point a CNAME at yet. Ask your administrator to set TRACKING_DOMAIN."
	case account.TrackingDomainVerified:
		status.Status = trackdns.CodeVerified
		status.Message = fmt.Sprintf("%s points at %s. Opens, clicks and the unsubscribe link in this mailbox's campaign mail are served there.", account.TrackingDomain, target)
	default:
		status.Status = trackingStatusPending
		status.Message = fmt.Sprintf("%s has not verified yet. Check it again to see what DNS returns for it right now.", account.TrackingDomain)
	}

	return status, nil
}

// VerifyTrackingDomain re-resolves the stored domain and persists the verdict.
// This is the escape hatch from "pending": a customer who fixed their record
// gets it honored now rather than on the next save.
func (s *emailService) VerifyTrackingDomain(ctx context.Context, orgID, emailAccountID string) (*models.TrackingDomainStatus, *errx.Error) {
	account, xerr := s.Get(ctx, orgID, emailAccountID)
	if xerr != nil {
		return nil, xerr
	}

	status := s.resolveTrackingDomain(ctx, account.TrackingDomain)
	// Re-verifying re-resolves; it never rewrites the stored value.
	status.TrackingDomain = account.TrackingDomain

	if err := s.emailRepository.UpdateTrackingDomain(ctx, orgID, emailAccountID, account.TrackingDomain, status.TrackingDomainVerified, status.TrackingDomainVerifiedAt); err != nil {
		return nil, err
	}

	return status, nil
}

// trackingStatusPending marks stored state that has not been re-resolved, as
// opposed to the codes a live lookup produces.
const trackingStatusPending = "pending"

// resolveTrackingDomain runs the live lookup and renders it as an API status.
func (s *emailService) resolveTrackingDomain(ctx context.Context, domain string) *models.TrackingDomainStatus {
	target := config.TrackingHostname()
	res := trackdns.Verify(ctx, domain, target)

	status := &models.TrackingDomainStatus{
		TrackingDomain:           res.Domain,
		TrackingDomainVerified:   res.Verified,
		CNAMETarget:              target,
		Status:                   res.Code,
		Message:                  res.Reason,
		Observed:                 res.Observed,
		TrackingHostUnresolvable: res.TargetUnresolvable,
	}
	if res.Verified {
		now := time.Now().UTC()
		status.TrackingDomainVerifiedAt = &now
	}
	return status
}

// CheckDomainAuth runs a live SPF/DKIM/DMARC lookup for a mailbox's sending
// domain and reports it without writing anything.
func (s *emailService) CheckDomainAuth(ctx context.Context, orgID, emailAccountID string) (*dnsauth.Result, *errx.Error) {
	_, res, xerr := s.resolveDomainAuth(ctx, orgID, emailAccountID)
	return res, xerr
}

// RefreshDomainAuth runs the same lookup and persists the verdict for every
// active mailbox on that domain (authentication is a per-domain property, so
// one owner's fix clears it for all of their mailboxes on it at once).
//
// This is the escape hatch from the send gate. Without it an owner who repairs
// their DNS would keep being blocked until the background sweep next reached
// their domain, which can be a day away, and "I fixed it and nothing happened"
// is how a correct gate still becomes a support incident.
func (s *emailService) RefreshDomainAuth(ctx context.Context, orgID, emailAccountID string) (*dnsauth.Result, *errx.Error) {
	domain, res, xerr := s.resolveDomainAuth(ctx, orgID, emailAccountID)
	if xerr != nil {
		return nil, xerr
	}
	if domain == "" {
		return res, nil
	}

	// Persist best-effort: the caller asked for a live check and gets the live
	// answer either way. A failed write only means the sweep re-derives it.
	_, _ = s.emailRepository.UpdateDomainAuthState(
		ctx, domain, res.State(), res.SPFFound, res.DKIMFound, res.DMARCFound,
		res.DMARCPolicy, res.Summary, time.Now(),
	)
	return res, nil
}

// resolveDomainAuth loads the organization's mailbox (Get is org-scoped, never user-scoped)
// and runs the DNS lookup, returning the domain so the persisting caller does not re-derive it.
func (s *emailService) resolveDomainAuth(ctx context.Context, orgID, emailAccountID string) (string, *dnsauth.Result, *errx.Error) {
	account, xerr := s.emailRepository.Get(ctx, orgID, emailAccountID)
	if xerr != nil {
		return "", nil, xerr
	}
	if account == nil {
		return "", nil, errx.ErrNotFound
	}

	domain := ""
	if at := strings.LastIndex(account.Email, "@"); at >= 0 {
		domain = strings.ToLower(strings.TrimSpace(account.Email[at+1:]))
	}

	res := dnsauth.Check(ctx, domain, nil)
	return domain, &res, nil
}

// Delete disconnects a mailbox. The worker is told to drop it BEFORE the row
// goes, because afterwards no assignment is left to read and nothing can repair
// a missed removal, so a removal that cannot be sent fails the whole delete.
func (s *emailService) Delete(ctx context.Context, userID, emailAccountID string) *errx.Error {
	accountID, err := uuid.Parse(emailAccountID)
	if err != nil {
		return errx.ErrUuid
	}

	// Read by id: Get is scoped by organization and was being handed a user id,
	// so it never found the mailbox and every side effect below was skipped.
	// Ownership moves here, or the removal below would be publishable for a
	// mailbox the caller does not own.
	account, xerr := s.emailRepository.GetByID(ctx, accountID)
	if xerr != nil {
		return xerr
	}
	if account == nil || !sameUser(account.UserID, userID) {
		return errx.ErrNotFound
	}

	if xerr := s.dropFromWorker(ctx, userID, accountID); xerr != nil {
		return xerr
	}

	// The refund travels inside the delete's transaction: the foreign key only
	// nulls worker_id, so a worker not credited here stays charged for a
	// mailbox that no longer exists, unrepairably.
	refund := worker.MailboxWeight(account.Provider, account.Warmup != nil)
	if xerr := s.emailRepository.Delete(ctx, userID, emailAccountID, refund); xerr != nil {
		// The removal already went out and the mailbox is still active: put it
		// back now instead of leaving it dark until the reconciler's next pass.
		s.loadAccountBestEffort(ctx, accountID)
		return xerr
	}

	s.removeFromAllWarmupPools(ctx, account)
	s.publishAccountEvent(ctx, pubsub.EventAccountDisconnected, account)

	if s.webhookService != nil && account.OrganizationID != nil {
		_, _ = s.webhookService.Dispatch(ctx, *account.OrganizationID, models.WebhookEventEmailAccountRemoved, map[string]any{
			"email_account_id": account.ID,
			"email":            account.Email,
			"provider":         account.Provider,
		})
	}
	return nil
}

// sameUser compares user ids as uuids, the way the delete's own WHERE clause
// does, so formatting alone never reads as a different owner.
func sameUser(a, b string) bool {
	left, aerr := uuid.Parse(a)
	right, berr := uuid.Parse(b)
	return aerr == nil && berr == nil && left == right
}

func (s *emailService) syncWarmupPoolMembership(ctx context.Context, account *models.Email) {
	if s.warmupService == nil || account == nil {
		return
	}

	// A mailbox Warmbly Cloud warms is no longer a partner here: warmup this
	// instance sent it carries a token the cloud cannot vouch for, so it would
	// be filed as ordinary mail in the owner's inbox. Only a definite answer
	// acts; an unreadable one leaves the membership alone rather than letting a
	// blip evict a healthy mailbox and discard its health record.
	if s.cloudLink != nil {
		enrolled, err := s.cloudLink.GetByAccount(ctx, account.ID)
		if err != nil {
			log.Warn().Err(err).Str("account_id", account.ID.String()).Msg("cloud link lookup failed; warmup pool membership left as it is")
			return
		}
		if enrolled != nil {
			s.removeFromAllWarmupPools(ctx, account)
			return
		}
	}

	if !s.canUseWarmupPool(ctx, account) {
		s.removeFromAllWarmupPools(ctx, account)
		return
	}

	role := "recipient_only"
	if account.Warmup != nil {
		role = "sender_receiver"
	}
	if xerr := s.warmupService.EnsurePoolMembershipWithRole(ctx, account.ID, s.resolveWarmupPoolType(ctx, account), role); xerr != nil {
		log.Warn().Str("account_id", account.ID.String()).Str("error", xerr.Message).Msg("warmup pool membership sync failed")
	}
}

func (s *emailService) removeFromAllWarmupPools(ctx context.Context, account *models.Email) {
	if s.warmupService == nil || account == nil {
		return
	}

	_ = s.warmupService.RemoveFromAllPools(ctx, account.ID)
}

func (s *emailService) canUseWarmupPool(ctx context.Context, account *models.Email) bool {
	if account == nil || account.Status != "active" || account.OrganizationID == nil || s.featureGate == nil {
		return false
	}
	canWarmup, err := s.featureGate.CanUseWarmup(ctx, *account.OrganizationID)
	return err == nil && canWarmup
}

// SyncWarmupPool re-evaluates one mailbox's local warmup pool membership, for
// callers that changed something the membership depends on but not the mailbox
// row itself (enrolling it in, or releasing it from, Warmbly Cloud).
func (s *emailService) SyncWarmupPool(ctx context.Context, accountID uuid.UUID) {
	account, xerr := s.emailRepository.GetByID(ctx, accountID)
	if xerr != nil {
		return
	}
	s.syncWarmupPoolMembership(ctx, account)
}

// orgSuspendedOrRestricted reports whether the workspace's posture bars the
// paid warmup pool. Fails open.
func (s *emailService) orgSuspendedOrRestricted(ctx context.Context, orgID uuid.UUID) bool {
	if s.orgRiskRepo == nil {
		return false
	}
	states, err := s.orgRiskRepo.GetOrgRiskStates(ctx, []uuid.UUID{orgID})
	if err != nil {
		return false
	}
	return states[orgID].ForcesFreeWarmupPool()
}

func (s *emailService) resolveWarmupPoolType(ctx context.Context, account *models.Email) string {
	if account == nil {
		return "premium"
	}
	// No organization means no entitlement to check, so the mailbox gets the
	// lower-trust pool rather than defaulting into the paid one.
	if account.OrganizationID == nil {
		return "free"
	}
	// A restricted organization leaves the paid pool whatever it pays. Checked
	// before the stored tier, which is never empty and would short-circuit it.
	if s.orgSuspendedOrRestricted(ctx, *account.OrganizationID) {
		return "free"
	}
	if account.WarmupPoolType != "" {
		return account.WarmupPoolType
	}
	if s.featureGate != nil {
		isPaid, err := s.featureGate.IsPaidOrganization(ctx, *account.OrganizationID)
		if err == nil && !isPaid {
			return "free"
		}
	}
	return "premium"
}
