package poollink

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
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

type managedOpsOrgs struct {
	organization.OrganizationService
	org *models.Organization
}

func (o *managedOpsOrgs) Get(context.Context, uuid.UUID) (*models.Organization, *errx.Error) {
	return o.org, nil
}

func newManagedOpsService(t *testing.T) (*service, *managedOpsRepo, *managedOpsEmailService) {
	t.Helper()
	redisURL := os.Getenv("WARMBLY_TEST_REDIS")
	if redisURL == "" {
		t.Skip("set WARMBLY_TEST_REDIS for real Redis OAuth fixtures")
	}
	c, err := cache.New(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	org, user := uuid.New(), uuid.New()
	r := &managedOpsRepo{instance: &models.PoolLinkInstance{ID: uuid.New(), OrganizationID: org, CreatedBy: &user, URL: "https://self.test"}}
	emails := &managedOpsEmails{accounts: map[uuid.UUID]*models.Email{}}
	e := &managedOpsEmailService{emails: emails}
	s := NewService(r, emails, e, nil, nil, nil, &managedOpsOrgs{org: &models.Organization{ID: org, OwnerUserID: user}}, nil).(*service)
	s.WireCache(c)
	return s, r, e
}
func startManagedOps(t *testing.T, s *service, r *managedOpsRepo) (models.PoolLinkOAuthStartRequest, string) {
	t.Helper()
	req := models.PoolLinkOAuthStartRequest{Protocol: models.ManagedConsentProtocol, RemoteID: uuid.New(), Session: strings.ReplaceAll(uuid.NewString(), "-", "") + "opaque", Provider: models.InboxProviderOutlook, ReturnURL: r.instance.URL + "/cloud-oauth/done"}
	start, xerr := s.StartOAuth(context.Background(), r.instance, req)
	if xerr != nil {
		t.Fatal(xerr)
	}
	u, err := url.Parse(start.URL)
	if err != nil {
		t.Fatal(err)
	}
	state := u.Query().Get("state")
	if !IsBrokerState(state) || strings.HasPrefix(state, BrokerStatePrefix) {
		t.Fatal("old callback can misinterpret a correlated state")
	}
	if _, xerr := s.ContinueOAuth(context.Background(), state, "fixture-browser-binding"); xerr != nil {
		t.Fatal(xerr)
	}
	return req, state
}

func TestManagedOAuthRealRedisDoubleCallbackReplayAndLostResult(t *testing.T) {
	s, r, e := newManagedOpsService(t)
	ctx := context.Background()
	req, state := startManagedOps(t, s, r)
	if again, xerr := s.StartOAuth(ctx, r.instance, req); xerr != nil || again.Session != req.Session {
		t.Fatal("start replay lost durable identity")
	}
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			_, _ = s.CompleteOAuthCallback(ctx, "outlook", "fake-code", state, "", "fixture-browser-binding")
		}()
	}
	wg.Wait()
	if e.exchanges != 1 || e.starts != 1 || r.op.State != "completed" {
		t.Fatalf("exchanges=%d,starts=%d,state=%s", e.exchanges, e.starts, r.op.State)
	}
	if err := s.cache.Del(ctx, brokerSessionKey(req.Session)).Err(); err != nil {
		t.Fatal(err)
	}
	s = &service{repo: r, emails: e.emails, emailSvc: e, orgs: s.orgs, cache: s.cache}
	for range 2 {
		got, xerr := s.FinishManagedOAuth(ctx, r.instance, models.PoolLinkOAuthFinishRequest{Protocol: 1, RemoteID: req.RemoteID})
		if xerr != nil || got.EmailAccountID != *r.op.PlannedAccountID || got.ConsentCompletedAt == nil {
			t.Fatalf("lost result recovery=%+v,err=%v", got, xerr)
		}
	}
	if e.exchanges != 1 || e.starts != 1 || len(e.emails.accounts) != 1 {
		t.Fatal("completion replay repeated exchange/start or created another mailbox")
	}
}

