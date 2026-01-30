package jobs

import (
	"context"
	"fmt"
	"time"

	ckf "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/repository"
)

// TrackingConsumer handles tracking events from the Rust tracking service
type TrackingConsumer struct {
	consumer             *kafka.Consumer
	taskRepo             repository.TaskRepository
	campaignProgressRepo repository.CampaignProgressRepository
	campaignRepo         repository.CampaignRepository
	contactRepo          repository.ContactRepository
	streamingPublisher   *pubsub.StreamingPublisher
	dedupeRepo           repository.TrackingDedupeRepository
	topic                string
}

// NewTrackingConsumer creates a new tracking consumer using existing Kafka infrastructure
// Config is loaded from AWS Parameter Store/Secrets Manager via config.LoadTrackingConsumerConfig
func NewTrackingConsumer(
	cfg *config.TrackingConsumerConfig,
	avrov2 *kafka.Avrov2,
	taskRepo repository.TaskRepository,
	campaignProgressRepo repository.CampaignProgressRepository,
	campaignRepo repository.CampaignRepository,
	contactRepo repository.ContactRepository,
	streamingPublisher *pubsub.StreamingPublisher,
	dedupeRepo repository.TrackingDedupeRepository,
) (*TrackingConsumer, error) {
	// Create Kafka consumer using existing infrastructure
	consumerConfig := kafka.NewConsumer(cfg.Brokers)
	consumerConfig.Set("group.id", cfg.GroupID)
	consumerConfig.Set("auto.offset.reset", "earliest")
	consumerConfig.Set("enable.auto.commit", false)

	// Configure SASL if enabled (credentials from AWS Secrets Manager)
	if cfg.SASLEnabled {
		consumerConfig.WithSASL(&kafka.SASLConfig{
			Username: cfg.SASLUsername,
			Password: cfg.SASLPassword,
		})
	}

	consumer, err := consumerConfig.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to create tracking consumer: %w", err)
	}

	// Attach Avrov2 for deserialization
	consumer.WithAvrov2(avrov2)

	// Subscribe to tracking events topic
	if err := consumer.SubscribeTopics([]string{cfg.Topic}); err != nil {
		consumer.Close()
		return nil, fmt.Errorf("failed to subscribe to tracking topic: %w", err)
	}

	return &TrackingConsumer{
		consumer:             consumer,
		taskRepo:             taskRepo,
		campaignProgressRepo: campaignProgressRepo,
		campaignRepo:         campaignRepo,
		contactRepo:          contactRepo,
		streamingPublisher:   streamingPublisher,
		dedupeRepo:           dedupeRepo,
		topic:                cfg.Topic,
	}, nil
}

// Start begins consuming tracking events
func (tc *TrackingConsumer) Start(ctx context.Context) error {
	return tc.consumer.Consume(ctx, tc.handleMessage)
}

// Close closes the consumer
func (tc *TrackingConsumer) Close() {
	if tc.consumer != nil {
		tc.consumer.Close()
	}
}

// handleMessage processes a raw Kafka message using Avro deserialization
func (tc *TrackingConsumer) handleMessage(msg *ckf.Message) error {
	var event events.TrackingEvent

	// Deserialize using Confluent Avrov2
	if err := tc.consumer.Avrov2.Deser.DeserializeInto(tc.topic, msg.Value, &event); err != nil {
		fmt.Printf("Failed to deserialize tracking event: %v\n", err)
		return nil // Don't fail - skip invalid events
	}

	return tc.HandleTrackingEvent(context.Background(), &event)
}

