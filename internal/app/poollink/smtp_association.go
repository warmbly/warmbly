package poollink

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// AssociateSMTPIMAP links an explicitly selected Cloud record without transferring either copy's credentials.
func (s *service) AssociateSMTPIMAP(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkSMTPAssociationRequest) (*models.PoolLinkMailboxState, *errx.Error) {
	if req.RemoteID == uuid.Nil || req.EmailAccountID == uuid.Nil || strings.TrimSpace(req.Email) == "" {
		return nil, ErrBadRequest
	}
	if !managedOperationLocked(ctx) {
		var state *models.PoolLinkMailboxState
		xerr := s.withManagedOperationLock(ctx, inst.ID, func(ctx context.Context) *errx.Error {
			var xerr *errx.Error
			state, xerr = s.AssociateSMTPIMAP(ctx, inst, req)
			return xerr
		})
		return state, xerr
	}
	if _, xerr := s.managedAuthority(ctx, inst, req.RemoteID); xerr != nil {
		return nil, xerr
	}
	acc, xerr := s.emails.Get(ctx, inst.OrganizationID.String(), req.EmailAccountID.String())
	if xerr == errx.ErrNotFound {
		return nil, ErrMailboxConflict
	}
	if xerr != nil {
		return nil, xerr
	}
	if acc == nil || acc.OrganizationID == nil || *acc.OrganizationID != inst.OrganizationID || acc.Status != "active" ||
		acc.Provider != string(models.InboxProviderSMTPIMAP) || !strings.EqualFold(acc.Email, strings.TrimSpace(req.Email)) {
		return nil, ErrMailboxConflict
	}
	m, err := s.repo.GetMailboxByRemote(ctx, inst.ID, req.RemoteID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if m != nil {
		if m.EmailAccountID != acc.ID {
			return nil, ErrMailboxConflict
		}
		if !m.Managed {
			if err := s.repo.RetainMailbox(ctx, inst.ID, req.RemoteID, acc.ID); err != nil {
				return nil, errx.InternalError()
			}
		}
		return s.GetMailbox(ctx, inst, req.RemoteID)
	}
	if existing, err := s.repo.GetMailboxByAccount(ctx, acc.ID); err != nil {
		return nil, errx.InternalError()
	} else if existing != nil || s.campaigns == nil {
		return nil, ErrMailboxConflict
	}
	if busy, err := s.campaigns.AccountHasActiveCampaign(ctx, acc.ID); err != nil {
		return nil, errx.InternalError()
	} else if busy {
		return nil, ErrMailboxConflict
	}
	if xerr := s.newMailboxAdmitted(ctx, inst.OrganizationID); xerr != nil {
		return nil, xerr
	}
	if err := s.repo.EnrollMailbox(ctx, &models.PoolLinkMailbox{InstanceID: inst.ID, RemoteID: req.RemoteID, EmailAccountID: acc.ID, Managed: true}); err != nil {
		return nil, enrollError(err)
	}
	return s.GetMailbox(ctx, inst, req.RemoteID)
}
