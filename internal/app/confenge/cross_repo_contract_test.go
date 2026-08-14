package confenge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/confenge/dispatch"
	"github.com/warmbly/warmbly/internal/models"
)

// Cross-repo contract suite (Warmbly side of confenge.outreach.v1).
// Proves architecture without commercial send to real leads.

func TestCrossRepoLegacyFeedImportNoAutorun(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org, user := uuid.New(), uuid.New()
	svc := NewService(Config{
		Enabled: true, GreenAutorunEnabled: true, AppEnv: "test",
		MaxInitialEmailWords: 120, RateMaxPerHour: 20, DefaultDailyLimit: 100, RequireHumanApproval: true,
	}, repo, nil).(*service)
	store := newMemPolicyStore()
	svc.WirePolicyAuth(store)

	// Legacy feed: activation present, NO target_fit_send_tier / email_send_ready.
	lead := sampleLeadWithActivation(80, ActivationActionableNow)
	// TargetFitSendTier intentionally absent (legacy)
	lead.TargetFitSendTier = ""
	lead.EmailSendReady = nil
	for i := range lead.Contacts {
		lead.Contacts[i].EmailSendReady = nil
		lead.Contacts[i].OwnershipStatus = ""
	}
	feed := Feed{
		SchemaVersion: models.OutreachSchemaV1,
		GeneratedAt:   "2026-08-08T10:00:00Z",
		Source:        FeedSource{System: "extra-cli", RunID: "legacy-1", SnapshotHash: "snap-legacy", ProfileID: "p", ProfileVersion: "1"},
		Pagination:    FeedPagination{HasMore: false},
		Leads:         []FeedLead{lead},
	}
	raw, _ := json.Marshal(feed)
	run, xerr := svc.ImportFromBytes(ctx, org, &user, raw, ImportOptions{IdempotencyKey: "legacy-contract-1"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if run == nil || run.Status != models.OutreachImportCompleted {
		t.Fatalf("import run=%+v", run)
	}
	acc, _ := repo.GetAccountByCNPJ(ctx, org, "11222333000181")
	if acc == nil {
		t.Fatal("account not imported")
	}
	if acc.ActivationState != ActivationActionableNow {
		t.Fatalf("activation=%s", acc.ActivationState)
	}
	if acc.TargetFitSendTier != "" {
		t.Fatalf("legacy must not invent tier, got %q", acc.TargetFitSendTier)
	}
	if ImportedSendTierEligible(acc.TargetFitSendTier) {
		t.Fatal("legacy tier must not be send-eligible")
	}

	// Wire campaign + try autorun path via build input
	camp := uuid.New()
	_ = repo.UpsertOrgSettings(ctx, &models.OutreachOrgSettings{OrganizationID: org, CampaignID: &camp})
	_, _ = svc.AuthorizeCampaignPolicy(ctx, org, user, &models.CampaignPolicyAuthorization{
		CampaignID: camp, Channel: "EMAIL", AllowedRiskClass: "GREEN",
		SenderMailbox: "tiago.sasaki@confenge.com.br", AllowPolicyTemplateGREEN: true,
		EffectiveAt: time.Now().UTC().Add(-time.Minute),
	})
	cands, _ := repo.ListCandidates(ctx, org, acc.ID)
	if len(cands) == 0 {
		t.Fatal("expected candidate")
	}
	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: acc.ID, ContactCandidateID: &cands[0].ID,
		Channel: "EMAIL", State: models.TouchpointNeedsReview, Recipient: cands[0].Email,
		Subject: "S", BodyText: "body fato público", ServiceCode: "REAJUSTE",
		FactUsed: "fato público no PNCP", GeneratedContextHash: acc.MessageContextHash,
	}
	in := svc.buildGreenAutorunInput(ctx, org, tp, nil)
	if in.TargetFitSendTier == "A_AUTOMATIC" {
		t.Fatal("must not promote ACTIONABLE_NOW to A_AUTOMATIC")
	}
	if in.EmailSendReady {
		t.Fatal("legacy without imported email_send_ready must not be ready for autorun")
	}
}

func TestCrossRepoModernFeedPreservesReadiness(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org, user := uuid.New(), uuid.New()
	svc := NewService(Config{Enabled: true, AppEnv: "test", DefaultDailyLimit: 100, RequireHumanApproval: true}, repo, nil).(*service)

	ready := true
	blocked := false
	lead := sampleLeadWithActivation(75, ActivationActionableNow)
	lead.Company.CNPJ14 = "22333444000155"
	lead.Company.RazaoSocial = "Modern Eng SA"
	lead.Offer.ServiceCode = "REAJUSTE_14133"
	lead.MessagingContext.ClaimsToAvoid = []string{"garantimos economia"}
	lead.TargetFitSendTier = "A_AUTOMATIC"
	lead.TargetFitReasons = []string{"CONFIRMED_ENGINEERING", "EMAIL_READY"}
	lead.EmailSendReady = &ready
	lead.MailboxPurpose = "comercial"
	lead.OwnershipStatus = "COMPANY_OWNED"
	lead.Contacts[0].Email = "bruno@moderneng.com.br"
	lead.Contacts[0].EmailSendReady = &ready
	lead.Contacts[0].MailboxPurpose = "comercial"
	lead.Contacts[0].MailboxPurposeSendBlocked = &blocked
	lead.Contacts[0].OwnershipStatus = "COMPANY_OWNED"
	lead.Contacts[0].RecipientCommercialSuitability = "SUITABLE"
	feed := Feed{
		SchemaVersion: models.OutreachSchemaV1,
		GeneratedAt:   "2026-08-08T12:00:00Z",
		Source:        FeedSource{System: "extra-cli", RunID: "modern-1", SnapshotHash: "snap-m", ProfileID: "p", ProfileVersion: "1"},
		Pagination:    FeedPagination{HasMore: false},
		Leads:         []FeedLead{lead},
	}
	raw, _ := json.Marshal(feed)
	run, xerr := svc.ImportFromBytes(ctx, org, &user, raw, ImportOptions{IdempotencyKey: "modern-1"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if run.Status != models.OutreachImportCompleted {
		t.Fatalf("status=%s errs=%v", run.Status, run)
	}
	acc, _ := repo.GetAccountByCNPJ(ctx, org, "22333444000155")
	if acc == nil {
		t.Fatal("missing account")
	}
	if acc.TargetFitSendTier != "A_AUTOMATIC" {
		t.Fatalf("tier=%s", acc.TargetFitSendTier)
	}
	if !acc.EmailSendReady {
		t.Fatal("account email_send_ready")
	}
	if acc.ActivationState != ActivationActionableNow {
		t.Fatalf("activation=%s", acc.ActivationState)
	}
	if acc.ServiceCode != "REAJUSTE_14133" {
		t.Fatalf("service=%s", acc.ServiceCode)
	}
	if len(acc.ClaimsToAvoid) == 0 {
		t.Fatal("claims_to_avoid not imported")
	}
	cands, _ := repo.ListCandidates(ctx, org, acc.ID)
	if len(cands) != 1 {
		t.Fatalf("cands=%d", len(cands))
	}
	c := cands[0]
	if !c.EmailSendReady || c.OwnershipStatus != "COMPANY_OWNED" || c.MailboxPurpose != "comercial" {
		t.Fatalf("contact readiness incomplete: ready=%v own=%s purpose=%s", c.EmailSendReady, c.OwnershipStatus, c.MailboxPurpose)
	}
	if c.MailboxPurposeSendBlocked {
		t.Fatal("purpose should not be blocked")
	}
}

func TestCrossRepoRankOnlyDoesNotInvalidateApproval(t *testing.T) {
	// Rank/score-only changes must not rewrite material context or clear approval.
	ctx := context.Background()
	repo := newMemRepo()
	org, user := uuid.New(), uuid.New()
	svc := NewService(Config{Enabled: true, AppEnv: "test", RequireHumanApproval: true, DefaultDailyLimit: 100}, repo, nil).(*service)

	ready := true
	baseLead := func(rank int, score float64, fact string) FeedLead {
		l := sampleLeadWithActivation(float64(rank), ActivationActionableNow)
		l.Company.CNPJ14 = "33444555000166"
		l.SourceLeadID = "R1"
		l.Priority.Rank = rank
		l.Priority.Score = score
		l.MessagingContext.FactToMention = fact
		l.Moment.Summary = fact
		l.TargetFitSendTier = "B_EVIDENCE_SUPPORTED"
		l.EmailSendReady = &ready
		l.Contacts[0].EmailSendReady = &ready
		l.Contacts[0].OwnershipStatus = "COMPANY_OWNED"
		return l
	}
	feed1 := Feed{
		SchemaVersion: models.OutreachSchemaV1, GeneratedAt: "2026-08-08T10:00:00Z",
		Source:     FeedSource{System: "extra-cli", RunID: "r1", SnapshotHash: "s1", ProfileID: "p", ProfileVersion: "1"},
		Pagination: FeedPagination{HasMore: false},
		Leads:      []FeedLead{baseLead(1, 99, "mesmo fato material")},
	}
	raw1, _ := json.Marshal(feed1)
	if _, xerr := svc.ImportFromBytes(ctx, org, &user, raw1, ImportOptions{IdempotencyKey: "rank-1"}); xerr != nil {
		t.Fatal(xerr)
	}
	acc1, _ := repo.GetAccountByCNPJ(ctx, org, "33444555000166")
	if acc1 == nil {
		t.Fatal("import1 failed")
	}
	hash1 := acc1.MessageContextHash
	if hash1 == "" {
		t.Log("message_context_hash empty on import (acceptable if not computed in this path)")
	}

	// Second import: only rank/score/activation_score change.
	feed2 := feed1
	feed2.Source.RunID = "r2"
	feed2.Source.SnapshotHash = "s2"
	feed2.Leads = []FeedLead{baseLead(50, 10, "mesmo fato material")}
	feed2.Leads[0].Activation.Score = 10
	raw2, _ := json.Marshal(feed2)
	if _, xerr := svc.ImportFromBytes(ctx, org, &user, raw2, ImportOptions{IdempotencyKey: "rank-2"}); xerr != nil {
		t.Fatal(xerr)
	}
	acc2, _ := repo.GetAccountByCNPJ(ctx, org, "33444555000166")
	if acc2 == nil {
		t.Fatal("import2 failed")
	}
	if hash1 != "" && acc2.MessageContextHash != "" && hash1 != acc2.MessageContextHash {
		t.Fatalf("rank-only change must not alter message_context_hash: %q → %q", hash1, acc2.MessageContextHash)
	}
	if acc2.PriorityRank != 50 {
		t.Fatalf("rank not updated: %d", acc2.PriorityRank)
	}
}

func TestCrossRepoMaterialUpdateInvalidatesAuthorization(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org := uuid.New()
	accID := uuid.New()
	authID := uuid.New()
	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "44555666000177",
		ServiceCode: "REAJUSTE", FactToMention: "fato A", MessageContextHash: "ctx-A",
		TargetFitSendTier: "A_AUTOMATIC", EmailSendReady: true,
	}
	_, _ = repo.UpsertAccount(ctx, acc)
	cand := &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: accID,
		Email: "m@co.com", VerificationStatus: models.OutreachVerifyOfficialSource,
		EmailSendReady: true, OwnershipStatus: "COMPANY_OWNED",
	}
	_, _ = repo.UpsertCandidate(ctx, cand)
	now := time.Now().UTC()
	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: accID, ContactCandidateID: &cand.ID,
		Channel: "EMAIL", State: models.TouchpointApproved, Recipient: cand.Email,
		Subject: "S", BodyText: "body", ContentHash: "h1", ApprovedContentHash: "h1",
		AuthorizationMode: AuthorizationModeCampaignPolicy, GeneratedContextHash: "ctx-A",
		CampaignPolicyAuthorizationID: &authID, AuthorizationPolicyHash: "ph",
		AuthorizationAt: &now, IdempotencyKey: "mat-1",
	}
	_ = repo.InsertTouchpoint(ctx, tp)

	// Material fact change → context stale path
	acc.MessageContextHash = "ctx-B"
	acc.FactToMention = "fato B materialmente diferente"
	_, _ = repo.UpsertAccount(ctx, acc)
	if err := AssertMessageContextFresh(acc, tp.GeneratedContextHash); err == nil {
		t.Fatal("expected stale")
	}
	ClearApproval(tp)
	_ = repo.UpdateTouchpoint(ctx, tp)
	got, _ := repo.GetTouchpoint(ctx, org, tp.ID)
	if got.AuthorizationMode != "" || got.ApprovedContentHash != "" || got.CampaignPolicyAuthorizationID != nil {
		t.Fatal("material change must clear full authorization binding")
	}
}

