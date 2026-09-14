package tasks

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

// Follow-up threading, end to end over the real tick (issue #472). Every step
// after a contact's first used to go out with no In-Reply-To, no References and
// no provider thread handle, so each one opened its own conversation and read
// as a second cold email. What has to hold is the whole loop: the first send
// is confirmed, the confirmation is recorded against its task, and the next
// tick hands the worker a message that names it.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/tasks/ -run LiveThread -v

// addFollowUp appends a second email step to the fixture's campaign and
// connects the first to it, the shape the wizard and the canvas both write.
func (f *campaignSendFixture) addFollowUp(t *testing.T, pool *pgxpool.Pool, subject string, threadReply bool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	stepID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
	        body_plain, body_html, wait_after, position, kind, thread_reply)
	     VALUES ($1, $2, $3, 'Step 2', $4, 'Bump', '<p>Bump</p>', 0, 1, 'email', $5)`,
		stepID, f.campaign, f.org, subject, threadReply); err != nil {
		t.Fatalf("follow-up step: %v", err)
	}
	conditions, err := json.Marshal(models.BranchConditions{
		Branches: []models.Branch{{BranchID: uuid.New().String(), TargetSequenceID: &stepID}},
	})
	if err != nil {
		t.Fatalf("marshal conditions: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sequences SET conditions = $2 WHERE id = $1`, f.step, conditions); err != nil {
		t.Fatalf("connect steps: %v", err)
	}
	return stepID
}

// tick runs one real campaign tick. The previous tick seeds its own successor,
// which is already pending for this campaign, so that one is cleared first:
// the chain allows exactly one queued tick at a time.
func (f *campaignSendFixture) tick(t *testing.T, svc *tasksService) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx,
		`UPDATE tasks SET status = 'cancelled' WHERE status = 'pending' AND id IN (
			SELECT task_id FROM campaign_tasks WHERE campaign_id = $1)`, f.campaign); err != nil {
		t.Fatalf("clear pending ticks: %v", err)
	}
	taskID := f.queueTick(t, svc.taskRepo)
	if xerr := svc.HandleCampaignTask(&proto.ProcessTask{TaskId: taskID.String()}); xerr != nil {
		t.Fatalf("campaign tick: %v", xerr)
	}
}

