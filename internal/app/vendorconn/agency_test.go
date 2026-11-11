package vendorconn

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/pkg/mailvendor"
	"github.com/warmbly/warmbly/internal/repository"
)

// memAgencyVendors is the agency's accounts in memory, writing workspace connections into the same store the service reads.
type memAgencyVendors struct {
	accounts map[uuid.UUID]*models.AgencyVendorAccount
	conns    *memVendors
	// clients are the agency's client workspaces; a link is only made for one of them.
	clients map[uuid.UUID]bool
}

func (m *memAgencyVendors) Create(_ context.Context, a *models.AgencyVendorAccount, _ uuid.UUID) error {
	cp := *a
	m.accounts[a.ID] = &cp
	return nil
}

func (m *memAgencyVendors) List(_ context.Context, agencyID uuid.UUID) ([]models.AgencyVendorAccount, error) {
	var out []models.AgencyVendorAccount
	for _, a := range m.accounts {
		if a.AgencyID != agencyID {
			continue
		}
		cp := *a
		cp.Credentials, cp.Status, cp.Clients = "", "active", nil
		for _, c := range m.conns.conns {
			if c.AgencyConnectionID != nil && *c.AgencyConnectionID == a.ID {
				cp.Clients = append(cp.Clients, models.AgencyVendorAccountClient{OrganizationID: c.OrganizationID, ConnectionID: c.ID, Status: c.Status})
			}
		}
		out = append(out, cp)
	}
	return out, nil
}

func (m *memAgencyVendors) Get(_ context.Context, agencyID, id uuid.UUID) (*models.AgencyVendorAccount, error) {
	if a, ok := m.accounts[id]; ok && a.AgencyID == agencyID {
		cp := *a
		return &cp, nil
	}
	return nil, nil
}

func (m *memAgencyVendors) AgencyOf(_ context.Context, id uuid.UUID) (uuid.UUID, bool, error) {
	if a, ok := m.accounts[id]; ok {
		return a.AgencyID, true, nil
	}
	return uuid.Nil, false, nil
}

func (m *memAgencyVendors) Update(_ context.Context, agencyID, id uuid.UUID, label, sealed string) (bool, error) {
	a, ok := m.accounts[id]
	if !ok || a.AgencyID != agencyID {
		return false, nil
	}
	a.Label = label
	if sealed != "" {
		a.Credentials = sealed
	}
	return true, nil
}

// Delete takes the workspace connections with it, as the foreign key does.
func (m *memAgencyVendors) Delete(_ context.Context, agencyID, id uuid.UUID) (bool, error) {
	if a, ok := m.accounts[id]; !ok || a.AgencyID != agencyID {
		return false, nil
	}
	delete(m.accounts, id)
	for cid, c := range m.conns.conns {
		if c.AgencyConnectionID != nil && *c.AgencyConnectionID == id {
			delete(m.conns.conns, cid)
		}
	}
	return true, nil
}

func (m *memAgencyVendors) Links(_ context.Context, id uuid.UUID) ([]repository.AgencyVendorLink, error) {
	var out []repository.AgencyVendorLink
	for _, c := range m.conns.conns {
		if c.AgencyConnectionID != nil && *c.AgencyConnectionID == id {
			out = append(out, repository.AgencyVendorLink{OrganizationID: c.OrganizationID, ConnectionID: c.ID})
		}
	}
	return out, nil
}

