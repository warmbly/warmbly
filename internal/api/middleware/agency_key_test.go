package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type memAgencyKeys struct {
	repository.AgencyAPIKeyRepository
	byHash map[string]*models.AgencyAPIKey
}

func (r *memAgencyKeys) Create(_ context.Context, agencyID, createdBy uuid.UUID, d *models.CreateAgencyAPIKey, prefix, suffix, hash string) (*models.AgencyAPIKey, error) {
	k := &models.AgencyAPIKey{ID: uuid.New(), AgencyID: agencyID, CreatedBy: createdBy, Name: d.Name, Permissions: d.Permissions, Status: models.APIKeyStatusActive}
	r.byHash[hash] = k
	return k, nil
}

func (r *memAgencyKeys) GetActiveByHash(_ context.Context, hash string) (*models.AgencyAPIKey, error) {
	return r.byHash[hash], nil
}

func (r *memAgencyKeys) TouchLastUsed(context.Context, uuid.UUID, string) error { return nil }

type memAgencyStaff struct {
	agency  uuid.UUID
	roles   map[uuid.UUID]models.AgencyRole
	clients map[uuid.UUID]uuid.UUID
}

func (s *memAgencyStaff) GetMember(_ context.Context, agencyID, userID uuid.UUID) (*models.AgencyMember, error) {
	role, ok := s.roles[userID]
	if !ok || agencyID != s.agency {
		return nil, nil
	}
	return &models.AgencyMember{AgencyID: agencyID, UserID: userID, Role: role}, nil
}

func (s *memAgencyStaff) GetClient(_ context.Context, orgID uuid.UUID) (*models.AgencyClient, error) {
	a, ok := s.clients[orgID]
	if !ok {
		return nil, nil
	}
	return &models.AgencyClient{OrganizationID: orgID, AgencyID: a}, nil
}

type agencyKeyWorld struct {
	h       *Handler
	staff   *memAgencyStaff
	orgs    *membershipOrgs
	secret  string
	creator uuid.UUID
	client  uuid.UUID
}

func newAgencyKeyWorld(t *testing.T, perms uint64) *agencyKeyWorld {
	t.Helper()
	creator, client := uuid.New(), uuid.New()
	staff := &memAgencyStaff{agency: uuid.New(), roles: map[uuid.UUID]models.AgencyRole{creator: models.AgencyRoleOwner},
		clients: map[uuid.UUID]uuid.UUID{}}
	staff.clients[client] = staff.agency
	svc := apikey.NewAgencyKeyService(&memAgencyKeys{byHash: map[string]*models.AgencyAPIKey{}}, staff, nil)
	k, xerr := svc.Create(context.Background(), staff.agency, creator, &models.CreateAgencyAPIKey{Name: "ci", Permissions: perms})
	if xerr != nil {
		t.Fatal(xerr)
	}
	now := time.Now()
	orgs := &membershipOrgs{members: map[uuid.UUID]*models.OrganizationMember{
		client: {OrganizationID: client, UserID: creator, Role: string(models.RoleAdmin), Permissions: models.RolePermissions[models.RoleAdmin], AcceptedAt: &now},
	}}
	return &agencyKeyWorld{h: &Handler{AgencyKeys: svc, OrganizationService: orgs}, staff: staff, orgs: orgs, secret: k.Secret, creator: creator, client: client}
}

type agencyCall struct {
	code   int
	errID  string
	org    string
	perms  uint64
	userID string
}