func TestManagedOAuthRecoverableAccountCommitAndUnknownExchangeAreDistinct(t *testing.T) {
	for _, failure := range []string{"completion_write", "unknown_exchange", "activation_write"} {
		t.Run(failure, func(t *testing.T) {
			s, r, e := newManagedOpsService(t)
			ctx := context.Background()
			req, state := startManagedOps(t, s, r)
			r.failComplete = failure == "completion_write"
			e.unknown = failure == "unknown_exchange"
			e.failStart = failure == "activation_write"
			_, _ = s.CompleteOAuthCallback(ctx, "outlook", "fake-code", state, "", "fixture-browser-binding")
			if err := s.cache.Del(ctx, brokerSessionKey(req.Session)).Err(); err != nil {
				t.Fatal(err)
			}
			r.failComplete = false
			e.failStart = false
			s = &service{repo: r, emails: e.emails, emailSvc: e, orgs: s.orgs, cache: s.cache}
			got, xerr := s.FinishManagedOAuth(ctx, r.instance, models.PoolLinkOAuthFinishRequest{Protocol: 1, RemoteID: req.RemoteID})
			if failure == "unknown_exchange" {
				if xerr != ErrOAuthUnknown || got != nil || r.mailbox != nil {
					t.Fatal("unknown provider exchange was treated as completion")
				}
			} else if xerr != nil || got == nil || e.starts != 1 || r.op.ActivationPending {
				t.Fatalf("known account commit did not recover: %v", xerr)
			}
			if e.exchanges != 1 {
				t.Fatal("authorization code retried after an uncertain outcome")
			}
		})
	}
}

func TestManagedOAuthExpiredRevokedInstanceOptOutAndWrongBrowserNeverExchange(t *testing.T) {
	for _, failure := range []string{"expired", "revoked_instance", "opt_out", "wrong_browser"} {
		t.Run(failure, func(t *testing.T) {
			s, r, e := newManagedOpsService(t)
			ctx := context.Background()
			req, state := startManagedOps(t, s, r)
			binding := "fixture-browser-binding"
			if failure == "expired" {
				r.op.ExpiresAt = time.Now().Add(-time.Minute)
			}
			if failure == "revoked_instance" {
				now := time.Now()
				r.instance.RevokedAt = &now
			}
			if failure == "opt_out" {
				_ = r.RevokeManagedOperations(ctx, r.instance.ID, &req.RemoteID)
			}
			if failure == "wrong_browser" {
				binding = "other-browser"
			}
			_, _ = s.CompleteOAuthCallback(ctx, "outlook", "fake-code", state, "", binding)
			if e.exchanges != 0 || r.mailbox != nil {
				t.Fatal("missing current authority permitted exchange")
			}
			if failure == "wrong_browser" {
				if _, xerr := s.peekBrokerState(ctx, state); xerr != nil {
					t.Fatal("wrong browser consumed the real browser's state")
				}
			}
		})
	}
}

func TestManagedAdoptionCompletionReplayDoesNotResumeExplicitPauseOrRevocation(t *testing.T) {
	s, r, e := newManagedOpsService(t)
	ctx := context.Background()
	id := uuid.New()
	e.emails.accounts[id] = &models.Email{ID: id, OrganizationID: &r.instance.OrganizationID, Status: "active", Provider: "outlook", AuthMethod: "oauth"}
	req := models.PoolLinkAdoptRequest{Protocol: 1, RemoteID: uuid.New(), EmailAccountID: id}
	for range 2 {
		if _, xerr := s.Adopt(ctx, r.instance, req); xerr != nil {
			t.Fatal(xerr)
		}
	}
	if e.starts != 1 {
		t.Fatal("adoption replay resumed an existing mailbox")
	}
	if _, xerr := s.PatchMailbox(ctx, r.instance, req.RemoteID, models.PoolLinkMailboxPatch{Lifecycle: "pause"}); xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := s.Adopt(ctx, r.instance, req); xerr != nil || e.starts != 1 {
		t.Fatal("replay undid explicit pause")
	}
	if xerr := s.Unenroll(ctx, r.instance, req.RemoteID); xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := s.Adopt(ctx, r.instance, req); xerr == nil || r.mailbox != nil {
		t.Fatal("adoption replay undid revocation")
	}
	if _, xerr := s.AccessToken(ctx, r.instance, req.RemoteID); xerr == nil {
		t.Fatal("revoked operation could broker a token")
	}
}

func TestLegacyOAuthFinishRemainsOneShot(t *testing.T) {
	s, r, e := newManagedOpsService(t)
	ctx := context.Background()
	id, remote := uuid.New(), uuid.New()
	e.emails.accounts[id] = &models.Email{ID: id, OrganizationID: &r.instance.OrganizationID, Status: "active", Provider: "outlook", AuthMethod: "oauth"}
	r.mailbox = &models.PoolLinkMailbox{InstanceID: r.instance.ID, RemoteID: remote, EmailAccountID: id, Managed: true}
	session := uuid.NewString()
	if err := s.cache.SetJSON(ctx, brokerSessionKey(session), brokerResult{InstanceID: r.instance.ID, RemoteID: remote}, brokerTTL); err != nil {
		t.Fatal(err)
	}
	if _, xerr := s.FinishOAuth(ctx, r.instance, session); xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := s.FinishOAuth(ctx, r.instance, session); xerr != ErrOAuthSession {
		t.Fatal("legacy one-shot contract changed")
	}
}
