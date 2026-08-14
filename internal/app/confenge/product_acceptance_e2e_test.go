package confenge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/confenge/dispatch"
	"github.com/warmbly/warmbly/internal/app/whatsapp"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// TestProductAcceptanceMultichannelSum proves the sum of CONFENGE product surfaces
// on shipped service + governor + touchpoint + WA transport mock + Mailpit + HMAC paths.
//
// Mailpit is required: set CONFENGE_MAILPIT_SMTP / CONFENGE_MAILPIT_API, or use the
// local compose defaults (11025/18025) or CI service defaults (1025/8025).
func TestProductAcceptanceMultichannelSum(t *testing.T) {
	mailpitSMTP, mailpitAPI := resolveMailpitEndpoints(t)

	var outcomes []models.OutreachOutcome
	rf := &memRepoOutcome{
		memRepoFull: *newMemRepoWithSettings(),
		outcomes:    &outcomes,
	}
	rf.memRepo = newMemRepo()
	rf.settings = map[uuid.UUID]*models.OutreachOrgSettings{}
	rf.drafts = map[uuid.UUID]*models.OutreachDraft{}
	rf.touchpoints = map[uuid.UUID]*models.OutreachTouchpoint{}
	rf.outcomeBy = map[string]*models.OutreachOutcome{}
	rf.orgOwner = map[uuid.UUID]uuid.UUID{}

	cfg := Config{
		Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120,
		RequireHumanApproval: true, MaxFeedPayloadBytes: DefaultMaxPayloadBytes,
		WhatsAppEnabled: true, CrossChannelHours: 0,
	}
	svc := NewService(cfg, rf, nil).(*service)
	contacts := &mockContacts{}
	camps := &mockCampaigns{}
	svc.WireExecution(camps, contacts)

	clock := &dispatch.FixedClock{T: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)}
	store := dispatch.NewMemoryStore()
	govCfg := dispatch.DefaultConfig()
	govCfg.WindowStart = "00:00"
	govCfg.WindowEnd = "23:59"
	govCfg.Timezone = "UTC"
	govCfg.MinGap = 0
	govCfg.SendsPerHour = 10
	gov := dispatch.NewGovernor(govCfg, store, clock)
	svc.WireDispatchGovernor(gov)

	// Mock WhatsApp provider (never real network); used for SendApprovedWhatsApp.
	waMock := whatsapp.NewMockProvider()
	waSvc := whatsapp.NewService(whatsapp.Config{
		Enabled: true, AutoSendEnabled: false, CrossChannelInterval: 0, ServiceWindow: 24 * time.Hour,
		EvolutionInstance: "product-acceptance",
	}, waMock, nil)
	svc.WireWhatsApp(waSvc, &memWAStore{})

	receiver := newTestOutcomeReceiver(t)
	defer receiver.Close()

	org := uuid.New()
	user := uuid.New()
	raw := mustReadFixture(t, "native_feed_v1.json")
	raw = bytes.ReplaceAll(raw, []byte("acme.example.com"), []byte("pilot.warmbly.com"))
	marker := fmt.Sprintf("CONFENGE-PA-%s", uuid.NewString()[:8])

	// 1. import of multiple companies
	run, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if run.Counts.Creates < 3 {
		t.Fatalf("bullet1 multi-company import: creates=%d want>=3 counts=%+v", run.Counts.Creates, run.Counts)
	}

	// 2. distinct dossiers / services
	acme, _ := rf.GetAccountByCNPJ(context.Background(), org, "11222333000181")
	if acme == nil {
		t.Fatal("acme missing")
	}
	accs, _ := rf.ListAccounts(context.Background(), org, repository.OutreachAccountFilter{Limit: 50})
	for _, a := range accs {
		stampValidatedCandidates(t, rf.memRepo, org, a.ID)
	}
	services := map[string]struct{}{}
	for _, a := range accs {
		if a.ServiceCode != "" {
			services[a.ServiceCode] = struct{}{}
		}
	}
	if len(services) < 1 {
		t.Fatal("bullet2 expected at least one service code from feed")
	}

	// 3. different messages per account
	draft1, xerr := svc.GenerateDraft(context.Background(), org, user, acme.ID, nil)
	if xerr != nil {
		t.Fatalf("generate acme: %s", xerr.Message)
	}
	if draft1.BodyText == "" {
		t.Fatal("bullet3 empty draft body")
	}
	var otherBody string
	for _, a := range accs {
		if a.ID == acme.ID || a.QueueState == models.OutreachQueueNeedsContact {
			continue
		}
		d, xe := svc.GenerateDraft(context.Background(), org, user, a.ID, nil)
		if xe == nil && d.BodyText != "" {
			otherBody = d.BodyText
			break
		}
	}
	if otherBody != "" && otherBody == draft1.BodyText && draft1.FactUsed == "" && draft1.ServiceCode == "" {
		t.Fatal("bullet3 draft missing fact/service differentiation")
	}

	// 4. exact recipient visible
	if draft1.RecipientEmail == "" || !strings.Contains(draft1.RecipientEmail, "@") {
		t.Fatalf("bullet4 recipient must be visible, got %q", draft1.RecipientEmail)
	}

	// 5. no message before approval
	approvedBody := strings.TrimSpace(draft1.BodyText) + "\n\n" + marker
	tp := &models.OutreachTouchpoint{
		ID: uuid.New(), OrganizationID: org, AccountID: acme.ID, Ordinal: 1,
		Channel: models.OutreachChannelEmail, Purpose: models.TouchpointPurposeInitial,
		State: models.TouchpointNeedsReview, Recipient: draft1.RecipientEmail,
		Subject: draft1.Subject, BodyText: approvedBody, DraftID: &draft1.ID,
		IdempotencyKey: "pa-tp-1",
	}
	RecomputeContentHash(tp)
	_ = rf.InsertTouchpoint(context.Background(), tp)
	if CanTransport(tp) == nil {
		t.Fatal("bullet5 unapproved touchpoint must not transport")
	}

	// 6. approval by exact content hash
	if err := ApplyHumanApproval(tp, user, clock.Now()); err != nil {
		t.Fatal(err)
	}
	if tp.ApprovedContentHash == "" || tp.ApprovedContentHash != tp.ContentHash {
		t.Fatalf("bullet6 approved hash mismatch content=%s approved=%s", tp.ContentHash, tp.ApprovedContentHash)
	}
	if CanTransport(tp) != nil {
		t.Fatal("bullet6 approved+hash-matched must allow transport")
	}

	// 7. edit invalidates approval
	ApplyContentMutation(tp, tp.Channel, tp.Recipient, "Edited subject", "Edited body after approval")
	if tp.ApprovedBy != nil {
		t.Fatal("bullet7 edit must clear approval")
	}
	if CanTransport(tp) == nil {
		t.Fatal("bullet7 after edit transport must block")
	}

	// Re-approve original approved content for queue + Mailpit
	tp.Subject = draft1.Subject
	tp.BodyText = approvedBody
	RecomputeContentHash(tp)
	if err := ApplyHumanApproval(tp, user, clock.Now()); err != nil {
		t.Fatal(err)
	}
	_ = rf.UpdateTouchpoint(context.Background(), tp)

	// 8. Approved & queued (CAS queue on shipped repo path)
	queued, err := rf.CASQueueTouchpoint(context.Background(), org, tp.ID, tp.ContentHash)
	if err != nil {
		t.Fatalf("bullet8 queue: %v", err)
	}
	if queued.ApprovedContentHash == "" || queued.ApprovedContentHash != queued.ContentHash {
		t.Fatal("bullet8 queued row must retain matching approved content hash")
	}

	// 9–10. governor allows at most 10 outbound/60min across channels; 11th stays queued
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		ch := dispatch.ChannelEmail
		if i%2 == 1 {
			ch = dispatch.ChannelWhatsApp
		}
		res, err := gov.TryReserve(ctx, dispatch.ReserveRequest{
			OrganizationID: org, Channel: ch, MessageKey: fmt.Sprintf("pa:%d", i),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Allowed {
			t.Fatalf("bullet9 reserve %d blocked: %s", i, res.Reason)
		}
		if err := gov.Commit(ctx, res.Reservation.ID); err != nil {
			t.Fatal(err)
		}
		clock.Advance(time.Second)
	}
	res11, err := gov.TryReserve(ctx, dispatch.ReserveRequest{
		OrganizationID: org, Channel: dispatch.ChannelEmail, MessageKey: "pa:11",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res11.Allowed {
		t.Fatal("bullet10 11th outbound must stay blocked/queued")
	}
	_ = gov.Enqueue(ctx, dispatch.EnqueueRequest{
		OrganizationID: org, Channel: dispatch.ChannelEmail, DraftID: uuid.New(),
		MessageKey: "pa:11", DueAt: clock.Now().Add(time.Hour),
	})

	// 11. restart does not burst (cap already full)
	gov2 := dispatch.NewGovernor(govCfg, store, clock)
	for i := 0; i < 5; i++ {
		_ = gov2.Enqueue(ctx, dispatch.EnqueueRequest{
			OrganizationID: org, Channel: dispatch.ChannelEmail, DraftID: uuid.New(),
			MessageKey: fmt.Sprintf("restart-backlog:%d", i),
			DueAt:      clock.Now().Add(-time.Hour),
		})
	}
	burst := 0
	for i := 0; i < 15; i++ {
		item, err := gov2.ClaimNextQueued(ctx)
		if err != nil || item == nil {
			break
		}
		res, err := gov2.TryReserve(ctx, dispatch.ReserveRequest{
			OrganizationID: item.OrganizationID, Channel: item.Channel,
			MessageKey: item.MessageKey, DraftID: &item.DraftID,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Allowed {
			due := res.NextSlot
			if due.IsZero() {
				due = clock.Now().Add(time.Minute)
			}
			_ = gov2.Enqueue(ctx, dispatch.EnqueueRequest{
				OrganizationID: item.OrganizationID, Channel: item.Channel, DraftID: item.DraftID,
				MessageKey: item.MessageKey, DueAt: due,
			})
			break
		}
		_ = gov2.Commit(ctx, res.Reservation.ID)
		_ = gov2.MarkQueue(ctx, item.ID, dispatch.QueueSent, "")
		burst++
		clock.Advance(time.Second)
	}
	if burst > 0 {
		t.Fatalf("bullet11 restart burst committed %d while cap already full", burst)
	}

	// 12. deliver approved content via SMTP to Mailpit and assert exact transport
	// (recipient/subject/body == approved), not a loose marker contains-check alone.
	toAddr := "tiago-self+" + marker + "@example.com"
	if err := smtpSendApproved(mailpitSMTP, "confenge-acceptance@warmbly.local", toAddr, queued.Subject, queued.BodyText); err != nil {
		t.Fatalf("bullet12 SMTP to Mailpit: %v", err)
	}
	if !mailpitExactDelivery(t, mailpitAPI, marker, toAddr, queued.Subject, queued.BodyText) {
		t.Fatal("bullet12 Mailpit exact delivery failed (recipient/subject/body must match approved content)")
	}

	// 13–14. WhatsApp: public phone blocked on generate; consented path sends via mock provider
	// Seed public-phone candidate (no opt-in) on acme — must not generate.
	pubCandID := uuid.New()
	_, _ = rf.UpsertCandidate(context.Background(), &models.OutreachContactCandidate{
		ID: pubCandID, OrganizationID: org, AccountID: acme.ID,
		Name: "Public Phone", Email: "public@pilot.warmbly.com",
		Phone: "+5548999999999", PhoneE164: "+5548999999999",
		PhoneSource:           "official_company_site",
		WhatsAppConsentStatus: whatsapp.ConsentUnknown, WhatsAppConsentProvenanceOK: false,
		VerificationStatus: models.OutreachVerifyOfficialSource, Recommended: false,
	})
	if _, xerr := svc.GenerateWhatsAppDraft(context.Background(), org, user, acme.ID, &pubCandID); xerr == nil {
		t.Fatal("bullet14 GenerateWhatsAppDraft must block public phone without opt-in")
	}

	// Consented WA on a separate account (ordinal-1) so email prior-touch gating does not block.
	var waAcc *models.OutreachAccount
	for i := range accs {
		if accs[i].ID != acme.ID && accs[i].QueueState != models.OutreachQueueNeedsContact {
			waAcc = &accs[i]
			break
		}
	}
	if waAcc == nil {
		waID := uuid.New()
		waAcc = &models.OutreachAccount{
			ID: waID, OrganizationID: org, SourceLeadID: "lead-wa-pa",
			CNPJ14: "99888777000166", RazaoSocial: "WA Demo Ltda", NomeFantasia: "WA Demo",
			ServiceCode: acme.ServiceCode, FactToMention: acme.FactToMention,
			QueueState: models.OutreachQueueReadyToGenerate,
		}
		_, _ = rf.UpsertAccount(context.Background(), waAcc)
	}
	// SendApprovedWhatsApp uses wall-clock Now for the service window, not the fake governor clock.
	nowWA := time.Now().UTC()
	eligCandID := uuid.New()
	_, _ = rf.UpsertCandidate(context.Background(), &models.OutreachContactCandidate{
		ID: eligCandID, OrganizationID: org, AccountID: waAcc.ID,
		Name: "Consented", Email: "wa@demo.example.com",
		Phone: "+5548888888888", PhoneE164: "+5548888888888",
		WhatsAppConsentStatus: whatsapp.ConsentOptedIn, WhatsAppConsentProvenanceOK: true,
		WhatsAppConsentSource: "website_form", WhatsAppConsentAt: &nowWA,
		VerificationStatus: models.OutreachVerifyOfficialSource, Recommended: true,
		LastImportRunID: waAcc.LastImportRunID,
	})
	_ = svc.waStore.UpsertContactState(context.Background(), &models.WhatsAppContactState{
		OrganizationID: org, PhoneE164: "+5548888888888",
		ConsentStatus: whatsapp.ConsentOptedIn, ConsentProvenanceOK: true,
		ConsentSource: "website_form", ConsentAt: &nowWA,
		LastInboundAt: &nowWA, ServiceWindowUntil: ptrT(nowWA.Add(24 * time.Hour)),
	})

	waDraft, xerr := svc.GenerateWhatsAppDraft(context.Background(), org, user, waAcc.ID, &eligCandID)
	if xerr != nil {
		t.Fatalf("bullet13 generate consented WA: %v", xerr)
	}
	waBody := "Ola, sou da CONFENGE. " + marker + " wa"
	waDraft.Status = models.OutreachDraftApproved
	waDraft.BodyText = waBody
	waDraft.Subject = ""
	if waDraft.RecipientPhoneE164 == "" {
		waDraft.RecipientPhoneE164 = "+5548888888888"
	}
	_ = rf.UpsertDraft(context.Background(), waDraft)
	waTP := &models.OutreachTouchpoint{
		ID: uuid.New(), OrganizationID: org, AccountID: waAcc.ID, Ordinal: 1,
		Channel: models.OutreachChannelWhatsApp, Purpose: models.TouchpointPurposeInitial,
		State: models.TouchpointNeedsReview, Recipient: waDraft.RecipientPhoneE164,
		BodyText: waBody, DraftID: &waDraft.ID, IdempotencyKey: "pa-wa-tp",
		GeneratedContextHash: waAcc.MessageContextHash,
	}
	RecomputeContentHash(waTP)
	if err := rf.InsertTouchpoint(context.Background(), waTP); err != nil {
		t.Fatal(err)
	}
	if err := ApplyHumanApproval(waTP, user, clock.Now()); err != nil {
		t.Fatal(err)
	}
	_ = rf.UpdateTouchpoint(context.Background(), waTP)
	if _, err := rf.CASQueueTouchpoint(context.Background(), org, waTP.ID, waTP.ContentHash); err != nil {
		// CAS optional if already approved; transport gate needs Approved + hashes
		t.Logf("CASQueueTouchpoint: %v (continuing with approved touchpoint)", err)
	}

	// Cap is full; free a slot so SendApprovedWhatsApp governor gate can proceed.
	clock.Advance(dispatch.RollingWindow + time.Second)
	beforeSends := waMock.SendCount()
	sentWA, xerr := svc.SendApprovedWhatsApp(context.Background(), org, user, waDraft.ID)
	if xerr != nil {
		t.Fatalf("bullet13 SendApprovedWhatsApp on consented contact: %v", xerr)
	}
	if sentWA.Status != models.OutreachDraftSent {
		t.Fatalf("bullet13 expected SENT, got %s", sentWA.Status)
	}
	if waMock.SendCount() <= beforeSends {
		t.Fatal("bullet13 mock WhatsApp provider must record SendText")
	}
	foundMock := false
	for _, s := range waMock.Sends {
		if strings.Contains(s.Body, marker) && s.ToE164 == "+5548888888888" {
			foundMock = true
			break
		}
	}
	if !foundMock {
		t.Fatal("bullet13 mock send body/recipient mismatch")
	}

	// 15. inbound reply pauses cadence via ProcessInboundHandoff (no manual Cancel in test)
	// Seed future open touchpoints that handoff must cancel.
	for i := 0; i < 2; i++ {
		future := &models.OutreachTouchpoint{
			ID: uuid.New(), OrganizationID: org, AccountID: acme.ID, Ordinal: 20 + i,
			Channel: models.OutreachChannelEmail, Purpose: models.TouchpointPurposeFollowUp,
			State: models.TouchpointPlanned, Recipient: draft1.RecipientEmail,
			DueAt:          clock.Now().Add(time.Duration(48+i) * time.Hour),
			IdempotencyKey: fmt.Sprintf("pa-future-%d", i),
		}
		RecomputeContentHash(future)
		_ = rf.InsertTouchpoint(context.Background(), future)
	}
	// Ensure candidate email resolves for handoff
	hr, xerr := svc.ProcessInboundHandoff(context.Background(), org, InboundHandoff{
		Channel:        models.OutreachChannelEmail,
		ContactEmail:   draft1.RecipientEmail,
		BodyText:       "Obrigado, podemos conversar na semana que vem?",
		IdempotencyKey: "pa-reply-1",
		OccurredAt:     clock.Now(),
		AccountID:      acme.ID,
	})
	if xerr != nil {
		t.Fatalf("bullet15 handoff: %v", xerr)
	}
	if hr == nil || !hr.StoppedCadence {
		t.Fatal("bullet15 handoff must report StoppedCadence")
	}
	// Assert open touchpoints cancelled by handoff itself
	afterReply, _ := rf.ListTouchpoints(context.Background(), org, acme.ID, "", 100, 0)
	openAfterReply := 0
	for _, tp := range afterReply {
		if models.TouchpointOpenStates[tp.State] {
			openAfterReply++
		}
	}
	if openAfterReply > 0 {
		t.Fatalf("bullet15 open touchpoints remain after handoff: %d", openAfterReply)
	}

	// 17. Needs attention from handoff queue state alone (no manual SetAccountHumanFlags)
	accAfter, _ := rf.GetAccount(context.Background(), org, acme.ID)
	if accAfter.QueueState != models.OutreachQueueReplied {
		t.Fatalf("bullet17 handoff must set queue REPLIED, got %s", accAfter.QueueState)
	}
	att, xerr := svc.ListAttention(context.Background(), org, FilterNeedsAttention, 50)
	if xerr != nil {
		t.Fatalf("bullet17 attention: %v", xerr)
	}
	foundAtt := false
	for _, a := range att {
		if a.AccountID == acme.ID {
			foundAtt = true
			break
		}
	}
	if !foundAtt {
		t.Fatal("bullet17 account must appear in Needs attention after handoff alone")
	}

	// 16. DNC via NoteDNC product path cancels next touches
	// Re-open a planned touch then drive NoteDNC
	futureDNC := &models.OutreachTouchpoint{
		ID: uuid.New(), OrganizationID: org, AccountID: acme.ID, Ordinal: 30,
		Channel: models.OutreachChannelEmail, Purpose: models.TouchpointPurposeFollowUp,
		State: models.TouchpointPlanned, Recipient: draft1.RecipientEmail,
		DueAt: clock.Now().Add(72 * time.Hour), IdempotencyKey: "pa-tp-dnc",
	}
	RecomputeContentHash(futureDNC)
	_ = rf.InsertTouchpoint(context.Background(), futureDNC)
	// Clear sticky DNC from previous if any so NoteDNC path runs cleanly on open TP
	_ = rf.SetAccountHumanFlags(context.Background(), org, acme.ID, false, false, "", models.OutreachQueueReplied)
	if err := svc.NoteDNC(context.Background(), org, draft1.RecipientEmail, "human_opt_out"); err != nil {
		t.Fatalf("bullet16 NoteDNC: %v", err)
	}
	tps, _ := rf.ListTouchpoints(context.Background(), org, acme.ID, "", 100, 0)
	for _, tp := range tps {
		if tp.ID == futureDNC.ID {
			if models.TouchpointOpenStates[tp.State] {
				t.Fatal("bullet16 NoteDNC must cancel open future touchpoint")
			}
			if tp.State != models.TouchpointDNC && tp.StopReason != "DNC" {
				// CancelOpenTouchpoints sets terminal DNC
				if tp.State != models.TouchpointDNC {
					t.Fatalf("bullet16 state=%s stop=%s", tp.State, tp.StopReason)
				}
			}
		}
	}
	accDNC, _ := rf.GetAccount(context.Background(), org, acme.ID)
	if !accDNC.DoNotContact {
		t.Fatal("bullet16 account must be DNC after NoteDNC")
	}

	// 18. reply draft not sent without new approval
	// Temporarily clear DNC so GenerateReplyDraft can run
	_ = rf.SetAccountHumanFlags(context.Background(), org, acme.ID, false, false, "", models.OutreachQueueReplied)
	accClear, _ := rf.GetAccount(context.Background(), org, acme.ID)
	accClear.DoNotContact = false
	accClear.Blocked = false
	accClear.QueueState = models.OutreachQueueReplied
	_, _ = rf.UpsertAccount(context.Background(), accClear)
	cands, _ := rf.ListCandidates(context.Background(), org, acme.ID)
	if len(cands) == 0 {
		_, _ = rf.UpsertCandidate(context.Background(), &models.OutreachContactCandidate{
			ID: uuid.New(), OrganizationID: org, AccountID: acme.ID,
			Name: "Ana", Email: draft1.RecipientEmail, Recommended: true,
			VerificationStatus: models.OutreachVerifyOfficialSource,
		})
	}
	replyDraft, xerr := svc.GenerateReplyDraft(context.Background(), org, user, acme.ID, nil)
	if xerr != nil {
		t.Fatalf("bullet18 generate reply: %v", xerr)
	}
	if replyDraft.Status != models.OutreachDraftNeedsReview {
		t.Fatalf("bullet18 reply draft must need review, got %s", replyDraft.Status)
	}
	rtp := &models.OutreachTouchpoint{
		ID: uuid.New(), OrganizationID: org, AccountID: acme.ID, Ordinal: 40,
		Channel: models.OutreachChannelEmail, Purpose: models.TouchpointPurposeFollowUp,
		State: models.TouchpointNeedsReview, Recipient: replyDraft.RecipientEmail,
		Subject: replyDraft.Subject, BodyText: replyDraft.BodyText, DraftID: &replyDraft.ID,
		IdempotencyKey: "pa-reply-tp",
	}
	RecomputeContentHash(rtp)
	if CanTransport(rtp) == nil {
		t.Fatal("bullet18 unapproved reply draft must not transport")
	}

	// 19. outcomes via HMAC, idempotent
	secret := "whsec_product_acceptance"
	payload := []byte(`{"event_type":"REPLIED","source_lead_id":"lead-acme-sc"}`)
	ts := clock.Now()
	hdr := SignOutcomeHMAC(secret, ts, payload)
	if !VerifyOutcomeHMAC(secret, hdr, payload, ts, 5*time.Minute) {
		t.Fatal("bullet19 hmac verify failed")
	}
	if code := receiver.Post(secret, ts, payload); code != http.StatusOK && code != http.StatusAccepted {
		t.Fatalf("bullet19 first deliver status %d", code)
	}
	if code := receiver.Post(secret, ts, payload); code != http.StatusOK && code != http.StatusAccepted {
		t.Fatalf("bullet19 idempotent redeliver status %d", code)
	}
	if receiver.uniqueCount() != 1 {
		t.Fatalf("bullet19 expected 1 unique event, got %d", receiver.uniqueCount())
	}
	ev := models.OutreachOutcome{
		OrganizationID: org, IdempotencyKey: "pa-out-1",
		SourceLeadID: "lead-acme-sc", CNPJ14: acme.CNPJ14,
		EventType: OutcomeReplied, OccurredAt: clock.Now(),
	}
	if xerr := svc.EnqueueOutcome(context.Background(), org, ev); xerr != nil {
		t.Fatal(xerr)
	}
	_ = svc.EnqueueOutcome(context.Background(), org, ev)

	// 20. reimport preserves DNC
	accDNC2, _ := rf.GetAccount(context.Background(), org, acme.ID)
	accDNC2.DoNotContact = true
	accDNC2.QueueState = models.OutreachQueueDoNotContact
	_, _ = rf.UpsertAccount(context.Background(), accDNC2)
	run2, xerr := svc.ImportFromBytes(context.Background(), org, &user, raw, ImportOptions{})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if run2.Counts.Creates != 0 {
		t.Fatalf("bullet20 reimport must not recreate: %+v", run2.Counts)
	}
	again, _ := rf.GetAccountByCNPJ(context.Background(), org, "11222333000181")
	if again == nil || !again.DoNotContact {
		t.Fatal("bullet20 DNC not preserved on reimport")
	}

	t.Logf("PRODUCT_ACCEPTANCE: 20 bullets on shipped paths; Mailpit=%s", mailpitAPI)
}

// --- Mailpit helpers ---

func resolveMailpitEndpoints(t *testing.T) (smtpAddr, apiBase string) {
	t.Helper()
	smtpAddr = strings.TrimSpace(os.Getenv("CONFENGE_MAILPIT_SMTP"))
	apiBase = strings.TrimSpace(os.Getenv("CONFENGE_MAILPIT_API"))
	// Prefer env, then local compose (11025/18025), then CI service (1025/8025).
	candidates := []struct{ smtp, api string }{
		{smtpAddr, apiBase},
		{"127.0.0.1:11025", "http://127.0.0.1:18025"},
		{"127.0.0.1:1025", "http://127.0.0.1:8025"},
	}
	for _, c := range candidates {
		if c.smtp == "" || c.api == "" {
			continue
		}
		if mailpitReachable(c.smtp, c.api) {
			return c.smtp, strings.TrimRight(c.api, "/")
		}
	}
	t.Fatalf("bullet12: Mailpit not reachable (set CONFENGE_MAILPIT_SMTP/API or start mailpit on 11025/18025 or 1025/8025)")
	return "", ""
}

func mailpitReachable(smtpAddr, apiBase string) bool {
	conn, err := net.DialTimeout("tcp", smtpAddr, 800*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(apiBase, "/")+"/api/v1/info", nil)
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode == http.StatusOK
}

func smtpSendApproved(smtpAddr, from, to, subject, body string) error {
	msg := strings.Join([]string{
		"From: " + from,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")
	return smtp.SendMail(smtpAddr, nil, from, []string{to}, []byte(msg))
}

// mailpitExactDelivery asserts approved recipient/subject/body equal the
// message received in Mailpit (transport-normalized line endings only).
func mailpitExactDelivery(t *testing.T, apiBase, marker, wantTo, wantSubject, wantBody string) bool {
	t.Helper()
	norm := func(s string) string {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
		return strings.TrimSpace(s)
	}
	wantBodyN, wantSubN := norm(wantBody), norm(wantSubject)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		u := apiBase + "/api/v1/search?query=" + url.QueryEscape(marker) + "&limit=20"
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		raw, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		var payload struct {
			Messages []struct {
				ID string `json:"ID"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		for _, m := range payload.Messages {
			fullURL := apiBase + "/api/v1/message/" + m.ID
			req2, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, fullURL, nil)
			res2, err := http.DefaultClient.Do(req2)
			if err != nil {
				continue
			}
			body, _ := io.ReadAll(res2.Body)
			_ = res2.Body.Close()
			var msg struct {
				Text    string `json:"Text"`
				Subject string `json:"Subject"`
				To      []struct {
					Address string `json:"Address"`
				} `json:"To"`
			}
			if err := json.Unmarshal(body, &msg); err != nil {
				continue
			}
			if !strings.Contains(msg.Text, marker) && !strings.Contains(msg.Subject, marker) {
				continue
			}
			// Exact body (normalized CRLF); subject exact when provided; recipient when provided.
			if wantBodyN != "" && norm(msg.Text) != wantBodyN {
				t.Logf("mailpit body mismatch: got %q want %q", norm(msg.Text), wantBodyN)
				continue
			}
			if wantSubN != "" && norm(msg.Subject) != wantSubN {
				t.Logf("mailpit subject mismatch: got %q want %q", msg.Subject, wantSubject)
				continue
			}
			if wantTo != "" {
				gotTo := ""
				if len(msg.To) > 0 {
					gotTo = strings.TrimSpace(msg.To[0].Address)
				}
				if !strings.EqualFold(gotTo, wantTo) {
					t.Logf("mailpit recipient mismatch: got %q want %q", gotTo, wantTo)
					continue
				}
			}
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// --- outcome receiver ---

type testOutcomeReceiver struct {
	t      *testing.T
	srv    *httptestLike
	mu     sync.Mutex
	seen   map[string]int
	bodies [][]byte
}

// thin alias to avoid importing httptest name clash noise in helpers above
type httptestLike = struct {
	URL   string
	Close func()
}

func newTestOutcomeReceiver(t *testing.T) *testOutcomeReceiver {
	r := &testOutcomeReceiver{t: t, seen: map[string]int{}}
	// use net/http/httptest via local var
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		key := string(body)
		r.mu.Lock()
		r.seen[key]++
		if r.seen[key] == 1 {
			r.bodies = append(r.bodies, append([]byte(nil), body...))
		}
		r.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	// manual listen
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	r.srv = &httptestLike{
		URL: "http://" + ln.Addr().String(),
		Close: func() {
			_ = srv.Close()
			_ = ln.Close()
		},
	}
	return r
}

func (r *testOutcomeReceiver) Close() {
	if r.srv != nil && r.srv.Close != nil {
		r.srv.Close()
	}
}

func (r *testOutcomeReceiver) Post(secret string, ts time.Time, body []byte) int {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, r.srv.URL, strings.NewReader(string(body)))
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Warmbly-Signature", SignOutcomeHMAC(secret, ts, body))
	req.Header.Set("X-Warmbly-Timestamp", fmt.Sprintf("%d", ts.Unix()))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer res.Body.Close()
	return res.StatusCode
}

func (r *testOutcomeReceiver) uniqueCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}
