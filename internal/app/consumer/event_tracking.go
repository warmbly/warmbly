package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// TrackingConsumer handles tracking events from the Rust tracking service. It
// subscribes on the shared event bus (Kafka or NATS) and decodes with the same
// codec the tracking producer writes (Avro on Kafka, JSON on NATS).
type TrackingConsumer struct {
	bus                  eventbus.EventBus
	codec                codec.Codec
	taskRepo             repository.TaskRepository
	campaignProgressRepo repository.CampaignProgressRepository
	campaignRepo         repository.CampaignRepository
	contactRepo          repository.ContactRepository
	streamingPublisher   *pubsub.StreamingPublisher
	dedupeRepo           repository.TrackingDedupeRepository
	// advancedService fires INSTANT open/click action chains the moment a
	// tracking event lands (the open/click analog of the reply path in
	// ProcessIncomingReply). Best-effort and nil-safe: when unset, opens/clicks
	// are still recorded and routed at the next step boundary by the scheduler.
	advancedService advanced.Service
	topic           string
	group           string
}

// NewTrackingConsumer wires the tracking consumer to the shared event bus.
func NewTrackingConsumer(
	bus eventbus.EventBus,
	cdc codec.Codec,
	topic, group string,
	taskRepo repository.TaskRepository,
	campaignProgressRepo repository.CampaignProgressRepository,
	campaignRepo repository.CampaignRepository,
	contactRepo repository.ContactRepository,
	streamingPublisher *pubsub.StreamingPublisher,
	dedupeRepo repository.TrackingDedupeRepository,
	advancedService advanced.Service,
) (*TrackingConsumer, error) {
	return &TrackingConsumer{
		bus:                  bus,
		codec:                cdc,
		taskRepo:             taskRepo,
		campaignProgressRepo: campaignProgressRepo,
		campaignRepo:         campaignRepo,
		contactRepo:          contactRepo,
		streamingPublisher:   streamingPublisher,
		dedupeRepo:           dedupeRepo,
		advancedService:      advancedService,
		topic:                topic,
		group:                group,
	}, nil
}

// Start subscribes to the tracking topic and blocks until ctx is cancelled.
func (tc *TrackingConsumer) Start(ctx context.Context) error {
	return tc.bus.Subscribe(ctx, []string{tc.topic}, tc.group, tc.receive)
}

// Close is a no-op: the event bus lifecycle is owned by the consumer main,
// which subscribes both worker-events and tracking on the same bus.
func (tc *TrackingConsumer) Close() {}

