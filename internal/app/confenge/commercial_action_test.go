package confenge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func loadReachabilityFeed(t *testing.T) *Feed {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "reachability_r0_r5_v1.json"))
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

func planLead(t *testing.T, lead FeedLead) PlannedAction {
	t.Helper()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	acc, cands, ev := feedLeadToModels(lead)
	var cand *models.OutreachContactCandidate
	if len(cands) > 0 {
		cand = &cands[0]
	}
	return PlanCommercialAction(PlanInput{
		Account: acc, Candidate: cand, Candidates: cands, Evidence: ev,
		Now: now, Snapshot: "reachability-r0-r5-v1",
	})
}

func TestReachabilityContractualFixtures(t *testing.T) {
	feed := loadReachabilityFeed(t)
	if len(feed.Leads) != 7 {
		t.Fatalf("want 7 contractual leads, got %d", len(feed.Leads))
	}
	byLead := map[string]PlannedAction{}
	for _, lead := range feed.Leads {
		p := planLead(t, lead)
		byLead[lead.SourceLeadID] = p
		fmt.Printf("MAP %s type=%s lane=%s class=%s actionable=%v email_sendable=%v dispatchable=%v no_action=%v rec=%s person=%q route=%s\n",
			lead.SourceLeadID, p.Action.ActionType, p.Action.Lane, p.Action.ReachabilityClass,
			p.Action.Actionable, p.Action.EmailSendable, p.Action.Dispatchable, p.NoAction,
			p.RecipientState, p.Action.PersonName, p.Action.RouteRelation)
	}

	r1 := byLead["r1-direct-email"]
	if r1.Action.Lane != models.LaneEmailNeedsReview || r1.Action.ActionType != models.ActionDirectEmail {
		t.Fatalf("R1 want EMAIL_NEEDS_REVIEW/DIRECT_EMAIL: %+v", r1.Action)
	}
	if !r1.Action.EmailSendable || r1.Action.Dispatchable {
		t.Fatalf("R1 must be email_sendable and not dispatchable: %+v", r1.Action)
	}
	if r1.RecipientState != RecipientValidated {
		t.Fatalf("R1 recipient %s", r1.RecipientState)
	}

	r2 := byLead["r2-inferred-email"]
	if r2.Action.Lane != models.LaneHumanReviewEmail || r2.Action.ActionType != models.ActionInferredEmailReview {
		t.Fatalf("R2 want HUMAN_REVIEW_EMAIL: %+v", r2.Action)
	}
	if r2.Action.EmailSendable || r2.Action.Dispatchable || r2.RecipientState == RecipientValidated {
		t.Fatalf("R2 must not be VALIDATED/sendable/dispatchable: %+v rec=%s", r2.Action, r2.RecipientState)
	}

	r3 := byLead["r3-routed-call"]
	if r3.Action.ActionType != models.ActionRoutedCall || r3.Action.Lane != models.LaneRoutedCallQueue {
		t.Fatalf("R3 want ROUTED_CALL: %+v", r3.Action)
	}
	if !r3.Action.Actionable || r3.Action.EmailSendable || r3.Action.Dispatchable {
		t.Fatalf("R3 actionable but not email: %+v", r3.Action)
	}
	if r3.Action.ActionType == models.ActionDirectCall || r3.Action.RouteRelation == models.RouteRelBelongsToNamedPerson {
		t.Fatalf("R3 must not be direct phone: %+v", r3.Action)
	}
	if r3.Action.PersonName != "Carlos Silva" {
		t.Fatalf("R3 person: %q", r3.Action.PersonName)
	}

	r4 := byLead["r4-role-mailbox"]
	if r4.Action.ActionType != models.ActionRoleEmail || r4.Action.Lane != models.LaneRoleEmailQueue {
		t.Fatalf("R4 want ROLE_EMAIL: %+v", r4.Action)
	}
	if r4.Action.PersonName != "" || r4.Action.EmailSendable {
		t.Fatalf("R4 must not become personal email: %+v", r4.Action)
	}

	r5 := byLead["r5-corporate-generic"]
	if r5.Action.Lane != models.LaneLowConfidenceManual {
		t.Fatalf("R5 want LOW_CONFIDENCE_MANUAL: %+v", r5.Action)
	}
	if r5.Action.EmailSendable || r5.Action.Dispatchable {
		t.Fatalf("R5 must not send: %+v", r5.Action)
	}

	r0 := byLead["r0-no-route"]
	if !r0.NoAction || r0.Action.Actionable {
		t.Fatalf("R0 want no action: %+v", r0)
	}

	blocked := byLead["blocked-dnc"]
	if blocked.Action.State != models.ActionStateBlocked || blocked.Action.Actionable {
		t.Fatalf("BLOCKED: %+v", blocked.Action)
	}

	// Replay: same snapshot yields deterministic ids.
	r3b := planLead(t, feed.Leads[2])
	if r3.Action.ID != r3b.Action.ID || r3.Action.IdempotencyKey != r3b.Action.IdempotencyKey {
		t.Fatalf("replay drifted id %s vs %s", r3.Action.ID, r3b.Action.ID)
	}
}

