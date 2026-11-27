package repository

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

// partnerWeekTTL bounds how stale a candidate's seven-day counts may be. They
// weight the draw and size the inbound cap, and move by a handful of messages
// a minute across the whole pool.
const partnerWeekTTL = time.Minute

// partnerDayTTL bounds how stale the cohorts, today's counts and bounces the
// draw reads may be. Send admission re-checks the recipient's cap live.
const partnerDayTTL = 20 * time.Second

// poolSnapshotRefreshTimeout bounds one pool-wide refresh, which runs detached
// from the caller that started it.
const poolSnapshotRefreshTimeout = 30 * time.Second

// poolSnapshot is one pool-wide read shared by every sender in the process.
// An expired snapshot is served while one refresh replaces it, so no sender
// waits on the pool-wide scan once the first one has landed.
type poolSnapshot[T any] struct {
	ttl  time.Duration
	db   sendResultDB
	load func(context.Context, sendResultDB) (T, error)

	mu   sync.Mutex
	at   time.Time
	data T
	ok   bool
	// refreshing is closed when the refresh in flight ends; nil when none is.
	refreshing chan struct{}
}

func newPoolSnapshot[T any](db sendResultDB, ttl time.Duration, load func(context.Context, sendResultDB) (T, error)) *poolSnapshot[T] {
	return &poolSnapshot[T]{ttl: ttl, db: db, load: load}
}

