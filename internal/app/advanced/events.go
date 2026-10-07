package advanced

import (
	"context"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/inboxtag"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// EventDispatcher fans a platform event out to customer webhooks and, via the
// webhook dispatch sink, to third-party integration actions (Slack ping, CRM
// upsert). It is satisfied by *webhook.Service. Kept as a local interface so
// the advanced package stays decoupled from the webhook package (no import
// cycle, and the consumer/backend wire whichever dispatcher they construct).
type EventDispatcher interface {
	Dispatch(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any) (uuid.UUID, error)
}

// WireDispatcher attaches the event dispatcher after construction. Done this
// way (rather than via the constructor) so the dispatcher — which itself may
// depend on services constructed later — can be supplied once the graph is
// fully wired. No-op if never called: emit() guards on a nil dispatcher.
// WireSegments attaches the segment repository the instant add_to_segment /
// remove_from_segment actions write through.
func (s *service) WireSegments(r repository.SegmentRepository) { s.segmentRepo = r }

// SegmentAware is the optional capability the caller uses to attach segments.
type SegmentAware interface {
	WireSegments(r repository.SegmentRepository)
}

func (s *service) WireDispatcher(d EventDispatcher) {
	s.dispatcher = d
}

// emit dispatches a platform event, best-effort. Reply detection runs in the
// consumer's hot path, so a webhook/integration hiccup must never block inbox
// ingest — failures are swallowed (Dispatch already logs its own).
func (s *service) emit(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data map[string]any) {
	if s.dispatcher == nil || orgID == uuid.Nil {
		return
	}
	if repository.QueueSendResultEffect(ctx, "webhook:"+string(eventType), repository.SendResultEffect{Kind: "webhook", OrganizationID: orgID, EventType: eventType, Data: data}) {
		return
	}
	repository.AfterSendResultCommit(ctx, func(ctx context.Context) { _, _ = s.dispatcher.Dispatch(ctx, orgID, eventType, data) })
}

// EmitCampaignEvent dispatches a campaign event (e.g. from a sequence "notify"
// action node) to customer webhooks and wired integrations. Best-effort — a
// dispatch hiccup must never block the sending pipeline.
func (s *service) EmitCampaignEvent(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data map[string]any) {
	s.emit(ctx, orgID, eventType, data)
}

// ReplyRealtimePublisher pushes an org-scoped EMAIL_REPLIED pulse to the live
// dashboard. Satisfied by *pubsub.StreamingPublisher; primitive-typed local
// interface so this package stays decoupled from the pubsub event types.
type ReplyRealtimePublisher interface {
	PublishEmailReplied(ctx context.Context, orgID, userID, campaignID, contactID, contactEmail, sequenceID string)
	// PublishCustomEvent pushes a developer-defined "fire event" to the gateway so
	// API-key websocket subscribers receive it (the campaign "fire event" step).
	PublishCustomEvent(ctx context.Context, orgID, actorID uuid.UUID, name string, payload map[string]string, source, sourceID string)
}

// WireRealtime attaches the realtime publisher after construction. No-op if
// never called: the emit site guards on nil.
func (s *service) WireRealtime(p ReplyRealtimePublisher) {
	s.realtime = p
}

// Notifier raises a per-user in-app notification (gated by the user's prefs).
// Satisfied by *notification.Service. Local interface to avoid an import cycle;
// wired post-construction in the consumer (where reply/bounce/complaint run).
type Notifier interface {
	Notify(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any)
	NotifyAboutMessage(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, uniboxEmailID uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any)
}

// WireNotifier attaches the notification service after construction.
func (s *service) WireNotifier(n Notifier) {
	s.notifier = n
}

// AutomationRunner launches an automation graph by id, so an instant
// "run_automation" action node (reply/open/click branch) can fire the same flow
// the scheduler runs at a step boundary. Satisfied by *integration.Service;
// kept as a local interface to avoid an import cycle (integration imports
// advanced) and wired post-construction once the integration service exists.
type AutomationRunner interface {
	RunAutomationByID(ctx context.Context, orgID, automationID uuid.UUID, data map[string]any) error
}

// WireAutomationRunner attaches the automation runner after construction. No-op
// if never called: the run_automation instant case guards on a nil runner.
func (s *service) WireAutomationRunner(r AutomationRunner) {
	s.automationRunner = r
}

// InboxAgent drafts a suggested reply when an inbound human reply lands (M10).
// Best-effort and self-detaching (it fans onto its own goroutine), so the reply
// hot path never blocks. Satisfied structurally by *inboxagent.Service via the
// shared models.InboxAgentReply param, so this package needs no import of it.
type InboxAgent interface {
	DraftForReply(ctx context.Context, r models.InboxAgentReply)
}

// WireInboxAgent attaches the inbox agent after construction. No-op if never
// called (the reply hook guards on a nil agent).
func (s *service) WireInboxAgent(a InboxAgent) {
	s.inboxAgent = a
}

// CRMOutbox carries CRM records written here (automation and reply tasks,
// campaign deals) to a connected CRM. Nil when the workspace has none.
type CRMOutbox interface {
	EnqueuePush(ctx context.Context, orgID uuid.UUID, objectType string, localID uuid.UUID)
}

// WireCRMOutbox attaches the connected-CRM outbox after construction.
func (s *service) WireCRMOutbox(o CRMOutbox) { s.crmOutbox = o }

func (s *service) pushCRM(ctx context.Context, orgID uuid.UUID, objectType string, localID uuid.UUID) {
	if s.crmOutbox != nil {
		s.crmOutbox.EnqueuePush(ctx, orgID, objectType, localID)
	}
}

// WireInboxTags attaches the inbox tagging verdict store after construction.
// No-op if never called (the reply hook guards on a nil store).
func (s *service) WireInboxTags(repo repository.InboxTagRepository) {
	s.inboxTags = repo
}

// recordReplyIntent copies a confident human-reply intent from the stored
// tagging verdict onto the contact's progress row, so reply_intent branches
// route on it with no model call. Best-effort: a miss or an error only logs.
func (s *service) recordReplyIntent(ctx context.Context, orgID uuid.UUID, messageID string, campaignID, contactID, sequenceID uuid.UUID) {
	if s.inboxTags == nil || messageID == "" {
		return
	}
	res, err := s.inboxTags.GetByMessageID(ctx, orgID, messageID)
	if err != nil {
		log.Warn().Err(err).Str("message_id", messageID).Msg("reply intent: inbox tag lookup failed")
		return
	}
	if res == nil || res.Kind != inboxtag.KindHumanReply || res.NeedsReview || res.Intent == "" || res.IntentConfidence < inboxtag.ConfFloor {
		return
	}
	if err := s.campaignProgressRepo.RecordReplyIntent(ctx, campaignID, contactID, sequenceID, res.Intent); err != nil {
		log.Warn().Err(err).Str("campaign_id", campaignID.String()).Str("contact_id", contactID.String()).Msg("reply intent: record failed")
	}
}

// inboxTagActed reports whether the tagger already took the named action on
// this message, so the reply hook does not repeat it.
func (s *service) inboxTagActed(ctx context.Context, orgID uuid.UUID, messageID, action string) bool {
	if s.inboxTags == nil || messageID == "" {
		return false
	}
	res, err := s.inboxTags.GetByMessageID(ctx, orgID, messageID)
	if err != nil || res == nil {
		return false
	}
	for _, a := range res.Actions {
		if a == action {
			return true
		}
	}
	return false
}

// returnDateDoubted reports that inbox tagging read the away message's return
// date as not the day the sender is back. No verdict means no doubt.
func (s *service) returnDateDoubted(ctx context.Context, orgID uuid.UUID, messageID string, back time.Time) bool {
	if s.inboxTags == nil || messageID == "" || orgID == uuid.Nil {
		return false
	}
	res, err := s.inboxTags.GetByMessageID(ctx, orgID, messageID)
	if err != nil {
		log.Warn().Err(err).Str("message_id", messageID).Msg("out-of-office hold: inbox tag lookup failed; parsed date stands")
		return false
	}
	return inboxtag.ReturnDateDoubted(res, back)
}

// notify raises an in-app notification off the hot path. It detaches from the
// request context (the ingest call may return first) and is best-effort.
func (s *service) notify(userID uuid.UUID, orgID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any) {
	if s.notifier == nil || userID == uuid.Nil {
		return
	}
	// Detached + time-bounded so a slow DB can't accumulate notify goroutines.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.notifier.Notify(ctx, userID, orgID, category, title, body, link, meta)
	}()
}

// notifyAboutMessage is notify for one unibox message, so the notification
// leaves with the message and is read with it.
func (s *service) notifyAboutMessage(userID uuid.UUID, orgID *uuid.UUID, uniboxEmailID uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any) {
	if s.notifier == nil || userID == uuid.Nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.notifier.NotifyAboutMessage(ctx, userID, orgID, uniboxEmailID, category, title, body, link, meta)
	}()
}

// UniboxThreadLink opens the conversation itself rather than the inbox.
func UniboxThreadLink(threadID string) string {
	if threadID == "" {
		return "/app/unibox"
	}
	return "/app/unibox/all/" + url.PathEscape(threadID)
}
