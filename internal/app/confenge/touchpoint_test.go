package confenge

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func sampleTouch(channel, recipient, subject, body string) *models.OutreachTouchpoint {
	tp := &models.OutreachTouchpoint{
		ID: uuid.New(), OrganizationID: uuid.New(), AccountID: uuid.New(), Ordinal: 1,
		Channel: channel, Purpose: models.TouchpointPurposeInitial, State: models.TouchpointNeedsReview,
		Recipient: recipient, Subject: subject, BodyText: body,
	}
	RecomputeContentHash(tp)
	return tp
}

func TestSendWithoutApprovalBlocked(t *testing.T) {
	tp := sampleTouch(models.OutreachChannelEmail, "lead@example.com", "Hi", "Hello")
	if err := CanTransport(tp); err == nil {
		t.Fatal("unapproved must block")
	}
	uid := uuid.New()
	tp.ApprovedBy = &uid
	tp.ApprovedContentHash = "deadbeef"
	tp.State = models.TouchpointApproved
	if err := CanTransport(tp); err == nil {
		t.Fatal("hash mismatch must block")
	}
}

func TestEditAfterApproveInvalidates(t *testing.T) {
	tp := sampleTouch(models.OutreachChannelEmail, "lead@example.com", "Hi", "Hello")
	if err := ApplyHumanApproval(tp, uuid.New(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	ApplyContentMutation(tp, tp.Channel, tp.Recipient, "Hi", "Edited")
	if tp.ApprovedBy != nil || CanTransport(tp) == nil {
		t.Fatal("edit must invalidate")
	}
}

func TestEveryMaterialContentMutationInvalidatesApproval(t *testing.T) {
	tests := []struct {
		name      string
		channel   string
		recipient string
		subject   string
		body      string
	}{
		{name: "subject", channel: models.OutreachChannelEmail, recipient: "lead@empresa.com.br", subject: "Novo assunto", body: "Corpo original"},
		{name: "body", channel: models.OutreachChannelEmail, recipient: "lead@empresa.com.br", subject: "Assunto original", body: "Novo corpo"},
		{name: "recipient", channel: models.OutreachChannelEmail, recipient: "outro@empresa.com.br", subject: "Assunto original", body: "Corpo original"},
		{name: "channel", channel: models.OutreachChannelWhatsApp, recipient: "+5511999999999", subject: "Assunto original", body: "Corpo original"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			touchpoint := sampleTouch(models.OutreachChannelEmail, "lead@empresa.com.br", "Assunto original", "Corpo original")
			if err := ApplyHumanApproval(touchpoint, uuid.New(), time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			ApplyContentMutation(touchpoint, test.channel, test.recipient, test.subject, test.body)
			if touchpoint.ApprovedBy != nil || touchpoint.ApprovedAt != nil || touchpoint.ApprovedContentHash != "" || touchpoint.State != models.TouchpointNeedsReview {
				t.Fatalf("approval survived %s mutation: %+v", test.name, touchpoint)
			}
		})
	}
}

func TestUpstreamContextInvalidationRevokesApprovalAndEnrollment(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	orgID, accountID, candidateID := uuid.New(), uuid.New(), uuid.New()
	draft := &models.OutreachDraft{
		ID: uuid.New(), OrganizationID: orgID, AccountID: accountID,
		ContactCandidateID: &candidateID, Status: models.OutreachDraftEnrolled,
	}
	campaignID, contactID := uuid.New(), uuid.New()
	draft.CampaignID, draft.EnrollmentContactID = &campaignID, &contactID
	_ = repo.UpsertDraft(ctx, draft)
	approver := uuid.New()
	touchpoint := &models.OutreachTouchpoint{
		OrganizationID: orgID, AccountID: accountID, ContactCandidateID: &candidateID,
		DraftID: &draft.ID, State: models.TouchpointQueued, Channel: models.OutreachChannelEmail,
		ContentHash: "content", ApprovedContentHash: "content", ApprovedBy: &approver,
		GeneratedContextHash: "old-context",
	}
	_ = repo.InsertTouchpoint(ctx, touchpoint)
	if err := repo.InvalidateAccountApprovalsForContext(ctx, orgID, accountID, "new-context"); err != nil {
		t.Fatal(err)
	}
	gotTouchpoint, _ := repo.GetTouchpoint(ctx, orgID, touchpoint.ID)
	gotDraft, _ := repo.GetDraft(ctx, orgID, draft.ID)
	if gotTouchpoint.State != models.TouchpointNeedsReview || gotTouchpoint.ApprovedBy != nil || gotTouchpoint.ApprovedContentHash != "" {
		t.Fatalf("touchpoint approval not invalidated: %+v", gotTouchpoint)
	}
	if gotDraft.Status != models.OutreachDraftNeedsReview || gotDraft.CampaignID != nil || gotDraft.EnrollmentContactID != nil {
		t.Fatalf("draft enrollment not invalidated: %+v", gotDraft)
	}
}

func TestAINeverWritesApprovedBy(t *testing.T) {
	tp := sampleTouch(models.OutreachChannelEmail, "a@b.com", "", "")
	ApplyContentMutation(tp, models.OutreachChannelEmail, "a@b.com", "S", "body")
	if tp.ApprovedBy != nil {
		t.Fatal("AI must not approve")
	}
	if ApplyHumanApproval(tp, uuid.Nil, time.Now().UTC()) == nil {
		t.Fatal("nil user")
	}
}

func TestConcurrentCASQueueSingleWinner(t *testing.T) {
	repo := newMemRepo()
	org, human := uuid.New(), uuid.New()
	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: uuid.New(), Ordinal: 1, Channel: models.OutreachChannelEmail,
		Purpose: models.TouchpointPurposeInitial, State: models.TouchpointNeedsReview,
		Recipient: "l@e.com", Subject: "S", BodyText: "B", IdempotencyKey: "t1",
	}
	RecomputeContentHash(tp)
	_ = ApplyHumanApproval(tp, human, time.Now().UTC())
	_ = repo.InsertTouchpoint(context.Background(), tp)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := repo.CASQueueTouchpoint(context.Background(), org, tp.ID, tp.ContentHash)
			if err != nil {
				t.Errorf("%v", err)
				return
			}
			if out != nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("wins=%d", wins.Load())
	}
}

