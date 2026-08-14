package confenge

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/confenge/dispatch"
	"github.com/warmbly/warmbly/internal/app/whatsapp"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLeadToCandidatePhoneConsent(t *testing.T) {
	src := "website_form"
	at := "2026-01-15T10:00:00Z"
	fc := FeedContact{
		Name:  "Maria",
		Email: "m@example.com",
		Phone: "(48) 99999-1111",
		PhoneObj: &FeedPhone{
			Raw: "(48) 99999-1111", E164: "+5548999991111",
			SourceKind: "official_company_site", SourceURL: "https://ex.com",
		},
		WhatsApp: &FeedWhatsApp{
			ConsentStatus: "OPTED_IN",
			ConsentSource: &src,
			ConsentAt:     &at,
			ProvenanceOK:  true,
		},
		VerificationStatus: models.OutreachVerifyOfficialSource,
	}
	c := leadToCandidate(uuid.New(), uuid.New(), uuid.New(), fc)
	if c.PhoneE164 != "+5548999991111" {
		t.Fatalf("e164=%s", c.PhoneE164)
	}
	if c.WhatsAppConsentStatus != whatsapp.ConsentOptedIn || !c.WhatsAppConsentProvenanceOK {
		t.Fatalf("consent=%s ok=%v", c.WhatsAppConsentStatus, c.WhatsAppConsentProvenanceOK)
	}
	fc2 := FeedContact{
		Phone:    "48999992222",
		WhatsApp: &FeedWhatsApp{ConsentStatus: "OPTED_IN", ProvenanceOK: false},
	}
	c2 := leadToCandidate(uuid.New(), uuid.New(), uuid.New(), fc2)
	if c2.WhatsAppConsentStatus == whatsapp.ConsentOptedIn {
		t.Fatal("must not invent opt-in")
	}
}

// memWAStore persists channel state for send/inbound integration tests.
type memWAStore struct {
	byPhone map[string]*models.WhatsAppContactState
	msgs    int
}

func (m *memWAStore) UpsertContactState(_ context.Context, st *models.WhatsAppContactState) *errx.Error {
	if m.byPhone == nil {
		m.byPhone = map[string]*models.WhatsAppContactState{}
	}
	cp := *st
	m.byPhone[st.PhoneE164] = &cp
	return nil
}
func (m *memWAStore) GetContactStateByPhone(_ context.Context, _ uuid.UUID, phone string) (*models.WhatsAppContactState, *errx.Error) {
	if m.byPhone == nil {
		return nil, nil
	}
	st := m.byPhone[phone]
	if st == nil {
		return nil, nil
	}
	cp := *st
	return &cp, nil
}
func (m *memWAStore) InsertMessage(context.Context, *models.WhatsAppMessage) (bool, *errx.Error) {
	m.msgs++
	return true, nil
}
func (m *memWAStore) GetInstanceByName(context.Context, string, string) (*models.WhatsAppInstance, *errx.Error) {
	return nil, nil
}
func (m *memWAStore) InsertWebhookEvent(context.Context, uuid.UUID, string, string, string, string, string) (bool, *errx.Error) {
	return true, nil
}

