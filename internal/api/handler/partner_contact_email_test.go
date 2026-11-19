package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/app/user"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	partnerLogin   = "p-0123456789abcdef01234567@graphkit.partner.invalid"
	partnerService = "service-0123456789abcdef@graphkit.partner.invalid"
	partnerContact = "ada@acme.com"
)

// partnerPerson is a person a partner signs in, as the user repository reads it.
func partnerPerson(id uuid.UUID) *models.User {
	return &models.User{ID: id, FirstName: "Ada", LastName: "Lovelace", Email: partnerLogin, ContactEmail: partnerContact}
}

type partnerPersonUsers struct{ user.UserService }

func (partnerPersonUsers) GetUser(_ context.Context, id uuid.UUID) (*models.User, *errx.Error) {
	return partnerPerson(id), nil
}

// partnerMembersRepo answers the member list the way the database does: login address plus contact email.
type partnerMembersRepo struct {
	repository.OrganizationRepository
	person, service uuid.UUID
}

func (r partnerMembersRepo) GetMembers(_ context.Context, orgID uuid.UUID) ([]models.OrganizationMember, error) {
	person := partnerPerson(r.person)
	owner := &models.User{ID: r.service, FirstName: "GraphKit", Email: partnerService}
	return []models.OrganizationMember{
		{OrganizationID: orgID, UserID: r.service, Role: "owner", User: owner, Email: owner.VisibleEmail(), Name: "GraphKit"},
		{OrganizationID: orgID, UserID: r.person, Role: "admin", User: person, Email: person.VisibleEmail(), Name: "Ada Lovelace"},
	}, nil
}

func (partnerMembersRepo) HydrateMemberRoles(context.Context, uuid.UUID, []models.OrganizationMember) error {
	return nil
}

func (partnerMembersRepo) ListMemberAccess(context.Context, uuid.UUID) (map[uuid.UUID]*models.MemberAccess, error) {
	return map[uuid.UUID]*models.MemberAccess{}, nil
}

func TestPartnerPersonResponsesShowContactEmailNeverLoginAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	org, person, service := uuid.New(), uuid.New(), uuid.New()
	h := &Handler{
		UserService:         partnerPersonUsers{},
		OrganizationService: organization.NewService(partnerMembersRepo{person: person, service: service}, nil, nil, nil, nil),
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.UserIDKey, person.String())
		if c.Request.URL.Path == "/organization/members" {
			c.Set(middleware.OrganizationIDKey, org)
		}
	})
	router.GET("/auth/me", h.GetUser)
	router.GET("/me", h.GetIdentity)
	router.GET("/organization/members", h.GetMembers)

	for _, path := range []string{"/auth/me", "/me", "/organization/members"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if strings.Contains(body, ".invalid") {
				t.Fatalf("response carries a sign-in address: %s", body)
			}
			if !strings.Contains(body, `"email":"`+partnerContact+`"`) {
				t.Fatalf("response does not show the contact email: %s", body)
			}
		})
	}
}

func TestVisibleEmail(t *testing.T) {
	for _, tc := range []struct{ login, contact, want string }{
		{"ada@acme.com", "", "ada@acme.com"},
		{"ada@acme.com", "other@acme.com", "ada@acme.com"},
		{partnerLogin, partnerContact, partnerContact},
		{partnerLogin, "", ""},
		{strings.ToUpper(partnerLogin), "x@y.invalid", ""},
	} {
		if got := models.VisibleEmail(tc.login, tc.contact); got != tc.want {
			t.Errorf("VisibleEmail(%q, %q) = %q; want %q", tc.login, tc.contact, got, tc.want)
		}
	}
}
