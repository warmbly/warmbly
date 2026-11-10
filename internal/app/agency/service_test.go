package agency

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type fakeRepo struct {
	repository.AgencyRepository
	agency     *models.Agency
	client     *models.AgencyClient
	sends      map[uuid.UUID]int64
	topups     int64
	sentToday  int
	grants     map[uuid.UUID]uuid.UUID
	cleared    []uuid.UUID
	sendsCalls int
	rollups    [][]models.AgencyPoolDay
	usageRange [2]time.Time
}

func (f *fakeRepo) GetAgencyForOrganization(_ context.Context, orgID uuid.UUID) (*models.Agency, error) {
	if f.client == nil || f.client.OrganizationID != orgID {
		return nil, nil
	}
	return f.agency, nil
}

func (f *fakeRepo) GetClient(_ context.Context, orgID uuid.UUID) (*models.AgencyClient, error) {
	if f.client == nil || f.client.OrganizationID != orgID {
		return nil, nil
	}
	return f.client, nil
}

func (f *fakeRepo) RollupPoolUsage(_ context.Context, _ uuid.UUID, days []models.AgencyPoolDay) error {
	f.rollups = append(f.rollups, days)
	return nil
}

func (f *fakeRepo) PoolUsage(_ context.Context, _ uuid.UUID, from, to time.Time) (map[uuid.UUID]int64, error) {
	f.sendsCalls++
	f.usageRange = [2]time.Time{from, to}
	return f.sends, nil
}

func (f *fakeRepo) SentTodayByClient(context.Context, uuid.UUID) (map[uuid.UUID]int, error) {
	if f.client == nil {
		return nil, nil
	}
	return map[uuid.UUID]int{f.client.OrganizationID: f.sentToday}, nil
}

func (f *fakeRepo) TopupSends(context.Context, uuid.UUID, time.Time) (int64, error) {
	return f.topups, nil
}

func (f *fakeRepo) SyncClientGrant(_ context.Context, _, orgID, planID uuid.UUID, _ *time.Time) error {
	f.grants[orgID] = planID
	return nil
}

func (f *fakeRepo) ClearClientGrant(_ context.Context, _, orgID uuid.UUID) error {
	f.cleared = append(f.cleared, orgID)
	delete(f.grants, orgID)
	return nil
}

type fakePlans struct{ pool *int }

func (p fakePlans) GetByID(_ context.Context, id uuid.UUID) (*models.Plan, error) {
	return &models.Plan{ID: id, MonthlySendPool: p.pool}, nil
}
func (p fakePlans) GetByStripePriceID(context.Context, string) (*models.Plan, error) { return nil, nil }
func (p fakePlans) List(context.Context, bool) ([]*models.Plan, error)               { return nil, nil }

func intp(v int) *int { return &v }

func newTestService(repo *fakeRepo, pool *int, billing bool, now time.Time) *Service {
	s := NewService(repo, nil, nil, nil, fakePlans{pool: pool})
	s.billing = billing
	s.now = func() time.Time { return now }
	return s
}

func activeAgency(now time.Time) *models.Agency {
	plan := models.AgencyPlanID
	start, end := now.AddDate(0, 0, -10), now.AddDate(0, 0, 20)
	return &models.Agency{ID: uuid.New(), PlanID: &plan, Status: models.SubscriptionStatusActive, CurrentPeriodStart: &start, CurrentPeriodEnd: &end}
}

func TestDailyLimitFromPool(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	org, other := uuid.New(), uuid.New()
	repo := &fakeRepo{
		agency:    activeAgency(now),
		client:    &models.AgencyClient{OrganizationID: org},
		sends:     map[uuid.UUID]int64{org: 400, other: 500},
		topups:    100,
		sentToday: 50,
	}
	s := newTestService(repo, intp(1_000), true, now)

	limit, ok := s.DailyLimit(context.Background(), org)
	if !ok || limit != 50+200 {
		t.Fatalf("got %d, %v; want 250 (today 50 + pool 1100 - used 900)", limit, ok)
	}

	// The period count is reused inside its TTL.
	_, _ = s.DailyLimit(context.Background(), org)
	if repo.sendsCalls != 1 {
		t.Fatalf("period counted %d times, want 1", repo.sendsCalls)
	}

	if _, ok := s.DailyLimit(context.Background(), uuid.New()); ok {
		t.Fatal("a workspace outside any agency must keep its own plan's limit")
	}
}

