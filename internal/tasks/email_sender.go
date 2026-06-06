package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// EmailMessage represents an email to be sent
type EmailMessage struct {
	From           string
	To             []string
	CC             []string
	BCC            []string
	Subject        string
	BodyHTML       string
	BodyPlain      string
	InReplyTo      string
	MessageID      string
	IsWarmup       bool
	Tracking       *models.TrackingInfo
	WarmupToken    string
	UserID         uuid.UUID
	UnsubscribeURL string
	// Attachments are file refs (S3 key + metadata). The worker fetches the
	// bytes from object storage at send time. Refs travel inside the S3 body
	// blob, never the Avro Kafka event.
	Attachments []models.AttachmentRef
}

// EmailSender interface for sending emails via workers
type EmailSender interface {
	Send(ctx context.Context, taskID uuid.UUID, msg EmailMessage, account models.Email) error
}

type emailSender struct {
	emailRepo repository.EmailRepository
	publisher events.Publisher
}

// NewEmailSender creates a new email sender
func NewEmailSender(emailRepo repository.EmailRepository, publisher events.Publisher) EmailSender {
	return &emailSender{
		emailRepo: emailRepo,
		publisher: publisher,
	}
}

// Send publishes an email to the worker service for sending
func (s *emailSender) Send(ctx context.Context, taskID uuid.UUID, msg EmailMessage, account models.Email) error {
	// Get worker ID for this email account
	workerID := account.WorkerID
	if workerID == nil {
		return fmt.Errorf("no worker assigned to email account %s", account.ID)
	}

	// For warmup emails, only use plaintext (no HTML)
	bodyHTML := msg.BodyHTML
	if msg.IsWarmup {
		bodyHTML = ""
	}

	// Create send email params
	params := &events.SendEmailParams{
		TaskID:         taskID,
		EmailID:        account.ID,
		UserID:         msg.UserID,
		To:             msg.To,
		CC:             msg.CC,
		BCC:            msg.BCC,
		InReplyTo:      msg.InReplyTo,
		Subject:        msg.Subject,
		MessageID:      msg.MessageID,
		BodyPlain:      msg.BodyPlain,
		BodyHTML:       bodyHTML,
		IsWarmup:       msg.IsWarmup,
		TrackingInfo:   msg.Tracking,
		WarmupToken:    msg.WarmupToken,
		UnsubscribeURL: msg.UnsubscribeURL,
		Attachments:    msg.Attachments,
	}

	// Publish send email event to worker
	if err := s.publisher.PublishSendEmail(ctx, *workerID, params); err != nil {
		return fmt.Errorf("failed to publish send email event: %w", err)
	}

	return nil
}

// generateMessageID generates a unique Message-ID header
func generateMessageID(fromEmail string) string {
	// Extract domain from email
	parts := strings.Split(fromEmail, "@")
	domain := "localhost"
	if len(parts) == 2 {
		domain = parts[1]
	}

	// Generate unique ID
	return fmt.Sprintf("<%s@%s>", uuid.New().String(), domain)
}
