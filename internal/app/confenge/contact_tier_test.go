package confenge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func loadContactTierFeed(t *testing.T) *Feed {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "contact_tiers_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	feed, err := ParseFeed(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateFeed(feed); err != nil {
		t.Fatal(err)
	}
	return feed
}

func TestContactTiersContractualFixture(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	feed := loadContactTierFeed(t)
	if len(feed.Leads) != 5 {
		t.Fatalf("want 5 contractual leads, got %d", len(feed.Leads))
	}
	rep := ProcessFeedCorpus(feed, 5, now)
	if rep.Selected != 5 {
		t.Fatalf("imported=%d", rep.Selected)
	}
	type row struct {
		Company, RecipientState, ContactTier, ActionLane, MessageabilityState, SendableBody string
	}
	byLead := map[string]row{}
	classes := make([]ContactClass, 0, len(feed.Leads))
	for _, lead := range feed.Leads {
		acc, cands, ev := feedLeadToModels(lead)
		var cand *models.OutreachContactCandidate
		if len(cands) > 0 {
			cand = &cands[0]
		}
		rec := ResolveRecipient(acc, cands, now)
		cls := ClassifyContactTier(acc, cand, now)
		pb := MustPlaybook()
		_, plan := BuildOutboundPlan(pb, acc, cand, ev, 1)
		out := ComposeFromPlan(plan, acc, cand, ChannelEmailInitial)
		lane := ClassifyActionLane(cls, rec, plan, out.BodyText)
		classes = append(classes, cls)
		byLead[lead.SourceLeadID] = row{
			Company: acc.NomeFantasia, RecipientState: rec.State, ContactTier: cls.Tier,
			ActionLane: lane, MessageabilityState: plan.Messageability, SendableBody: out.BodyText,
		}
		if lane == LaneNeedsReviewEmail && (cls.Tier == ContactTierC || cls.Tier == ContactTierD) {
			t.Fatalf("%s role/generic entered NEEDS_REVIEW", lead.SourceLeadID)
		}
	}
	a := byLead["tier-a-named-email"]
	if a.ContactTier != ContactTierA || a.RecipientState != RecipientValidated {
		t.Fatalf("TIER A must be VALIDATED: %+v", a)
	}
	if a.ActionLane != LaneNeedsReviewEmail || strings.TrimSpace(a.SendableBody) == "" {
		t.Fatalf("TIER A + READY must be NEEDS_REVIEW with body: %+v", a)
	}
	b := byLead["tier-b-named-manual"]
	if b.ContactTier != ContactTierB || b.RecipientState == RecipientValidated || b.ActionLane != LaneManualOutreach {
		t.Fatalf("TIER B %+v", b)
	}
	c := byLead["tier-c-role-mailbox"]
	if c.ContactTier != ContactTierC || c.RecipientState == RecipientValidated || c.ActionLane != LaneRoleMailboxException {
		t.Fatalf("TIER C %+v", c)
	}
	d := byLead["tier-d-generic"]
	if d.ContactTier != ContactTierD || d.RecipientState == RecipientValidated || d.ActionLane != LaneLowConfidenceManual {
		t.Fatalf("TIER D %+v", d)
	}
	e := byLead["tier-e-exhausted"]
	if e.ContactTier != ContactTierE || e.ActionLane != LaneBlockedExhausted {
		t.Fatalf("TIER E %+v", e)
	}
	funnel := SummarizeContactFunnel(classes, ContactFunnel{})
	if funnel.Imported != 5 || funnel.TierA != 1 || funnel.TierB != 1 || funnel.TierC != 1 || funnel.TierD != 1 || funnel.BlockedExhausted != 1 {
		t.Fatalf("funnel %+v", funnel)
	}
	rep2 := ProcessFeedCorpus(feed, 5, now)
	if rep2.RecipientValidated != rep.RecipientValidated || rep2.Selected != rep.Selected {
		t.Fatalf("replay drifted")
	}
}