func TestDailyLimitClientCap(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	org := uuid.New()
	repo := &fakeRepo{
		agency:    activeAgency(now),
		client:    &models.AgencyClient{OrganizationID: org, MonthlySendLimit: intp(450)},
		sends:     map[uuid.UUID]int64{org: 400},
		sentToday: 10,
	}
	s := newTestService(repo, intp(150_000), true, now)
	if limit, ok := s.DailyLimit(context.Background(), org); !ok || limit != 60 {
		t.Fatalf("got %d, %v; want 60 (today 10 + cap 450 - used 400)", limit, ok)
	}
}

func TestDailyLimitLapsedAgencyFallsBack(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	org := uuid.New()
	a := activeAgency(now)
	a.Status = models.SubscriptionStatusCanceled
	repo := &fakeRepo{agency: a, client: &models.AgencyClient{OrganizationID: org}}
	s := newTestService(repo, intp(150_000), true, now)
	if _, ok := s.DailyLimit(context.Background(), org); ok {
		t.Fatal("a lapsed agency must leave the workspace on its own plan")
	}
}

func TestDailyLimitUnlimited(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	org := uuid.New()
	selfHosted := &fakeRepo{agency: &models.Agency{ID: uuid.New()}, client: &models.AgencyClient{OrganizationID: org}}
	if limit, ok := newTestService(selfHosted, nil, false, now).DailyLimit(context.Background(), org); !ok || limit != -1 {
		t.Fatalf("self-host: got %d, %v; want -1", limit, ok)
	}
	scale := &fakeRepo{agency: activeAgency(now), client: &models.AgencyClient{OrganizationID: org}}
	if limit, ok := newTestService(scale, intp(0), true, now).DailyLimit(context.Background(), org); !ok || limit != -1 {
		t.Fatalf("plan with no pool cap: got %d, %v; want -1", limit, ok)
	}
}

func TestSyncGrantDecisions(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	org := uuid.New()

	repo := &fakeRepo{grants: map[uuid.UUID]uuid.UUID{}}
	s := newTestService(repo, intp(150_000), true, now)
	if err := s.syncGrant(context.Background(), activeAgency(now), org); err != nil {
		t.Fatal(err)
	}
	if repo.grants[org] != models.AgencyPlanID {
		t.Fatal("an active agency must grant its plan")
	}

	lapsed := activeAgency(now)
	end := now.Add(-models.AgencyGrantGrace - time.Hour)
	lapsed.CurrentPeriodEnd = &end
	if err := s.syncGrant(context.Background(), lapsed, org); err != nil {
		t.Fatal(err)
	}
	if _, ok := repo.grants[org]; ok {
		t.Fatal("a period past its grace must clear the grant")
	}

	selfHosted := &fakeRepo{grants: map[uuid.UUID]uuid.UUID{}}
	if err := newTestService(selfHosted, nil, false, now).syncGrant(context.Background(), activeAgency(now), org); err != nil {
		t.Fatal(err)
	}
	if len(selfHosted.grants) != 0 || len(selfHosted.cleared) != 0 {
		t.Fatal("a self-host writes no grants: every gate is already open")
	}
}

func TestStaffRoleRules(t *testing.T) {
	owner, admin, member := models.AgencyRoleOwner, models.AgencyRoleAdmin, models.AgencyRoleMember
	if checkAssignable(owner, admin) != nil || checkAssignable(owner, member) != nil || checkAssignable(admin, member) != nil {
		t.Fatal("owners assign admin and member, admins assign member")
	}
	if checkAssignable(admin, admin) == nil {
		t.Fatal("an admin must not make admins")
	}
	if checkAssignable(owner, owner) == nil {
		t.Fatal("nobody assigns the owner role")
	}
	if canChange(admin, admin) || canChange(owner, owner) || canChange(member, member) {
		t.Fatal("admins cannot change admins, nobody changes the owner, members change nobody")
	}
	if !canChange(owner, admin) || !canChange(admin, member) {
		t.Fatal("owners change admins, admins change members")
	}
}