func serve(t *testing.T, r *gin.Engine, method, path, auth, org string) agencyCall {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	req.Header.Set("Authorization", "Bearer "+auth)
	if org != "" {
		req.Header.Set(models.AgencyOrganizationHeader, org)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var body struct {
		Code   string `json:"code"`
		Org    string `json:"org"`
		Perms  uint64 `json:"perms"`
		UserID string `json:"user_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return agencyCall{code: w.Code, errID: body.Code, org: body.Org, perms: body.Perms, userID: body.UserID}
}

func echoCaller(c *gin.Context) {
	org := ""
	if id := GetOrganizationID(c); id != nil {
		org = id.String()
	}
	c.JSON(http.StatusOK, gin.H{"org": org, "perms": GetAPIKeyPermissions(c), "user_id": c.GetString(UserIDKey)})
}

func TestAgencyKeyActsOnlyOnACurrentClient(t *testing.T) {
	w := newAgencyKeyWorld(t, models.APIPermReadCampaigns|models.APIPermAgencyRead)
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/v1/campaigns", w.h.CombinedAuthMiddleware(), echoCaller)

	got := serve(t, r, http.MethodGet, "/v1/campaigns", w.secret, w.client.String())
	if got.code != http.StatusOK || got.org != w.client.String() || got.userID != w.creator.String() {
		t.Fatalf("client call = %+v", got)
	}
	if got.perms != models.APIPermReadCampaigns {
		t.Errorf("workspace call carried bits %b, want only the workspace ones", got.perms)
	}

	otherAgencysClient := uuid.New()
	w.staff.clients[otherAgencysClient] = uuid.New()
	for name, tc := range map[string]struct {
		org    string
		code   int
		errID  string
		before func()
	}{
		"missing header":          {"", http.StatusBadRequest, apikey.CodeAgencyKeyOrgRequired, nil},
		"malformed header":        {"not-a-uuid", http.StatusBadRequest, apikey.CodeAgencyKeyOrgRequired, nil},
		"not a client":            {uuid.NewString(), http.StatusForbidden, apikey.CodeAgencyKeyNotClient, nil},
		"another agency's client": {otherAgencysClient.String(), http.StatusForbidden, apikey.CodeAgencyKeyNotClient, nil},
	} {
		if got := serve(t, r, http.MethodGet, "/v1/campaigns", w.secret, tc.org); got.code != tc.code || got.errID != tc.errID {
			t.Errorf("%s: %+v, want %d %s", name, got, tc.code, tc.errID)
		}
	}

	// The creator's membership in the client is what the key acts through.
	saved := w.orgs.members[w.client]
	delete(w.orgs.members, w.client)
	if got := serve(t, r, http.MethodGet, "/v1/campaigns", w.secret, w.client.String()); got.code != http.StatusUnauthorized || got.errID != apikey.CodeAgencyKeyHolderLeft {
		t.Errorf("key reached a client its creator has no membership in: %+v", got)
	}
	w.orgs.members[w.client] = saved

	delete(w.staff.clients, w.client)
	if got := serve(t, r, http.MethodGet, "/v1/campaigns", w.secret, w.client.String()); got.code != http.StatusForbidden || got.errID != apikey.CodeAgencyKeyNotClient {
		t.Errorf("detached client still reachable: %+v", got)
	}
}

func TestAgencyKeyEndsWithItsCreatorsRole(t *testing.T) {
	w := newAgencyKeyWorld(t, models.APIPermReadCampaigns)
	r := gin.New()
	r.GET("/v1/campaigns", w.h.CombinedAuthMiddleware(), echoCaller)
	w.staff.roles[w.creator] = models.AgencyRoleMember
	if got := serve(t, r, http.MethodGet, "/v1/campaigns", w.secret, w.client.String()); got.code != http.StatusUnauthorized || got.errID != apikey.CodeAgencyKeyHolderLeft {
		t.Errorf("demoted creator's key still works: %+v", got)
	}
}

func TestAgencyRouteGate(t *testing.T) {
	w := newAgencyKeyWorld(t, models.APIPermAgencyRead|models.APIPermReadCampaigns)
	r := gin.New()
	g := r.Group("/v1/agency", w.h.AgencyAuthMiddleware())
	g.GET("/:agencyId", RequireAgencyAccess(models.APIPermAgencyRead), echoCaller)
	g.GET("/:agencyId/usage", RequireAgencyAccess(models.APIPermAgencyReadUsage), echoCaller)
	g.POST("/:agencyId/billing/checkout", RequireAgencyAccess(0), echoCaller)
	own := "/v1/agency/" + w.staff.agency.String()

	if got := serve(t, r, http.MethodGet, own, w.secret, ""); got.code != http.StatusOK || got.userID != w.creator.String() || got.org != "" {
		t.Errorf("own agency with the bit: %+v", got)
	}
	if got := serve(t, r, http.MethodGet, "/v1/agency/"+uuid.NewString(), w.secret, ""); got.code != http.StatusNotFound || got.errID != "agency_not_found" {
		t.Errorf("another agency: %+v", got)
	}
	if got := serve(t, r, http.MethodGet, own+"/usage", w.secret, ""); got.code != http.StatusForbidden {
		t.Errorf("missing bit: %+v", got)
	}
	if got := serve(t, r, http.MethodPost, own+"/billing/checkout", w.secret, ""); got.code != http.StatusForbidden || got.errID != apikey.CodeAgencyKeyNotAllowed {
		t.Errorf("session-only route: %+v", got)
	}
	if got := serve(t, r, http.MethodGet, own, "wmbly_workspacekey", ""); got.code != http.StatusForbidden || got.errID != "agency_credential_required" {
		t.Errorf("workspace key on an agency route: %+v", got)
	}
}

func TestSessionOnlyRoutesRefuseAgencyKeys(t *testing.T) {
	w := newAgencyKeyWorld(t, models.APIPermReadCampaigns)
	r := gin.New()
	r.GET("/v1/organization", w.h.AuthMiddleware(), echoCaller)
	r.GET("/v1/api-keys", w.h.CombinedAuthMiddleware(), RefuseAgencyKey(), echoCaller)
	if got := serve(t, r, http.MethodGet, "/v1/organization", w.secret, w.client.String()); got.code != http.StatusForbidden || got.errID != apikey.CodeAgencyKeyNotAllowed {
		t.Errorf("session route: %+v", got)
	}
	if got := serve(t, r, http.MethodGet, "/v1/api-keys", w.secret, w.client.String()); got.code != http.StatusForbidden || got.errID != apikey.CodeAgencyKeyNotAllowed {
		t.Errorf("workspace key management: %+v", got)
	}
}
