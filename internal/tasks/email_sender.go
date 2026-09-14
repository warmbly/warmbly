package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// EmailMessage represents an email to be sent
type EmailMessage struct {
	From      string
	To        []string
	CC        []string
	BCC       []string
	Subject   string
	BodyHTML  string
	BodyPlain string
	InReplyTo string
	MessageID string
	// ThreadID is the provider-side conversation handle. Gmail only appends to
	// an existing thread when it is set on the outbound message; a matching
	// Subject and In-Reply-To are not enough.
	ThreadID       string
	IsWarmup       bool
	Tracking       *models.TrackingInfo
	WarmupToken    string
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

// WorkerLiveness reports whether a worker is still heartbeating. Satisfied by
// repository.WorkerRepository.
type WorkerLiveness interface {
	IsWorkerLive(ctx context.Context, workerID uuid.UUID) (bool, error)
}

// ErrWorkerOffline is returned by Send when the mailbox's worker has stopped
// heartbeating. The send is not published: a command queued for a worker that
// is gone is never executed and never answered, which would leave the step
// looking sent forever. The worker reconciler moves the mailbox to a live
// worker and the dead-letter retry replays the task.
var ErrWorkerOffline = errors.New("the mailbox's sending worker is offline")

// ErrSendDispatchUnknown wraps a failure of the publish call itself. Every
// other Send failure happens before anything is published and is safe to retry
// immediately; this one is not, because the bus may have taken the command and
// a worker may still deliver it. A campaign send that fails this way keeps its
// reservation and is left to the worker result (or the reclaimer) rather than
// being offered again.
var ErrSendDispatchUnknown = errors.New("the send was not confirmed as queued and may already be on its way to a worker")

type emailSender struct {
	emailRepo repository.EmailRepository
	publisher events.Publisher
	liveness  WorkerLiveness
}

// NewEmailSender creates a new email sender
func NewEmailSender(emailRepo repository.EmailRepository, publisher events.Publisher) EmailSender {
	return &emailSender{
		emailRepo: emailRepo,
		publisher: publisher,
	}
}

// WireWorkerLiveness makes Send refuse to publish to a worker that is not
// heartbeating. Optional; without it Send trusts the assignment.
func (s *emailSender) WireWorkerLiveness(l WorkerLiveness) {
	s.liveness = l
}

// Send publishes an email to the worker service for sending
func (s *emailSender) Send(ctx context.Context, taskID uuid.UUID, msg EmailMessage, account models.Email) error {
	// Get worker ID for this email account
	workerID := account.WorkerID
	if workerID == nil {
		return fmt.Errorf("no worker assigned to email account %s", account.ID)
	}
	if s.liveness != nil {
		live, err := s.liveness.IsWorkerLive(ctx, *workerID)
		if err != nil {
			return fmt.Errorf("check worker %s liveness: %w", workerID, err)
		}
		if !live {
			return fmt.Errorf("%w (worker %s, mailbox %s)", ErrWorkerOffline, workerID, account.Email)
		}
	}

	// Email content is sealed with the organization DEK; an account without an
	// organization cannot be encrypted for transport.
	if account.OrganizationID == nil {
		return fmt.Errorf("email account %s has no organization", account.ID)
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
		OrgID:          *account.OrganizationID,
		To:             msg.To,
		CC:             msg.CC,
		BCC:            msg.BCC,
		InReplyTo:      msg.InReplyTo,
		ThreadID:       msg.ThreadID,
		Subject:        msg.Subject,
		MessageID:      msg.MessageID,
		BodyPlain:      msg.BodyPlain,
		BodyHTML:       bodyHTML,
		IsWarmup:       msg.IsWarmup,
		TrackingInfo:   msg.Tracking,
		WarmupToken:    msg.WarmupToken,
		UnsubscribeURL: msg.UnsubscribeURL,
		Attachments:    msg.Attachments,
		// The name as saved now, so a rename applies to the very next send
		// instead of waiting for the worker's cached identity to be rebuilt.
		FromName: strings.TrimSpace(account.Name),
	}

	// A chosen send-as alias applies to campaign and unibox mail only. Warmup
	// pairs mailboxes by their own addresses and verifies its token against
	// them, so sending warmup as an alias would break the pairing it is
	// meant to prove.
	if !msg.IsWarmup {
		params.FromEmail = strings.TrimSpace(account.SendAsEmail)
	}

	// Publish send email event to worker
	if err := s.publisher.PublishSendEmail(ctx, *workerID, params); err != nil {
		return fmt.Errorf("%w: %v", ErrSendDispatchUnknown, err)
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

// HeartbeatChecker reports whether a worker's short-lived heartbeat key is
// present. Satisfied by *cache.Cache (go-redis Exists).
type HeartbeatChecker interface {
	Exists(ctx context.Context, keys ...string) *redis.IntCmd
}

// workerLiveness combines the registry's view of a worker (active, seen in the
// last ten minutes) with its three-minute heartbeat key, so a crashed worker
// stops receiving sends within minutes rather than at the end of the registry
// window. A heartbeat lookup error fails open to the registry's answer: a
// cache blip must not stop every send.
type workerLiveness struct {
	repo      WorkerLiveness
	heartbeat HeartbeatChecker
}

// NewWorkerLiveness builds the liveness check Send uses. heartbeat may be nil.
func NewWorkerLiveness(repo WorkerLiveness, heartbeat HeartbeatChecker) WorkerLiveness {
	return &workerLiveness{repo: repo, heartbeat: heartbeat}
}

func (l *workerLiveness) IsWorkerLive(ctx context.Context, workerID uuid.UUID) (bool, error) {
	live, err := l.repo.IsWorkerLive(ctx, workerID)
	if err != nil || !live {
		return live, err
	}
	if l.heartbeat == nil {
		return true, nil
	}
	n, herr := l.heartbeat.Exists(ctx, "worker:heartbeat:"+workerID.String()).Result()
	if herr != nil {
		return true, nil
	}
	return n > 0, nil
}