func TestReplyAndDNCCancelFutures(t *testing.T) {
	for _, mode := range []string{"reply", "dnc"} {
		repo := newMemRepo()
		svc := testSvc(repo).(*service)
		org, user := uuid.New(), uuid.New()
		acc := &models.OutreachAccount{ID: uuid.New(), OrganizationID: org, CNPJ14: "12345678000199", RazaoSocial: "A", QueueState: models.OutreachQueueEnrolled, SourceLeadID: "L"}
		_, _ = repo.UpsertAccount(context.Background(), acc)
		email := mode + "@example.com"
		cand := &models.OutreachContactCandidate{ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, Email: email, Name: "N", VerificationStatus: models.OutreachVerifyOfficialSource}
		_, _ = repo.UpsertCandidate(context.Background(), cand)
		if _, x := svc.PlanAccountCadence(context.Background(), org, user, acc.ID, &cand.ID, models.OutreachChannelEmail); x != nil {
			t.Fatal(mode, x)
		}
		if mode == "reply" {
			_ = svc.NoteReply(context.Background(), org, email, nil)
		} else {
			_ = svc.NoteDNC(context.Background(), org, email, "opt")
		}
		list, _ := repo.ListTouchpoints(context.Background(), org, acc.ID, "", 50, 0)
		for _, tp := range list {
			if models.TouchpointOpenStates[tp.State] {
				t.Fatalf("%s still open %s", mode, tp.State)
			}
		}
	}
}

func TestEmailAndWhatsAppShareApprovalGate(t *testing.T) {
	for _, ch := range []string{models.OutreachChannelEmail, models.OutreachChannelWhatsApp} {
		rec := "a@b.com"
		if ch == models.OutreachChannelWhatsApp {
			rec = "+5511999999999"
		}
		tp := sampleTouch(ch, rec, "S", "B")
		if CanTransport(tp) == nil {
			t.Fatal(ch)
		}
		_ = ApplyHumanApproval(tp, uuid.New(), time.Now().UTC())
		if CanTransport(tp) != nil {
			t.Fatal(ch, "approved")
		}
	}
}

