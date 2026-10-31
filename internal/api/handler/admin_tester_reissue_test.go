package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/auth"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/repository"
)

type reissueUsers struct {
	repository.UserRepository
	tester   bool
	missing  bool
	hash     string
	until    time.Time
	reissued int
	log      *[]string
}

func (r *reissueUsers) IsCreatedTester(context.Context, uuid.UUID) (bool, error) {
	if r.missing {
		return false, repository.ErrUserNotFound
	}
	return r.tester, nil
}

func (r *reissueUsers) ReissueTesterPassword(_ context.Context, _ uuid.UUID, hash string) (time.Time, error) {
	if !r.tester {
		return time.Time{}, repository.ErrNotTester
	}
	r.reissued++
	r.hash = hash
	r.until = time.Now().Add(48 * time.Hour)
	*r.log = append(*r.log, "store")
	return r.until, nil
}

type reissueTokens struct {
	token.TokenService
	log *[]string
}

func (t *reissueTokens) RevokeOtherSessions(context.Context, uuid.UUID, uuid.UUID) *errx.Error {
	*t.log = append(*t.log, "revoke")
	return nil
}

type reissueAuth struct {
	auth.AuthService
	log *[]string
}

func (a *reissueAuth) ForgetTrustedDevices(context.Context, uuid.UUID) *errx.Error {
	*a.log = append(*a.log, "forget")
	return nil
}

type reissueAdmins struct {
	organization.OrganizationService
	perms models.AdminPermission
}

func (o reissueAdmins) GetUserAdminPermissions(context.Context, uuid.UUID) (uint32, error) {
	return uint32(o.perms), nil
}

// reissueRouter registers the handler behind the same gates routes.go gives it.
func reissueRouter(h *Handler, session *models.Session, perms models.AdminPermission) *gin.Engine {
	gin.SetMode(gin.TestMode)
	m := &middleware.Handler{OrganizationService: reissueAdmins{perms: perms}}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if session != nil {
			c.Set(middleware.SessionKey, session)
		}
		c.Next()
	})
	r.POST("/admin/testers/:id/password", m.AdminMiddleware(), middleware.RequireAdminPermission(models.AdminPermManageTesters), middleware.RequireFreshAuth(), h.AdminReissueTesterPassword)
	return r
}

func reissue(r *gin.Engine) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/testers/"+uuid.NewString()+"/password", strings.NewReader("")))
	return w
}

func reissueFixture(tester bool) (*Handler, *reissueUsers, *[]string) {
	var log []string
	users := &reissueUsers{tester: tester, log: &log}
	h := &Handler{UserRepo: users, TokenService: &reissueTokens{log: &log}, AuthService: &reissueAuth{log: &log}}
	return h, users, &log
}

// A fresh, MFA-verified admin with manage_testers gets a new 48-hour initial
// password; sessions and remembered devices end before it is stored.
func TestAdminReissueTesterPassword(t *testing.T) {
	h, users, log := reissueFixture(true)
	now := time.Now()
	w := reissue(reissueRouter(h, &models.Session{UserID: uuid.New(), MFAVerified: true, ReauthAt: &now}, models.AdminPermManageTesters))
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	var res adminReissueTesterPasswordResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Password, "Tester-") || len(res.Password) != len("Tester-")+20 || !res.PasswordChangeRequired {
		t.Fatalf("response %+v", res)
	}
	if ok, _ := argon2.Verify(res.Password, users.hash); !ok {
		t.Fatal("the stored hash is not the returned password's")
	}
	if left := time.Until(res.PasswordExpiresAt); left > 48*time.Hour || left < 47*time.Hour {
		t.Fatalf("expires in %v", left)
	}
	if got := strings.Join(*log, ","); got != "revoke,forget,store,revoke" {
		t.Fatalf("order = %s, want sessions and devices ended before the store", got)
	}
}

// Nothing is revoked or stored unless every gate passes and the account was created as a tester.
func TestAdminReissueTesterPasswordRefusals(t *testing.T) {
	now, stale := time.Now(), time.Now().Add(-time.Hour)
	for _, tc := range []struct {
		name    string
		session *models.Session
		perms   models.AdminPermission
		tester  bool
		missing bool
		code    int
	}{
		{"not created as a tester", &models.Session{UserID: uuid.New(), MFAVerified: true, ReauthAt: &now}, models.AdminPermManageTesters, false, false, http.StatusNotFound},
		{"unknown account", &models.Session{UserID: uuid.New(), MFAVerified: true, ReauthAt: &now}, models.AdminPermManageTesters, true, true, http.StatusNotFound},
		{"admin without a second factor", &models.Session{UserID: uuid.New(), ReauthAt: &now}, models.AdminPermManageTesters, true, false, http.StatusForbidden},
		{"admin without manage_testers", &models.Session{UserID: uuid.New(), MFAVerified: true, ReauthAt: &now}, models.AdminPermViewUsers, true, false, http.StatusForbidden},
		{"no recent confirmation", &models.Session{UserID: uuid.New(), MFAVerified: true, ReauthAt: &stale}, models.AdminPermManageTesters, true, false, http.StatusForbidden},
		{"not an admin", &models.Session{UserID: uuid.New(), MFAVerified: true, ReauthAt: &now}, 0, true, false, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, users, log := reissueFixture(tc.tester)
			users.missing = tc.missing
			w := reissue(reissueRouter(h, tc.session, tc.perms))
			if w.Code != tc.code {
				t.Fatalf("HTTP %d, want %d: %s", w.Code, tc.code, w.Body.String())
			}
			if users.reissued != 0 || len(*log) != 0 {
				t.Fatalf("a refused call did %v", *log)
			}
		})
	}
}