// get returns the snapshot. Until a first refresh lands the caller waits for
// it, then reads through fallback if it failed; only the caller's own context
// ending fails the draw, which a fallback read could not survive either.
func (c *poolSnapshot[T]) get(ctx context.Context, fallback sendResultDB) (T, error) {
	c.mu.Lock()
	data, ok := c.data, c.ok
	wait := c.refreshing
	if (!ok || time.Since(c.at) >= c.ttl) && wait == nil {
		wait = make(chan struct{})
		c.refreshing = wait
		go c.refresh(context.WithoutCancel(ctx), wait)
	}
	c.mu.Unlock()
	if ok {
		return data, nil
	}
	select {
	case <-wait:
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
	c.mu.Lock()
	data, ok = c.data, c.ok
	c.mu.Unlock()
	if ok {
		return data, nil
	}
	return c.load(ctx, fallback)
}

// refresh replaces the snapshot and keeps the old one on failure.
func (c *poolSnapshot[T]) refresh(ctx context.Context, done chan struct{}) {
	ctx, cancel := context.WithTimeout(ctx, poolSnapshotRefreshTimeout)
	defer cancel()
	// The pool, not a caller's transaction: the snapshot is shared.
	data, err := c.load(ctx, c.db)
	c.mu.Lock()
	if err == nil {
		c.data, c.at, c.ok = data, time.Now(), true
	}
	c.refreshing = nil
	c.mu.Unlock()
	close(done)
}

// partnerWeek is what a mailbox sent, received and had junked over seven days,
// and what the pool handed off to it, how much of that arrived and how many
// sender workspaces lost mail to it.
type partnerWeek struct {
	sent, received, junked     int
	delivered, arrived, lostBy int
}

func (w partnerWeek) unreachable() bool {
	return models.WarmupRecipientUnreachable(w.delivered, w.arrived, w.lostBy)
}

// warmupInboundSQL selects the pool's handed-off warmup mail to the recipients
// warmup_tokens wt names, once past the arrival grace or arrived, joined to
// each sender as s.
var warmupInboundSQL = fmt.Sprintf(`
		FROM warmup_tokens wt
		JOIN email_accounts s ON s.id = wt.sender_account_id
		WHERE wt.created_at >= NOW() - interval '7 days'
		  AND (wt.created_at < NOW() - make_interval(hours => %d) OR wt.consumed_at IS NOT NULL)
		  AND wt.sent_message_id <> ''`, config.WarmupRecipientArrivalGraceHours)

// warmupInboundColumns are delivered, arrived and lost_by over warmupInboundSQL.
const warmupInboundColumns = `COUNT(*) AS delivered, COUNT(wt.consumed_at) AS arrived,
		       COUNT(DISTINCT s.organization_id) FILTER (WHERE wt.consumed_at IS NULL) AS lost_by`

var partnerWeekSQL = `
	WITH recv AS (
		SELECT wr.email_account_id AS id, COUNT(*) AS week, COUNT(sr.id) AS junk
		FROM warmup_received wr
		LEFT JOIN warmup_spam_reports sr
		  ON sr.reporter_account_id = wr.email_account_id AND sr.message_id = wr.message_id
		 AND sr.report_type = 'spam_placement' AND wr.message_id <> ''
		WHERE wr.created_at >= NOW() - interval '7 days'
		GROUP BY 1
	),
	sent AS (
		SELECT wt.sender_account_id AS id, COUNT(*) AS week
		FROM warmup_tokens wt
		WHERE wt.created_at >= NOW() - interval '7 days' AND wt.sent_message_id <> ''
		GROUP BY 1
	),
	inbound AS (
		SELECT wt.recipient_account_id AS id, ` + warmupInboundColumns + warmupInboundSQL + `
		GROUP BY 1
	)
	SELECT COALESCE(recv.id, sent.id, inbound.id), COALESCE(sent.week, 0), COALESCE(recv.week, 0), COALESCE(recv.junk, 0),
	       COALESCE(inbound.delivered, 0), COALESCE(inbound.arrived, 0), COALESCE(inbound.lost_by, 0)
	FROM recv FULL JOIN sent ON sent.id = recv.id
	FULL JOIN inbound ON inbound.id = COALESCE(recv.id, sent.id)`

func loadPartnerWeeks(ctx context.Context, db sendResultDB) (map[uuid.UUID]partnerWeek, error) {
	rows, err := db.Query(ctx, partnerWeekSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[uuid.UUID]partnerWeek)
	for rows.Next() {
		var id uuid.UUID
		var w partnerWeek
		if err := rows.Scan(&id, &w.sent, &w.received, &w.junked, &w.delivered, &w.arrived, &w.lostBy); err != nil {
			return nil, err
		}
		out[id] = w
	}
	return out, rows.Err()
}

// partnerToday is the new threads a mailbox received and was dispatched today,
// the two numbers its inbound cap is held against.
type partnerToday struct {
	received, inflight int
}

// partnerDay is the pool's new threads per recipient today and the sender
// workspaces each bounced recipient bounced.
type partnerDay struct {
	todays  map[uuid.UUID]partnerToday
	bounced map[uuid.UUID][]uuid.UUID
}

var partnerTodaySQL = `
	SELECT wr.email_account_id, COUNT(*), 0
	FROM warmup_received wr
	WHERE wr.created_at >= date_trunc('day', NOW()) AND ` + newThreadSQL("wr") + `
	GROUP BY 1
	UNION ALL
	SELECT wt.recipient_account_id, 0, COUNT(*)
	FROM warmup_tokens wt
	WHERE wt.created_at >= date_trunc('day', NOW()) AND ` + newThreadSQL("wt") + `
	GROUP BY 1`

// reservedRecipientTodaySQL recounts today for the recipients of reserved task
// $1, leaving that task out, so a revalidated send is not held against itself.
var reservedRecipientTodaySQL = `
	SELECT r.id,
	       (SELECT COUNT(*) FROM warmup_received wr
	        WHERE wr.email_account_id = r.id AND wr.created_at >= date_trunc('day', NOW()) AND ` + newThreadSQL("wr") + `),
	       (SELECT COUNT(*) FROM warmup_tokens wt
	        WHERE wt.recipient_account_id = r.id AND wt.created_at >= date_trunc('day', NOW())
	          AND wt.task_id <> $1 AND ` + newThreadSQL("wt") + `)
	FROM (SELECT DISTINCT recipient_account_id AS id FROM warmup_tokens WHERE task_id = $1) r`

func loadPartnerDay(ctx context.Context, db sendResultDB) (partnerDay, error) {
	todays, err := scanPartnerTodays(ctx, db, partnerTodaySQL)
	if err != nil {
		return partnerDay{}, err
	}
	bounced, err := scanBounced(ctx, db, warmupBouncedAllSQL)
	if err != nil {
		return partnerDay{}, err
	}
	return partnerDay{todays: todays, bounced: bounced}, nil
}

func scanPartnerTodays(ctx context.Context, db sendResultDB, query string, args ...any) (map[uuid.UUID]partnerToday, error) {
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[uuid.UUID]partnerToday)
	for rows.Next() {
		var id uuid.UUID
		var received, inflight int
		if err := rows.Scan(&id, &received, &inflight); err != nil {
			return nil, err
		}
		t := out[id]
		t.received += received
		t.inflight += inflight
		out[id] = t
	}
	return out, rows.Err()
}