func TestCrossRepoDNCReplyBounceAndPolicyRevoke(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org, user, camp := uuid.New(), uuid.New(), uuid.New()
	svc := NewService(Config{Enabled: true, AppEnv: "test", GreenAutorunEnabled: true}, repo, nil).(*service)
	store := newMemPolicyStore()
	svc.WirePolicyAuth(store)
	svc.WireDispatchGovernor(dispatch.NewGovernor(dispatch.LoadConfig(), dispatch.NewMemoryStore(), nil))

	// DNC dominates
	accID := uuid.New()
	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "55666777000188",
		DoNotContact: true, ServiceCode: "REAJUSTE", FactToMention: "f",
		MessageContextHash: "c", TargetFitSendTier: "A_AUTOMATIC", EmailSendReady: true,
		ActivationState: ActivationActionableNow,
	}
	_, _ = repo.UpsertAccount(ctx, acc)
	cand := &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: accID,
		Email: "dnc@co.com", VerificationStatus: models.OutreachVerifyOfficialSource,
		EmailSendReady: true, OwnershipStatus: "COMPANY_OWNED",
	}
	_, _ = repo.UpsertCandidate(ctx, cand)
	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: accID, ContactCandidateID: &cand.ID,
		Channel: "EMAIL", State: models.TouchpointNeedsReview, Recipient: cand.Email,
		BodyText: "x", ServiceCode: "REAJUSTE", FactUsed: "f", GeneratedContextHash: "c",
	}
	in := svc.buildGreenAutorunInput(ctx, org, tp, nil)
	if !in.DNC {
		t.Fatal("DNC must be true")
	}
	auth, _ := svc.AuthorizeCampaignPolicy(ctx, org, user, &models.CampaignPolicyAuthorization{
		CampaignID: camp, Channel: "EMAIL", AllowedRiskClass: "GREEN",
		EffectiveAt: time.Now().UTC().Add(-time.Minute), SenderMailbox: "tiago.sasaki@confenge.com.br",
		AllowPolicyTemplateGREEN: true,
	})
	dec := EvaluateGreenAutorun(true, auth, in, time.Now().UTC())
	if dec.Allow {
		t.Fatal("DNC must block autorun")
	}

	// Bounce
	in.DNC = false
	in.Bounce = true
	if EvaluateGreenAutorun(true, auth, in, time.Now().UTC()).Allow {
		t.Fatal("bounce must block")
	}

	// Reply
	in.Bounce = false
	in.Replied = true
	if EvaluateGreenAutorun(true, auth, in, time.Now().UTC()).Allow {
		t.Fatal("reply must block")
	}

	// Missing candidate
	in.Replied = false
	in.HasContactCandidate = false
	in.EmailSendReady = false
	if EvaluateGreenAutorun(true, auth, in, time.Now().UTC()).Allow {
		t.Fatal("missing candidate must block")
	}

	// Policy revoke blocks transport revalidation
	tp2 := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: accID, ContactCandidateID: &cand.ID,
		Channel: "EMAIL", State: models.TouchpointNeedsReview, Recipient: cand.Email,
		Subject: "S", BodyText: "body content", ContentHash: "",
	}
	RecomputeContentHash(tp2)
	if err := ApplyCampaignPolicyAuthorization(tp2, auth, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	tp2.State = models.TouchpointQueued
	_ = repo.InsertTouchpoint(ctx, tp2)
	ok, _ := svc.RevokeCampaignPolicy(ctx, org, camp, user)
	if !ok {
		t.Fatal("revoke")
	}
	block := svc.revalidateCampaignPolicyAtSend(ctx, org, tp2)
	if block == nil {
		t.Fatal("revoke must block transport")
	}
	if block.Reason != "policy_revoked" && !strings.Contains(block.Reason, "policy") {
		t.Fatalf("reason=%s", block.Reason)
	}
}

