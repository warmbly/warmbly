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
// carried within the day, never a step the recipient was already sent, and never
// another workspace's or a paused campaign's.
func TestLiveCampaignCopyCandidatesFollowRunningOpeners(t *testing.T) {
	f := newPoolLinkFixture(t)
	c := newCopyCampaignFixture(t, f)
	r := NewWarmupContentRepository(f.pool)
	ctx := t.Context()

	got, err := r.CampaignCopyCandidates(ctx, f.org, f.sender, f.recipient)
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
	got, err = r.CampaignCopyCandidates(ctx, f.org, f.sender, f.recipient)
	if err != nil || len(got) != 1 || got[0].SequenceID != c.s1 {
		t.Fatalf("an arm carried today came back: %+v %v", got, err)
	}
	// Another sender of the workspace may carry s3, but not to a recipient that already got that step.
	second := uuid.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
		SELECT $1, user_id, organization_id, $2, name, '', '', provider, status, campaign_limit, min_wait_time, timezone FROM email_accounts WHERE id = $3`, second, second.String()+"@fixture.example", f.sender); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM email_accounts WHERE id = $1`, second) })
	if got, err = r.CampaignCopyCandidates(ctx, f.org, second, f.recipient); err != nil || len(got) != 1 || got[0].SequenceID != c.s1 {
		t.Fatalf("a recipient was offered a step it was already sent: %+v %v", got, err)
	}
	if got, err = r.CampaignCopyCandidates(ctx, f.org, second, uuid.New()); err != nil || len(got) != 2 {
		t.Fatalf("a fresh recipient must be offered every arm: %+v %v", got, err)
	}

	other := newPoolLinkFixture(t)
	if foreign, err := r.CampaignCopyCandidates(ctx, other.org, other.sender, other.recipient); err != nil || len(foreign) != 0 {
		t.Fatalf("another workspace's sender drew this campaign's copy: %+v %v", foreign, err)
	}
	if foreign, err := r.CampaignCopyCandidates(ctx, other.org, f.sender, f.recipient); err != nil || len(foreign) != 0 {
		t.Fatalf("a sender drew copy under an organization it does not belong to: %+v %v", foreign, err)
	}

	if _, err := f.pool.Exec(ctx, `UPDATE campaigns SET status = 'paused' WHERE id = $1`, c.campaign); err != nil {
		t.Fatal(err)
	}
	if got, err = r.CampaignCopyCandidates(ctx, f.org, f.sender, f.recipient); err != nil || len(got) != 0 {
		t.Fatalf("a paused campaign's copy is still warmed: %+v %v", got, err)
	}
}

