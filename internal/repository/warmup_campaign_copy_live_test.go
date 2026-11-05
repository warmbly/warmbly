package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// copyCampaignFixture is one campaign in f's organization that every active
// mailbox there sends for (no tags, no explicit senders), with an entry step
// (s1), a follow-up in the lead's thread (s2), a step that opens its own
// thread (s3) and a wait node. s1's Original is paused and it has an active
// variant B with no subject of its own and a paused variant C.
type copyCampaignFixture struct {
	campaign, s1, s2, s3, variantB uuid.UUID
}

func newCopyCampaignFixture(t *testing.T, f *poolLinkFixture) copyCampaignFixture {
	t.Helper()
	ctx := context.Background()
	c := copyCampaignFixture{campaign: uuid.New(), s1: uuid.New(), s2: uuid.New(), s3: uuid.New(), variantB: uuid.New()}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, status, updated_at, created_at)
	      VALUES ($1, $2, $3, 'Copy warmup', '', 62, 'active', NOW(), NOW())`, c.campaign, f.user, f.org)
	steps := []struct {
		id          uuid.UUID
		name, kind  string
		position    int
		threadReply bool
	}{
		{c.s1, "Intro", "email", 1, true},
		{c.s2, "Bump", "email", 2, true},
		{c.s3, "New angle", "email", 3, false},
		{uuid.New(), "Wait", "wait", 4, false},
	}
	for _, s := range steps {
		exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html, wait_after, position, kind, thread_reply)
		      VALUES ($1, $2, $3, $4, $8, $9, '', 0, $5, $6, $7)`, s.id, c.campaign, f.org, s.name, s.position, s.kind, s.threadReply, "Subject "+s.name, "Body of "+s.name)
	}
	exec(`INSERT INTO campaign_ab_variants (campaign_id, sequence_id, name, weight, is_control, is_active) VALUES ($1, $2, 'Original', 100, true, false)`, c.campaign, c.s1)
	exec(`INSERT INTO campaign_ab_variants (id, campaign_id, sequence_id, name, weight, subject, body_plain, is_active) VALUES ($1, $2, $3, 'B', 100, '', 'Body of B', true)`, c.variantB, c.campaign, c.s1)
	exec(`INSERT INTO campaign_ab_variants (campaign_id, sequence_id, name, weight, subject, body_plain, is_active) VALUES ($1, $2, 'C', 100, 'Subject C', 'Body of C', false)`, c.campaign, c.s1)
	t.Cleanup(func() {
		for _, sql := range []string{
			`DELETE FROM campaign_ab_variants WHERE campaign_id = $1`,
			`DELETE FROM sequences WHERE campaign_id = $1`,
			`DELETE FROM campaigns WHERE id = $1`,
		} {
			if _, err := f.pool.Exec(context.Background(), sql, c.campaign); err != nil {
				t.Errorf("cleanup %q: %v", sql, err)
			}
		}
	})
	return c
}

