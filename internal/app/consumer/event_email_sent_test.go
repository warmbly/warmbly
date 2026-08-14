package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/warmbly/warmbly/internal/app/confenge"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type emailSentTaskRepo struct {
	repository.TaskRepository
	task *repository.CampaignTask
}

func (r emailSentTaskRepo) GetCampaignTask(context.Context, uuid.UUID) (*repository.CampaignTask, error) {
	return r.task, nil
}

type emailSentCampaignRepo struct {
	repository.CampaignRepository
	campaign *models.Campaign
}

func (r emailSentCampaignRepo) GetByID(context.Context, uuid.UUID) (*models.Campaign, error) {
	return r.campaign, nil
}

type emailSentCompletion struct {
	called     int
	orgID      uuid.UUID
	campaignID uuid.UUID
	contactID  uuid.UUID
	sequenceID uuid.UUID
	messageID  string
	err        error
}

func (c *emailSentCompletion) CompleteCampaignEmail(_ context.Context, orgID, campaignID, contactID, sequenceID uuid.UUID, providerMessageID string) error {
	c.called++
	c.orgID = orgID
	c.campaignID = campaignID
	c.contactID = contactID
	c.sequenceID = sequenceID
	c.messageID = providerMessageID
	return c.err
}

func TestHandleEmailSentProjectsOnlyProviderSuccess(t *testing.T) {
	taskID := uuid.New()
	orgID := uuid.New()
	campaignID := uuid.New()
	contactID := uuid.New()
	sequenceID := uuid.New()
	completion := &emailSentCompletion{}
	svc := &JobsService{
		TaskRepo:      emailSentTaskRepo{task: &repository.CampaignTask{TaskID: taskID, CampaignID: &campaignID, ContactID: &contactID, SequenceID: &sequenceID}},
		CampaignRepo:  emailSentCampaignRepo{campaign: &models.Campaign{ID: campaignID, OrganizationID: &orgID, Name: "CONFENGE pilot"}},
		ConfengeSends: completion,
	}

	require.NoError(t, svc.HandleEmailSent(context.Background(), &models.SendEmailResult{
		TaskID: taskID, Success: true, MessageID: "rfc-message", ProviderMsgID: "provider-message",
	}))
	require.Equal(t, 1, completion.called)
	require.Equal(t, orgID, completion.orgID)
	require.Equal(t, campaignID, completion.campaignID)
	require.Equal(t, contactID, completion.contactID)
	require.Equal(t, sequenceID, completion.sequenceID)
	require.Equal(t, "provider-message", completion.messageID)
}

func TestHandleEmailSentDoesNotPoisonOrdinaryCampaignEvents(t *testing.T) {
	taskID, orgID, campaignID, contactID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	completion := &emailSentCompletion{err: confenge.ErrCampaignTouchpointNotFound}
	svc := &JobsService{
		TaskRepo:      emailSentTaskRepo{task: &repository.CampaignTask{TaskID: taskID, CampaignID: &campaignID, ContactID: &contactID}},
		CampaignRepo:  emailSentCampaignRepo{campaign: &models.Campaign{ID: campaignID, OrganizationID: &orgID, Name: "Ordinary campaign"}},
		ConfengeSends: completion,
	}

	require.NoError(t, svc.HandleEmailSent(context.Background(), &models.SendEmailResult{TaskID: taskID, Success: true}))
	require.Equal(t, 1, completion.called)
	completion.err = errors.New("database unavailable")
	require.Error(t, svc.HandleEmailSent(context.Background(), &models.SendEmailResult{TaskID: taskID, Success: true}))
}

func TestHandleEmailSentRejectsNonSuccessResult(t *testing.T) {
	svc := &JobsService{ConfengeSends: &emailSentCompletion{}}
	require.Error(t, svc.HandleEmailSent(context.Background(), &models.SendEmailResult{TaskID: uuid.New()}))
}

func TestHandleEmailSentIgnoresNonCampaignTask(t *testing.T) {
	completion := &emailSentCompletion{}
	svc := &JobsService{
		TaskRepo:      emailSentTaskRepo{},
		CampaignRepo:  emailSentCampaignRepo{},
		ConfengeSends: completion,
	}
	require.NoError(t, svc.HandleEmailSent(context.Background(), &models.SendEmailResult{TaskID: uuid.New(), Success: true}))
	require.Zero(t, completion.called)
}