func (m *memAgencyVendors) Linked(_ context.Context, orgID, id uuid.UUID) (*models.VendorConnection, error) {
	for _, c := range m.conns.conns {
		if c.OrganizationID == orgID && c.AgencyConnectionID != nil && *c.AgencyConnectionID == id {
			cp := *c
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memAgencyVendors) CreateLinked(_ context.Context, c *models.VendorConnection, id, _ uuid.UUID) (bool, error) {
	if !m.clients[c.OrganizationID] {
		return false, nil
	}
	cp := *c
	cp.Status, cp.AgencyConnectionID = "active", &id
	m.conns.conns[c.ID] = &cp
	c.Status, c.AgencyConnectionID = "active", &id
	return true, nil
}

// staffRoles answers Get for the people on an agency's staff.
type staffRoles map[uuid.UUID]models.AgencyRole

func (s staffRoles) Get(_ context.Context, agencyID, userID uuid.UUID) (*models.Agency, *errx.Error) {
	role, ok := s[userID]
	if !ok {
		return nil, errx.NewWithIdentifier(errx.NotFound, "agency_not_found", "no")
	}
	return &models.Agency{ID: agencyID, Role: role}, nil
}

type clientList []models.AgencyClientMembership

func (c clientList) ClientMemberships(context.Context, uuid.UUID, uuid.UUID) ([]models.AgencyClientMembership, error) {
	return c, nil
}

type agencyFixture struct {
	svc                   *Service
	client                *fakeClient
	conns                 *memVendors
	accounts              *memAgencyVendors
	agency                uuid.UUID
	owner, member, guest  uuid.UUID
	acme, readOnly, other uuid.UUID
}

func newAgencyFixture(t *testing.T) *agencyFixture {
	t.Helper()
	f := &agencyFixture{client: &fakeClient{}, agency: uuid.New(), owner: uuid.New(), member: uuid.New(), guest: uuid.New(),
		acme: uuid.New(), readOnly: uuid.New(), other: uuid.New()}
	f.svc, f.conns, _, _, _ = newTestService(t, f.client)
	f.accounts = &memAgencyVendors{accounts: map[uuid.UUID]*models.AgencyVendorAccount{}, conns: f.conns, clients: map[uuid.UUID]bool{f.acme: true, f.readOnly: true}}
	instance, err := encrypt.NewEncrypter([]byte(strings.Repeat("i", 32)))
	if err != nil {
		t.Fatal(err)
	}
	admin, viewer := string(models.RoleAdmin), string(models.RoleViewer)
	f.svc.WireAgencies(f.accounts, staffRoles{f.owner: models.AgencyRoleOwner, f.member: models.AgencyRoleMember}, clientList{
		{OrganizationID: f.acme, Name: "Acme", Role: &admin, Permissions: models.RolePermissions[models.RoleAdmin], AccessScope: models.AccessScopeWorkspace},
		{OrganizationID: f.readOnly, Name: "Beta", Role: &viewer, Permissions: models.PermViewCampaigns, AccessScope: models.AccessScopeWorkspace},
	}, instance)
	return f
}

func (f *agencyFixture) account(t *testing.T) *models.AgencyVendorAccount {
	t.Helper()
	a, xerr := f.svc.CreateAgencyAccount(context.Background(), f.agency, f.owner, CreateInput{Vendor: "zapmail", Fields: map[string]string{"api_key": " zm_secret "}})
	if xerr != nil {
		t.Fatal(xerr)
	}
	return a
}

func TestAgencyAccountIsVerifiedSealedAndForManagersOnly(t *testing.T) {
	f := newAgencyFixture(t)
	ctx := context.Background()
	in := CreateInput{Vendor: "zapmail", Fields: map[string]string{"api_key": "zm_secret"}}

	if _, xerr := f.svc.CreateAgencyAccount(ctx, f.agency, f.member, in); xerr == nil || xerr.Identifier != "agency_forbidden" {
		t.Fatalf("a staff member who is not an admin: got %v", xerr)
	}
	if _, xerr := f.svc.CreateAgencyAccount(ctx, f.agency, f.guest, in); xerr == nil || xerr.Identifier != "agency_not_found" {
		t.Fatalf("someone outside the agency: got %v", xerr)
	}
	f.client.verify = mailvendor.ErrUnauthorized
	if _, xerr := f.svc.CreateAgencyAccount(ctx, f.agency, f.owner, in); xerr == nil || len(f.accounts.accounts) != 0 {
		t.Fatalf("a key the vendor refuses was stored: %v", xerr)
	}
	f.client.verify = nil

	a := f.account(t)
	stored := f.accounts.accounts[a.ID]
	if stored.Credentials == "" || strings.Contains(stored.Credentials, "zm_secret") || a.Credentials != "" || a.Label != "Zapmail" {
		t.Fatalf("stored %q, returned %+v", stored.Credentials, a)
	}
	// Any staff member lists the accounts, without a key.
	list, xerr := f.svc.AgencyAccounts(ctx, f.agency, f.member)
	if xerr != nil || len(list) != 1 || list[0].Credentials != "" {
		t.Fatalf("list = %+v, %v", list, xerr)
	}
}

func TestUsingAnAgencyAccountGivesTheClientItsOwnConnection(t *testing.T) {
	f := newAgencyFixture(t)
	ctx := context.Background()
	a := f.account(t)
	f.client.boxes = []mailvendor.Mailbox{{ID: "m1", Email: "alex@acme.test"}}

	conn, xerr := f.svc.UseAgencyAccount(ctx, f.agency, f.member, a.ID, f.acme)
	if xerr != nil {
		t.Fatal(xerr)
	}
	stored := f.conns.conns[conn.ID]
	if stored.OrganizationID != f.acme || stored.AgencyConnectionID == nil || *stored.AgencyConnectionID != a.ID || conn.Credentials != "" {
		t.Fatalf("connection = %+v", stored)
	}
	// Sealed for the workspace, not copied from the agency's sealed value.
	if stored.Credentials == f.accounts.accounts[a.ID].Credentials || strings.Contains(stored.Credentials, "zm_secret") {
		t.Fatalf("workspace credentials = %q", stored.Credentials)
	}
	// The workspace's connection works like any other: the vendor is asked with the agency's key.
	boxes, xerr := f.svc.Mailboxes(ctx, f.acme, conn.ID)
	if xerr != nil || len(boxes) != 1 || f.client.fields["api_key"] != "zm_secret" {
		t.Fatalf("mailboxes = %+v, %v, asked with %v", boxes, xerr, f.client.fields)
	}
	// Asking again finds the same connection.
	again, xerr := f.svc.UseAgencyAccount(ctx, f.agency, f.owner, a.ID, f.acme)
	if xerr != nil || again.ID != conn.ID || len(f.conns.conns) != 1 {
		t.Fatalf("second use = %+v, %v, %d connections", again, xerr, len(f.conns.conns))
	}
}

func TestUsingAnAgencyAccountIsHeldToTheAgencysClientsAndTheCallersAccess(t *testing.T) {
	f := newAgencyFixture(t)
	ctx := context.Background()
	a := f.account(t)

	if _, xerr := f.svc.UseAgencyAccount(ctx, f.agency, f.owner, a.ID, f.other); xerr == nil || xerr.Identifier != "agency_client_not_found" {
		t.Fatalf("a workspace that is not a client: got %v", xerr)
	}
	if _, xerr := f.svc.UseAgencyAccount(ctx, f.agency, f.owner, a.ID, f.readOnly); xerr == nil || xerr.Identifier != "agency_client_mailboxes_forbidden" {
		t.Fatalf("a client the caller cannot manage mailboxes in: got %v", xerr)
	}
	if _, xerr := f.svc.UseAgencyAccount(ctx, f.agency, f.guest, a.ID, f.acme); xerr == nil || xerr.Identifier != "agency_not_found" {
		t.Fatalf("someone outside the agency: got %v", xerr)
	}
	if _, xerr := f.svc.UseAgencyAccount(ctx, uuid.New(), f.owner, a.ID, f.acme); xerr == nil {
		t.Fatal("an account of another agency was used")
	}
	if len(f.conns.conns) != 0 {
		t.Fatalf("a refused use made a connection: %v", f.conns.conns)
	}
}

func TestANewKeyReachesEveryClientThatUsesTheAccount(t *testing.T) {
	f := newAgencyFixture(t)
	ctx := context.Background()
	a := f.account(t)
	conn, xerr := f.svc.UseAgencyAccount(ctx, f.agency, f.owner, a.ID, f.acme)
	if xerr != nil {
		t.Fatal(xerr)
	}
	before := f.conns.conns[conn.ID].Credentials

	if _, xerr := f.svc.UpdateAgencyAccount(ctx, f.agency, f.member, a.ID, CreateInput{Label: "x"}); xerr == nil || xerr.Identifier != "agency_forbidden" {
		t.Fatalf("a staff member who is not an admin: got %v", xerr)
	}
	out, xerr := f.svc.UpdateAgencyAccount(ctx, f.agency, f.owner, a.ID, CreateInput{Label: "Main Zapmail", Fields: map[string]string{"api_key": "zm_rotated"}})
	if xerr != nil {
		t.Fatal(xerr)
	}
	stored := f.conns.conns[conn.ID]
	if out.Label != "Main Zapmail" || stored.Label != "Main Zapmail" || stored.Credentials == before || len(out.Clients) != 1 {
		t.Fatalf("account %+v, connection %+v", out, stored)
	}
	if _, xerr := f.svc.Mailboxes(ctx, f.acme, conn.ID); xerr != nil || f.client.fields["api_key"] != "zm_rotated" {
		t.Fatalf("the client still uses %v (%v)", f.client.fields, xerr)
	}
	// Deleting the account ends its use in every client.
	if xerr := f.svc.DeleteAgencyAccount(ctx, f.agency, f.owner, a.ID); xerr != nil || len(f.conns.conns) != 0 || len(f.accounts.accounts) != 0 {
		t.Fatalf("delete: %v, %d connections left", xerr, len(f.conns.conns))
	}
}

func TestOnlyAgencyStaffBrowseAConnectionMadeFromAnAgencyAccount(t *testing.T) {
	f := newAgencyFixture(t)
	ctx := context.Background()
	a := f.account(t)
	conn, xerr := f.svc.UseAgencyAccount(ctx, f.agency, f.owner, a.ID, f.acme)
	if xerr != nil {
		t.Fatal(xerr)
	}
	// Someone the client invited is in the workspace but not on the agency's staff.
	if xerr := f.svc.GuardAgencyAccount(ctx, f.acme, conn.ID, f.guest, false); xerr == nil || xerr.Identifier != "agency_vendor_staff_only" {
		t.Fatalf("a client's own member: got %v", xerr)
	}
	if xerr := f.svc.GuardAgencyAccount(ctx, f.acme, conn.ID, f.member, false); xerr != nil {
		t.Fatalf("agency staff: %v", xerr)
	}
	// The key is the agency's, so it is not changed in one workspace.
	if xerr := f.svc.GuardAgencyAccount(ctx, f.acme, conn.ID, f.owner, true); xerr == nil || xerr.Identifier != "agency_vendor_managed" {
		t.Fatalf("a key change in the workspace: got %v", xerr)
	}
	// A connection the workspace made itself answers its own members.
	own, xerr := f.svc.Create(ctx, f.acme, f.guest, CreateInput{Vendor: "zapmail", Fields: map[string]string{"api_key": "own"}})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if xerr := f.svc.GuardAgencyAccount(ctx, f.acme, own.ID, f.guest, true); xerr != nil {
		t.Fatalf("the workspace's own connection: %v", xerr)
	}
	// And one of another workspace is not found.
	if xerr := f.svc.GuardAgencyAccount(ctx, f.readOnly, conn.ID, f.owner, false); xerr == nil {
		t.Fatal("a connection of another workspace passed the guard")
	}
}
