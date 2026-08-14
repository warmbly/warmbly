package confenge

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type mockCampaigns struct {
	created *models.Campaign
	gets    map[string]*models.Campaign
}

func (m *mockCampaigns) Create(ctx context.Context, userID string, orgID *uuid.UUID, data *models.CreateCampaign) (*models.Campaign, *errx.Error) {
	if data.Name != DefaultCampaignName {
		return nil, errx.New(errx.BadRequest, "unexpected name")
	}
	if data.StopOnReply == nil || !*data.StopOnReply {
		return nil, errx.New(errx.BadRequest, "stop_on_reply required")
	}
	if data.Timezone == nil || *data.Timezone != "America/Sao_Paulo" {
		return nil, errx.New(errx.BadRequest, "timezone")
	}
	if len(data.Sequences) != 1 {
		return nil, errx.New(errx.BadRequest, "want 1 campaign shell step")
	}
	if data.Sequences[0].WaitAfter != nil {
		return nil, errx.New(errx.BadRequest, "no wait_after")
	}
	c := &models.Campaign{ID: uuid.New(), Name: data.Name, Status: "draft"}
	m.created = c
	if m.gets == nil {
		m.gets = map[string]*models.Campaign{}
	}
	m.gets[c.ID.String()] = c
	return c, nil
}

func (m *mockCampaigns) Get(ctx context.Context, orgID, id string) (*models.Campaign, *errx.Error) {
	if c := m.gets[id]; c != nil {
		return c, nil
	}
	return nil, errx.New(errx.NotFound, "missing")
}

type mockContacts struct {
	added []models.AddContact
}

func (m *mockContacts) Add(ctx context.Context, userID string, orgID uuid.UUID, contacts []models.AddContact) ([]models.Contact, *errx.Error) {
	m.added = append(m.added, contacts...)
	out := make([]models.Contact, len(contacts))
	for i, c := range contacts {
		out[i] = models.Contact{ID: uuid.New(), Email: c.Email, FirstName: c.FirstName, Company: c.Company}
	}
	return out, nil
}

func TestBootstrapCampaignIdempotent(t *testing.T) {
	repo := newMemRepo()
	// enrich memRepo for org settings
	settings := map[uuid.UUID]*models.OutreachOrgSettings{}
	// override via embedding won't work — use real methods if we stored on memRepo
	// Patch: use service with mock campaigns that tracks creates
	camps := &mockCampaigns{}
	svc := testSvc(repo).(*service)
	svc.WireExecution(camps, &mockContacts{})
	// need GetOrgSettings/Upsert on memRepo - already stub returns nil
	// First create stores settings - but memRepo UpsertOrgSettings is no-op and Get returns nil
	// so every bootstrap creates again. Fix memRepo to store settings.
	org := uuid.New()
	user := uuid.New()

	// Use a settings-aware mem repo extension
	r2 := newMemRepoWithSettings()
	svc2 := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120, RequireHumanApproval: true}, r2, nil).(*service)
	camps2 := &mockCampaigns{}
	svc2.WireExecution(camps2, &mockContacts{})

	c1, xerr := svc2.BootstrapCampaign(context.Background(), org, user)
	if xerr != nil {
		t.Fatal(xerr)
	}
	c2, xerr := svc2.BootstrapCampaign(context.Background(), org, user)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if c1.ID != c2.ID {
		t.Fatalf("bootstrap not idempotent: %s vs %s", c1.ID, c2.ID)
	}
	if camps2.created == nil || camps2.created.ID != c1.ID {
		t.Fatal("expected single create")
	}
	_ = settings
}

