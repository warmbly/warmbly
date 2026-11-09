package poollink

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type conversionRepo struct {
	repository.PoolLinkRepository
	mailbox   *models.PoolLinkMailbox
	converted bool
}

func (r *conversionRepo) GetMailboxByRemote(_ context.Context, instance, remote uuid.UUID) (*models.PoolLinkMailbox, error) {
	if r.mailbox != nil && r.mailbox.InstanceID == instance && r.mailbox.RemoteID == remote {
		m := *r.mailbox
		return &m, nil
	}
	return nil, nil
}

func (r *conversionRepo) ConvertToCloud(_ context.Context, instance, remote uuid.UUID) error {
	r.converted = true
	r.mailbox.Executor = models.PoolLinkExecutorCloud
	return nil
}

type conversionAccounts struct {
	managedOpsEmails
	restored *models.SmtpImap
}

func (r *conversionAccounts) RestoreMailboxCredential(_ context.Context, _ uuid.UUID, _ *models.PoolLinkOAuthCredential, creds *models.SmtpImap) error {
	r.restored = creds
	return nil
}

type conversionScheduler struct{ scheduled []uuid.UUID }

func (s *conversionScheduler) EnsureWarmupScheduled(_ context.Context, id uuid.UUID) error {
	s.scheduled = append(s.scheduled, id)
	return nil
}

func newConversion(t *testing.T) (*service, *conversionRepo, *conversionAccounts, *conversionScheduler, *models.PoolLinkInstance) {
	t.Helper()
	org, owner := uuid.New(), uuid.New()
	inst := &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: org, CreatedBy: &owner}
	acc := &models.Email{ID: uuid.New(), OrganizationID: &org, Email: "kept@example.test", Provider: string(models.InboxProviderSMTPIMAP), Status: "active"}
	repo := &conversionRepo{mailbox: &models.PoolLinkMailbox{InstanceID: inst.ID, RemoteID: uuid.New(), EmailAccountID: acc.ID, Executor: models.PoolLinkExecutorInstance}}
	accounts := &conversionAccounts{managedOpsEmails: managedOpsEmails{accounts: map[uuid.UUID]*models.Email{acc.ID: acc}}}
	sched := &conversionScheduler{}
	s := &service{repo: repo, emails: accounts, emailSvc: &adoptEmails{org: org}, restorer: accounts, scheduler: sched}
	return s, repo, accounts, sched, inst
}

func conversionCredential() *models.SmtpImap {
	return &models.SmtpImap{
		SMTP: &models.Service{Host: "smtp.example.test", Port: 465, Username: "kept@example.test", Password: "synthetic", Security: models.MailSecurityTLS},
		IMAP: &models.Service{Host: "imap.example.test", Port: 993, Username: "kept@example.test", Password: "synthetic", Security: models.MailSecurityTLS},
	}
}

// A mailbox whose credential stayed on its instance returns to Cloud once the instance enrolls it with one.
func TestEnrollWithACredentialHandsALegacyInstanceMailboxToCloud(t *testing.T) {
	for _, executor := range []models.PoolLinkExecutor{"", models.PoolLinkExecutorCloud} {
		t.Run("executor="+string(executor), func(t *testing.T) {
			s, repo, accounts, sched, inst := newConversion(t)
			req := models.PoolLinkEnrollRequest{RemoteID: repo.mailbox.RemoteID, Email: "kept@example.test", Provider: models.InboxProviderSMTPIMAP, SMTPIMAP: conversionCredential(), Executor: executor}
			got, xerr := s.Enroll(context.Background(), inst, req)
			if xerr != nil {
				t.Fatalf("enroll: %v", xerr)
			}
			if !repo.converted || accounts.restored == nil || got.Executor != models.PoolLinkExecutorCloud {
				t.Fatalf("converted %v, restored %v, executor %q", repo.converted, accounts.restored != nil, got.Executor)
			}
			if len(sched.scheduled) != 1 || sched.scheduled[0] != repo.mailbox.EmailAccountID {
				t.Fatalf("warmup chain not restarted: %v", sched.scheduled)
			}
		})
	}
}

