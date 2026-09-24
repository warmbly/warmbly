package vendorconn

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/mailboximport"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailvendor"
	"github.com/warmbly/warmbly/internal/repository"
)

type fakeClient struct {
	fields   map[string]string
	verify   error
	boxes    []mailvendor.Mailbox
	creds    map[string]mailvendor.Credentials
	lists    int
	credsHit int
}

func (f *fakeClient) Vendor() string               { return "inboxkit" }
func (f *fakeClient) Verify(context.Context) error { return f.verify }
func (f *fakeClient) List(context.Context) ([]mailvendor.Mailbox, error) {
	f.lists++
	return f.boxes, nil
}
func (f *fakeClient) Credentials(_ context.Context, m mailvendor.Mailbox) (mailvendor.Credentials, error) {
	f.credsHit++
	c, ok := f.creds[m.ID]
	if !ok {
		return mailvendor.Credentials{}, mailvendor.ErrNotFound
	}
	return c, nil
}

type memVendors struct {
	conns map[uuid.UUID]*models.VendorConnection
}

func (m *memVendors) Create(_ context.Context, c *models.VendorConnection, _ uuid.UUID) error {
	cp := *c
	cp.Status = "active"
	m.conns[c.ID] = &cp
	return nil
}
func (m *memVendors) List(_ context.Context, org uuid.UUID) ([]models.VendorConnection, error) {
	var out []models.VendorConnection
	for _, c := range m.conns {
		if c.OrganizationID == org {
			cp := *c
			cp.Credentials = ""
			out = append(out, cp)
		}
	}
	return out, nil
}
func (m *memVendors) Get(_ context.Context, org, id uuid.UUID) (*models.VendorConnection, error) {
	if c, ok := m.conns[id]; ok && c.OrganizationID == org {
		cp := *c
		return &cp, nil
	}
	return nil, nil
}
func (m *memVendors) Delete(_ context.Context, org, id uuid.UUID) (bool, error) {
	if c, ok := m.conns[id]; ok && c.OrganizationID == org {
		delete(m.conns, id)
		return true, nil
	}
	return false, nil
}
func (m *memVendors) Update(_ context.Context, org, id uuid.UUID, label, sealed string) (bool, error) {
	c, ok := m.conns[id]
	if !ok || c.OrganizationID != org {
		return false, nil
	}
	c.Label = label
	if sealed != "" {
		c.Credentials, c.Status = sealed, "active"
	}
	return true, nil
}
func (m *memVendors) SetStatus(_ context.Context, id uuid.UUID, status, last string) error {
	if c, ok := m.conns[id]; ok {
		c.Status, c.LastError = status, last
	}
	return nil
}
func (m *memVendors) ReconnectCandidates(context.Context, int) ([]repository.VendorReconnect, error) {
	return nil, nil
}

type fakeMailboxes struct {
	existing map[string]models.EmailRef
	links    map[uuid.UUID]string
	stored   *repository.SMTPCredentials
}

func (f *fakeMailboxes) FindManyInOrganization(context.Context, uuid.UUID, []string) (map[string]models.EmailRef, *errx.Error) {
	return f.existing, nil
}
func (f *fakeMailboxes) SetVendorLink(_ context.Context, a, _ uuid.UUID, id string) *errx.Error {
	f.links[a] = id
	return nil
}
func (f *fakeMailboxes) GetSMTPCredentials(context.Context, uuid.UUID) (*repository.SMTPCredentials, *errx.Error) {
	return f.stored, nil
}

type fakeReconnect struct{ got *models.SmtpImap }

func (f *fakeReconnect) UpdateSMTPIMAPCredentials(_ context.Context, _ *uuid.UUID, _ uuid.UUID, c *models.SmtpImap) (*models.Email, *errx.Error) {
	f.got = c
	return &models.Email{}, nil
}

type fakeImporter struct{ in mailboximport.ListInput }

func (f *fakeImporter) CreateFromList(_ context.Context, in mailboximport.ListInput) (*models.MailboxImport, *errx.Error) {
	f.in = in
	return &models.MailboxImport{ID: uuid.New(), Total: len(in.Rows)}, nil
}

func newTestService(t *testing.T, client *fakeClient) (*Service, *memVendors, *fakeMailboxes, *fakeImporter, *fakeReconnect) {
	t.Helper()
	repo := &memVendors{conns: map[uuid.UUID]*models.VendorConnection{}}
	mb := &fakeMailboxes{existing: map[string]models.EmailRef{}, links: map[uuid.UUID]string{}}
	imp := &fakeImporter{}
	rc := &fakeReconnect{}
	s := NewService(Deps{
		Repo: repo, Cipher: cipher.NewStatic([]byte(strings.Repeat("k", 32))), Mailboxes: mb, Reconnect: rc, Importer: imp,
		NewClient: func(vendor string, fields map[string]string) (mailvendor.Client, error) {
			if _, ok := mailvendor.Lookup(vendor); !ok {
				return nil, mailvendor.ErrUnknownVendor
			}
			client.fields = fields
			return client, nil
		},
	})
	return s, repo, mb, imp, rc
}