func TestEnrollRejectsUnapproved(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120, RequireHumanApproval: true}, r, nil).(*service)
	svc.WireExecution(&mockCampaigns{}, &mockContacts{})
	org := uuid.New()
	// seed account + candidate + draft NEEDS_REVIEW
	accID := uuid.New()
	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "11222333000181",
		RazaoSocial: "ACME", QueueState: models.OutreachQueueReadyToGenerate,
		FactToMention: "fato", ServiceCode: "S",
	}
	_, _ = r.UpsertAccount(context.Background(), acc)
	candID := uuid.New()
	cand := &models.OutreachContactCandidate{
		ID: candID, OrganizationID: org, AccountID: accID,
		Email: "a@example.com", VerificationStatus: models.OutreachVerifyOfficialSource, Recommended: true,
	}
	_, _ = r.UpsertCandidate(context.Background(), cand)
	d := &models.OutreachDraft{
		ID: uuid.New(), OrganizationID: org, AccountID: accID, ContactCandidateID: &candID,
		Subject: "Oi", BodyText: "corpo com fato publico ok", FactUsed: "fato",
		ServiceCode: "S", Status: models.OutreachDraftNeedsReview, RiskClass: "GREEN",
		RecipientEmail: "a@example.com", VerificationStatus: models.OutreachVerifyOfficialSource,
	}
	ok := true
	d.ValidationOK = &ok
	_ = r.UpsertDraft(context.Background(), d)

	_, xerr := svc.EnrollDraft(context.Background(), org, uuid.New(), d.ID)
	if xerr == nil {
		t.Fatal("must reject non-approved")
	}
}

func TestEnrollApprovedHappyPath(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120, RequireHumanApproval: true}, r, nil).(*service)
	contacts := &mockContacts{}
	svc.WireExecution(&mockCampaigns{}, contacts)
	org := uuid.New()
	user := uuid.New()
	accID := uuid.New()
	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "11222333000181",
		RazaoSocial: "ACME", NomeFantasia: "ACME", QueueState: models.OutreachQueueApproved,
		FactToMention: "prorrogacao do contrato", ServiceCode: "ADDITIVE_REVIEW", SourceLeadID: "L1",
	}
	_, _ = r.UpsertAccount(context.Background(), acc)
	candID := uuid.New()
	cand := &models.OutreachContactCandidate{
		ID: candID, OrganizationID: org, AccountID: accID, Name: "Ana Silva",
		Email: "ana@example.com", VerificationStatus: models.OutreachVerifyOfficialSource, Recommended: true,
	}
	_, _ = r.UpsertCandidate(context.Background(), cand)
	d := &models.OutreachDraft{
		ID: uuid.New(), OrganizationID: org, AccountID: accID, ContactCandidateID: &candID,
		Subject: "Sobre ACME", BodyText: "Ola Ana,\n\nNotei a prorrogacao do contrato. Faz sentido conversarmos?\n\nPosso enviar checklist?",
		FactUsed: "prorrogacao do contrato", ServiceCode: "ADDITIVE_REVIEW",
		Status: models.OutreachDraftApproved, RiskClass: "GREEN",
		RecipientEmail: "ana@example.com", VerificationStatus: models.OutreachVerifyOfficialSource,
	}
	ok := true
	d.ValidationOK = &ok
	_ = r.UpsertDraft(context.Background(), d)

	// Draft-only APPROVED must not enroll (fail-closed without touchpoint).
	if _, xerr := svc.EnrollDraft(context.Background(), org, user, d.ID); xerr == nil {
		t.Fatal("draft-only enroll must be blocked without touchpoint")
	}

	// Linked transport-valid touchpoint allows enroll.
	now := time.Now().UTC()
	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: accID, Ordinal: 1,
		Channel: models.OutreachChannelEmail, Purpose: models.TouchpointPurposeInitial,
		State: models.TouchpointNeedsReview, Recipient: "ana@example.com",
		Subject: d.Subject, BodyText: d.BodyText, DraftID: &d.ID, IdempotencyKey: "enroll-tp",
	}
	RecomputeContentHash(tp)
	if err := ApplyHumanApproval(tp, user, now); err != nil {
		t.Fatal(err)
	}
	// CAS-queue style state used after QueueTouchpoint; transport allows APPROVED or QUEUED.
	_ = r.InsertTouchpoint(context.Background(), tp)

	out, xerr := svc.EnrollDraft(context.Background(), org, user, d.ID)
	if xerr != nil {
		t.Fatal(xerr.Message)
	}
	if out.Status != models.OutreachDraftEnrolled {
		t.Fatalf("status %s", out.Status)
	}
	if out.CampaignID == nil || out.EnrollmentContactID == nil {
		t.Fatal("missing campaign/contact ids")
	}
	if len(contacts.added) != 1 || contacts.added[0].Email != "ana@example.com" {
		t.Fatalf("contact add: %+v", contacts.added)
	}
	// idempotent re-enroll
	out2, xerr := svc.EnrollDraft(context.Background(), org, user, d.ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if out2.ID != out.ID {
		t.Fatal("re-enroll should return same draft")
	}
}

