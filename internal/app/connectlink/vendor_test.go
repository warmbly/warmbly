package connectlink

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/vendorconn"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// fakeVendors is a vendor account with three mailboxes; a ticket is good once, for the link it was made for.
type fakeVendors struct {
	tickets   map[string]string // ticket -> scope
	created   []vendorconn.CreateInput
	deleted   int
	imported  []string
	importErr *errx.Error
	options   models.MailboxImportOptions
	actor     uuid.UUID
}

var vendorBoxes = []models.VendorMailbox{
	{ID: "m1", Email: "ava@client.test"}, {ID: "m2", Email: "leo@client.test"}, {ID: "m3", Email: "mia@client.test", Connected: true},
}

func (f *fakeVendors) Catalog() []vendorconn.Vendor {
	return []vendorconn.Vendor{{ID: "zapmail", Label: "Zapmail", Fields: []vendorconn.VendorField{{Key: "api_key", Label: "API key", Secret: true, Required: true}}}}
}

func (f *fakeVendors) Hold(_ context.Context, _ uuid.UUID, scope string, in vendorconn.CreateInput) (string, []models.VendorMailbox, *errx.Error) {
	if in.Fields["api_key"] != "good" {
		return "", nil, errx.NewWithIdentifier(errx.BadRequest, "mailbox_vendor_unauthorized", "refused")
	}
	if f.tickets == nil {
		f.tickets = map[string]string{}
	}
	ticket := uuid.NewString()
	f.tickets[ticket] = scope
	return ticket, vendorBoxes, nil
}

func (f *fakeVendors) Redeem(_ context.Context, _ uuid.UUID, scope, ticket string) (*vendorconn.CreateInput, *errx.Error) {
	if got, ok := f.tickets[ticket]; !ok || got != scope {
		return nil, vendorconn.ErrHoldExpired
	}
	delete(f.tickets, ticket)
	return &vendorconn.CreateInput{Vendor: "zapmail", Fields: map[string]string{"api_key": "good"}}, nil
}

func (f *fakeVendors) Create(_ context.Context, _, userID uuid.UUID, in vendorconn.CreateInput) (*models.VendorConnection, *errx.Error) {
	f.created, f.actor = append(f.created, in), userID
	return &models.VendorConnection{ID: uuid.New(), Vendor: in.Vendor, Label: in.Label}, nil
}

func (f *fakeVendors) ImportPicked(_ context.Context, _, _, _ uuid.UUID, in vendorconn.ImportInput, _ ...int) (*models.MailboxImport, []string, *errx.Error) {
	if f.importErr != nil {
		return nil, nil, f.importErr
	}
	f.options = in.Options
	var emails []string
	for _, id := range in.MailboxIDs {
		for _, b := range vendorBoxes {
			if b.ID == id {
				emails = append(emails, b.Email)
			}
		}
	}
	f.imported = emails
	return &models.MailboxImport{ID: uuid.New()}, emails, nil
}

func (f *fakeVendors) Delete(context.Context, uuid.UUID, uuid.UUID) *errx.Error {
	f.deleted++
	return nil
}

func (r *fakeRepo) ReserveMany(_ context.Context, orgID, id uuid.UUID, n int) (bool, *errx.Error) {
	l := r.link
	if l.OrganizationID != orgID || l.ID != id || l.MailboxesConnected+n > l.MaxMailboxes {
		return false, nil
	}
	l.MailboxesConnected += n
	return true, nil
}

func (r *fakeRepo) ReleaseMany(_ context.Context, _, _ uuid.UUID, n int) *errx.Error {
	r.link.MailboxesConnected -= n
	return nil
}

// Get lets Page read the workspace's name; none is needed here.
func (w *fakeWorkspaces) Get(context.Context, uuid.UUID) (*models.Organization, *errx.Error) {
	return nil, nil
}

func vendorFixture(t *testing.T) (*Service, *fakeRepo, *fakeVendors, *models.ClientConnectLink) {
	t.Helper()
	s, repo, _, _, token := newFixture(t)
	repo.link.AllowedProviders = []string{"smtp_imap", "vendor"}
	repo.link.Warmup = true
	repo.link.TagIDs = []uuid.UUID{uuid.New()}
	v := &fakeVendors{}
	s.WireVendors(v)
	link, xerr := s.Resolve(context.Background(), token)
	if xerr != nil {
		t.Fatal(xerr)
	}
	return s, repo, v, link
}