// receive decodes a tracking-events bus message and dispatches it.
func (tc *TrackingConsumer) receive(_ context.Context, msg eventbus.Message) error {
	var event events.TrackingEvent
	if err := tc.codec.Deserialize(context.Background(), tc.topic, msg.Payload, &event); err != nil {
		log.Warn().Err(err).Msg("failed to deserialize tracking event")
		return nil // don't fail - skip invalid events
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

	// Classify opens: machine fetches (Apple MPP prefetch, UA-less clients)
	// still count as delivery signal but are labeled, and must never fire
	// open-triggered automations (a prefetch is not intent).
	machineOpen := event.EventType == events.EventTypeEmailOpened && isMachineOpen(event.UserAgent)

	// Check for duplicate at consumer level (belt and suspenders with Rust service)
	if tc.dedupeRepo != nil {
		processed, err := tc.dedupeRepo.IsProcessed(ctx, taskID, event.EventType, urlHash)
		if err != nil {
			// Log but continue - allow processing on dedupe errors
			log.Warn().Err(err).Str("task_id", event.TaskID).Msg("tracking dedupe check failed")
		} else if processed {
			// A HUMAN open after a machine-labeled one upgrades the label
			// (MPP prefetched at delivery; the person actually read it later
			// from another network). Quiet write only: the open was already
			// counted once, so no automations and no re-publish.
			if event.EventType == events.EventTypeEmailOpened && !machineOpen {
				if campaignTask, terr := tc.taskRepo.GetCampaignTask(ctx, taskID); terr == nil &&
					campaignTask != nil && campaignTask.CampaignID != nil &&
					campaignTask.ContactID != nil && campaignTask.SequenceID != nil {
					_ = tc.campaignProgressRepo.RecordEmailOpened(ctx,
						*campaignTask.CampaignID, *campaignTask.ContactID, *campaignTask.SequenceID, false)
				}
			}
			return nil
		}
	}

	// Get campaign task to find campaign/contact/sequence IDs
	campaignTask, err := tc.taskRepo.GetCampaignTask(ctx, taskID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", event.TaskID).Msg("failed to get campaign task for tracking event")
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

	// Record the event, then fire any INSTANT open/click action chain for the
	// contact's current step the moment the signal lands (the open/click analog of
	// the reply path in ProcessIncomingReply). instantKind maps the tracking event
	// to the matcher's eventKind. Firing happens AFTER the Record* write so the
	// matcher reads the just-stamped opened_at / clicked_at off the progress row.
	var instantKind string
	switch event.EventType {
	case events.EventTypeEmailOpened:
		err = tc.campaignProgressRepo.RecordEmailOpened(ctx,
			*campaignTask.CampaignID,
			*campaignTask.ContactID,
			*campaignTask.SequenceID,
			machineOpen)
		if !machineOpen {
			instantKind = "open"
		}
	case events.EventTypeEmailClicked:
		err = tc.campaignProgressRepo.RecordEmailClicked(ctx,
			*campaignTask.CampaignID,
			*campaignTask.ContactID,
			*campaignTask.SequenceID)
		instantKind = "click"
	default:
		// Unknown event type, skip
		return nil
	}

	if err != nil {
		log.Error().Err(err).Str("task_id", event.TaskID).Str("event_type", string(event.EventType)).Msg("failed to record tracking event")
		return nil
	}

	// INSTANT open/click trigger: best-effort and non-blocking, mirroring the
	// reply path. A failure (or a nil service in a process that doesn't wire it)
	// must never block tracking ingest; the scheduler still routes the matching
	// opened/clicked branch at the next step boundary. Exactly-once per (step,
	// eventKind) is enforced inside FireInstantActions via ClaimInstantFire.
	if tc.advancedService != nil && instantKind != "" {
		tc.advancedService.FireInstantActions(ctx,
			*campaignTask.CampaignID,
			*campaignTask.ContactID,
			*campaignTask.SequenceID,
			instantKind)
	}

	// Mark as processed for deduplication
	if tc.dedupeRepo != nil {
		if err := tc.dedupeRepo.MarkProcessed(ctx, taskID, event.EventType, urlHash); err != nil {
			log.Warn().Err(err).Str("task_id", event.TaskID).Msg("failed to mark tracking event as processed")
		}
	}

	// Publish to Pub/Sub for realtime updates
	tc.publishTrackingEvent(ctx, campaignTask, *event, machineOpen)

	return nil
}

// publishTrackingEvent publishes the tracking event to Pub/Sub for realtime UI
// updates AND fans an opt-in firehose webhook (campaign.email_opened/clicked).
func (tc *TrackingConsumer) publishTrackingEvent(ctx context.Context, task *repository.CampaignTask, event events.TrackingEvent, machine bool) {
	// Get campaign to find user ID + org
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

	// Fan an opt-in firehose webhook for the open/click (org-scoped). Human opens
	// only — a machine prefetch is not engagement intent — clicks always.
	if tc.advancedService != nil && campaign.OrganizationID != nil {
		var whType models.WebhookEventType
		switch {
		case event.EventType == events.EventTypeEmailOpened && !machine:
			whType = models.WebhookEventCampaignEmailOpened
		case event.EventType == events.EventTypeEmailClicked:
			whType = models.WebhookEventCampaignEmailClicked
		}
		if whType != "" {
			data := map[string]any{
				"campaign_id":   task.CampaignID.String(),
				"contact_id":    task.ContactID.String(),
				"contact_email": contactEmail,
				"sequence_id":   task.SequenceID.String(),
			}
			if event.EventType == events.EventTypeEmailClicked && event.OriginalURL != nil {
				data["url"] = *event.OriginalURL
			}
			tc.advancedService.EmitCampaignEvent(ctx, *campaign.OrganizationID, whType, data)
		}
	}

	if tc.streamingPublisher == nil {
		return
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

	// Publish tracking event (org-scoped: opens/clicks pulse live for the
	// whole team, not just the campaign owner)
	var orgID string
	if campaign.OrganizationID != nil {
		orgID = campaign.OrganizationID.String()
	}
	trackingPayload := &pubsub.TrackingEventPayload{
		BaseEvent: pubsub.BaseEvent{
			EventType: eventType,
			UserID:    campaign.UserID,
			Timestamp: time.Now(),
		},
		OrgID:        orgID,
		CampaignID:   task.CampaignID.String(),
		ContactID:    task.ContactID.String(),
		ContactEmail: contactEmail,
		SequenceID:   task.SequenceID.String(),
		Machine:      machine,
	}

	if event.EventType == events.EventTypeEmailClicked && event.OriginalURL != nil {
		trackingPayload.OriginalURL = *event.OriginalURL
	}

	tc.streamingPublisher.PublishTrackingEvent(ctx, trackingPayload)
}

// hashURL creates a short hash of a URL for deduplication
func hashURL(u string) string {
	if u == "" {
		return ""
	}
	h := sha256.Sum256([]byte(u))
	return hex.EncodeToString(h[:8])
}