// memRepo with settings + drafts storage
type memRepoFull struct {
	*memRepo
	settings map[uuid.UUID]*models.OutreachOrgSettings
	drafts   map[uuid.UUID]*models.OutreachDraft
}

func newMemRepoWithSettings() *memRepoFull {
	return &memRepoFull{
		memRepo:  newMemRepo(),
		settings: map[uuid.UUID]*models.OutreachOrgSettings{},
		drafts:   map[uuid.UUID]*models.OutreachDraft{},
	}
}

func (m *memRepoFull) GetOrgSettings(ctx context.Context, orgID uuid.UUID) (*models.OutreachOrgSettings, error) {
	s := m.settings[orgID]
	if s == nil {
		return nil, nil
	}
	cp := *s
	return &cp, nil
}

func (m *memRepoFull) UpsertOrgSettings(ctx context.Context, s *models.OutreachOrgSettings) error {
	cp := *s
	m.settings[s.OrganizationID] = &cp
	return nil
}

func (m *memRepoFull) UpsertDraft(ctx context.Context, d *models.OutreachDraft) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	cp := *d
	m.drafts[d.ID] = &cp
	return nil
}

func (m *memRepoFull) GetDraft(ctx context.Context, orgID, id uuid.UUID) (*models.OutreachDraft, error) {
	d := m.drafts[id]
	if d == nil || d.OrganizationID != orgID {
		return nil, nil
	}
	cp := *d
	return &cp, nil
}

func (m *memRepoFull) GetActiveDraftForAccount(ctx context.Context, orgID, accountID uuid.UUID) (*models.OutreachDraft, error) {
	for _, d := range m.drafts {
		if d.OrganizationID == orgID && d.AccountID == accountID {
			switch d.Status {
			case models.OutreachDraftNotGenerated, models.OutreachDraftGenerating, models.OutreachDraftNeedsReview, models.OutreachDraftApproved:
				cp := *d
				return &cp, nil
			}
		}
	}
	return nil, nil
}

func (m *memRepoFull) EnqueueOutcome(ctx context.Context, ev *models.OutreachOutcome) error {
	return m.memRepo.EnqueueOutcome(ctx, ev)
}

func (m *memRepoFull) GetOutcomeByIdempotency(ctx context.Context, orgID uuid.UUID, key string) (*models.OutreachOutcome, error) {
	return m.memRepo.GetOutcomeByIdempotency(ctx, orgID, key)
}

func (m *memRepoFull) ListDrafts(ctx context.Context, orgID uuid.UUID, status string, limit, offset int) ([]models.OutreachDraft, error) {
	var out []models.OutreachDraft
	for _, d := range m.drafts {
		if d.OrganizationID == orgID && (status == "" || d.Status == status) {
			out = append(out, *d)
		}
	}
	if offset > 0 {
		if offset >= len(out) {
			return nil, nil
		}
		out = out[offset:]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memRepoFull) FindCandidateByEmail(ctx context.Context, orgID uuid.UUID, email string) (*models.OutreachContactCandidate, *models.OutreachAccount, error) {
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
