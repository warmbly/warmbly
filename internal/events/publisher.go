package events

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/emsg"
	"github.com/warmbly/warmbly/internal/repository"
)

// Publisher handles event publishing to Kafka and S3 storage
type Publisher interface {
	// Storage
	StoreEmailBody(ctx context.Context, taskID, orgID uuid.UUID, plainText, htmlBody string) (string, error)

	// Email events - sends to worker via Kafka
	PublishSendEmail(ctx context.Context, workerID uuid.UUID, params *SendEmailParams) error

	// Analytics events
	PublishEmailSent(ctx context.Context, task *repository.Task, account *models.Email, campaign *models.Campaign, contact *models.Contact, sequence *models.Sequence) error
	PublishWarmupEmailSent(ctx context.Context, task *repository.Task, senderAccount *models.Email, targetAccount *models.Email, isReply bool) error

	// Warmup action events
	PublishWarmupAction(ctx context.Context, workerID uuid.UUID, action *models.WarmupEmailAction) error
	// PublishMessageSeen relays a read/unread change made in the unibox out to
	// the mailbox provider.
	PublishMessageSeen(ctx context.Context, workerID uuid.UUID, action *models.MessageSeenAction) error

	// Worker change notifications
	PublishAddEmail(ctx context.Context, workerID uuid.UUID, email *models.AddWorkerEmail) error
	PublishRemoveEmail(ctx context.Context, workerID uuid.UUID, remove *models.RemoveWorkerEmail) error

	// PublishEmailValidation sends a mailbox credential-validation request to the
	// worker. Routed through the bus+codec like every other worker event, so it
	// works on both Kafka and NATS.
	PublishEmailValidation(ctx context.Context, workerID string, body models.EventWorkerEmailValidation) error
}

// SendEmailParams contains parameters for publishing a send email event
type SendEmailParams struct {
	TaskID         uuid.UUID
	EmailID        uuid.UUID
	OrgID          uuid.UUID
	To             []string
	CC             []string
	BCC            []string
	InReplyTo      string
	ThreadID       string
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
	// FromName is the mailbox display name at publish time. It travels in the
	// emsg blob for the same reason attachments do, and lets a renamed mailbox
	// send under its new name without a worker reload.
	FromName string
	// FromEmail is the verified provider alias the mailbox sends as, resolved
	// at publish time from the same row. Empty means the mailbox's own
	// address, which is every mailbox that has not picked an alias.
	FromEmail string
}

// sender is the identity a message goes out under, carried together because
// the two halves are one decision and are read as a pair.
type sender struct {
	Name  string
	Email string
}

type publisher struct {
	bus           eventbus.EventBus
	storageClient storage.Store
	codec         codec.Codec
	cipherService cipher.CipherService

	// Last time a publish failure on each topic was reported. See
	// reportPublishFailure.
	failuresMu  sync.Mutex
	lastFailure map[string]time.Time
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
		lastFailure:   map[string]time.Time{},
	}
}

