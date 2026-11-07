package mailboximport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestAuthorizingMessageNamesTheVendorSigninStage(t *testing.T) {
	trust := authorizingMessage(VendorAuthorization{Pending: true, Trusting: true, Vendor: "Zapmail", Stage: "queued"}, causeGoogleSignin, "acme.io")
	for _, want := range []string{"Authorizing: Zapmail is adding Warmbly as a trusted app on acme.io", "Zapmail status: queued", "signs in to each mailbox", "within 2 hours"} {
		if !strings.Contains(trust, want) {
			t.Fatalf("trust message %q lacks %q", trust, want)
		}
	}
	signin := authorizingMessage(VendorAuthorization{Pending: true, SigningIn: true, Vendor: "InboxKit", Stage: "processing"}, causeGoogleSignin, "acme.io")
	for _, want := range []string{"Signing in: InboxKit is signing in to this mailbox", "InboxKit status: processing", "use Sign in on this row", "within 6 hours"} {
		if !strings.Contains(signin, want) {
			t.Fatalf("sign-in message %q lacks %q", signin, want)
		}
	}
	if strings.Contains(signin, "admin sign-in") || strings.Contains(trust, "admin sign-in") {
		t.Fatal("a Google row was offered a Microsoft admin sign-in")
	}
}

// consentVendors answers per address and counts how often each row was asked.
type consentVendors struct {
	fakeVendors
	answers map[string]VendorAuthorization
	asked   map[string]int
	settled map[string]string
}

func (v *consentVendors) AuthorizeMailbox(_ context.Context, r VendorAuthorizeRequest) VendorAuthorization {
	v.asked[r.Email]++
	return v.answers[r.Email]
}

func (v *consentVendors) ConsentSettled(_ context.Context, _, _ uuid.UUID, email, reason string) {
	v.settled[email] = reason
}

type parkedRepo struct {
	repository.MailboxImportRepository
	rows     []repository.ImportWorkRow
	resumed  []int
	messages map[int]string
	imp      *models.MailboxImport
	row      *models.MailboxImportRow
}

func (r *parkedRepo) ParkedRows(context.Context, string, int) ([]repository.ImportWorkRow, error) {
	return r.rows, nil
}
func (r *parkedRepo) SetParkedMessage(_ context.Context, _ uuid.UUID, line int, _, message string) (bool, error) {
	r.messages[line] = message
	return true, nil
}
func (r *parkedRepo) TouchParked(context.Context, string, []repository.ImportWorkRow) error {
	return nil
}
func (r *parkedRepo) ResumeParked(_ context.Context, _ uuid.UUID, line int, _ string) error {
	r.resumed = append(r.resumed, line)
	return nil
}
func (r *parkedRepo) Get(_ context.Context, orgID, id uuid.UUID) (*models.MailboxImport, error) {
	if r.imp == nil || r.imp.OrganizationID != orgID || r.imp.ID != id {
		return nil, nil
	}
	return r.imp, nil
}
func (r *parkedRepo) GetRow(_ context.Context, orgID, id uuid.UUID, line int) (*models.MailboxImportRow, string, error) {
	if r.imp == nil || r.imp.OrganizationID != orgID || r.imp.ID != id || r.row == nil || r.row.Line != line {
		return nil, "", nil
	}
	return r.row, "", nil
}

