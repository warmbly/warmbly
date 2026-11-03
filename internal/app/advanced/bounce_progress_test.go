package advanced

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func (*suppressionRepo) MarkVariantEvent(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

type ownedCampaigns struct {
	repository.CampaignRepository
	org uuid.UUID
}

func (r ownedCampaigns) GetByID(_ context.Context, id uuid.UUID) (*models.Campaign, error) {
	return &models.Campaign{ID: id, OrganizationID: &r.org}, nil
}

type ownedContacts struct{ repository.ContactRepository }

func (ownedContacts) GetByIDsAndOrganization(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]models.Contact, *errx.Error) {
	return []models.Contact{{ID: ids[0]}}, nil
}

type stepTasks struct {
	repository.TaskRepository
	ct *repository.CampaignTask
}

func (r stepTasks) GetCampaignTask(context.Context, uuid.UUID) (*repository.CampaignTask, error) {
	return r.ct, nil
}

type bouncedProgress struct {
	repository.CampaignProgressRepository
	bounced int
}

func (p *bouncedProgress) RecordEmailBounced(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	p.bounced++
	return nil
}

// A kept bounce leaves the lead in its campaign; an address bounce drops it.
func TestKeptBounceLeavesCampaignProgress(t *testing.T) {
	for _, tc := range []struct {
		reason      string
		wantBounced int
	}{
		{"552 5.2.2 Mailbox full", 0},
		{"550 5.7.1 Recipient address rejected: policy restriction", 0},
		{"550 5.1.1 User unknown", 1},
	} {
		org, campaign, contact, task, step := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
		progress := &bouncedProgress{}
		s := &service{
			repo:                 &suppressionRepo{},
			campaignRepo:         ownedCampaigns{org: org},
			contactRepo:          ownedContacts{},
			taskRepo:             stepTasks{ct: &repository.CampaignTask{TaskID: task, CampaignID: &campaign, ContactID: &contact, SequenceID: &step}},
			campaignProgressRepo: progress,
		}
		req := &models.IngestDeliverabilityEventRequest{
			EventType: models.DeliverabilityEventBounce, RecipientEmail: "lead@example.com", Reason: tc.reason,
			CampaignID: &campaign, ContactID: &contact, TaskID: &task,
		}
		if xerr := s.IngestDeliverabilityEvent(context.Background(), org, req); xerr != nil {
			t.Fatalf("%q: %v", tc.reason, xerr)
		}
		if progress.bounced != tc.wantBounced {
			t.Fatalf("%q: RecordEmailBounced called %d times, want %d", tc.reason, progress.bounced, tc.wantBounced)
		}
		if req.AddressKept != (tc.wantBounced == 0) {
			t.Fatalf("%q: AddressKept = %v", tc.reason, req.AddressKept)
		}
	}
}
