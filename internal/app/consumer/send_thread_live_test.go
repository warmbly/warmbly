package jobs

import (
	"context"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// The campaign follow-up threading loop, end to end through the control plane:
// the worker answers a send with the conversation handle the provider gave it,
// the consumer records it against the task, and the next step's lookup finds
// it. Every campaign follow-up used to open its own conversation because none
// of this existed (issue #472).
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/app/consumer/ -run LiveThread -v
func TestLiveThreadHandleEmailSentRecordsTheConversationHandle(t *testing.T) {
	handle := liveDB(t)
	ctx := context.Background()
	s := liveJobsService(handle)
	f := newSendResultFixture(t, handle)

	taskID := f.stampSend(t, s)
	if err := s.HandleEmailSent(ctx, models.SendEmailResult{
		TaskID:    taskID,
		Success:   true,
		MessageID: "<first@test.local>",
		ThreadID:  "gmail-thread-1",
	}); err != nil {
		t.Fatalf("handle sent: %v", err)
	}

	parent, err := repository.NewCampaignProgressRepository(handle.Pool).
		ThreadParentForLead(ctx, f.campaign, f.contact)
	if err != nil {
		t.Fatalf("thread parent: %v", err)
	}
	if parent == nil {
		t.Fatal("the contact's next step found nothing to reply to")
	}
	if parent.MessageID != "<first@test.local>" {
		t.Errorf("parent message id = %q, want the id the worker reported", parent.MessageID)
	}
	if parent.ThreadID != "gmail-thread-1" {
		t.Errorf("parent thread id = %q, want the handle the worker reported", parent.ThreadID)
	}
	if parent.SenderID != f.mailbox {
		t.Errorf("parent sender = %s, want the sending mailbox %s", parent.SenderID, f.mailbox)
	}
	if parent.Subject != "Hi" {
		t.Errorf("conversation subject = %q, want the first step's subject", parent.Subject)
	}
}

// A provider with no conversation handle (SMTP, Graph) still gives the
// follow-up a Message-ID to reference, and must not blank one already there.
func TestLiveThreadHandleEmailSentKeepsTheHandleWhenAResultCarriesNone(t *testing.T) {
	handle := liveDB(t)
	ctx := context.Background()
	s := liveJobsService(handle)
	f := newSendResultFixture(t, handle)

	taskID := f.stampSend(t, s)
	if err := s.HandleEmailSent(ctx, models.SendEmailResult{
		TaskID: taskID, Success: true, MessageID: "<first@test.local>", ThreadID: "gmail-thread-1",
	}); err != nil {
		t.Fatalf("handle sent: %v", err)
	}
	// A redelivered result from a worker that reports no handle (or an older
	// worker that does not know the field) must not erase the one recorded.
	if err := s.HandleEmailSent(ctx, models.SendEmailResult{
		TaskID: taskID, Success: true, MessageID: "<first@test.local>",
	}); err != nil {
		t.Fatalf("handle sent (no handle): %v", err)
	}

	parent, err := repository.NewCampaignProgressRepository(handle.Pool).
		ThreadParentForLead(ctx, f.campaign, f.contact)
	if err != nil {
		t.Fatalf("thread parent: %v", err)
	}
	if parent == nil || parent.ThreadID != "gmail-thread-1" {
		t.Fatalf("parent = %+v, want the recorded thread handle kept", parent)
	}
}
