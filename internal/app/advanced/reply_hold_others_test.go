package advanced

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// elsewhereRecorder records the holds a reply writes across campaigns.
type elsewhereRecorder struct {
	*incomingReplyProgressRepo
	except     []uuid.UUID
	until      *time.Time
	source     string
	everywhere int
}

func (r *elsewhereRecorder) HoldLeadElsewhere(_ context.Context, _, except uuid.UUID, until *time.Time, _, source string) ([]uuid.UUID, error) {
	r.except = append(r.except, except)
	r.until, r.source = until, source
	return []uuid.UUID{uuid.New()}, nil
}

func (r *elsewhereRecorder) HoldLeadEverywhere(context.Context, uuid.UUID, *time.Time, string, string) ([]uuid.UUID, error) {
	r.everywhere++
	return nil, nil
}

func newReplyHoldService(stopAll bool) (*service, *elsewhereRecorder, uuid.UUID, uuid.UUID) {
	orgID, accountID, contactID := uuid.New(), uuid.New(), uuid.New()
	account := &models.Email{ID: accountID, OrganizationID: &orgID, Email: "owner@acme.test"}
	svc, progress := newIncomingReplyService(account, &models.Contact{ID: contactID, Email: "lead@example.com"}, contactID)
	progress.advanced.stopAll = stopAll
	rec := &elsewhereRecorder{incomingReplyProgressRepo: progress}
	svc.campaignProgressRepo = rec
	return svc, rec, accountID, *svc.taskRepo.(incomingReplyTaskRepo).campaign.CampaignID
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
	svc, rec, accountID, answered := newReplyHoldService(true)
	if xerr := svc.ProcessIncomingReply(context.Background(), accountID,
		replyFromLead(accountID, "Re: Hello", "Sounds good, let's talk Tuesday.", nil)); xerr != nil {
		t.Fatal(xerr)
	}
	if len(rec.except) != 1 || rec.except[0] != answered {
		t.Fatalf("HoldLeadElsewhere excepts %v, want once for the answered campaign %s", rec.except, answered)
	}
	if rec.source != models.LeadHoldSourceInboxTagging {
		t.Fatalf("hold source = %q, want an automatic hold a member can resume", rec.source)
	}
	if rec.until == nil || time.Until(*rec.until) < 300*24*time.Hour {
		t.Fatalf("hold lifts at %v, want about a year out", rec.until)
	}
}

// Off, the default, a reply stops only the campaign it answered.
func TestHumanReplyHoldsNothingElseByDefault(t *testing.T) {
	if models.DefaultAdvancedOutreachSettings().ReplyIntent.StopAllCampaignsOnReply {
		t.Fatal("stop_all_campaigns_on_reply defaults on, want off")
	}
	svc, rec, accountID, _ := newReplyHoldService(false)
	if xerr := svc.ProcessIncomingReply(context.Background(), accountID,
		replyFromLead(accountID, "Re: Hello", "Sounds good, let's talk Tuesday.", nil)); xerr != nil {
		t.Fatal(xerr)
	}
	if len(rec.except) != 0 {
		t.Fatalf("HoldLeadElsewhere called %d times with the setting off", len(rec.except))
	}
}

// An auto-reply is not the person answering, so it never holds other campaigns.
func TestAutoReplyHoldsNoOtherCampaigns(t *testing.T) {
	svc, rec, accountID, _ := newReplyHoldService(true)
	if xerr := svc.ProcessIncomingReply(context.Background(), accountID,
		replyFromLead(accountID, "Automatic reply: Hello", "I am out of the office until Monday.",
			[]string{"Auto-Submitted:auto-replied"})); xerr != nil {
		t.Fatal(xerr)
	}
	if rec.replied != 0 || rec.classified != 1 {
		t.Fatalf("auto-reply not read as automated: replied=%d classified=%d", rec.replied, rec.classified)
	}
	if len(rec.except) != 0 {
		t.Fatalf("an auto-reply held other campaigns %d times", len(rec.except))
	}
}
