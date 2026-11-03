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
	createdOrg      uuid.UUID
	disableBehavior bool
}

func (r *existingConnectionAccounts) NewSMTPIMAPAccount(_ context.Context, _ string, req models.NewSMTPIMAPAccount) (*models.Email, *errx.Error) {
	r.createdOrg = *req.OrganizationID
	r.disableBehavior = req.DisableSendingBehavior
	a := &models.Email{ID: uuid.New(), OrganizationID: req.OrganizationID, Email: req.Email, Provider: "smtp_imap", Status: "active"}
	r.accounts[a.ID] = a
	return a, nil
}

func (r *existingConnectionAccounts) NewOauthAccount(_ context.Context, _ string, req models.NewOauthAccount) (*models.Email, *errx.Error) {
	r.createdOrg = *req.OrganizationID
	r.disableBehavior = req.DisableSendingBehavior
	a := &models.Email{ID: uuid.New(), OrganizationID: req.OrganizationID, Email: req.Email, Provider: string(req.Provider), Status: "active"}
	r.accounts[a.ID] = a
	return a, nil
}

func TestGatewayOAuthEnrollmentDoesNotInheritCloudWorkday(t *testing.T) {
	for _, provider := range []models.InboxProvider{models.InboxProviderGoogle, models.InboxProviderOutlook} {
		t.Run(string(provider), func(t *testing.T) {
			org, owner := uuid.New(), uuid.New()
			r := &adoptLinkRepo{}
			accounts := &existingConnectionAccounts{managedOpsEmails: managedOpsEmails{accounts: map[uuid.UUID]*models.Email{}}}
			s := &service{repo: r, emails: accounts, emailSvc: &adoptEmails{org: org}}
			inst := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: org, CreatedBy: &owner}
			req := models.PoolLinkEnrollRequest{RemoteID: uuid.New(), Email: "sender@example.test", Provider: provider, OAuth: &models.PoolLinkOAuthCredential{RefreshToken: "synthetic"}}
			if _, xerr := s.Enroll(context.Background(), inst, req); xerr != nil {
				t.Fatal(xerr)
			}
			if !accounts.disableBehavior || accounts.createdOrg != org {
				t.Fatal("gateway OAuth enrollment inherited Cloud's weekday workday")
			}
		})
	}
}

type freeConnectionGate struct{ feature.FeatureGateService }

type conflictingEnrollmentAccounts struct {
	existingConnectionAccounts
	xerr *errx.Error
}

func (r *conflictingEnrollmentAccounts) NewSMTPIMAPAccount(context.Context, string, models.NewSMTPIMAPAccount) (*models.Email, *errx.Error) {
	return nil, r.xerr
}

func (r *conflictingEnrollmentAccounts) NewOauthAccount(context.Context, string, models.NewOauthAccount) (*models.Email, *errx.Error) {
	return nil, r.xerr
}

// FindInOrganization finds no adoptable copy, so the conflict stands.
func (r *conflictingEnrollmentAccounts) FindInOrganization(context.Context, uuid.UUID, string) (*models.EmailRef, *errx.Error) {
	return nil, nil
}

func TestEnrollmentExplainsAnExistingUnassociatedCloudMailbox(t *testing.T) {
	t.Setenv("BILLING_PROVIDER", "none")
	for _, provider := range []models.InboxProvider{models.InboxProviderSMTPIMAP, models.InboxProviderGoogle, models.InboxProviderOutlook} {
		t.Run(string(provider), func(t *testing.T) {
			for _, failure := range []*errx.Error{errx.ErrEmailOnboardAlreadyExists, errx.InternalError()} {
				org, owner := uuid.New(), uuid.New()
				r := &adoptLinkRepo{}
				accounts := &conflictingEnrollmentAccounts{xerr: failure}
				emails := &adoptEmails{org: org}
				s := &service{repo: r, emails: accounts, emailSvc: emails}
				inst := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: org, CreatedBy: &owner}
				tls := &models.Service{Host: "mail.example.test", Port: 993, Security: models.MailSecurityTLS}
				req := models.PoolLinkEnrollRequest{RemoteID: uuid.New(), Email: "sender@example.test", Provider: provider,
					OAuth: &models.PoolLinkOAuthCredential{RefreshToken: "synthetic"}, SMTPIMAP: &models.SmtpImap{SMTP: tls, IMAP: tls}}
				got, xerr := s.Enroll(context.Background(), inst, req)
				want := failure
				if failure == errx.ErrEmailOnboardAlreadyExists {
					want = ErrMailboxConflict
				}
				if got != nil || xerr != want || r.enrolled != nil || emails.started {
					t.Fatalf("conflict must not attach or start an existing mailbox: %+v, %v", got, xerr)
				}
			}
		})
	}
}

