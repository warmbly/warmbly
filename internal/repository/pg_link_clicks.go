package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// LinkClick is one recorded click on one tracked link. Machine clicks (a
// security gateway walking every link at delivery time) are kept for the
// record with the reason they were flagged, but never count as engagement.
type LinkClick struct {
	ID            uuid.UUID
	TrackedLinkID *uuid.UUID
	TaskID        uuid.UUID
	CampaignID    uuid.UUID
	ContactID     uuid.UUID
	SequenceID    uuid.UUID
	Destination   string
	Label         string
	UserAgent     string
	IPHash        string
	Machine       bool
	MachineReason string
	ClickedAt     time.Time
	// Origin is what the click said about where it came from.
	Origin models.EngagementOrigin
	// AnnouncePending marks a person's click whose effects wait for the
	// burst window; the row is the durable record of that work.
	AnnouncePending bool
}

// Machine-click reasons stored in email_link_clicks.machine_reason.
const (
	LinkClickReasonPrefetch = "prefetch" // no user agent: never a person's browser
	LinkClickReasonInstant  = "instant"  // arrived inside the machine window after dispatch
	LinkClickReasonBurst    = "burst"    // a second link of the same email from the same source within seconds
	LinkClickReasonScanner  = "scanner"  // walked from a known mail-filtering network
)

// LinkClickRepository is the per-link click log behind the contact timeline
// and the scanner heuristics. Only the tracking consumer writes it.
type LinkClickRepository interface {
	Insert(ctx context.Context, click *LinkClick) error
	// CountRecentOtherLinks counts clicks from the same source on OTHER links
	// of the same email since the given time: the burst signal. A link is
	// identified by its ticket when known, else by destination (events from
	// an older tracking build).
	CountRecentOtherLinks(ctx context.Context, taskID uuid.UUID, ipHash string, linkID *uuid.UUID, destination string, since time.Time) (int, error)
	// MarkBurst flags the source's earlier human-labelled clicks on the email
	// since the given time as machine, once a burst is recognised.
	MarkBurst(ctx context.Context, taskID uuid.UUID, ipHash string, since time.Time) (int64, error)
	// HasHumanClick reports whether any click on the step is still labelled
	// as a person's.
	HasHumanClick(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) (bool, error)
	// IsMachine reads a logged click's current classification, which a burst
	// recognised after the fact may have changed.
	IsMachine(ctx context.Context, id uuid.UUID) (bool, error)
	// HasHumanClickOn reports whether a person already clicked this exact
	// link of the email, so a repeat is not logged twice. The ticket is the
	// identity when known (two links may share a destination); the
	// destination is the fallback for events from an older tracking build.
	HasHumanClickOn(ctx context.Context, taskID uuid.UUID, linkID *uuid.UUID, destination string) (bool, error)
	// ClaimAnnounce leases a pending click's announcement for one attempt
	// and reports the click's classification at that moment. claimed is
	// false when another attempt holds a live lease or the announcement is
	// done. A lease that expires without CompleteAnnounce is offered again.
	ClaimAnnounce(ctx context.Context, id uuid.UUID) (claimed bool, machine bool, err error)
	// CompleteAnnounce records that the click's effects ran, so neither the
	// timer nor the sweep offers it again.
	CompleteAnnounce(ctx context.Context, id uuid.UUID) error
	// ListPendingAnnouncements returns clicks whose announcement is still
	// pending, not under a live lease, and whose burst window closed before
	// `before`: what a consumer restart or a failed attempt left behind.
	ListPendingAnnouncements(ctx context.Context, before time.Time, limit int) ([]LinkClick, error)
	// Cleanup deletes clicks older than the retention window.
	Cleanup(ctx context.Context, olderThanDays int) (int64, error)
}

type linkClickRepository struct {
	db *pgxpool.Pool
}

// NewLinkClickRepository creates a new link click repository.
func NewLinkClickRepository(db *pgxpool.Pool) LinkClickRepository {
	return &linkClickRepository{db: db}
}

func (r *linkClickRepository) Insert(ctx context.Context, c *LinkClick) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.ClickedAt.IsZero() {
		c.ClickedAt = time.Now()
	}
	query := `
		INSERT INTO email_link_clicks
			(id, tracked_link_id, task_id, campaign_id, contact_id, sequence_id,
			 destination, label, user_agent, ip_hash, machine, machine_reason, clicked_at,
			 client, device_type, os, browser, browser_version, country_code, region, city,
			 client_type, device_hidden, announce_pending)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
		        $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)
	`
	_, err := r.db.Exec(ctx, query,
		c.ID, c.TrackedLinkID, c.TaskID, c.CampaignID, c.ContactID, c.SequenceID,
		c.Destination, c.Label, c.UserAgent, c.IPHash, c.Machine, c.MachineReason, c.ClickedAt,
		c.Origin.Client, c.Origin.DeviceType, c.Origin.OS, c.Origin.Browser, c.Origin.BrowserVersion,
		c.Origin.CountryCode, c.Origin.Region, c.Origin.City,
		c.Origin.ClientType, c.Origin.DeviceHidden, c.AnnouncePending,
	)
	return err
}

