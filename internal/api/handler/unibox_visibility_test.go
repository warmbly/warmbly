package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/group"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/unibox"
	"github.com/warmbly/warmbly/internal/app/user"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type visibilityUser struct{ user.UserService }

func (*visibilityUser) GetUser(_ context.Context, id uuid.UUID) (*models.User, *errx.Error) {
	return &models.User{ID: id, Categories: []models.Group{{ID: uuid.New(), Title: "Cached category"}}}, nil
}

type visibilityGroup struct {
	group.GroupService
	groups []models.Group
	calls  int
}

func (s *visibilityGroup) List(context.Context, uuid.UUID) ([]models.Group, *errx.Error) {
	s.calls++
	return s.groups, nil
}

type visibilityOrganization struct {
	organization.OrganizationService
	scope *models.ResourceScope
}

func (s *visibilityOrganization) ResolveMemberScope(context.Context, uuid.UUID, uuid.UUID) (*models.ResourceScope, *errx.Error) {
	return s.scope, nil
}

type visibilityUnibox struct {
	unibox.UniboxService
	groups  []models.Group
	org     uuid.UUID
	allowed []uuid.UUID
	calls   int
	err     *errx.Error
}

func (s *visibilityUnibox) CategoriesForMailboxes(_ context.Context, org uuid.UUID, allowed []uuid.UUID) ([]models.Group, *errx.Error) {
	s.org, s.allowed, s.calls = org, allowed, s.calls+1
	return s.groups, s.err
}

func (s *visibilityUnibox) ListThreadLabelsWithin(_ context.Context, org uuid.UUID, _ string, allowed []uuid.UUID) ([]models.MiniCategory, *errx.Error) {
	s.org, s.allowed, s.calls = org, allowed, s.calls+1
	return []models.MiniCategory{}, s.err
}

func TestAuthMeCategoriesUseMailboxGrants(t *testing.T) {
	gin.SetMode(gin.TestMode)
	org, uid, mailbox, otherMailbox, campaign := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	visible := models.Group{ID: uuid.New(), Title: "Visible"}
	hidden := models.Group{ID: uuid.New(), Title: "Hidden"}
	for _, tc := range []struct {
		name       string
		restricted bool
		mailboxes  []uuid.UUID
		groups     []models.Group
		err        *errx.Error
		want       int
	}{
		{"mixed grants", true, []uuid.UUID{mailbox, otherMailbox}, []models.Group{visible}, nil, 1},
		{"empty grants", true, []uuid.UUID{}, []models.Group{}, nil, 0},
		{"nil grants", true, nil, []models.Group{}, nil, 0},
		{"registry error fails closed", true, []uuid.UUID{mailbox}, nil, errx.InternalError(), 0},
		{"unrestricted registry", false, nil, nil, nil, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ub := &visibilityUnibox{groups: tc.groups, err: tc.err}
			category := &visibilityGroup{groups: []models.Group{visible, hidden}}
			scope := &models.ResourceScope{Mailboxes: tc.mailboxes, Campaigns: []uuid.UUID{campaign}}
			h := &Handler{
				UserService: &visibilityUser{}, UniboxService: ub, CategoryService: category,
				FolderService: &visibilityGroup{}, TagService: &visibilityGroup{},
				OrganizationService: &visibilityOrganization{scope: scope},
			}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(middleware.UserIDKey, uid.String())
				c.Set(middleware.OrganizationIDKey, org)
				if tc.restricted {
					c.Set(middleware.SessionMemberKey, &models.OrganizationMember{UserID: uid, AccessScope: models.AccessScopeRestricted})
				}
			})
			router.GET("/auth/me", h.GetUser)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/me", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			var response models.User
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if len(response.Categories) != tc.want || response.Categories == nil {
				t.Fatalf("categories = %+v; want %d entries in a non-nil list", response.Categories, tc.want)
			}
			if tc.restricted {
				if category.calls != 0 || ub.calls != 1 || ub.org != org || !reflect.DeepEqual(ub.allowed, tc.mailboxes) {
					t.Fatalf("category scope: registry calls %d, inbox calls %d, org %s, allowed %v", category.calls, ub.calls, ub.org, ub.allowed)
				}
				if len(response.Categories) > 0 && response.Categories[0].ID != visible.ID {
					t.Fatalf("category from hidden or cached data: %+v", response.Categories)
				}
			} else if category.calls != 1 || ub.calls != 0 {
				t.Fatalf("unrestricted registry calls %d, inbox calls %d", category.calls, ub.calls)
			}
		})
	}
}

func TestUniboxThreadLabelsUseEffectiveMailboxGrants(t *testing.T) {
	gin.SetMode(gin.TestMode)
	org, mailbox, keyOnly := uuid.New(), uuid.New(), uuid.New()
	for _, tc := range []struct {
		name       string
		restricted bool
		scope      *models.ResourceScope
		key        []uuid.UUID
		want       []uuid.UUID
	}{
		{"member", true, &models.ResourceScope{Mailboxes: []uuid.UUID{mailbox}}, nil, []uuid.UUID{mailbox}},
		{"empty grants", true, &models.ResourceScope{}, nil, models.NoneMatch()},
		{"missing scope", true, nil, nil, models.NoneMatch()},
		{"intersection", true, &models.ResourceScope{Mailboxes: []uuid.UUID{mailbox}}, []uuid.UUID{mailbox, keyOnly}, []uuid.UUID{mailbox}},
		{"disjoint grants", true, &models.ResourceScope{Mailboxes: []uuid.UUID{mailbox}}, []uuid.UUID{keyOnly}, models.NoneMatch()},
		{"key only", false, nil, []uuid.UUID{mailbox}, []uuid.UUID{mailbox}},
		{"unrestricted", false, nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ub := &visibilityUnibox{err: errx.New(errx.NotFound, "thread not found")}
			h := &Handler{UniboxService: ub}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(middleware.OrganizationIDKey, org)
				if tc.restricted {
					c.Set(middleware.SessionMemberKey, &models.OrganizationMember{AccessScope: models.AccessScopeRestricted})
				}
				if tc.scope != nil {
					c.Set(middleware.ResourceScopeKey, tc.scope)
				}
				if tc.key != nil {
					c.Set(middleware.AuthTypeKey, middleware.AuthTypeAPIKey)
					c.Set(middleware.APIKeyAllowedEmailAccountsKey, tc.key)
				}
			})
			router.GET("/unibox/thread/labels", h.GetUniboxThreadLabels)
			var notFound string
			for _, thread := range []string{"inaccessible", "unknown", "foreign"} {
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/unibox/thread/labels?thread_id="+thread, nil))
				if rec.Code != http.StatusNotFound || ub.org != org || !reflect.DeepEqual(ub.allowed, tc.want) {
					t.Fatalf("status %d: %s, org %s, allowed %v; want %v", rec.Code, rec.Body.String(), ub.org, ub.allowed, tc.want)
				}
				if notFound != "" && notFound != rec.Body.String() {
					t.Fatalf("thread existence leaked: %s != %s", notFound, rec.Body.String())
				}
				notFound = rec.Body.String()
			}
			if ub.calls != 3 {
				t.Fatalf("scoped label reader called %d times", ub.calls)
			}
		})
	}
}