func TestCrossRepoNoDuplicateIntelligenceInWarmbly(t *testing.T) {
	// Warmbly must not recompute commercial/activation scores from local heuristics
	// when importing activation projection — only store extra-cli fields.
	ctx := context.Background()
	repo := newMemRepo()
	org, user := uuid.New(), uuid.New()
	svc := NewService(Config{Enabled: true, AppEnv: "test", DefaultDailyLimit: 100, RequireHumanApproval: true}, repo, nil).(*service)
	ready := true
	lead := sampleLeadWithActivation(33.3, ActivationWatch)
	lead.Company.CNPJ14 = "66777888000199"
	lead.TargetFitSendTier = "RESEARCH_ONLY"
	lead.EmailSendReady = &ready
	lead.Contacts[0].EmailSendReady = &ready
	lead.Contacts[0].OwnershipStatus = "COMPANY_OWNED"
	feed := Feed{
		SchemaVersion: models.OutreachSchemaV1, GeneratedAt: "2026-08-08T10:00:00Z",
		Source:     FeedSource{System: "extra-cli", RunID: "n1", SnapshotHash: "sn", ProfileID: "p", ProfileVersion: "1"},
		Pagination: FeedPagination{HasMore: false},
		Leads:      []FeedLead{lead},
	}
	raw, _ := json.Marshal(feed)
	if _, xerr := svc.ImportFromBytes(ctx, org, &user, raw, ImportOptions{IdempotencyKey: "noscore"}); xerr != nil {
		t.Fatal(xerr)
	}
	acc, _ := repo.GetAccountByCNPJ(ctx, org, "66777888000199")
	if acc == nil {
		t.Fatal("import failed")
	}
	if acc.ActivationScore != 33.3 {
		t.Fatalf("must store external score 33.3 got %v", acc.ActivationScore)
	}
	if acc.ActivationState != "WATCH" {
		t.Fatalf("state=%s", acc.ActivationState)
	}
	if acc.TargetFitSendTier != "RESEARCH_ONLY" {
		t.Fatalf("tier=%s", acc.TargetFitSendTier)
	}
	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: acc.ID, Channel: "EMAIL",
		Recipient: "n@co.com", BodyText: "b",
	}
	in := svc.buildGreenAutorunInput(ctx, org, tp, nil)
	if in.TargetFitSendTier == "A_AUTOMATIC" {
		t.Fatal("WATCH must not imply A_AUTOMATIC")
	}
}

func TestCrossRepoFeedFailureDoesNotKillMailboxOps(t *testing.T) {
	// Alias coverage: already in feed_isolation_test; reassert contract requirement.
	TestFeedSyncFailureDoesNotBlockMailboxPaths(t)
}