func TestReimportDoesNotReactivateSentOrDNC(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	acc := &models.OutreachAccount{ID: uuid.New(), OrganizationID: org, CNPJ14: "12345678000177", RazaoSocial: "G", QueueState: models.OutreachQueueSent, SourceLeadID: "L3"}
	_, _ = repo.UpsertAccount(context.Background(), acc)
	sent := &models.OutreachTouchpoint{OrganizationID: org, AccountID: acc.ID, Ordinal: 1, Channel: models.OutreachChannelEmail, State: models.TouchpointSent, Recipient: "x@y.com", BodyText: "s", IdempotencyKey: "s1"}
	_ = repo.InsertTouchpoint(context.Background(), sent)
	if _, x := svc.PlanAccountCadence(context.Background(), org, user, acc.ID, nil, models.OutreachChannelEmail); x == nil {
		t.Fatal("SENT replan")
	}
	acc2 := &models.OutreachAccount{ID: uuid.New(), OrganizationID: org, CNPJ14: "12345678000166", RazaoSocial: "D", DoNotContact: true, QueueState: models.OutreachQueueDoNotContact, SourceLeadID: "L4"}
	_, _ = repo.UpsertAccount(context.Background(), acc2)
	dnc := &models.OutreachTouchpoint{OrganizationID: org, AccountID: acc2.ID, Ordinal: 1, State: models.TouchpointDNC, Channel: models.OutreachChannelEmail, IdempotencyKey: "d1"}
	_ = repo.InsertTouchpoint(context.Background(), dnc)
	if _, x := svc.PlanAccountCadence(context.Background(), org, user, acc2.ID, nil, models.OutreachChannelEmail); x == nil {
		t.Fatal("DNC replan")
	}
	acc2.DoNotContact = false
	_, _ = repo.UpsertAccount(context.Background(), acc2)
	got, _ := repo.GetAccount(context.Background(), org, acc2.ID)
	if !got.DoNotContact {
		t.Fatal("preserve DNC")
	}
}

func TestRestartPreservesStates(t *testing.T) {
	repo := newMemRepo()
	svc1 := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	acc := &models.OutreachAccount{ID: uuid.New(), OrganizationID: org, CNPJ14: "12345678000155", RazaoSocial: "E", QueueState: models.OutreachQueueReadyToGenerate, SourceLeadID: "L5", ServiceCode: "ADITIVOS", FactToMention: "termo aditivo 1 ao contrato 88/2021 publicado", MomentEvidenceIDs: []string{"ev-1"}}
	_, _ = repo.UpsertAccount(context.Background(), acc)
	cand := &models.OutreachContactCandidate{ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, Email: "k@e.com", Name: "C", VerificationStatus: models.OutreachVerifyOfficialSource}
	applyValidatedIdentity(cand)
	_, _ = repo.UpsertCandidate(context.Background(), cand)
	_, _ = repo.UpsertEvidence(context.Background(), &models.OutreachEvidence{
		OrganizationID: org, AccountID: acc.ID, SourceEvidenceID: "ev-1",
		Synthesis: acc.FactToMention, EpistemicClass: models.OutreachEpistemicConfirmedFact,
	})
	list, x := svc1.PlanAccountCadence(context.Background(), org, user, acc.ID, &cand.ID, models.OutreachChannelEmail)
	if x != nil {
		t.Fatal(x)
	}
	_, x = svc1.GenerateTouchpointDraft(context.Background(), org, user, list[0].ID)
	if x != nil {
		t.Fatal(x)
	}
	svc2 := testSvc(repo).(*service)
	r, x := svc2.GetTouchpoint(context.Background(), org, list[0].ID)
	if x != nil || r.State != models.TouchpointNeedsReview || r.BodyText == "" {
		t.Fatal("restart")
	}
}

func TestQueueWithoutApprovalServiceBlocked(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	acc := &models.OutreachAccount{ID: uuid.New(), OrganizationID: org, CNPJ14: "12345678000144", RazaoSocial: "Z", QueueState: models.OutreachQueueReadyToGenerate, SourceLeadID: "L6", ServiceCode: "ADITIVOS", FactToMention: "termo aditivo 1 ao contrato 88/2021 publicado", MomentEvidenceIDs: []string{"ev-1"}}
	_, _ = repo.UpsertAccount(context.Background(), acc)
	cand := &models.OutreachContactCandidate{ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, Email: "q@e.com", Name: "Q", VerificationStatus: models.OutreachVerifyOfficialSource}
	_, _ = repo.UpsertCandidate(context.Background(), cand)
	list, _ := svc.PlanAccountCadence(context.Background(), org, user, acc.ID, &cand.ID, models.OutreachChannelEmail)
	tp, _ := svc.GenerateTouchpointDraft(context.Background(), org, user, list[0].ID)
	if _, x := svc.QueueTouchpoint(context.Background(), org, user, tp.ID); x == nil {
		t.Fatal("must block")
	}
}

