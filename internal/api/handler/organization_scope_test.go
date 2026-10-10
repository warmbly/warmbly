package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type currentOrganizationService struct {
	organization.OrganizationService
	org         *models.Organization
	counts      *models.OrganizationCounts
	countsErr   *errx.Error
	getErr      *errx.Error
	limits      *models.OrganizationLimits
	scope       *models.ResourceScope
	countOrg    uuid.UUID
	scopedCalls int
	wideCalls   int
}

func (s *currentOrganizationService) Get(context.Context, uuid.UUID) (*models.Organization, *errx.Error) {
	return s.org, s.getErr
}

func (s *currentOrganizationService) GetScopedOrganizationCounts(_ context.Context, org uuid.UUID, scope *models.ResourceScope) (*models.OrganizationCounts, *errx.Error) {
	s.scopedCalls++
	s.countOrg, s.scope = org, scope
	return s.counts, s.countsErr
}

func (s *currentOrganizationService) GetOrganizationCounts(context.Context, uuid.UUID) (*models.OrganizationCounts, *errx.Error) {
	s.wideCalls++
	return s.counts, s.countsErr
}

func (s *currentOrganizationService) GetOrganizationLimits(context.Context, uuid.UUID) (*models.OrganizationLimits, *errx.Error) {
	return s.limits, nil
}

func TestCurrentOrganizationScopesCountsAndRedactsWorkspaceOnlyFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, restricted := range []bool{false, true} {
		name := "workspace"
		if restricted {
			name = "restricted"
		}
		t.Run(name, func(t *testing.T) {
			orgID, campaignID, mailboxID := uuid.New(), uuid.New(), uuid.New()
			s := &currentOrganizationService{
				org:    &models.Organization{ID: orgID, Name: "Acme", Timezone: "Europe/Budapest", ProductDescription: "Private product", ICPNotes: "Private ICP", VoiceProfile: "Private voice"},
				counts: &models.OrganizationCounts{TotalCampaigns: 3, ActiveCampaigns: 2, TotalContacts: 4, TotalMembers: 99, EmailAccounts: 1, EmailsSentToday: 5},
				limits: &models.OrganizationLimits{},
			}
			h := &Handler{OrganizationService: s}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(middleware.OrganizationIDKey, orgID)
				if restricted {
					c.Set(middleware.SessionMemberKey, &models.OrganizationMember{Role: "Viewer", AccessScope: models.AccessScopeRestricted})
					c.Set(middleware.ResourceScopeKey, &models.ResourceScope{Campaigns: []uuid.UUID{campaignID}, Mailboxes: []uuid.UUID{mailboxID, uuid.New()}})
					c.Set(middleware.AuthTypeKey, middleware.AuthTypeAPIKey)
					c.Set(middleware.APIKeyAllowedEmailAccountsKey, []uuid.UUID{mailboxID})
				}
			})
			r.GET("/v1/organization/current", h.GetCurrentOrganization)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/organization/current", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"product_description", "icp_notes", "voice_profile"} {
				_, present := body[key]
				if present == restricted {
					t.Fatalf("%s presence=%v for restricted=%v", key, present, restricted)
				}
			}
			var counts map[string]int
			if err := json.Unmarshal(body["counts"], &counts); err != nil {
				t.Fatal(err)
			}
			_, membersPresent := counts["total_members"]
			if membersPresent == restricted {
				t.Fatalf("member count presence=%v for restricted=%v", membersPresent, restricted)
			}
			for key, want := range map[string]int{"total_campaigns": 3, "active_campaigns": 2, "total_contacts": 4, "email_accounts": 1, "emails_sent_today": 5} {
				if value, present := counts[key]; !present || value != want {
					t.Fatalf("%s=%d present=%v, want %d", key, value, present, want)
				}
			}
			for _, key := range []string{"id", "name", "timezone", "limits"} {
				if _, present := body[key]; !present {
					t.Fatalf("compatible workspace field %s missing", key)
				}
			}
			if restricted {
				if s.wideCalls != 0 || s.scopedCalls != 1 || s.countOrg != orgID || len(s.scope.Campaigns) != 1 || s.scope.Campaigns[0] != campaignID || len(s.scope.Mailboxes) != 1 || s.scope.Mailboxes[0] != mailboxID {
					t.Fatalf("wrong scoped count query: %+v", s)
				}
			} else if s.wideCalls != 1 || s.scopedCalls != 0 {
				t.Fatalf("unrestricted query changed: %+v", s)
			}
		})
	}
}

