package confenge

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/replyclassify"
	"github.com/warmbly/warmbly/internal/models"
)

func seedHandoffAccount(t *testing.T, r *memRepoFull, org uuid.UUID, cnpj, email string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	accID := uuid.New()
	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: cnpj,
		RazaoSocial: "Empresa " + cnpj[:4], NomeFantasia: "Emp " + cnpj[:4],
		QueueState: models.OutreachQueueEnrolled, SourceLeadID: "lead-" + cnpj[:6],
		ServiceCode: "ADDITIVE_REVIEW", FactToMention: "prorrogacao do contrato",
		CommercialState: "NEW",
	}
	if _, err := r.UpsertAccount(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	candID := uuid.New()
	wc := uuid.New()
	cand := &models.OutreachContactCandidate{
		ID: candID, OrganizationID: org, AccountID: accID,
		Email: email, Name: "Ana Silva", Role: "Diretora",
		VerificationStatus: models.OutreachVerifyOfficialSource,
		Recommended:        true,
		WarmblyContactID:   &wc,
	}
	if _, err := r.UpsertCandidate(context.Background(), cand); err != nil {
		t.Fatal(err)
	}
	return accID, candID
}

func TestProcessInboundHandoffEmailCancelsQueued(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120, RequireHumanApproval: true}, r, nil).(*service)
	crm := &mockCRM{}
	svc.WireCRM(crm)
	org := uuid.New()
	accID, _ := seedHandoffAccount(t, r, org, "11222333000181", "ana@example.com")

	// Queued drafts that must be cancelled.
	for _, st := range []string{models.OutreachDraftNeedsReview, models.OutreachDraftApproved, models.OutreachDraftEnrolled, models.OutreachDraftGenerating} {
		d := &models.OutreachDraft{
			ID: uuid.New(), OrganizationID: org, AccountID: accID,
			Status: st, Subject: "x", BodyText: "y", Channel: models.OutreachChannelEmail,
		}
		_ = r.UpsertDraft(context.Background(), d)
	}
	// Sent draft → REPLIED
	sent := &models.OutreachDraft{
		ID: uuid.New(), OrganizationID: org, AccountID: accID,
		Status: models.OutreachDraftSent, Subject: "sent", BodyText: "body",
	}
	_ = r.UpsertDraft(context.Background(), sent)

	res, xerr := svc.ProcessInboundHandoff(context.Background(), org, InboundHandoff{
		Channel: models.OutreachChannelEmail, ContactEmail: "ana@example.com",
		BodyText:       "Tenho interesse, vamos agendar",
		IdempotencyKey: "test-email-1", ActorID: uuid.New(),
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if res.Duplicate || res.NotConfenge {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.Intent.Intent != IntentPositiveInterest {
		t.Fatalf("intent=%s", res.Intent.Intent)
	}
	if res.CancelledDrafts < 4 {
		t.Fatalf("cancelled=%d want >=4", res.CancelledDrafts)
	}
	acc, _ := r.GetAccount(context.Background(), org, accID)
	if acc.QueueState != models.OutreachQueueReplied {
		t.Fatalf("queue=%s", acc.QueueState)
	}
	if acc.CommercialState != IntentPositiveInterest {
		t.Fatalf("commercial=%s", acc.CommercialState)
	}
	// Sent → REPLIED
	d, _ := r.GetDraft(context.Background(), org, sent.ID)
	if d.Status != models.OutreachDraftReplied {
		t.Fatalf("sent draft status=%s", d.Status)
	}
	// CRM deal in Respondeu path (positive)
	if crm.deals < 1 {
		t.Fatal("expected deal opened at Respondeu, never Ganho")
	}
}

func TestProcessInboundHandoffCancelsOpenTouchpoints(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120, RequireHumanApproval: true}, r, nil).(*service)
	org := uuid.New()
	accID, _ := seedHandoffAccount(t, r, org, "33444555000103", "carla@example.com")

	// Planned + queued future touches must be cancelled by handoff itself.
	for i, st := range []string{models.TouchpointPlanned, models.TouchpointQueued, models.TouchpointApproved} {
		tp := &models.OutreachTouchpoint{
			ID: uuid.New(), OrganizationID: org, AccountID: accID, Ordinal: i + 2,
			Channel: models.OutreachChannelEmail, Purpose: models.TouchpointPurposeFollowUp,
			State: st, Recipient: "carla@example.com",
			DueAt: time.Now().UTC().Add(48 * time.Hour), IdempotencyKey: fmt.Sprintf("handoff-tp-%d", i),
		}
		RecomputeContentHash(tp)
		if err := r.InsertTouchpoint(context.Background(), tp); err != nil {
			t.Fatal(err)
		}
	}

	res, xerr := svc.ProcessInboundHandoff(context.Background(), org, InboundHandoff{
		Channel: models.OutreachChannelEmail, ContactEmail: "carla@example.com",
		BodyText: "Obrigado, me ligue na semana que vem", IdempotencyKey: "test-tp-cancel-1",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if !res.StoppedCadence {
		t.Fatal("expected StoppedCadence")
	}
	open, err := r.ListTouchpoints(context.Background(), org, accID, "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, tp := range open {
		if models.TouchpointOpenStates[tp.State] {
			t.Fatalf("open touchpoint still %s after handoff", tp.State)
		}
		if tp.State != models.TouchpointReplied && tp.StopReason != "REPLY" {
			// terminal replied path
			if tp.State != models.TouchpointReplied {
				t.Fatalf("touchpoint state=%s stop=%s", tp.State, tp.StopReason)
			}
		}
	}
	acc, _ := r.GetAccount(context.Background(), org, accID)
	if acc.QueueState != models.OutreachQueueReplied {
		t.Fatalf("queue=%s want REPLIED for Needs attention", acc.QueueState)
	}
}

func TestProcessInboundHandoffIdempotent(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120}, r, nil).(*service)
	org := uuid.New()
	seedHandoffAccount(t, r, org, "22333444000192", "bob@example.com")

	in := InboundHandoff{
		Channel: models.OutreachChannelEmail, ContactEmail: "bob@example.com",
		BodyText: "ok", IdempotencyKey: "dup-key-1",
	}
	r1, xerr := svc.ProcessInboundHandoff(context.Background(), org, in)
	if xerr != nil {
		t.Fatal(xerr)
	}
	r2, xerr := svc.ProcessInboundHandoff(context.Background(), org, in)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if r1.Duplicate {
		t.Fatal("first should not be duplicate")
	}
	if !r2.Duplicate {
		t.Fatal("second must be duplicate")
	}
	if len(r.outcomes) != 1 {
		t.Fatalf("outcomes=%d want 1", len(r.outcomes))
	}
}

