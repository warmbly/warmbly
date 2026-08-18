package wmail

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emsg"
	"github.com/warmbly/warmbly/internal/repository"
)

// tickStats is what one sync pass learned about live mail, folded into the
// relayed state at the end of the pass.
type tickStats struct {
	// seen counts new (unknown to the platform) live messages the server
	// reported this pass, admitted or not.
	seen int
	// deferred counts those left on the server for budget reasons.
	deferred int
	// denied remembers each lane's first denial this pass, so a thousand
	// waiting messages cost one budget check, not a thousand.
	denied map[SyncLane]Admission
	// aborted is set when the pass must end now: escalation, a control-plane
	// failure, or cancellation. Every cursor is held where it stands.
	aborted bool
}

func (s *tickStats) laneDenied(lane SyncLane) bool {
	_, ok := s.denied[lane]
	return ok
}

// laneFor decides which budget a new message draws from. A reply to a
// conversation this mailbox owns rides the priority lane; the lookup goes to
// the control plane and fails closed to live, so an unreachable backend only
// costs priority, never correctness.
func (w *WMail) laneFor(ctx context.Context, msg *models.EmailMessageData, backfill bool) SyncLane {
	if backfill {
		return LaneBackfill
	}
	if w.SyncContext == nil || msg == nil {
		return LaneLive
	}
	if len(msg.InReplyTo) == 0 && msg.ThreadID == "" {
		return LaneLive
	}
	own, err := w.SyncContext.IsOwnConversation(ctx, w.UserID, w.ID, msg.InReplyTo, msg.ThreadID)
	if err != nil {
		log.Debug().Err(err).Str("email_id", w.ID.String()).Msg("own-conversation lookup failed; treating as live")
		return LaneLive
	}
	if own {
		return LanePriority
	}
	return LaneLive
}

// admit charges the lane for one message. When the budget is exhausted it
// records the hold on the relayed state and, for chronic daily overage,
// escalates. It returns false when the caller must leave the message on the
// server for a later pass.
func (w *WMail) admit(ctx context.Context, lane SyncLane, stats *tickStats) bool {
	if stats.laneDenied(lane) {
		if lane != LaneBackfill {
			stats.deferred++
		}
		return false
	}
	adm := w.gov.Admit(ctx, lane)
	if adm.OK {
		return true
	}
	if stats.denied == nil {
		stats.denied = map[SyncLane]Admission{}
	}
	stats.denied[lane] = adm
	if lane == LaneBackfill {
		// Pacing, not fair use: the import simply continues next tick.
		return false
	}
	stats.deferred++
	w.tracker.throttle(adm)
	if adm.Reason == models.SyncThrottleDaily && w.gov.RecordThrottledDay(ctx) {
		stats.aborted = true
		w.escalate(errx.ErrMailSyncFairUse)
	}
	return false
}

// observeLive counts new live mail for flood detection and escalates when the
// hourly volume is beyond what any real mailbox produces. A message already
// classified on an earlier pass (it is waiting on budget, so the server keeps
// re-offering it) is not counted again; otherwise a held mailbox would look
// like a flood on its own deferred backlog. Returns true when the mailbox
// was deactivated and the pass must stop.
func (w *WMail) observeLive(ctx context.Context, ids []string, stats *tickStats) bool {
	n := 0
	for _, id := range ids {
		if _, seen := w.laneCache.get(id); !seen {
			n++
		}
	}
	if n == 0 {
		return false
	}
	stats.seen += n
	if w.gov.ObserveLive(ctx, n) {
		stats.aborted = true
		w.escalate(errx.ErrMailSyncFlood)
		return true
	}
	return false
}

// laneOf classifies a new message once and remembers the answer until it is
// stored, so a deferred message costs one lookup, not one per pass.
func (w *WMail) laneOf(ctx context.Context, key string, msg *models.EmailMessageData, backfill bool) SyncLane {
	if backfill {
		return LaneBackfill
	}
	if lane, ok := w.laneCache.get(key); ok {
		return lane
	}
	lane := w.laneFor(ctx, msg, false)
	w.laneCache.put(key, lane)
	return lane
}

// beginTick releases an expired hold so the state reports "within budget"
// before the pass looks at anything.
func (w *WMail) beginTick() {
	w.tracker.release(time.Now())
}

// endTick folds the pass into the relayed state. deferred is the number of
// live messages still waiting on the server; it is reported as-is (not
// accumulated) so a pass that admits everything zeroes it.
func (w *WMail) endTick(stats *tickStats) {
	w.tracker.setDeferred(stats.deferred)
	w.tracker.touch(time.Now())
}

// storeNew is the shared tail of every provider's new-message path: record
// the provider-id map entry, store the body, emit the bounce and NEW_EMAIL
// events. mapKey is the id the provider reports on later remove/flag events
// (RFC Message-ID for IMAP, provider message id for Gmail and Graph).
func (w *WMail) storeNew(ctx context.Context, msg *models.EmailMessageData, data *models.EmailMessageStoreData, mapKey string) error {
	// Body first: a map entry without a body would make the message "known"
	// on the next pass and never retried, while an orphaned body is harmless.
	if err := w.StoreBody(ctx, data.ID, &emsg.EmailBlob{
		HTMLBody:  []byte(capBody(msg.BodyHTML)),
		PlainText: []byte(capBody(msg.BodyPlain)),
	}); err != nil {
		return err
	}

	if err := w.EmailMessageMapRepository.Add(ctx, repository.EmailMessageData{
		UserID:    w.UserID.String(),
		EmailID:   w.ID.String(),
		MessageID: mapKey,
		ID:        data.ID.String(),
		ThreadID:  data.ThreadID,
	}); err != nil {
		return err
	}

	w.maybeEmitBounce(msg)

	// The consumer decodes NEW_EMAIL as JobEventNewEmail{user_id, message}.
	return w.onEvent(models.JobEventTypeNewEmail, &models.JobEventNewEmail{
		UserID:  w.UserID,
		Message: data,
	})
}

// capBody bounds a stored body part at MaxEmailBodySize. IMAP already reads
// at most that much off the wire; Gmail and Graph hand over whole bodies, so
// without this an oversized message stored unbounded on those providers.
func capBody(s string) string {
	if len(s) <= config.MaxEmailBodySize {
		return s
	}
	return s[:config.MaxEmailBodySize]
}