func TestQueueKillSwitchPreservesApprovedState(t *testing.T) {
	killPath := filepath.Join(t.TempDir(), "kill")
	t.Setenv(EnvKillSwitchPath, killPath)
	if err := EngageKillSwitch(); err != nil {
		t.Fatal(err)
	}
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: uuid.New(), Ordinal: 1,
		Channel: models.OutreachChannelEmail, State: models.TouchpointApproved,
		Recipient: "approved@company.test", Subject: "Approved", BodyText: "Approved body",
		IdempotencyKey: "kill-preserves-approval",
	}
	RecomputeContentHash(tp)
	tp.ApprovedContentHash = tp.ContentHash
	if err := repo.InsertTouchpoint(context.Background(), tp); err != nil {
		t.Fatal(err)
	}
	if _, xerr := svc.QueueTouchpoint(context.Background(), org, user, tp.ID); xerr == nil {
		t.Fatal("kill switch must block queue")
	}
	stored, err := repo.GetTouchpoint(context.Background(), org, tp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != models.TouchpointApproved || stored.ApprovedContentHash != tp.ContentHash {
		t.Fatalf("paused queue mutated approval: state=%s approved_hash=%q", stored.State, stored.ApprovedContentHash)
	}
}

func TestEnrollDraftBlockedWithoutTouchApproval(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	acc := &models.OutreachAccount{ID: uuid.New(), OrganizationID: org, CNPJ14: "12345678000122", RazaoSocial: "G", QueueState: models.OutreachQueueApproved, SourceLeadID: "L8"}
	_, _ = repo.UpsertAccount(context.Background(), acc)
	cand := &models.OutreachContactCandidate{ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, Email: "g@e.com", Name: "G", VerificationStatus: models.OutreachVerifyOfficialSource}
	_, _ = repo.UpsertCandidate(context.Background(), cand)
	draft := &models.OutreachDraft{ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, ContactCandidateID: &cand.ID, Channel: models.OutreachChannelEmail, RecipientEmail: cand.Email, Subject: "S", BodyText: "B", Status: models.OutreachDraftApproved}
	_ = repo.UpsertDraft(context.Background(), draft)
	tp := &models.OutreachTouchpoint{OrganizationID: org, AccountID: acc.ID, Ordinal: 1, Channel: models.OutreachChannelEmail, State: models.TouchpointNeedsReview, Recipient: cand.Email, Subject: "S", BodyText: "B", DraftID: &draft.ID, IdempotencyKey: "g"}
	RecomputeContentHash(tp)
	_ = repo.InsertTouchpoint(context.Background(), tp)
	if _, x := svc.EnrollDraft(context.Background(), org, user, draft.ID); x == nil {
		t.Fatal("must block enroll")
	}
}

func TestCadencePolicyV1NoFalseUrgency(t *testing.T) {
	steps := CadencePolicyV1()
	if len(steps) < 2 || steps[0].DelayAfterPrev != 0 {
		t.Fatal("shape")
	}
	for i := 1; i < len(steps); i++ {
		if steps[i].DelayAfterPrev < 24*time.Hour {
			t.Fatal("aggressive")
		}
	}
}

func TestDraftOnlyEnrollBlocked(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	svc.WireExecution(&mockCampaigns{}, &mockContacts{})
	org, user := uuid.New(), uuid.New()
	acc := &models.OutreachAccount{ID: uuid.New(), OrganizationID: org, CNPJ14: "11222333000181", RazaoSocial: "A", QueueState: models.OutreachQueueApproved, SourceLeadID: "L"}
	_, _ = repo.UpsertAccount(context.Background(), acc)
	cand := &models.OutreachContactCandidate{ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, Email: "a@b.com", Name: "A", VerificationStatus: models.OutreachVerifyOfficialSource}
	_, _ = repo.UpsertCandidate(context.Background(), cand)
	d := &models.OutreachDraft{ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, ContactCandidateID: &cand.ID, Channel: models.OutreachChannelEmail, RecipientEmail: cand.Email, Subject: "S", BodyText: "B", Status: models.OutreachDraftApproved, RiskClass: "GREEN", FactUsed: "f", ServiceCode: "S", VerificationStatus: models.OutreachVerifyOfficialSource}
	ok := true
	d.ValidationOK = &ok
	_ = repo.UpsertDraft(context.Background(), d)
	if _, xerr := svc.EnrollDraft(context.Background(), org, user, d.ID); xerr == nil {
		t.Fatal("draft-only enroll must fail")
	}
}

