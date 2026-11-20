package partner

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/delegation"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

const sweepBatch = 200

// SyncMailboxes keeps partners current on their workspaces' mailboxes: one connected inside Warmbly gets a
// row (off) and mailbox.created, a removed one mailbox.deleted, and a change of status, enablement or
// warmup state mailbox.updated, each announced once.
func (s *Service) SyncMailboxes(ctx context.Context) error {
	if !s.mailboxesOn() {
		return nil
	}
	adopted, err := s.mb.Repo.AdoptPartnerMailboxes(ctx, sweepBatch)
	if err != nil {
		return err
	}
	for _, id := range adopted {
		s.announce(ctx, id, models.PartnerEventMailboxCreated)
	}
	removed, err := s.mb.Repo.MarkRemovedPartnerMailboxes(ctx, sweepBatch)
	if err != nil {
		return err
	}
	for _, id := range removed {
		s.announce(ctx, id, models.PartnerEventMailboxDeleted)
	}
	changed, err := s.mb.Repo.ChangedPartnerMailboxes(ctx, sweepBatch)
	if err != nil {
		return err
	}
	for _, id := range changed {
		s.announce(ctx, id, models.PartnerEventMailboxUpdated)
	}
	return nil
}

// announce publishes a mailbox event with the mailbox as it is now and records what was told.
func (s *Service) announce(ctx context.Context, id uuid.UUID, eventType string) {
	v, err := s.mb.Repo.GetPartnerMailboxByID(ctx, id)
	if err != nil || v == nil {
		if err != nil {
			errs.CaptureException(err)
		}
		return
	}
	m := v.Mailbox
	limit := defaultSendsPerMailboxPerDay
	if po, err := s.Repo.GetPartnerOrg(ctx, m.PartnerID, m.Mode, m.OrgExternalID); err == nil && po != nil {
		limit = po.CampaignSendsPerMailboxPerDay
	}
	view := mailboxView(v, limit, s.now())
	if err := s.Publish(ctx, m.PartnerID, m.Mode, eventType, m.OrgExternalID, view); err != nil {
		errs.CaptureException(err)
		return
	}
	if err := s.mb.Repo.SetPartnerMailboxPublished(ctx, id, publishedState(v)); err != nil {
		errs.CaptureException(err)
	}
}

// ConnectDue connects mailboxes attached with the token or delegation method, in the background, with backoff.
func (s *Service) ConnectDue(ctx context.Context) error {
	if !s.mailboxesOn() || s.mb.Delegator == nil {
		return nil
	}
	rows, err := s.mb.Repo.ClaimPartnerConnects(ctx, connectBatch, connectLease)
	if err != nil {
		return err
	}
	for i := range rows {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		s.connectOne(cctx, &rows[i])
		cancel()
	}
	return nil
}

