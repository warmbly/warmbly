package inboxtag

import "time"

// ThreadState contains the stored classification and timestamps used for follow-up labels.
type ThreadState struct {
	ThreadID string
	// LastInboundAt and LastOutboundAt are the most recent message each way.
	// Zero means there has never been one.
	LastInboundAt  time.Time
	LastOutboundAt time.Time
	// BestIntent is the most recent trusted intent already classified in this thread.
	BestIntent string
	// Kind of the most recent classified inbound message. A thread whose only
	// inbound was a bounce or an autoresponder is not a conversation.
	LastKind string
}

// FollowUp decides which follow-up label, if any, a thread should wear.
//
// Returns "" for a thread that should wear none, which is most of them: a
// finished conversation, one nobody is waiting on, or one there is nothing to
// chase. An empty answer is a real answer here, and the sync removes whatever
// the thread was wearing before.
//
// Pure, so it is free to re-run over every thread as often as we like. That
// matters: these states change with the calendar, not with any event, so
// "recompute everything" has to be cheap enough to do on a schedule.
func FollowUp(s ThreadState, now time.Time) string {
	// A thread whose inbound was a bounce, an autoresponder or a platform
	// notice is not somebody thinking it over. Chasing it would be chasing a
	// mail server.
	if IsAutomatedKind(s.LastKind) {
		return ""
	}

	// They said no, or asked to be left alone. Never chase. This check comes
	// before everything else because it is the one with a cost attached to
	// getting it wrong.
	if IsClosedIntent(s.BestIntent) {
		return ""
	}

	hasInbound := !s.LastInboundAt.IsZero()
	hasOutbound := !s.LastOutboundAt.IsZero()

	// Nothing has been sent: not a thread we are waiting on.
	if !hasOutbound {
		return ""
	}

	// They spoke last. The ball is ours, and the only question is whether we
	// have been sitting on it long enough to say so.
	if hasInbound && s.LastInboundAt.After(s.LastOutboundAt) {
		// An unknown inbound message may be automated rather than a person waiting on us.
		if s.LastKind == "" {
			return ""
		}
		if daysSince(s.LastInboundAt, now) >= OurCourtDays {
			return LabelNeedsReply
		}
		return ""
	}

	// We spoke last. How long ago, and was it going anywhere?
	days := daysSince(s.LastOutboundAt, now)

	// A thread that reached a positive intent and then went quiet is the
	// expensive silence, so it gets its own label and a longer fuse.
	if IsPositiveIntent(s.BestIntent) {
		if days >= GoingColdDays {
			return LabelGoneQuiet
		}
		return ""
	}

	if days >= FollowUpDueDays {
		return LabelFollowUp
	}
	return ""
}

// daysSince counts whole days between two instants. Kept as its own function
// because it is the only arithmetic here and it is the thing a reader will want
// to check when a label appears a day early or late.
func daysSince(t, now time.Time) int {
	if t.IsZero() || now.Before(t) {
		return 0
	}
	return int(now.Sub(t).Hours() / 24)
}