func TestPromoteDuePlannedAfterPriorSent(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	acc := &models.OutreachAccount{ID: uuid.New(), OrganizationID: org, CNPJ14: "11222333000199", RazaoSocial: "B", QueueState: models.OutreachQueueReadyToGenerate, SourceLeadID: "L2"}
	_, _ = repo.UpsertAccount(context.Background(), acc)
	cand := &models.OutreachContactCandidate{ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, Email: "b@b.com", Name: "B", VerificationStatus: models.OutreachVerifyOfficialSource}
	_, _ = repo.UpsertCandidate(context.Background(), cand)
	list, xerr := svc.PlanAccountCadence(context.Background(), org, user, acc.ID, &cand.ID, models.OutreachChannelEmail)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if len(list) < 2 {
		t.Fatal("need follow-ups")
	}
	// Complete first touch
	first, _ := repo.GetTouchpoint(context.Background(), org, list[0].ID)
	first.State = models.TouchpointSent
	now := time.Now().UTC()
	first.SentAt = &now
	_ = repo.UpdateTouchpoint(context.Background(), first)
	// Next is PLANNED with future due_at — force due_at into the past to simulate scheduler
	second, _ := repo.GetTouchpoint(context.Background(), org, list[1].ID)
	if second.State != models.TouchpointPlanned {
		t.Fatalf("want PLANNED got %s", second.State)
	}
	second.DueAt = now.Add(-time.Hour)
	_ = repo.UpdateTouchpoint(context.Background(), second)
	// ListReview promotes due planned into queue
	review, xerr := svc.ListReviewTouchpoints(context.Background(), org, 50, 0)
	if xerr != nil {
		t.Fatal(xerr)
	}
	found := false
	for _, tp := range review {
		if tp.ID == second.ID && tp.State == models.TouchpointDue {
			found = true
		}
	}
	if !found {
		// also check reload
		re, _ := repo.GetTouchpoint(context.Background(), org, second.ID)
		if re == nil || re.State != models.TouchpointDue {
			t.Fatalf("expected DUE after promote, got %+v review=%d", re, len(review))
		}
	}
}

func TestRequireTouchTransportNilDraftBlocks(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org := uuid.New()
	if _, xerr := svc.requireTouchTransport(context.Background(), org, uuid.New()); xerr == nil {
		t.Fatal("missing touchpoint must block")
	}
}

func TestPromoteDueDoesNotReleaseWhilePriorOpen(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	org, user := uuid.New(), uuid.New()
	acc := &models.OutreachAccount{ID: uuid.New(), OrganizationID: org, CNPJ14: "11222333000200", RazaoSocial: "C", QueueState: models.OutreachQueueReadyToGenerate, SourceLeadID: "L3"}
	_, _ = repo.UpsertAccount(context.Background(), acc)
	cand := &models.OutreachContactCandidate{ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, Email: "c@b.com", Name: "C", VerificationStatus: models.OutreachVerifyOfficialSource}
	_, _ = repo.UpsertCandidate(context.Background(), cand)
	list, xerr := svc.PlanAccountCadence(context.Background(), org, user, acc.ID, &cand.ID, models.OutreachChannelEmail)
	if xerr != nil {
		t.Fatal(xerr)
	}
	// First still open (DUE / will be needs review)
	first, _ := repo.GetTouchpoint(context.Background(), org, list[0].ID)
	first.State = models.TouchpointNeedsReview
	first.BodyText = "open prior"
	RecomputeContentHash(first)
	_ = repo.UpdateTouchpoint(context.Background(), first)
	// Second due_at elapsed but prior open
	second, _ := repo.GetTouchpoint(context.Background(), org, list[1].ID)
	second.DueAt = time.Now().UTC().Add(-time.Hour)
	_ = repo.UpdateTouchpoint(context.Background(), second)

	_, _ = svc.ListReviewTouchpoints(context.Background(), org, 50, 0)
	re, _ := repo.GetTouchpoint(context.Background(), org, second.ID)
	if re.State != models.TouchpointPlanned {
		t.Fatalf("must stay PLANNED while prior open, got %s", re.State)
	}
	// Approve/queue next must also refuse
	if _, xerr := svc.ApproveTouchpoint(context.Background(), org, user, second.ID, ApprovalOptions{}); xerr == nil {
		t.Fatal("approve next while prior open must fail")
	}
}