// PublishSendEmail stores email body in S3 and publishes a send email event to the worker
func (p *publisher) PublishSendEmail(ctx context.Context, workerID uuid.UUID, params *SendEmailParams) error {
	// Store email body (and attachment refs) in S3. The attachment refs ride
	// inside the emsg blob so the worker receives them via BodyS3Key without any
	// change to the Avro event contract.
	if p.bus == nil {
		return fmt.Errorf("event bus not configured; cannot hand send %s to a worker", params.TaskID)
	}
	if p.storageClient == nil {
		// The worker reads the body from object storage; without it the send
		// would be published body-less and fail there.
		return fmt.Errorf("object storage not configured; cannot hand send %s to a worker", params.TaskID)
	}
	s3Key, err := p.storeEmailBody(ctx, params.TaskID, params.OrgID, params.BodyPlain, params.BodyHTML, params.Attachments,
		sender{Name: params.FromName, Email: params.FromEmail})
	if err != nil {
		return fmt.Errorf("failed to store email body: %w", err)
	}

	// Encrypt subject
	subject := params.Subject
	if p.cipherService != nil {
		c, cerr := p.cipherService.Cipher(ctx, params.OrgID)
		if cerr != nil {
			return fmt.Errorf("failed to get cipher: %w", cerr)
		}
		encSubject, cerr := c.Encrypt(ctx, params.Subject)
		if cerr != nil {
			return fmt.Errorf("failed to encrypt subject: %w", cerr)
		}
		subject = encSubject
	}

	// Parent is what makes a reply land inside its conversation. ThreadID is
	// the provider-side handle (Gmail appends to a thread only when it is set);
	// MessageID is the RFC Message-ID the recipient's own client threads on.
	// Both fields already exist on the event, so this adds no schema change.
	var parent *models.EmailParent
	if params.ThreadID != "" || params.InReplyTo != "" {
		parent = &models.EmailParent{
			ThreadID:  params.ThreadID,
			MessageID: strings.Trim(params.InReplyTo, "<>"),
		}
	}

	// Create SendEmail message for worker
	sendEmail := &models.SendEmail{
		TaskID:         params.TaskID,
		EmailID:        params.EmailID,
		OrgID:          params.OrgID,
		To:             params.To,
		Cc:             params.CC,
		Bcc:            params.BCC,
		Subject:        subject,
		BodyS3Key:      s3Key,
		MessageID:      params.MessageID,
		InReplyTo:      params.InReplyTo,
		Parent:         parent,
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
func (p *publisher) StoreEmailBody(ctx context.Context, taskID, orgID uuid.UUID, plainText, htmlBody string) (string, error) {
	return p.storeEmailBody(ctx, taskID, orgID, plainText, htmlBody, nil, sender{})
}

// storeEmailBody encodes the email body plus attachment refs into the emsg blob
// and uploads it to object storage, returning the S3 key. Bodies are encrypted
// with the organization DEK before encoding; attachment refs and the from name
// are plaintext metadata (the bytes refs point to are stored separately and the
// worker fetches them by key).
func (p *publisher) storeEmailBody(ctx context.Context, taskID, orgID uuid.UUID, plainText, htmlBody string, attachments []models.AttachmentRef, from sender) (string, error) {
	if p.storageClient == nil {
		return "", nil
	}

	encPlainText := plainText
	encHTMLBody := htmlBody
	if p.cipherService != nil {
		c, err := p.cipherService.Cipher(ctx, orgID)
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
		FromName:  from.Name,
		FromEmail: from.Email,
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

// PublishMessageSeen relays a unibox read/unread change to the worker holding
// the mailbox. Keyed by mailbox like every other per-mailbox event, so one
// mailbox's relays stay in order relative to each other.
func (p *publisher) PublishMessageSeen(ctx context.Context, workerID uuid.UUID, action *models.MessageSeenAction) error {
	workerEvent := models.WorkerEvent{
		Type: models.WorkerEventTypeMessageSeen,
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

// PublishEmailValidation publishes a credential-validation request to the worker.
func (p *publisher) PublishEmailValidation(ctx context.Context, workerID string, body models.EventWorkerEmailValidation) error {
	workerEvent := models.WorkerEvent{
		Type: models.WorkerEventTypeEmailValidation,
		Body: body,
	}
	return p.publish(kafka.GetWorkerTopic(workerID), body.OrgID.String(), workerEvent)
}

// publishFailureInterval is how often one topic's publish failure is reported.
// A topic the broker persistently refuses (a missing ACL, a name it will not
// auto-create) fails on every message, and reporting each one buried every
// other issue under hundreds of copies of the same sentence.
const publishFailureInterval = 5 * time.Minute

// reportPublishFailure reports at most one failure per topic per interval. The
// caller still gets the error, so nothing downstream changes.
func (p *publisher) reportPublishFailure(topic string, err error) {
	now := time.Now()
	p.failuresMu.Lock()
	last, seen := p.lastFailure[topic]
	if seen && now.Sub(last) < publishFailureInterval {
		p.failuresMu.Unlock()
		return
	}
	p.lastFailure[topic] = now
	p.failuresMu.Unlock()
	errs.CaptureException(fmt.Errorf("failed to publish event: %w", err))
}

// publish serializes (via codec) and publishes (via bus) an event.
func (p *publisher) publish(topic, key string, event interface{}) error {
	if p.bus == nil {
		// Bus not configured, skip publishing.
		return nil
	}

	if p.codec == nil {
		errs.CaptureException(fmt.Errorf("codec not configured, topic: %s", topic))
		return fmt.Errorf("codec not configured")
	}

	ctx := context.Background()
	data, err := p.codec.Serialize(ctx, topic, event)
	if err != nil {
		errs.CaptureException(fmt.Errorf("failed to serialize event: %w", err))
		return err
	}
	if err := p.bus.Publish(ctx, topic, key, data); err != nil {
		// A bus closed under us is shutdown, not a fault worth an issue.
		if !errors.Is(err, eventbus.ErrBusClosed) {
			p.reportPublishFailure(topic, err)
		}
		return err
	}
	return nil
}
