package poollink

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

var ErrOAuthUnknown = errx.NewWithIdentifier(errx.Conflict, "pool_link_oauth_unknown", "The provider exchange has no confirmed result. Do not retry the authorization code; start a new sign-in after removing this pending connection.")

type managedOperationKey struct{}

func managedOperationLocked(ctx context.Context) bool { return ctx.Value(managedOperationKey{}) != nil }

func (s *service) withManagedOperationLock(ctx context.Context, instanceID uuid.UUID, fn func(context.Context) *errx.Error) *errx.Error {
	r, ok := s.repo.(repository.PoolLinkManagedRepository)
	if !ok {
		return errx.InternalError()
	}
	err := r.WithManagedOperationLock(ctx, instanceID, func() error {
		xerr := fn(context.WithValue(ctx, managedOperationKey{}, true))
		if xerr != nil {
			return xerr
		}
		return nil
	})
	if err == nil {
		return nil
	}
	var xerr *errx.Error
	if errors.As(err, &xerr) {
		return xerr
	}
	return errx.InternalError()
}

func (s *service) currentManagedInstance(ctx context.Context, inst *models.PoolLinkInstance) *errx.Error {
	current, err := s.repo.GetInstance(ctx, inst.ID)
	if err != nil {
		return errx.InternalError()
	}
	if current == nil || current.OrganizationID != inst.OrganizationID || current.RevokedAt != nil {
		return ErrInstanceRevoked
	}
	allowed, err := s.repo.(repository.PoolLinkManagedRepository).ManagedInstanceAuthorized(ctx, inst.ID, inst.OrganizationID)
	if err != nil {
		return errx.InternalError()
	}
	if !allowed {
		return ErrInstanceRevoked
	}
	return nil
}

func (s *service) managedCapacity(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID) *errx.Error {
	m, err := s.repo.GetMailboxByRemote(ctx, inst.ID, remoteID)
	if err != nil {
		return errx.InternalError()
	}
	if m != nil {
		return nil
	}
	plan, xerr := s.Plan(ctx, inst.OrganizationID)
	if xerr != nil {
		return xerr
	}
	if plan.MailboxLimit != nil && plan.Enrolled >= *plan.MailboxLimit {
		return ErrMailboxLimit
	}
	return nil
}

