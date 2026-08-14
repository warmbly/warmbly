package confenge

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Synthetic path: import fixture → generate template draft → block bad copy →
// approve → enroll → reimport → DNC → bounce/HMAC. In-memory only.
func TestSyntheticOperationalFlow(t *testing.T) {
	var outcomes []models.OutreachOutcome
	rf := &memRepoOutcome{
		memRepoFull: *newMemRepoWithSettings(),
		outcomes:    &outcomes,
	}
	// Fix maps after value copy of memRepoFull
	rf.memRepo = newMemRepo()
	rf.settings = map[uuid.UUID]*models.OutreachOrgSettings{}
	rf.drafts = map[uuid.UUID]*models.OutreachDraft{}

	cfg := Config{
		Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120,
		RequireHumanApproval: true, MaxFeedPayloadBytes: DefaultMaxPayloadBytes,
	}
	svc := NewService(cfg, rf, nil).(*service)
	contacts := &mockContacts{}
	camps := &mockCampaigns{}
	svc.WireExecution(camps, contacts)

	org := uuid.New()
	user := uuid.New()
	raw := mustReadFixture(t, "native_feed_v1.json")
	raw = bytes.ReplaceAll(raw, []byte("acme.example.com"), []byte("pilot.warmbly.com"))

	run, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if run.Counts.Creates < 4 {
		t.Fatalf("import creates: %+v", run.Counts)
	}

	noMail, _ := rf.GetAccountByCNPJ(context.Background(), org, "55444333000122")
	if noMail == nil || noMail.QueueState != models.OutreachQueueNeedsContact {
		t.Fatalf("no-email company: %+v", noMail)
	}

	acme, _ := rf.GetAccountByCNPJ(context.Background(), org, "11222333000181")
	if acme == nil {
		t.Fatal("acme missing")
	}
	stampValidatedCandidates(t, rf.memRepo, org, acme.ID)

	draft, xerr := svc.GenerateDraft(context.Background(), org, user, acme.ID, nil)
	if xerr != nil {
		t.Fatal(xerr.Message)
	}
	if draft.Status != models.OutreachDraftNeedsReview {
		t.Fatalf("status %s", draft.Status)
	}

	// Banned phrase must fail approve after edit
	bad := "Identificamos dinheiro a receber na conta"
	if _, xerr = svc.ReviewDraft(context.Background(), org, user, draft.ID, "edit", &DraftEdit{BodyText: &bad}); xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr = svc.ReviewDraft(context.Background(), org, user, draft.ID, "approve", nil); xerr == nil {
		t.Fatal("expected approve to fail on banned phrase")
	}

	// Clean human edit + approve (body must clear structural min-word/fact-anchor floors)
	subj := "Sobre a prorrogacao na ACME"
	body := "Ola Ana,\n\nNotei " + acme.FactToMention + ".\n\n" +
		"Faz sentido conversarmos sobre o controle de aditivos desta obra?\n\n" +
		"Posso enviar um checklist de 1 pagina?\n\nAbracos"
	if _, xerr = svc.ReviewDraft(context.Background(), org, user, draft.ID, "edit", &DraftEdit{Subject: &subj, BodyText: &body}); xerr != nil {
		t.Fatal(xerr)
	}
	// Ensure fact_used and service align for validators
	dMid, _ := rf.GetDraft(context.Background(), org, draft.ID)
	dMid.FactUsed = acme.FactToMention
	dMid.ServiceCode = acme.ServiceCode
	dMid.EvidenceIDs = []string{"ev-acme-1"}
	_ = rf.UpsertDraft(context.Background(), dMid)

	approved, xerr := svc.ReviewDraft(context.Background(), org, user, draft.ID, "approve", nil)
	if xerr != nil {
		t.Fatal(xerr.Message)
	}
	if approved.Status != models.OutreachDraftApproved {
		t.Fatalf("status %s", approved.Status)
	}

	// Per-touch approval required before enroll/transport.
	now := time.Now().UTC()
	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: approved.AccountID, Ordinal: 1,
		Channel: models.OutreachChannelEmail, Purpose: models.TouchpointPurposeInitial,
		State: models.TouchpointNeedsReview, Recipient: approved.RecipientEmail,
		Subject: approved.Subject, BodyText: approved.BodyText, DraftID: &approved.ID,
		ContactCandidateID:   approved.ContactCandidateID,
		GeneratedContextHash: acme.MessageContextHash, IdempotencyKey: "e2e-tp",
	}
	RecomputeContentHash(tp)
	if err := ApplyHumanApproval(tp, user, now); err != nil {
		t.Fatal(err)
	}
	_ = rf.InsertTouchpoint(context.Background(), tp)

	enrolled, xerr := svc.EnrollDraft(context.Background(), org, user, draft.ID)
	if xerr != nil {
		t.Fatal(xerr.Message)
	}
	if enrolled.Status != models.OutreachDraftEnrolled {
		t.Fatalf("enroll status %s", enrolled.Status)
	}
	if len(contacts.added) != 1 {
		t.Fatal("expected contact promotion")
	}

	run2, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if run2.Counts.Creates != 0 {
		t.Fatalf("reimport creates should be 0: %+v", run2.Counts)
	}

	acc, _ := rf.GetAccountByCNPJ(context.Background(), org, "11222333000181")
	acc.DoNotContact = true
	acc.QueueState = models.OutreachQueueDoNotContact
	_, _ = rf.UpsertAccount(context.Background(), acc)
	_, _ = svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{})
	again, _ := rf.GetAccountByCNPJ(context.Background(), org, "11222333000181")
	if again == nil || !again.DoNotContact {
		t.Fatal("DNC not preserved")
	}

	email := contacts.added[0].Email
	if err := svc.NoteBounce(context.Background(), org, email, "550 user unknown"); err != nil {
		t.Fatal(err)
	}

	secret := "whsec_test"
	payload := []byte(`{"event_type":"REPLIED"}`)
	ts := time.Now().UTC()
	hdr := SignOutcomeHMAC(secret, ts, payload)
	if !VerifyOutcomeHMAC(secret, hdr, payload, ts, 5*time.Minute) {
		t.Fatal("hmac roundtrip")
	}
	if !strings.Contains(hdr, "v1=") {
		t.Fatalf("header shape %q", hdr)
	}
}

type memRepoOutcome struct {
	memRepoFull
	outcomes *[]models.OutreachOutcome
}

func (m *memRepoOutcome) EnqueueOutcome(ctx context.Context, ev *models.OutreachOutcome) error {
	if m.outcomes != nil {
		*m.outcomes = append(*m.outcomes, *ev)
	}
	return nil
}

func (m *memRepoOutcome) FindCandidateByEmail(ctx context.Context, orgID uuid.UUID, email string) (*models.OutreachContactCandidate, *models.OutreachAccount, error) {
	for _, list := range m.cands {
		for i := range list {
			if list[i].OrganizationID == orgID && strings.EqualFold(list[i].Email, email) {
				acc, _ := m.GetAccount(ctx, orgID, list[i].AccountID)
				cp := list[i]
				return &cp, acc, nil
			}
		}
	}
	return nil, nil, nil
}
