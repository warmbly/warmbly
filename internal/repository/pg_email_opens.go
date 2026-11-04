package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// EmailOpen is one recorded open of one step: when, from what client and
// where. The progress row keeps only the first open per step; this keeps
// them all, machine ones included and labelled, for the contact's timeline
// and the campaign's audience breakdown.
type EmailOpen struct {
	ID            uuid.UUID
	TaskID        uuid.UUID
	CampaignID    uuid.UUID
	ContactID     uuid.UUID
	SequenceID    uuid.UUID
	OpenedAt      time.Time
	Machine       bool
	MachineReason string
	UserAgent     string
	IPHash        string
	Origin        models.EngagementOrigin
}

// Machine-open reasons stored in email_opens.machine_reason.
const (
	EmailOpenReasonPrefetch = "prefetch" // a mail client prefetch or a fetch with no browser
	EmailOpenReasonInstant  = "instant"  // arrived inside the machine window after dispatch
	EmailOpenReasonScanner  = "scanner"  // fetched from a known mail-filtering network
)

// EmailOpenRepository is the per-event open log. Only the tracking consumer
// writes it.
type EmailOpenRepository interface {
	Insert(ctx context.Context, open *EmailOpen) error
	// HasHumanOpen reports whether a person's own open is on record for the
	// step, which decides whether an open a click implied can be withdrawn.
	HasHumanOpen(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) (bool, error)
	// Cleanup deletes opens older than the retention window.
	Cleanup(ctx context.Context, olderThanDays int) (int64, error)
	// BackfillOriginBatch re-reads the next batch of logged opens and clicks
	// by the current origin rules.
	BackfillOriginBatch(ctx context.Context, limit int, derive OriginDeriver) (done, busy bool, err error)
}

type emailOpenRepository struct {
	db *pgxpool.Pool
}

// NewEmailOpenRepository creates a new email open repository.
func NewEmailOpenRepository(db *pgxpool.Pool) EmailOpenRepository {
	return &emailOpenRepository{db: db}
}

func (r *emailOpenRepository) Insert(ctx context.Context, o *EmailOpen) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	if o.OpenedAt.IsZero() {
		o.OpenedAt = time.Now()
	}
	query := `
		INSERT INTO email_opens
			(id, task_id, campaign_id, contact_id, sequence_id, opened_at,
			 machine, machine_reason, user_agent, ip_hash,
			 client, device_type, os, browser, browser_version, country_code, region, city,
			 client_type, device_hidden)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
	`
	_, err := r.db.Exec(ctx, query,
		o.ID, o.TaskID, o.CampaignID, o.ContactID, o.SequenceID, o.OpenedAt,
		o.Machine, o.MachineReason, o.UserAgent, o.IPHash,
		o.Origin.Client, o.Origin.DeviceType, o.Origin.OS, o.Origin.Browser, o.Origin.BrowserVersion,
		o.Origin.CountryCode, o.Origin.Region, o.Origin.City,
		o.Origin.ClientType, o.Origin.DeviceHidden,
	)
	return err
}

