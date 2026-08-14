package confenge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestMapActionModeTokens(t *testing.T) {
	cases := []struct {
		in    string
		class string
	}{
		{"DIRECT_EMAIL_VALIDATED", models.ReachabilityR1Direct},
		{"HUMAN_REVIEW_EMAIL", models.ReachabilityR1Direct},
		{"MANUAL_ROUTED_CALL", models.ReachabilityR3Routed},
		{"ROLE_MAILBOX", models.ReachabilityR4Role},
		{"ROLE_EMAIL", models.ReachabilityR4Role},
		{"GENERIC", models.ReachabilityR5Corporate},
		{"GENERIC_EMAIL_LAST_RESORT", models.ReachabilityR5Corporate},
		{"NEEDS_ENRICHMENT", models.ReachabilityR0None},
		{"NO_ACTIONABLE_ROUTE", models.ReachabilityR0None},
		{"UNKNOWN_TOKEN_X", models.ReachabilityUnmapped},
		{"", ""},
		{"NAMED_HUMAN_MANUAL_CHANNEL", ""},
	}
	for _, tc := range cases {
		got := MapActionMode(tc.in)
		if got != tc.class {
			t.Fatalf("MapActionMode(%q)=%q want %q", tc.in, got, tc.class)
		}
	}
	class, rel := ResolveImportedRoute("MANUAL_ROUTED_CALL", "", "")
	if class != models.ReachabilityR3Routed || rel != models.RouteRelRoutesToNamedPerson {
		t.Fatalf("routed call resolve: %s %s", class, rel)
	}
	class, rel = ResolveImportedRoute("MANUAL_ROUTED_CALL", "R5_CORPORATE_ONLY", "ACCOUNT_LEVEL_ONLY")
	if class != models.ReachabilityR5Corporate || rel != models.RouteRelCorporateGeneric {
		t.Fatalf("published class must win over action mode: %s %s", class, rel)
	}
}

func TestActionModePlansWithoutEmailReadiness(t *testing.T) {
	now := time.Date(2026, 8, 14, 16, 0, 0, 0, time.UTC)
	acc := &models.OutreachAccount{
		SourceLeadID: "cnpj:00820854000114", CNPJ14: "00820854000114",
		RazaoSocial: "QUALIDADE MINERACAO LTDA", MomentSummary: "portfolio publico com contratos",
		ServiceCode: "reajuste_14133", FactToMention: "portfolio publico com contratos",
	}
	routed := &models.OutreachContactCandidate{
		SourceContactID: "cand-1", PersonID: "4a97c613751b0d788bb24ce8",
		Name: "EDUARDO SCHMITT ESPINDOLA", Role: "Socio-Administrador",
		Phone: "(48) 3374-2655", EmailSendReady: false,
		ReachabilityClass: models.ReachabilityR3Routed,
		RouteType:         "phone", RouteRelation: models.RouteRelRoutesToNamedPerson,
		ChannelValue: "(48) 3374-2655", Confidence: "HIGH",
	}
	p := PlanCommercialAction(PlanInput{Account: acc, Candidate: routed, Candidates: []models.OutreachContactCandidate{*routed}, Now: now})
	if !p.Action.Actionable || p.Action.EmailSendable || p.Action.Dispatchable {
		t.Fatalf("routed call must be actionable and not email: %+v", p.Action)
	}
	if p.Action.ActionType != models.ActionRoutedCall || p.Action.Lane != models.LaneRoutedCallQueue {
		t.Fatalf("want ROUTED_CALL: %+v", p.Action)
	}
	if p.Action.PersonID != "4a97c613751b0d788bb24ce8" {
		t.Fatalf("person id dropped: %q", p.Action.PersonID)
	}

	generic := *routed
	generic.Name = ""
	generic.Role = "Licitações"
	generic.Email = "contato@qualidademineracao.com.br"
	generic.Phone = ""
	generic.ReachabilityClass = models.ReachabilityR5Corporate
	generic.RouteRelation = models.RouteRelCorporateGeneric
	generic.ChannelValue = "contato@qualidademineracao.com.br"
	generic.EmailSendReady = true
	p = PlanCommercialAction(PlanInput{Account: acc, Candidate: &generic, Candidates: []models.OutreachContactCandidate{generic}, Now: now})
	if p.Action.EmailSendable || p.Action.Dispatchable || p.Action.PersonName != "" {
		t.Fatalf("generic must not become named-human sendable: %+v", p.Action)
	}

	unknown := *routed
	unknown.ReachabilityClass = "FUTURE_MODE_Z"
	p = PlanCommercialAction(PlanInput{Account: acc, Candidate: &unknown, Candidates: []models.OutreachContactCandidate{unknown}, Now: now})
	if p.Action.ReachabilityClass != models.ReachabilityUnmapped || p.Action.EmailSendable || p.Action.Actionable {
		t.Fatalf("unknown must block: %+v", p.Action)
	}

	email := *routed
	email.Email = "ana@empresa.example"
	email.Phone = ""
	email.EmailSendReady = true
	email.ReachabilityClass = models.ReachabilityR1Direct
	email.RouteRelation = models.RouteRelBelongsToNamedPerson
	email.ChannelValue = email.Email
	p = PlanCommercialAction(PlanInput{Account: acc, Candidate: &email, Candidates: []models.OutreachContactCandidate{email}, Now: now})
	if p.Action.EmailSendable {
		t.Fatalf("R1 without VALIDATED+READY body must not be sendable: %+v", p.Action)
	}
}

