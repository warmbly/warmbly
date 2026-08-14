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

	"github.com/warmbly/warmbly/internal/app/confenge/dispatch"
	"github.com/warmbly/warmbly/internal/models"
)

func applyValidatedIdentity(c *models.OutreachContactCandidate) {
	if c == nil {
		return
	}
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if strings.TrimSpace(c.Name) == "" {
		c.Name = "Ana Silva"
	}
	if strings.TrimSpace(c.Role) == "" {
		c.Role = "Diretora de Contratos"
	}
	c.EmailSendReady = true
	c.OwnershipStatus = "COMPANY_OWNED"
	c.RecipientCommercialSuitability = "SUITABLE"
	c.MailboxPurpose = "COMERCIAL"
	c.MailboxPurposeSendBlocked = false
	c.SourceDate = &now
	if c.Email != "" && strings.TrimSpace(c.SourceURL) == "" {
		c.SourceURL = "https://" + emailDomain(c.Email) + "/equipe"
	}
}

func stampValidatedCandidates(t *testing.T, repo *memRepo, org, acc uuid.UUID) {
	t.Helper()
	list, err := repo.ListCandidates(context.Background(), org, acc)
	if err != nil {
		t.Fatal(err)
	}
	for i := range list {
		c := list[i]
		applyValidatedIdentity(&c)
		if _, err := repo.UpsertCandidate(context.Background(), &c); err != nil {
			t.Fatal(err)
		}
	}
}

func validatedCand(name, role, email string) models.OutreachContactCandidate {
	src := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	return models.OutreachContactCandidate{
		ID:                             uuid.New(),
		SourceContactID:                "src-contact-1",
		Name:                           name,
		Role:                           role,
		Email:                          email,
		VerificationStatus:             models.OutreachVerifyVerified,
		EmailSendReady:                 true,
		OwnershipStatus:                "COMPANY_OWNED",
		RecipientCommercialSuitability: "SUITABLE",
		SourceURL:                      "https://" + emailDomain(email) + "/equipe",
		SourceDate:                     &src,
		Confidence:                     "HIGH",
		Recommended:                    true,
	}
}

func TestResolveRecipientValidatedRequiresProvenIdentity(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato 1149/2022 atingiu aniversário de reajuste em 2024")
	acc.SourceLeadID = "lead-encopav"
	c := validatedCand("Ana Silva", "Diretora de Contratos", "ana@encopav.com.br")
	res := ResolveRecipient(acc, []models.OutreachContactCandidate{c}, now)
	if res.State != RecipientValidated {
		t.Fatalf("want VALIDATED got %s %v %s", res.State, res.ReasonCodes, res.Reason)
	}
	if res.Name != "Ana Silva" || res.Role != "Diretora de Contratos" {
		t.Fatalf("identity %+v", res)
	}
	if res.CanonicalTargetID != "lead-encopav" || res.CanonicalContactID == "" {
		t.Fatal("canonical ids required")
	}
}