func sealedPayload(t *testing.T, ciph cipher.CipherService, org uuid.UUID, p payload) string {
	t.Helper()
	c, err := ciph.Cipher(context.Background(), org)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p)
	out, err := c.Encrypt(context.Background(), string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestResumeAsksEachMailboxOnceTheDomainIsTrusted(t *testing.T) {
	org, conn, imp, owner := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ciph := cipher.NewStatic([]byte(strings.Repeat("k", 32)))
	row := func(line int, email, mailbox string) repository.ImportWorkRow {
		return repository.ImportWorkRow{ImportID: imp, OrgID: org, CreatedBy: &owner, Line: line, Email: email, Code: causeGoogleSignin,
			Payload: sealedPayload(t, ciph, org, payload{VendorConnectionID: &conn, VendorMailboxID: mailbox})}
	}
	repo := &parkedRepo{messages: map[int]string{}, rows: []repository.ImportWorkRow{
		row(1, "ava@trusting.io", "m1"), row(2, "leo@trusting.io", "m2"),
		row(3, "mia@signing.io", "m3"), row(4, "noah@signing.io", "m4"), row(5, "zoe@signing.io", "m5"),
	}}
	vendors := &consentVendors{asked: map[string]int{}, settled: map[string]string{}, answers: map[string]VendorAuthorization{
		"ava@trusting.io": {Pending: true, Trusting: true, Domain: true, Vendor: "InboxKit"},
		"leo@trusting.io": {Message: "must not be asked: the domain answer covers it"},
		"mia@signing.io":  {Pending: true, SigningIn: true, Vendor: "InboxKit", Stage: "queued"},
		"noah@signing.io": {Pending: true, SigningIn: true, Vendor: "InboxKit", Stage: "processing"},
		"zoe@signing.io":  {Message: "InboxKit could not sign in to zoe@signing.io for Warmbly. Use Sign in on this row."},
	}}
	s := &Service{repo: repo, cipher: ciph, vendors: vendors, kick: make(chan struct{}, 1)}
	s.resumeVendorAuthorizations(context.Background())

	if vendors.asked["leo@trusting.io"] != 0 || vendors.asked["ava@trusting.io"] != 1 {
		t.Fatalf("a domain-wide answer was not reused: %v", vendors.asked)
	}
	for _, e := range []string{"mia@signing.io", "noah@signing.io", "zoe@signing.io"} {
		if vendors.asked[e] != 1 {
			t.Fatalf("%s asked %d times, want once each: %v", e, vendors.asked[e], vendors.asked)
		}
	}
	if len(repo.resumed) != 1 || repo.resumed[0] != 5 {
		t.Fatalf("resumed lines %v, want only the row whose sign-in ended", repo.resumed)
	}
	if !strings.HasPrefix(repo.messages[2], "Authorizing:") || !strings.HasPrefix(repo.messages[3], "Signing in:") || !strings.Contains(repo.messages[4], "status: processing") {
		t.Fatalf("parked messages = %v", repo.messages)
	}
}

func TestVendorConsentWantedOnlyForTheWaitingRow(t *testing.T) {
	org, imp := uuid.New(), uuid.New()
	repo := &parkedRepo{
		imp: &models.MailboxImport{ID: imp, OrganizationID: org, Status: models.ImportRunning},
		row: &models.MailboxImportRow{Line: 7, Email: "Ava@Acme.io", Status: models.ImportRowNeedsSignin},
	}
	s := &Service{repo: repo}
	ctx := context.Background()
	if !s.VendorConsentWanted(ctx, org, imp, 7, "ava@acme.io") {
		t.Fatal("the waiting row was refused")
	}
	for name, ok := range map[string]bool{
		"another workspace": s.VendorConsentWanted(ctx, uuid.New(), imp, 7, "ava@acme.io"),
		"another line":      s.VendorConsentWanted(ctx, org, imp, 8, "ava@acme.io"),
		"another address":   s.VendorConsentWanted(ctx, org, imp, 7, "leo@acme.io"),
	} {
		if ok {
			t.Fatalf("%s was accepted", name)
		}
	}
	repo.row.Status = models.ImportRowConnected
	if s.VendorConsentWanted(ctx, org, imp, 7, "ava@acme.io") {
		t.Fatal("a connected row was accepted")
	}
	repo.row.Status, repo.imp.Status = models.ImportRowNeedsSignin, models.ImportCancelled
	if s.VendorConsentWanted(ctx, org, imp, 7, "ava@acme.io") {
		t.Fatal("a cancelled import was accepted")
	}
}

func TestVendorConsentSettledReachesTheVendor(t *testing.T) {
	vendors := &consentVendors{settled: map[string]string{}}
	s := &Service{vendors: vendors}
	s.VendorConsentSettled(context.Background(), uuid.New(), uuid.New(), "ava@acme.io", "wrong account")
	if vendors.settled["ava@acme.io"] != "wrong account" {
		t.Fatalf("settled = %v", vendors.settled)
	}
}