func TestCurrentOrganizationRestrictedFailuresDoNotReturnWorkspaceData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		scope  *models.ResourceScope
		counts *models.OrganizationCounts
		err    *errx.Error
	}{
		{name: "unresolved grants", counts: &models.OrganizationCounts{}},
		{name: "count error", scope: &models.ResourceScope{}, err: errx.InternalError()},
		{name: "nil counts", scope: &models.ResourceScope{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &currentOrganizationService{org: &models.Organization{ID: uuid.New(), VoiceProfile: "private"}, counts: tc.counts, countsErr: tc.err}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(middleware.OrganizationIDKey, s.org.ID)
				c.Set(middleware.SessionMemberKey, &models.OrganizationMember{AccessScope: models.AccessScopeRestricted})
				if tc.scope != nil {
					c.Set(middleware.ResourceScopeKey, tc.scope)
				}
			})
			r.GET("/v1/organization/current", (&Handler{OrganizationService: s}).GetCurrentOrganization)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/organization/current", nil))
			if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "voice_profile") || strings.Contains(w.Body.String(), "total_members") || s.wideCalls != 0 {
				t.Fatalf("failure returned workspace data: HTTP %d %s", w.Code, w.Body.String())
			}
		})
	}
}

type creationPolicyRepo struct {
	repository.OrganizationRepository
	members []models.OrganizationMember
	admin   uint32
	created bool
	user    uuid.UUID
}

func (r *creationPolicyRepo) GetUserOrganizations(_ context.Context, user uuid.UUID) ([]models.OrganizationMember, error) {
	r.user = user
	return r.members, nil
}

func (r *creationPolicyRepo) GetUserAdminPermissions(context.Context, uuid.UUID) (uint32, error) {
	return r.admin, nil
}
func (r *creationPolicyRepo) Create(context.Context, *models.Organization) error {
	r.created = true
	return nil
}
func (*creationPolicyRepo) AddMember(context.Context, *models.OrganizationMember) error { return nil }
func (*creationPolicyRepo) CreateRole(context.Context, *models.OrganizationRole) error  { return nil }

type creationPolicyUsers struct{ repository.UserRepository }

func (creationPolicyUsers) GetBanState(context.Context, uuid.UUID) (uint32, error) { return 0, nil }
func (creationPolicyUsers) GetUser(_ context.Context, id uuid.UUID) (*models.User, error) {
	return &models.User{ID: id, MaxOrganizations: 5}, nil
}

type organizationScopeAudit struct{ audit.AuditService }

func (organizationScopeAudit) LogAction(context.Context, uuid.UUID, uuid.UUID, models.AuditAction, models.AuditEntityType, *uuid.UUID, string, string, map[string]string, map[string]string) {
}

func TestOrganizationCreationPolicyAcrossCredentialAndSelectionContexts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("DEPLOYMENT_MODE", "self_hosted")
	for _, auth := range []string{middleware.AuthTypeJWT, middleware.AuthTypeAPIKey, middleware.AuthTypeOAuth} {
		for _, selected := range []bool{false, true} {
			for _, admin := range []bool{false, true} {
				name := auth + "/unselected/member"
				if selected {
					name = auth + "/selected/member"
				}
				if admin {
					name += "/admin"
				}
				t.Run(name, func(t *testing.T) {
					user := uuid.New()
					repo := &creationPolicyRepo{members: []models.OrganizationMember{{UserID: user, Role: "Viewer", AccessScope: models.AccessScopeRestricted}}}
					if admin {
						repo.admin = uint32(models.AdminPermViewUsers)
					}
					service := organization.NewService(repo, nil, creationPolicyUsers{}, nil, nil)
					h := &Handler{OrganizationService: service, AuditService: organizationScopeAudit{}}
					gate := &middleware.Handler{OrganizationService: service}
					r := gin.New()
					r.Use(func(c *gin.Context) {
						c.Set(middleware.AuthTypeKey, auth)
						c.Set(middleware.UserIDKey, user.String())
						if selected {
							c.Set(middleware.OrganizationIDKey, uuid.New())
							c.Set(middleware.SessionMemberKey, &repo.members[0])
						}
					})
					r.POST("/v1/organization", gate.ResourceScopeGate(), h.CreateOrganization)
					w := httptest.NewRecorder()
					r.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/organization", strings.NewReader(`{"name":"Acme","timezone":"UTC"}`)))
					want := http.StatusForbidden
					if admin {
						want = http.StatusCreated
					}
					if w.Code != want || repo.created != admin || repo.user != user {
						t.Fatalf("HTTP %d %s; want %d created=%v checked creator=%v", w.Code, w.Body.String(), want, repo.created, repo.user == user)
					}
				})
			}
		}
	}
}