func TestAClientConnectsMailboxesFromItsVendorAccount(t *testing.T) {
	s, repo, v, link := vendorFixture(t)
	ctx := context.Background()

	if page := s.Page(ctx, link); len(page.Vendors) != 1 || page.Vendors[0].ID != "zapmail" {
		t.Fatalf("the page offers %+v", page.Vendors)
	}
	if _, xerr := s.VendorMailboxes(ctx, link, VendorAccountInput{Vendor: "zapmail", Fields: map[string]string{"api_key": "bad"}}); xerr == nil {
		t.Fatal("a key the vendor refuses was listed")
	}
	list, xerr := s.VendorMailboxes(ctx, link, VendorAccountInput{Vendor: "zapmail", Fields: map[string]string{"api_key": "good"}})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if list.Ticket == "" || len(list.Mailboxes) != 3 || list.Remaining != 2 || !list.Mailboxes[2].Connected {
		t.Fatalf("list = %+v", list)
	}

	res, xerr := s.VendorImport(ctx, link, VendorImportInput{Ticket: list.Ticket, MailboxIDs: []string{"m1", "m2", "m1", ""}})
	if xerr != nil {
		t.Fatal(xerr)
	}
	// Imported in the creator's name, with the link's tags and warmup, and each mailbox counted and recorded.
	if v.actor != *repo.link.CreatedBy || len(v.imported) != 2 || len(repo.uses) != 2 || repo.link.MailboxesConnected != 2 || res.Link.MailboxesConnected != 2 {
		t.Fatalf("imported %v as %s, uses %d, link at %d", v.imported, v.actor, len(repo.uses), repo.link.MailboxesConnected)
	}
	if v.options.Settings.Warmup == nil || !*v.options.Settings.Warmup || len(v.options.Settings.TagIDs) != 1 || v.created[0].Label != "Zapmail (from a client)" {
		t.Fatalf("options %+v, connection %+v", v.options.Settings, v.created[0])
	}
	// The ticket was good once.
	fresh, _ := repo.Get(ctx, link.OrganizationID, link.ID)
	if _, xerr := s.VendorImport(ctx, fresh, VendorImportInput{Ticket: list.Ticket, MailboxIDs: []string{"m3"}}); xerr == nil {
		t.Fatal("a ticket was used twice")
	}
}

func TestAVendorImportIsHeldToTheLinksLimit(t *testing.T) {
	s, repo, v, link := vendorFixture(t)
	ctx := context.Background()
	list, xerr := s.VendorMailboxes(ctx, link, VendorAccountInput{Vendor: "zapmail", Fields: map[string]string{"api_key": "good"}})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := s.VendorImport(ctx, link, VendorImportInput{Ticket: list.Ticket}); xerr == nil || xerr.Identifier != "client_link_no_mailboxes" {
		t.Fatalf("nothing picked: got %v", xerr)
	}
	// More than the link takes is refused before the ticket is spent, so picking fewer needs no key again.
	if _, xerr := s.VendorImport(ctx, link, VendorImportInput{Ticket: list.Ticket, MailboxIDs: []string{"m1", "m2", "m3"}}); xerr == nil || xerr.Identifier != "client_link_too_many_mailboxes" {
		t.Fatalf("three on a link of two: got %v", xerr)
	}
	if len(v.created) != 0 || repo.link.MailboxesConnected != 0 {
		t.Fatalf("a refused import left %d connections and %d slots taken", len(v.created), repo.link.MailboxesConnected)
	}
	if _, xerr := s.VendorImport(ctx, link, VendorImportInput{Ticket: list.Ticket, MailboxIDs: []string{"m1"}}); xerr != nil {
		t.Fatalf("picking fewer with the same ticket: %v", xerr)
	}
}

func TestAFailedVendorImportGivesItsSlotsBack(t *testing.T) {
	s, repo, v, link := vendorFixture(t)
	ctx := context.Background()
	list, xerr := s.VendorMailboxes(ctx, link, VendorAccountInput{Vendor: "zapmail", Fields: map[string]string{"api_key": "good"}})
	if xerr != nil {
		t.Fatal(xerr)
	}
	v.importErr = errx.InternalError()
	if _, xerr := s.VendorImport(ctx, link, VendorImportInput{Ticket: list.Ticket, MailboxIDs: []string{"m1", "m2"}}); xerr == nil {
		t.Fatal("a failed import was reported as started")
	}
	if repo.link.MailboxesConnected != 0 || v.deleted != 1 || len(repo.uses) != 0 {
		t.Fatalf("slots %d, connections removed %d, uses %d", repo.link.MailboxesConnected, v.deleted, len(repo.uses))
	}
}

func TestALinkThatDoesNotTakeAVendorAccountRefusesOne(t *testing.T) {
	s, repo, _, link := vendorFixture(t)
	ctx := context.Background()
	in := VendorAccountInput{Vendor: "zapmail", Fields: map[string]string{"api_key": "good"}}

	repo.link.AllowedProviders = []string{"smtp_imap"}
	plain, _ := repo.Get(ctx, link.OrganizationID, link.ID)
	if _, xerr := s.VendorMailboxes(ctx, plain, in); xerr == nil || xerr.Identifier != "client_link_provider_not_allowed" {
		t.Fatalf("a link without the vendor way in: got %v", xerr)
	}
	if page := s.Page(ctx, plain); len(page.Vendors) != 0 {
		t.Fatalf("the page offers vendors it does not take: %+v", page.Vendors)
	}
	// A ticket made for one link is no good on another.
	list, xerr := s.VendorMailboxes(ctx, link, in)
	if xerr != nil {
		t.Fatal(xerr)
	}
	other := *link
	other.ID = uuid.New()
	repo.link.ID = other.ID
	if _, xerr := s.VendorImport(ctx, &other, VendorImportInput{Ticket: list.Ticket, MailboxIDs: []string{"m1"}}); xerr == nil || xerr.Identifier != "mailbox_vendor_hold_expired" {
		t.Fatalf("a ticket of another link: got %v", xerr)
	}
	// Without the vendor service the way in is simply not offered.
	s.WireVendors(nil)
	if _, xerr := s.VendorMailboxes(ctx, link, in); xerr == nil || xerr.Identifier != "client_link_provider_unavailable" {
		t.Fatalf("no vendor service: got %v", xerr)
	}
}