func (s *service) FinishManagedOAuth(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkOAuthFinishRequest) (*models.PoolLinkMailboxState, *errx.Error) {
	if req.Protocol != models.ManagedConsentProtocol || req.RemoteID == uuid.Nil {
		return nil, ErrBadRequest
	}
	if !managedOperationLocked(ctx) {
		var out *models.PoolLinkMailboxState
		xerr := s.withManagedOperationLock(ctx, inst.ID, func(ctx context.Context) *errx.Error {
			var xerr *errx.Error
			out, xerr = s.FinishManagedOAuth(ctx, inst, req)
			return xerr
		})
		return out, xerr
	}
	if xerr := s.currentManagedInstance(ctx, inst); xerr != nil {
		return nil, xerr
	}
	r := s.repo.(repository.PoolLinkManagedRepository)
	op, err := r.GetManagedOperation(ctx, inst.ID, req.RemoteID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if op == nil || op.OrganizationID != inst.OrganizationID {
		return nil, ErrOAuthSession
	}
	if op.State == "pending" && time.Now().Before(op.ExpiresAt) {
		return nil, ErrOAuthPending
	}
	if op.State == "exchanging" && time.Now().Before(op.ExpiresAt) && op.PlannedAccountID != nil {
		acc, xerr := s.emails.GetByID(ctx, *op.PlannedAccountID)
		if xerr != nil && xerr != errx.ErrNotFound {
			return nil, xerr
		}
		if acc == nil {
			return nil, ErrOAuthUnknown
		}
		if acc.OrganizationID == nil || *acc.OrganizationID != inst.OrganizationID || acc.Provider != string(op.Provider) || acc.Status != "active" {
			return nil, ErrOAuthUnknown
		}
		if err := r.CompleteManagedOperation(ctx, inst.ID, req.RemoteID, acc.ID); err != nil {
			return nil, errx.InternalError()
		}
		op, err = r.GetManagedOperation(ctx, inst.ID, req.RemoteID)
		if err != nil {
			return nil, errx.InternalError()
		}
	}
	if op.State != "completed" || op.CompletedAt == nil || op.AccountID == nil {
		return nil, ErrOAuthSession
	}
	if op.ActivationPending {
		if _, xerr := s.emailSvc.SetWarmupLifecycle(ctx, inst.OrganizationID.String(), op.AccountID.String(), "start"); xerr != nil {
			return nil, xerr
		}
		if err := r.CompleteManagedActivation(ctx, inst.ID, req.RemoteID); err != nil {
			return nil, errx.InternalError()
		}
		_ = s.emailSvc.LoadAccountOntoWorker(ctx, *op.AccountID)
		if s.scheduler != nil {
			_ = s.scheduler.EnsureWarmupScheduled(ctx, *op.AccountID)
		}
	}
	out, xerr := s.GetMailbox(ctx, inst, req.RemoteID)
	if xerr != nil {
		return nil, xerr
	}
	if !out.Managed || out.Status != "active" || op.AccountID == nil || out.EmailAccountID != *op.AccountID {
		return nil, ErrMailboxInactive
	}
	out.ConsentCompletedAt = op.CompletedAt
	return out, nil
}

func (s *service) managedAuthority(ctx context.Context, inst *models.PoolLinkInstance, remoteID uuid.UUID) (*models.PoolLinkManagedOperation, *errx.Error) {
	if xerr := s.currentManagedInstance(ctx, inst); xerr != nil {
		return nil, xerr
	}
	r := s.repo.(repository.PoolLinkManagedRepository)
	op, err := r.GetManagedOperation(ctx, inst.ID, remoteID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if op != nil && op.State != "completed" {
		return nil, ErrOAuthSession
	}
	return op, nil
}

func (s *service) adoptManaged(ctx context.Context, inst *models.PoolLinkInstance, req models.PoolLinkAdoptRequest) (*models.PoolLinkMailboxState, *errx.Error) {
	if !managedOperationLocked(ctx) {
		var out *models.PoolLinkMailboxState
		xerr := s.withManagedOperationLock(ctx, inst.ID, func(ctx context.Context) *errx.Error {
			var xerr *errx.Error
			out, xerr = s.adoptManaged(ctx, inst, req)
			return xerr
		})
		return out, xerr
	}
	if xerr := s.currentManagedInstance(ctx, inst); xerr != nil {
		return nil, xerr
	}
	acc, xerr := s.emails.GetByID(ctx, req.EmailAccountID)
	if xerr != nil {
		return nil, xerr
	}
	if acc == nil || acc.OrganizationID == nil || *acc.OrganizationID != inst.OrganizationID || acc.Status != "active" || acc.AuthMethod == models.MailAuthDelegated ||
		(acc.Provider != string(models.InboxProviderGoogle) && acc.Provider != string(models.InboxProviderOutlook)) {
		return nil, ErrNotAdoptable
	}
	r := s.repo.(repository.PoolLinkManagedRepository)
	op := &models.PoolLinkManagedOperation{OrganizationID: inst.OrganizationID, InstanceID: &inst.ID, RemoteID: &req.RemoteID,
		Kind: "adopt", Provider: models.InboxProvider(acc.Provider), PlannedAccountID: &acc.ID, ExpiresAt: time.Now().Add(brokerTTL)}
	if err := r.CreateManagedOperation(ctx, op); err != nil {
		return nil, ErrAlreadyAdopted
	}
	existing, err := r.GetManagedOperation(ctx, inst.ID, req.RemoteID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if existing == nil || (existing.State != "pending" && existing.State != "completed") {
		return nil, ErrAlreadyAdopted
	}
	if err := r.CompleteManagedOperation(ctx, inst.ID, req.RemoteID, acc.ID); err != nil {
		return nil, errx.InternalError()
	}
	return s.FinishManagedOAuth(ctx, inst, models.PoolLinkOAuthFinishRequest{Protocol: models.ManagedConsentProtocol, RemoteID: req.RemoteID})
}