func TestCreateVerifiesAndSealsTheKey(t *testing.T) {
	client := &fakeClient{}
	s, repo, _, _, _ := newTestService(t, client)
	org := uuid.New()
	c, xerr := s.Create(context.Background(), org, uuid.New(), CreateInput{Vendor: "inboxkit", Fields: map[string]string{"api_key": " ik_secret ", "workspace_id": "w1"}})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if client.fields["api_key"] != "ik_secret" {
		t.Fatalf("fields were not trimmed: %v", client.fields)
	}
	stored := repo.conns[c.ID]
	if stored.Credentials == "" || strings.Contains(stored.Credentials, "ik_secret") {
		t.Fatalf("key stored in the clear: %q", stored.Credentials)
	}
	if c.Credentials != "" || c.Label != "InboxKit" {
		t.Fatalf("returned connection = %+v", c)
	}
	client.verify = mailvendor.ErrUnauthorized
	if _, xerr := s.Create(context.Background(), org, uuid.New(), CreateInput{Vendor: "inboxkit", Fields: map[string]string{"api_key": "bad"}}); xerr == nil || xerr.Identifier != mailboximport.ErrIDVendorUnauthorized {
		t.Fatalf("a rejected key = %v", xerr)
	}
	if _, xerr := s.Create(context.Background(), org, uuid.New(), CreateInput{Vendor: "nope"}); xerr == nil || xerr.Identifier != ErrIDUnknownVendor {
		t.Fatalf("an unknown vendor = %v", xerr)
	}
}

func TestMailboxesMarksConnectedAndImportPicks(t *testing.T) {
	client := &fakeClient{boxes: []mailvendor.Mailbox{
		{ID: "a", Email: "a@acme.io", FirstName: "Alex", LastName: "R", Provider: "google"},
		{ID: "b", Email: "b@acme.io"},
		{ID: "c", Email: "c@acme.io"},
	}}
	s, _, mb, imp, _ := newTestService(t, client)
	org := uuid.New()
	c, _ := s.Create(context.Background(), org, uuid.New(), CreateInput{Vendor: "inboxkit", Fields: map[string]string{"api_key": "k", "workspace_id": "w"}})
	mb.existing["b@acme.io"] = models.EmailRef{ID: uuid.New()}

	boxes, xerr := s.Mailboxes(context.Background(), org, c.ID)
	if xerr != nil || len(boxes) != 3 || !boxes[1].Connected || boxes[0].Name != "Alex R" {
		t.Fatalf("boxes = %+v, %v", boxes, xerr)
	}
	if _, xerr := s.Mailboxes(context.Background(), uuid.New(), c.ID); xerr == nil {
		t.Fatal("another workspace listed the vendor account")
	}
	if _, xerr := s.Import(context.Background(), org, uuid.New(), c.ID, ImportInput{All: true}); xerr != nil {
		t.Fatal(xerr)
	}
	if len(imp.in.Rows) != 2 || imp.in.Source != "vendor" || imp.in.Rows[0].VendorMailboxID != "a" || *imp.in.Rows[0].VendorConnectionID != c.ID {
		t.Fatalf("all picked = %+v (the connected one is left out)", imp.in.Rows)
	}
	if _, xerr := s.Import(context.Background(), org, uuid.New(), c.ID, ImportInput{MailboxIDs: []string{"b"}}); xerr != nil || len(imp.in.Rows) != 1 {
		t.Fatalf("an explicit pick includes a connected mailbox for updating: %+v", imp.in.Rows)
	}
}

func TestFieldsMapsCredentialsToImportColumns(t *testing.T) {
	client := &fakeClient{creds: map[string]mailvendor.Credentials{
		"a": {Password: "pw", AppPassword: "abcdefghijklmnop"},
		"f": {Password: "pw2", SMTP: &mailvendor.Endpoint{Host: "smtp.forge.io", Port: 465, Security: "tls", Username: "f@x.io"},
			IMAP: &mailvendor.Endpoint{Host: "imap.forge.io", Port: 993, Password: "imap-only"}},
	}}
	s, _, _, _, _ := newTestService(t, client)
	org := uuid.New()
	c, _ := s.Create(context.Background(), org, uuid.New(), CreateInput{Vendor: "inboxkit", Fields: map[string]string{"api_key": "k", "workspace_id": "w"}})

	f, xerr := s.Fields(context.Background(), org, c.ID, "a", "a@acme.io", "google")
	if xerr != nil || f[models.ImportFieldAppPassword] != "abcdefghijklmnop" || f[models.ImportFieldSMTPHost] != "" {
		t.Fatalf("google fields = %v, %v", f, xerr)
	}
	f, xerr = s.Fields(context.Background(), org, c.ID, "f", "f@x.io", "smtp")
	if xerr != nil || f[models.ImportFieldSMTPPort] != "465" || f[models.ImportFieldIMAPPassword] != "imap-only" || f[models.ImportFieldSMTPUsername] != "f@x.io" {
		t.Fatalf("smtp fields = %v, %v", f, xerr)
	}
	if _, xerr := s.Fields(context.Background(), org, c.ID, "gone", "g@x.io", ""); xerr == nil || xerr.Identifier != ErrIDUnavailable {
		t.Fatalf("a missing mailbox = %v", xerr)
	}
}

