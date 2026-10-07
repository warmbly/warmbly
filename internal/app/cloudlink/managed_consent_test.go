package cloudlink

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type consentFaultRepo struct {
	*enrollmentFaultRepo
	consents    []*models.CloudManagedConsent
	allowed     bool
	createErr   error
	activateErr error
}

func (r *consentFaultRepo) BindManagedCloudAccount(_ context.Context, org, id, cloud uuid.UUID) error {
	for _, c := range r.consents {
		if c.ID == id && c.OrganizationID == org {
			if c.CloudAccountID != nil && *c.CloudAccountID != cloud {
				return errors.New("cloud identity changed")
			}
			copy := cloud
			c.CloudAccountID = &copy
			return nil
		}
	}
	return errors.New("missing consent")
}

func (r *consentFaultRepo) CreateManagedConsent(_ context.Context, c *models.CloudManagedConsent) error {
	if r.createErr != nil {
		return r.createErr
	}
	copy := *c
	copy.State = "pending"
	r.consents = append(r.consents, &copy)
	return nil
}
func (r *consentFaultRepo) GetManagedConsent(_ context.Context, org, user uuid.UUID, hash string) (*models.CloudManagedConsent, error) {
	for _, c := range r.consents {
		if c.OrganizationID == org && c.UserID != nil && *c.UserID == user && c.SessionHash != nil && *c.SessionHash == hash {
			copy := *c
			return &copy, nil
		}
	}
	return nil, nil
}
func (r *consentFaultRepo) ListManagedConsents(_ context.Context, instance uuid.UUID) ([]models.CloudManagedConsent, error) {
	var out []models.CloudManagedConsent
	for _, c := range r.consents {
		if c.InstanceID != nil && *c.InstanceID == instance && (c.State == "pending" || c.State == "unknown" || c.State == "pending_remove") {
			out = append(out, *c)
		}
	}
	return out, nil
}
func (r *consentFaultRepo) FindManagedAdoption(_ context.Context, org, user, instance, cloud uuid.UUID) (*models.CloudManagedConsent, error) {
	for _, c := range r.consents {
		if c.Kind == "adopt" && c.OrganizationID == org && c.UserID != nil && *c.UserID == user && c.InstanceID != nil && *c.InstanceID == instance && c.CloudAccountID != nil && *c.CloudAccountID == cloud && c.State != "revoked" && c.State != "pending_remove" {
			copy := *c
			return &copy, nil
		}
	}
	return nil, nil
}
func (r *consentFaultRepo) SetManagedConsentState(_ context.Context, org, id uuid.UUID, state string, account *uuid.UUID) error {
	if state == "active" && r.activateErr != nil {
		return r.activateErr
	}
	for _, c := range r.consents {
		if c.ID == id && c.OrganizationID == org {
			if c.State == "pending_remove" && state == "active" {
				return errors.New("revoked")
			}
			c.State = state
			if account != nil {
				copy := *account
				c.AccountID = &copy
			}
			return nil
		}
	}
	return errors.New("missing consent")
}
func (r *consentFaultRepo) RevokeManagedConsents(_ context.Context, instance uuid.UUID, account *uuid.UUID) error {
	for _, c := range r.consents {
		if c.InstanceID != nil && *c.InstanceID == instance && (account == nil || (c.PlannedAccountID != nil && *c.PlannedAccountID == *account) || (c.AccountID != nil && *c.AccountID == *account)) {
			c.State = "pending_remove"
		}
	}
	return nil
}
func (r *consentFaultRepo) ManagedConsentAuthorized(context.Context, *models.CloudManagedConsent) (bool, error) {
	return r.allowed, nil
}
func (r *consentFaultRepo) CanBrokerManagedToken(context.Context, uuid.UUID) (bool, error) {
	return r.allowed && len(r.consents) > 0 && r.consents[0].State == "active", nil
}
func (r *consentFaultRepo) Enroll(_ context.Context, account, remote, instance uuid.UUID, managed bool) (*models.CloudLinkMailbox, error) {
	if r.confirmErr != nil {
		return nil, r.confirmErr
	}
	r.mailbox = &models.CloudLinkMailbox{EmailAccountID: account, RemoteID: remote, InstanceID: instance, Managed: managed, EnrollmentState: "active"}
	return r.mailbox, nil
}
func (r *consentFaultRepo) SetStanding(context.Context, uuid.UUID, *models.WarmupHealthInfo, bool) (models.WarmupHealthState, error) {
	return models.WarmupHealthHealthy, nil
}

type consentAccounts struct {
	repository.EmailRepository
	accounts        map[uuid.UUID]*models.Email
	creates         int
	persistThenFail bool
}

