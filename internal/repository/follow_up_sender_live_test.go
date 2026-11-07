package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// A follow-up references the whole conversation, whichever mailbox sent each
// step, oldest first and ending with the parent.
func TestLiveThreadParentReferencesTheWholeConversationAcrossMailboxes(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newThreadParentFixture(t, pool)
	first := f.step(0, "Quick question", true)
	second := f.step(1, "", true)
	third := f.step(2, "", true)
	id := func(n string) string { return "<" + n + "." + f.contact.String()[:8] + "@mail.test>" }
	f.send(first, f.mailbox, id("one"), "thr-1", 180)
	f.send(second, f.other, id("two"), "", 120)
	f.send(third, f.mailbox, id("three"), "thr-1", 60)

	p := f.parent(t)
	if p == nil {
		t.Fatal("got no parent, want the third send")
	}
	want := []string{id("one"), id("two"), id("three")}
	if fmt.Sprint(p.References) != fmt.Sprint(want) {
		t.Fatalf("references = %v, want %v", p.References, want)
	}
	if p.Subject != "Quick question" || p.MessageID != id("three") {
		t.Fatalf("parent = %+v, want the third send under the opening subject", p)
	}
}

// A step that opened a fresh conversation is where the chain starts: the
// earlier thread is a different conversation.
func TestLiveThreadParentReferencesStopAtTheStepThatStartedTheThread(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newThreadParentFixture(t, pool)
	first := f.step(0, "Quick question", true)
	fresh := f.step(1, "New angle", false)
	after := f.step(2, "", true)
	id := func(n string) string { return "<" + n + "." + f.contact.String()[:8] + "@mail.test>" }
	f.send(first, f.mailbox, id("one"), "thr-1", 180)
	f.send(fresh, f.other, id("two"), "", 120)
	f.send(after, f.mailbox, id("three"), "", 60)

	p := f.parent(t)
	want := []string{id("two"), id("three")}
	if p == nil || fmt.Sprint(p.References) != fmt.Sprint(want) {
		t.Fatalf("parent = %+v, want references %v", p, want)
	}
}

// A copy answering a lead whose follow-ups rotated is attributed through any
// mailbox that sent to the lead, not only the latest one.
func TestLiveLeadForCopiedReplyAcceptsAnyMailboxThatSentToTheLead(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 2)
	repo := NewCampaignProgressRepository(pool)
	ctx := context.Background()
	a, b := f.leads[0], f.leads[1]
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	if err := repo.SetLeadCC(ctx, f.org, f.campaign, a, []uuid.UUID{b}); err != nil {
		t.Fatalf("SetLeadCC: %v", err)
	}
	earlier := uuid.New()
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	      VALUES ($1, $2, $3, $4, 'Earlier', '', '', 'smtp_imap', 'active', 50, 0, 'UTC')`,
		earlier, f.owner, f.org, "earlier-"+earlier.String()[:8]+"@test.local")
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM campaign_tasks WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id = $1)`, earlier)
		_, _ = pool.Exec(c, `DELETE FROM tasks WHERE email_account_id = $1`, earlier)
		_, _ = pool.Exec(c, `DELETE FROM email_accounts WHERE id = $1`, earlier)
	})
	task := uuid.New()
	exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id)
	      VALUES ($1, 'campaign', $2, 'completed', $3)`, task, earlier, task.String()+"@mail.test")
	exec(`INSERT INTO campaign_tasks (task_id, campaign_id, contact_id, sequence_id) VALUES ($1, $2, $3, $4)`,
		task, f.campaign, a, f.step)
	// The latest step came from the fixture's mailbox, which the binding names.
	exec(`UPDATE campaign_leads SET email_account_id = $3 WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, a, f.mailbox)
	exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at)
	      VALUES ($1, $2, $3, NOW())`, f.campaign, a, f.step)

	ref, err := repo.LeadForCopiedReply(ctx, b, earlier)
	if err != nil || ref == nil || ref.ContactID != a {
		t.Fatalf("LeadForCopiedReply via the earlier sender = %+v, %v; want the lead", ref, err)
	}
	if ref, err := repo.LeadForCopiedReply(ctx, b, uuid.New()); err != nil || ref != nil {
		t.Fatalf("LeadForCopiedReply from a mailbox that never sent = %+v, %v; want nil", ref, err)
	}
}
