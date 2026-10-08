package poollink

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type adoptLinkRepo struct {
	repository.PoolLinkRepository
	enrolled *models.PoolLinkMailbox
}

func (r *adoptLinkRepo) GetMailboxByAccount(context.Context, uuid.UUID) (*models.PoolLinkMailbox, error) {
	return nil, nil
}

func (r *adoptLinkRepo) EnrollMailbox(_ context.Context, m *models.PoolLinkMailbox) error {
	r.enrolled = m
	return nil
}

func (r *adoptLinkRepo) GetMailboxByRemote(context.Context, uuid.UUID, uuid.UUID) (*models.PoolLinkMailbox, error) {
	return r.enrolled, nil
}

type adoptAccounts struct {
	repository.EmailRepository
	acc *models.Email
}

type existingConnectionAccounts struct {
	managedOpsEmails
	createdOrg uuid.UUID
}

func (r *existingConnectionAccounts) NewSMTPIMAPAccount(_ context.Context, _ string, req models.NewSMTPIMAPAccount) (*models.Email, *errx.Error) {
	r.createdOrg = *req.OrganizationID
	a := &models.Email{ID: uuid.New(), OrganizationID: req.OrganizationID, Email: req.Email, Provider: "smtp_imap", Status: "active"}
	r.accounts[a.ID] = a
	return a, nil
}

type freeConnectionGate struct{ feature.FeatureGateService }

func (freeConnectionGate) HasPremiumWarmup(context.Context, uuid.UUID) (bool, *errx.Error) {
	return false, nil
}

func TestExistingServerConnectionEnrollsWithinItsCloudWorkspaceAllowance(t *testing.T) {
	t.Setenv("BILLING_PROVIDER", "stripe")
	org, owner := uuid.New(), uuid.New()
	r := &adoptLinkRepo{}
	accounts := &existingConnectionAccounts{managedOpsEmails: managedOpsEmails{accounts: map[uuid.UUID]*models.Email{}}}
	emails := &adoptEmails{org: org}
	s := &service{repo: r, emails: accounts, emailSvc: emails, gate: freeConnectionGate{}}
	inst := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: org, CreatedBy: &owner}
	tls := &models.Service{Host: "mail.example.test", Port: 993, Security: models.MailSecurityTLS}
	req := models.PoolLinkEnrollRequest{RemoteID: uuid.New(), Email: "sender@example.test", Provider: models.InboxProviderSMTPIMAP, SMTPIMAP: &models.SmtpImap{SMTP: tls, IMAP: tls}}
	got, xerr := s.Enroll(context.Background(), inst, req)
	if xerr != nil || got == nil || r.enrolled.InstanceID != inst.ID || accounts.createdOrg != org || !emails.started {
		t.Fatalf("enrollment on existing connection = %+v, %v", got, xerr)
	}
	for len(accounts.accounts) < models.FreeWorkspaceMailboxLimit {
		accounts.accounts[uuid.New()] = &models.Email{}
	}
	r.enrolled = nil
	req.RemoteID = uuid.New()
	if _, xerr := s.Enroll(context.Background(), inst, req); xerr != ErrMailboxLimit {
		t.Fatalf("existing connection bypassed allowance: %v", xerr)
	}
}

func (r adoptAccounts) GetByID(context.Context, uuid.UUID) (*models.Email, *errx.Error) {
	return r.acc, nil
}

// adoptEmails scopes the lifecycle write by workspace, as the repository does.
type adoptEmails struct {
	email.EmailService
	org     uuid.UUID
	started bool
}

func (e *adoptEmails) SetWarmupLifecycle(_ context.Context, orgID, _, action string) (*models.Email, *errx.Error) {
	if orgID != e.org.String() {
		return nil, errx.ErrNotFound
	}
	e.started = action == "start"
	return &models.Email{}, nil
}

func (e *adoptEmails) LoadAccountOntoWorker(context.Context, uuid.UUID) error { return nil }

type conflictApprovalRepo struct{ repository.PoolLinkRepository }

func (conflictApprovalRepo) GetMailboxByRemote(context.Context, uuid.UUID, uuid.UUID) (*models.PoolLinkMailbox, error) {
	return nil, nil
}

func (conflictApprovalRepo) GetCodeByUserCode(context.Context, string) (*models.PoolLinkCode, error) {
	org := uuid.New()
	return &models.PoolLinkCode{UserCode: "ABCD-EFGH", Status: models.PoolLinkCodePending, RemoteOrganizationID: &org}, nil
}
func (conflictApprovalRepo) CreateInstance(context.Context, *models.PoolLinkInstance, string) error {
	return repository.ErrCloudWorkspaceLinked
}

func TestApprovalExplainsThatCloudWorkspaceIsAlreadyConnected(t *testing.T) {
	s := &service{repo: conflictApprovalRepo{}}
	if _, xerr := s.ApproveCode(context.Background(), "ABCD-EFGH", uuid.New(), uuid.New()); xerr != ErrWorkspaceLinked {
		t.Fatalf("duplicate approval = %v", xerr)
	}
}

func TestNewConnectionsStillRequireWorkspaceScope(t *testing.T) {
	s := &service{repo: conflictApprovalRepo{}}
	if _, xerr := s.StartCode(context.Background(), models.PoolLinkStartRequest{InstanceName: "Legacy"}); xerr != ErrLegacyLink {
		t.Fatalf("legacy device handshake = %v", xerr)
	}
}

// Adopting a workspace mailbox into a linked instance must start its warmup on the cloud.
func TestExistingServerConnectionAdoptsAndStartsWarmupInItsCloudWorkspace(t *testing.T) {
	org, owner := uuid.New(), uuid.New()
	acc := &models.Email{ID: uuid.New(), OrganizationID: &org, Status: "active", Provider: string(models.InboxProviderGoogle)}
	emails := &adoptEmails{org: org}
	s := &service{repo: &adoptLinkRepo{}, emails: adoptAccounts{acc: acc}, emailSvc: emails}
	inst := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: org, CreatedBy: &owner}

	if _, xerr := s.Adopt(context.Background(), inst, models.PoolLinkAdoptRequest{RemoteID: uuid.New(), EmailAccountID: acc.ID}); xerr != nil {
		t.Fatalf("Adopt: %v", xerr)
	}
	if !emails.started {
		t.Fatal("warmup was not started in the workspace; the mailbox would sit enrolled and never warm")
	}
	other := uuid.New()
	acc.OrganizationID = &other
	if _, xerr := s.Adopt(context.Background(), inst, models.PoolLinkAdoptRequest{RemoteID: uuid.New(), EmailAccountID: acc.ID}); xerr != ErrNotAdoptable {
		t.Fatalf("another workspace's mailbox was accepted: %v", xerr)
	}
}