// partnerDayFor is the day's snapshot, with the recipient of a reserved send
// being revalidated recounted live so the send is not counted against itself.
func (r *warmupRepository) partnerDayFor(ctx context.Context) (partnerDay, error) {
	day, err := r.partnerDay.get(ctx, resultDB(ctx, r.db))
	if err != nil {
		return partnerDay{}, err
	}
	excludeTask, _ := ctx.Value(warmupExecutionExclusion{}).(uuid.UUID)
	if excludeTask == uuid.Nil {
		return day, nil
	}
	live, err := scanPartnerTodays(ctx, resultDB(ctx, r.db), reservedRecipientTodaySQL, excludeTask)
	if err != nil || len(live) == 0 {
		return day, err
	}
	todays := maps.Clone(day.todays)
	maps.Copy(todays, live)
	return partnerDay{todays: todays, bounced: day.bounced}, nil
}

// newThreadSQL is true for warmup mail (alias has task_id) that opens a thread. A
// reply answers mail its recipient sent, so it neither counts against the
// recipient's inbound cap nor uses the share a tier leaves to the other.
func newThreadSQL(alias string) string {
	return `NOT EXISTS(SELECT 1 FROM warmup_tasks rw WHERE rw.task_id=` + alias + `.task_id AND rw.parent_task_id IS NOT NULL)`
}

// partnerInboundCap is how much one inbox may receive from the pool in a day,
// scaled to the share the sender's tier may fill. warmupInboxRoomSQL holds the
// same rule in SQL.
func partnerInboundCap(sentWeek, sharePercent int) int {
	daily := ((sentWeek + 6) / 7) * config.WarmupInboundDailyMultiple
	daily = min(max(daily, config.WarmupInboundDailyFloor), config.WarmupInboundDailyCeiling)
	return daily * sharePercent / 100
}

// warmupInboxRoomSQL is true while the new threads recipient $1 has today, on its local day, are within sender share $2 of its cap.
var warmupInboxRoomSQL = `SELECT GREATEST(
	 (SELECT COUNT(*) FROM warmup_received wr WHERE wr.email_account_id=ea.id AND wr.created_at >= date_trunc('day',NOW() AT TIME ZONE COALESCE(NULLIF(ea.timezone,''),NULLIF(o.timezone,''),'UTC')) AT TIME ZONE COALESCE(NULLIF(ea.timezone,''),NULLIF(o.timezone,''),'UTC') AND ` + newThreadSQL("wr") + `),
	 (SELECT COUNT(*) FROM warmup_tokens wt WHERE wt.recipient_account_id=ea.id AND wt.created_at >= date_trunc('day',NOW() AT TIME ZONE COALESCE(NULLIF(ea.timezone,''),NULLIF(o.timezone,''),'UTC')) AT TIME ZONE COALESCE(NULLIF(ea.timezone,''),NULLIF(o.timezone,''),'UTC') AND ` + newThreadSQL("wt") + `))
	 <= LEAST(GREATEST((((SELECT COUNT(*) FROM warmup_tokens wt WHERE wt.sender_account_id=ea.id AND wt.sent_message_id<>'' AND wt.created_at>=NOW()-INTERVAL '7 days')+6)/7)*` + inboundDailyMultipleSQL + `,` + inboundDailyFloorSQL + `),` + inboundDailyCeilingSQL + `)*$2/100
	 FROM email_accounts ea JOIN organizations o ON o.id=ea.organization_id WHERE ea.id=$1`

// inboundSharePercent: a paying sender may fill all of an inbox's daily cap; a
// free sender stops short, so the rest of every inbox's day stays open to the
// premium tier.
func inboundSharePercent(poolType string) int {
	if poolType == string(models.WarmupPoolPremium) {
		return 100
	}
	return config.WarmupFreeInboundSharePercent
}

// capPartnerCandidates attaches each candidate's seven-day counts and drops
// every one that is unreachable, bounced out or already at its inbound cap for
// today, before any count or sample is taken from the set, so a thin tier is
// sized on who can still receive. A bounce from fewer workspaces stays on the
// candidate as BouncedBy for each sender to apply to itself.
func (r *warmupRepository) capPartnerCandidates(ctx context.Context, poolType string, cands []models.WarmupPartnerCandidate) ([]models.WarmupPartnerCandidate, error) {
	if len(cands) == 0 {
		return cands, nil
	}
	weeks, err := r.partnerWeeks7d.get(ctx, resultDB(ctx, r.db))
	if err != nil {
		return nil, err
	}
	day, err := r.partnerDayFor(ctx)
	if err != nil {
		return nil, err
	}
	share := inboundSharePercent(poolType)
	out := cands[:0]
	for _, c := range cands {
		w, t := weeks[c.ID], day.todays[c.ID]
		c.BouncedBy = day.bounced[c.ID]
		if w.unreachable() || models.WarmupRecipientBouncedOut(len(c.BouncedBy)) ||
			max(t.received, t.inflight) >= partnerInboundCap(w.sent, share) {
			continue
		}
		c.Sent7d, c.Received7d, c.Junked7d = w.sent, w.received, w.junked
		c.Delivered7d, c.Arrived7d = w.delivered, w.arrived
		out = append(out, c)
	}
	return out, nil
}