func (freeConnectionGate) HasPremiumWarmup(context.Context, uuid.UUID) (bool, *errx.Error) {
	return false, nil
}

type paidConnectionGate struct{ feature.FeatureGateService }

func (paidConnectionGate) HasPremiumWarmup(context.Context, uuid.UUID) (bool, *errx.Error) {
	return true, nil
}

// A free workspace keeps what it enrolled but its connection adds no new mailbox; a paid one does.
func TestExistingServerConnectionEnrollsOnlyOnAPaidCloudWorkspace(t *testing.T) {
	t.Setenv("BILLING_PROVIDER", "stripe")
	for _, tc := range []struct {
		name string
		gate feature.FeatureGateService
		want *errx.Error
	}{{"free", freeConnectionGate{}, ErrMailboxSubscription}, {"paid", paidConnectionGate{}, nil}} {
		t.Run(tc.name, func(t *testing.T) {
			org, owner := uuid.New(), uuid.New()
			r := &adoptLinkRepo{}
			accounts := &existingConnectionAccounts{managedOpsEmails: managedOpsEmails{accounts: map[uuid.UUID]*models.Email{}}}
			emails := &adoptEmails{org: org}
			s := &service{repo: r, emails: accounts, emailSvc: emails, gate: tc.gate}
			inst := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: org, CreatedBy: &owner}
			tls := &models.Service{Host: "mail.example.test", Port: 993, Security: models.MailSecurityTLS}
			req := models.PoolLinkEnrollRequest{RemoteID: uuid.New(), Email: "[EMAIL]", Provider: models.InboxProviderSMTPIMAP, SMTPIMAP: &models.SmtpImap{SMTP: tls, IMAP: tls}}
			got, xerr := s.Enroll(context.Background(), inst, req)
			if xerr != tc.want {
				t.Fatalf("enrollment on existing connection = %+v, %v; want %v", got, xerr, tc.want)
			}
			if tc.want != nil {
				if got != nil || r.enrolled != nil || accounts.createdOrg != uuid.Nil || emails.started {
					t.Fatalf("refused enrollment still created state: %+v", got)
				}
				return
			}
			if got == nil || r.enrolled.InstanceID != inst.ID || accounts.createdOrg != org || !emails.started {
				t.Fatalf("enrollment on existing connection = %+v", got)
			}
			if !accounts.disableBehavior {
				t.Fatal("gateway warmup inherited Cloud's weekday workday")
			}
		})
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

// No plan links a new instance: the handshake is refused at both ends, even for a code issued earlier.
func TestNewLinksAreClosed(t *testing.T) {
	t.Setenv("BILLING_PROVIDER", "stripe")
	s := &service{gate: paidConnectionGate{}}
	if _, xerr := s.StartCode(context.Background(), models.PoolLinkStartRequest{InstanceName: "Self-hosted", RemoteOrganizationID: uuid.New()}); xerr != ErrLinkClosed {
		t.Fatalf("start = %v", xerr)
	}
	if _, xerr := s.ApproveCode(context.Background(), "ABCD-EFGH", uuid.New(), uuid.New()); xerr != ErrLinkClosed {
		t.Fatalf("approval on a paid workspace = %v", xerr)
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

// A free workspace linked earlier keeps re-signing in what it holds, and adds nothing new.
func TestFreeLinkedWorkspaceRenewsHeldMailboxesOnly(t *testing.T) {
	t.Setenv("BILLING_PROVIDER", "stripe")
	inst := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: uuid.New()}
	held := &service{repo: &adoptLinkRepo{enrolled: &models.PoolLinkMailbox{InstanceID: inst.ID, RemoteID: uuid.New()}}, gate: freeConnectionGate{}}
	if xerr := held.managedCapacity(context.Background(), inst, uuid.New()); xerr != nil {
		t.Fatalf("held mailbox refused: %v", xerr)
	}
	fresh := &service{repo: &adoptLinkRepo{}, gate: freeConnectionGate{}}
	if xerr := fresh.managedCapacity(context.Background(), inst, uuid.New()); xerr != ErrMailboxSubscription {
		t.Fatalf("new mailbox = %v", xerr)
	}
	paid := &service{repo: &adoptLinkRepo{}, gate: paidConnectionGate{}}
	if xerr := paid.managedCapacity(context.Background(), inst, uuid.New()); xerr != nil {
		t.Fatalf("paid workspace refused: %v", xerr)
	}
}