func TestProcessInboundHandoffWhatsApp(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120}, r, nil).(*service)
	org := uuid.New()
	accID := uuid.New()
	_, _ = r.UpsertAccount(context.Background(), &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "33444555000103",
		RazaoSocial: "WA Co", QueueState: models.OutreachQueueSent, SourceLeadID: "W1",
	})
	_, _ = r.UpsertCandidate(context.Background(), &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: accID,
		Email: "wa@example.com", PhoneE164: "+5511999990001",
		VerificationStatus: models.OutreachVerifyOfficialSource,
	})
	res, xerr := svc.ProcessInboundHandoff(context.Background(), org, InboundHandoff{
		Channel: models.OutreachChannelWhatsApp, ContactPhone: "+5511999990001",
		BodyText: "quanto custa o servico?", IdempotencyKey: "wa-1",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if res.NotConfenge {
		t.Fatal("should resolve by phone")
	}
	if res.Intent.Intent != IntentQuestion {
		t.Fatalf("intent=%s", res.Intent.Intent)
	}
}

func TestProcessInboundHandoffDNCSticky(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120}, r, nil).(*service)
	org := uuid.New()
	accID, _ := seedHandoffAccount(t, r, org, "44555666000114", "dnc@example.com")

	_, xerr := svc.ProcessInboundHandoff(context.Background(), org, InboundHandoff{
		Channel: models.OutreachChannelEmail, ContactEmail: "dnc@example.com",
		BodyText: "unsubscribe please", IdempotencyKey: "dnc-1",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	acc, _ := r.GetAccount(context.Background(), org, accID)
	if !acc.DoNotContact || acc.QueueState != models.OutreachQueueDoNotContact {
		t.Fatalf("DNC not set: %+v", acc)
	}
	// Later positive reply must not clear sticky DNC.
	_, xerr = svc.ProcessInboundHandoff(context.Background(), org, InboundHandoff{
		Channel: models.OutreachChannelEmail, ContactEmail: "dnc@example.com",
		BodyText: "tenho interesse vamos agendar", IdempotencyKey: "dnc-2",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	acc, _ = r.GetAccount(context.Background(), org, accID)
	if !acc.DoNotContact || acc.QueueState != models.OutreachQueueDoNotContact {
		t.Fatalf("sticky DNC broken: %+v", acc)
	}
}

func TestProcessInboundHandoffNeverAutoWon(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120}, r, nil).(*service)
	crm := &mockCRM{}
	svc.WireCRM(crm)
	org := uuid.New()
	seedHandoffAccount(t, r, org, "55666777000125", "won@example.com")

	for _, cls := range []string{"won", "ganho", replyclassify.ClassPositive} {
		_, xerr := svc.ProcessInboundHandoff(context.Background(), org, InboundHandoff{
			Channel: models.OutreachChannelEmail, ContactEmail: "won@example.com",
			PreClass: cls, BodyText: "ganhamos",
			IdempotencyKey: "won-" + cls,
			ActorID:        uuid.New(),
		})
		if xerr != nil {
			t.Fatal(xerr)
		}
	}
	for _, o := range r.outcomes {
		if o.EventType == OutcomeWon {
			t.Fatalf("auto-WON outcome: %+v", o)
		}
	}
	a := ClassifyReplyForCRM("won")
	if a.OutcomeType == OutcomeWon {
		t.Fatal("ClassifyReplyForCRM mapped won→WON")
	}
	a = ClassifyReplyForCRM("ganho")
	if a.OutcomeType == OutcomeWon {
		t.Fatal("ClassifyReplyForCRM mapped ganho→WON")
	}
}

func TestGenerateReplyDraftAIOffTemplate(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120, RequireHumanApproval: true}, r, nil).(*service)
	// ai nil → template
	org := uuid.New()
	accID, candID := seedHandoffAccount(t, r, org, "66777888000136", "tpl@example.com")
	acc, _ := r.GetAccount(context.Background(), org, accID)
	acc.QueueState = models.OutreachQueueReplied
	acc.CommercialState = IntentQuestion
	_, _ = r.UpsertAccount(context.Background(), acc)

	d, xerr := svc.GenerateReplyDraft(context.Background(), org, uuid.New(), accID, &candID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if d.Status != models.OutreachDraftNeedsReview {
		t.Fatalf("status=%s", d.Status)
	}
	if d.Provider != "template" {
		t.Fatalf("provider=%s", d.Provider)
	}
	if !containsFlag(d.RiskFlags, "never_auto_send") {
		t.Fatalf("flags=%v", d.RiskFlags)
	}
	if !containsFlag(d.RiskFlags, "reply_draft") {
		t.Fatalf("flags=%v", d.RiskFlags)
	}
}

