package cloudlink

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type workspaceLinkRepo struct {
	repository.CloudLinkRepository
	links     map[uuid.UUID]*models.CloudLink
	mailboxes map[uuid.UUID]*models.CloudLinkMailbox
}

type workspaceConsentRepo struct {
	*consentFaultRepo
	workspace *models.CloudLink
}

func (r *workspaceConsentRepo) Get(_ context.Context, org *uuid.UUID) (*models.CloudLink, error) {
	if org != nil {
		return r.workspace, nil
	}
	return r.link, nil
}

func TestManagedCompletionUsesConsentedLegacyInstanceAlongsideNewWorkspaceLink(t *testing.T) {
	f := newConsentFixture(t)
	ctx := context.Background()
	start, xerr := f.s.StartOAuth(ctx, f.org, f.user, models.InboxProviderGoogle)
	if xerr != nil {
		t.Fatal(xerr)
	}
	f.r.link.OrganizationID = nil
	f.s.repo = &workspaceConsentRepo{consentFaultRepo: f.r, workspace: &models.CloudLink{
		InstanceID: uuid.New(), OrganizationID: &f.org, CloudURL: "http://127.0.0.1:1",
	}}
	account, xerr := f.s.FinishOAuth(ctx, f.org, f.user, start.Session)
	if xerr != nil || account == nil || f.r.mailbox == nil || f.r.mailbox.InstanceID != f.r.link.InstanceID {
		t.Fatalf("completion lost its original instance: %+v, %v", account, xerr)
	}
	if f.finishes != 1 || f.emails.creates != 1 {
		t.Fatal("legacy consent was not completed exactly once")
	}
	f.r.link = nil
	if _, xerr := f.s.FinishOAuth(ctx, f.org, f.user, start.Session); xerr != ErrOAuthSession || f.finishes != 1 {
		t.Fatalf("revoked source link reused the new workspace connection: %v", xerr)
	}
}

func (r *workspaceLinkRepo) WithReconciliationLock(_ context.Context, fn func() error) error {
	return fn()
}
func (r *workspaceLinkRepo) SetDisconnectPending(_ context.Context, id uuid.UUID) error {
	r.links[id].DisconnectPending = true
	return nil
}
func (r *workspaceLinkRepo) CarryStanding(context.Context, uuid.UUID, *models.WarmupHealthInfo) error {
	return nil
}

func (r *workspaceLinkRepo) Get(_ context.Context, org *uuid.UUID) (*models.CloudLink, error) {
	var legacy *models.CloudLink
	for _, l := range r.links {
		if l.OrganizationID == nil {
			legacy = l
		}
		if org != nil && l.OrganizationID != nil && *l.OrganizationID == *org {
			return l, nil
		}
	}
	return legacy, nil
}
func (r *workspaceLinkRepo) GetByInstance(_ context.Context, id uuid.UUID) (*models.CloudLink, error) {
	return r.links[id], nil
}
func (r *workspaceLinkRepo) GetByAccount(_ context.Context, id uuid.UUID) (*models.CloudLinkMailbox, error) {
	return r.mailboxes[id], nil
}
func (r *workspaceLinkRepo) List(context.Context) ([]models.CloudLinkMailbox, error) {
	var rows []models.CloudLinkMailbox
	for _, m := range r.mailboxes {
		rows = append(rows, *m)
	}
	return rows, nil
}
func (r *workspaceLinkRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(r.links, id)
	return nil
}
func (r *workspaceLinkRepo) UnenrollAll(_ context.Context, id uuid.UUID) error {
	for key, m := range r.mailboxes {
		if m.InstanceID == id {
			delete(r.mailboxes, key)
		}
	}
	return nil
}
func (r *workspaceLinkRepo) CanStore() error { return nil }

func (r *workspaceLinkRepo) ListLinks(context.Context) ([]models.CloudLink, error) {
	out := make([]models.CloudLink, 0, len(r.links))
	for _, link := range r.links {
		out = append(out, *link)
	}
	return out, nil
}

