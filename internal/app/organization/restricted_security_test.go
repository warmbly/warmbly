package organization

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type workspaceCreationRepo struct {
	createRepo
	members       []models.OrganizationMember
	membershipErr error
	admin         uint32
	adminErr      error
	checkedUser   uuid.UUID
	adminLookups  int
}

func (r *workspaceCreationRepo) GetUserOrganizations(_ context.Context, userID uuid.UUID) ([]models.OrganizationMember, error) {
	r.checkedUser = userID
	return r.members, r.membershipErr
}

func (r *workspaceCreationRepo) GetUserAdminPermissions(_ context.Context, userID uuid.UUID) (uint32, error) {
	if userID != r.checkedUser {
		panic("administrator lookup must match account memberships")
	}
	r.adminLookups++
	return r.admin, r.adminErr
}

func TestWorkspaceCreationChecksAllAccountMemberships(t *testing.T) {
	restricted := models.OrganizationMember{Role: "Viewer", AccessScope: models.AccessScopeRestricted}
	workspace := models.OrganizationMember{Role: "Viewer", AccessScope: models.AccessScopeWorkspace}
	for _, mode := range []string{"self_hosted", "cloud"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("DEPLOYMENT_MODE", mode)
			for _, tc := range []struct {
				name    string
				members []models.OrganizationMember
				admin   uint32
				want    errx.Code
			}{
				{name: "new account"},
				{name: "unrestricted", members: []models.OrganizationMember{workspace}},
				{name: "legacy unrestricted", members: []models.OrganizationMember{{Role: "Viewer"}}},
				{name: "only restricted", members: []models.OrganizationMember{restricted, restricted}, want: errx.Forbidden},
				{name: "mixed memberships", members: []models.OrganizationMember{restricted, workspace}},
				{name: "owner is unrestricted", members: []models.OrganizationMember{{Role: string(models.RoleOwner), AccessScope: models.AccessScopeRestricted}}},
				{name: "instance admin", members: []models.OrganizationMember{restricted}, admin: uint32(models.AdminPermViewUsers)},
			} {
				t.Run(tc.name, func(t *testing.T) {
					repo := &workspaceCreationRepo{members: tc.members, admin: tc.admin}
					s := &organizationService{orgRepo: repo, userRepo: createUsers{}}
					userID := uuid.New()
					org, xerr := s.Create(context.Background(), userID, "Acme", "UTC")
					if repo.checkedUser != userID {
						t.Fatal("creation did not check the creator's account memberships")
					}
					if tc.want != 0 {
						if xerr == nil || xerr.Code != tc.want || org != nil || repo.created != nil {
							t.Fatalf("restricted creation: org=%+v err=%v written=%+v", org, xerr, repo.created)
						}
						return
					}
					if xerr != nil || org == nil || repo.created == nil {
						t.Fatalf("eligible creation refused: %v", xerr)
					}
				})
			}
		})
	}
}

func TestWorkspaceCreationFailsClosedOnPolicyLookupErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		repo workspaceCreationRepo
	}{
		{"memberships", workspaceCreationRepo{membershipErr: errors.New("database unavailable")}},
		{"admin exemption", workspaceCreationRepo{members: []models.OrganizationMember{{AccessScope: models.AccessScopeRestricted}}, adminErr: errors.New("database unavailable")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &organizationService{orgRepo: &tc.repo, userRepo: createUsers{}}
			org, xerr := s.Create(context.Background(), uuid.New(), "Acme", "UTC")
			if xerr == nil || xerr.Code != errx.Internal || org != nil || tc.repo.created != nil {
				t.Fatalf("lookup failure did not fail closed: org=%+v err=%v written=%+v", org, xerr, tc.repo.created)
			}
		})
	}
}

type scopedCountsRepo struct {
	repository.OrganizationRepository
	counts *models.OrganizationCounts
	err    error
	org    uuid.UUID
	scope  *models.ResourceScope
	calls  int
}

func (r *scopedCountsRepo) GetScopedOrganizationCounts(_ context.Context, org uuid.UUID, scope *models.ResourceScope) (*models.OrganizationCounts, error) {
	r.org, r.scope = org, scope
	r.calls++
	return r.counts, r.err
}