// Placement per arm reads the openings and the receipts their recipients
// filed, counts only the big three in the headline, leaves reply turns out, and
// never reaches another workspace.
func TestLiveCampaignStepPlacement(t *testing.T) {
	f := newPoolLinkFixture(t)
	c := newCopyCampaignFixture(t, f)
	r := NewWarmupContentRepository(f.pool)
	ctx := t.Context()
	since := time.Now().Add(-time.Hour)

	msgID := "<" + uuid.NewString() + "@fixture>"
	if _, err := f.pool.Exec(ctx, `INSERT INTO warmup_tokens(task_id, sender_account_id, recipient_account_id, content_source, sequence_id, variant_id, sent_message_id)
		VALUES ($1, $2, $3, 'campaign', $4, $5, $6)`, f.task, f.sender, f.recipient, c.s1, c.variantB, msgID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO warmup_received(email_account_id, internal_id, message_id, sender_account_id, first_landing, rescue_outcome)
		VALUES ($1, $2, $3, $4, 'spam', 'moved')`, f.recipient, uuid.New(), msgID, f.sender); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"gmail", "outlook", "yahoo", "zoho"} {
		if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET mail_host = $2 WHERE id = $1`, f.recipient, host); err != nil {
			t.Fatal(err)
		}
		if steps, err := r.CampaignStepPlacement(ctx, f.org, c.campaign, since); err != nil || len(steps) != 0 {
			t.Fatalf("a %s recipient counted toward the step's placement: %+v %v", host, steps, err)
		}
	}
	var address, provider string
	if err := f.pool.QueryRow(ctx, `SELECT email, provider::text FROM email_accounts WHERE id = $1`, f.recipient).Scan(&address, &provider); err != nil {
		t.Fatal(err)
	}
	// A free address is never a business inbox, whatever host is stored for it.
	for _, free := range []struct{ host, provider, domain string }{
		{"", "gmail", "gmail.com"}, {"", "outlook", "hotmail.co.uk"}, {"google_workspace", "gmail", "gmail.com"},
	} {
		if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET mail_host = $2, provider = $3, email = $4 WHERE id = $1`,
			f.recipient, free.host, free.provider, uuid.NewString()+"@"+free.domain); err != nil {
			t.Fatal(err)
		}
		if steps, err := r.CampaignStepPlacement(ctx, f.org, c.campaign, since); err != nil || len(steps) != 0 {
			t.Fatalf("a %s address with host %q counted toward the step's placement: %+v %v", free.domain, free.host, steps, err)
		}
	}
	// A business domain whose host was never detected is still read from its provider.
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET mail_host = '', provider = 'gmail', email = $2 WHERE id = $1`,
		f.recipient, uuid.NewString()+"@northridge.example"); err != nil {
		t.Fatal(err)
	}
	if steps, err := r.CampaignStepPlacement(ctx, f.org, c.campaign, since); err != nil || len(steps) != 1 || steps[0].ByGroup[0].Group != "google" {
		t.Fatalf("a business address with no detected host was not counted at Google: %+v %v", steps, err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET mail_host = 'google_workspace', email = $2, provider = $3 WHERE id = $1`, f.recipient, address, provider); err != nil {
		t.Fatal(err)
	}

	for _, status := range []string{"active", "paused"} {
		if _, err := f.pool.Exec(ctx, `UPDATE campaigns SET status = $2 WHERE id = $1`, c.campaign, status); err != nil {
			t.Fatal(err)
		}
		steps, err := r.CampaignStepPlacement(ctx, f.org, c.campaign, since)
		if err != nil {
			t.Fatal(err)
		}
		if len(steps) != 1 || steps[0].SequenceID != c.s1 || steps[0].VariantID == nil || *steps[0].VariantID != c.variantB {
			t.Fatalf("%s campaign: want variant B of the intro alone, got %+v", status, steps)
		}
		p := steps[0].Placement
		if p.Sent != 1 || p.Reported != 1 || p.Spam != 1 || p.Rescued != 1 || p.Inbox != 0 || len(steps[0].ByGroup) != 1 || steps[0].ByGroup[0].Group != "google" {
			t.Fatalf("%s campaign: placement %+v by group %+v", status, p, steps[0].ByGroup)
		}
	}

	other := newPoolLinkFixture(t)
	if foreign, err := r.CampaignStepPlacement(ctx, other.org, c.campaign, since); err != nil || len(foreign) != 0 {
		t.Fatalf("another workspace read this campaign's placement: %+v %v", foreign, err)
	}
}

// The dispatch-time lookup admits a campaign-copy send, which names no bank
// conversation, and still refuses any other send without one.
func TestLiveExecutionContextAdmitsCampaignCopy(t *testing.T) {
	f, r := lineageFixture(t)
	ctx := t.Context()
	if _, err := f.pool.Exec(ctx, `INSERT INTO warmup_tasks(task_id,lineage_version,subject,scenario_version,rendering_version,max_turns) VALUES($1,1,'Hiring at Northridge?','natural-v1','canonical-v1',3)`, f.task); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO warmup_tokens(task_id,sender_account_id,recipient_account_id,content_source,sequence_id) VALUES($1,$2,$3,'campaign',$4)`, f.task, f.sender, f.recipient, uuid.New()); err != nil {
		t.Fatal(err)
	}
	recipient, c, err := r.WarmupExecutionContext(ctx, f.task)
	if err != nil || recipient != f.recipient || c.ContentSource != "campaign" || c.ConversationID != nil || c.MaxTurns != 3 {
		t.Fatalf("campaign copy refused at dispatch: %v %+v %v", recipient, c, err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE warmup_tokens SET content_source='ai' WHERE task_id=$1`, f.task); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.WarmupExecutionContext(ctx, f.task); err == nil {
		t.Fatal("a bank send with no conversation was admitted")
	}
}

// A received campaign-copy opening is owed a reply like any warmup thread, and
// a token with no conversation from any other source is not.
func TestLiveCampaignCopyThreadIsAnswerable(t *testing.T) {
	f, r := lineageFixture(t)
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	msgID := uuid.NewString() + "@" + "acme.example"
	exec(`INSERT INTO warmup_tasks(task_id,lineage_version,subject,scenario_version,rendering_version,max_turns)
		VALUES($1,1,'Hiring at Northridge?','natural-v1','canonical-v1',3)`, f.task)
	exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,content_source,sequence_id,conversation_turn,sent_message_id)
		VALUES($1,$2,$3,$4,'campaign',$5,0,$6)`, uuid.New(), f.task, f.sender, f.recipient, uuid.New(), "<"+msgID+">")
	receipt := uuid.New()
	exec(`INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id,created_at)
		VALUES($1,$2,$3,$4,NOW()-INTERVAL '2 hours')`, f.recipient, receipt, f.sender, msgID)
	if err := r.RecordVerifiedWarmupParent(ctx, f.task, f.recipient, receipt, "thread", WarmupReceiptProof{SenderAddress: f.senderTo}); err != nil {
		t.Fatal(err)
	}
	owed, err := r.OwedWarmupReplies(ctx, f.recipient, 8, WarmupReplyExclusions{})
	if err != nil || len(owed) != 1 || owed[0].Parent.ContentSource != "campaign" || owed[0].Parent.ConversationID != nil || owed[0].Parent.MaxTurns != 3 {
		t.Fatalf("a campaign copy opening was not offered for a reply: %+v %v", owed, err)
	}
	exec(`UPDATE warmup_tokens SET content_source='ai' WHERE task_id=$1`, f.task)
	if owed, err = r.OwedWarmupReplies(ctx, f.recipient, 8, WarmupReplyExclusions{}); err != nil || len(owed) != 0 {
		t.Fatalf("a bank thread with no conversation was offered: %+v %v", owed, err)
	}
}