// HandleTrackingEvent processes a tracking event
func (tc *TrackingConsumer) HandleTrackingEvent(ctx context.Context, event *events.TrackingEvent) error {
	// Parse and validate task ID
	taskID, err := uuid.Parse(event.TaskID)
	if err != nil {
		// Invalid task ID, skip
		return nil
	}

	// Calculate URL hash for click event deduplication
	urlHash := ""
	if event.EventType == events.EventTypeEmailClicked && event.OriginalURL != nil && *event.OriginalURL != "" {
		urlHash = hashURL(*event.OriginalURL)
	}

	// Check for duplicate at consumer level (belt and suspenders with Rust service)
	if tc.dedupeRepo != nil {
		processed, err := tc.dedupeRepo.IsProcessed(ctx, taskID, event.EventType, urlHash)
		if err != nil {
			// Log but continue - allow processing on dedupe errors
			fmt.Printf("Dedupe check failed: %v\n", err)
		} else if processed {
			// Already processed, skip
			return nil
		}
	}

	// Get campaign task to find campaign/contact/sequence IDs
	campaignTask, err := tc.taskRepo.GetCampaignTask(ctx, taskID)
	if err != nil {
		fmt.Printf("Failed to get campaign task: %v\n", err)
		return nil
	}

	if campaignTask == nil || campaignTask.CampaignID == nil {
		// Task not found or not a campaign task, skip
		return nil
	}

	// Ensure we have contact_id and sequence_id
	if campaignTask.ContactID == nil || campaignTask.SequenceID == nil {
		// Missing required fields, skip
		return nil
	}

	// Record the event
	switch event.EventType {
	case events.EventTypeEmailOpened:
		err = tc.campaignProgressRepo.RecordEmailOpened(ctx,
			*campaignTask.CampaignID,
			*campaignTask.ContactID,
			*campaignTask.SequenceID)
	case events.EventTypeEmailClicked:
		err = tc.campaignProgressRepo.RecordEmailClicked(ctx,
			*campaignTask.CampaignID,
			*campaignTask.ContactID,
			*campaignTask.SequenceID)
	default:
		// Unknown event type, skip
		return nil
	}

	if err != nil {
		fmt.Printf("Failed to record %s event: %v\n", event.EventType, err)
		return nil
	}

	// Mark as processed for deduplication
	if tc.dedupeRepo != nil {
		if err := tc.dedupeRepo.MarkProcessed(ctx, taskID, event.EventType, urlHash); err != nil {
			fmt.Printf("Failed to mark event as processed: %v\n", err)
		}
	}

	// Publish to Pub/Sub for realtime updates
	tc.publishTrackingEvent(ctx, campaignTask, *event)

	return nil
}

// publishTrackingEvent publishes the tracking event to Pub/Sub for realtime UI updates
func (tc *TrackingConsumer) publishTrackingEvent(ctx context.Context, task *repository.CampaignTask, event events.TrackingEvent) {
	if tc.streamingPublisher == nil {
		return
	}

	// Get campaign to find user ID
	campaign, err := tc.campaignRepo.GetByID(ctx, *task.CampaignID)
	if err != nil || campaign == nil {
		return
	}

	// Get contact email for display
	var contactEmail string
	if task.ContactID != nil {
		contact, xerr := tc.contactRepo.GetByID(ctx, *task.ContactID)
		if xerr == nil && contact != nil {
			contactEmail = contact.Email
		}
	}

	// Determine event type
	var eventType pubsub.EventType
	switch event.EventType {
	case events.EventTypeEmailOpened:
		eventType = pubsub.EventEmailOpened
	case events.EventTypeEmailClicked:
		eventType = pubsub.EventEmailClicked
	default:
		return
	}

	// Publish tracking event
	trackingPayload := &pubsub.TrackingEventPayload{
		BaseEvent: pubsub.BaseEvent{
			EventType: eventType,
			UserID:    campaign.UserID,
			Timestamp: time.Now(),
		},
		CampaignID:   task.CampaignID.String(),
		ContactID:    task.ContactID.String(),
		ContactEmail: contactEmail,
		SequenceID:   task.SequenceID.String(),
	}

	if event.EventType == events.EventTypeEmailClicked && event.OriginalURL != nil {
		trackingPayload.OriginalURL = *event.OriginalURL
	}

	tc.streamingPublisher.PublishTrackingEvent(ctx, trackingPayload)
}

// hashURL creates a short hash of a URL for deduplication
func hashURL(url string) string {
	if url == "" {
		return ""
	}
	// Simple hash using first 8 chars of URL + length
	if len(url) > 8 {
		return fmt.Sprintf("%s_%d", url[:8], len(url))
	}
	return url
}
