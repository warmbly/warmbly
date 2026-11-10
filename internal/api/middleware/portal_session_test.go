package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// fakeClients is the agency_clients table: workspace to the agency holding it.
type fakeClients struct {
	mu    sync.Mutex
	held  map[uuid.UUID]uuid.UUID
	reads int
}

func (f *fakeClients) GetClient(_ context.Context, orgID uuid.UUID) (*models.AgencyClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	agency, ok := f.held[orgID]
	if !ok {
		return nil, nil
	}
	return &models.AgencyClient{OrganizationID: orgID, AgencyID: agency}, nil
}

func (f *fakeClients) detach(orgID uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.held, orgID)
}

type portalFixture struct {
	agency, user, client, other uuid.UUID
	clients                     *fakeClients
	cache                       *PortalClientCache
}

func newPortalFixture(current uuid.UUID, fx *portalFixture) *Handler {
	session := &models.Session{ID: uuid.New(), UserID: fx.user, CurrentOrganizationID: &current, PortalAgencyID: &fx.agency}
	members := map[uuid.UUID]*models.OrganizationMember{
		fx.client: {OrganizationID: fx.client, UserID: fx.user, Permissions: models.PermViewContacts},
		fx.other:  {OrganizationID: fx.other, UserID: fx.user, Permissions: models.PermViewContacts},
	}
	return &Handler{
		TokenService:        sessionTokens{session: session},
		OrganizationService: &membershipOrgs{members: members},
		PortalClients:       fx.cache,
	}
}

func portalSetup() *portalFixture {
	fx := &portalFixture{agency: uuid.New(), user: uuid.New(), client: uuid.New(), other: uuid.New()}
	fx.clients = &fakeClients{held: map[uuid.UUID]uuid.UUID{fx.client: fx.agency, fx.other: uuid.New()}}
	fx.cache = NewPortalClientCache(fx.clients)
	return fx
}

func serveRoute(h *Handler, method, pattern, path string) (int, *uuid.UUID, string) {
	var seen *uuid.UUID
	r := gin.New()
	r.Handle(method, pattern, h.AuthMiddleware(), func(c *gin.Context) {
		seen = GetOrganizationID(c)
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	req.Header.Set("Authorization", "Bearer t")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, seen, rec.Body.String()
}

func TestPortalSessionReachesItsAgencyClient(t *testing.T) {
	fx := portalSetup()
	code, seen, _ := serveRoute(newPortalFixture(fx.client, fx), http.MethodGet, "/v1/contacts", "/v1/contacts")
	if code != http.StatusOK || seen == nil || *seen != fx.client {
		t.Fatalf("client workspace: status %d, org %v; want 200 and the client", code, seen)
	}
}

func TestPortalSessionHasNoWorkspaceOutsideItsAgency(t *testing.T) {
	fx := portalSetup()
	code, seen, _ := serveRoute(newPortalFixture(fx.other, fx), http.MethodGet, "/v1/contacts", "/v1/contacts")
	if code != http.StatusOK || seen != nil {
		t.Fatalf("other agency's workspace: status %d, org %v; want no workspace in the request", code, seen)
	}
}

func TestPortalSessionLosesADetachedClient(t *testing.T) {
	fx := portalSetup()
	now := time.Now()
	fx.cache.now = func() time.Time { return now }
	h := newPortalFixture(fx.client, fx)
	if _, seen, _ := serveRoute(h, http.MethodGet, "/v1/contacts", "/v1/contacts"); seen == nil {
		t.Fatal("client workspace not reached before the detach")
	}
	fx.clients.detach(fx.client)
	now = now.Add(portalClientTTL + time.Second)
	if _, seen, _ := serveRoute(h, http.MethodGet, "/v1/contacts", "/v1/contacts"); seen != nil {
		t.Fatalf("detached client still reached: %v", seen)
	}
}

func TestPortalSessionCannotSwitchOutsideItsAgency(t *testing.T) {
	fx := portalSetup()
	h := newPortalFixture(fx.client, fx)
	const pattern = "/v1/organization/switch/:id"
	if code, _, body := serveRoute(h, http.MethodPost, pattern, "/v1/organization/switch/"+fx.other.String()); code != http.StatusForbidden || !strings.Contains(body, CodePortalSessionRestricted) {
		t.Fatalf("switch to another agency's workspace: %d %s", code, body)
	}
	if code, _, body := serveRoute(h, http.MethodPost, pattern, "/v1/organization/switch/"+fx.client.String()); code != http.StatusOK {
		t.Fatalf("switch to a client: %d %s", code, body)
	}
}

func TestPortalSessionRefusedOnAccountRoute(t *testing.T) {
	fx := portalSetup()
	h := newPortalFixture(fx.client, fx)
	code, _, body := serveRoute(h, http.MethodPost, "/v1/auth/me/password", "/v1/auth/me/password")
	if code != http.StatusForbidden || !strings.Contains(body, CodePortalSessionRestricted) {
		t.Fatalf("change password: %d %s", code, body)
	}
}

func TestPortalSessionHeldToItsAgencyRoutes(t *testing.T) {
	fx := portalSetup()
	h := newPortalFixture(fx.client, fx)
	const pattern = "/v1/agency/:agencyId/clients"
	if code, _, _ := serveRoute(h, http.MethodGet, pattern, "/v1/agency/"+fx.agency.String()+"/clients"); code != http.StatusOK {
		t.Fatalf("own agency: %d", code)
	}
	if code, _, body := serveRoute(h, http.MethodGet, pattern, "/v1/agency/"+uuid.NewString()+"/clients"); code != http.StatusForbidden || !strings.Contains(body, CodePortalSessionRestricted) {
		t.Fatalf("other agency: %d %s", code, body)
	}
}

func TestMainSessionIsNotHeldToAPortal(t *testing.T) {
	fx := portalSetup()
	h := newPortalFixture(fx.other, fx)
	h.TokenService = sessionTokens{session: &models.Session{ID: uuid.New(), UserID: fx.user, CurrentOrganizationID: &fx.other}}
	if code, seen, _ := serveRoute(h, http.MethodGet, "/v1/contacts", "/v1/contacts"); code != http.StatusOK || seen == nil {
		t.Fatalf("main session: %d %v", code, seen)
	}
	if code, _, body := serveRoute(h, http.MethodPost, "/v1/auth/me/password", "/v1/auth/me/password"); strings.Contains(body, CodePortalSessionRestricted) {
		t.Fatalf("main session refused as a portal session: %d %s", code, body)
	}
}
