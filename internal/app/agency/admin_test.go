package agency

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// operatorRepo is fakeRepo plus the writes an operator makes.
type operatorRepo struct {
	*fakeRepo
	staff    map[uuid.UUID]bool
	added    []uuid.UUID
	recorded []*models.AgencyPoolTopup
	untils   map[uuid.UUID]*time.Time
}

func (r *operatorRepo) Get(context.Context, uuid.UUID) (*models.Agency, error) { return r.agency, nil }

func (r *operatorRepo) SetManagedPlan(_ context.Context, _, planID, by uuid.UUID, reason string, until *time.Time) error {
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	r.agency.ManagedPlanID, r.agency.ManagedAt, r.agency.ManagedBy, r.agency.ManagedReason, r.agency.ManagedUntil = &planID, &at, &by, &reason, until
	return nil
}

func (r *operatorRepo) ClearManagedPlan(context.Context, uuid.UUID) error {
	r.agency.ManagedPlanID, r.agency.ManagedAt, r.agency.ManagedBy, r.agency.ManagedReason, r.agency.ManagedUntil = nil, nil, nil, nil, nil
	return nil
}

func (r *operatorRepo) SetOverrides(_ context.Context, _ uuid.UUID, pool *int, ai *models.Micro, by uuid.UUID, reason string) error {
	r.agency.PoolOverride, r.agency.AIAllowanceOverride, r.agency.OverrideBy, r.agency.OverrideReason = pool, ai, &by, &reason
	return nil
}

func (r *operatorRepo) SyncClientGrant(_ context.Context, _, orgID, planID uuid.UUID, until *time.Time) error {
	r.grants[orgID] = planID
	r.untils[orgID] = until
	return nil
}

func (r *operatorRepo) ListClientOrganizations(context.Context, uuid.UUID) ([]models.AgencyClient, error) {
	if r.client == nil {
		return nil, nil
	}
	return []models.AgencyClient{*r.client}, nil
}

func (r *operatorRepo) ListMembers(context.Context, uuid.UUID) ([]models.AgencyMember, error) {
	return nil, nil
}

