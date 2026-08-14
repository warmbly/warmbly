package dispatch

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Governor struct {
	cfg       Config
	store     Store
	clock     Clock
	reserveMu sync.Mutex
}

func NewGovernor(cfg Config, store Store, clock Clock) *Governor {
	if clock == nil {
		clock = RealClock{}
	}
	if cfg.SendsPerHour <= 0 {
		cfg.SendsPerHour = DefaultSendsPerHour
	}
	// MinGap == 0 is valid (no gap); only negative means "unset".
	if cfg.MinGap < 0 {
		cfg.MinGap = time.Duration(DefaultMinGapSeconds) * time.Second
	}
	if cfg.Timezone == "" {
		cfg.Timezone = DefaultTimezone
	}
	if cfg.WindowStart == "" {
		cfg.WindowStart = DefaultWindowStart
	}
	if cfg.WindowEnd == "" {
		cfg.WindowEnd = DefaultWindowEnd
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = DefaultLeaseTTL
	}
	return &Governor{cfg: cfg, store: store, clock: clock}
}

func (g *Governor) Config() Config { return g.cfg }

func (g *Governor) SetConfig(cfg Config) {
	if cfg.SendsPerHour > 0 {
		g.cfg.SendsPerHour = cfg.SendsPerHour
	}
	if cfg.MinGap >= 0 {
		g.cfg.MinGap = cfg.MinGap
	}
	if cfg.Timezone != "" {
		g.cfg.Timezone = cfg.Timezone
	}
	if cfg.WindowStart != "" {
		g.cfg.WindowStart = cfg.WindowStart
	}
	if cfg.WindowEnd != "" {
		g.cfg.WindowEnd = cfg.WindowEnd
	}
	if cfg.LeaseTTL > 0 {
		g.cfg.LeaseTTL = cfg.LeaseTTL
	}
	g.cfg.EnvPaused = cfg.EnvPaused
	g.cfg.EnvPauseReason = cfg.EnvPauseReason
}

func (g *Governor) TryReserve(ctx context.Context, req ReserveRequest) (ReserveResult, error) {
	g.reserveMu.Lock()
	defer g.reserveMu.Unlock()

	now := g.clock.Now().UTC()
	capN := g.cfg.SendsPerHour
	if req.CapOverride > 0 && (capN < 1 || req.CapOverride < capN) {
		capN = req.CapOverride
	}
	if capN < 1 {
		capN = DefaultSendsPerHour
	}
	out := ReserveResult{Cap: capN}

	if req.MessageKey == "" {
		return out, fmt.Errorf("message_key required")
	}
	if req.Channel != ChannelEmail && req.Channel != ChannelWhatsApp {
		return out, fmt.Errorf("invalid channel %q", req.Channel)
	}

	if g.cfg.EnvPaused {
		out.Reason = "env_paused"
		if g.cfg.EnvPauseReason != "" {
			out.Reason = g.cfg.EnvPauseReason
		}
		out.NextSlot = now.Add(g.cfg.MinGap)
		return out, nil
	}
	ctrl, err := g.store.GetControl(ctx)
	if err != nil {
		return out, err
	}
	if ctrl.Paused {
		out.Reason = "paused"
		if ctrl.PauseReason != "" {
			out.Reason = ctrl.PauseReason
		}
		out.NextSlot = now.Add(g.cfg.MinGap)
		return out, nil
	}

	inWin, werr := InSendWindowBusiness(now, g.cfg.Timezone, g.cfg.WindowStart, g.cfg.WindowEnd, g.cfg.BusinessDaysOnly)
	if werr != nil {
		return out, werr
	}
	if !inWin {
		if g.cfg.BusinessDaysOnly && !IsBusinessDay(now, g.cfg.Timezone) {
			out.Reason = "outside_business_day"
		} else {
			out.Reason = "outside_send_window"
		}
		out.NextSlot = NextWindowOpenBusiness(now, g.cfg.Timezone, g.cfg.WindowStart, g.cfg.WindowEnd, g.cfg.BusinessDaysOnly)
		return out, nil
	}

	// Full reserve decision under store serialization (multi-worker safe).
	atomic, err := g.store.TryReserveAtomic(ctx, AtomicReserveInput{
		Req: req, Now: now, Cap: capN, MinGap: g.cfg.MinGap,
		LeaseTTL: g.cfg.LeaseTTL, Window: RollingWindow,
	})
	if err != nil {
		return out, err
	}
	out.Allowed = atomic.Allowed
	out.AlreadyCommitted = atomic.AlreadyCommitted
	out.Reservation = atomic.Reservation
	out.Reason = atomic.Reason
	out.NextSlot = atomic.NextSlot
	out.SentLastHour = atomic.SentLastHour
	return out, nil
}

func (g *Governor) Commit(ctx context.Context, reservationID uuid.UUID) error {
	return g.store.CommitReservation(ctx, reservationID, g.clock.Now().UTC())
}

// CommitByMessageKey records provider-confirmed delivery for an async transport.
func (g *Governor) CommitByMessageKey(ctx context.Context, messageKey string) error {
	if g == nil || g.store == nil || messageKey == "" {
		return fmt.Errorf("dispatch reservation key is unavailable")
	}
	reservation, err := g.store.GetReservationByKey(ctx, messageKey)
	if err != nil {
		return err
	}
	if reservation == nil {
		return fmt.Errorf("dispatch reservation not found for provider-confirmed send")
	}
	return g.store.CommitReservation(ctx, reservation.ID, g.clock.Now().UTC())
}

