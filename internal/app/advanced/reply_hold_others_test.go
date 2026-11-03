package advanced

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// elsewhereRecorder records the holds a reply writes across campaigns.
type elsewhereRecorder struct {
	*incomingReplyProgressRepo
	contacts   []uuid.UUID
	except     []uuid.UUID
	until      *time.Time
	source     string
	everywhere int
	failWith   error
}

func (r *elsewhereRecorder) HoldLeadElsewhere(_ context.Context, contact, except uuid.UUID, until *time.Time, _, source string) ([]uuid.UUID, error) {
	r.contacts = append(r.contacts, contact)
	r.except = append(r.except, except)
	r.until, r.source = until, source
	if r.failWith != nil {
		return nil, r.failWith
	}
	return []uuid.UUID{uuid.New()}, nil
}

func (r *elsewhereRecorder) HoldLeadEverywhere(context.Context, uuid.UUID, *time.Time, string, string) ([]uuid.UUID, error) {
	r.everywhere++
	return nil, nil
}

type replyHoldFixture struct {
	svc       *service
	rec       *elsewhereRecorder
	accountID uuid.UUID
	contactID uuid.UUID
	answered  uuid.UUID
}

func newReplyHoldService(stopAll bool) replyHoldFixture {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "owner@acme.test"}
	svc, progress := newIncomingReplyService(account, &models.Contact{ID: contactID, Email: "lead@example.com"}, contactID)
	progress.advanced.stopAll = stopAll
	rec := &elsewhereRecorder{incomingReplyProgressRepo: progress}
	svc.campaignProgressRepo = rec
	return replyHoldFixture{svc: svc, rec: rec, accountID: accountID, contactID: contactID,
		answered: *svc.taskRepo.(incomingReplyTaskRepo).campaign.CampaignID}
}

func replyFromLead(accountID uuid.UUID, subject, body string, flags []string) *models.EmailMessageStoreData {
	return &models.EmailMessageStoreData{
		ID: uuid.New(), EmailID: accountID, Folder: models.FolderInbox,
		FromAddr:  []string{"Lead <lead@example.com>"},
		ToAddr:    []string{"owner@acme.test"},
		InReplyTo: []string{"<send@acme.test>"},
		Subject:   subject, Snippet: body, BodyText: body,
		Flags: flags,
	}
}

// With the setting on, a human reply holds the contact in every campaign but
// the one it answered, as a dated automatic hold.
func TestHumanReplyHoldsOtherCampaignsWhenEnabled(t *testing.T) {
	f := newReplyHoldService(true)
	if xerr := f.svc.ProcessIncomingReply(context.Background(), f.accountID,
		replyFromLead(f.accountID, "Re: Hello", "Sounds good, let's talk Tuesday.", nil)); xerr != nil {
		t.Fatal(xerr)
	}
	if len(f.rec.except) != 1 || f.rec.except[0] != f.answered {
		t.Fatalf("HoldLeadElsewhere excepts %v, want once for the answered campaign %s", f.rec.except, f.answered)
	}
	if f.rec.contacts[0] != f.contactID {
		t.Fatalf("held contact %s, want the replying contact %s", f.rec.contacts[0], f.contactID)
	}
	if f.rec.source != models.LeadHoldSourceInboxTagging {
		t.Fatalf("hold source = %q, want an automatic hold a member can resume", f.rec.source)
	}
	if f.rec.until == nil || time.Until(*f.rec.until) < 300*24*time.Hour {
		t.Fatalf("hold lifts at %v, want about a year out", f.rec.until)
	}
}

// A hold that cannot be written leaves the reply unprocessed, so it is retried.
func TestFailedReplyHoldLeavesTheReplyToRetry(t *testing.T) {
	f := newReplyHoldService(true)
	f.rec.failWith = errors.New("connection reset")
	if xerr := f.svc.ProcessIncomingReply(context.Background(), f.accountID,
		replyFromLead(f.accountID, "Re: Hello", "Sounds good, let's talk Tuesday.", nil)); xerr == nil {
		t.Fatal("a failed hold was swallowed")
	}
	if f.rec.completed != 0 {
		t.Fatalf("the reply was marked processed %d times after a failed hold", f.rec.completed)
	}
}

// Off, the default, a reply stops only the campaign it answered.
func TestHumanReplyHoldsNothingElseByDefault(t *testing.T) {
	if models.DefaultAdvancedOutreachSettings().ReplyIntent.StopAllCampaignsOnReply {
		t.Fatal("stop_all_campaigns_on_reply defaults on, want off")
	}
	f := newReplyHoldService(false)
	if xerr := f.svc.ProcessIncomingReply(context.Background(), f.accountID,
		replyFromLead(f.accountID, "Re: Hello", "Sounds good, let's talk Tuesday.", nil)); xerr != nil {
		t.Fatal(xerr)
	}
	if len(f.rec.except) != 0 {
		t.Fatalf("HoldLeadElsewhere called %d times with the setting off", len(f.rec.except))
	}
}

// An auto-reply is not the person answering, so it never holds other
// campaigns; an away message holds the lead through its own path instead.
func TestAutoReplyHoldsNoOtherCampaigns(t *testing.T) {
	f := newReplyHoldService(true)
	if xerr := f.svc.ProcessIncomingReply(context.Background(), f.accountID,
		replyFromLead(f.accountID, "Automatic reply: Hello", "I am out of the office until Monday.",
			[]string{"Auto-Submitted:auto-replied"})); xerr != nil {
		t.Fatal(xerr)
	}
	if f.rec.replied != 0 || f.rec.classified != 1 {
		t.Fatalf("auto-reply not read as automated: replied=%d classified=%d", f.rec.replied, f.rec.classified)
	}
	if len(f.rec.except) != 0 {
		t.Fatalf("an auto-reply held other campaigns %d times", len(f.rec.except))
	}
	if f.rec.everywhere != 1 {
		t.Fatalf("an out-of-office auto-reply held the lead everywhere %d times, want once", f.rec.everywhere)
	}
}
