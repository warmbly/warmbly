package repository

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

// partnerWeekTTL bounds how stale a candidate's seven-day counts may be. They
// weight the draw and size the inbound cap, and move by a handful of messages
// a minute across the whole pool; today's counts, which decide the cap, are
// always read live.
const partnerWeekTTL = time.Minute

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

// partnerWeekRefreshTimeout bounds one pool-wide refresh, which runs detached
// from the caller that started it.
const partnerWeekRefreshTimeout = 30 * time.Second

// partnerWeekCache holds the pool-wide seven-day counts for one process, so
// every sender shares one aggregate instead of rescanning the window per call.
type partnerWeekCache struct {
	ttl  time.Duration
	mu   sync.Mutex
	at   time.Time
	data map[uuid.UUID]partnerWeek
	// refreshing is closed when the refresh in flight ends; nil when none is.
	refreshing chan struct{}
}

func newPartnerWeekCache(ttl time.Duration) *partnerWeekCache {
	return &partnerWeekCache{ttl: ttl}
}

const partnerWeekSQL = `
	WITH recv AS (
		SELECT wr.email_account_id AS id, COUNT(*) AS week, COUNT(sr.id) AS junk
		FROM warmup_received wr
		LEFT JOIN warmup_spam_reports sr
		  ON sr.reporter_account_id = wr.email_account_id AND sr.message_id = wr.message_id
		 AND sr.report_type = 'spam_placement' AND wr.message_id <> ''
		WHERE wr.created_at >= NOW() - interval '7 days' %[1]s
		GROUP BY 1
	),
	sent AS (
		SELECT wt.sender_account_id AS id, COUNT(*) AS week
		FROM warmup_tokens wt
		WHERE wt.created_at >= NOW() - interval '7 days' AND wt.sent_message_id <> '' %[2]s
		GROUP BY 1
	),
	inbound AS (
		SELECT wt.recipient_account_id AS id, %[4]s %[5]s %[3]s
		GROUP BY 1
	)
	SELECT COALESCE(recv.id, sent.id, inbound.id), COALESCE(sent.week, 0), COALESCE(recv.week, 0), COALESCE(recv.junk, 0),
	       COALESCE(inbound.delivered, 0), COALESCE(inbound.arrived, 0), COALESCE(inbound.lost_by, 0)
	FROM recv FULL JOIN sent ON sent.id = recv.id
	FULL JOIN inbound ON inbound.id = COALESCE(recv.id, sent.id)`

var (
	partnerWeekAllSQL = fmt.Sprintf(partnerWeekSQL, "", "", "", warmupInboundColumns, warmupInboundSQL)
	partnerWeekIDsSQL = fmt.Sprintf(partnerWeekSQL,
		"AND wr.email_account_id = ANY($1::uuid[])", "AND wt.sender_account_id = ANY($1::uuid[])",
		"AND wt.recipient_account_id = ANY($1::uuid[])", warmupInboundColumns, warmupInboundSQL)
)

// partnerWeeks returns the seven-day counts for ids: from the shared cache
// when the repository has one, otherwise read exactly for those ids. An
// expired snapshot is served while one refresh replaces it, so no sender waits
// on the pool-wide scan once the first one has landed.
func (r *warmupRepository) partnerWeeks(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]partnerWeek, error) {
	c := r.partnerWeeks7d
	if c == nil {
		return scanPartnerWeeks(ctx, resultDB(ctx, r.db), partnerWeekIDsSQL, ids)
	}
	c.mu.Lock()
	data, fresh := c.data, c.data != nil && time.Since(c.at) < c.ttl
	wait := c.refreshing
	if !fresh && wait == nil {
		wait = make(chan struct{})
		c.refreshing = wait
		go r.refreshPartnerWeeks(context.WithoutCancel(ctx), c, wait)
	}
	c.mu.Unlock()
	if data != nil {
		return data, nil
	}
	select {
	case <-wait:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	c.mu.Lock()
	data = c.data
	c.mu.Unlock()
	if data != nil {
		return data, nil
	}
	// The first refresh failed; this caller reads its own ids exactly.
	return scanPartnerWeeks(ctx, resultDB(ctx, r.db), partnerWeekIDsSQL, ids)
}

// refreshPartnerWeeks replaces the snapshot and keeps the old one on failure.
func (r *warmupRepository) refreshPartnerWeeks(ctx context.Context, c *partnerWeekCache, done chan struct{}) {
	ctx, cancel := context.WithTimeout(ctx, partnerWeekRefreshTimeout)
	defer cancel()
	// The pool, not a caller's transaction: the snapshot is shared.
	data, err := scanPartnerWeeks(ctx, r.db, partnerWeekAllSQL)
	c.mu.Lock()
	if err == nil {
		c.data, c.at = data, time.Now()
	}
	c.refreshing = nil
	c.mu.Unlock()
	close(done)
}

func scanPartnerWeeks(ctx context.Context, db sendResultDB, query string, args ...any) (map[uuid.UUID]partnerWeek, error) {
	rows, err := db.Query(ctx, query, args...)
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

// partnerTodays reads today's arrivals and dispatches live. A reserved send
// being revalidated is left out of its own recipient's count.
func (r *warmupRepository) partnerTodays(ctx context.Context, ids []uuid.UUID, excludeTask uuid.UUID) (map[uuid.UUID]partnerToday, error) {
	rows, err := resultDB(ctx, r.db).Query(ctx, `
		SELECT wr.email_account_id, COUNT(*), 0
		FROM warmup_received wr
		WHERE wr.created_at >= date_trunc('day', NOW()) AND wr.email_account_id = ANY($1::uuid[])
		  AND `+newThreadSQL("wr")+`
		GROUP BY 1
		UNION ALL
		SELECT wt.recipient_account_id, 0, COUNT(*)
		FROM warmup_tokens wt
		WHERE wt.created_at >= date_trunc('day', NOW()) AND wt.recipient_account_id = ANY($1::uuid[])
		  AND wt.task_id <> $2 AND `+newThreadSQL("wt")+`
		GROUP BY 1`, ids, excludeTask)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[uuid.UUID]partnerToday, len(ids))
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
// every one that is unreachable or already at its inbound cap for today,
// before any count or sample is taken from the set, so a thin tier is sized on
// who can still receive.
func (r *warmupRepository) capPartnerCandidates(ctx context.Context, poolType string, cands []models.WarmupPartnerCandidate) ([]models.WarmupPartnerCandidate, error) {
	if len(cands) == 0 {
		return cands, nil
	}
	ids := make([]uuid.UUID, len(cands))
	for i, c := range cands {
		ids[i] = c.ID
	}
	weeks, err := r.partnerWeeks(ctx, ids)
	if err != nil {
		return nil, err
	}
	excludeTask, _ := ctx.Value(warmupExecutionExclusion{}).(uuid.UUID)
	todays, err := r.partnerTodays(ctx, ids, excludeTask)
	if err != nil {
		return nil, err
	}
	share := inboundSharePercent(poolType)
	out := cands[:0]
	for _, c := range cands {
		w, t := weeks[c.ID], todays[c.ID]
		if w.unreachable() || max(t.received, t.inflight) >= partnerInboundCap(w.sent, share) {
			continue
		}
		c.Sent7d, c.Received7d, c.Junked7d = w.sent, w.received, w.junked
		c.Delivered7d, c.Arrived7d = w.delivered, w.arrived
		out = append(out, c)
	}
	return out, nil
}