func TestContactTierGenericCannotApproveViaHumanPath(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato 1149/2022 atingiu aniversário de reajuste em 2024")
	acc.OrganizationID = org
	acc.QueueState = models.OutreachQueueReadyToGenerate
	if _, err := repo.UpsertAccount(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	c := validatedCand("Pessoa Histórica", "Contato", "contato@atlasobras.com.br")
	c.OrganizationID, c.AccountID, c.MailboxPurpose = org, acc.ID, "GENERIC_CONTACT"
	if _, err := repo.UpsertCandidate(context.Background(), &c); err != nil {
		t.Fatal(err)
	}
	cls := ClassifyContactTier(acc, &c, time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC))
	if cls.Tier == ContactTierA || cls.EmailValidated {
		t.Fatalf("generic must not be TIER A: %+v", cls)
	}
	list, xerr := svc.PlanAccountCadence(context.Background(), org, user, acc.ID, &c.ID, models.OutreachChannelEmail)
	if xerr != nil || len(list) == 0 {
		t.Fatalf("plan: %v", xerr)
	}
	tp, xerr := svc.GenerateTouchpointDraft(context.Background(), org, user, list[0].ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if tp.State == models.TouchpointNeedsReview {
		t.Fatalf("generic cannot enter NEEDS_REVIEW: %+v", tp)
	}
	if _, xerr := svc.ApproveTouchpoint(context.Background(), org, user, tp.ID, ApprovalOptions{GenericRecipientAcknowledged: true}); xerr == nil {
		t.Fatal("generic acknowledgement must not approve")
	}
}

func TestContactTierRoleMailboxGenerateNeverNeedsReview(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	acc := testAccount("ADITIVOS", "ADITIVO", "termo aditivo 2 ao contrato 88/2021 publicado em julho/2026")
	acc.OrganizationID = org
	acc.QueueState = models.OutreachQueueReadyToGenerate
	if _, err := repo.UpsertAccount(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	c := validatedCand("", "Licitações", "licitacoes@metrovia.com.br")
	c.Name, c.OrganizationID, c.AccountID = "", org, acc.ID
	c.MailboxPurpose = "ROLE_MAILBOX"
	c.RecipientCommercialSuitability = "SUITABLE_GENERIC"
	if _, err := repo.UpsertCandidate(context.Background(), &c); err != nil {
		t.Fatal(err)
	}
	cls := ClassifyContactTier(acc, &c, time.Now().UTC())
	if cls.Tier != ContactTierC || cls.NamedHuman {
		t.Fatalf("want TIER C not named human: %+v", cls)
	}
	list, xerr := svc.PlanAccountCadence(context.Background(), org, user, acc.ID, &c.ID, models.OutreachChannelEmail)
	if xerr != nil || len(list) == 0 {
		t.Fatalf("plan: %v", xerr)
	}
	tp, xerr := svc.GenerateTouchpointDraft(context.Background(), org, user, list[0].ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if tp.State == models.TouchpointNeedsReview {
		t.Fatalf("role mailbox must not enter NEEDS_REVIEW: %+v", tp)
	}
}

func TestContactTierFeedImportIdempotent(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	raw, err := os.ReadFile(filepath.Join("testdata", "contact_tiers_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	run1, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{IdempotencyKey: "tiers-1"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	run2, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{IdempotencyKey: "tiers-2"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if run1.Counts.Creates < 5 {
		t.Fatalf("first import creates=%d", run1.Counts.Creates)
	}
	if run2.Counts.Creates != 0 && run2.Counts.Unchanged+run2.Counts.Updates < 5 {
		t.Fatalf("replay must not invent accounts: first=%+v second=%+v", run1.Counts, run2.Counts)
	}
	list, lerr := repo.ListAccounts(context.Background(), org, repository.OutreachAccountFilter{Limit: 50})
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(list) != 5 {
		t.Fatalf("accounts after replay=%d", len(list))
	}
}

func TestHumanCorrectionReasonCodesIncludeContactLearning(t *testing.T) {
	hc, err := RecordHumanDecision(DecisionReject, "d1", "actor", "old", "old", "s", "s", []string{"wrong_person", "wrong_hook", "channel_preference", "contact_correction"})
	if err != nil {
		t.Fatal(err)
	}
	if hc.Silent {
		t.Fatal("correction must not be silent")
	}
	got := map[string]bool{}
	for _, r := range hc.ReasonCodes {
		got[r] = true
	}
	for _, want := range []string{"wrong_person", "wrong_hook", "channel_preference", "contact_correction"} {
		if !got[want] {
			t.Fatalf("missing reason %s in %+v", want, hc.ReasonCodes)
		}
	}
	d := &models.OutreachDraft{ID: uuid.New(), BodyText: "old"}
	attachHumanCorrection(d, hc)
	var val ValidationResult
	if err := json.Unmarshal(d.ValidationJSON, &val); err != nil || val.HumanCorrection == nil || val.HumanCorrection.Decision != DecisionReject {
		t.Fatalf("persist failed: %+v err=%v", val.HumanCorrection, err)
	}
}

func TestApplyManualActionPersistsWithoutApproving(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	acc := &models.OutreachAccount{OrganizationID: org, CNPJ14: "33222000000182", QueueState: models.OutreachQueueNeedsContact, NomeFantasia: "Pampas"}
	if _, err := repo.UpsertAccount(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	hc, xerr := svc.ApplyManualAction(context.Background(), org, user, acc.ID, ManualMarkContacted, "contact_correction")
	if xerr != nil {
		t.Fatal(xerr)
	}
	if hc.Decision == DecisionApprove {
		t.Fatal("manual action must not approve")
	}
	if _, xerr := svc.ApplyManualAction(context.Background(), org, user, acc.ID, ManualPromoteAfterEvidence, ""); xerr == nil {
		t.Fatal("promote must not silently approve")
	}
}

func TestManualActionNeverApproves(t *testing.T) {
	item := BuildManualItem(
		&models.OutreachAccount{NomeFantasia: "Pampas", ServiceCode: "ADITIVOS", MomentSummary: "Aditivo recente", FactToMention: "termo aditivo 2"},
		&models.OutreachContactCandidate{Name: "Bruno Lima", Role: "Gerente de Contratos", LinkedInURL: "https://linkedin.com/in/x", Confidence: "MEDIUM"},
		RecipientResolution{State: RecipientException, Company: "Pampas", Name: "Bruno Lima", Role: "Gerente de Contratos"},
		ContactClass{Tier: ContactTierB, Lane: LaneManualOutreach, NamedHuman: true, Channel: "linkedin", RecommendedNext: "Abordar no canal publicado.", Warning: "Sem e-mail validado."},
		OutboundMessagePlan{Hook: "termo aditivo 2", ServiceCode: "ADITIVOS"},
	)
	if item.Lane != LaneManualOutreach || item.Person == "" {
		t.Fatalf("manual item %+v", item)
	}
	for _, a := range item.Actions {
		if a == DecisionApprove || a == "APPROVE" || a == "APROVAR" {
			t.Fatalf("manual queue must not offer approve: %+v", item.Actions)
		}
	}
}

func TestTwoValidatedHumansNeverEnterNeedsReview(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org := uuid.New()
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato 1149/2022 atingiu aniversário de reajuste em 2024")
	acc.OrganizationID = org
	acc.QueueState = models.OutreachQueueReadyToGenerate
	acc.NomeFantasia = "Horizonte Sul"
	if _, err := repo.UpsertAccount(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	a := validatedCand("Ana Souza", "Diretora de Contratos", "ana.souza@horizontesul.com.br")
	a.OrganizationID, a.AccountID, a.SourceContactID = org, acc.ID, "ct-ana"
	b := validatedCand("Bruno Lima", "Gerente de Contratos", "bruno.lima@horizontesul.com.br")
	b.OrganizationID, b.AccountID, b.SourceContactID = org, acc.ID, "ct-bruno"
	if _, err := repo.UpsertCandidate(context.Background(), &a); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertCandidate(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	cands := []models.OutreachContactCandidate{a, b}
	rec := ResolveRecipient(acc, cands, now)
	if rec.State != RecipientException {
		t.Fatalf("two validated humans must be EXCEPTION, got %s %v", rec.State, rec.ReasonCodes)
	}
	cls := ClassifyContactTier(acc, &a, now)
	_, plan := BuildOutboundPlan(MustPlaybook(), acc, &a, nil, 1)
	out := ComposeFromPlan(plan, acc, &a, ChannelEmailInitial)
	lane := ClassifyActionLane(cls, rec, plan, out.BodyText)
	if lane == LaneNeedsReviewEmail {
		t.Fatalf("EXCEPTION must never become NEEDS_REVIEW: tier=%s rec=%s lane=%s", cls.Tier, rec.State, lane)
	}
	cockpit, xerr := svc.CollectContactCockpit(context.Background(), org)
	if xerr != nil {
		t.Fatal(xerr)
	}
	for _, item := range cockpit.Ready {
		if item.CanonicalTargetID == acc.ID.String() {
			t.Fatalf("conflict account leaked into NEEDS_REVIEW: %+v", item)
		}
	}
}

func TestCollectContactCockpitLanesAndOutcomeFunnel(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	raw, err := os.ReadFile(filepath.Join("testdata", "contact_tiers_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{IdempotencyKey: "cockpit-lanes"}); xerr != nil {
		t.Fatal(xerr)
	}
	accs, err := repo.ListAccounts(context.Background(), org, repository.OutreachAccountFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(accs) != 5 {
		t.Fatalf("imported=%d", len(accs))
	}
	for i := range accs {
		accs[i].QueueState = models.OutreachQueueReadyToGenerate
		if _, err := repo.UpsertAccount(context.Background(), &accs[i]); err != nil {
			t.Fatal(err)
		}
	}
	sent := &models.OutreachAccount{OrganizationID: org, CNPJ14: "70000000000101", QueueState: models.OutreachQueueSent, NomeFantasia: "Enviada"}
	replied := &models.OutreachAccount{OrganizationID: org, CNPJ14: "70000000000112", QueueState: models.OutreachQueueReplied, NomeFantasia: "Respondeu"}
	meeting := &models.OutreachAccount{OrganizationID: org, CNPJ14: "70000000000123", QueueState: models.OutreachQueueMeeting, NomeFantasia: "Reuniao"}
	for _, acc := range []*models.OutreachAccount{sent, replied, meeting} {
		if _, err := repo.UpsertAccount(context.Background(), acc); err != nil {
			t.Fatal(err)
		}
	}
	cockpit, xerr := svc.CollectContactCockpit(context.Background(), org)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if cockpit.Funnel.Contacted < 1 || cockpit.Funnel.Replied < 1 || cockpit.Funnel.Meeting < 1 {
		t.Fatalf("outcome funnel dead: %+v", cockpit.Funnel)
	}
	byLane := map[string]int{}
	for _, item := range cockpit.Ready {
		byLane[item.Lane]++
		if item.Lane != LaneNeedsReviewEmail {
			t.Fatalf("ready list must only hold NEEDS_REVIEW: %+v", item)
		}
	}
	for _, item := range cockpit.Manual {
		byLane[item.Lane]++
		if item.Lane == LaneNeedsReviewEmail {
			t.Fatalf("manual list must not hold NEEDS_REVIEW: %+v", item)
		}
	}
	if byLane[LaneManualOutreach] < 1 || byLane[LaneRoleMailboxException] < 1 || byLane[LaneLowConfidenceManual] < 1 {
		t.Fatalf("fixture lanes missing: %+v ready=%d manual=%d", byLane, len(cockpit.Ready), len(cockpit.Manual))
	}
}