func TestDecideChannelGenerateAndSend(t *testing.T) {
	org := uuid.New()
	accID := uuid.New()
	candID := uuid.New()
	user := uuid.New()
	repo := newMemRepo()
	repo.byID[accID] = &models.OutreachAccount{
		ID: accID, OrganizationID: org, SourceLeadID: "L1", CNPJ14: "12345678000199",
		FactToMention: "termo aditivo 1 ao contrato X publicado", ServiceCode: "ADITIVOS",
		EntryOffer: "revisão", QuestionToAsk: "Posso explicar?",
		QueueState: models.OutreachQueueReadyToGenerate,
	}
	markTestAccountTargetFitReady(repo.byID[accID])
	repo.accounts[accKey(org, "12345678000199")] = repo.byID[accID]

	now := time.Now().UTC()
	repo.cands[accID] = []models.OutreachContactCandidate{{
		ID: candID, OrganizationID: org, AccountID: accID,
		Name: "Tiago", Email: "t@ex.com", PhoneE164: "+5548999887766",
		WhatsAppConsentStatus: whatsapp.ConsentUserInitiated, WhatsAppConsentProvenanceOK: true,
		WhatsAppConsentSource: "inbound", WhatsAppConsentAt: &now,
		VerificationStatus: models.OutreachVerifyOfficialSource, Recommended: true,
	}}

	mock := whatsapp.NewMockProvider()
	waSvc := whatsapp.NewService(whatsapp.Config{
		Enabled: true, AutoSendEnabled: false, CrossChannelInterval: 0, ServiceWindow: 24 * time.Hour,
		EvolutionInstance: "test",
	}, mock, nil)
	store := &memWAStore{}
	cfg := Config{Enabled: true, WhatsAppEnabled: true, RequireHumanApproval: true, CrossChannelHours: 0, MaxInitialEmailWords: 120}
	svc := NewService(cfg, repo, nil).(*service)
	svc.WireWhatsApp(waSvc, store)
	dispatchConfig := dispatch.DefaultConfig()
	dispatchConfig.WindowStart, dispatchConfig.WindowEnd, dispatchConfig.Timezone = "00:00", "23:59", "UTC"
	dispatchConfig.BusinessDaysOnly = false
	svc.WireDispatchGovernor(dispatch.NewGovernor(dispatchConfig, dispatch.NewMemoryStore(), &dispatch.FixedClock{T: now}))

	// Case A: public phone
	pubID := uuid.New()
	repo.cands[accID] = append(repo.cands[accID], models.OutreachContactCandidate{
		ID: pubID, OrganizationID: org, AccountID: accID,
		PhoneE164: "+5548111111111", WhatsAppConsentStatus: whatsapp.ConsentUnknown,
		Email: "p@ex.com", VerificationStatus: models.OutreachVerifyOfficialSource,
	})
	dec, xerr := svc.DecideChannel(context.Background(), org, accID, &pubID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if dec.Action != ChannelActionWhatsAppBlocked {
		t.Fatalf("case A: %+v", dec)
	}

	// Generate WhatsApp draft
	draft, xerr := svc.GenerateWhatsAppDraft(context.Background(), org, user, accID, &candID)
	if xerr != nil {
		t.Fatalf("generate: %v", xerr)
	}
	if draft.Channel != models.OutreachChannelWhatsApp || draft.BodyText == "" {
		t.Fatalf("draft=%+v", draft)
	}
	if mock.SendCount() != 0 {
		t.Fatal("generate must not send")
	}

	// Open service window via inbound (persisted in store) then send approved draft.
	_, err := svc.HandleWhatsAppInbound(context.Background(), org, whatsapp.ChannelEvent{
		Channel: whatsapp.ChannelWhatsApp, Provider: whatsapp.ProviderEvolution,
		EventType: whatsapp.EventMessageReceived, ExternalMessageID: "in1",
		ExternalEventID: "in1:MESSAGE_RECEIVED", FromE164: "+5548999887766",
		OccurredAt: now, Content: whatsapp.Content{Type: whatsapp.ContentText, Text: "Oi, pode falar no whats"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := store.GetContactStateByPhone(context.Background(), org, "+5548999887766"); st == nil || st.ServiceWindowUntil == nil {
		t.Fatal("inbound must open service window in store")
	}
	draft.Status = models.OutreachDraftApproved
	_ = repo.UpsertDraft(context.Background(), draft)

	// Draft-only APPROVED must not send.
	if _, xerr := svc.SendApprovedWhatsApp(context.Background(), org, user, draft.ID); xerr == nil {
		t.Fatal("draft-only WhatsApp send must be blocked without touchpoint")
	}

	// Per-touch human approval gate.
	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: accID, Ordinal: 1,
		Channel: models.OutreachChannelWhatsApp, Purpose: models.TouchpointPurposeInitial,
		State: models.TouchpointNeedsReview, Recipient: draft.RecipientPhoneE164,
		BodyText: draft.BodyText, DraftID: &draft.ID, IdempotencyKey: "wa-tp-1",
	}
	RecomputeContentHash(tp)
	if err := ApplyHumanApproval(tp, user, now); err != nil {
		t.Fatal(err)
	}
	_ = repo.InsertTouchpoint(context.Background(), tp)

	sent, xerr := svc.SendApprovedWhatsApp(context.Background(), org, user, draft.ID)
	if xerr != nil {
		t.Fatalf("send: %v", xerr)
	}
	if sent.Status != models.OutreachDraftSent {
		t.Fatalf("status=%s", sent.Status)
	}
	if mock.SendCount() != 1 {
		t.Fatalf("send count=%d", mock.SendCount())
	}
}

func TestHandleWhatsAppInboundOptOutStopsAndOutcomes(t *testing.T) {
	org := uuid.New()
	accID := uuid.New()
	candID := uuid.New()
	repo := newMemRepo()
	repo.byID[accID] = &models.OutreachAccount{
		ID: accID, OrganizationID: org, SourceLeadID: "L2", CNPJ14: "999",
		QueueState: models.OutreachQueueSent,
	}
	repo.accounts[accKey(org, "999")] = repo.byID[accID]
	repo.cands[accID] = []models.OutreachContactCandidate{{
		ID: candID, OrganizationID: org, AccountID: accID,
		Email: "x@ex.com", PhoneE164: "+5548999000000",
		WhatsAppConsentStatus: whatsapp.ConsentOptedIn, WhatsAppConsentProvenanceOK: true,
	}}
	draftID := uuid.New()
	repo.drafts[draftID] = &models.OutreachDraft{
		ID: draftID, OrganizationID: org, AccountID: accID,
		Channel: models.OutreachChannelWhatsApp, Status: models.OutreachDraftSent,
		RecipientPhoneE164: "+5548999000000",
	}
	mock := whatsapp.NewMockProvider()
	waSvc := whatsapp.NewService(whatsapp.Config{Enabled: true, ServiceWindow: 24 * time.Hour}, mock, nil)
	svc := NewService(Config{Enabled: true, WhatsAppEnabled: true}, repo, nil).(*service)
	svc.WireWhatsApp(waSvc, nil)

	res, err := svc.HandleWhatsAppInbound(context.Background(), org, whatsapp.ChannelEvent{
		EventType: whatsapp.EventMessageReceived, ExternalMessageID: "m2",
		ExternalEventID: "m2:MESSAGE_RECEIVED", FromE164: "+5548999000000",
		OccurredAt: time.Now().UTC(),
		Content:    whatsapp.Content{Type: whatsapp.ContentText, Text: "não tenho interesse"},
		Channel:    whatsapp.ChannelWhatsApp, Provider: whatsapp.ProviderEvolution,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.StopSequences || !res.OptOut.Confident {
		t.Fatalf("res=%+v", res)
	}
	// candidate sticky DNC
	c, _ := repo.GetCandidate(context.Background(), org, candID)
	if c == nil || !c.DoNotContact {
		t.Fatal("candidate should be DNC after opt-out")
	}
	if len(repo.outcomes) == 0 {
		t.Fatal("expected outcome enqueued")
	}
	res2, err := svc.HandleWhatsAppInbound(context.Background(), org, whatsapp.ChannelEvent{
		EventType: whatsapp.EventMessageReceived, ExternalMessageID: "m2",
		ExternalEventID: "m2:MESSAGE_RECEIVED", FromE164: "+5548999000000",
		OccurredAt: time.Now().UTC(),
		Content:    whatsapp.Content{Type: whatsapp.ContentText, Text: "não tenho interesse"},
		Channel:    whatsapp.ChannelWhatsApp, Provider: whatsapp.ProviderEvolution,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Duplicate {
		t.Fatal("expected duplicate")
	}
}

func TestSendApprovedWhatsAppRejectsEmailDraft(t *testing.T) {
	org := uuid.New()
	accID := uuid.New()
	user := uuid.New()
	repo := newMemRepo()
	repo.byID[accID] = &models.OutreachAccount{
		ID: accID, OrganizationID: org, SourceLeadID: "L3", CNPJ14: "111",
	}
	repo.accounts[accKey(org, "111")] = repo.byID[accID]
	draftID := uuid.New()
	// EMAIL draft that happens to have a phone — must never send via WhatsApp.
	repo.drafts[draftID] = &models.OutreachDraft{
		ID: draftID, OrganizationID: org, AccountID: accID,
		Channel: models.OutreachChannelEmail, Status: models.OutreachDraftApproved,
		RecipientPhoneE164: "+5548999000111", BodyText: "email body",
	}
	mock := whatsapp.NewMockProvider()
	waSvc := whatsapp.NewService(whatsapp.Config{Enabled: true, ServiceWindow: 24 * time.Hour}, mock, nil)
	svc := NewService(Config{Enabled: true, WhatsAppEnabled: true}, repo, nil).(*service)
	svc.WireWhatsApp(waSvc, nil)

	_, xerr := svc.SendApprovedWhatsApp(context.Background(), org, user, draftID)
	if xerr == nil {
		t.Fatal("EMAIL draft must be rejected even when phone is set")
	}
	if mock.SendCount() != 0 {
		t.Fatalf("provider must not be called, sends=%d", mock.SendCount())
	}
}

var _ Service = (*service)(nil)