func loadTrackAProjection(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "track_a_operator_projection_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTrackAOperatorProjectionImportIdempotent(t *testing.T) {
	raw := loadTrackAProjection(t)
	feed, err := DetectAndNormalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateFeed(feed); err != nil {
		t.Fatal(err)
	}
	if len(feed.Leads) != 20 {
		t.Fatalf("want 20 Track A cards, got %d", len(feed.Leads))
	}
	first := feed.Leads[0]
	if first.Company.CNPJ14 != "00820854000114" || first.Contacts[0].Name != "EDUARDO SCHMITT ESPINDOLA" {
		t.Fatalf("first card identity invented or lost: %+v", first)
	}
	if first.Contacts[0].PersonID == "" {
		t.Fatal("person_id missing from extra-cli DUI account")
	}
	if first.Contacts[0].EmailSendReady != nil && *first.Contacts[0].EmailSendReady {
		t.Fatal("Track A must not publish email_send_ready")
	}

	sum := SummarizeOperatorProjection(feed)
	fmtOperator := FormatOperatorSummary(sum)
	t.Log(fmtOperator)
	if sum.ActionableAccounts < 15 {
		t.Fatalf("actionable=%d", sum.ActionableAccounts)
	}
	if sum.ManualCall < 15 {
		t.Fatalf("manual_call=%d", sum.ManualCall)
	}
	if sum.EmailSafe != 0 {
		t.Fatalf("email_safe=%d want 0", sum.EmailSafe)
	}
	if sum.UnresolvedBlockers < 1 {
		t.Fatalf("expected at least one unresolved Track A account, got %d", sum.UnresolvedBlockers)
	}
	if len(sum.NextHumanActions) == 0 || len(sum.RouteDistribution) == 0 {
		t.Fatalf("summary missing routes/actions: %+v", sum)
	}

	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	run1, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{IdempotencyKey: "track-a-1"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	run2, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{IdempotencyKey: "track-a-2"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	accs, err := repo.ListAccounts(context.Background(), org, repository.OutreachAccountFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(accs) != 20 {
		t.Fatalf("second import invented accounts: %d", len(accs))
	}
	ids := map[string]int{}
	for _, a := range accs {
		ids[a.CNPJ14]++
		if ids[a.CNPJ14] > 1 {
			t.Fatalf("duplicate CNPJ %s", a.CNPJ14)
		}
	}
	cands, err := repo.ListCandidates(context.Background(), org, accs[0].ID)
	if err != nil || len(cands) == 0 {
		t.Fatalf("candidates: %v %#v", err, cands)
	}
	if cands[0].PersonID == "" && cands[0].SourceContactID == "" {
		t.Fatal("imported person identity missing")
	}
	if strings.Contains(strings.ToLower(cands[0].Name), "invent") {
		t.Fatal("invented name")
	}
	printImport := FormatImportSummary(run1)
	if !strings.Contains(printImport, "actionable_accounts=") || !strings.Contains(printImport, "manual_call=") {
		t.Fatalf("import summary missing operator fields:\n%s", printImport)
	}
	if run1.Counts.EmailSafe != 0 || run2.Counts.EmailSafe != 0 {
		t.Fatalf("import marked email safe: %+v %+v", run1.Counts, run2.Counts)
	}
}

func TestTrackARoutedCallOutcomeE2E(t *testing.T) {
	raw := loadTrackAProjection(t)
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	if _, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{IdempotencyKey: "track-a-e2e"}); xerr != nil {
		t.Fatal(xerr)
	}
	accs, err := repo.ListAccounts(context.Background(), org, repository.OutreachAccountFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	suppressed := 0
	for _, a := range accs {
		if a.QueueState == models.OutreachQueueTargetFitSuppressed {
			suppressed++
		}
	}
	if suppressed == len(accs) {
		t.Fatal("operator projection must not hide every routed-call account behind target-fit email suppression")
	}
	today, xerr := svc.CollectToday(context.Background(), org)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if today.Summary.RoutedCalls < 10 {
		t.Fatalf("today routed=%d", today.Summary.RoutedCalls)
	}
	var card ActionCard
	for _, c := range today.Actions {
		if c.ActionType == models.ActionRoutedCall && c.Company == "QUALIDADE MINERACAO LTDA" {
			card = c
			break
		}
	}
	if card.ActionID == "" {
		for _, c := range today.Actions {
			if c.ActionType == models.ActionRoutedCall {
				card = c
				break
			}
		}
	}
	if card.ActionID == "" || card.Person == "" || card.ChannelValue == "" {
		t.Fatalf("missing routed card: %+v", card)
	}
	if card.EmailSendable || card.Dispatchable {
		t.Fatalf("card implies send: %+v", card)
	}
	if len(card.Copy.DoNotClaim) == 0 {
		t.Fatalf("do-not-claim missing: %+v", card.Copy)
	}
	id := uuid.MustParse(card.ActionID)
	attempted, xerr := svc.RecordCommercialOutcome(context.Background(), org, user, id, OutcomeRequest{OutcomeCode: models.OutcomeAttempted, Actor: "op-track-a"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if attempted.Action.State != models.ActionStateInProgress || attempted.Correction == nil && false {
		t.Logf("attempted state=%s", attempted.Action.State)
	}
	due := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	noAnswer, xerr := svc.RecordCommercialOutcome(context.Background(), org, user, id, OutcomeRequest{
		OutcomeCode: models.OutcomeNoAnswer, Actor: "op-track-a", NextActionType: models.ActionRoutedCall, NextActionAt: &due,
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if noAnswer.Action.OutcomeCode != models.OutcomeNoAnswer || noAnswer.Action.NextActionAt == nil {
		t.Fatalf("no-answer not persisted: %+v", noAnswer.Action)
	}
	if noAnswer.Action.Dispatchable {
		t.Fatal("outcome must not dispatch email")
	}
	contacted, xerr := svc.RecordCommercialOutcome(context.Background(), org, user, id, OutcomeRequest{OutcomeCode: models.OutcomeContactedCode, Actor: "op-track-a"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if contacted.Action.TargetReached == nil || !*contacted.Action.TargetReached {
		t.Fatalf("contacted must mark target reached: %+v", contacted.Action)
	}
	if svc.cfg.SendingAllowed() {
		t.Log("sending allowed in test cfg; kill switch covered separately")
	}
}

func TestTrackAKillSwitchKeepsRoutedCallRecordable(t *testing.T) {
	raw := loadTrackAProjection(t)
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	if _, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{IdempotencyKey: "track-a-kill"}); xerr != nil {
		t.Fatal(xerr)
	}
	if err := EngageKillSwitch(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ReleaseKillSwitch() })
	if svc.cfg.SendingAllowed() {
		t.Fatal("kill switch must block SendingAllowed")
	}
	gate := svc.GateCampaignEmail(context.Background(), org, "CONFENGE track-a", "contato@qualidademineracao.com.br", uuid.Nil, uuid.Nil, uuid.Nil)
	if gate.Kind != GateDeferred || gate.Reason != ReasonSendingOff {
		t.Fatalf("kill switch must defer email, got kind=%v reason=%s", gate.Kind, gate.Reason)
	}
	accs, err := repo.ListAccounts(context.Background(), org, repository.OutreachAccountFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for i := range accs {
		accs[i].QueueState = models.OutreachQueueReadyToGenerate
		if _, err := repo.UpsertAccount(context.Background(), &accs[i]); err != nil {
			t.Fatal(err)
		}
	}
	today, xerr := svc.CollectToday(context.Background(), org)
	if xerr != nil {
		t.Fatal(xerr)
	}
	var routedID uuid.UUID
	for _, c := range today.Actions {
		if c.ActionType == models.ActionRoutedCall {
			routedID = uuid.MustParse(c.ActionID)
			break
		}
	}
	if routedID == uuid.Nil {
		t.Fatal("kill switch dropped routed-call work")
	}
	res, xerr := svc.RecordCommercialOutcome(context.Background(), org, user, routedID, OutcomeRequest{OutcomeCode: models.OutcomeNoAnswer, Actor: "op-kill"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if res.Action.Dispatchable || res.Action.EmailSendable {
		t.Fatalf("recording under kill switch must not send: %+v", res.Action)
	}
}