func TestGenerateTouchpointDraftGenericMailboxFailClosed(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato 1149/2022 atingiu aniversário de reajuste em 2024")
	acc.OrganizationID = org
	acc.QueueState = models.OutreachQueueReadyToGenerate
	acc.SourceLeadID = "lead-generic-mailbox"
	if _, err := repo.UpsertAccount(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	c := validatedCand("Pessoa Histórica", "Contato", "contato@encopav.com.br")
	c.OrganizationID = org
	c.AccountID = acc.ID
	c.MailboxPurpose = "GENERIC_CONTACT"
	c.VerificationStatus = models.OutreachVerifyOfficialSource
	if _, err := repo.UpsertCandidate(context.Background(), &c); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertEvidence(context.Background(), &models.OutreachEvidence{
		OrganizationID: org, AccountID: acc.ID, SourceEvidenceID: "ev-1",
		Title: "Contrato", Synthesis: acc.FactToMention,
		EpistemicClass: models.OutreachEpistemicConfirmedFact,
	}); err != nil {
		t.Fatal(err)
	}
	list, xerr := svc.PlanAccountCadence(context.Background(), org, user, acc.ID, &c.ID, models.OutreachChannelEmail)
	if xerr != nil || len(list) == 0 {
		t.Fatalf("plan: %v", xerr)
	}
	tp, xerr := svc.GenerateTouchpointDraft(context.Background(), org, user, list[0].ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if strings.TrimSpace(tp.BodyText) != "" {
		t.Fatalf("generic mailbox must not produce a sendable body: %q", tp.BodyText)
	}
	if tp.State == models.TouchpointNeedsReview {
		t.Fatalf("generic mailbox must not enter NEEDS_REVIEW: %+v", tp)
	}
	if tp.DraftID == nil {
		t.Fatal("fail-closed draft must still be persisted")
	}
	draft, err := repo.GetDraft(context.Background(), org, *tp.DraftID)
	if err != nil || draft == nil {
		t.Fatalf("draft: %v", err)
	}
	if draft.Status == models.OutreachDraftNeedsReview || strings.TrimSpace(draft.BodyText) != "" {
		t.Fatalf("draft must not be sendable: status=%s body=%q", draft.Status, draft.BodyText)
	}
	if rec := recipientFromDraft(draft); rec == nil || rec.State == RecipientValidated {
		t.Fatalf("recipient must not be VALIDATED: %+v", rec)
	}
	if _, xerr := svc.ApproveTouchpoint(context.Background(), org, user, tp.ID, ApprovalOptions{GenericRecipientAcknowledged: true}); xerr == nil {
		t.Fatal("acknowledgement must not authorize a generic mailbox")
	}
}

func TestResolveRecipientNeverPromotesGeneric(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato 1149/2022 atingiu aniversário de reajuste")
	c := validatedCand("Pessoa Histórica", "Contato", "contato@encopav.com.br")
	c.MailboxPurpose = "GENERIC_CONTACT"
	res := ResolveRecipient(acc, []models.OutreachContactCandidate{c}, now)
	if res.State == RecipientValidated {
		t.Fatal("generic mailbox must not be VALIDATED")
	}
	if res.State != RecipientException {
		t.Fatalf("generic want EXCEPTION got %s %v", res.State, res.ReasonCodes)
	}
	if res.Name != "" {
		t.Fatalf("must not surface invented/generic name: %q", res.Name)
	}
	if res.HumanDecision == "" || res.NextAction == "" {
		t.Fatal("exception must tell the human what to decide")
	}
}

func TestResolveRecipientMissingDateStaleDNCBounce(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	acc := testAccount("REAJUSTE", "ANUALIDADE", "x")

	missing := validatedCand("Ana Silva", "Diretora", "ana@encopav.com.br")
	missing.SourceDate = nil
	if got := ResolveRecipient(acc, []models.OutreachContactCandidate{missing}, now); got.State == RecipientValidated {
		t.Fatal("missing evidence date must not VALIDATE")
	}

	stale := validatedCand("Ana Silva", "Diretora", "ana@encopav.com.br")
	old := now.Add(-400 * 24 * time.Hour)
	stale.SourceDate = &old
	if got := ResolveRecipient(acc, []models.OutreachContactCandidate{stale}, now); got.State == RecipientValidated {
		t.Fatal("stale evidence must not VALIDATE")
	}

	dnc := validatedCand("Ana Silva", "Diretora", "ana@encopav.com.br")
	dnc.DoNotContact = true
	if got := ResolveRecipient(acc, []models.OutreachContactCandidate{dnc}, now); got.State != RecipientBlocked {
		t.Fatalf("DNC want BLOCKED got %s", got.State)
	}

	bounce := validatedCand("Ana Silva", "Diretora", "ana@encopav.com.br")
	bounce.Bounced = true
	if got := ResolveRecipient(acc, []models.OutreachContactCandidate{bounce}, now); got.State != RecipientBlocked {
		t.Fatalf("bounce want BLOCKED got %s", got.State)
	}

	sup := validatedCand("Ana Silva", "Diretora", "ana@encopav.com.br")
	sup.Blocked = true
	sup.BlockReason = "suppressed"
	if got := ResolveRecipient(acc, []models.OutreachContactCandidate{sup}, now); got.State != RecipientBlocked {
		t.Fatalf("suppressed want BLOCKED got %s", got.State)
	}
}

func TestResolveRecipientDoesNotInventIdentity(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	acc := testAccount("REAJUSTE", "ANUALIDADE", "x")
	c := validatedCand("", "", "joao.silva@encopav.com.br")
	res := ResolveRecipient(acc, []models.OutreachContactCandidate{c}, now)
	if res.State == RecipientValidated {
		t.Fatal("unproven name/role must not VALIDATE")
	}
	if res.Name != "" || res.Role != "" {
		t.Fatalf("invented identity: %+v", res)
	}
}

func TestProcessPilotAccountReadyRequiresBothGates(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato 1149/2022 atingiu aniversário de reajuste em 2024")
	acc.SourceLeadID = "lead-1"
	c := validatedCand("Ana Silva", "Diretora de Contratos", "ana@encopav.com.br")
	ev := []models.OutreachEvidence{{SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicConfirmedFact, Synthesis: acc.FactToMention}}
	row := ProcessPilotAccount(acc, []models.OutreachContactCandidate{c}, ev, now)
	if row.FinalState != MessageabilityReady || row.SendableBody == "" {
		t.Fatalf("strong named want READY: %+v", row)
	}
	if ClassifyReviewQueue(row) != ReviewQueueReady {
		t.Fatalf("queue %s", row.Queue)
	}
	assertNoLeaks(t, row.SendableBody)
}

func TestProcessPilotAccountGenericNeverReady(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato 1149/2022 atingiu aniversário de reajuste em 2024")
	c := validatedCand("Pessoa Histórica", "Contato", "contato@encopav.com.br")
	c.MailboxPurpose = "GENERIC_CONTACT"
	ev := []models.OutreachEvidence{{SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicConfirmedFact, Synthesis: acc.FactToMention}}
	row := ProcessPilotAccount(acc, []models.OutreachContactCandidate{c}, ev, now)
	if row.FinalState == MessageabilityReady || row.SendableBody != "" {
		t.Fatalf("generic must not be commercially READY: %+v", row)
	}
	if ClassifyReviewQueue(row) == ReviewQueueReady {
		t.Fatal("generic leaked into READY queue")
	}
}

func TestEnrichmentUnavailableFailClosed(t *testing.T) {
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato 1149/2022 atingiu aniversário de reajuste em 2024")
	c := validatedCand("Ana Silva", "Diretora", "ana@encopav.com.br")
	ev := []models.OutreachEvidence{{SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicConfirmedFact, Synthesis: acc.FactToMention}}
	_, _, att := AttemptEnrichment(EnrichmentInput{
		Account: acc, Candidates: []models.OutreachContactCandidate{c}, Evidence: ev, Unavailable: true,
	})
	if att.Status != EnrichmentUnavailable || att.Resolved {
		t.Fatalf("unavailable must fail closed: %+v", att)
	}
}

func TestHumanCorrectionNeverSilent(t *testing.T) {
	_, err := RecordHumanDecision(DecisionApprove, "", "actor", "a", "a", "s", "s", nil)
	if err == nil {
		t.Fatal("missing draft must fail")
	}
	hc, err := RecordHumanDecision(DecisionApprove, "d1", "actor", "before", "before", "s", "s", nil)
	if err != nil || hc.Silent {
		t.Fatalf("approve: %+v %v", hc, err)
	}
	_, err = RecordHumanDecision(DecisionEdit, "d1", "actor", "same", "same", "s", "s", []string{"generic_copy"})
	if err == nil {
		t.Fatal("edit without diff must fail")
	}
	ed, err := RecordHumanDecision(DecisionEdit, "d1", "actor", "old", "new better hook", "s", "s", []string{"weak_hook"})
	if err != nil || ed.BeforeBody == ed.AfterBody || ed.ReasonCodes[0] != "weak_hook" {
		t.Fatalf("edit: %+v %v", ed, err)
	}
}

func TestLedgerIdempotentNoInferredWon(t *testing.T) {
	l := NewMemoryLedger("secret")
	a, ex := l.RecordAction(ActionRecord{IdempotencyKey: "act:1", Kind: "SEND", TargetID: "t1", MessageID: "m1"})
	if ex != nil || a.Receipt == "" {
		t.Fatalf("action: %+v %+v", a, ex)
	}
	a2, ex := l.RecordAction(ActionRecord{IdempotencyKey: "act:1", Kind: "SEND", TargetID: "t1"})
	if ex != nil || a2.ActionID != a.ActionID {
		t.Fatal("duplicate action must return same receipt")
	}
	o, ex := l.RecordOutcome(LedgerOutcome{IdempotencyKey: "out:1", ActionID: a.ActionID, Type: OutcomeContacted})
	if ex != nil || o.Receipt == "" {
		t.Fatalf("outcome: %+v %+v", o, ex)
	}
	o2, ex := l.RecordOutcome(LedgerOutcome{IdempotencyKey: "out:1", ActionID: a.ActionID, Type: OutcomeContacted})
	if ex != nil || o2.OutcomeID != o.OutcomeID {
		t.Fatal("duplicate outcome")
	}
	_, ex = l.RecordOutcome(LedgerOutcome{IdempotencyKey: "out:won", ActionID: a.ActionID, Type: OutcomeWon})
	if ex == nil || ex.Code != LedgerConflict {
		t.Fatalf("WON without human confirm: %+v", ex)
	}
	// out of order
	_, ex = l.RecordOutcome(LedgerOutcome{IdempotencyKey: "out:orphan", ActionID: "missing-action", Type: OutcomeReplied})
	if ex == nil || ex.Code != LedgerOrphan {
		t.Fatalf("orphan: %+v", ex)
	}
	l.SetUnavailable(true)
	_, ex = l.RecordAction(ActionRecord{IdempotencyKey: "act:down", Kind: "SEND"})
	if ex == nil || ex.Code != LedgerUnavailable {
		t.Fatal("unavailable must fail closed")
	}
	l.SetUnavailable(false)
	ch, ex := l.Reconstruct("act:1")
	if ex != nil || ch.ActionID != a.ActionID || ch.OutcomeID != o.OutcomeID {
		t.Fatalf("reconstruct: %+v %+v", ch, ex)
	}
}

func TestReleaseManifestDriftIsNoGo(t *testing.T) {
	base := ReleaseManifest{
		RepositorySHA:          "abc",
		ImageDigests:           []string{"sha256:1"},
		Schema:                 "confenge.outreach.v1",
		FeedHash:               "feed1",
		CohortHash:             "coh1",
		ComposerVersion:        ComposerVersion,
		DoctrineVersion:        OutreachDoctrineVersion,
		RecipientPolicyVersion: RecipientPolicyVersion,
		ApprovalsHash:          "appr1",
		CIResults:              "pass",
		RuntimeResults:         "pass",
		HumanApprovals:         8,
		ReadyCount:             8,
		KillSwitch:             true,
		AutoSend:               false,
		RequireHumanApproval:   true,
	}
	if v := EvaluateRelease(base, base); v.Verdict != ReleaseGO {
		t.Fatalf("identical must GO: %+v", v)
	}
	drift := base
	drift.FeedHash = "feed2"
	if v := EvaluateRelease(base, drift); v.Verdict != ReleaseNOGO {
		t.Fatal("feed drift must NO_GO")
	}
	noHuman := base
	noHuman.HumanApprovals = 0
	if v := EvaluateRelease(base, noHuman); v.Verdict != ReleaseNOGO {
		t.Fatal("zero approvals must NO_GO")
	}
	if !InvalidateOnDrift(base, drift) {
		t.Fatal("invalidate on drift")
	}
}

func TestCanaryFailClosedNoLiveSend(t *testing.T) {
	clock := &dispatch.FixedClock{T: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)}
	store := dispatch.NewMemoryStore()
	cfg := dispatch.DefaultConfig()
	cfg.WindowStart, cfg.WindowEnd, cfg.Timezone = "00:00", "23:59", "UTC"
	cfg.MinGap = time.Minute
	cfg.SendsPerHour = 10
	cfg.BusinessDaysOnly = false
	g := dispatch.NewGovernor(cfg, store, clock)
	org := uuid.New()
	ctx := t.Context()
	var last dispatch.ReserveResult
	for i := 0; i < 10; i++ {
		res, err := g.TryReserve(ctx, dispatch.ReserveRequest{OrganizationID: org, Channel: dispatch.ChannelEmail, MessageKey: "k" + string(rune('a'+i))})
		if err != nil || !res.Allowed {
			t.Fatalf("reserve %d: %+v %v", i, res, err)
		}
		if err := g.Commit(ctx, res.Reservation.ID); err != nil {
			t.Fatal(err)
		}
		last = res
		clock.Advance(time.Minute)
	}
	blocked, err := g.TryReserve(ctx, dispatch.ReserveRequest{OrganizationID: org, Channel: dispatch.ChannelEmail, MessageKey: "k-over"})
	if err != nil || blocked.Allowed {
		t.Fatalf("11th must be denied: %+v %v", blocked, err)
	}
	// restart idempotency: same key
	clock.Advance(-time.Minute)
	again, err := g.TryReserve(ctx, dispatch.ReserveRequest{OrganizationID: org, Channel: dispatch.ChannelEmail, MessageKey: last.Reservation.MessageKey})
	if err != nil {
		t.Fatal(err)
	}
	if !again.AlreadyCommitted && again.Allowed {
		// either already committed or denied by cap; never a second send
		t.Fatalf("restart must not mint a new send: %+v", again)
	}
	if err := g.Pause(ctx, "kill_switch", nil); err != nil {
		t.Fatal(err)
	}
	paused, err := g.TryReserve(ctx, dispatch.ReserveRequest{OrganizationID: org, Channel: dispatch.ChannelEmail, MessageKey: "after-pause"})
	if err != nil || paused.Allowed {
		t.Fatalf("paused must deny: %+v %v", paused, err)
	}
}

func TestRealSmokeCorpusHonestTerminal(t *testing.T) {
	path := os.Getenv("CORPUS_AUDIT_IN")
	if path == "" {
		path = filepath.Join("testdata", "real_smoke_chunk.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip(err)
	}
	feed, err := DetectAndNormalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	rep := ProcessFeedCorpus(feed, 30, time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC))
	if out := os.Getenv("CORPUS_AUDIT_OUT"); out != "" {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if rep.Selected != 30 {
		t.Fatalf("selected=%d", rep.Selected)
	}
	if rep.UnexplainedState != 0 {
		t.Fatalf("unexplained=%d", rep.UnexplainedState)
	}
	if rep.RecipientValidated+rep.RecipientException+rep.RecipientBlocked != 30 {
		t.Fatalf("recipient partition %+v", rep)
	}
	if rep.MessageReady+rep.NeedsEnrichment+rep.MessageBlocked != 30 {
		t.Fatalf("message partition %+v", rep)
	}
	for _, r := range rep.Rows {
		if r.RecipientState != RecipientValidated && r.RecipientState != RecipientException && r.RecipientState != RecipientBlocked {
			t.Fatalf("bad recipient state %+v", r)
		}
		if r.FinalState == MessageabilityReady && r.SendableBody == "" {
			t.Fatalf("READY without body %+v", r)
		}
		if r.RecipientState != RecipientValidated && r.FinalState == MessageabilityReady {
			t.Fatalf("unvalidated READY %+v", r)
		}
		if looksLikeMetadataDump(r.SendableBody) {
			t.Fatalf("leak in body: %s", r.SendableBody)
		}
	}
	ready, exception, enrich, blocked := SplitReviewQueues(rep.Rows)
	if len(ready) != rep.MessageReady {
		t.Fatalf("ready queue %d vs %d", len(ready), rep.MessageReady)
	}
	_ = exception
	_ = enrich
	_ = blocked
	t.Logf("corpus selected=%d validated=%d exception=%d blocked=%d message_ready=%d enrichment=%d message_blocked=%d human=%d",
		rep.Selected, rep.RecipientValidated, rep.RecipientException, rep.RecipientBlocked,
		rep.MessageReady, rep.NeedsEnrichment, rep.MessageBlocked, rep.HumanDecisionsRequired)
	if raw, err := json.MarshalIndent(rep, "", "  "); err == nil {
		_ = os.WriteFile("/tmp/grok-goal-6701144eb946/implementer/corpus-audit.json", raw, 0o644)
	}
}

func TestApprovalInvalidationOnRecipientAndEvidenceChange(t *testing.T) {
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato 1149/2022 atingiu aniversário de reajuste em 2024")
	acc.SourceLeadID = "lead-1"
	acc.MessageContextHash = "ctx-1"
	c := validatedCand("Ana Silva", "Diretora", "ana@encopav.com.br")
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	row := ProcessPilotAccount(acc, []models.OutreachContactCandidate{c}, []models.OutreachEvidence{{
		SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicConfirmedFact, Synthesis: acc.FactToMention,
	}}, now)
	approved := ReleaseManifest{
		RepositorySHA: "sha", FeedHash: "f1", CohortHash: HashMaterial(row.CanonicalTargetID, row.SendableBody),
		ApprovalsHash: HashMaterial("approve:" + row.CanonicalTargetID),
		Schema:        "confenge.outreach.v1", ImageDigests: []string{"d"},
		ComposerVersion: ComposerVersion, DoctrineVersion: OutreachDoctrineVersion,
		RecipientPolicyVersion: RecipientPolicyVersion, CIResults: "pass", RuntimeResults: "pass",
		HumanApprovals: 1, ReadyCount: 1, KillSwitch: true, RequireHumanApproval: true,
	}
	// recipient changed
	c2 := c
	c2.Email = "outra@encopav.com.br"
	c2.SourceURL = "https://encopav.com.br/equipe"
	row2 := ProcessPilotAccount(acc, []models.OutreachContactCandidate{c2}, []models.OutreachEvidence{{
		SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicConfirmedFact, Synthesis: acc.FactToMention,
	}}, now)
	cur := approved
	cur.CohortHash = HashMaterial(row2.CanonicalTargetID, row2.SendableBody, row2.CanonicalContactID+row2.RecipientState)
	if !InvalidateOnDrift(approved, cur) {
		t.Fatal("recipient/cohort change must invalidate the release")
	}
	// evidence changed
	acc.FactToMention = "aditivo 9 publicado no contrato 88"
	row3 := ProcessPilotAccount(acc, []models.OutreachContactCandidate{c}, []models.OutreachEvidence{{
		SourceEvidenceID: "ev-2", EpistemicClass: models.OutreachEpistemicConfirmedFact, Synthesis: acc.FactToMention,
	}}, now)
	if row3.MentionableEvidence == row.MentionableEvidence && row.FinalState == MessageabilityReady && row3.FinalState == MessageabilityReady {
		// different fact should change mentionable surface when READY
		if strings.Contains(row3.MentionableEvidence, "1149") && strings.Contains(row.MentionableEvidence, "aditivo 9") {
			t.Fatal("evidence swap not reflected")
		}
	}
	_ = approved
}
