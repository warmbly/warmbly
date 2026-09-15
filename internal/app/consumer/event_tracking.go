package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mileusna/useragent"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/geo"
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
	evidence             advanced.EvidenceRecorder
	streamingPublisher   *pubsub.StreamingPublisher
	dedupeRepo           repository.TrackingDedupeRepository
	trackedLinks         repository.TrackedLinkRepository
	linkClicks           repository.LinkClickRepository
	// afterBurstWindow runs fn once the click burst window has passed, so a
	// human click's side effects wait for the burst rule's verdict.
	afterBurstWindow func(fn func())
	// advancedService fires INSTANT open/click action chains the moment a
	// tracking event lands (the open/click analog of the reply path in
	// ProcessIncomingReply). Best-effort and nil-safe: when unset, opens/clicks
	// are still recorded and routed at the next step boundary by the scheduler.
	advancedService advanced.Service
	// opens is the per-event open log; geo resolves a source network to a
	// location for opens and clicks. Both optional.
	opens repository.EmailOpenRepository
	geo   *geo.Client
	// retention is the operator-editable window the engagement prune obeys.
	// Injected post-construction; nil keeps the compiled default.
	retention RetentionSource
	tracking  TrackingPolicySource
	topic     string
	group     string
}

// RetentionSource is the operator-editable retention section, satisfied by
// instancesettings.Service. Read on every prune pass, so an edit in the admin
// panel takes effect on the next sweep rather than at the next restart.
type RetentionSource interface {
	RetentionWindows(ctx context.Context) instancesettings.Retention
}

// WireRetention attaches the instance settings the engagement prune reads its
// window from.
func (tc *TrackingConsumer) WireRetention(src RetentionSource) { tc.retention = src }

// engagementRetentionDays is the window the next prune pass uses.
func (tc *TrackingConsumer) engagementRetentionDays(ctx context.Context) int {
	if tc.retention == nil {
		return config.EngagementEventRetentionDaysDefault
	}
	return tc.retention.RetentionWindows(ctx).EngagementEventDays
}

// TrackingPolicySource is the operator-editable engagement-classification
// section, satisfied by instancesettings.Service. Read per event rather than
// held from boot, so an edit in the admin panel needs no restart. The
// consumer's own service caches for instancesettings.cacheTTL and the backend
// that wrote the row is a different process, so an edit lands within that TTL,
// not on the very next event.
type TrackingPolicySource interface {
	TrackingPolicy(ctx context.Context) instancesettings.Tracking
}

// WireTrackingPolicy attaches the instance settings the machine-window rule
// reads its windows from.
func (tc *TrackingConsumer) WireTrackingPolicy(src TrackingPolicySource) { tc.tracking = src }

// machineWindows are the windows the next classification uses. An unwired
// source falls back to the compiled defaults, which is what the consumer runs
// on before the settings document has ever been written.
func (tc *TrackingConsumer) machineWindows(ctx context.Context) instancesettings.Tracking {
	if tc.tracking == nil {
		return instancesettings.DefaultTracking()
	}
	return tc.tracking.TrackingPolicy(ctx)
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
	trackedLinks repository.TrackedLinkRepository,
	linkClicks repository.LinkClickRepository,
	advancedService advanced.Service,
	evidence advanced.EvidenceRecorder,
	opens repository.EmailOpenRepository,
	geoClient *geo.Client,
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
		trackedLinks:         trackedLinks,
		linkClicks:           linkClicks,
		afterBurstWindow: func(fn func()) {
			// One second past the window covers event-time skew between the
			// tracking service and the consumer.
			time.AfterFunc(time.Duration(config.TrackingClickBurstSeconds+1)*time.Second, fn)
		},
		advancedService: advancedService,
		evidence:        evidence,
		opens:           opens,
		geo:             geoClient,
		topic:           topic,
		group:           group,
	}, nil
}

// Start subscribes to the tracking topic and blocks until ctx is cancelled.
// It also runs the daily prune of the open and click logs.
func (tc *TrackingConsumer) Start(ctx context.Context) error {
	if tc.opens != nil || tc.linkClicks != nil {
		go tc.pruneEngagementLogs(ctx)
	}
	if tc.linkClicks != nil {
		go tc.sweepPendingClicks(ctx)
	}
	return tc.bus.Subscribe(ctx, []string{tc.topic}, tc.group, tc.receive)
}

