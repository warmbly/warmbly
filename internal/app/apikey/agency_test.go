package apikey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type agencyKeyRepo struct {
	repository.AgencyAPIKeyRepository
	byHash map[string]*models.AgencyAPIKey
}

func (r *agencyKeyRepo) Create(_ context.Context, agencyID, createdBy uuid.UUID, d *models.CreateAgencyAPIKey, prefix, suffix, hash string) (*models.AgencyAPIKey, error) {
	k := &models.AgencyAPIKey{ID: uuid.New(), AgencyID: agencyID, CreatedBy: createdBy, Name: d.Name, KeyPrefix: prefix, KeySuffix: suffix, Permissions: d.Permissions, Status: models.APIKeyStatusActive}
	r.byHash[hash] = k
	return k, nil
}

func (r *agencyKeyRepo) GetActiveByHash(_ context.Context, hash string) (*models.AgencyAPIKey, error) {
	return r.byHash[hash], nil
}

type agencyStaff struct {
	roles   map[uuid.UUID]models.AgencyRole // by user, in the one agency under test
	agency  uuid.UUID
	clients map[uuid.UUID]uuid.UUID // org -> agency
}

func (s *agencyStaff) GetMember(_ context.Context, agencyID, userID uuid.UUID) (*models.AgencyMember, error) {
	role, ok := s.roles[userID]
	if !ok || agencyID != s.agency {
		return nil, nil
	}
	return &models.AgencyMember{AgencyID: agencyID, UserID: userID, Role: role}, nil
}

func (s *agencyStaff) GetClient(_ context.Context, orgID uuid.UUID) (*models.AgencyClient, error) {
	a, ok := s.clients[orgID]
	if !ok {
		return nil, nil
	}
	return &models.AgencyClient{OrganizationID: orgID, AgencyID: a}, nil
}

func identifier(xerr *errx.Error) string {
	if xerr == nil {
		return ""
	}
	return xerr.Identifier
}

func TestAgencyKeyIsBrandedAndStoredHashed(t *testing.T) {
	raw, prefix, suffix, hash, err := generateKeyWith(AgencyKeyPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, AgencyKeyPrefix) || strings.HasPrefix(raw, KeyPrefix) || strings.HasPrefix(raw, models.OAuthAccessTokenPrefix) {
		t.Fatalf("raw key %q is not branded as an agency key only", raw[:12])
	}
	if prefix != raw[:len(AgencyKeyPrefix)+2] || suffix != raw[len(raw)-4:] {
		t.Fatalf("display prefix %q / suffix %q do not come from the key", prefix, suffix)
	}
	sum := sha256.Sum256([]byte(raw))
	if hash != hex.EncodeToString(sum[:]) || strings.Contains(hash, raw[len(AgencyKeyPrefix):]) {
		t.Fatal("the stored value is not the SHA-256 of the key")
	}
	if _, wp, _, _, _ := generateKey(); len(wp) != displayPrefixLen {
		t.Fatalf("workspace display prefix changed length: %q", wp)
	}
}

func newAgencyKeyFixture(t *testing.T, perms uint64) (*AgencyKeyService, *agencyStaff, *models.AgencyAPIKeyWithSecret) {
	t.Helper()
	owner := uuid.New()
	staff := &agencyStaff{agency: uuid.New(), roles: map[uuid.UUID]models.AgencyRole{owner: models.AgencyRoleAdmin}, clients: map[uuid.UUID]uuid.UUID{}}
	svc := NewAgencyKeyService(&agencyKeyRepo{byHash: map[string]*models.AgencyAPIKey{}}, staff, nil)
	k, xerr := svc.Create(context.Background(), staff.agency, owner, &models.CreateAgencyAPIKey{Name: "ci", Permissions: perms})
	if xerr != nil {
		t.Fatal(xerr)
	}
	return svc, staff, k
}