func (r *operatorRepo) ClearStaleGrants(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (r *operatorRepo) GetMember(_ context.Context, _, userID uuid.UUID) (*models.AgencyMember, error) {
	if !r.staff[userID] {
		return nil, nil
	}
	return &models.AgencyMember{UserID: userID, Role: models.AgencyRoleAdmin}, nil
}

func (r *operatorRepo) AddClient(_ context.Context, _, orgID, _ uuid.UUID) error {
	r.added = append(r.added, orgID)
	r.client = &models.AgencyClient{OrganizationID: orgID}
	return nil
}

func (r *operatorRepo) RecordTopup(_ context.Context, t *models.AgencyPoolTopup) (bool, error) {
	r.recorded = append(r.recorded, t)
	r.topups += int64(t.Sends)
	return true, nil
}

type operatorUsers struct{}

func (operatorUsers) GetUser(context.Context, uuid.UUID) (*models.User, error) { return nil, nil }
func (operatorUsers) GetUserByEmail(context.Context, string) (*models.User, error) {
	return nil, nil
}

type operatorOrgs struct{ owner uuid.UUID }

func (o operatorOrgs) Create(context.Context, uuid.UUID, string, string) (*models.Organization, *errx.Error) {
	return nil, nil
}
func (o operatorOrgs) CreateAgencyClient(context.Context, uuid.UUID, string, string) (*models.Organization, *errx.Error) {
	return nil, nil
}
func (o operatorOrgs) Get(_ context.Context, orgID uuid.UUID) (*models.Organization, *errx.Error) {
	return &models.Organization{ID: orgID, OwnerUserID: o.owner}, nil
}

type operatorSubs struct{ sub *models.Subscription }

func (s operatorSubs) GetByOrganizationID(context.Context, uuid.UUID) (*models.Subscription, error) {
	return s.sub, nil
}

func newOperatorService(a *models.Agency, client *models.AgencyClient, pool *int, now time.Time) (*Service, *operatorRepo) {
	repo := &operatorRepo{
		fakeRepo: &fakeRepo{agency: a, client: client, grants: map[uuid.UUID]uuid.UUID{}},
		staff:    map[uuid.UUID]bool{}, untils: map[uuid.UUID]*time.Time{},
	}
	s := NewService(repo, operatorOrgs{}, operatorUsers{}, operatorSubs{}, fakePlans{pool: pool})
	s.billing = true
	s.now = func() time.Time { return now }
	return s, repo
}

func TestAdminGrantPlanEntitlesClients(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	org, admin := uuid.New(), uuid.New()
	a := &models.Agency{ID: uuid.New(), Status: models.SubscriptionStatusIncomplete}
	s, repo := newOperatorService(a, &models.AgencyClient{OrganizationID: org}, intp(500_000), now)
	ctx := context.Background()

	if _, ok := s.DailyLimit(ctx, org); ok {
		t.Fatal("an agency without a plan must leave its client on its own limit")
	}
	d, xerr := s.AdminGrantPlan(ctx, a.ID, models.AgencyProPlanID, admin, "  Annual contract, invoiced  ", nil)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if repo.grants[org] != models.AgencyProPlanID || repo.untils[org] != nil {
		t.Fatalf("client grant: got %s until %v, want Agency Pro with no end", repo.grants[org], repo.untils[org])
	}
	if !d.Agency.Entitled || d.Agency.ManagedPlan == nil || !d.Agency.ManagedPlan.Active || d.Agency.ManagedPlan.Reason != "Annual contract, invoiced" {
		t.Fatalf("detail: %+v", d.Agency.ManagedPlan)
	}
	if d.Agency.Grant == nil || d.Agency.Grant.PlanID != models.AgencyProPlanID {
		t.Fatalf("the dashboard must see the plan in force: %+v", d.Agency.Grant)
	}
	if d.Usage.PoolSize != 500_000 {
		t.Fatalf("pool: got %d, want the granted plan's 500000", d.Usage.PoolSize)
	}

	d, xerr = s.AdminRevokePlan(ctx, a.ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if _, held := repo.grants[org]; held || d.Agency.Entitled || d.Agency.ManagedPlan != nil {
		t.Fatalf("after the plan ends the client must hold nothing: grants=%v entitled=%v", repo.grants, d.Agency.Entitled)
	}
}

func TestAdminGrantPlanRefusals(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	a := &models.Agency{ID: uuid.New(), Status: models.SubscriptionStatusIncomplete}
	ctx := context.Background()
	past := now.Add(-time.Hour)

	s, _ := newOperatorService(a, nil, intp(150_000), now)
	if _, xerr := s.AdminGrantPlan(ctx, a.ID, models.AgencyPlanID, uuid.New(), "   ", nil); xerr == nil {
		t.Fatal("a grant without a reason must be refused")
	}
	if _, xerr := s.AdminGrantPlan(ctx, a.ID, models.AgencyPlanID, uuid.New(), "partner", &past); xerr == nil {
		t.Fatal("a grant that already ended must be refused")
	}
	// A plan without a pool is not an agency plan.
	regular, _ := newOperatorService(a, nil, nil, now)
	if _, xerr := regular.AdminGrantPlan(ctx, a.ID, uuid.New(), uuid.New(), "partner", nil); xerr == nil || xerr.Identifier != "agency_plan_invalid" {
		t.Fatalf("a regular plan must be refused, got %v", xerr)
	}
	if a.ManagedAt != nil {
		t.Fatal("a refused grant must record nothing")
	}
}

func TestAdminOverridesAndCredit(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	admin := uuid.New()
	a := activeAgency(now)
	s, repo := newOperatorService(a, nil, intp(500_000), now)
	ctx := context.Background()

	if _, xerr := s.AdminSetOverrides(ctx, a.ID, admin, intp(2_000_000), nil, ""); xerr == nil {
		t.Fatal("an override without a reason must be refused")
	}
	if _, xerr := s.AdminSetOverrides(ctx, a.ID, admin, intp(0), nil, "scale"); xerr == nil {
		t.Fatal("a pool of zero must be refused")
	}
	allowance := 250 * models.MicroPerUSD
	d, xerr := s.AdminSetOverrides(ctx, a.ID, admin, intp(2_000_000), &allowance, "Agency Scale contract")
	if xerr != nil {
		t.Fatal(xerr)
	}
	if d.Usage.PoolSize != 2_000_000 || d.Agency.Overrides == nil || *d.Agency.Overrides.AIAllowanceUSD != 250 {
		t.Fatalf("override: pool %d, %+v", d.Usage.PoolSize, d.Agency.Overrides)
	}

	d, xerr = s.AdminCreditPool(ctx, a.ID, admin, 50_000, "apology for the outage")
	if xerr != nil {
		t.Fatal(xerr)
	}
	if len(repo.recorded) != 1 || repo.recorded[0].StripePaymentIntentID != nil || repo.recorded[0].GrantedBy == nil || *repo.recorded[0].GrantedBy != admin {
		t.Fatalf("credit row: %+v", repo.recorded)
	}
	if d.Usage.Total != 2_050_000 {
		t.Fatalf("total: got %d, want pool plus the credit", d.Usage.Total)
	}

	// Clearing both puts the agency back on its plan's pool; no reason is needed to remove one.
	d, xerr = s.AdminSetOverrides(ctx, a.ID, admin, nil, nil, "")
	if xerr != nil {
		t.Fatal(xerr)
	}
	if d.Usage.PoolSize != 500_000 || d.Agency.Overrides != nil {
		t.Fatalf("cleared: pool %d, %+v", d.Usage.PoolSize, d.Agency.Overrides)
	}
}

func TestAdminCreditPoolNeedsAPool(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	a := &models.Agency{ID: uuid.New(), Status: models.SubscriptionStatusIncomplete}
	s, repo := newOperatorService(a, nil, intp(150_000), now)
	if _, xerr := s.AdminCreditPool(context.Background(), a.ID, uuid.New(), 1_000, "gift"); xerr == nil || xerr.Identifier != "agency_no_pool" {
		t.Fatalf("an agency without a plan has no pool to add to, got %v", xerr)
	}
	if len(repo.recorded) != 0 {
		t.Fatal("a refused credit must record nothing")
	}
}

func TestAdminAttachClientNeedsOwnerOnStaff(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	owner, org := uuid.New(), uuid.New()
	a := activeAgency(now)
	a.OwnerUserID = uuid.New()
	s, repo := newOperatorService(a, nil, intp(150_000), now)
	s.orgs = operatorOrgs{owner: owner}
	ctx := context.Background()

	if _, xerr := s.AdminAttachClient(ctx, a.ID, org); xerr == nil || xerr.Identifier != "agency_client_owner_not_staff" {
		t.Fatalf("a workspace whose owner is not on the agency's staff must be refused, got %v", xerr)
	}
	if len(repo.added) != 0 {
		t.Fatal("a refused attach must add nothing")
	}

	repo.staff[owner] = true
	paid := "sub_123"
	s.subs = operatorSubs{sub: &models.Subscription{StripeSubscriptionID: &paid, Status: models.SubscriptionStatusActive}}
	if _, xerr := s.AdminAttachClient(ctx, a.ID, org); xerr == nil || xerr.Identifier != "agency_client_has_subscription" {
		t.Fatalf("a workspace paying for its own plan must be refused, got %v", xerr)
	}

	s.subs = operatorSubs{}
	if _, xerr := s.AdminAttachClient(ctx, a.ID, org); xerr != nil {
		t.Fatal(xerr)
	}
	if len(repo.added) != 1 || repo.added[0] != org || repo.grants[org] != models.AgencyPlanID {
		t.Fatalf("attach: added %v, grants %v", repo.added, repo.grants)
	}
}
