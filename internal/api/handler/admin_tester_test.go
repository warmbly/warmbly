package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type testerUsers struct {
	repository.UserRepository
	userID uuid.UUID
	until  time.Time
	// stored, when set, is the expiry the database kept in place of the requested one.
	stored *time.Time
}

func (r *testerUsers) GetUserByEmail(context.Context, string) (*models.User, error) { return nil, nil }

func (r *testerUsers) CreateExemptUser(_ context.Context, email *mail.Address, _, _ string, _ *uuid.UUID, until time.Time) (*models.User, error) {
	r.until = until
	return &models.User{ID: r.userID, Email: email.Address, PasswordExpiresAt: r.stored}, nil
}

type testerWorkspaces struct {
	organization.OrganizationService
	orgID, actor, roleID uuid.UUID
	until                time.Time
	created, joined      bool
}

func (s *testerWorkspaces) CreateTesterWorkspace(_ context.Context, _, actor uuid.UUID, _, _ string, until time.Time) (*models.Organization, *errx.Error) {
	s.created, s.actor, s.until = true, actor, until
	return &models.Organization{ID: s.orgID, Category: models.OrganizationCategoryTest}, nil
}

func (s *testerWorkspaces) AttachTester(_ context.Context, orgID, _, actor, roleID uuid.UUID) (*models.OrganizationMember, *errx.Error) {
	s.joined, s.orgID, s.actor, s.roleID = true, orgID, actor, roleID
	return &models.OrganizationMember{OrganizationID: orgID}, nil
}

func TestAdminCreateTesterOnlyGrantsDedicatedWorkspaces(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, existing := range []bool{false, true} {
		actor, orgID, roleID := uuid.New(), uuid.New(), uuid.New()
		users := &testerUsers{userID: uuid.New()}
		orgs := &testerWorkspaces{orgID: orgID}
		h := &Handler{UserRepo: users, OrganizationService: orgs}
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set(middleware.AdminUserIDKey, actor) })
		r.POST("/testers", h.AdminCreateTester)
		req := adminCreateTesterRequest{Email: "reviewer@example.test", Reason: "OAuth review", PasswordHours: 24}
		if existing {
			req.OrgID, req.RoleID = &orgID, &roleID
		}
		raw, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/testers", strings.NewReader(string(raw))))
		if w.Code != http.StatusOK {
			t.Fatalf("create tester existing=%v: HTTP %d", existing, w.Code)
		}
		var response adminCreateTesterResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Joined != existing || orgs.created == existing || orgs.joined != existing || orgs.actor != actor {
			t.Fatalf("wrong provisioning path for existing=%v", existing)
		}
		if existing && orgs.roleID != roleID {
			t.Fatal("existing workspace role changed")
		}
		if !existing && !orgs.until.Equal(users.until) {
			t.Fatal("test entitlement did not use the reviewer password expiry")
		}
		if !response.PasswordExpiresAt.Equal(users.until) || !strings.HasPrefix(response.Password, "Tester-") {
			t.Fatal("reviewer credentials response is incomplete")
		}
	}
}

type testerSampleService struct {
	organization.OrganizationService
	orgID, actor uuid.UUID
	ip, agent    string
	called       bool
	err          *errx.Error
}

func (s *testerSampleService) SeedTesterWorkspace(_ context.Context, orgID, actor uuid.UUID, ip, agent string) (*models.TesterSampleData, *errx.Error) {
	s.called, s.orgID, s.actor, s.ip, s.agent = true, orgID, actor, ip, agent
	return &models.TesterSampleData{OrganizationID: orgID, Created: true, SeededAt: time.Now()}, s.err
}