func TestAgencyKeyCreateOnlyByManagersWithAgencyBits(t *testing.T) {
	svc, staff, _ := newAgencyKeyFixture(t, models.APIPermReadCampaigns)
	member := uuid.New()
	staff.roles[member] = models.AgencyRoleMember
	ctx := context.Background()
	if _, xerr := svc.Create(ctx, staff.agency, member, &models.CreateAgencyAPIKey{Name: "x", Permissions: models.APIPermReadCampaigns}); identifier(xerr) != "agency_forbidden" {
		t.Errorf("a staff member created a key: %v", xerr)
	}
	if _, xerr := svc.Create(ctx, uuid.New(), member, &models.CreateAgencyAPIKey{Name: "x", Permissions: models.APIPermReadCampaigns}); identifier(xerr) != "agency_not_found" {
		t.Errorf("a key was created in an agency the caller is not part of: %v", xerr)
	}
	var admin uuid.UUID
	for u, r := range staff.roles {
		if r == models.AgencyRoleAdmin {
			admin = u
		}
	}
	for _, perms := range []uint64{0, models.APIPermAPIKeys, models.APIPermRealtimeSubscribe, 1 << 62} {
		if _, xerr := svc.Create(ctx, staff.agency, admin, &models.CreateAgencyAPIKey{Name: "x", Permissions: perms}); identifier(xerr) != CodeAgencyKeyInvalidPerms {
			t.Errorf("permissions %b accepted: %v", perms, xerr)
		}
	}
}

func TestAgencyKeyAuthenticateFollowsItsCreator(t *testing.T) {
	svc, staff, k := newAgencyKeyFixture(t, models.APIPermAgencyRead)
	ctx := context.Background()
	if got, xerr := svc.Authenticate(ctx, k.Secret); xerr != nil || got.ID != k.ID {
		t.Fatalf("live key refused: %v", xerr)
	}
	if _, xerr := svc.Authenticate(ctx, "wmbly_"+k.Secret[len(AgencyKeyPrefix):]); xerr != errx.ErrAuth {
		t.Errorf("a workspace-branded key authenticated as an agency key: %v", xerr)
	}
	if _, xerr := svc.Authenticate(ctx, k.Secret+"x"); xerr != errx.ErrAuth {
		t.Errorf("an unknown key authenticated: %v", xerr)
	}
	staff.roles[k.CreatedBy] = models.AgencyRoleMember
	if _, xerr := svc.Authenticate(ctx, k.Secret); identifier(xerr) != CodeAgencyKeyHolderLeft {
		t.Errorf("a key outlived its creator's admin role: %v", xerr)
	}
	delete(staff.roles, k.CreatedBy)
	if _, xerr := svc.Authenticate(ctx, k.Secret); identifier(xerr) != CodeAgencyKeyHolderLeft {
		t.Errorf("a key outlived its creator leaving the agency: %v", xerr)
	}
}

func TestAgencyKeyReachesOnlyCurrentClients(t *testing.T) {
	svc, staff, k := newAgencyKeyFixture(t, models.APIPermReadCampaigns)
	ctx := context.Background()
	client, stranger, otherAgencysClient := uuid.New(), uuid.New(), uuid.New()
	staff.clients[client] = staff.agency
	staff.clients[otherAgencysClient] = uuid.New()

	if xerr := svc.RequireClient(ctx, &k.AgencyAPIKey, client); xerr != nil {
		t.Fatalf("client refused: %v", xerr)
	}
	for name, org := range map[string]uuid.UUID{"not a client": stranger, "another agency's client": otherAgencysClient} {
		if xerr := svc.RequireClient(ctx, &k.AgencyAPIKey, org); identifier(xerr) != CodeAgencyKeyNotClient {
			t.Errorf("%s reached: %v", name, xerr)
		}
	}
	delete(staff.clients, client)
	if xerr := svc.RequireClient(ctx, &k.AgencyAPIKey, client); identifier(xerr) != CodeAgencyKeyNotClient {
		t.Errorf("a detached client is still reachable: %v", xerr)
	}
}
