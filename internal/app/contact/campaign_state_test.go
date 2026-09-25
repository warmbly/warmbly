package contact

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
)

type fakePreviewer struct {
	pv    *scheduler.ContactSendPreview
	calls int
}

func (f *fakePreviewer) PreviewContactSend(context.Context, uuid.UUID, uuid.UUID) (*scheduler.ContactSendPreview, error) {
	f.calls++
	return f.pv, nil
}

// A reply ends the flow only when the router says so: a campaign without
// stop_on_reply, or a reply branch, keeps sending to someone who replied.
func TestFillNextActionAsksTheRouterAboutARepliedLead(t *testing.T) {
	step := uuid.New()
	due := time.Now().Add(48 * time.Hour)
	p := &fakePreviewer{pv: &scheduler.ContactSendPreview{
		Route:     &repository.ContactRoute{Target: &step, DueAt: &due},
		State:     models.NextActionWaiting,
		NotBefore: &due,
	}}
	s := &contactService{previewer: p}
	st := &models.ContactCampaignState{
		LeadStatus: models.LeadStatusReplied,
		Steps:      []models.ContactCampaignStep{{ID: step, Label: "Email 2"}},
	}
	s.fillNextAction(context.Background(), st, uuid.New())

	if p.calls != 1 {
		t.Fatalf("previewer called %d times, want 1", p.calls)
	}
	if st.EndedReason != "" {
		t.Fatalf("EndedReason = %q, want empty while a step is still routed", st.EndedReason)
	}
	if st.Next == nil || st.Next.StepLabel != "Email 2" {
		t.Fatalf("Next = %+v, want the routed step", st.Next)
	}
}

func TestFillNextActionEndsARepliedLeadTheRouterStopped(t *testing.T) {
	p := &fakePreviewer{pv: &scheduler.ContactSendPreview{Route: &repository.ContactRoute{}}}
	s := &contactService{previewer: p}
	st := &models.ContactCampaignState{LeadStatus: models.LeadStatusReplied}
	s.fillNextAction(context.Background(), st, uuid.New())

	if st.Next != nil {
		t.Fatalf("Next = %+v, want nil", st.Next)
	}
	if st.EndedReason != "Replied, sending stopped" {
		t.Fatalf("EndedReason = %q", st.EndedReason)
	}
}

// A reply outranks a failed or undeliverable send in the status, so the
// router's exclusion has to end the flow instead.
func TestFillNextActionEndsARepliedLeadTheRouterExcludes(t *testing.T) {
	step := uuid.New()
	for _, excluded := range []string{"failed", "undeliverable", "bounced", "suppressed"} {
		p := &fakePreviewer{pv: &scheduler.ContactSendPreview{
			Route: &repository.ContactRoute{Target: &step, Excluded: excluded},
			State: models.NextActionBlocked,
		}}
		s := &contactService{previewer: p}
		st := &models.ContactCampaignState{LeadStatus: models.LeadStatusReplied}
		s.fillNextAction(context.Background(), st, uuid.New())
		if st.Next != nil || st.EndedReason == "" {
			t.Errorf("%s: Next = %+v, EndedReason = %q; want ended", excluded, st.Next, st.EndedReason)
		}
	}
}

func TestFillNextActionEndsTerminalLeadsWithoutAPreview(t *testing.T) {
	for _, status := range []string{
		models.LeadStatusUnsubscribed, models.LeadStatusBounced,
		models.LeadStatusFailed, models.LeadStatusUndeliverable,
	} {
		p := &fakePreviewer{}
		s := &contactService{previewer: p}
		st := &models.ContactCampaignState{LeadStatus: status}
		s.fillNextAction(context.Background(), st, uuid.New())
		if p.calls != 0 {
			t.Errorf("%s: previewer called", status)
		}
		if st.EndedReason == "" {
			t.Errorf("%s: no EndedReason", status)
		}
	}
}