func TestAdminSeedTesterWorkspaceRequiresAuthorityAndValidTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	actor, orgID := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name    string
		perms   models.AdminPermission
		actor   bool
		id      string
		missing bool
		err     *errx.Error
		status  int
		called  bool
	}{
		{name: "allowed", perms: models.AdminPermManageTesters, actor: true, id: orgID.String(), status: http.StatusOK, called: true},
		{name: "view only", perms: models.AdminPermViewUsers, actor: true, id: orgID.String(), status: http.StatusForbidden},
		{name: "no actor", perms: models.AdminPermManageTesters, id: orgID.String(), status: http.StatusUnauthorized},
		{name: "invalid target", perms: models.AdminPermManageTesters, actor: true, id: "not-a-uuid", status: http.StatusBadRequest},
		{name: "nil target", perms: models.AdminPermManageTesters, actor: true, id: uuid.Nil.String(), status: http.StatusBadRequest},
		{name: "inactive workspace", perms: models.AdminPermManageTesters, actor: true, id: orgID.String(), err: errx.New(errx.BadRequest, "inactive"), status: http.StatusBadRequest, called: true},
		{name: "unavailable", perms: models.AdminPermManageTesters, actor: true, id: orgID.String(), missing: true, status: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &testerSampleService{err: tc.err}
			h := &Handler{}
			if !tc.missing {
				h.OrganizationService = s
			}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(middleware.AdminPermissionsKey, tc.perms)
				if tc.actor {
					c.Set(middleware.AdminUserIDKey, actor)
				}
			})
			r.POST("/organizations/:id/sample-data", middleware.RequireAdminPermission(models.AdminPermManageTesters), h.AdminSeedTesterWorkspace)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/organizations/"+tc.id+"/sample-data", nil)
			req.Header.Set("User-Agent", "sample-review")
			r.ServeHTTP(w, req)
			if w.Code != tc.status || s.called != tc.called {
				t.Fatalf("HTTP %d, called=%v: %s", w.Code, s.called, w.Body.String())
			}
			if s.called && (s.orgID != orgID || s.actor != actor || s.agent != "sample-review" || s.ip == "") {
				t.Fatal("target or audit attribution changed")
			}
		})
	}
}

func TestTesterPasswordLifetimeIsAtMostTwoDays(t *testing.T) {
	for _, tc := range []struct {
		req  adminCreateTesterRequest
		want time.Duration
		ok   bool
	}{
		{adminCreateTesterRequest{}, 48 * time.Hour, true},
		{adminCreateTesterRequest{PasswordHours: 6}, 6 * time.Hour, true},
		{adminCreateTesterRequest{PasswordDays: 2}, 48 * time.Hour, true},
		{adminCreateTesterRequest{PasswordHours: 49}, 0, false},
		{adminCreateTesterRequest{PasswordDays: 30}, 0, false},
		{adminCreateTesterRequest{PasswordHours: -1}, 0, false},
	} {
		got, err := testerPasswordLifetime(tc.req)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("%+v: got %v, err %v; want %v, ok %v", tc.req, got, err, tc.want, tc.ok)
		}
	}
}

// The response and the workspace grant carry the expiry the database stored, and the password is marked initial.
func TestAdminCreateTesterUsesTheStoredExpiry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stored := time.Now().Add(47 * time.Hour).Truncate(time.Second)
	users := &testerUsers{userID: uuid.New(), stored: &stored}
	orgs := &testerWorkspaces{orgID: uuid.New()}
	h := &Handler{UserRepo: users, OrganizationService: orgs}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.AdminUserIDKey, uuid.New()) })
	r.POST("/testers", h.AdminCreateTester)
	body := `{"email":"` + "reviewer" + "@example.test" + `","reason":"OAuth review"}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/testers", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d", w.Code)
	}
	var response adminCreateTesterResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.PasswordExpiresAt.Equal(stored) || !orgs.until.Equal(stored) {
		t.Fatalf("response %v, grant %v; want the stored %v", response.PasswordExpiresAt, orgs.until, stored)
	}
	if !response.PasswordChangeRequired {
		t.Fatal("an issued password was not marked as one to change")
	}
}