// confirmSend is what the worker's EMAIL_SENT does to the task that just went
// out: it records the Message-ID the provider put on the wire and, on Gmail,
// the conversation it landed in.
func (f *campaignSendFixture) confirmSend(t *testing.T, pool *pgxpool.Pool, messageID, threadID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		UPDATE tasks SET message_id = $2, thread_id = $3
		WHERE id = (
			SELECT ct.task_id FROM campaign_tasks ct
			JOIN tasks t ON t.id = ct.task_id
			WHERE ct.campaign_id = $1 AND ct.contact_id IS NOT NULL
			ORDER BY t.created_at DESC LIMIT 1
		)`, f.campaign, messageID, threadID); err != nil {
		t.Fatalf("confirm send: %v", err)
	}
}

func TestLiveThreadFollowUpRepliesOnTheFirstEmail(t *testing.T) {
	handle := liveCampaignDB(t)
	sender := &recordingSender{}
	svc := liveCampaignService(t, handle, sender)
	f := newCampaignSendFixture(t, handle.Pool)
	f.addFollowUp(t, handle.Pool, "", true)

	// Tick one: the contact's first email. Nothing to reply to yet.
	f.tick(t, svc)
	opener := sender.message(t, 0)
	if opener.InReplyTo != "" || opener.ThreadID != "" {
		t.Fatalf("the first email was threaded onto something: in_reply_to=%q thread=%q", opener.InReplyTo, opener.ThreadID)
	}
	if opener.Subject != "Hi" {
		t.Fatalf("first subject = %q, want the step's own", opener.Subject)
	}

	f.confirmSend(t, handle.Pool, "<opener@test.local>", "gmail-thread-1")

	// Tick two: the follow-up.
	f.tick(t, svc)
	if sender.count() != 2 {
		t.Fatalf("%d sends, want the follow-up too", sender.count())
	}
	followUp := sender.message(t, 1)
	if followUp.InReplyTo != "<opener@test.local>" {
		t.Errorf("follow-up in_reply_to = %q, want the first email's Message-ID", followUp.InReplyTo)
	}
	if followUp.ThreadID != "gmail-thread-1" {
		t.Errorf("follow-up thread = %q, want the conversation the first email landed in", followUp.ThreadID)
	}
	if followUp.Subject != "Hi" {
		t.Errorf("follow-up subject = %q, want the conversation's", followUp.Subject)
	}
}

// A step with reply-in-thread off opens its own conversation, subject and all,
// even though the contact already has one.
func TestLiveThreadFollowUpCanStartANewConversation(t *testing.T) {
	handle := liveCampaignDB(t)
	sender := &recordingSender{}
	svc := liveCampaignService(t, handle, sender)
	f := newCampaignSendFixture(t, handle.Pool)
	f.addFollowUp(t, handle.Pool, "New angle", false)

	f.tick(t, svc)
	f.confirmSend(t, handle.Pool, "<opener@test.local>", "gmail-thread-1")

	f.tick(t, svc)
	followUp := sender.message(t, 1)
	if followUp.InReplyTo != "" || followUp.ThreadID != "" {
		t.Fatalf("the step was threaded anyway: in_reply_to=%q thread=%q", followUp.InReplyTo, followUp.ThreadID)
	}
	if followUp.Subject != "New angle" {
		t.Errorf("follow-up subject = %q, want its own", followUp.Subject)
	}
}

// The provider thread handle belongs to the mailbox that owns it. A follow-up
// leaving from a different address still references the parent in its headers,
// which is what threads it for the recipient, but must not name a conversation
// its own provider has never heard of.
func TestLiveThreadHandleIsDroppedWhenTheMailboxChanged(t *testing.T) {
	handle := liveCampaignDB(t)
	sender := &recordingSender{}
	svc := liveCampaignService(t, handle, sender)
	f := newCampaignSendFixture(t, handle.Pool)
	f.addFollowUp(t, handle.Pool, "", true)

	f.tick(t, svc)
	f.confirmSend(t, handle.Pool, "<opener@test.local>", "gmail-thread-1")
	// The first send is attributed to a mailbox that is not the one sending now.
	if _, err := handle.Pool.Exec(context.Background(),
		`UPDATE campaign_tasks SET contact_id = contact_id WHERE campaign_id = $1`, f.campaign); err != nil {
		t.Fatalf("touch campaign tasks: %v", err)
	}
	if _, err := handle.Pool.Exec(context.Background(), `
		UPDATE tasks SET email_account_id = $2
		WHERE id IN (SELECT task_id FROM campaign_tasks WHERE campaign_id = $1 AND contact_id IS NOT NULL)
		  AND message_id <> ''`, f.campaign, f.otherMailbox(t, handle.Pool)); err != nil {
		t.Fatalf("reattribute the first send: %v", err)
	}

	f.tick(t, svc)
	followUp := sender.message(t, 1)
	if followUp.InReplyTo != "<opener@test.local>" {
		t.Errorf("follow-up in_reply_to = %q, want the headers to still thread it", followUp.InReplyTo)
	}
	if followUp.ThreadID != "" {
		t.Errorf("follow-up thread = %q, want none: it belongs to another mailbox", followUp.ThreadID)
	}
}

// otherMailbox is a second connected mailbox in the same workspace, for the
// case where a lead's sequence changed address.
func (f *campaignSendFixture) otherMailbox(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	        signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	     VALUES ($1, $2, $3, $4, 'Other', '', '', 'smtp_imap', 'active', 50, 0, 'UTC')`,
		id, f.user, f.org, "other-"+id.String()[:8]+"@test.local"); err != nil {
		t.Fatalf("other mailbox: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		if _, err := pool.Exec(c, `DELETE FROM campaign_tasks WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id = $1)`, id); err != nil {
			t.Errorf("cleanup other mailbox tasks: %v", err)
		}
		if _, err := pool.Exec(c, `DELETE FROM tasks WHERE email_account_id = $1`, id); err != nil {
			t.Errorf("cleanup other mailbox tasks: %v", err)
		}
		if _, err := pool.Exec(c, `DELETE FROM email_accounts WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup other mailbox: %v", err)
		}
	})
	return id
}

// The confirmation that carries a send's Message-ID comes back from the
// worker, so it can be missing: the consumer was down, or the result was lost.
// The follow-up then has nothing to reply to, which is fine for the headers,
// but its subject must still be the conversation's. A step authored in the
// composer has no subject of its own to fall back on, so without this it would
// ship with no Subject header at all.
func TestLiveThreadFollowUpKeepsTheSubjectWhenTheParentWasNeverConfirmed(t *testing.T) {
	handle := liveCampaignDB(t)
	sender := &recordingSender{}
	svc := liveCampaignService(t, handle, sender)
	f := newCampaignSendFixture(t, handle.Pool)
	f.addFollowUp(t, handle.Pool, "", true)

	f.tick(t, svc)
	// No confirmSend: the worker's EMAIL_SENT never landed.
	f.tick(t, svc)

	followUp := sender.message(t, 1)
	if followUp.Subject != "Hi" {
		t.Errorf("follow-up subject = %q, want the conversation's", followUp.Subject)
	}
	if followUp.InReplyTo != "" || followUp.ThreadID != "" {
		t.Errorf("follow-up threaded onto an unconfirmed send: in_reply_to=%q thread=%q",
			followUp.InReplyTo, followUp.ThreadID)
	}
}

// The provider handle only goes on a message carrying the conversation's
// subject, because Gmail refuses to file one that does not match. A step that
// opened the thread with a subject nothing can reproduce still gets the
// headers, which is what threads it for the recipient.
func TestLiveThreadHandleIsDroppedWhenTheConversationSubjectIsGone(t *testing.T) {
	handle := liveCampaignDB(t)
	sender := &recordingSender{}
	svc := liveCampaignService(t, handle, sender)
	f := newCampaignSendFixture(t, handle.Pool)
	f.addFollowUp(t, handle.Pool, "", true)

	f.tick(t, svc)
	f.confirmSend(t, handle.Pool, "<opener@test.local>", "gmail-thread-1")
	// The step that opened the conversation is gone, so its subject cannot be
	// read off anything.
	if _, err := handle.Pool.Exec(context.Background(),
		`UPDATE sequences SET subject = '' WHERE id = $1`, f.step); err != nil {
		t.Fatalf("blank the opener's subject: %v", err)
	}

	f.tick(t, svc)
	followUp := sender.message(t, 1)
	if followUp.InReplyTo != "<opener@test.local>" {
		t.Errorf("follow-up in_reply_to = %q, want the headers to still thread it", followUp.InReplyTo)
	}
	if followUp.ThreadID != "" {
		t.Errorf("follow-up thread = %q, want none: the subject cannot be matched", followUp.ThreadID)
	}
}