func (g *Governor) Release(ctx context.Context, reservationID uuid.UUID, errText string) error {
	state := StateReleased
	if errText != "" {
		state = StateFailed
	}
	return g.store.ReleaseReservation(ctx, reservationID, state, errText)
}

func (g *Governor) Enqueue(ctx context.Context, req EnqueueRequest) error {
	if req.DueAt.IsZero() {
		req.DueAt = g.clock.Now().UTC()
	}
	return g.store.Enqueue(ctx, &QueueItem{
		OrganizationID: req.OrganizationID, Channel: req.Channel, DraftID: req.DraftID,
		MessageKey: req.MessageKey, RecipientRef: req.RecipientRef,
		DueAt: req.DueAt.UTC(), Priority: req.Priority,
		Status: QueueQueued, CreatedAt: g.clock.Now().UTC(),
	})
}

func (g *Governor) CancelQueued(ctx context.Context, messageKey, reason string) error {
	return g.store.CancelQueue(ctx, messageKey, reason)
}

func (g *Governor) Pause(ctx context.Context, reason string, by *uuid.UUID) error {
	if reason == "" {
		reason = "manual_pause"
	}
	return g.store.SetPaused(ctx, true, reason, by)
}

func (g *Governor) Resume(ctx context.Context, by *uuid.UUID) error {
	return g.store.SetPaused(ctx, false, "", by)
}

func (g *Governor) Status(ctx context.Context, orgID *uuid.UUID) (Status, error) {
	now := g.clock.Now().UTC()
	st := Status{
		Cap: g.cfg.SendsPerHour, MinGapSeconds: int(g.cfg.MinGap / time.Second),
		Timezone: g.cfg.Timezone, WindowStart: g.cfg.WindowStart, WindowEnd: g.cfg.WindowEnd,
	}
	_, _ = g.store.ExpireStaleReservations(ctx, now)
	sent, err := g.store.CountSendsSince(ctx, now.Add(-RollingWindow))
	if err != nil {
		return st, err
	}
	st.SentLastHour = sent
	leases, err := g.store.CountActiveLeases(ctx, now)
	if err != nil {
		return st, err
	}
	st.ActiveLeases = leases
	queued, err := g.store.CountQueued(ctx, orgID)
	if err != nil {
		return st, err
	}
	st.QueuedApproved = queued
	ctrl, err := g.store.GetControl(ctx)
	if err != nil {
		return st, err
	}
	st.Paused = ctrl.Paused || g.cfg.EnvPaused
	st.PauseReason = ctrl.PauseReason
	if g.cfg.EnvPaused {
		st.Paused = true
		if g.cfg.EnvPauseReason != "" {
			st.PauseReason = g.cfg.EnvPauseReason
		} else if st.PauseReason == "" {
			st.PauseReason = "env_paused"
		}
	}
	inWin, _ := InSendWindowBusiness(now, g.cfg.Timezone, g.cfg.WindowStart, g.cfg.WindowEnd, g.cfg.BusinessDaysOnly)
	st.InSendWindow = inWin
	if st.Paused {
		t := now.Add(g.cfg.MinGap)
		st.NextSlotAt = &t
	} else if !inWin {
		t := NextWindowOpenBusiness(now, g.cfg.Timezone, g.cfg.WindowStart, g.cfg.WindowEnd, g.cfg.BusinessDaysOnly)
		st.NextSlotAt = &t
	} else {
		occupied, last, err := g.store.ListOccupied(ctx, now, RollingWindow)
		if err != nil {
			return st, err
		}
		snap := WindowSnapshot{
			OccupiedAt: occupied, LastOccupied: last,
			Cap: g.cfg.SendsPerHour, MinGap: g.cfg.MinGap, Window: RollingWindow, Now: now,
		}
		ok, _, next := snap.CanGrant()
		if !ok {
			st.NextSlotAt = &next
		}
	}
	fails, err := g.store.ListRecentFailures(ctx, DefaultMaxRecentFails)
	if err != nil {
		return st, err
	}
	st.RecentFailures = fails
	return st, nil
}

func (g *Governor) RecordFailure(ctx context.Context, orgID uuid.UUID, channel, messageKey string, draftID *uuid.UUID, errText string) error {
	oid := orgID
	return g.store.RecordFailure(ctx, FailureRecord{
		OrganizationID: &oid, Channel: channel, MessageKey: messageKey,
		DraftID: draftID, ErrorText: errText, OccurredAt: g.clock.Now().UTC(),
	})
}

// ClaimNextQueued transactionally claims the next fair due item (status -> reserved).
func (g *Governor) ClaimNextQueued(ctx context.Context) (*QueueItem, error) {
	return g.store.ClaimNextQueued(ctx, g.clock.Now().UTC())
}

// CancelByRecipient cancels queued items for a contact email/phone (DNC/opt-out/bounce).
func (g *Governor) CancelByRecipient(ctx context.Context, orgID uuid.UUID, recipientRef, reason string) (int, error) {
	return g.store.CancelQueueByRecipient(ctx, orgID, recipientRef, reason)
}

func (g *Governor) MarkQueue(ctx context.Context, id uuid.UUID, status, errText string) error {
	return g.store.UpdateQueueStatus(ctx, id, status, errText)
}

func (g *Governor) countSince(ctx context.Context, since time.Time) int {
	n, _ := g.store.CountSendsSince(ctx, since)
	return n
}