type staffRepo struct {
	repository.AgencyRepository
	agency  models.Agency
	members map[uuid.UUID]models.AgencyRole
	upserts int
}

func (r *staffRepo) GetMember(_ context.Context, agencyID, userID uuid.UUID) (*models.AgencyMember, error) {
	role, ok := r.members[userID]
	if !ok {
		return nil, nil
	}
	return &models.AgencyMember{AgencyID: agencyID, UserID: userID, Role: role}, nil
}

func (r *staffRepo) Get(context.Context, uuid.UUID) (*models.Agency, error) {
	a := r.agency
	return &a, nil
}

func (r *staffRepo) UpsertMember(context.Context, uuid.UUID, uuid.UUID, models.AgencyRole) error {
	r.upserts++
	return nil
}

type fakeUsers struct{ byEmail *models.User }

func (u fakeUsers) GetUser(context.Context, uuid.UUID) (*models.User, error) { return u.byEmail, nil }
func (u fakeUsers) GetUserByEmail(context.Context, string) (*models.User, error) {
	return u.byEmail, nil
}

func TestAddStaffCannotDemoteAPeer(t *testing.T) {
	actor, peer := uuid.New(), uuid.New()
	repo := &staffRepo{agency: models.Agency{ID: uuid.New()}, members: map[uuid.UUID]models.AgencyRole{
		actor: models.AgencyRoleAdmin, peer: models.AgencyRoleAdmin,
	}}
	s := NewService(repo, nil, fakeUsers{byEmail: &models.User{ID: peer, Email: "peer@example.com"}}, nil, fakePlans{})
	if _, _, xerr := s.AddStaff(context.Background(), repo.agency.ID, actor, "peer@example.com", models.AgencyRoleMember); xerr == nil {
		t.Fatal("an admin re-adding another admin as a member must be refused")
	}
	if repo.upserts != 0 {
		t.Fatalf("role written %d times", repo.upserts)
	}
}

type fakeOrgs struct{ capped, exempt int }

func (o *fakeOrgs) Create(context.Context, uuid.UUID, string, string) (*models.Organization, *errx.Error) {
	o.capped++
	return nil, errx.New(errx.Forbidden, "maximum organization limit reached")
}

func (o *fakeOrgs) CreateAgencyClient(context.Context, uuid.UUID, string, string) (*models.Organization, *errx.Error) {
	o.exempt++
	return nil, errx.New(errx.Forbidden, "stop here")
}

func (o *fakeOrgs) Get(context.Context, uuid.UUID) (*models.Organization, *errx.Error) {
	return nil, nil
}

func TestCreateClientWorkspaceCapNeedsAPaidAgency(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	actor := uuid.New()
	for _, tc := range []struct {
		name    string
		agency  models.Agency
		billing bool
		exempt  bool
	}{
		{"unpaid agency on a billed instance", models.Agency{ID: uuid.New(), Status: models.SubscriptionStatusIncomplete}, true, false},
		{"paid agency", *activeAgency(now), true, true},
		{"no billing", models.Agency{ID: uuid.New()}, false, true},
	} {
		repo := &staffRepo{agency: tc.agency, members: map[uuid.UUID]models.AgencyRole{actor: models.AgencyRoleOwner}}
		orgs := &fakeOrgs{}
		s := NewService(repo, orgs, nil, nil, fakePlans{})
		s.billing = tc.billing
		s.now = func() time.Time { return now }
		_, _ = s.CreateClient(context.Background(), tc.agency.ID, actor, "Client", "")
		if (orgs.exempt == 1) != tc.exempt || orgs.exempt+orgs.capped != 1 {
			t.Fatalf("%s: capped=%d exempt=%d", tc.name, orgs.capped, orgs.exempt)
		}
	}
}