func TestReconnectOnlyWhenTheVendorPasswordChanged(t *testing.T) {
	client := &fakeClient{creds: map[string]mailvendor.Credentials{"a": {AppPassword: "newpasswordabcde"}}}
	s, _, mb, _, rc := newTestService(t, client)
	org := uuid.New()
	c, _ := s.Create(context.Background(), org, uuid.New(), CreateInput{Vendor: "inboxkit", Fields: map[string]string{"api_key": "k", "workspace_id": "w"}})
	cand := repository.VendorReconnect{AccountID: uuid.New(), OrgID: org, ConnectionID: c.ID, VendorMailboxID: "a", Email: "a@acme.io"}

	mb.stored = &repository.SMTPCredentials{SMTPHost: "smtp.gmail.com", SMTPPort: 587, SMTPPassword: "newpasswordabcde", IMAPPassword: "newpasswordabcde"}
	s.reconnectOne(context.Background(), cand)
	if rc.got != nil {
		t.Fatal("reconnected with the same password")
	}
	mb.stored.SMTPPassword, mb.stored.IMAPPassword = "old", "old"
	s.reconnectOne(context.Background(), cand)
	if rc.got == nil || rc.got.SMTP.Password != "newpasswordabcde" || rc.got.SMTP.Host != "smtp.gmail.com" {
		t.Fatalf("reconnect = %+v", rc.got)
	}
}

func TestClientIsReusedAcrossRows(t *testing.T) {
	client := &fakeClient{boxes: []mailvendor.Mailbox{{ID: "a", Email: "a@acme.io"}}, creds: map[string]mailvendor.Credentials{"a": {Password: "p"}}}
	calls := 0
	repo := &memVendors{conns: map[uuid.UUID]*models.VendorConnection{}}
	s := NewService(Deps{
		Repo: repo, Cipher: cipher.NewStatic([]byte(strings.Repeat("k", 32))),
		Mailboxes: &fakeMailboxes{existing: map[string]models.EmailRef{}, links: map[uuid.UUID]string{}},
		NewClient: func(string, map[string]string) (mailvendor.Client, error) { calls++; return client, nil },
	})
	org := uuid.New()
	c, _ := s.Create(context.Background(), org, uuid.New(), CreateInput{Vendor: "inboxkit", Fields: map[string]string{"api_key": "k", "workspace_id": "w"}})
	calls = 0
	for i := 0; i < 5; i++ {
		if _, xerr := s.Fields(context.Background(), org, c.ID, "a", "a@acme.io", ""); xerr != nil {
			t.Fatal(xerr)
		}
	}
	if calls != 1 {
		t.Fatalf("built %d clients for one connection", calls)
	}
}

func TestPendingNoteNamesTheStepOrWhoToAsk(t *testing.T) {
	now := time.Date(2026, 9, 24, 16, 20, 0, 0, time.UTC)
	stalled := pendingNote("InboxKit", models.GrantProviderMicrosoft, "app", nil, "ws:39235702",
		mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationPending, Stage: "processing", UpdatedAt: now.Add(-49 * time.Minute)}, now)
	if !strings.Contains(stalled, "since 15:31 UTC") || !strings.Contains(stalled, "request 39235702") || strings.Contains(stalled, "ws:") {
		t.Fatalf("stalled note = %q", stalled)
	}
	if n := pendingNote("InboxKit", models.GrantProviderMicrosoft, "app", nil, "ws:1",
		mailvendor.AuthorizationStatus{Stage: "processing", UpdatedAt: now.Add(-2 * time.Minute)}, now); n != "" {
		t.Fatalf("a request that just moved got a note: %q", n)
	}
	google := pendingNote("InboxKit", models.GrantProviderGoogle, "1234567890", []string{"https://www.googleapis.com/auth/gmail.modify"}, "ws:1",
		mailvendor.AuthorizationStatus{Stage: "pending", UpdatedAt: now}, now)
	if !strings.Contains(google, "client ID 1234567890") || !strings.Contains(google, "Domain-wide delegation") || !strings.Contains(google, "gmail.modify") {
		t.Fatalf("google note = %q", google)
	}
}