func TestChangeReferralAndResumeAtDate(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120}, r, nil).(*service)
	org := uuid.New()
	accID, _ := seedHandoffAccount(t, r, org, "77888999000147", "ref@example.com")

	cand, xerr := svc.ChangeReferralRecipient(context.Background(), org, uuid.New(), accID, "Maria Souza", "maria@example.com", "CFO", "")
	if xerr != nil {
		t.Fatal(xerr)
	}
	if cand.Email != "maria@example.com" || !cand.Recommended {
		t.Fatalf("referral cand: %+v", cand)
	}
	acc, _ := r.GetAccount(context.Background(), org, accID)
	if acc.QueueState != models.OutreachQueueNeedsContact {
		t.Fatalf("queue after referral=%s", acc.QueueState)
	}
	if acc.CommercialState != IntentReferral {
		t.Fatalf("commercial=%s", acc.CommercialState)
	}

	future := time.Now().UTC().Add(10 * 24 * time.Hour)
	acc2, xerr := svc.ResumeAtDate(context.Background(), org, uuid.New(), accID, future, "proximo trimestre")
	if xerr != nil {
		t.Fatal(xerr)
	}
	if acc2.CommercialState != IntentNotNow {
		t.Fatalf("commercial=%s", acc2.CommercialState)
	}
	if !NeverAutoReopenCadence() {
		t.Fatal("policy: never auto-reopen cadence")
	}
	// DNC blocks resume
	accID2, _ := seedHandoffAccount(t, r, org, "88999000000158", "nodate@example.com")
	_ = r.SetAccountHumanFlags(context.Background(), org, accID2, true, true, "dnc", models.OutreachQueueDoNotContact)
	if _, xerr := svc.ResumeAtDate(context.Background(), org, uuid.New(), accID2, future, ""); xerr == nil {
		t.Fatal("resume on DNC must fail")
	}
}