func (e *consentAccounts) GetByID(_ context.Context, id uuid.UUID) (*models.Email, *errx.Error) {
	if acc := e.accounts[id]; acc != nil {
		return acc, nil
	}
	return nil, errx.ErrNotFound
}
func (e *consentAccounts) NewManagedAccount(_ context.Context, _ string, data models.NewOauthAccount) (*models.Email, *errx.Error) {
	e.creates++
	acc := &models.Email{ID: data.ID, OrganizationID: data.OrganizationID, Provider: string(data.Provider), Name: data.Name, Email: data.Email, Status: "active"}
	e.accounts[data.ID] = acc
	if e.persistThenFail {
		return nil, errx.InternalError()
	}
	return acc, nil
}

type consentFixture struct {
	s                         *service
	r                         *consentFaultRepo
	emails                    *consentAccounts
	org, user, cloud          uuid.UUID
	starts, finishes, deletes int
	protocol                  int
	lostFinish                bool
	revoked                   bool
}

func newConsentFixture(t *testing.T) *consentFixture {
	t.Helper()
	f := &consentFixture{org: uuid.New(), user: uuid.New(), cloud: uuid.New(), protocol: models.ManagedConsentProtocol}
	r := &consentFaultRepo{enrollmentFaultRepo: &enrollmentFaultRepo{stubLinkRepo: &stubLinkRepo{link: &models.CloudLink{InstanceID: uuid.New(), OrganizationID: &f.org}}}, allowed: true}
	f.r = r
	f.emails = &consentAccounts{accounts: map[uuid.UUID]*models.Email{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		path := q.URL.Path
		switch {
		case q.Method == http.MethodDelete:
			f.deletes++
			f.revoked = true
			w.WriteHeader(http.StatusNoContent)
		case path == "/v1/pool-link/instance":
			_ = json.NewEncoder(w).Encode(models.PoolLinkInstanceInfo{Instance: models.PoolLinkInstance{ID: r.link.InstanceID}, ManagedConsentProtocol: f.protocol})
		case path == "/v1/pool-link/instance/workspace-mailboxes":
			_ = json.NewEncoder(w).Encode([]models.PoolLinkWorkspaceMailbox{{ID: f.cloud, Status: "active", Provider: "gmail"}})
		case path == "/v1/pool-link/instance/oauth/start-correlated":
			f.starts++
			var req models.PoolLinkOAuthStartRequest
			_ = json.NewDecoder(q.Body).Decode(&req)
			_ = json.NewEncoder(w).Encode(models.PoolLinkOAuthStartResponse{Session: req.Session, URL: "https://cloud.test/consent"})
		case path == "/v1/pool-link/instance/oauth/finish-correlated" || path == "/v1/pool-link/instance/mailboxes/adopt-correlated":
			f.finishes++
			if f.revoked {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code":"pool_link_oauth_session"}`))
				return
			}
			if f.lostFinish && f.finishes == 1 {
				conn, _, _ := w.(http.Hijacker).Hijack()
				_ = conn.Close()
				return
			}
			remote := *r.consents[0].RemoteID
			completed := time.Now()
			_ = json.NewEncoder(w).Encode(models.PoolLinkMailboxState{RemoteID: remote, EmailAccountID: f.cloud, Email: "consented@test.local", Provider: "gmail", Status: "active", Managed: true, ConsentCompletedAt: &completed, Health: &models.WarmupHealthInfo{State: "healthy"}})
		default:
			t.Errorf("unexpected legacy/unknown request: %s %s", q.Method, path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	r.link.CloudURL = srv.URL
	f.s = NewService(r, f.emails, nil).(*service)
	return f
}

func TestManagedConsentSurvivesRestartLostAcknowledgmentAndLocalPersistenceFailures(t *testing.T) {
	for _, failure := range []string{"lost_ack", "local_account_ack", "enrollment_write", "consent_activation"} {
		t.Run(failure, func(t *testing.T) {
			f := newConsentFixture(t)
			ctx := context.Background()
			start, xerr := f.s.StartOAuth(ctx, f.org, f.user, models.InboxProviderGoogle)
			if xerr != nil {
				t.Fatal(xerr)
			}
			if len(start.Session) < 32 || f.r.consents[0].SessionHash == nil || *f.r.consents[0].SessionHash == start.Session {
				t.Fatal("session not opaque/hash-only")
			}
			if failure == "lost_ack" {
				f.lostFinish = true
			}
			if failure == "local_account_ack" {
				f.emails.persistThenFail = true
			}
			if failure == "enrollment_write" {
				f.r.confirmErr = errors.New("database unavailable")
			}
			if failure == "consent_activation" {
				f.r.activateErr = errors.New("database unavailable")
			}
			if _, xerr := f.s.FinishOAuth(ctx, f.org, f.user, start.Session); xerr == nil {
				t.Fatal("unconfirmed completion reported success")
			}
			if f.r.consents[0].State != "pending" || f.deletes != 0 {
				t.Fatal("recoverable operation compensated or lost")
			}
			f.emails.persistThenFail = false
			f.r.confirmErr = nil
			f.r.activateErr = nil
			f.s = NewService(f.r, f.emails, nil).(*service)
			acc, xerr := f.s.FinishOAuth(ctx, f.org, f.user, start.Session)
			if xerr != nil {
				t.Fatal(xerr)
			}
			again, xerr := f.s.FinishOAuth(ctx, f.org, f.user, start.Session)
			if xerr != nil || again.ID != acc.ID || f.emails.creates != 1 || f.r.consents[0].State != "active" {
				t.Fatalf("non-idempotent mirror: creates=%d,state=%s,err=%v", f.emails.creates, f.r.consents[0].State, xerr)
			}
		})
	}
}

func TestManagedConsentRequiresCurrentActorCapabilityAndSavedIntent(t *testing.T) {
	for _, failure := range []string{"old_cloud", "intent_write", "wrong_user", "wrong_org", "expired", "revoked", "authority_revoked"} {
		t.Run(failure, func(t *testing.T) {
			f := newConsentFixture(t)
			ctx := context.Background()
			if failure == "old_cloud" {
				f.protocol = 0
			}
			if failure == "intent_write" {
				f.r.createErr = errors.New("database unavailable")
			}
			start, xerr := f.s.StartOAuth(ctx, f.org, f.user, models.InboxProviderGoogle)
			if failure == "old_cloud" || failure == "intent_write" {
				if xerr == nil || f.starts != 0 {
					t.Fatal("remote action preceded capability/saved consent")
				}
				return
			}
			if xerr != nil {
				t.Fatal(xerr)
			}
			org, user := f.org, f.user
			if failure == "wrong_user" {
				user = uuid.New()
			}
			if failure == "wrong_org" {
				org = uuid.New()
			}
			if failure == "expired" {
				f.r.consents[0].ExpiresAt = time.Now().Add(-time.Minute)
			}
			if failure == "revoked" {
				f.r.consents[0].State = "pending_remove"
			}
			if failure == "authority_revoked" {
				f.r.allowed = false
			}
			if _, xerr := f.s.FinishOAuth(ctx, org, user, start.Session); xerr == nil || f.finishes != 0 || f.emails.creates != 0 {
				t.Fatal("expired/revoked/wrong-actor consent caused a remote action")
			}
		})
	}
}

func TestManagedAdoptionRetryKeepsItsOriginalRemoteIdentity(t *testing.T) {
	f := newConsentFixture(t)
	ctx := context.Background()
	f.lostFinish = true
	if _, xerr := f.s.Adopt(ctx, f.org, f.user, f.cloud); xerr == nil {
		t.Fatal("lost adoption acknowledgement reported success")
	}
	original := *f.r.consents[0].RemoteID
	f.s = NewService(f.r, f.emails, nil).(*service)
	if _, xerr := f.s.Adopt(ctx, f.org, f.user, f.cloud); xerr != nil {
		t.Fatal(xerr)
	}
	if len(f.r.consents) != 1 || *f.r.consents[0].RemoteID != original || f.emails.creates != 1 {
		t.Fatal("adoption retry duplicated an operation or mailbox")
	}
}

func TestManagedRevocationReconciliationAndCachedTokenCannotReviveConsent(t *testing.T) {
	f := newConsentFixture(t)
	ctx := context.Background()
	start, xerr := f.s.StartOAuth(ctx, f.org, f.user, models.InboxProviderGoogle)
	if xerr != nil {
		t.Fatal(xerr)
	}
	f.r.consents[0].State = "pending_remove"
	if xerr := f.s.reconcileManagedConsents(ctx, f.r.link); xerr != nil {
		t.Fatal(xerr)
	}
	if f.r.consents[0].State != "revoked" || f.deletes != 1 || f.emails.creates != 0 {
		t.Fatal("pending revocation was not retained through remote confirmation")
	}
	if _, xerr := f.s.FinishOAuth(ctx, f.org, f.user, start.Session); xerr == nil {
		t.Fatal("late callback revived consent")
	}
	id := *f.r.consents[0].PlannedAccountID
	f.s.tokens[id] = cachedToken{token: &models.PoolLinkAccessToken{AccessToken: "fixture"}, expires: time.Now().Add(time.Minute)}
	if _, xerr := f.s.AccessToken(ctx, id); xerr == nil {
		t.Fatal("cached token bypassed current consent restriction")
	}
}