func TestScopedOrganizationCountsRequireResolvedScope(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scope  *models.ResourceScope
		counts *models.OrganizationCounts
		err    error
		fail   bool
	}{
		{name: "unresolved", fail: true},
		{name: "no grants", scope: &models.ResourceScope{}, counts: &models.OrganizationCounts{}},
		{name: "grants", scope: &models.ResourceScope{Campaigns: []uuid.UUID{uuid.New()}, Mailboxes: []uuid.UUID{uuid.New()}}, counts: &models.OrganizationCounts{TotalCampaigns: 1, EmailAccounts: 1}},
		{name: "query failure", scope: &models.ResourceScope{}, err: errors.New("database unavailable"), fail: true},
		{name: "missing result", scope: &models.ResourceScope{}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &scopedCountsRepo{counts: tc.counts, err: tc.err}
			s := &organizationService{orgRepo: r}
			orgID := uuid.New()
			counts, xerr := s.GetScopedOrganizationCounts(context.Background(), orgID, tc.scope)
			if tc.fail {
				if counts != nil || xerr == nil || xerr.Code != errx.Internal {
					t.Fatalf("counts=%+v err=%v; want fail closed", counts, xerr)
				}
			} else if xerr != nil || counts != tc.counts || r.scope != tc.scope || r.org != orgID {
				t.Fatalf("scope/counts changed: %+v, %v", counts, xerr)
			}
			if tc.scope == nil && r.calls != 0 {
				t.Fatal("unresolved scope reached repository")
			}
		})
	}
}

type targetLimitRepo struct {
	repository.OrganizationRepository
	member    *models.OrganizationMember
	memberErr error
	org, user uuid.UUID
	request   *models.LimitIncreaseRequest
	listed    bool
	updated   bool
}

func (r *targetLimitRepo) GetMember(_ context.Context, org, user uuid.UUID) (*models.OrganizationMember, error) {
	r.org, r.user = org, user
	return r.member, r.memberErr
}

func (r *targetLimitRepo) ListLimitRequestsForOrg(context.Context, uuid.UUID) ([]models.LimitIncreaseRequest, error) {
	r.listed = true
	return []models.LimitIncreaseRequest{*r.request}, nil
}

func (r *targetLimitRepo) GetLimitRequest(context.Context, uuid.UUID) (*models.LimitIncreaseRequest, error) {
	return r.request, nil
}

func (r *targetLimitRepo) UpdateLimitRequestStatus(context.Context, uuid.UUID, models.LimitRequestStatus, uuid.UUID, string) error {
	r.updated = true
	return nil
}

func TestLimitRequestsRequireUnrestrictedTargetMembership(t *testing.T) {
	for _, tc := range []struct {
		name   string
		member *models.OrganizationMember
		err    error
		want   errx.Code
	}{
		{name: "restricted target", member: &models.OrganizationMember{AccessScope: models.AccessScopeRestricted}, want: errx.Forbidden},
		{name: "nonmember", want: errx.Forbidden},
		{name: "lookup failure", err: errors.New("database unavailable"), want: errx.Internal},
		{name: "workspace target", member: &models.OrganizationMember{AccessScope: models.AccessScopeWorkspace}},
		{name: "owner", member: &models.OrganizationMember{Role: string(models.RoleOwner), AccessScope: models.AccessScopeRestricted}},
	} {
		for _, operation := range []string{"submit", "list", "cancel"} {
			t.Run(tc.name+"/"+operation, func(t *testing.T) {
				org, user := uuid.New(), uuid.New()
				r := &targetLimitRepo{member: tc.member, memberErr: tc.err, request: &models.LimitIncreaseRequest{
					ID: uuid.New(), OrganizationID: org, SubmittedBy: user, Status: models.LimitRequestStatusPending,
				}}
				s := &organizationService{orgRepo: r}
				var xerr *errx.Error
				switch operation {
				case "submit":
					if tc.want == 0 {
						// Submission's later entitlement checks are covered separately.
						xerr = s.requireWorkspaceMember(context.Background(), org, user)
					} else {
						_, xerr = s.SubmitLimitIncreaseRequest(context.Background(), org, user, &models.CreateLimitIncreaseRequest{Field: "max_campaigns", Requested: 100})
					}
				case "list":
					_, xerr = s.ListLimitRequestsForOrg(context.Background(), org, user)
				case "cancel":
					xerr = s.CancelLimitRequest(context.Background(), r.request.ID, user)
				}
				if r.org != org || r.user != user {
					t.Fatalf("checked %s/%s instead of target %s/%s", r.org, r.user, org, user)
				}
				if tc.want != 0 {
					if xerr == nil || xerr.Code != tc.want || r.listed || r.updated {
						t.Fatalf("target refused incorrectly: err=%v listed=%v updated=%v", xerr, r.listed, r.updated)
					}
				} else if xerr != nil || (operation == "list" && !r.listed) || (operation == "cancel" && !r.updated) {
					t.Fatalf("unrestricted target refused: %v", xerr)
				}
			})
		}
	}
}
