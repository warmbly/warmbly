package poollink

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type managedOpsRepo struct {
	repository.PoolLinkRepository
	mu           sync.Mutex
	instance     *models.PoolLinkInstance
	op           *models.PoolLinkManagedOperation
	mailbox      *models.PoolLinkMailbox
	failComplete bool
}

func (r *managedOpsRepo) ManagedInstanceAuthorized(_ context.Context, instance, org uuid.UUID) (bool, error) {
	return instance == r.instance.ID && org == r.instance.OrganizationID && r.instance.RevokedAt == nil, nil
}

func (r *managedOpsRepo) WithManagedOperationLock(_ context.Context, _ uuid.UUID, fn func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fn()
}
func (r *managedOpsRepo) GetInstance(_ context.Context, id uuid.UUID) (*models.PoolLinkInstance, error) {
	if id == r.instance.ID {
		return r.instance, nil
	}
	return nil, nil
}
func (r *managedOpsRepo) CreateManagedOperation(_ context.Context, op *models.PoolLinkManagedOperation) error {
	if r.op != nil {
		if *r.op.RemoteID != *op.RemoteID || r.op.Provider != op.Provider || r.op.Kind != op.Kind {
			return errors.New("identity conflict")
		}
		return nil
	}
	copy := *op
	copy.ID = uuid.New()
	copy.State = "pending"
	copy.ActivationPending = true
	r.op = &copy
	return nil
}
func (r *managedOpsRepo) GetManagedOperation(_ context.Context, instance, remote uuid.UUID) (*models.PoolLinkManagedOperation, error) {
	if r.op != nil && *r.op.InstanceID == instance && *r.op.RemoteID == remote {
		copy := *r.op
		return &copy, nil
	}
	return nil, nil
}
func (r *managedOpsRepo) ClaimManagedOperation(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	if r.op.State != "pending" || !time.Now().Before(r.op.ExpiresAt) {
		return false, nil
	}
	r.op.State = "exchanging"
	return true, nil
}
func (r *managedOpsRepo) CompleteManagedOperation(_ context.Context, instance, remote, account uuid.UUID) error {
	if r.failComplete {
		return errors.New("database unavailable")
	}
	if r.op.State == "revoked" || (r.op.State != "completed" && !time.Now().Before(r.op.ExpiresAt)) {
		return errors.New("authority lost")
	}
	r.op.State = "completed"
	r.op.AccountID = &account
	if r.op.CompletedAt == nil {
		now := time.Now()
		r.op.CompletedAt = &now
	}
	r.mailbox = &models.PoolLinkMailbox{InstanceID: instance, RemoteID: remote, EmailAccountID: account, Managed: true}
	return nil
}
func (r *managedOpsRepo) CompleteManagedActivation(context.Context, uuid.UUID, uuid.UUID) error {
	r.op.ActivationPending = false
	return nil
}
func (r *managedOpsRepo) RevokeManagedOperations(context.Context, uuid.UUID, *uuid.UUID) error {
	if r.op != nil {
		r.op.State = "revoked"
	}
	return nil
}
func (r *managedOpsRepo) GetMailboxByRemote(_ context.Context, instance, remote uuid.UUID) (*models.PoolLinkMailbox, error) {
	if r.mailbox != nil && r.mailbox.InstanceID == instance && r.mailbox.RemoteID == remote {
		return r.mailbox, nil
	}
	return nil, nil
}
func (r *managedOpsRepo) DeleteMailbox(context.Context, uuid.UUID, uuid.UUID) error {
	r.mailbox = nil
	return nil
}

type managedOpsEmails struct {
	repository.EmailRepository
	accounts map[uuid.UUID]*models.Email
}

func (r *managedOpsEmails) GetByID(_ context.Context, id uuid.UUID) (*models.Email, *errx.Error) {
	if a := r.accounts[id]; a != nil {
		return a, nil
	}
	return nil, errx.ErrNotFound
}
func (r *managedOpsEmails) CountForOrganization(context.Context, uuid.UUID) (int, *errx.Error) {
	return len(r.accounts), nil
}
func (*managedOpsEmails) CountWarmingForOrganization(context.Context, uuid.UUID) (int, *errx.Error) {
	return 0, nil
}

type managedOpsEmailService struct {
	email.EmailService
	emails                   *managedOpsEmails
	exchanges, starts, loads int
	unknown                  bool
	failStart                bool
}

func (*managedOpsEmailService) OAuthCallbackOrigin(models.InboxProvider) (string, *errx.Error) {
	return "https://cloud.test", nil
}
func (*managedOpsEmailService) OAuthAuthorizeURL(_ models.InboxProvider, state, _ string) (string, *errx.Error) {
	return "https://provider.test/?state=" + state, nil
}
func (e *managedOpsEmailService) OAuthConnectWithCodeForAccount(_ context.Context, _ string, org *uuid.UUID, provider models.InboxProvider, _, _ string, id uuid.UUID) (*models.Email, *errx.Error) {
	e.exchanges++
	if e.unknown {
		return nil, errx.InternalError()
	}
	a := &models.Email{ID: id, OrganizationID: org, Status: "active", Provider: string(provider), AuthMethod: "oauth", Email: "fixture@test.local"}
	e.emails.accounts[id] = a
	return a, nil
}
func (e *managedOpsEmailService) SetWarmupLifecycle(_ context.Context, _, id, action string) (*models.Email, *errx.Error) {
	if e.failStart {
		return nil, errx.InternalError()
	}
	if action == "start" {
		e.starts++
	}
	return e.emails.accounts[uuid.MustParse(id)], nil
}
func (e *managedOpsEmailService) LoadAccountOntoWorker(context.Context, uuid.UUID) error {
	e.loads++
	return nil
}
