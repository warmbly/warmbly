package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type scopeOrgs struct {
	organization.OrganizationService
	scope    *models.ResourceScope
	resolved int
	err      *errx.Error
}

func (f *scopeOrgs) ResolveMemberScope(context.Context, uuid.UUID, uuid.UUID) (*models.ResourceScope, *errx.Error) {
	f.resolved++
	return f.scope, f.err
}

func TestResourceScopeGateFailsClosedOnResolutionFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name string
		err  *errx.Error
	}{
		{"nil scope", nil},
		{"resolver error", errx.InternalError()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orgs := &scopeOrgs{err: tc.err}
			rec, seen := serveScoped(t, &Handler{OrganizationService: orgs}, restrictedMember(uuid.New()), nil, http.MethodGet, "/v1/organization/current", "/v1/organization/current")
			if rec.Code != http.StatusInternalServerError || seen != nil || orgs.resolved != 1 {
				t.Fatalf("resolution failure proceeded: HTTP %d seen=%v resolved=%d", rec.Code, seen != nil, orgs.resolved)
			}
		})
	}
}

func TestResourceScopeGateDefersCreationToAccountPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, auth := range []string{AuthTypeJWT, AuthTypeAPIKey, AuthTypeOAuth} {
		t.Run(auth, func(t *testing.T) {
			orgs := &scopeOrgs{err: errx.InternalError()}
			h := &Handler{OrganizationService: orgs}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(AuthTypeKey, auth)
				c.Set(SessionMemberKey, restrictedMember(uuid.New()))
			})
			r.POST("/v1/organization", h.ResourceScopeGate(), func(c *gin.Context) { c.Status(http.StatusAccepted) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/organization", nil))
			if w.Code != http.StatusAccepted || orgs.resolved != 0 {
				t.Fatalf("creation was tied to selected member resolution: HTTP %d resolved=%d", w.Code, orgs.resolved)
			}
		})
	}
}

func restrictedMember(orgID uuid.UUID) *models.OrganizationMember {
	return &models.OrganizationMember{OrganizationID: orgID, UserID: uuid.New(), Role: "Viewer",
		Permissions: models.RestrictedPermissionMask, AccessScope: models.AccessScopeRestricted}
}

// serveScoped runs one request through the gate as member, with an optional API key allowlist.
func serveScoped(t *testing.T, h *Handler, member *models.OrganizationMember, keyList []uuid.UUID, method, route, path string, gates ...gin.HandlerFunc) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	var seen *gin.Context
	r := gin.New()
	r.Use(RequestIDMiddleware())
	chain := []gin.HandlerFunc{func(c *gin.Context) {
		c.Set(SessionMemberKey, member)
		c.Set(OrganizationIDKey, member.OrganizationID)
		c.Set(UserIDKey, member.UserID.String())
		if keyList != nil {
			c.Set(AuthTypeKey, AuthTypeAPIKey)
			c.Set(APIKeyAllowedEmailAccountsKey, keyList)
		} else {
			c.Set(AuthTypeKey, AuthTypeJWT)
		}
		c.Next()
	}, h.ResourceScopeGate()}
	chain = append(chain, gates...)
	chain = append(chain, func(c *gin.Context) {
		seen = c.Copy()
		c.Status(http.StatusOK)
	})
	r.Handle(method, route, chain...)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), method, path, nil))
	return rec, seen
}

func TestResourceScopeGateRefusesRoutesTheScopeCannotFilter(t *testing.T) {
	orgID := uuid.New()
	orgs := &scopeOrgs{scope: &models.ResourceScope{}}
	h := &Handler{OrganizationService: orgs}
	member := restrictedMember(orgID)

	rec, _ := serveScoped(t, h, member, nil, http.MethodPost, "/v1/contacts/search", "/v1/contacts/search")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("contacts search: status %d, want 403", rec.Code)
	}
	var body struct{ Code string }
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != MemberAccessRestricted {
		t.Fatalf("code = %q, want %q", body.Code, MemberAccessRestricted)
	}
	// A write on a scope-aware path is still refused: the allowlist is per method.
	if rec, _ := serveScoped(t, h, member, nil, http.MethodPatch, "/v1/campaigns/:id", "/v1/campaigns/"+uuid.NewString()); rec.Code != http.StatusForbidden {
		t.Fatalf("campaign write: status %d, want 403", rec.Code)
	}
	if rec, _ := serveScoped(t, h, member, nil, http.MethodGet, "/v1/campaigns", "/v1/campaigns"); rec.Code != http.StatusOK {
		t.Fatalf("campaign list: status %d, want 200", rec.Code)
	}
	if orgs.resolved != 1 {
		t.Fatalf("scope resolved %d times, want once for the one allowed request", orgs.resolved)
	}
}

