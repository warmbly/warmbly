package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Review mode holds every lead not yet emailed, and every lead enrolled later,
// through the same hold column routing already skips; nothing but an approval
// or turning review off releases one.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveCampaignReview -v
func TestLiveCampaignReviewHoldsUntilApproved(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	review := NewCampaignReviewRepository(pool)
	progress := NewCampaignProgressRepository(handle.Pool)

	pending := func(contact uuid.UUID) bool {
		t.Helper()
		var at *time.Time
		if err := pool.QueryRow(ctx, `SELECT review_pending_at FROM campaign_leads WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, contact).Scan(&at); err != nil {
			t.Fatalf("read review: %v", err)
		}
		return at != nil
	}
	source := func(contact uuid.UUID) string {
		t.Helper()
		var s *string
		if err := pool.QueryRow(ctx, `SELECT pause_source FROM campaign_leads WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, contact).Scan(&s); err != nil {
			t.Fatalf("read hold: %v", err)
		}
		if s == nil {
			return ""
		}
		return *s
	}
	step := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html, wait_after, position, kind)
		VALUES ($1, $2, $3, 'Intro', 'Hi {{.FirstName}}', 'Hello', '<p>Hello</p>', 0, 1, 'email')`, step, f.campaign, f.org); err != nil {
		t.Fatalf("step: %v", err)
	}

	before := addLead(t, f, "before-"+uuid.New().String()[:6]+"@test.local", "valid", true)
	emailed := addLead(t, f, "emailed-"+uuid.New().String()[:6]+"@test.local", "valid", true)
	paused := addLead(t, f, "paused-"+uuid.New().String()[:6]+"@test.local", "valid", true)
	if _, err := pool.Exec(ctx, `INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at) VALUES ($1, $2, $3, NOW())`, f.campaign, emailed, step); err != nil {
		t.Fatalf("progress: %v", err)
	}
	if _, err := progress.HoldLead(ctx, f.campaign, paused, nil, "by hand", models.LeadHoldSourceManual); err != nil {
		t.Fatalf("pause: %v", err)
	}

	held, released, err := review.SetReviewLeads(ctx, f.campaign, true)
	if err != nil || held != 2 || released != 0 {
		t.Fatalf("enable = %d held, %d released, %v; want both leads not yet emailed", held, released, err)
	}
	if !pending(before) || source(before) != "review" {
		t.Fatalf("lead enrolled before review: pending %v, slot %q", pending(before), source(before))
	}
	// A paused lead waits too, and keeps its pause in the slot.
	if !pending(paused) || source(paused) != "manual" {
		t.Fatalf("paused lead: pending %v, slot %q", pending(paused), source(paused))
	}
	if pending(emailed) || source(emailed) != "" {
		t.Fatalf("emailed lead was touched")
	}
	// Every enrolment path goes through the trigger.
	later := addLead(t, f, "later-"+uuid.New().String()[:6]+"@test.local", "valid", true)
	if !pending(later) || source(later) != "review" {
		t.Fatalf("lead enrolled in a review campaign: pending %v, slot %q", pending(later), source(later))
	}
	if hold, err := progress.GetLeadHold(ctx, f.campaign, later); err != nil || hold == nil || hold.Source != models.LeadHoldSourceReview {
		t.Fatalf("hold of a waiting lead = %+v, %v", hold, err)
	}

	// A member's pause takes the slot and the lead still waits; resuming it puts the review hold back.
	if hold, err := progress.HoldLead(ctx, f.campaign, later, nil, "", models.LeadHoldSourceManual); err != nil || hold == nil || hold.Source != models.LeadHoldSourceManual {
		t.Fatalf("manual pause over review = %+v, %v", hold, err)
	}
	if !pending(later) || source(later) != "manual" {
		t.Fatalf("paused waiting lead: pending %v, slot %q", pending(later), source(later))
	}
	if lifted, err := progress.ResumeLead(ctx, f.campaign, later); err != nil || !lifted || !pending(later) || source(later) != "review" {
		t.Fatalf("resume of a paused waiting lead lifted %v (%v): pending %v, slot %q", lifted, err, pending(later), source(later))
	}
	// Nor can an out-of-office hold.
	if held, err := progress.HoldLeadEverywhere(ctx, later, nil, "away", models.LeadHoldSourceOutOfOffice); err != nil || len(held) != 0 {
		t.Fatalf("automatic hold over review = %v, %v; want refused", held, err)
	}

	// A copied contact is held as cc, and goes back to review when the copy is removed.
	if _, err := pool.Exec(ctx, `INSERT INTO campaign_lead_cc (campaign_id, contact_id, cc_contact_id) VALUES ($1, $2, $3)`, f.campaign, emailed, later); err != nil {
		t.Fatalf("cc: %v", err)
	}
	if got := source(later); got != "cc" {
		t.Fatalf("copied lead = %q, want cc", got)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM campaign_lead_cc WHERE campaign_id = $1 AND cc_contact_id = $2`, f.campaign, later); err != nil {
		t.Fatalf("uncc: %v", err)
	}
	if !pending(later) || source(later) != "review" {
		t.Fatalf("lead released from cc in a review campaign: pending %v, slot %q", pending(later), source(later))
	}

	// Resuming lifts the pause and nothing else: the lead still waits, with the review hold older releases read.
	if _, err := progress.ResumeLead(ctx, f.campaign, paused); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !pending(paused) || source(paused) != "review" {
		t.Fatalf("resumed lead: pending %v, slot %q", pending(paused), source(paused))
	}
	// Resuming a lead that only waits for review approves nothing.
	if lifted, err := progress.ResumeLead(ctx, f.campaign, later); err != nil || lifted || !pending(later) || source(later) != "review" {
		t.Fatalf("resume of a waiting lead lifted %v (%v): pending %v, slot %q", lifted, err, pending(later), source(later))
	}

	counts, err := review.Counts(ctx, f.campaign)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Total != 4 || counts.Waiting != 3 || counts.Started != 1 {
		t.Fatalf("counts = %+v, want 3 waiting and 1 started", counts)
	}
	missing, err := review.MissingCounts(ctx, f.campaign, []string{"FirstName", "Company", "bogus"})
	if err != nil {
		t.Fatal(err)
	}
	if missing["Company"] != 3 || missing["FirstName"] != 0 {
		t.Fatalf("missing = %v; want company blank on the 3 unstarted leads", missing)
	}

	rows, more, err := review.ListLeads(ctx, f.campaign, models.CampaignReviewLeadsFilter{Status: models.LeadReviewWaiting, Limit: 2, Gaps: []string{"Company"}})
	if err != nil || len(rows) != 2 || !more {
		t.Fatalf("first page = %d rows, more %v, %v", len(rows), more, err)
	}
	if rows[0].Hold == nil || rows[0].Hold.Source != models.LeadHoldSourceReview || len(rows[0].Missing) != 1 {
		t.Fatalf("row = %+v", rows[0])
	}
	next, _, err := review.ListLeads(ctx, f.campaign, models.CampaignReviewLeadsFilter{Status: models.LeadReviewWaiting, Limit: 2,
		Cursor: &models.CampaignReviewCursor{Email: rows[1].Email, ContactID: rows[1].ContactID}})
	if err != nil || len(next) != 1 || next[0].ContactID == rows[0].ContactID || next[0].ContactID == rows[1].ContactID {
		t.Fatalf("second page = %+v, %v", next, err)
	}

	// Routing reads waiting for review as a hold even with the slot emptied, as
	// a release that predates review_pending_at would leave it.
	if _, err := pool.Exec(ctx, `UPDATE campaign_leads SET paused_at = NULL, pause_source = NULL WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, later); err != nil {
		t.Fatal(err)
	}
	if route, err := progress.RouteContact(ctx, f.campaign, later); err != nil || route == nil || route.Hold == nil || route.Hold.Source != models.LeadHoldSourceReview {
		t.Fatalf("route of a waiting lead with an empty slot = %+v, %v; want held for review", route, err)
	}

	// Per-lead copy round trip, removed with the lead.
	o, err := review.UpsertOverride(ctx, models.LeadStepOverride{CampaignID: f.campaign, ContactID: later, StepID: step, Subject: "Mine", BodyHTML: "<p>Only you</p>"})
	if err != nil || o.Subject != "Mine" {
		t.Fatalf("upsert = %+v, %v", o, err)
	}
	if got, _ := review.GetOverride(ctx, f.campaign, later, step); got == nil || got.BodyHTML != "<p>Only you</p>" {
		t.Fatalf("get = %+v", got)
	}
	// The copy of a step already sent no longer changes.
	if _, err := review.UpsertOverride(ctx, models.LeadStepOverride{CampaignID: f.campaign, ContactID: emailed, StepID: step, BodyHTML: "<p>Late</p>"}); !errors.Is(err, ErrLeadStepSent) {
		t.Fatalf("override of a sent step = %v, want ErrLeadStepSent", err)
	}
	if _, err := review.DeleteOverride(ctx, f.campaign, emailed, step); !errors.Is(err, ErrLeadStepSent) {
		t.Fatalf("reset of a sent step = %v, want ErrLeadStepSent", err)
	}
	if err := review.SaveLeadAIVariable(ctx, f.campaign, emailed, step, "b1", "late", true); !errors.Is(err, ErrLeadStepSent) {
		t.Fatalf("AI copy of a sent step = %v, want ErrLeadStepSent", err)
	}

	// AI written ahead: a lead with every block cached counts as written.
	if err := progress.SaveResolvedAIVariable(ctx, f.campaign, before, step, "b1", "text"); err != nil {
		t.Fatal(err)
	}
	written, toWrite, err := review.AIProgress(ctx, f.campaign, step, []string{"b1"})
	if err != nil || written != 1 || toWrite != 1 {
		t.Fatalf("ai progress = %d written, %d to write, %v; want 1 and 1 (the personalized lead needs none)", written, toWrite, err)
	}
	ids, total, err := review.PendingAILeads(ctx, f.campaign, step, []string{"b1"}, nil, true, nil, 10)
	if err != nil || total != 1 || len(ids) != 1 || ids[0].ContactID != paused {
		t.Fatalf("pending = %v of %d, %v", ids, total, err)
	}

	// Approving one lead releases only it; reopening puts it back.
	if n, err := review.ApproveLeads(ctx, f.campaign, []uuid.UUID{before}, false); err != nil || n != 1 {
		t.Fatalf("approve = %d, %v", n, err)
	}
	if pending(before) || source(before) != "" {
		t.Fatalf("approved lead: pending %v, slot %q", pending(before), source(before))
	}
	// Enabling again moves nothing, so an approval is never undone by a retry.
	if held, released, err := review.SetReviewLeads(ctx, f.campaign, true); err != nil || held != 0 || released != 0 || pending(before) {
		t.Fatalf("repeat enable = %d held, %d released, %v; approved lead pending %v", held, released, err, pending(before))
	}
	if n, err := review.ReopenLeads(ctx, f.campaign, []uuid.UUID{before, emailed}); err != nil || n != 1 {
		t.Fatalf("reopen = %d, %v; want only the lead not yet emailed", n, err)
	}

	// Off approves everyone waiting.
	_, released, err = review.SetReviewLeads(ctx, f.campaign, false)
	if err != nil || released != 3 {
		t.Fatalf("disable released %d, %v; want 3", released, err)
	}
	if if2 := addLead(t, f, "after-"+uuid.New().String()[:6]+"@test.local", "valid", true); source(if2) != "" {
		t.Fatalf("lead enrolled after review was turned off is held")
	}
}

// An org transfer import keeps each lead's hold as it was exported.
func TestLiveCampaignReviewImportKeepsHolds(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE campaigns SET review_leads = true WHERE id = $1`, f.campaign); err != nil {
		t.Fatal(err)
	}
	contact := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields)
		VALUES ($1, $2, $3, $4, 'A', 'B', '', '', '{}'::jsonb)`, contact, f.owner, f.org, "imported-"+contact.String()[:6]+"@test.local"); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT set_config('warmbly.org_import', 'on', true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO campaign_leads (campaign_id, contact_id) VALUES ($1, $2)`, f.campaign, contact); err != nil {
		t.Fatal(err)
	}
	var source *string
	if err := tx.QueryRow(ctx, `SELECT pause_source FROM campaign_leads WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, contact).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != nil {
		t.Fatalf("imported lead was held with %q", *source)
	}
}
