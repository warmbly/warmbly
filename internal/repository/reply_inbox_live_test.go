package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// replyInboxFixture adds a mailbox that only receives (the reply inbox) beside
// the routed-pairs mailbox that sends, and one campaign send from the latter.
type replyInboxFixture struct {
	*routedPairsFixture
	inbox      uuid.UUID
	inboxEmail string
	task       uuid.UUID
	messageID  string
}

func newReplyInboxFixture(t *testing.T, replyTo func(inboxEmail string) string) *replyInboxFixture {
	t.Helper()
	_, pool := liveContactDB(t)
	f := &replyInboxFixture{routedPairsFixture: newRoutedPairsFixture(t, pool, 2), inbox: uuid.New(), task: uuid.New()}
	f.inboxEmail = "replies-" + f.inbox.String()[:8] + "@test.local"
	f.messageID = "<" + f.task.String() + "@test.local>"
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(70, len(sql))], err)
		}
	}
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	      VALUES ($1, $2, $3, $4, 'Replies', '', '', 'smtp_imap', 'active', 50, 0, 'UTC')`,
		f.inbox, f.owner, f.org, f.inboxEmail)
	exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id, reply_to, completed_at)
	      VALUES ($1, 'campaign', $2, 'completed', $3, $4, NOW())`, f.task, f.mailbox, f.messageID, replyTo(f.inboxEmail))
	exec(`INSERT INTO campaign_tasks (task_id, campaign_id, contact_id, sequence_id) VALUES ($1, $2, $3, $4)`,
		f.task, f.campaign, f.leads[0], f.step)
	exec(`UPDATE campaign_leads SET email_account_id = $3 WHERE campaign_id = $1 AND contact_id = $2`,
		f.campaign, f.leads[0], f.mailbox)
	exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, dispatched_at, dispatch_task_id, replied_at)
	      VALUES ($1, $2, $3, NOW(), NOW(), $4, NOW())`, f.campaign, f.leads[0], f.step, f.task)

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM unibox_emails WHERE email_id = $1`, f.inbox},
			{`DELETE FROM campaign_contact_progress WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaign_tasks WHERE task_id = $1`, f.task},
			{`DELETE FROM tasks WHERE id = $1`, f.task},
			{`DELETE FROM email_accounts WHERE id = $1`, f.inbox},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return f
}

func TestLiveReplyInboxOwnsTheConversationItsSendsPointedAt(t *testing.T) {
	handle, _ := liveContactDB(t)
	f := newReplyInboxFixture(t, func(inbox string) string { return inbox })
	repo := NewEmailSyncStateRepository(handle)
	ctx := context.Background()

	own, err := repo.IsOwnConversation(ctx, f.owner, f.inbox, []string{f.messageID}, "")
	if err != nil || !own {
		t.Fatalf("IsOwnConversation at the reply inbox = %v, %v; want true", own, err)
	}
	// A mailbox the send did not name has no claim on the reply.
	own, err = repo.IsOwnConversation(ctx, f.owner, uuid.New(), []string{f.messageID}, "")
	if err != nil || own {
		t.Fatalf("IsOwnConversation elsewhere = %v, %v; want false", own, err)
	}
}

func TestLiveReplyInboxOwnsNothingWithoutTheHeader(t *testing.T) {
	handle, _ := liveContactDB(t)
	f := newReplyInboxFixture(t, func(string) string { return "" })
	repo := NewEmailSyncStateRepository(handle)

	own, err := repo.IsOwnConversation(context.Background(), f.owner, f.inbox, []string{f.messageID}, "")
	if err != nil || own {
		t.Fatalf("IsOwnConversation with no Reply-To = %v, %v; want false", own, err)
	}
}