func (r *emailOpenRepository) HasHumanOpen(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM email_opens
			WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3 AND machine = false
		)
	`
	var ok bool
	err := r.db.QueryRow(ctx, query, campaignID, contactID, sequenceID).Scan(&ok)
	return ok, err
}

func (r *emailOpenRepository) Cleanup(ctx context.Context, olderThanDays int) (int64, error) {
	return DeleteInBatches(ctx, r.db, "email_opens",
		`SELECT ctid FROM email_opens WHERE opened_at < NOW() - $2 * INTERVAL '1 day' LIMIT $1`, olderThanDays)
}

// OriginDeriver re-reads one logged event's origin from its stored user agent
// and the location it was stored with.
type OriginDeriver func(userAgent string, click bool, stored models.EngagementOrigin) models.EngagementOrigin

// originBackfillKey is the admin_settings row holding the backfill's progress.
const originBackfillKey = "engagement.origin_backfill"

// originBackfillLock serialises backfill batches across consumers.
const originBackfillLock int64 = 0x6f7269676e // "orign"

type originBackfillState struct {
	OpensAfter  *uuid.UUID `json:"opens_after,omitempty"`
	OpensDone   bool       `json:"opens_done"`
	ClicksAfter *uuid.UUID `json:"clicks_after,omitempty"`
	ClicksDone  bool       `json:"clicks_done"`
}

// BackfillOriginBatch re-derives the origin of the next `limit` opens, then
// clicks, in id order and records how far it got, in one transaction under an
// advisory lock. busy reports another consumer holding the lock; done that
// both logs have been walked, which every later call answers from one read.
func (r *emailOpenRepository) BackfillOriginBatch(ctx context.Context, limit int, derive OriginDeriver) (done, busy bool, err error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, originBackfillLock).Scan(&locked); err != nil {
		return false, false, err
	}
	if !locked {
		return false, true, nil
	}
	var st originBackfillState
	var raw []byte
	switch err := tx.QueryRow(ctx, `SELECT value FROM admin_settings WHERE key = $1`, originBackfillKey).Scan(&raw); {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return false, false, err
	default:
		if err := json.Unmarshal(raw, &st); err != nil {
			return false, false, err
		}
	}
	if st.OpensDone && st.ClicksDone {
		return true, false, nil
	}

	table, after, click := "email_opens", st.OpensAfter, false
	if st.OpensDone {
		table, after, click = "email_link_clicks", st.ClicksAfter, true
	}
	// A nil cursor starts below every id.
	cursor := uuid.Nil
	if after != nil {
		cursor = *after
	}
	rows, err := tx.Query(ctx, `
		SELECT id, user_agent, client, client_type, device_hidden, device_type, os, browser, browser_version,
		       country_code, region, city
		FROM `+table+`
		WHERE id > $1
		ORDER BY id
		LIMIT $2`, cursor, limit)
	if err != nil {
		return false, false, err
	}
	var ids []uuid.UUID
	var cols [9][]string
	var hidden []bool
	scanned := 0
	for rows.Next() {
		var id uuid.UUID
		var ua string
		var o models.EngagementOrigin
		if err := rows.Scan(&id, &ua, &o.Client, &o.ClientType, &o.DeviceHidden, &o.DeviceType, &o.OS, &o.Browser, &o.BrowserVersion,
			&o.CountryCode, &o.Region, &o.City); err != nil {
			rows.Close()
			return false, false, err
		}
		scanned++
		cursor = id
		n := derive(ua, click, o)
		if n == o {
			continue
		}
		ids = append(ids, id)
		for i, v := range []string{n.Client, n.ClientType, n.DeviceType, n.OS, n.Browser, n.BrowserVersion, n.CountryCode, n.Region, n.City} {
			cols[i] = append(cols[i], v)
		}
		hidden = append(hidden, n.DeviceHidden)
	}
	seen := rows.Err()
	rows.Close()
	if seen != nil {
		return false, false, seen
	}

	if len(ids) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE `+table+` t
			SET client = v.client, client_type = v.client_type, device_hidden = v.device_hidden,
			    device_type = v.device_type, os = v.os, browser = v.browser, browser_version = v.browser_version,
			    country_code = v.country_code, region = v.region, city = v.city
			FROM unnest($1::uuid[], $2::text[], $3::text[], $4::bool[], $5::text[], $6::text[], $7::text[], $8::text[],
			            $9::text[], $10::text[], $11::text[])
			     AS v(id, client, client_type, device_hidden, device_type, os, browser, browser_version, country_code, region, city)
			WHERE t.id = v.id`,
			ids, cols[0], cols[1], hidden, cols[2], cols[3], cols[4], cols[5], cols[6], cols[7], cols[8]); err != nil {
			return false, false, err
		}
	}

	// A short batch reached the end of the log. Rows written after that are
	// already read by the current rules.
	last := scanned < limit
	if click {
		st.ClicksAfter, st.ClicksDone = &cursor, last
	} else {
		st.OpensAfter, st.OpensDone = &cursor, last
	}
	out, err := json.Marshal(st)
	if err != nil {
		return false, false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO admin_settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, originBackfillKey, out); err != nil {
		return false, false, err
	}
	return false, false, tx.Commit(ctx)
}