func (s *Service) connectOne(ctx context.Context, row *models.PartnerMailbox) {
	po, err := s.Repo.GetPartnerOrg(ctx, row.PartnerID, row.Mode, row.OrgExternalID)
	if err != nil || po == nil || po.OrganizationID == nil {
		s.connectRetry(ctx, row, connectFailedMsg)
		return
	}
	if po.State.Paused() {
		// Waits for the workspace to resume, spending no attempt.
		next := s.now().Add(30 * time.Minute)
		_ = s.mb.Repo.FinishPartnerConnect(ctx, row.ID, nil, models.PartnerConnectPending, nil, &next)
		return
	}
	p, err := s.Repo.GetPartner(ctx, row.PartnerID)
	if err != nil || p == nil {
		s.connectRetry(ctx, row, connectFailedMsg)
		return
	}
	if p.Status != models.PartnerStatusActive {
		// A suspended partner connects nothing; the mailbox waits, spending no attempt.
		next := s.now().Add(30 * time.Minute)
		_ = s.mb.Repo.FinishPartnerConnect(ctx, row.ID, nil, models.PartnerConnectPending, nil, &next)
		return
	}
	provider := grantProvider(row.Provider)
	orgID := *po.OrganizationID
	name := s.senderName(row.FirstName, row.LastName, row.Email)
	method := ""
	if row.ConnectionMethod != nil {
		method = *row.ConnectionMethod
	}
	var acc *models.Email
	var xerr *errx.Error
	switch {
	case method == "token":
		acc, xerr = s.mb.Delegator.ConnectPurchased(ctx, orgID, p.ServiceUserID, provider, row.Email, name, "", true)
	case infrastructureMicrosoft(row.Source, row.Provider, method) && !delegationVouched(p, row.Email):
		// Partner input alone never records a grant: the tenant's administrator consenting through a link does.
		var pending bool
		if pending, xerr = s.consentPending(ctx, p, orgID, row); xerr == nil && pending {
			s.awaitConsent(ctx, row)
			return
		}
		if xerr == nil {
			acc, xerr = s.mb.Delegator.ConnectPurchased(ctx, orgID, p.ServiceUserID, provider, row.Email, name, "", false)
		}
	default:
		acc, xerr = s.mb.Delegator.ConnectPartnerDelegated(ctx, orgID, p.ServiceUserID, provider, row.Email, name, delegationVouched(p, row.Email))
	}
	if xerr != nil && xerr.Identifier == delegation.ErrIDMicrosoftTenantConsent {
		s.awaitConsent(ctx, row)
		return
	}
	if xerr != nil {
		log.Warn().Str("partner_mailbox", row.ID.String()).Str("reason", xerr.ResponseCode()).Msg("partner: connecting an attached mailbox failed")
		msg := connectFailedMsg
		if xerr.Code != errx.Internal && xerr.Code != errx.ServiceUnavailable && xerr.Message != "" {
			msg = xerr.Message
		}
		s.connectRetry(ctx, row, msg)
		return
	}
	if err := s.mb.Repo.FinishPartnerConnect(ctx, row.ID, &acc.ID, models.PartnerConnectConnected, nil, nil); err != nil {
		errs.CaptureException(err)
		return
	}
	s.syncWarmup(ctx, orgID, acc.ID, row.Enablement)
}

// consentRetry is how often a mailbox waiting on its tenant's consent checks again; the consent itself wakes it at once.
const consentRetry = 30 * time.Minute

// awaitConsent parks a Microsoft 365 mailbox until its tenant's administrator consents, spending no attempt.
func (s *Service) awaitConsent(ctx context.Context, row *models.PartnerMailbox) {
	if err := s.mb.Repo.AwaitPartnerAction(ctx, row.ID, models.ActionMicrosoftConsent, delegation.TenantConsentMessage(emailDomain(row.Email)), s.now().Add(consentRetry)); err != nil {
		errs.CaptureException(err)
	}
}

// connectRetry backs a connect off, and gives up after connectMaxAttempts with the reason the partner reads.
func (s *Service) connectRetry(ctx context.Context, row *models.PartnerMailbox, msg string) {
	if row.ConnectAttempts >= connectMaxAttempts {
		_ = s.mb.Repo.FinishPartnerConnect(ctx, row.ID, nil, models.PartnerConnectFailed, &msg, nil)
		return
	}
	next := s.now().Add(min(30*time.Minute, time.Duration(1<<min(row.ConnectAttempts, 6))*30*time.Second))
	_ = s.mb.Repo.FinishPartnerConnect(ctx, row.ID, nil, models.PartnerConnectPending, &msg, &next)
}

// WakeMicrosoftConsent runs a workspace's connects waiting on these domains' tenant consent on the next pass.
func (s *Service) WakeMicrosoftConsent(ctx context.Context, orgID uuid.UUID, domains []string) {
	if !s.mailboxesOn() {
		return
	}
	if err := s.mb.Repo.WakePartnerAction(ctx, orgID, models.ActionMicrosoftConsent, domains); err != nil {
		errs.CaptureException(err)
	}
}