func TestEnrollRefusesInstanceExecution(t *testing.T) {
	s, repo, accounts, _, inst := newConversion(t)
	for name, req := range map[string]models.PoolLinkEnrollRequest{
		"existing mailbox": {RemoteID: repo.mailbox.RemoteID, Email: "kept@example.test", Provider: models.InboxProviderSMTPIMAP, Executor: models.PoolLinkExecutorInstance},
		"new mailbox":      {RemoteID: uuid.New(), Email: "new@example.test", Provider: models.InboxProviderSMTPIMAP, Executor: models.PoolLinkExecutorInstance},
		"unknown executor": {RemoteID: uuid.New(), Email: "new@example.test", Provider: models.InboxProviderSMTPIMAP, Executor: "edge"},
	} {
		if _, xerr := s.Enroll(context.Background(), inst, req); xerr != ErrExecutorInvalid {
			t.Fatalf("%s: err %v, want %v", name, xerr, ErrExecutorInvalid)
		}
	}
	if repo.converted || accounts.restored != nil {
		t.Fatal("a refused enrollment changed the mailbox")
	}
}

// Without a credential the mailbox keeps its executor and stays out of the pool.
func TestEnrollWithoutACredentialLeavesALegacyInstanceMailboxPaused(t *testing.T) {
	s, repo, accounts, sched, inst := newConversion(t)
	got, xerr := s.Enroll(context.Background(), inst, models.PoolLinkEnrollRequest{RemoteID: repo.mailbox.RemoteID, Email: "kept@example.test", Provider: models.InboxProviderSMTPIMAP})
	if xerr != nil {
		t.Fatalf("enroll: %v", xerr)
	}
	if repo.converted || accounts.restored != nil || got.Executor != models.PoolLinkExecutorInstance || len(sched.scheduled) != 0 {
		t.Fatalf("converted %v, restored %v, executor %q, scheduled %v", repo.converted, accounts.restored != nil, got.Executor, sched.scheduled)
	}
}

func TestEnrollRefusesACleartextCredentialBeforeConverting(t *testing.T) {
	s, repo, accounts, _, inst := newConversion(t)
	creds := conversionCredential()
	creds.SMTP.Security = models.MailSecurityNone
	req := models.PoolLinkEnrollRequest{RemoteID: repo.mailbox.RemoteID, Email: "kept@example.test", Provider: models.InboxProviderSMTPIMAP, SMTPIMAP: creds}
	if _, xerr := s.Enroll(context.Background(), inst, req); xerr != ErrCleartextMailbox {
		t.Fatalf("err %v, want %v", xerr, ErrCleartextMailbox)
	}
	if repo.converted || accounts.restored != nil {
		t.Fatal("a refused credential was stored")
	}
}

// Only an SMTP/IMAP credential returns a mailbox to Cloud: an OAuth grant from the instance's own app is one Cloud cannot refresh.
func TestEnrollRefusesAnOAuthHandOver(t *testing.T) {
	oauth := &models.PoolLinkOAuthCredential{RefreshToken: "instance-app-grant"}

	s, repo, accounts, _, inst := newConversion(t)
	mixed := models.PoolLinkEnrollRequest{RemoteID: repo.mailbox.RemoteID, Email: "kept@example.test", Provider: models.InboxProviderSMTPIMAP, SMTPIMAP: conversionCredential(), OAuth: oauth}
	if _, xerr := s.Enroll(context.Background(), inst, mixed); xerr != ErrBadCredential {
		t.Fatalf("smtp mailbox with an OAuth grant too: err %v, want %v", xerr, ErrBadCredential)
	}

	google := models.Email{ID: repo.mailbox.EmailAccountID, OrganizationID: &inst.OrganizationID, Email: "kept@example.test", Provider: string(models.InboxProviderGoogle), Status: "active"}
	accounts.accounts[google.ID] = &google
	req := models.PoolLinkEnrollRequest{RemoteID: repo.mailbox.RemoteID, Email: "kept@example.test", Provider: models.InboxProviderGoogle, OAuth: oauth}
	if _, xerr := s.Enroll(context.Background(), inst, req); xerr != ErrBadCredential {
		t.Fatalf("google mailbox with the instance's grant: err %v, want %v", xerr, ErrBadCredential)
	}
	if repo.converted || accounts.restored != nil {
		t.Fatal("a refused hand-over changed the mailbox")
	}
}