func TestRedirectListingPreservesHealthyLinkOwnershipWhenAnotherLinkFails(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer unavailable" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []models.DomainRedirect{{Domain: "healthy.example"}}})
	}))
	defer srv.Close()
	healthy := &models.CloudLink{InstanceID: uuid.New(), CloudURL: srv.URL, Token: "healthy"}
	unavailable := &models.CloudLink{InstanceID: uuid.New(), CloudURL: srv.URL, Token: "unavailable"}
	s := &service{repo: &workspaceLinkRepo{links: map[uuid.UUID]*models.CloudLink{healthy.InstanceID: healthy, unavailable.InstanceID: unavailable}}}
	rows, xerr := s.ListRedirects(context.Background())
	if xerr != nil || len(rows) != 1 || rows[0].Domain != "healthy.example" || rows[0].CloudLinkInstanceID == nil || *rows[0].CloudLinkInstanceID != healthy.InstanceID {
		t.Fatalf("healthy redirects not preserved: %+v, %v", rows, xerr)
	}
}

func (r *workspaceLinkRepo) ListForOrg(context.Context, uuid.UUID, *uuid.UUID) ([]models.CloudLinkMailbox, error) {
	return r.List(context.Background())
}

func TestWarmupReportsUseEachMailboxesOriginalLink(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	org := uuid.New()
	legacyRemote, scopedRemote := uuid.New(), uuid.New()
	got := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req models.PoolLinkWarmupReportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		auth := r.Header.Get("Authorization")
		got[auth]++
		want := scopedRemote
		if auth == "Bearer legacy" {
			want = legacyRemote
		}
		if len(req.RemoteIDs) != 1 || req.RemoteIDs[0] != want {
			t.Errorf("wrong mailbox on %s", auth)
		}
		_ = json.NewEncoder(w).Encode([]models.WarmupDailyStats{{Date: req.From, EmailsSent: 1, Active: true}})
	}))
	defer srv.Close()
	legacy := &models.CloudLink{InstanceID: uuid.New(), CloudURL: srv.URL, Token: "legacy"}
	scoped := &models.CloudLink{InstanceID: uuid.New(), OrganizationID: &org, CloudURL: srv.URL, Token: "scoped"}
	a, b := uuid.New(), uuid.New()
	repo := &workspaceLinkRepo{links: map[uuid.UUID]*models.CloudLink{legacy.InstanceID: legacy, scoped.InstanceID: scoped},
		mailboxes: map[uuid.UUID]*models.CloudLinkMailbox{a: {EmailAccountID: a, RemoteID: legacyRemote, InstanceID: legacy.InstanceID}, b: {EmailAccountID: b, RemoteID: scopedRemote, InstanceID: scoped.InstanceID}}}
	svc := &service{repo: repo}
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows, xerr := svc.WarmupStats(context.Background(), org, nil, day, day)
	if xerr != nil || len(rows) != 1 || rows[0].EmailsSent != 2 || got["Bearer legacy"] != 1 || got["Bearer scoped"] != 1 {
		t.Fatalf("per-link reports = %+v, %v; calls %v", rows, xerr, got)
	}
}

func TestWorkspaceResolutionDoesNotGrantNewAccessOnLegacyLinks(t *testing.T) {
	org, other := uuid.New(), uuid.New()
	legacy := &models.CloudLink{InstanceID: uuid.New()}
	scoped := &models.CloudLink{InstanceID: uuid.New(), OrganizationID: &org}
	repo := &workspaceLinkRepo{links: map[uuid.UUID]*models.CloudLink{legacy.InstanceID: legacy, scoped.InstanceID: scoped}}
	svc := &service{repo: repo}
	if got, xerr := svc.newLink(context.Background(), org); xerr != nil || got != scoped {
		t.Fatalf("workspace link = %v, %v", got, xerr)
	}
	if _, xerr := svc.newLink(context.Background(), other); xerr != ErrLegacyLink {
		t.Fatalf("legacy granted new access: %v", xerr)
	}
	if _, xerr := svc.StartOAuth(context.Background(), other, uuid.New(), models.InboxProviderGoogle); xerr != ErrLegacyLink {
		t.Fatalf("legacy OAuth = %v", xerr)
	}
	svc.emails = stubEmails{account: &models.Email{ID: uuid.New(), OrganizationID: &other}}
	if _, xerr := svc.Enroll(context.Background(), other, uuid.New()); xerr != ErrLegacyLink {
		t.Fatalf("legacy enrollment = %v", xerr)
	}
}