func TestDraftEditAfterTouchApproveBlocksTransport(t *testing.T) {
	repo := newMemRepo()
	svc := testSvc(repo).(*service)
	svc.WireExecution(&mockCampaigns{}, &mockContacts{})
	org, user := uuid.New(), uuid.New()
	acc := &models.OutreachAccount{ID: uuid.New(), OrganizationID: org, CNPJ14: "11222333000211", RazaoSocial: "D", NomeFantasia: "D", QueueState: models.OutreachQueueApproved, SourceLeadID: "L4", FactToMention: "fato", ServiceCode: "S"}
	_, _ = repo.UpsertAccount(context.Background(), acc)
	cand := &models.OutreachContactCandidate{ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, Email: "d@b.com", Name: "D", VerificationStatus: models.OutreachVerifyOfficialSource}
	_, _ = repo.UpsertCandidate(context.Background(), cand)
	d := &models.OutreachDraft{
		ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, ContactCandidateID: &cand.ID,
		Channel: models.OutreachChannelEmail, RecipientEmail: cand.Email,
		Subject: "S", BodyText: "approved body text", Status: models.OutreachDraftApproved,
		FactUsed: "fato", ServiceCode: "S", RiskClass: "GREEN", VerificationStatus: models.OutreachVerifyOfficialSource,
	}
	ok := true
	d.ValidationOK = &ok
	_ = repo.UpsertDraft(context.Background(), d)

	now := time.Now().UTC()
	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: acc.ID, Ordinal: 1, Channel: models.OutreachChannelEmail,
		Purpose: models.TouchpointPurposeInitial, State: models.TouchpointNeedsReview,
		Recipient: cand.Email, Subject: d.Subject, BodyText: d.BodyText, DraftID: &d.ID, IdempotencyKey: "div-tp",
	}
	RecomputeContentHash(tp)
	if err := ApplyHumanApproval(tp, user, now); err != nil {
		t.Fatal(err)
	}
	_ = repo.InsertTouchpoint(context.Background(), tp)

	// Mutate draft after touchpoint approval (legacy ReviewDraft path)
	newBody := "sneaky rewritten body after approve"
	if _, xerr := svc.ReviewDraft(context.Background(), org, user, d.ID, "edit", &DraftEdit{BodyText: &newBody}); xerr != nil {
		t.Fatal(xerr)
	}
	// Re-mark draft approved for enroll path (edit sets NEEDS_REVIEW)
	d2, _ := repo.GetDraft(context.Background(), org, d.ID)
	d2.Status = models.OutreachDraftApproved
	_ = repo.UpsertDraft(context.Background(), d2)

	if _, xerr := svc.EnrollDraft(context.Background(), org, user, d.ID); xerr == nil {
		t.Fatal("enroll after draft divergence must block")
	}
	// Touchpoint approval should have been cleared by edit
	tp2, _ := repo.GetTouchpoint(context.Background(), org, tp.ID)
	if tp2.ApprovedContentHash != "" || tp2.ApprovedBy != nil {
		t.Fatalf("edit must clear touchpoint approval: %+v", tp2)
	}
}

func TestPriorReleasedHelper(t *testing.T) {
	a := uuid.New()
	priors := []models.OutreachTouchpoint{
		{AccountID: a, Ordinal: 1, State: models.TouchpointNeedsReview},
		{AccountID: a, Ordinal: 2, State: models.TouchpointPlanned},
	}
	if PriorReleased(priors, 2) {
		t.Fatal("open prior must block")
	}
	priors[0].State = models.TouchpointSent
	if !PriorReleased(priors, 2) {
		t.Fatal("sent prior must release")
	}
	priors[0].State = models.TouchpointApproved
	if PriorReleased(priors, 2) {
		t.Fatal("approved prior still open for next")
	}
}