// announceLease is how long one attempt at a click's effects may take before
// the sweep is allowed to try again.
const announceLease = "2 minutes"

func (r *linkClickRepository) ClaimAnnounce(ctx context.Context, id uuid.UUID) (bool, bool, error) {
	query := `
		UPDATE email_link_clicks
		SET announce_claimed_at = NOW()
		WHERE id = $1 AND announce_pending
		  AND (announce_claimed_at IS NULL OR announce_claimed_at < NOW() - INTERVAL '` + announceLease + `')
		RETURNING machine
	`
	var machine bool
	err := r.db.QueryRow(ctx, query, id).Scan(&machine)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, machine, nil
}

func (r *linkClickRepository) CompleteAnnounce(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE email_link_clicks SET announce_pending = false WHERE id = $1`, id)
	return err
}

func (r *linkClickRepository) ListPendingAnnouncements(ctx context.Context, before time.Time, limit int) ([]LinkClick, error) {
	query := `
		SELECT id, tracked_link_id, task_id, campaign_id, contact_id, sequence_id,
		       destination, label, machine, machine_reason, clicked_at,
		       client, device_type, os, browser, browser_version, country_code, region, city,
		       client_type, device_hidden
		FROM email_link_clicks
		WHERE announce_pending AND clicked_at < $1
		  AND (announce_claimed_at IS NULL OR announce_claimed_at < NOW() - INTERVAL '` + announceLease + `')
		ORDER BY clicked_at ASC
		LIMIT $2
	`
	rows, err := r.db.Query(ctx, query, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LinkClick
	for rows.Next() {
		var c LinkClick
		if err := rows.Scan(&c.ID, &c.TrackedLinkID, &c.TaskID, &c.CampaignID, &c.ContactID, &c.SequenceID,
			&c.Destination, &c.Label, &c.Machine, &c.MachineReason, &c.ClickedAt,
			&c.Origin.Client, &c.Origin.DeviceType, &c.Origin.OS, &c.Origin.Browser, &c.Origin.BrowserVersion,
			&c.Origin.CountryCode, &c.Origin.Region, &c.Origin.City,
			&c.Origin.ClientType, &c.Origin.DeviceHidden); err != nil {
			return nil, err
		}
		c.AnnouncePending = true
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *linkClickRepository) Cleanup(ctx context.Context, olderThanDays int) (int64, error) {
	return DeleteInBatches(ctx, r.db, "email_link_clicks",
		`SELECT ctid FROM email_link_clicks WHERE clicked_at < NOW() - $2 * INTERVAL '1 day' LIMIT $1`, olderThanDays)
}

func (r *linkClickRepository) CountRecentOtherLinks(ctx context.Context, taskID uuid.UUID, ipHash string, linkID *uuid.UUID, destination string, since time.Time) (int, error) {
	// A row is "another link" when both sides have a ticket and they differ;
	// a row without a ticket (older tracking build) is compared by
	// destination so a repeat click on one link never reads as a burst.
	query := `
		SELECT COUNT(*)
		FROM email_link_clicks
		WHERE task_id = $1
		  AND ip_hash = $2
		  AND clicked_at >= $3
		  AND CASE
		        WHEN tracked_link_id IS NOT NULL AND $4::uuid IS NOT NULL THEN tracked_link_id <> $4::uuid
		        ELSE destination <> $5
		      END
	`
	var n int
	err := r.db.QueryRow(ctx, query, taskID, ipHash, since, linkID, destination).Scan(&n)
	return n, err
}

func (r *linkClickRepository) MarkBurst(ctx context.Context, taskID uuid.UUID, ipHash string, since time.Time) (int64, error) {
	query := `
		UPDATE email_link_clicks
		SET machine = true, machine_reason = $4, announce_pending = false
		WHERE task_id = $1
		  AND ip_hash = $2
		  AND clicked_at >= $3
		  AND machine = false
	`
	tag, err := r.db.Exec(ctx, query, taskID, ipHash, since, LinkClickReasonBurst)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *linkClickRepository) HasHumanClick(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM email_link_clicks
			WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3 AND machine = false
		)
	`
	var ok bool
	err := r.db.QueryRow(ctx, query, campaignID, contactID, sequenceID).Scan(&ok)
	return ok, err
}

func (r *linkClickRepository) IsMachine(ctx context.Context, id uuid.UUID) (bool, error) {
	var machine bool
	err := r.db.QueryRow(ctx, `SELECT machine FROM email_link_clicks WHERE id = $1`, id).Scan(&machine)
	return machine, err
}

func (r *linkClickRepository) HasHumanClickOn(ctx context.Context, taskID uuid.UUID, linkID *uuid.UUID, destination string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM email_link_clicks
			WHERE task_id = $1 AND destination = $2 AND machine = false
		)
	`
	args := []any{taskID, destination}
	if linkID != nil {
		query = `
			SELECT EXISTS (
				SELECT 1 FROM email_link_clicks
				WHERE task_id = $1 AND tracked_link_id = $2 AND machine = false
			)
		`
		args = []any{taskID, *linkID}
	}
	var ok bool
	err := r.db.QueryRow(ctx, query, args...).Scan(&ok)
	return ok, err
}