// A sender's warmup carries the opening arms of the campaigns it sends for:
// never a follow-up, a paused Original or a paused variant, never an arm it
// carried within the day, and never another workspace's or a paused campaign's.
func TestLiveCampaignCopyCandidatesFollowRunningOpeners(t *testing.T) {
	f := newPoolLinkFixture(t)
	c := newCopyCampaignFixture(t, f)
	r := NewWarmupContentRepository(f.pool)
	ctx := t.Context()

	got, err := r.CampaignCopyCandidates(ctx, f.org, f.sender)
	if err != nil {
		t.Fatal(err)
	}
	arms := map[string]string{}
	for _, a := range got {
		key := a.SequenceID.String()
		if a.VariantID != nil {
			key += "/" + a.VariantID.String()
		}
		arms[key] = a.Subject + " | " + a.BodyPlain
	}
	want := map[string]string{
		c.s1.String() + "/" + c.variantB.String(): "Subject Intro | Body of B",
		c.s3.String(): "Subject New angle | Body of New angle",
	}
	if len(arms) != len(want) {
		t.Fatalf("got arms %v, want %v", arms, want)
	}
	for k, v := range want {
		if arms[k] != v {
			t.Fatalf("arm %s: got %q, want %q (all: %v)", k, arms[k], v, arms)
		}
	}

	// Carried today: that arm waits a day.
	if _, err := f.pool.Exec(ctx, `INSERT INTO warmup_tokens(task_id, sender_account_id, recipient_account_id, content_source, sequence_id)
		VALUES ($1, $2, $3, 'campaign', $4)`, f.task, f.sender, f.recipient, c.s3); err != nil {
		t.Fatal(err)
	}
	got, err = r.CampaignCopyCandidates(ctx, f.org, f.sender)
	if err != nil || len(got) != 1 || got[0].SequenceID != c.s1 {
		t.Fatalf("an arm carried today came back: %+v %v", got, err)
	}

	other := newPoolLinkFixture(t)
	if foreign, err := r.CampaignCopyCandidates(ctx, other.org, other.sender); err != nil || len(foreign) != 0 {
		t.Fatalf("another workspace's sender drew this campaign's copy: %+v %v", foreign, err)
	}
	if foreign, err := r.CampaignCopyCandidates(ctx, other.org, f.sender); err != nil || len(foreign) != 0 {
		t.Fatalf("a sender drew copy under an organization it does not belong to: %+v %v", foreign, err)
	}

	if _, err := f.pool.Exec(ctx, `UPDATE campaigns SET status = 'paused' WHERE id = $1`, c.campaign); err != nil {
		t.Fatal(err)
	}
	if got, err = r.CampaignCopyCandidates(ctx, f.org, f.sender); err != nil || len(got) != 0 {
		t.Fatalf("a paused campaign's copy is still warmed: %+v %v", got, err)
	}
}

// Placement per arm reads the sends and the receipts their recipients filed,
// counts only the big three in the headline, stays listed after the campaign
// pauses, and never reaches another workspace.
func TestLiveCampaignCopyStepsReportPlacement(t *testing.T) {
	f := newPoolLinkFixture(t)
	c := newCopyCampaignFixture(t, f)
	r := NewWarmupContentRepository(f.pool)
	ctx := t.Context()
	since := time.Now().Add(-time.Hour)

	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET mail_host = 'gmail' WHERE id = $1`, f.recipient); err != nil {
		t.Fatal(err)
	}
	msgID := "<" + uuid.NewString() + "@fixture>"
	if _, err := f.pool.Exec(ctx, `INSERT INTO warmup_tokens(task_id, sender_account_id, recipient_account_id, content_source, sequence_id, variant_id, sent_message_id)
		VALUES ($1, $2, $3, 'campaign', $4, $5, $6)`, f.task, f.sender, f.recipient, c.s1, c.variantB, msgID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO warmup_received(email_account_id, internal_id, message_id, sender_account_id, first_landing, rescue_outcome)
		VALUES ($1, $2, $3, $4, 'spam', 'moved')`, f.recipient, uuid.New(), msgID, f.sender); err != nil {
		t.Fatal(err)
	}

	for _, status := range []string{"active", "paused"} {
		if _, err := f.pool.Exec(ctx, `UPDATE campaigns SET status = $2 WHERE id = $1`, c.campaign, status); err != nil {
			t.Fatal(err)
		}
		steps, err := r.CampaignCopySteps(ctx, f.org, since)
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, s := range steps {
			if s.SequenceID == c.s2 {
				t.Fatal("a follow-up in the lead's thread was listed as warmable copy")
			}
			if s.SequenceID != c.s1 || s.VariantID == nil || *s.VariantID != c.variantB {
				continue
			}
			found = true
			p := s.Placement
			if p.Sent != 1 || p.Reported != 1 || p.Spam != 1 || p.Rescued != 1 || p.Inbox != 0 || len(s.ByGroup) != 1 || s.ByGroup[0].Group != "google" {
				t.Fatalf("%s campaign: placement %+v by group %+v", status, p, s.ByGroup)
			}
			if s.CampaignName != "Copy warmup" || s.StepName != "Intro" || s.VariantName != "B" || s.CampaignStatus != status {
				t.Fatalf("%s campaign: labels %+v", status, s)
			}
		}
		if !found {
			t.Fatalf("%s campaign: variant B missing from %+v", status, steps)
		}
	}

	other := newPoolLinkFixture(t)
	if foreign, err := r.CampaignCopySteps(ctx, other.org, since); err != nil || len(foreign) != 0 {
		t.Fatalf("another workspace read this campaign's copy: %+v %v", foreign, err)
	}
}
