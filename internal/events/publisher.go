package events

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emsg"
	"github.com/warmbly/warmbly/internal/repository"
)

// Publisher handles event publishing to Kafka and S3 storage
type Publisher interface {
	// Storage
	StoreEmailBody(ctx context.Context, taskID uuid.UUID, plainText, htmlBody string) (string, error)

	// Email events - sends to worker via Kafka
	PublishSendEmail(ctx context.Context, workerID uuid.UUID, params *SendEmailParams) error

	// Analytics events
	PublishEmailSent(ctx context.Context, task *repository.Task, account *models.Email, campaign *models.Campaign, contact *models.Contact, sequence *models.Sequence) error
	PublishWarmupEmailSent(ctx context.Context, task *repository.Task, senderAccount *models.Email, targetAccount *models.Email, isReply bool) error

	// Campaign events
	PublishCampaignProgress(ctx context.Context, campaignID uuid.UUID, progress *repository.CampaignProgress) error

	// Task events
	PublishTaskCreated(ctx context.Context, task *repository.Task) error
	PublishTaskCompleted(ctx context.Context, task *repository.Task) error

	// Warmup action events
	PublishWarmupAction(ctx context.Context, workerID uuid.UUID, action *models.WarmupEmailAction) error

	// Worker change notifications
	PublishAddEmail(ctx context.Context, workerID uuid.UUID, email *models.AddWorkerEmail) error
	PublishRemoveEmail(ctx context.Context, workerID uuid.UUID, remove *models.RemoveWorkerEmail) error
}

// SendEmailParams contains parameters for publishing a send email event
type SendEmailParams struct {
	TaskID       uuid.UUID
	EmailID      uuid.UUID
	UserID       uuid.UUID
	To           []string
	CC           []string
	BCC          []string
	InReplyTo    string
	Subject      string
	MessageID    string
	BodyPlain    string
	BodyHTML     string
	IsWarmup     bool
	TrackingInfo *models.TrackingInfo
	WarmupToken  string
}

type publisher struct {
	producer      *kafka.Producer
	storageClient *storage.Client
	avrov2        *kafka.Avrov2
	cipherService cipher.CipherService
}

// NewPublisher creates a new event publisher
func NewPublisher(producer *kafka.Producer, storageClient *storage.Client, avrov2 *kafka.Avrov2, cipherService cipher.CipherService) Publisher {
	return &publisher{
		producer:      producer,
		storageClient: storageClient,
		avrov2:        avrov2,
		cipherService: cipherService,
	}
}

// PublishSendEmail stores email body in S3 and publishes a send email event to the worker
func (p *publisher) PublishSendEmail(ctx context.Context, workerID uuid.UUID, params *SendEmailParams) error {
	// Store email body in S3
	s3Key, err := p.StoreEmailBody(ctx, params.TaskID, params.BodyPlain, params.BodyHTML)
	if err != nil {
		return fmt.Errorf("failed to store email body: %w", err)
	}

	// Encrypt subject
	subject := params.Subject
	if p.cipherService != nil {
		c, cerr := p.cipherService.Cipher(ctx, params.UserID)
		if cerr != nil {
			return fmt.Errorf("failed to get cipher: %w", cerr)
		}
		encSubject, cerr := c.Encrypt(ctx, params.Subject)
		if cerr != nil {
			return fmt.Errorf("failed to encrypt subject: %w", cerr)
		}
		subject = encSubject
	}

	// Create SendEmail message for worker
	sendEmail := &models.SendEmail{
		TaskID:       params.TaskID,
		EmailID:      params.EmailID,
		UserID:       params.UserID,
		To:           params.To,
		Cc:           params.CC,
		Bcc:          params.BCC,
		Subject:      subject,
		BodyS3Key:    s3Key,
		MessageID:    params.MessageID,
		InReplyTo:    params.InReplyTo,
		IsWarmup:     params.IsWarmup,
		TrackingInfo: params.TrackingInfo,
		WarmupToken:  params.WarmupToken,
	}

	// Publish worker event
	workerEvent := models.WorkerEvent{
		Type: models.WorkerEventTypeSendEmail,
		Body: sendEmail,
	}

	workerTopic := kafka.GetWorkerTopic(workerID.String())
	return p.publish(workerTopic, params.TaskID.String(), workerEvent)
}

// StoreEmailBody stores email body in S3 and returns the S3 key
func (p *publisher) StoreEmailBody(ctx context.Context, taskID uuid.UUID, plainText, htmlBody string) (string, error) {
	if p.storageClient == nil {
		return "", nil
	}

	// Create email blob
	blob := &emsg.EmailBlob{
		PlainText: []byte(plainText),
		HTMLBody:  []byte(htmlBody),
	}

	data, err := blob.EncodeBinary()
	if err != nil {
		return "", fmt.Errorf("failed to encode email blob: %w", err)
	}

	// Generate S3 key
	s3Key := fmt.Sprintf("emails/%s/%s.emsg", time.Now().Format("2006/01/02"), taskID.String())

	// Upload to S3
	_, err = p.storageClient.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(p.storageClient.Bucket),
		Key:         aws.String(s3Key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/octet-stream"),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload email body to S3: %w", err)
	}

	return s3Key, nil
}

