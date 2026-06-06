package events

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emsg"
	"github.com/warmbly/warmbly/internal/repository"
)

// Publisher handles event publishing to Kafka and S3 storage
type Publisher interface {
	// Storage
	StoreEmailBody(ctx context.Context, taskID, userID uuid.UUID, plainText, htmlBody string) (string, error)

	// Email events - sends to worker via Kafka
	PublishSendEmail(ctx context.Context, workerID uuid.UUID, params *SendEmailParams) error

	// Analytics events
	PublishEmailSent(ctx context.Context, task *repository.Task, account *models.Email, campaign *models.Campaign, contact *models.Contact, sequence *models.Sequence) error
	PublishWarmupEmailSent(ctx context.Context, task *repository.Task, senderAccount *models.Email, targetAccount *models.Email, isReply bool) error

	// Warmup action events
	PublishWarmupAction(ctx context.Context, workerID uuid.UUID, action *models.WarmupEmailAction) error

	// Worker change notifications
	PublishAddEmail(ctx context.Context, workerID uuid.UUID, email *models.AddWorkerEmail) error
	PublishRemoveEmail(ctx context.Context, workerID uuid.UUID, remove *models.RemoveWorkerEmail) error
}

// SendEmailParams contains parameters for publishing a send email event
type SendEmailParams struct {
	TaskID         uuid.UUID
	EmailID        uuid.UUID
	UserID         uuid.UUID
	To             []string
	CC             []string
	BCC            []string
	InReplyTo      string
	Subject        string
	MessageID      string
	BodyPlain      string
	BodyHTML       string
	IsWarmup       bool
	TrackingInfo   *models.TrackingInfo
	WarmupToken    string
	UnsubscribeURL string
	// Attachments are file refs put into the emsg EmailBlob inside the S3 body
	// object (reached by the worker via BodyS3Key). They are deliberately NOT
	// added to models.SendEmail / the Avro event — the Kafka contract is fixed.
	Attachments []models.AttachmentRef
}

type publisher struct {
	bus           eventbus.EventBus
	storageClient storage.Store
	codec         codec.Codec
	cipherService cipher.CipherService
}

// NewPublisher creates a new event publisher. bus is the transport (Kafka or
// NATS); codec is the serialization (Avro or JSON). Both come from FromEnv
// constructors in cmd/*/main.go.
func NewPublisher(bus eventbus.EventBus, storageClient storage.Store, c codec.Codec, cipherService cipher.CipherService) Publisher {
	return &publisher{
		bus:           bus,
		storageClient: storageClient,
		codec:         c,
		cipherService: cipherService,
	}
}

// PublishSendEmail stores email body in S3 and publishes a send email event to the worker
func (p *publisher) PublishSendEmail(ctx context.Context, workerID uuid.UUID, params *SendEmailParams) error {
	// Store email body (and attachment refs) in S3. The attachment refs ride
	// inside the emsg blob so the worker receives them via BodyS3Key without any
	// change to the Avro event contract.
	s3Key, err := p.storeEmailBody(ctx, params.TaskID, params.UserID, params.BodyPlain, params.BodyHTML, params.Attachments)
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
		TaskID:         params.TaskID,
		EmailID:        params.EmailID,
		UserID:         params.UserID,
		To:             params.To,
		Cc:             params.CC,
		Bcc:            params.BCC,
		Subject:        subject,
		BodyS3Key:      s3Key,
		MessageID:      params.MessageID,
		InReplyTo:      params.InReplyTo,
		IsWarmup:       params.IsWarmup,
		TrackingInfo:   params.TrackingInfo,
		WarmupToken:    params.WarmupToken,
		UnsubscribeURL: params.UnsubscribeURL,
	}

	// Publish worker event
	workerEvent := models.WorkerEvent{
		Type: models.WorkerEventTypeSendEmail,
		Body: sendEmail,
	}

	workerTopic := kafka.GetWorkerTopic(workerID.String())
	return p.publish(workerTopic, params.TaskID.String(), workerEvent)
}

// StoreEmailBody stores email body in S3 and returns the S3 key. It is the
// interface method; the attachment-aware path goes through storeEmailBody.
func (p *publisher) StoreEmailBody(ctx context.Context, taskID, userID uuid.UUID, plainText, htmlBody string) (string, error) {
	return p.storeEmailBody(ctx, taskID, userID, plainText, htmlBody, nil)
}

// storeEmailBody encodes the email body plus attachment refs into the emsg blob
// and uploads it to object storage, returning the S3 key. Bodies are encrypted
// per-user before encoding; attachment refs are plaintext metadata (the bytes
// they point to are stored separately and the worker fetches them by key).
func (p *publisher) storeEmailBody(ctx context.Context, taskID, userID uuid.UUID, plainText, htmlBody string, attachments []models.AttachmentRef) (string, error) {
	if p.storageClient == nil {
		return "", nil
	}

	encPlainText := plainText
	encHTMLBody := htmlBody
	if p.cipherService != nil {
		c, err := p.cipherService.Cipher(ctx, userID)
		if err != nil {
			return "", fmt.Errorf("failed to get cipher: %w", err)
		}
		if plainText != "" {
			encPlainText, err = c.Encrypt(ctx, plainText)
			if err != nil {
				return "", fmt.Errorf("failed to encrypt plain body: %w", err)
			}
		}
		if htmlBody != "" {
			encHTMLBody, err = c.Encrypt(ctx, htmlBody)
			if err != nil {
				return "", fmt.Errorf("failed to encrypt html body: %w", err)
			}
		}
	}

	// Create email blob. Attachment refs are carried inside the blob so the
	// worker can fetch each file's bytes from object storage at send time.
	blob := &emsg.EmailBlob{
		PlainText: []byte(encPlainText),
		HTMLBody:  []byte(encHTMLBody),
	}
	for _, a := range attachments {
		blob.Attachments = append(blob.Attachments, emsg.Attachment{
			S3Key:    a.S3Key,
			Filename: a.Filename,
			MimeType: a.MimeType,
		})
	}

	data, err := blob.EncodeBinary()
	if err != nil {
		return "", fmt.Errorf("failed to encode email blob: %w", err)
	}

	// Generate S3 key
	s3Key := fmt.Sprintf("emails/%s/%s.emsg", time.Now().Format("2006/01/02"), taskID.String())

	// Upload to storage
	if err := p.storageClient.Put(ctx, s3Key, bytes.NewReader(data), "application/octet-stream"); err != nil {
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

// publish serializes (via codec) and publishes (via bus) an event.
func (p *publisher) publish(topic, key string, event interface{}) error {
	if p.bus == nil {
		// Bus not configured, skip publishing.
		return nil
	}

	if p.codec == nil {
		sentry.CaptureException(fmt.Errorf("codec not configured, topic: %s", topic))
		return fmt.Errorf("codec not configured")
	}
	if p.bus == nil {
		sentry.CaptureException(fmt.Errorf("event bus not configured, topic: %s", topic))
		return fmt.Errorf("event bus not configured")
	}

	ctx := context.Background()
	data, err := p.codec.Serialize(ctx, topic, event)
	if err != nil {
		sentry.CaptureException(fmt.Errorf("failed to serialize event: %w", err))
		return err
	}
	if err := p.bus.Publish(ctx, topic, key, data); err != nil {
		sentry.CaptureException(fmt.Errorf("failed to publish event: %w", err))
		return err
	}
	return nil
}