func TestResourceScopeGateLeavesWorkspaceMembersAlone(t *testing.T) {
	orgs := &scopeOrgs{}
	h := &Handler{OrganizationService: orgs}
	for _, m := range []*models.OrganizationMember{
		{OrganizationID: uuid.New(), UserID: uuid.New(), Role: "Viewer", AccessScope: models.AccessScopeWorkspace},
		// The owner is never restricted, whatever a stale row says.
		{OrganizationID: uuid.New(), UserID: uuid.New(), Role: string(models.RoleOwner), AccessScope: models.AccessScopeRestricted},
	} {
		rec, seen := serveScoped(t, h, m, nil, http.MethodPost, "/v1/contacts/search", "/v1/contacts/search")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d, want 200", m.Role, rec.Code)
		}
		if AllowedCampaigns(seen) != nil || AllowedEmailAccounts(seen) != nil {
			t.Fatalf("%s: workspace member got an allowlist", m.Role)
		}
	}
	if orgs.resolved != 0 {
		t.Fatalf("scope resolved for a workspace member")
	}
}

func TestRestrictedMemberAllowlists(t *testing.T) {
	orgID := uuid.New()
	granted, other, keyOnly := uuid.New(), uuid.New(), uuid.New()
	campaign := uuid.New()

	t.Run("an empty grant matches nothing", func(t *testing.T) {
		h := &Handler{OrganizationService: &scopeOrgs{scope: &models.ResourceScope{}}}
		_, c := serveScoped(t, h, restrictedMember(orgID), nil, http.MethodGet, "/v1/campaigns", "/v1/campaigns")
		for name, list := range map[string][]uuid.UUID{"campaigns": AllowedCampaigns(c), "mailboxes": AllowedEmailAccounts(c), "folders": AllowedFolders(c)} {
			if len(list) == 0 {
				t.Fatalf("%s: empty allowlist reads as unrestricted", name)
			}
		}
		if CampaignAllowed(c, uuid.Nil) || EmailAccountAllowed(c, uuid.Nil) {
			t.Fatal("the none-match sentinel was treated as a grant")
		}
	})

	t.Run("grants are the allowlist", func(t *testing.T) {
		h := &Handler{OrganizationService: &scopeOrgs{scope: &models.ResourceScope{Campaigns: []uuid.UUID{campaign}, Mailboxes: []uuid.UUID{granted}}}}
		_, c := serveScoped(t, h, restrictedMember(orgID), nil, http.MethodGet, "/v1/campaigns", "/v1/campaigns")
		if !CampaignAllowed(c, campaign) || CampaignAllowed(c, uuid.New()) {
			t.Fatal("campaign allowlist wrong")
		}
		if !EmailAccountAllowed(c, granted) || EmailAccountAllowed(c, other) {
			t.Fatal("mailbox allowlist wrong")
		}
	})

	t.Run("an API key is held to its own list and its holder's grants", func(t *testing.T) {
		h := &Handler{OrganizationService: &scopeOrgs{scope: &models.ResourceScope{Mailboxes: []uuid.UUID{granted, other}}}}
		_, c := serveScoped(t, h, restrictedMember(orgID), []uuid.UUID{granted, keyOnly}, http.MethodGet, "/v1/emails", "/v1/emails")
		got := AllowedEmailAccounts(c)
		if len(got) != 1 || got[0] != granted {
			t.Fatalf("allowlist = %v, want only %v", got, granted)
		}
		_, c = serveScoped(t, h, restrictedMember(orgID), []uuid.UUID{keyOnly}, http.MethodGet, "/v1/emails", "/v1/emails")
		if got := AllowedEmailAccounts(c); len(got) == 0 || EmailAccountAllowed(c, keyOnly) {
			t.Fatalf("disjoint key and grants must allow nothing, got %v", got)
		}
	})
}

func TestRequireCampaignParamHidesOutOfScopeCampaigns(t *testing.T) {
	orgID := uuid.New()
	campaign := uuid.New()
	h := &Handler{OrganizationService: &scopeOrgs{scope: &models.ResourceScope{Campaigns: []uuid.UUID{campaign}}}}
	for id, want := range map[uuid.UUID]int{campaign: http.StatusOK, uuid.New(): http.StatusNotFound} {
		rec, _ := serveScoped(t, h, restrictedMember(orgID), nil, http.MethodGet, "/v1/campaigns/:id", "/v1/campaigns/"+id.String(), RequireCampaignParam("id"))
		if rec.Code != want {
			t.Fatalf("campaign %s: status %d, want %d", id, rec.Code, want)
		}
	}
}