func TestCommercialOutcomeFollowupAndGuards(t *testing.T) {
	feed := loadReachabilityFeed(t)
	var routed, call models.OutreachCommercialAction
	for _, lead := range feed.Leads {
		p := planLead(t, lead)
		switch lead.SourceLeadID {
		case "r3-routed-call":
			routed = p.Action
		case "r1-direct-email":
			// reuse as a CALL target by planning a named-manual call
		}
	}
	if routed.ActionType != models.ActionRoutedCall {
		t.Fatal("need routed call fixture")
	}

	g, err := ApplyCommercialOutcome(routed, OutcomeRequest{OutcomeCode: models.OutcomeGatekeeperReached, Actor: "op-1"})
	if err != nil {
		t.Fatal(err)
	}
	if g.Action.State != models.ActionStateNeedsFollowup || g.Action.TargetReached == nil || *g.Action.TargetReached {
		t.Fatalf("gatekeeper is intelligence, not target reached: %+v", g.Action)
	}
	fmt.Printf("OUTCOME ROUTED_CALL -> %s state=%s followup=%v\n", g.Action.OutcomeCode, g.Action.State, g.Followup != nil)

	ref, err := ApplyCommercialOutcome(g.Action, OutcomeRequest{
		OutcomeCode: models.OutcomeReferredToOtherPerson, Actor: "op-1",
		ReferralName: "Maria", ReferralRole: "contratos", NextActionType: models.ActionDirectCall,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Followup == nil || ref.Followup.PersonName != "Maria" {
		t.Fatalf("referral must mint child: %+v", ref.Followup)
	}
	if ref.Followup.ActionType != models.ActionRoutedCall || ref.Followup.RouteRelation != models.RouteRelRoutesToNamedPerson {
		t.Fatalf("switchboard referral must stay ROUTED_CALL, not direct phone: %+v", ref.Followup)
	}
	if ref.Followup.ChannelValue != routed.ChannelValue {
		t.Fatalf("child must keep company number %q, got %q", routed.ChannelValue, ref.Followup.ChannelValue)
	}
	card := AssembleActionCard(*ref.Followup)
	if !strings.Contains(card.RouteEpistemology, "Nao e o telefone direto") {
		t.Fatalf("Maria card must keep switchboard epistemology: %+v", card)
	}
	if ref.Followup.ParentActionID == nil || *ref.Followup.ParentActionID != ref.Action.ID {
		t.Fatal("follow-up must link to parent")
	}
	if ref.Correction == nil || ref.Correction.Kind != CorrectionNewPersonDiscovered || ref.Correction.Source != CorrectionSourceHuman {
		t.Fatalf("referral correction: %+v", ref.Correction)
	}
	fmt.Printf("OUTCOME REFERRED child type=%s person=%s parent=%s\n", ref.Followup.ActionType, ref.Followup.PersonName, ref.Followup.ParentActionID)

	// CALL → TARGET_REACHED → INTERESTED → MEETING, never WON.
	call = *ref.Followup
	tr, err := ApplyCommercialOutcome(call, OutcomeRequest{OutcomeCode: models.OutcomeTargetReached, Actor: "op-1"})
	if err != nil {
		t.Fatal(err)
	}
	if tr.Action.TargetReached == nil || !*tr.Action.TargetReached {
		t.Fatal("target reached")
	}
	in, err := ApplyCommercialOutcome(tr.Action, OutcomeRequest{OutcomeCode: models.OutcomeInterested, Actor: "op-1"})
	if err != nil {
		t.Fatal(err)
	}
	if in.Action.InterestState != models.OutcomeInterested {
		t.Fatalf("interest %s", in.Action.InterestState)
	}
	mt, err := ApplyCommercialOutcome(in.Action, OutcomeRequest{OutcomeCode: models.OutcomeMeetingScheduled, Actor: "op-1"})
	if err != nil {
		t.Fatal(err)
	}
	if mt.Action.InterestState != models.OutcomeMeetingScheduled || mt.Action.OutcomeCode == "WON" {
		t.Fatalf("meeting must not infer WON: %+v", mt.Action)
	}
	if _, err := ApplyCommercialOutcome(mt.Action, OutcomeRequest{OutcomeCode: "WON", Actor: "op-1"}); err == nil {
		t.Fatal("WON must be rejected")
	}
	fmt.Printf("OUTCOME CALL -> TARGET_REACHED -> INTERESTED -> %s won_inferred=false\n", mt.Action.OutcomeCode)

	wrong, err := ApplyCommercialOutcome(routed, OutcomeRequest{OutcomeCode: models.OutcomeWrongPerson, Actor: "op-1"})
	if err != nil {
		t.Fatal(err)
	}
	if CanReplanPerson(wrong.Action, wrong.Action.PersonFingerprint) {
		t.Fatal("WRONG_PERSON must block same person")
	}
	inv, err := ApplyCommercialOutcome(routed, OutcomeRequest{OutcomeCode: models.OutcomeInvalidRoute, Actor: "op-1"})
	if err != nil {
		t.Fatal(err)
	}
	if CanReplanRoute(inv.Action, inv.Action.RouteFingerprint) {
		t.Fatal("INVALID_ROUTE must block silent replan")
	}

	dnc, err := ApplyCommercialOutcome(routed, OutcomeRequest{OutcomeCode: models.OutcomeDNCCode, Actor: "op-1"})
	if err != nil {
		t.Fatal(err)
	}
	if dnc.Action.State != models.ActionStateBlocked || dnc.Action.Actionable {
		t.Fatalf("DNC must block: %+v", dnc.Action)
	}
}

func TestCommercialAdversarialPromotions(t *testing.T) {
	feed := loadReachabilityFeed(t)
	by := map[string]FeedLead{}
	for _, l := range feed.Leads {
		by[l.SourceLeadID] = l
	}

	r3 := planLead(t, by["r3-routed-call"])
	if r3.Action.ActionType == models.ActionDirectCall {
		t.Fatal("switchboard+person must not become direct phone")
	}

	r4 := planLead(t, by["r4-role-mailbox"])
	if r4.Action.ActionType == models.ActionDirectEmail || r4.Action.PersonName != "" {
		t.Fatal("role mailbox + leftover name must not become personal email")
	}

	r2 := planLead(t, by["r2-inferred-email"])
	if r2.RecipientState == RecipientValidated || r2.Action.EmailSendable {
		t.Fatal("R2 must not enter VALIDATED/sendable")
	}

	blocked := planLead(t, by["blocked-dnc"])
	if blocked.Action.Actionable {
		t.Fatal("DNC must block matching route")
	}

	// Person change invalidates content.
	a := r3.Action
	a.ContentHash = "old"
	InvalidateOnPersonChange(&a, "outra pessoa|diretor")
	if a.ContentHash != "" || !containsWarning(a.Warnings, "Pessoa alterada") {
		t.Fatalf("person change: %+v", a)
	}
	b := r3.Action
	b.ContentHash = "old"
	InvalidateOnRouteChange(&b, "other-route")
	if b.ContentHash != "" || b.Dispatchable {
		t.Fatalf("route change: %+v", b)
	}

	// Stale freshness gates start.
	stale := r3.Action
	MarkStaleFreshness(&stale, "")
	if _, err := StartCommercialAction(stale, "op", time.Time{}); err == nil {
		t.Fatal("stale action must not start without review")
	}

	// Duplicate import / replay ids.
	p1 := planLead(t, by["r3-routed-call"])
	p2 := planLead(t, by["r3-routed-call"])
	if p1.Action.ID != p2.Action.ID {
		t.Fatal("duplicate snapshot must reuse id")
	}

	// Manual action never becomes email send.
	if r3.Action.EmailSendable || r3.Action.Dispatchable || r4.Action.EmailSendable {
		t.Fatal("manual route must not create email send bypass")
	}

	// Unknown reachability fails closed.
	lead := by["r1-direct-email"]
	acc, cands, ev := feedLeadToModels(lead)
	cands[0].ReachabilityClass = "FUTURE_CLASS_X"
	p := PlanCommercialAction(PlanInput{Account: acc, Candidate: &cands[0], Candidates: cands, Evidence: ev})
	if p.Action.ReachabilityClass != models.ReachabilityUnmapped || p.Action.EmailSendable || p.Action.Dispatchable {
		t.Fatalf("unknown class must fail closed: %+v", p.Action)
	}

	// Absent class does not invent R-labels.
	cands[0].ReachabilityClass = ""
	p = PlanCommercialAction(PlanInput{Account: acc, Candidate: &cands[0], Candidates: cands, Evidence: ev})
	if p.Action.ReachabilityClass != "" {
		t.Fatalf("missing class invented %s", p.Action.ReachabilityClass)
	}
}

func TestAssembleTodayCardFromR3(t *testing.T) {
	feed := loadReachabilityFeed(t)
	var actions []models.OutreachCommercialAction
	for _, lead := range feed.Leads {
		p := planLead(t, lead)
		if !p.NoAction {
			actions = append(actions, p.Action)
		}
	}
	today := AssembleToday(actions)
	fmt.Printf("TODAY calls=%d routed=%d review=%d inferred=%d role=%d form=%d total=%d\n",
		today.Summary.Calls, today.Summary.RoutedCalls, today.Summary.EmailsToReview,
		today.Summary.InferredEmails, today.Summary.RoleEmails, today.Summary.ContactForms, today.Summary.Total)
	if today.Summary.RoutedCalls < 1 || today.Summary.EmailsToReview < 1 || today.Summary.RoleEmails < 1 {
		t.Fatalf("today summary missing work: %+v", today.Summary)
	}
	var card ActionCard
	for _, c := range today.Actions {
		if c.ActionType == models.ActionRoutedCall {
			card = c
			break
		}
	}
	if card.Company == "" || card.Person != "Carlos Silva" || card.Role == "" {
		t.Fatalf("card identity: %+v", card)
	}
	if card.RecommendedAction == "" || card.WhyNow == "" || card.FactualHook == "" || card.Offer == "" {
		t.Fatalf("card missing work fields: %+v", card)
	}
	if !strings.Contains(card.RouteEpistemology, "Nao e o telefone direto") {
		t.Fatalf("switchboard epistemology missing: %q", card.RouteEpistemology)
	}
	if card.Copy.Opening == "" || len(card.Copy.DoNotClaim) == 0 {
		t.Fatalf("call script missing: %+v", card.Copy)
	}
	if card.EmailSendable || card.Dispatchable {
		t.Fatal("card must not imply send")
	}
	raw, _ := json.Marshal(card)
	fmt.Printf("CARD %s\n", raw)
}

func TestCommercialImportTodayAndReferral(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	raw, err := os.ReadFile(filepath.Join("testdata", "reachability_r0_r5_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{IdempotencyKey: "r0-r5-1"}); xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{IdempotencyKey: "r0-r5-2"}); xerr != nil {
		t.Fatal(xerr)
	}
	open, err := repo.ListCommercialActions(context.Background(), org, uuid.Nil, true, 50)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]int{}
	for _, a := range open {
		keys[a.IdempotencyKey]++
		if keys[a.IdempotencyKey] > 1 {
			t.Fatalf("duplicate open action %s", a.IdempotencyKey)
		}
	}
	accs, err := repo.ListAccounts(context.Background(), org, repository.OutreachAccountFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for i := range accs {
		if accs[i].DoNotContact {
			continue
		}
		accs[i].QueueState = models.OutreachQueueReadyToGenerate
		if _, err := repo.UpsertAccount(context.Background(), &accs[i]); err != nil {
			t.Fatal(err)
		}
	}
	today, xerr := svc.CollectToday(context.Background(), org)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if today.Summary.RoutedCalls < 1 || today.Summary.EmailsToReview < 1 {
		t.Fatalf("today after import: %+v", today.Summary)
	}
	var routedID uuid.UUID
	for _, c := range today.Actions {
		if c.ActionType == models.ActionRoutedCall {
			routedID = uuid.MustParse(c.ActionID)
			break
		}
	}
	if routedID == uuid.Nil {
		t.Fatal("missing routed call in today")
	}
	res, xerr := svc.RecordCommercialOutcome(context.Background(), org, user, routedID, OutcomeRequest{
		OutcomeCode: models.OutcomeReferredToOtherPerson, ReferralName: "Maria", ReferralRole: "contratos",
		NextActionType: models.ActionDirectCall,
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if res.Followup == nil || res.Followup.ParentActionID == nil || *res.Followup.ParentActionID != routedID {
		t.Fatalf("follow-up not persisted: %+v", res.Followup)
	}
	if res.Followup.ActionType != models.ActionRoutedCall || res.Followup.RouteRelation != models.RouteRelRoutesToNamedPerson {
		t.Fatalf("persisted follow-up must stay routed: %+v", res.Followup)
	}
	today2, xerr := svc.CollectToday(context.Background(), org)
	if xerr != nil {
		t.Fatal(xerr)
	}
	foundChild := false
	for _, c := range today2.Actions {
		if c.Person == "Maria" && c.ParentActionID == routedID.String() {
			foundChild = true
			if c.ActionType != models.ActionRoutedCall || !strings.Contains(c.RouteEpistemology, "Nao e o telefone direto") {
				t.Fatalf("today Maria card must stay routed switchboard: %+v", c)
			}
		}
	}
	if !foundChild {
		t.Fatalf("follow-up missing from today: %+v", today2.Actions)
	}
}

// Kill switch / dispatch pause must not drop planned commercial work or
// promote any card to dispatchable. Sending is deferred; Today stays.
func TestCommercialTodaySurvivesKillSwitchAndPause(t *testing.T) {
	t.Setenv(EnvKillSwitchPath, filepath.Join(t.TempDir(), "kill"))
	t.Setenv(EnvSendingPaused, "false")

	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	raw, err := os.ReadFile(filepath.Join("testdata", "reachability_r0_r5_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{IdempotencyKey: "pause-1"}); xerr != nil {
		t.Fatal(xerr)
	}
	accs, err := repo.ListAccounts(context.Background(), org, repository.OutreachAccountFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for i := range accs {
		if accs[i].DoNotContact {
			continue
		}
		accs[i].QueueState = models.OutreachQueueReadyToGenerate
		if _, err := repo.UpsertAccount(context.Background(), &accs[i]); err != nil {
			t.Fatal(err)
		}
	}
	before, xerr := svc.CollectToday(context.Background(), org)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if before.Summary.RoutedCalls < 1 || before.Summary.EmailsToReview < 1 || before.Summary.InferredEmails < 1 {
		t.Fatalf("pre-pause today missing work: %+v", before.Summary)
	}
	ids := map[string]ActionCard{}
	for _, c := range before.Actions {
		ids[c.ActionID] = c
		if c.Dispatchable {
			t.Fatalf("pre-pause card dispatchable: %+v", c)
		}
	}

	if err := EngageKillSwitch(); err != nil {
		t.Fatal(err)
	}
	if svc.cfg.SendingAllowed() {
		t.Fatal("kill switch must block SendingAllowed")
	}
	gate := svc.GateCampaignEmail(context.Background(), org, "CONFENGE pause retention", "ana.souza@aurora.example", uuid.Nil, uuid.Nil, uuid.Nil)
	if gate.Kind != GateDeferred || gate.Reason != ReasonSendingOff {
		t.Fatalf("pause must defer CONFENGE send, got kind=%v reason=%s", gate.Kind, gate.Reason)
	}

	after, xerr := svc.CollectToday(context.Background(), org)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if after.Summary.RoutedCalls != before.Summary.RoutedCalls ||
		after.Summary.EmailsToReview != before.Summary.EmailsToReview ||
		after.Summary.InferredEmails != before.Summary.InferredEmails {
		t.Fatalf("pause dropped today work: before=%+v after=%+v", before.Summary, after.Summary)
	}
	for _, c := range after.Actions {
		if _, ok := ids[c.ActionID]; !ok {
			t.Fatalf("pause invented a new card: %+v", c)
		}
		if c.Dispatchable || (c.ActionType != models.ActionDirectEmail && c.EmailSendable) {
			t.Fatalf("pause must not promote sendability: %+v", c)
		}
		if c.ActionType == models.ActionRoutedCall && !c.Actionable {
			t.Fatalf("routed call must stay actionable through pause: %+v", c)
		}
		if c.ActionType == models.ActionInferredEmailReview && (c.EmailSendable || c.Dispatchable) {
			t.Fatalf("inferred email must stay unsendable through pause: %+v", c)
		}
	}
	open, err := repo.ListCommercialActions(context.Background(), org, uuid.Nil, false, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) == 0 {
		t.Fatal("pause must not delete persisted commercial actions")
	}
}

// Record DNC on a shipped Today card, then CollectToday again: the same
// route must stay off Today (persist must not upsert a fresh READY row).
func TestCommercialDNCStaysOffToday(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	raw, err := os.ReadFile(filepath.Join("testdata", "reachability_r0_r5_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{IdempotencyKey: "dnc-today-1"}); xerr != nil {
		t.Fatal(xerr)
	}
	accs, err := repo.ListAccounts(context.Background(), org, repository.OutreachAccountFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for i := range accs {
		if accs[i].DoNotContact {
			continue
		}
		accs[i].QueueState = models.OutreachQueueReadyToGenerate
		if _, err := repo.UpsertAccount(context.Background(), &accs[i]); err != nil {
			t.Fatal(err)
		}
	}
	before, xerr := svc.CollectToday(context.Background(), org)
	if xerr != nil {
		t.Fatal(xerr)
	}
	var routedID uuid.UUID
	for _, c := range before.Actions {
		if c.ActionType == models.ActionRoutedCall {
			routedID = uuid.MustParse(c.ActionID)
			break
		}
	}
	if routedID == uuid.Nil {
		t.Fatal("need ROUTED_CALL in today before DNC")
	}
	res, xerr := svc.RecordCommercialOutcome(context.Background(), org, user, routedID, OutcomeRequest{
		OutcomeCode: models.OutcomeDNCCode, Notes: "do not call again",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if res.Action.State != models.ActionStateBlocked || res.Action.Actionable || !res.Action.BlockedPerson || !res.Action.BlockedRoute {
		t.Fatalf("DNC must block person and route: %+v", res.Action)
	}
	if CanReplanPerson(res.Action, res.Action.PersonFingerprint) || CanReplanRoute(res.Action, res.Action.RouteFingerprint) {
		t.Fatal("DNC must refuse silent replan of the same person/route")
	}

	after, xerr := svc.CollectToday(context.Background(), org)
	if xerr != nil {
		t.Fatal(xerr)
	}
	for _, c := range after.Actions {
		if c.ActionID == routedID.String() {
			t.Fatalf("DNC card must leave Today, got %+v", c)
		}
		if c.ActionType == models.ActionRoutedCall && c.Actionable {
			t.Fatalf("no actionable ROUTED_CALL after DNC: %+v", c)
		}
	}
	stored, err := repo.GetCommercialAction(context.Background(), org, routedID)
	if err != nil || stored == nil {
		t.Fatalf("persisted DNC action: %v", err)
	}
	if stored.State != models.ActionStateBlocked || stored.OutcomeCode != models.OutcomeDNCCode || stored.Actionable {
		t.Fatalf("CollectToday must not replace DNC with a fresh READY card: %+v", stored)
	}
	fmt.Printf("DNC stays off today id=%s state=%s outcome=%s\n", stored.ID, stored.State, stored.OutcomeCode)
}

func containsWarning(in []string, sub string) bool {
	for _, w := range in {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}
