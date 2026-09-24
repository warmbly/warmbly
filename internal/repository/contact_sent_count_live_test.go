package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Wait, condition and action steps stamp sent_at so routing moves past them,
// but they send nothing. Every "sent" a person reads counts email steps only:
// a flow of Email, Wait, Email, Action that reached its end sent two emails,
// not four.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveSentCountsEmailSteps -v
func TestLiveSentCountsEmailStepsOnly(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()

	step := func(position int, kind string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
		          body_plain, body_html, wait_after, position, kind)
		      VALUES ($1, $2, $3, 'Step', 'Hi', 'Hello', '<p>Hello</p>', 0, $4, $5)`, id, f.campaign, f.org, position, kind); err != nil {
			t.Fatalf("sequence: %v", err)
		}
		return id
	}
	email1, wait, email2, action := step(1, "email"), step(2, "wait"), step(3, "email"), step(4, "action")

	finished := addLead(t, f, "done-"+uuid.New().String()[:6]+"@test.local", "valid", true)
	midway := addLead(t, f, "mid-"+uuid.New().String()[:6]+"@test.local", "valid", true)
	stamp := func(contact uuid.UUID, seqs ...uuid.UUID) {
		t.Helper()
		for i, s := range seqs {
			if _, err := pool.Exec(ctx, `INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at)
			      VALUES ($1, $2, $3, NOW() - make_interval(mins => $4))`, f.campaign, contact, s, 60-i); err != nil {
				t.Fatalf("progress: %v", err)
			}
		}
	}
	stamp(finished, email1, wait, email2, action)
	stamp(midway, email1, wait)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM campaign_contact_progress WHERE campaign_id = $1`, f.campaign)
		_, _ = pool.Exec(context.Background(), `DELETE FROM sequences WHERE campaign_id = $1`, f.campaign)
	})

	contacts := &contactRepository{DB: handle}

	// The contact drawer's Sent tile.
	detail, xerr := contacts.GetDetail(ctx, f.owner, &f.org, finished)
	if xerr != nil {
		t.Fatalf("GetDetail: %v", xerr)
	}
	if detail.Engagement.TotalSent != 2 {
		t.Errorf("contact sent = %d, want 2", detail.Engagement.TotalSent)
	}

	// The Leads view: per-lead count and the status it decides.
	got := searchEngagement(t, contacts, f, models.SearchContacts{})
	if lead := got[finished].CampaignLead; lead == nil || lead.Sent != 2 || lead.Status != models.LeadStatusCompleted {
		t.Errorf("finished lead = %+v, want sent 2, completed", lead)
	}
	if lead := got[midway].CampaignLead; lead == nil || lead.Sent != 1 || lead.Status != models.LeadStatusActive {
		t.Errorf("midway lead = %+v, want sent 1, active", lead)
	}
	completed := searchEngagement(t, contacts, f, models.SearchContacts{LeadStatus: models.LeadStatusCompleted})
	if len(completed) != 1 || completed[finished].ID != finished {
		t.Errorf("completed filter returned %d leads, want only the finished one", len(completed))
	}
	counts, cerr := contacts.CampaignLeadCounts(ctx, f.org.String(), f.campaign.String())
	if cerr != nil {
		t.Fatalf("CampaignLeadCounts: %v", cerr)
	}
	if counts.Completed != 1 || counts.Processing != 1 || counts.Contacted != 2 {
		t.Errorf("lead counts = completed %d processing %d contacted %d, want 1/1/2", counts.Completed, counts.Processing, counts.Contacted)
	}

	// The campaign's own progress and the breaker's denominator.
	progress := NewCampaignProgressRepository(pool)
	p, err := progress.GetCampaignProgress(ctx, f.campaign)
	if err != nil {
		t.Fatalf("GetCampaignProgress: %v", err)
	}
	if p.EmailsSent != 3 || p.TotalSequences != 2 {
		t.Errorf("campaign progress = sent %d steps %d, want 3 sent over 2 email steps", p.EmailsSent, p.TotalSequences)
	}
	rates, err := progress.GetCampaignRollingRates(ctx, f.campaign, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("GetCampaignRollingRates: %v", err)
	}
	if rates.Sent != 3 {
		t.Errorf("rolling sent = %d, want 3", rates.Sent)
	}

	// A reply with no thread to attribute it to lands on the last email, never
	// on the action step after it.
	latest, err := progress.GetLatestCampaignSequenceForContact(ctx, finished)
	if err != nil {
		t.Fatalf("GetLatestCampaignSequenceForContact: %v", err)
	}
	if latest == nil || latest.SequenceID != email2 {
		t.Errorf("latest step = %+v, want the second email %v", latest, email2)
	}
}