func TestListAttentionCockpitFilters(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120}, r, nil).(*service)
	org := uuid.New()
	accID, _ := seedHandoffAccount(t, r, org, "99000111000169", "att@example.com")
	acc, _ := r.GetAccount(context.Background(), org, accID)
	acc.QueueState = models.OutreachQueueReplied
	acc.CommercialState = IntentPositiveInterest
	_, _ = r.UpsertAccount(context.Background(), acc)

	list, xerr := svc.ListAttention(context.Background(), org, FilterNeedsAttention, 20)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if len(list) < 1 {
		t.Fatal("expected attention items")
	}
	found := false
	for _, it := range list {
		if it.AccountID == accID {
			found = true
			if it.SuggestedAction == "" {
				t.Fatal("missing suggested action")
			}
			if !strings.Contains(strings.ToLower(it.CompanyName), "emp") {
				t.Fatalf("company=%s", it.CompanyName)
			}
		}
	}
	if !found {
		t.Fatal("account not in attention list")
	}
	item, xerr := svc.GetAttention(context.Background(), org, accID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if item.AccountID != accID {
		t.Fatal("get attention mismatch")
	}
}

func TestOOOWithoutDateDoesNotInventResume(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120}, r, nil).(*service)
	org := uuid.New()
	accID, _ := seedHandoffAccount(t, r, org, "10111213000170", "ooo@example.com")
	res, xerr := svc.ProcessInboundHandoff(context.Background(), org, InboundHandoff{
		Channel: models.OutreachChannelEmail, ContactEmail: "ooo@example.com",
		Subject: "Out of Office", BodyText: "estou de ferias, volto em breve",
		IdempotencyKey: "ooo-1",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if res.Intent.Intent != IntentOutOfOffice {
		t.Fatalf("intent=%s", res.Intent.Intent)
	}
	if res.Intent.OOOReturnDate != nil {
		t.Fatalf("must not invent OOO date: %v", res.Intent.OOOReturnDate)
	}
	if strings.Contains(res.Intent.SuggestedAction, "2026") {
		t.Fatalf("suggested action invented date: %s", res.Intent.SuggestedAction)
	}
	_ = accID
}

func TestApplyReplyCRMCreatesTaskWithoutActor(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120}, r, nil).(*service)
	crm := &mockCRM{}
	svc.WireCRM(crm)
	org := uuid.New()
	// ensure owner exists for Nil actor path
	owner, _ := r.GetOrgOwnerUserID(context.Background(), org)
	if owner == uuid.Nil {
		t.Fatal("owner")
	}
	accID := uuid.New()
	cnpj := fmt.Sprintf("%014d", int(accID[0])<<24|int(accID[1])<<16|int(accID[2])<<8|int(accID[3]))
	cnpj = (cnpj + "00000000000000")[:14]
	_, _ = r.UpsertAccount(context.Background(), &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: cnpj, RazaoSocial: "ACME", QueueState: models.OutreachQueueSent, SourceLeadID: "L1",
	})
	contactID := uuid.New()
	_, _ = r.UpsertCandidate(context.Background(), &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: accID, Email: "ana@acme.com",
		VerificationStatus: models.OutreachVerifyOfficialSource, Recommended: true, WarmblyContactID: &contactID,
	})
	// Production path: ActorID is Nil
	_, xerr := svc.ProcessInboundHandoff(context.Background(), org, InboundHandoff{
		Channel: models.OutreachChannelEmail, ContactEmail: "ana@acme.com",
		BodyText: "Tenho interesse, vamos agendar", ActorID: uuid.Nil, IdempotencyKey: "nil-actor-1",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if crm.tasks < 1 {
		t.Fatal("expected CRM task even when ActorID is Nil (org owner fallback)")
	}
}

func TestEmailBodyDrivesCommercialLexiconDNC(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120}, r, nil).(*service)
	org := uuid.New()
	accID := uuid.New()
	cnpj := fmt.Sprintf("%014d", int(accID[0])<<24|int(accID[1])<<16|int(accID[2])<<8|int(accID[3]))
	cnpj = (cnpj + "00000000000000")[:14]
	_, _ = r.UpsertAccount(context.Background(), &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: cnpj, RazaoSocial: "X", QueueState: models.OutreachQueueSent, SourceLeadID: "Ldnc",
	})
	_, _ = r.UpsertCandidate(context.Background(), &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: accID, Email: "dncbody@acme.com",
		VerificationStatus: models.OutreachVerifyOfficialSource, Recommended: true,
	})
	// PreClass unknown, but body has DNC — lexicon must win
	res, xerr := svc.ProcessInboundHandoff(context.Background(), org, InboundHandoff{
		Channel: models.OutreachChannelEmail, ContactEmail: "dncbody@acme.com",
		Subject: "Re: convite", BodyText: "Please remove me and do not contact again",
		PreClass: replyclassify.ClassUnknown, IdempotencyKey: "body-dnc-1",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if res.Intent.Intent != IntentDoNotContact {
		t.Fatalf("intent=%s want DNC from body", res.Intent.Intent)
	}
}

func TestResumeAtDateCreatesApprovalDraft(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120}, r, nil).(*service)
	org, user := uuid.New(), uuid.New()
	accID := uuid.New()
	cnpj := fmt.Sprintf("%014d", int(accID[0])<<24|int(accID[1])<<16|int(accID[2])<<8|int(accID[3]))
	cnpj = (cnpj + "00000000000000")[:14]
	_, _ = r.UpsertAccount(context.Background(), &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: cnpj, RazaoSocial: "Y", QueueState: models.OutreachQueueReplied, SourceLeadID: "Lr",
	})
	_, _ = r.UpsertCandidate(context.Background(), &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: accID, Email: "r@acme.com",
		VerificationStatus: models.OutreachVerifyOfficialSource, Recommended: true,
	})
	future := time.Now().UTC().Add(96 * time.Hour)
	acc, xerr := svc.ResumeAtDate(context.Background(), org, user, accID, future, "depois das ferias")
	if xerr != nil {
		t.Fatal(xerr)
	}
	if acc.QueueState != models.OutreachQueueNeedsReview {
		t.Fatalf("queue=%s want NEEDS_REVIEW", acc.QueueState)
	}
	drafts, _ := r.ListDrafts(context.Background(), org, models.OutreachDraftNeedsReview, 20, 0)
	found := false
	for _, d := range drafts {
		if d.AccountID == accID && strings.HasPrefix(d.StrategyCode, "RESUME_AT:") {
			found = true
			if !containsFlag(d.RiskFlags, "never_auto_send") || !containsFlag(d.RiskFlags, "requires_human_approval") {
				t.Fatalf("flags=%v", d.RiskFlags)
			}
		}
	}
	if !found {
		t.Fatal("expected explicit resume draft subject to approval")
	}
}