func TestLiveCopiedReplyAtTheReplyInboxFindsTheLead(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newReplyInboxFixture(t, func(inbox string) string { return inbox })
	repo := NewCampaignProgressRepository(pool)
	ctx := context.Background()
	a, b := f.leads[0], f.leads[1]

	if err := repo.SetLeadCC(ctx, f.org, f.campaign, a, []uuid.UUID{b}); err != nil {
		t.Fatalf("SetLeadCC: %v", err)
	}
	ref, err := repo.LeadForCopiedReply(ctx, b, f.inbox)
	if err != nil || ref == nil || ref.ContactID != a || ref.CampaignID != f.campaign {
		t.Fatalf("LeadForCopiedReply at the reply inbox = %+v, %v; want the lead", ref, err)
	}
}

func TestLiveCopiedReplyAtAnUnnamedMailboxFindsNothing(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newReplyInboxFixture(t, func(string) string { return "someone-else@test.local" })
	repo := NewCampaignProgressRepository(pool)
	ctx := context.Background()
	a, b := f.leads[0], f.leads[1]

	if err := repo.SetLeadCC(ctx, f.org, f.campaign, a, []uuid.UUID{b}); err != nil {
		t.Fatalf("SetLeadCC: %v", err)
	}
	if ref, err := repo.LeadForCopiedReply(ctx, b, f.inbox); err != nil || ref != nil {
		t.Fatalf("LeadForCopiedReply at a mailbox the send did not name = %+v, %v; want nil", ref, err)
	}
}

func TestLiveRecentActivityNamesTheSendingMailbox(t *testing.T) {
	handle, _ := liveContactDB(t)
	f := newReplyInboxFixture(t, func(inbox string) string { return inbox })
	items, xerr := NewAnalyticsRepository(handle).GetRecentActivity(context.Background(), f.org, 10, nil)
	if xerr != nil {
		t.Fatalf("GetRecentActivity: %v", xerr)
	}
	for _, it := range items {
		if it.Type != "replied" || it.ContactID != f.leads[0] {
			continue
		}
		if it.SenderID == nil || *it.SenderID != f.mailbox || it.SenderEmail == "" || it.SenderEmail == f.inboxEmail {
			t.Fatalf("reply credits sender %v %q; want the sending mailbox %s", it.SenderID, it.SenderEmail, f.mailbox)
		}
		return
	}
	t.Fatalf("no reply in recent activity: %+v", items)
}

func TestLiveThreadMarksTheMailboxAReplyAnswers(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newReplyInboxFixture(t, func(inbox string) string { return inbox })
	ctx := context.Background()
	reply, own := uuid.New(), uuid.New()
	thread := "thread-" + f.task.String()[:8]
	for _, row := range []struct {
		id      uuid.UUID
		parents []string
	}{
		// Clients differ on the brackets; both forms have to match.
		{reply, []string{f.messageID[1 : len(f.messageID)-1]}},
		{own, []string{"<unrelated@test.local>"}},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO unibox_emails (id, user_id, email_id, folder, provider_folder, thread_id, in_reply_to, internal_date)
			VALUES ($1, $2, $3, 'inbox', 'inbox', $4, $5, NOW())`, row.id, f.owner, f.inbox, thread, row.parents); err != nil {
			t.Fatalf("unibox row: %v", err)
		}
	}

	res, err := NewUniboxRepository(handle).GetByThread(ctx, f.org, uuid.Nil, thread, 10, "")
	if err != nil {
		t.Fatalf("GetByThread: %v", err)
	}
	got := map[uuid.UUID]*uuid.UUID{}
	for _, m := range res.Data {
		got[m.ID] = m.AnswersMailboxID
	}
	if got[reply] == nil || *got[reply] != f.mailbox {
		t.Fatalf("reply answers %v; want the sending mailbox %s", got[reply], f.mailbox)
	}
	if got[own] != nil {
		t.Fatalf("unrelated message answers %v; want none", got[own])
	}
	// Another workspace reads none of it.
	other, err := NewUniboxRepository(handle).GetByThread(ctx, uuid.New(), uuid.Nil, thread, 10, "")
	if err != nil || len(other.Data) != 0 {
		t.Fatalf("thread from another workspace = %+v, %v; want empty", other, err)
	}
}
