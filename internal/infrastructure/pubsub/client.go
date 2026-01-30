package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
)

// Client wraps Google Pub/Sub for real-time streaming
type Client struct {
	client    *pubsub.Client
	projectID string
}

// NewClient creates a new Pub/Sub client
func NewClient(ctx context.Context, projectID string) (*Client, error) {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub client: %w", err)
	}

	return &Client{
		client:    client,
		projectID: projectID,
	}, nil
}

// Close closes the Pub/Sub client
func (c *Client) Close() error {
	return c.client.Close()
}

// getTopic gets or creates a topic
func (c *Client) getTopic(ctx context.Context, topicID string) (*pubsub.Topic, error) {
	topic := c.client.Topic(topicID)
	exists, err := topic.Exists(ctx)
	if err != nil {
		return nil, err
	}

	if !exists {
		topic, err = c.client.CreateTopic(ctx, topicID)
		if err != nil {
			return nil, err
		}
	}

	return topic, nil
}

// Publish publishes a message to a topic
func (c *Client) Publish(ctx context.Context, topicID string, data interface{}, attributes map[string]string) error {
	topic, err := c.getTopic(ctx, topicID)
	if err != nil {
		return fmt.Errorf("failed to get topic: %w", err)
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	result := topic.Publish(ctx, &pubsub.Message{
		Data:       jsonData,
		Attributes: attributes,
	})

	_, err = result.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	return nil
}

// StreamingPublisher handles real-time streaming to users
type StreamingPublisher struct {
	client *Client
}

// NewStreamingPublisher creates a new streaming publisher
func NewStreamingPublisher(client *Client) *StreamingPublisher {
	return &StreamingPublisher{
		client: client,
	}
}

// Topic names for real-time updates
const (
	TopicTaskStatus      = "task-status"
	TopicCampaignUpdate  = "campaign-update"
	TopicWarmupUpdate    = "warmup-update"
	TopicEmailError      = "email-error"
	TopicEmailWarning    = "email-warning"
)

// Event types
type EventType string

const (
	EventTaskCreated   EventType = "TASK_CREATED"
	EventTaskStarted   EventType = "TASK_STARTED"
	EventTaskCompleted EventType = "TASK_COMPLETED"
	EventTaskFailed    EventType = "TASK_FAILED"
	EventEmailSent     EventType = "EMAIL_SENT"
	EventEmailFailed   EventType = "EMAIL_FAILED"
	EventError         EventType = "ERROR"
	EventWarning       EventType = "WARNING"
)

// StreamEvent represents a real-time event for users
type StreamEvent struct {
	EventType EventType `json:"event_type"`
	UserID    string    `json:"user_id"`
	TaskID    string    `json:"task_id,omitempty"`
	EmailID   string    `json:"email_id,omitempty"`
	Message   string    `json:"message,omitempty"`
	Data      any       `json:"data,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// PublishTaskStatus publishes a task status update
func (p *StreamingPublisher) PublishTaskStatus(ctx context.Context, userID string, taskID uuid.UUID, eventType EventType, message string, data any) {
	if p.client == nil {
		return
	}

	event := StreamEvent{
		EventType: eventType,
		UserID:    userID,
		TaskID:    taskID.String(),
		Message:   message,
		Data:      data,
		Timestamp: time.Now(),
	}

	attrs := map[string]string{
		"user_id":    userID,
		"event_type": string(eventType),
	}

	if err := p.client.Publish(ctx, TopicTaskStatus, event, attrs); err != nil {
		sentry.CaptureException(fmt.Errorf("failed to publish task status: %w", err))
	}
}

// PublishEmailError publishes an email error for user visibility
func (p *StreamingPublisher) PublishEmailError(ctx context.Context, userID string, emailID uuid.UUID, taskID uuid.UUID, errorTitle, errorMessage string) {
	if p.client == nil {
		return
	}

	event := StreamEvent{
		EventType: EventError,
		UserID:    userID,
		TaskID:    taskID.String(),
		EmailID:   emailID.String(),
		Message:   errorTitle,
		Data: map[string]string{
			"title":   errorTitle,
			"message": errorMessage,
		},
		Timestamp: time.Now(),
	}

	attrs := map[string]string{
		"user_id":    userID,
		"email_id":   emailID.String(),
		"event_type": string(EventError),
	}

	if err := p.client.Publish(ctx, TopicEmailError, event, attrs); err != nil {
		sentry.CaptureException(fmt.Errorf("failed to publish email error: %w", err))
	}
}

// PublishEmailWarning publishes an email warning for user visibility
func (p *StreamingPublisher) PublishEmailWarning(ctx context.Context, userID string, emailID uuid.UUID, warningTitle, warningMessage string) {
	if p.client == nil {
		return
	}

	event := StreamEvent{
		EventType: EventWarning,
		UserID:    userID,
		EmailID:   emailID.String(),
		Message:   warningTitle,
		Data: map[string]string{
			"title":   warningTitle,
			"message": warningMessage,
		},
		Timestamp: time.Now(),
	}

	attrs := map[string]string{
		"user_id":    userID,
		"email_id":   emailID.String(),
		"event_type": string(EventWarning),
	}

	if err := p.client.Publish(ctx, TopicEmailWarning, event, attrs); err != nil {
		sentry.CaptureException(fmt.Errorf("failed to publish email warning: %w", err))
	}
}

// PublishCampaignProgress publishes campaign progress update
func (p *StreamingPublisher) PublishCampaignProgress(ctx context.Context, userID string, campaignID uuid.UUID, progress any) {
	if p.client == nil {
		return
	}

	event := StreamEvent{
		EventType: EventTaskCompleted,
		UserID:    userID,
		Message:   "Campaign progress updated",
		Data:      progress,
		Timestamp: time.Now(),
	}

	attrs := map[string]string{
		"user_id":     userID,
		"campaign_id": campaignID.String(),
		"event_type":  "CAMPAIGN_PROGRESS",
	}

	if err := p.client.Publish(ctx, TopicCampaignUpdate, event, attrs); err != nil {
		sentry.CaptureException(fmt.Errorf("failed to publish campaign progress: %w", err))
	}
}

// PublishWarmupStats publishes warmup statistics update
func (p *StreamingPublisher) PublishWarmupStats(ctx context.Context, userID string, emailID uuid.UUID, stats any) {
	if p.client == nil {
		return
	}

	event := StreamEvent{
		EventType: EventTaskCompleted,
		UserID:    userID,
		EmailID:   emailID.String(),
		Message:   "Warmup statistics updated",
		Data:      stats,
		Timestamp: time.Now(),
	}

	attrs := map[string]string{
		"user_id":    userID,
		"email_id":   emailID.String(),
		"event_type": "WARMUP_STATS",
	}

	if err := p.client.Publish(ctx, TopicWarmupUpdate, event, attrs); err != nil {
		sentry.CaptureException(fmt.Errorf("failed to publish warmup stats: %w", err))
	}
}