// warmupBouncedFromSQL selects pool warmup sends to the recipients the
// predicate on wt.recipient_account_id names that bounced as an unknown address
// within the bounce window, with no verified arrival at the recipient since,
// joined to each sender as s.
func warmupBouncedFromSQL(recipient string) string {
	return fmt.Sprintf(`
	FROM warmup_tokens wt
	JOIN email_accounts s ON s.id = wt.sender_account_id
	WHERE %s
	  AND wt.bounced_at >= NOW() - make_interval(days => %d)
	  AND s.organization_id IS NOT NULL
	  AND NOT EXISTS (
	      SELECT 1 FROM warmup_tokens a
	      WHERE a.recipient_account_id = wt.recipient_account_id AND a.consumed_at > wt.bounced_at)`,
		recipient, config.WarmupRecipientBounceDays)
}

var (
	warmupBouncedAllSQL = `SELECT wt.recipient_account_id, s.organization_id` +
		warmupBouncedFromSQL("wt.bounced_at IS NOT NULL") + ` GROUP BY 1, 2`
	warmupBouncedOneSQL = `SELECT wt.recipient_account_id, s.organization_id` +
		warmupBouncedFromSQL("wt.recipient_account_id = $1") + ` GROUP BY 1, 2`
)

// scanBounced maps each bounced recipient to the sender workspaces it bounced.
func scanBounced(ctx context.Context, db sendResultDB, query string, args ...any) (map[uuid.UUID][]uuid.UUID, error) {
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[uuid.UUID][]uuid.UUID)
	for rows.Next() {
		var recipient, org uuid.UUID
		if err := rows.Scan(&recipient, &org); err != nil {
			return nil, err
		}
		out[recipient] = append(out[recipient], org)
	}
	return out, rows.Err()
}

// WarmupRecipientBounced reports whether senderOrg may not mail recipient
// because its address bounced that workspace's warmup, or enough workspaces'.
func (r *warmupRepository) WarmupRecipientBounced(ctx context.Context, recipient, senderOrg uuid.UUID) (bool, error) {
	// Live: the reply path decides on this one recipient.
	bounced, err := scanBounced(ctx, resultDB(ctx, r.db), warmupBouncedOneSQL, recipient)
	if err != nil {
		return false, err
	}
	c := models.WarmupPartnerCandidate{BouncedBy: bounced[recipient]}
	return c.BouncedFrom(&senderOrg) || models.WarmupRecipientBouncedOut(len(c.BouncedBy)), nil
}

// RecordWarmupBounce stamps the warmup send a permanent unknown-address DSN in
// the sender's own mailbox is about. A named failed recipient must be the
// send's recipient. Reports whether a send was stamped.
func (r *warmupRepository) RecordWarmupBounce(ctx context.Context, senderAccountID uuid.UUID, originalMessageID, failedRecipient string) (bool, error) {
	tag, err := resultDB(ctx, r.db).Exec(ctx, `
		UPDATE warmup_tokens wt SET bounced_at = NOW()
		FROM email_accounts rcpt
		WHERE rcpt.id = wt.recipient_account_id
		  AND wt.sender_account_id = $1
		  AND wt.sent_message_id <> '' AND btrim(wt.sent_message_id, '<>') = $2
		  AND wt.bounced_at IS NULL
		  AND ($3 = '' OR lower(rcpt.email) = lower($3))`,
		senderAccountID, originalMessageID, failedRecipient)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// WarmupBounceRecorder records a warmup send's unknown-address bounce.
type WarmupBounceRecorder interface {
	RecordWarmupBounce(ctx context.Context, senderAccountID uuid.UUID, originalMessageID, failedRecipient string) (bool, error)
}

// WarmupBouncedRecipients answers whether a workspace may still mail an address.
type WarmupBouncedRecipients interface {
	WarmupRecipientBounced(ctx context.Context, recipient, senderOrg uuid.UUID) (bool, error)
}
