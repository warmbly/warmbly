package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type completedCampaignRepo struct {
	repository.CampaignRepository
	org      uuid.UUID
	reopened int
}

func (r *completedCampaignRepo) GetByID(_ context.Context, id uuid.UUID) (*models.Campaign, error) {
	return &models.Campaign{ID: id, OrganizationID: &r.org, Status: "completed"}, nil
}

func (*completedCampaignRepo) DecrementCampaignDailySend(context.Context, uuid.UUID, time.Time, bool) error {
	return nil
}

func (r *completedCampaignRepo) ReopenAfterSendFailure(context.Context, uuid.UUID) (bool, error) {
	r.reopened++
	return true, nil
}

type keepingAdvanced struct {
	copyAdvanced
	keep bool
}

func (a *keepingAdvanced) IngestDeliverabilityEvent(ctx context.Context, org uuid.UUID, req *models.IngestDeliverabilityEventRequest) *errx.Error {
	req.AddressKept = a.keep
	return a.copyAdvanced.IngestDeliverabilityEvent(ctx, org, req)
}

// A refusal that kept the lead leaves its step to retry, so a campaign that
// completed meanwhile reopens; a refusal that dropped the lead does not.
func TestKeptRefusalReopensCompletedCampaign(t *testing.T) {
	for _, tc := range []struct {
		name       string
		keep       bool
		wantReopen int
	}{
		{"address kept", true, 1},
		{"address dropped", false, 0},
	} {
		campaign, lead, step, taskID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		campaigns := &completedCampaignRepo{org: uuid.New()}
		adv := &keepingAdvanced{keep: tc.keep}
		s := &JobsService{
			TaskRepo: &evidenceTaskRepo{
				task: &repository.Task{ID: taskID, TaskType: "campaign", EmailAccountID: uuid.New(), Status: "completed"},
				ct:   &repository.CampaignTask{TaskID: taskID, CampaignID: &campaign, ContactID: &lead, SequenceID: &step},
			},
			CampaignRepo:         campaigns,
			CampaignProgressRepo: &copyProgressRepo{},
			ContactRepo:          copyContactRepo{email: "lead@example.com"},
			AdvancedService:      adv,
		}
		if err := s.HandleEmailFailed(context.Background(), models.SendEmailResult{
			TaskID: taskID,
			Error: &models.EmailSendError{
				Code:      string(errx.MailErrorCodeRecipientRejected),
				Message:   "The mail server rejected the recipient: The SMTP server refused this recipient (SMTP rcpt to status 550); enhanced status 5.7.1",
				Recipient: "lead@example.com",
			},
		}); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(adv.events) != 1 {
			t.Fatalf("%s: %d bounce events, want 1", tc.name, len(adv.events))
		}
		if campaigns.reopened != tc.wantReopen {
			t.Fatalf("%s: reopened %d times, want %d", tc.name, campaigns.reopened, tc.wantReopen)
		}
	}
}