type organizationLimitPolicyRepo struct {
	repository.OrganizationRepository
	request                 *models.LimitIncreaseRequest
	member                  *models.OrganizationMember
	checkedOrg, checkedUser uuid.UUID
	listed, updated         bool
}

func (r *organizationLimitPolicyRepo) GetMember(_ context.Context, org, user uuid.UUID) (*models.OrganizationMember, error) {
	r.checkedOrg, r.checkedUser = org, user
	return r.member, nil
}
func (r *organizationLimitPolicyRepo) GetLimitRequest(context.Context, uuid.UUID) (*models.LimitIncreaseRequest, error) {
	return r.request, nil
}
func (r *organizationLimitPolicyRepo) ListLimitRequestsForOrg(context.Context, uuid.UUID) ([]models.LimitIncreaseRequest, error) {
	r.listed = true
	return []models.LimitIncreaseRequest{*r.request}, nil
}
func (r *organizationLimitPolicyRepo) UpdateLimitRequestStatus(context.Context, uuid.UUID, models.LimitRequestStatus, uuid.UUID, string) error {
	r.updated = true
	return nil
}

func TestOrganizationLimitHandlersAuthorizeTheTargetNotTheSelectedWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, selected := range []string{"none", "workspace", "restricted"} {
		for _, targetRestricted := range []bool{false, true} {
			for _, operation := range []string{"submit", "list", "cancel"} {
				if operation == "submit" && !targetRestricted {
					continue
				}
				name := selected + "/workspace-target/" + operation
				if targetRestricted {
					name = selected + "/restricted-target/" + operation
				}
				t.Run(name, func(t *testing.T) {
					user, org, selectedOrg := uuid.New(), uuid.New(), uuid.New()
					repo := &organizationLimitPolicyRepo{member: &models.OrganizationMember{OrganizationID: org, UserID: user, AccessScope: models.AccessScopeWorkspace}, request: &models.LimitIncreaseRequest{ID: uuid.New(), OrganizationID: org, SubmittedBy: user, Status: models.LimitRequestStatusPending}}
					if targetRestricted {
						repo.member.AccessScope = models.AccessScopeRestricted
					}
					service := organization.NewService(repo, nil, nil, nil, nil)
					h := &Handler{OrganizationService: service}
					gate := &middleware.Handler{OrganizationService: service}
					r := gin.New()
					r.Use(func(c *gin.Context) {
						session := &models.Session{UserID: user}
						if selected != "none" {
							session.CurrentOrganizationID = &selectedOrg
							member := &models.OrganizationMember{OrganizationID: selectedOrg, UserID: user, AccessScope: models.AccessScopeWorkspace}
							if selected == "restricted" {
								member.AccessScope = models.AccessScopeRestricted
							}
							c.Set(middleware.OrganizationIDKey, selectedOrg)
							c.Set(middleware.SessionMemberKey, member)
						}
						c.Set(middleware.SessionKey, session)
					})
					method, route, path := http.MethodGet, "/v1/organization/:orgId/limit-requests", "/v1/organization/"+org.String()+"/limit-requests"
					fn, want := h.ListOrgLimitRequests, http.StatusOK
					if operation == "cancel" {
						method, route, path, fn, want = http.MethodDelete, "/v1/limit-requests/:id", "/v1/limit-requests/"+repo.request.ID.String(), h.CancelLimitRequest, http.StatusNoContent
					}
					if operation == "submit" {
						method, fn = http.MethodPost, h.SubmitLimitIncreaseRequest
					}
					if targetRestricted {
						want = http.StatusForbidden
					}
					r.Handle(method, route, gate.ResourceScopeGate(), fn)
					w := httptest.NewRecorder()
					r.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(`{"field":"max_campaigns","requested":100,"reason":"Additional campaign capacity is needed"}`)))
					if w.Code != want || repo.checkedOrg != org || repo.checkedUser != user {
						t.Fatalf("HTTP %d %s; want %d checked target=%v caller=%v", w.Code, w.Body.String(), want, repo.checkedOrg == org, repo.checkedUser == user)
					}
					if targetRestricted && (repo.listed || repo.updated) {
						t.Fatal("restricted target reached limit data")
					}
				})
			}
		}
	}
}