// sweepPendingClicks fires the effects of human clicks whose timer never
// ran or never finished: the consumer restarted inside the burst window, a
// claim failed, or an attempt died mid-way and its lease expired. Runs at
// start and every minute until ctx ends. A click the timer completed is not
// offered, and one under a live lease is not offered twice.
func (tc *TrackingConsumer) sweepPendingClicks(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		before := time.Now().Add(-time.Duration(config.TrackingClickBurstSeconds+1) * time.Second)
		pending, err := tc.linkClicks.ListPendingAnnouncements(ctx, before, 200)
		if err != nil {
			log.Warn().Err(err).Msg("could not list pending click announcements")
		}
		for i := range pending {
			c := &pending[i]
			task := &repository.CampaignTask{TaskID: c.TaskID, CampaignID: &c.CampaignID, ContactID: &c.ContactID, SequenceID: &c.SequenceID}
			destination := c.Destination
			event := events.TrackingEvent{
				EventType:   events.EventTypeEmailClicked,
				TaskID:      c.TaskID.String(),
				OriginalURL: &destination,
				Timestamp:   c.ClickedAt.Format(time.RFC3339Nano),
			}
			if c.TrackedLinkID != nil {
				id := c.TrackedLinkID.String()
				event.LinkID = &id
			}
			tc.finishHumanClick(task, event, c.ID, c.Label, c.Origin)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// pruneEngagementLogs deletes opens and clicks older than the retention
// window, at start and then daily. The progress-row summary stays, so
// nothing a count, filter or branch reads is affected.
func (tc *TrackingConsumer) pruneEngagementLogs(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		days := tc.engagementRetentionDays(pctx)
		if tc.opens != nil {
			if n, err := tc.opens.Cleanup(pctx, days); err != nil {
				log.Warn().Err(err).Msg("open log prune failed")
			} else if n > 0 {
				log.Info().Int64("deleted", n).Msg("open log pruned")
			}
		}
		if tc.linkClicks != nil {
			if n, err := tc.linkClicks.Cleanup(pctx, days); err != nil {
				log.Warn().Err(err).Msg("click log prune failed")
			} else if n > 0 {
				log.Info().Int64("deleted", n).Msg("click log pruned")
			}
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
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

// HandleTrackingEvent processes a tracking event.
//
// Opens and clicks are classified before they count. The edge already drops
// crawlers and security scanners it can name by user agent, and labels the
// ones it recognises by source network; here the ones it cannot are caught by
// what they do: a fetch with no browser, a fetch inside the machine window
// after dispatch (nobody reads that fast), and clicks on several links of one
// email within seconds (a gateway walking the message).
// A machine open is still recorded, labelled, because it proves delivery. A
// machine click is logged per link with its reason but never stamps the step
// as clicked, fires no automation, and sends no webhook: "clicked" keeps
// meaning a person. Because a burst is only recognisable from its second
// click, a human click's side effects wait out the burst window before
// firing, on the classification the click has by then.
func (tc *TrackingConsumer) HandleTrackingEvent(ctx context.Context, event *events.TrackingEvent) error {
	// Parse and validate task ID
	taskID, err := uuid.Parse(event.TaskID)
	if err != nil {
		// Invalid task ID, skip
		return nil
	}

	// Click dedupe identity: the ticket, so two links sharing a destination
	// are two clicks; the URL only for events from an older tracking build.
	urlHash := ""
	if event.EventType == events.EventTypeEmailClicked {
		switch {
		case event.LinkID != nil && *event.LinkID != "":
			urlHash = hashURL("link:" + *event.LinkID)
		case event.OriginalURL != nil && *event.OriginalURL != "":
			urlHash = hashURL(*event.OriginalURL)
		}
	}

	// Get campaign task to find campaign/contact/sequence IDs
	campaignTask, err := tc.taskRepo.GetCampaignTask(ctx, taskID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", event.TaskID).Msg("failed to get campaign task for tracking event")
		return nil
	}
	if campaignTask == nil || campaignTask.CampaignID == nil || campaignTask.ContactID == nil || campaignTask.SequenceID == nil {
		// Task not found, not a campaign task, or missing its linkage: skip
		return nil
	}
	campaignID, contactID, sequenceID := *campaignTask.CampaignID, *campaignTask.ContactID, *campaignTask.SequenceID

	at := eventTime(event.Timestamp)
	sentAt, err := tc.campaignProgressRepo.GetStepSentAt(ctx, campaignID, contactID, sequenceID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", event.TaskID).Msg("failed to read step dispatch time; classifying by user agent only")
		sentAt = nil
	}

	// Classify. Machine opens (Apple MPP prefetch, UA-less clients, a fetch
	// inside the machine window) still count as delivery signal but are
	// labelled, and must never fire open-triggered automations.
	var machine bool
	var reason string
	windows := tc.machineWindows(ctx)
	seen := engagement{
		userAgent: event.UserAgent,
		scanner:   event.Scanner,
		probable:  event.ScannerProbable,
		sentAt:    sentAt,
		at:        at,
	}
	switch event.EventType {
	case events.EventTypeEmailOpened:
		kind := windows.OpenWindow()
		machine, reason = classifyOpen(seen, kind, windows.ProbableWindow(kind))
	case events.EventTypeEmailClicked:
		kind := windows.ClickWindow()
		machine, reason = classifyClick(seen, kind, windows.ProbableWindow(kind))
	default:
		// Unknown event type, skip
		return nil
	}

	// What the request said about where it came from, for the logs and the
	// live feed. The source network is resolved here and goes no further.
	origin := tc.originOf(event)

	// Check for duplicate at consumer level (belt and suspenders with Rust service)
	if tc.dedupeRepo != nil {
		processed, err := tc.dedupeRepo.IsProcessed(ctx, taskID, event.EventType, urlHash)
		if err != nil {
			// Log but continue - allow processing on dedupe errors
			log.Warn().Err(err).Str("task_id", event.TaskID).Msg("tracking dedupe check failed")
		} else if processed {
			// A HUMAN engagement after a machine-labelled one upgrades the
			// label (a gateway scanned at delivery; the person acted later).
			// Quiet write only: the event was already counted once, so no
			// automations and no re-publish.
			if event.EventType == events.EventTypeEmailOpened {
				// Every open is logged, repeats and machines included: a
				// second open from another device is worth seeing.
				tc.logOpen(ctx, campaignTask, event, at, machine, reason, origin)
			}
			if machine {
				return nil
			}
			switch event.EventType {
			case events.EventTypeEmailOpened:
				_ = tc.campaignProgressRepo.RecordEmailOpened(ctx, campaignID, contactID, sequenceID, false)
			case events.EventTypeEmailClicked:
				tc.upgradeClick(ctx, campaignTask, event, at, origin)
			}
			return nil
		}
	}

	// Record the event, then fire any INSTANT open/click action chain for the
	// contact's current step the moment the signal lands (the open/click analog of
	// the reply path in ProcessIncomingReply). instantKind maps the tracking event
	// to the matcher's eventKind. Firing happens AFTER the Record* write so the
	// matcher reads the just-stamped opened_at / clicked_at off the progress row.
	var instantKind string
	var linkLabel string
	var deferred bool
	switch event.EventType {
	case events.EventTypeEmailOpened:
		err = tc.campaignProgressRepo.RecordEmailOpened(ctx, campaignID, contactID, sequenceID, machine)
		tc.logOpen(ctx, campaignTask, event, at, machine, reason, origin)
		if !machine {
			instantKind = "open"
			// A human open proves the mailbox is live; a prefetch proves
			// only that a proxy fetched an image.
			if tc.evidence != nil {
				tc.evidence.RecordEvidence(ctx, contactID, models.Step(&campaignID, &sequenceID), "opened", sequenceID.String(), "")
			}
		}
	case events.EventTypeEmailClicked:
		var click *repository.LinkClick
		machine, reason, click, err = tc.recordClick(ctx, campaignTask, event, at, machine, reason, origin)
		if click != nil {
			linkLabel = click.Label
		}
		if err == nil && !machine {
			// The stamp is stored state a burst can walk back; the effects
			// cannot be recalled, so they wait for the window to close.
			err = tc.campaignProgressRepo.RecordEmailClicked(ctx, campaignID, contactID, sequenceID)
			if err == nil && click != nil && tc.afterBurstWindow != nil {
				deferred = true
				task, ev, clickID, label := campaignTask, *event, click.ID, linkLabel
				tc.afterBurstWindow(func() { tc.finishHumanClick(task, ev, clickID, label, origin) })
			} else if err == nil {
				instantKind = "click"
				if tc.evidence != nil {
					tc.evidence.RecordEvidence(ctx, contactID, models.Step(&campaignID, &sequenceID), "clicked", sequenceID.String(), "")
				}
			}
		}
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
		tc.advancedService.FireInstantActions(ctx, campaignID, contactID, sequenceID, instantKind)
	}

	// Mark as processed for deduplication
	if tc.dedupeRepo != nil {
		if err := tc.dedupeRepo.MarkProcessed(ctx, taskID, event.EventType, urlHash); err != nil {
			log.Warn().Err(err).Str("task_id", event.TaskID).Msg("failed to mark tracking event as processed")
		}
	}

	if machine {
		log.Debug().Str("task_id", event.TaskID).Str("event_type", string(event.EventType)).Str("reason", reason).Msg("tracking event classified as machine")
	}

	// Publish to Pub/Sub for realtime updates (a deferred human click
	// publishes once its verdict is final)
	if !deferred {
		tc.publishTrackingEvent(ctx, campaignTask, *event, machine, linkLabel, origin)
	}

	return nil
}

// finishHumanClick runs the effects of a click that looked human when it
// landed, once the burst window has passed: if a burst relabelled it in the
// meantime it is announced as automated and nothing else fires. The click
// row carries the pending flag, written before the event was marked
// processed, so a consumer restart inside the window hands the click to
// sweepPendingClicks instead of losing it, and a redelivery cannot fire it
// twice. When the claim cannot be made at all, nothing fires here and the
// sweep retries: an automation for a scanner's click is worse than a late
// one, and the step boundary still routes on the stored stamp.
func (tc *TrackingConsumer) finishHumanClick(task *repository.CampaignTask, event events.TrackingEvent, clickID uuid.UUID, label string, origin models.EngagementOrigin) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// The claim is the verdict and the lease in one write: a burst that
	// relabelled the click in the meantime shows up as machine, and a click
	// already announced, or under another attempt's live lease, is not
	// claimed. The row is completed only after the effects ran, so a crash
	// in between is retried by the sweep once the lease expires (at-least-
	// once: the instant actions claim their own once-only fire, the rest
	// may repeat after a crash).
	var claimed, machine bool
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if claimed, machine, err = tc.linkClicks.ClaimAnnounce(ctx, clickID); err == nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
		}
	}
	if err != nil {
		log.Error().Err(err).Str("click_id", clickID.String()).Msg("could not claim click announcement; left for the sweep")
		return
	}
	if !claimed {
		return
	}
	if machine {
		tc.publishTrackingEvent(ctx, task, event, true, label, origin)
	} else {
		if tc.evidence != nil {
			tc.evidence.RecordEvidence(ctx, *task.ContactID, models.Step(task.CampaignID, task.SequenceID), "clicked", task.SequenceID.String(), "")
		}
		if tc.advancedService != nil {
			tc.advancedService.FireInstantActions(ctx, *task.CampaignID, *task.ContactID, *task.SequenceID, "click")
		}
		tc.publishTrackingEvent(ctx, task, event, false, label, origin)
	}
	if err := tc.linkClicks.CompleteAnnounce(ctx, clickID); err != nil {
		log.Warn().Err(err).Str("click_id", clickID.String()).Msg("could not mark click announcement complete; the sweep may repeat it after the lease")
	}
}

// resolveLink names the clicked link: the minted ticket when the event
// carries one (destination and anchor text as stored at send time), else the
// URL the event reports. nil ticket id means the click log row stands alone.
func (tc *TrackingConsumer) resolveLink(ctx context.Context, event *events.TrackingEvent) (*uuid.UUID, string, string) {
	var destination string
	if event.OriginalURL != nil {
		destination = *event.OriginalURL
	}
	if event.LinkID == nil || tc.trackedLinks == nil {
		return nil, destination, ""
	}
	id, err := uuid.Parse(*event.LinkID)
	if err != nil {
		return nil, destination, ""
	}
	link, err := tc.trackedLinks.GetByID(ctx, id)
	if err != nil || link == nil {
		return nil, destination, ""
	}
	if link.Destination != "" {
		destination = link.Destination
	}
	return &link.ID, destination, link.Label
}

// recordClick logs the click per link and applies the burst rule: a click on
// a second link of the same email from the same source inside the burst
// window turns this click AND the earlier ones into machine clicks. When
// that leaves the step with no human click, the clicked stamp the first
// click already wrote is walked back. Returns the final classification and
// the logged row (nil when nothing could be logged).
func (tc *TrackingConsumer) recordClick(ctx context.Context, task *repository.CampaignTask, event *events.TrackingEvent, at time.Time, machine bool, reason string, origin models.EngagementOrigin) (bool, string, *repository.LinkClick, error) {
	if tc.linkClicks == nil {
		return machine, reason, nil, nil
	}
	linkID, destination, label := tc.resolveLink(ctx, event)
	if destination == "" {
		return machine, reason, nil, nil
	}
	ipHash := ""
	if event.IPHash != nil {
		ipHash = *event.IPHash
	}
	userAgent := ""
	if event.UserAgent != nil {
		userAgent = *event.UserAgent
	}

	burstSince := at.Add(-time.Duration(config.TrackingClickBurstSeconds) * time.Second)
	burst := false
	if !machine && ipHash != "" {
		n, err := tc.linkClicks.CountRecentOtherLinks(ctx, task.TaskID, ipHash, linkID, destination, burstSince)
		if err != nil {
			log.Warn().Err(err).Str("task_id", task.TaskID.String()).Msg("burst check failed; treating click as a person's")
		} else if n > 0 {
			machine, reason, burst = true, repository.LinkClickReasonBurst, true
		}
	}

	click := &repository.LinkClick{
		TrackedLinkID: linkID,
		TaskID:        task.TaskID,
		CampaignID:    *task.CampaignID,
		ContactID:     *task.ContactID,
		SequenceID:    *task.SequenceID,
		Destination:   destination,
		Label:         label,
		UserAgent:     userAgent,
		IPHash:        ipHash,
		Machine:       machine,
		MachineReason: reason,
		ClickedAt:     at,
		Origin:        origin,
		// A person's click waits out the burst window; the flag is the
		// durable record of that, written before the event is marked
		// processed, so a restart or a redelivery fires it exactly once.
		AnnouncePending: !machine && tc.afterBurstWindow != nil,
	}
	if err := tc.linkClicks.Insert(ctx, click); err != nil {
		return machine, reason, click, err
	}

	if burst {
		if _, err := tc.linkClicks.MarkBurst(ctx, task.TaskID, ipHash, burstSince); err != nil {
			log.Warn().Err(err).Str("task_id", task.TaskID.String()).Msg("failed to relabel burst clicks")
		}
		if err := tc.campaignProgressRepo.UnrecordEmailClicked(ctx, *task.CampaignID, *task.ContactID, *task.SequenceID); err != nil {
			log.Warn().Err(err).Str("task_id", task.TaskID.String()).Msg("failed to walk back the click stamp after a burst")
		}
	}
	return machine, reason, click, nil
}

// upgradeClick handles a human click on a link this email was already
// credited for: the step is stamped clicked if only machines had clicked so
// far, and the click is logged once so the timeline shows the person's.
func (tc *TrackingConsumer) upgradeClick(ctx context.Context, task *repository.CampaignTask, event *events.TrackingEvent, at time.Time, origin models.EngagementOrigin) {
	_ = tc.campaignProgressRepo.RecordEmailClicked(ctx, *task.CampaignID, *task.ContactID, *task.SequenceID)
	if tc.linkClicks == nil {
		return
	}
	linkID, destination, label := tc.resolveLink(ctx, event)
	if destination == "" {
		return
	}
	if seen, err := tc.linkClicks.HasHumanClickOn(ctx, task.TaskID, linkID, destination); err != nil || seen {
		return
	}
	ipHash, userAgent := "", ""
	if event.IPHash != nil {
		ipHash = *event.IPHash
	}
	if event.UserAgent != nil {
		userAgent = *event.UserAgent
	}
	_ = tc.linkClicks.Insert(ctx, &repository.LinkClick{
		TrackedLinkID: linkID,
		TaskID:        task.TaskID,
		CampaignID:    *task.CampaignID,
		ContactID:     *task.ContactID,
		SequenceID:    *task.SequenceID,
		Destination:   destination,
		Label:         label,
		UserAgent:     userAgent,
		IPHash:        ipHash,
		ClickedAt:     at,
		Origin:        origin,
	})
}

// logOpen writes one row to the open log for this event, whatever it was
// classified as; the label travels with it.
func (tc *TrackingConsumer) logOpen(ctx context.Context, task *repository.CampaignTask, event *events.TrackingEvent, at time.Time, machine bool, reason string, origin models.EngagementOrigin) {
	if tc.opens == nil {
		return
	}
	open := &repository.EmailOpen{
		TaskID:        task.TaskID,
		CampaignID:    *task.CampaignID,
		ContactID:     *task.ContactID,
		SequenceID:    *task.SequenceID,
		OpenedAt:      at,
		Machine:       machine,
		MachineReason: reason,
		Origin:        origin,
	}
	if event.UserAgent != nil {
		open.UserAgent = clipString(*event.UserAgent, 512)
	}
	if event.IPHash != nil {
		open.IPHash = *event.IPHash
	}
	if err := tc.opens.Insert(ctx, open); err != nil {
		log.Warn().Err(err).Str("task_id", task.TaskID.String()).Msg("failed to log open")
	}
}

// originOf reads what the event says about its source: the user agent parsed
// to client, browser and device, and the source network resolved to a
// location. The network is used here and dropped.
func (tc *TrackingConsumer) originOf(event *events.TrackingEvent) models.EngagementOrigin {
	var o models.EngagementOrigin
	if event.UserAgent != nil && strings.TrimSpace(*event.UserAgent) != "" {
		ua := useragent.Parse(*event.UserAgent)
		o.OS, o.Browser, o.BrowserVersion = ua.OS, ua.Name, ua.Version
		o.DeviceType = deviceType(ua)
		o.Client = clientName(*event.UserAgent)
	}
	if event.ClientIP != nil && tc.geo != nil {
		if addr, err := netip.ParseAddr(strings.TrimSpace(*event.ClientIP)); err == nil && !addr.IsPrivate() && !addr.IsLoopback() {
			if info, err := tc.geo.Lookup(addr); err == nil && info != nil {
				o.CountryCode = info.CountryCode
				o.Region = info.Region
				if info.City != "Unknown" {
					o.City = info.City
				}
			}
		}
	}
	return o
}

func clipString(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

// publishTrackingEvent publishes the tracking event to Pub/Sub for realtime UI
// updates AND fans an opt-in firehose webhook (campaign.email_opened/clicked).
func (tc *TrackingConsumer) publishTrackingEvent(ctx context.Context, task *repository.CampaignTask, event events.TrackingEvent, machine bool, linkLabel string, origin models.EngagementOrigin) {
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

	// Fan an opt-in firehose webhook for the open/click (org-scoped). People
	// only: a prefetch or a gateway walking the links is not engagement.
	if tc.advancedService != nil && campaign.OrganizationID != nil && !machine {
		var whType models.WebhookEventType
		switch event.EventType {
		case events.EventTypeEmailOpened:
			whType = models.WebhookEventCampaignEmailOpened
		case events.EventTypeEmailClicked:
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
				if linkLabel != "" {
					data["link_label"] = linkLabel
				}
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
		OccurredAt:   eventTime(event.Timestamp),
		Client:       origin.Client,
		DeviceType:   origin.DeviceType,
		CountryCode:  origin.CountryCode,
		City:         origin.City,
	}

	if event.EventType == events.EventTypeEmailClicked && event.OriginalURL != nil {
		trackingPayload.OriginalURL = *event.OriginalURL
		trackingPayload.LinkLabel = linkLabel
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