func TestLegacyMailboxKeepsItsTokenAfterWorkspaceReconnects(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	org, account := uuid.New(), uuid.New()
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer legacy-token" {
			t.Errorf("used wrong workspace token")
		}
		_ = json.NewEncoder(w).Encode(models.PoolLinkAccessToken{AccessToken: "provider-fixture", ExpiresAt: time.Now().Add(time.Hour)})
	}))
	defer srv.Close()
	legacy := &models.CloudLink{InstanceID: uuid.New(), CloudURL: srv.URL, Token: "legacy-token"}
	scoped := &models.CloudLink{InstanceID: uuid.New(), OrganizationID: &org, CloudURL: srv.URL, Token: "new-workspace-token"}
	repo := &workspaceLinkRepo{links: map[uuid.UUID]*models.CloudLink{legacy.InstanceID: legacy, scoped.InstanceID: scoped},
		mailboxes: map[uuid.UUID]*models.CloudLinkMailbox{account: {EmailAccountID: account, RemoteID: uuid.New(), InstanceID: legacy.InstanceID, Managed: true}}}
	svc := &service{repo: repo, tokens: map[uuid.UUID]cachedToken{}}
	tok, xerr := svc.AccessToken(context.Background(), account)
	if xerr != nil || tok.AccessToken != "provider-fixture" || requests != 1 {
		t.Fatalf("legacy token = %v, %v; calls %d", tok, xerr, requests)
	}
}

func TestDisconnectAffectsOnlyTheSelectedLink(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	org, other := uuid.New(), uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.Header.Get("Authorization") != "Bearer scoped" {
			t.Errorf("revoked wrong link: %s", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	legacy := &models.CloudLink{InstanceID: uuid.New(), CloudURL: srv.URL, Token: "legacy"}
	scoped := &models.CloudLink{InstanceID: uuid.New(), OrganizationID: &org, CloudURL: srv.URL, Token: "scoped"}
	otherLink := &models.CloudLink{InstanceID: uuid.New(), OrganizationID: &other, CloudURL: srv.URL, Token: "other"}
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	repo := &workspaceLinkRepo{links: map[uuid.UUID]*models.CloudLink{legacy.InstanceID: legacy, scoped.InstanceID: scoped, otherLink.InstanceID: otherLink},
		mailboxes: map[uuid.UUID]*models.CloudLinkMailbox{a: {EmailAccountID: a, InstanceID: legacy.InstanceID}, b: {EmailAccountID: b, InstanceID: scoped.InstanceID}, c: {EmailAccountID: c, InstanceID: otherLink.InstanceID}}}
	svc := &service{repo: repo,
		tokens: map[uuid.UUID]cachedToken{a: {}, b: {}, c: {}}}
	var disconnected uuid.UUID
	svc.OnDisconnect(func(_ context.Context, id uuid.UUID) { disconnected = id })
	if xerr := svc.Disconnect(context.Background(), org, false); xerr != nil {
		t.Fatal(xerr)
	}
	if len(repo.links) != 2 || repo.links[legacy.InstanceID] == nil || repo.links[otherLink.InstanceID] == nil || len(repo.mailboxes) != 2 || repo.mailboxes[b] != nil {
		t.Fatalf("another connection was removed: %+v", repo)
	}
	if disconnected != scoped.InstanceID || len(svc.tokens) != 2 || legacy.DisconnectPending || otherLink.DisconnectPending {
		t.Fatal("disconnect invalidated another workspace's state")
	}
	if xerr := svc.Disconnect(context.Background(), org, false); xerr != ErrLegacyLink {
		t.Fatalf("implicit legacy disconnect = %v", xerr)
	}
}

func TestConnectRefusesCloudWithoutWorkspaceSupport(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	org := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req models.PoolLinkStartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RemoteOrganizationID != org {
			t.Errorf("handshake omitted workspace: %v", err)
		}
		_ = json.NewEncoder(w).Encode(models.PoolLinkStartResponse{UserCode: "ABCD-EFGH"})
	}))
	defer srv.Close()
	svc := &service{repo: &workspaceLinkRepo{}, pending: map[uuid.UUID]*PendingConnect{}}
	if _, xerr := svc.StartConnect(context.Background(), org, uuid.New(), srv.URL); xerr == nil || xerr.Identifier != "cloud_link_upgrade_required" {
		t.Fatalf("old Cloud was accepted: %v", xerr)
	}
}