// PublishEmailSent publishes an email sent event
func (p *publisher) PublishEmailSent(
	ctx context.Context,
	task *repository.Task,
	account *models.Email,
	campaign *models.Campaign,
	contact *models.Contact,
	sequence *models.Sequence,
) error {
	event := EmailSentEvent{
		EventType:  EventTypeEmailSent,
		TaskID:     task.ID,
		AccountID:  account.ID,
		CampaignID: campaign.ID,
		ContactID:  contact.ID,
		SequenceID: sequence.ID,
		MessageID:  task.MessageID,
		Recipient:  contact.Email,
		Subject:    sequence.Subject,
		SentAt:     time.Now(),
	}

	return p.publish(TopicEmailEvents, task.ID.String(), event)
}

// PublishWarmupEmailSent publishes a warmup email sent event
func (p *publisher) PublishWarmupEmailSent(
	ctx context.Context,
	task *repository.Task,
	senderAccount *models.Email,
	targetAccount *models.Email,
	isReply bool,
) error {
	event := WarmupEmailSentEvent{
		EventType:       EventTypeWarmupEmailSent,
		TaskID:          task.ID,
		SenderAccountID: senderAccount.ID,
		TargetAccountID: targetAccount.ID,
		MessageID:       task.MessageID,
		IsReply:         isReply,
		SentAt:          time.Now(),
	}

	return p.publish(TopicWarmupEvents, task.ID.String(), event)
}

// PublishCampaignProgress publishes a campaign progress update
func (p *publisher) PublishCampaignProgress(
	ctx context.Context,
	campaignID uuid.UUID,
	progress *repository.CampaignProgress,
) error {
	event := CampaignProgressEvent{
		EventType:     EventTypeCampaignProgress,
		CampaignID:    campaignID,
		TotalContacts: progress.TotalContacts,
		EmailsSent:    progress.EmailsSent,
		EmailsPending: progress.EmailsPending,
		EmailsOpened:  progress.EmailsOpened,
		EmailsClicked: progress.EmailsClicked,
		EmailsReplied: progress.EmailsReplied,
		EmailsBounced: progress.EmailsBounced,
		UpdatedAt:     time.Now(),
	}

	return p.publish(TopicCampaignEvents, campaignID.String(), event)
}

// PublishTaskCreated publishes a task created event
func (p *publisher) PublishTaskCreated(ctx context.Context, task *repository.Task) error {
	scheduledAt := time.Time{}
	if task.ScheduledAt != nil {
		scheduledAt = *task.ScheduledAt
	}

	event := TaskEvent{
		EventType:      EventTypeTaskCreated,
		TaskID:         task.ID,
		TaskType:       task.TaskType,
		EmailAccountID: task.EmailAccountID,
		Status:         task.Status,
		ScheduledAt:    scheduledAt,
		Timestamp:      time.Now(),
	}

	return p.publish(TopicTaskEvents, task.ID.String(), event)
}

// PublishTaskCompleted publishes a task completed event
func (p *publisher) PublishTaskCompleted(ctx context.Context, task *repository.Task) error {
	scheduledAt := time.Time{}
	if task.ScheduledAt != nil {
		scheduledAt = *task.ScheduledAt
	}

	event := TaskEvent{
		EventType:      EventTypeTaskCompleted,
		TaskID:         task.ID,
		TaskType:       task.TaskType,
		EmailAccountID: task.EmailAccountID,
		Status:         task.Status,
		ScheduledAt:    scheduledAt,
		Timestamp:      time.Now(),
	}

	return p.publish(TopicTaskEvents, task.ID.String(), event)
}

// PublishWarmupAction publishes a warmup action event to the worker
func (p *publisher) PublishWarmupAction(ctx context.Context, workerID uuid.UUID, action *models.WarmupEmailAction) error {
	workerEvent := models.WorkerEvent{
		Type: models.WorkerEventTypeWarmupAction,
		Body: action,
	}

	workerTopic := kafka.GetWorkerTopic(workerID.String())
	return p.publish(workerTopic, action.EmailID.String(), workerEvent)
}

// PublishAddEmail publishes an add email event to the worker
func (p *publisher) PublishAddEmail(ctx context.Context, workerID uuid.UUID, email *models.AddWorkerEmail) error {
	workerEvent := models.WorkerEvent{
		Type: models.WorkerEventTypeAddEmail,
		Body: email,
	}

	workerTopic := kafka.GetWorkerTopic(workerID.String())
	return p.publish(workerTopic, email.ID.String(), workerEvent)
}

// PublishRemoveEmail publishes a remove email event to the worker
func (p *publisher) PublishRemoveEmail(ctx context.Context, workerID uuid.UUID, remove *models.RemoveWorkerEmail) error {
	workerEvent := models.WorkerEvent{
		Type: models.WorkerEventTypeRemoveEmail,
		Body: remove,
	}

	workerTopic := kafka.GetWorkerTopic(workerID.String())
	return p.publish(workerTopic, remove.EmailID, workerEvent)
}

// publish serializes and publishes an event to Kafka using Avro v2
func (p *publisher) publish(topic, key string, event interface{}) error {
	if p.producer == nil {
		// Producer not configured, skip publishing
		return nil
	}

	var data []byte
	var err error

	if p.avrov2 != nil {
		data, err = p.avrov2.Ser.Serialize(topic, event)
	} else {
		// Fallback: this shouldn't happen in production
		sentry.CaptureException(fmt.Errorf("avro v2 serializer not configured, topic: %s", topic))
		return fmt.Errorf("avro v2 serializer not configured")
	}
	if err != nil {
		sentry.CaptureException(fmt.Errorf("failed to serialize event: %w", err))
		return err
	}

	if err := p.producer.Produce(topic, []byte(key), data); err != nil {
		sentry.CaptureException(fmt.Errorf("failed to produce event: %w", err))
		return err
	}

	return nil
}