func TestGenerateReplyDraftSurfacesAwaitingApproval(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120}, r, nil).(*service)
	org, user := uuid.New(), uuid.New()
	accID := uuid.New()
	cnpj := fmt.Sprintf("%014d", int(accID[0])<<24|int(accID[1])<<16|int(accID[2])<<8|int(accID[3]))
	cnpj = (cnpj + "00000000000000")[:14]
	_, _ = r.UpsertAccount(context.Background(), &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: cnpj, RazaoSocial: "Z", QueueState: models.OutreachQueueReplied,
		CommercialState: IntentQuestion, SourceLeadID: "Lq",
	})
	_, _ = r.UpsertCandidate(context.Background(), &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: accID, Email: "q@acme.com", Name: "Q",
		VerificationStatus: models.OutreachVerifyOfficialSource, Recommended: true,
	})
	// Seed handoff outcome for confidence/snippet
	_ = r.EnqueueOutcome(context.Background(), &models.OutreachOutcome{
		OrganizationID: org, CNPJ14: cnpj, SourceLeadID: "Lq", ContactEmail: "q@acme.com",
		EventType: OutcomeReplied, OccurredAt: time.Now().UTC(), IdempotencyKey: "seed-out-1",
		Payload: []byte(`{"channel":"EMAIL","intent":"QUESTION","confidence":0.7,"snippet":"Quanto custa?","subject":"Re: proposta"}`),
	})
	d, xerr := svc.GenerateReplyDraft(context.Background(), org, user, accID, nil)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if d.Status != models.OutreachDraftNeedsReview {
		t.Fatal(d.Status)
	}
	acc, _ := r.GetAccount(context.Background(), org, accID)
	if acc.QueueState != models.OutreachQueueNeedsReview {
		t.Fatalf("queue=%s want NEEDS_REVIEW for awaiting approval filter", acc.QueueState)
	}
	items, xerr := svc.ListAttention(context.Background(), org, FilterAwaitingApproval, 20)
	if xerr != nil {
		t.Fatal(xerr)
	}
	found := false
	for _, it := range items {
		if it.AccountID == accID {
			found = true
			if it.ReplyDraftID == nil {
				t.Fatal("reply draft id")
			}
		}
	}
	if !found {
		t.Fatal("awaiting approval should list reply draft accounts")
	}
	detail, xerr := svc.GetAttention(context.Background(), org, accID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if detail.Confidence < 0.5 {
		t.Fatalf("confidence=%v", detail.Confidence)
	}
	if detail.LastSnippet == "" && detail.Thread == "" {
		t.Fatal("thread/snippet required")
	}
}

// Without WireCRM (historical consumer posture), handoff must not panic but
// also cannot create CRM tasks — documents why consumer main must WireCRM.
func TestHandoffWithoutWireCRMCreatesNoTasks(t *testing.T) {
	r := newMemRepoWithSettings()
	svc := NewService(Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120}, r, nil).(*service)
	// deliberately do NOT WireCRM
	crm := &mockCRM{}
	org := uuid.New()
	accID := uuid.New()
	cnpj := fmt.Sprintf("%014d", int(accID[0])<<24|int(accID[1])<<16|int(accID[2])<<8|int(accID[3]))
	cnpj = (cnpj + "00000000000000")[:14]
	_, _ = r.UpsertAccount(context.Background(), &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: cnpj, RazaoSocial: "NoCRM", QueueState: models.OutreachQueueSent, SourceLeadID: "Lnc",
	})
	contactID := uuid.New()
	_, _ = r.UpsertCandidate(context.Background(), &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: accID, Email: "nocrm@acme.com",
		VerificationStatus: models.OutreachVerifyOfficialSource, Recommended: true, WarmblyContactID: &contactID,
	})
	_, xerr := svc.ProcessInboundHandoff(context.Background(), org, InboundHandoff{
		Channel: models.OutreachChannelEmail, ContactEmail: "nocrm@acme.com",
		BodyText: "Tenho interesse", ActorID: uuid.New(), IdempotencyKey: "no-crm-1",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if crm.tasks != 0 {
		t.Fatalf("without WireCRM expected 0 tasks, got %d", crm.tasks)
	}
	// Now wire and confirm tasks appear (real consumer posture after fix).
	svc.WireCRM(crm)
	_, xerr = svc.ProcessInboundHandoff(context.Background(), org, InboundHandoff{
		Channel: models.OutreachChannelEmail, ContactEmail: "nocrm@acme.com",
		BodyText: "Tenho interesse, vamos falar", ActorID: uuid.New(), IdempotencyKey: "with-crm-1",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if crm.tasks < 1 {
		t.Fatal("after WireCRM expected CRM task on positive interest")
	}
}
