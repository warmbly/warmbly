package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/app/tz"
	"github.com/warmbly/warmbly/internal/bitmask"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

type CampaignRepository interface {
	Create(ctx context.Context, userID string, orgID *uuid.UUID, data *models.CreateCampaign) (*models.Campaign, *errx.Error)
	Get(ctx context.Context, userID, id string) (*models.Campaign, error)
	GetByID(ctx context.Context, campaignID uuid.UUID) (*models.Campaign, error)
	GetSequenceByID(ctx context.Context, sequenceID uuid.UUID) (*models.Sequence, error)
	GetSequencesByCampaignID(ctx context.Context, campaignID uuid.UUID) ([]models.Sequence, error)
	Search(ctx context.Context, userID, query string, cursor, folder *string, limit int32) (*models.CampaignsResult, error)
	Update(ctx context.Context, userID, query string, data *models.UpdateCampaign) (*models.Campaign, *errx.Error)
	UpdateStatus(ctx context.Context, campaignID uuid.UUID, status string) error
	UpdateStatusWithLock(ctx context.Context, campaignID uuid.UUID, status string) error
	PauseAllByUserID(ctx context.Context, userID uuid.UUID, reason string) error
	Delete(ctx context.Context, userID, id string) error

	// Campaign start/stop
	StartCampaign(ctx context.Context, campaignID uuid.UUID) error
	StopCampaign(ctx context.Context, campaignID uuid.UUID) error
	ValidateCampaignReady(ctx context.Context, campaignID uuid.UUID) error
	GetPendingCampaignTasks(ctx context.Context, campaignID uuid.UUID) ([]Task, error)
	CountActiveForOrganization(ctx context.Context, orgID uuid.UUID) (int, error)
	AccountHasActiveCampaign(ctx context.Context, accountID uuid.UUID) (bool, error)
	// CountActiveCampaignsForAccount returns how many active campaigns send
	// from the given mailbox (matched through the campaign's email tags OR an
	// explicit campaign_senders row). Used by the warmup scheduler to keep a
	// low-volume health-check warmup running whenever a mailbox is in use by a
	// live campaign.
	CountActiveCampaignsForAccount(ctx context.Context, accountID uuid.UUID) (int, error)

	// ── Explicit sender pool (feature 1) ────────────────────────────────
	// GetCampaignSenders returns the campaign's explicit sender rows.
	GetCampaignSenders(ctx context.Context, campaignID uuid.UUID) ([]models.CampaignSender, error)
	// ReplaceCampaignSenders atomically replaces the explicit sender pool
	// (delete-all + multi-insert in one tx). Every account must belong to the
	// campaign owner; an empty list clears the pool (the campaign then resolves
	// from its email tags, or all active mailboxes when it has none).
	ReplaceCampaignSenders(ctx context.Context, campaignID uuid.UUID, in []models.CampaignSenderInput) ([]models.CampaignSender, *errx.Error)
	// AdvanceCampaignSender bumps the round-robin cursor and stamps last_sent_at
	// for the chosen mailbox in a single atomic UPDATE. Fires only on a genuine
	// send so the round-robin/LRU cursors stay coherent under concurrency.
	AdvanceCampaignSender(ctx context.Context, campaignID, accountID uuid.UUID) error

	// ── Per-campaign ramp + daily counters (features 2 & 4) ─────────────
	// AdvanceRampLevel advances the persisted ramp level once per UTC day.
	// Idempotent and a no-op when ramp is disabled or already advanced today.
	AdvanceRampLevel(ctx context.Context, campaignID uuid.UUID) error
	// IncrementCampaignDailySend bumps today's send counters; newLead also
	// increments new_leads_started (a position-1 send).
	IncrementCampaignDailySend(ctx context.Context, campaignID uuid.UUID, newLead bool) error
	// CountNewLeadsStartedToday returns new_leads_started for the current UTC
	// day (0 when no row exists yet).
	CountNewLeadsStartedToday(ctx context.Context, campaignID uuid.UUID) (int, error)

	// ── Campaign-scoped tracking domain (feature 5) ─────────────────────
	// SetCampaignTrackingDomainVerified flips the verified flag / timestamp on
	// the campaign-scoped tracking-domain override.
	SetCampaignTrackingDomainVerified(ctx context.Context, campaignID uuid.UUID, verified bool, at *time.Time) error
}

type campaignRepository struct {
	DB *db.DB
}

func NewCampaignRepostory(db *db.DB) CampaignRepository {
	return &campaignRepository{
		DB: db,
	}
}

// CAMPAIGN_SELECT and its scan dests (getCampaign), CAMPAIGN_SELECT_FULL +
// getCampaignFull, and the hand-written GetByID Scan MUST stay in lockstep:
// the new send-control columns are appended AFTER created_at in the base list
// (and BETWEEN created_at and the tag/folder aggregates in the _FULL list) so
// every scanner reads the same column order. A mismatch is a runtime scan error
// that compiles and lints cleanly — change all four together.
const CAMPAIGN_SELECT = `id, name, description, status,
		  stop_on_reply, open_tracking, link_tracking,
		  text_only, daily_limit, unsubscribe_header, risky_emails,
		  cc_addr, bcc_addr, start_date, end_date, timezone, days,
		  start_time, end_time,
		  contact_order_by, contact_order_dir, contact_order_field,
		  updated_at, created_at,
		  sender_strategy, rotation_mode,
		  ramp_enabled, ramp_start, ramp_increment, ramp_ceiling, ramp_level, ramp_level_date,
		  esp_match_mode, max_new_leads_per_day, prioritize_new_leads,
		  tracking_domain, tracking_domain_verified, tracking_domain_verified_at,
		  schedule_windows`

func getCampaign(rows db.Scannable, campaign *models.Campaign, extra ...any) error {
	var dest []any = []any{
		&campaign.ID, &campaign.Name, &campaign.Description, &campaign.Status,
		&campaign.StopOnReply, &campaign.OpenTracking, &campaign.LinkTracking,
		&campaign.TextOnly, &campaign.DailyLimit, &campaign.UnsubscribeHeader, &campaign.RiskyEmails,
		&campaign.CC, &campaign.BCC, &campaign.StartDate, &campaign.EndDate, &campaign.Timezone, &campaign.Days,
		&campaign.StartTime, &campaign.EndTime,
		&campaign.ContactOrderBy, &campaign.ContactOrderDir, &campaign.ContactOrderField,
		&campaign.UpdatedAt, &campaign.CreatedAt,
		&campaign.SenderStrategy, &campaign.RotationMode,
		&campaign.RampEnabled, &campaign.RampStart, &campaign.RampIncrement, &campaign.RampCeiling, &campaign.RampLevel, &campaign.RampLevelDate,
		&campaign.ESPMatchMode, &campaign.MaxNewLeadsPerDay, &campaign.PrioritizeNewLeads,
		&campaign.TrackingDomain, &campaign.TrackingDomainVerified, &campaign.TrackingDomainVerifiedAt,
		&campaign.ScheduleWindows,
	}
	dest = append(dest, extra...)
	return rows.Scan(
		dest...,
	)
}

const CAMPAIGN_SELECT_FULL = `
	c.id, c.name, c.description, c.status,
	c.stop_on_reply, c.open_tracking, c.link_tracking,
	c.text_only, c.daily_limit, c.unsubscribe_header, c.risky_emails,
	c.cc_addr, c.bcc_addr, c.start_date, c.end_date, c.timezone, c.days,
	c.start_time, c.end_time,
	c.contact_order_by, c.contact_order_dir, c.contact_order_field,
	c.updated_at, c.created_at,
	c.sender_strategy, c.rotation_mode,
	c.ramp_enabled, c.ramp_start, c.ramp_increment, c.ramp_ceiling, c.ramp_level, c.ramp_level_date,
	c.esp_match_mode, c.max_new_leads_per_day, c.prioritize_new_leads,
	c.tracking_domain, c.tracking_domain_verified, c.tracking_domain_verified_at,
	c.schedule_windows,
	COALESCE(array_agg(cet.tag_id) FILTER (WHERE cet.tag_id IS NOT NULL), '{}') AS email_tag_ids,
	COALESCE(array_agg(cec.folder_id) FILTER (WHERE cec.folder_id IS NOT NULL), '{}') AS email_folder_ids
`

func getCampaignFull(rows db.Scannable, campaign *models.Campaign) error {
	return getCampaign(rows, campaign, &campaign.EmailTags, &campaign.Folders)
}

// Create inserts a new campaign and, in the same transaction, applies every
// optional bit of initial config the caller sent (schedule, tracking flags,
// sender pool, initial sequences, A/B variants, advanced overrides). The
// previous version omitted updated_at/created_at which are NOT NULL and have
// no DEFAULT, so any call returned a 500.
func (r *campaignRepository) Create(ctx context.Context, userID string, orgID *uuid.UUID, data *models.CreateCampaign) (*models.Campaign, *errx.Error) {
	// Validate all optional inputs up front so we don't open a tx for a
	// payload we'll reject. Required fields are validated by the service.
	days := bitmask.DefaultDays()
	if data.Days != nil {
		if err := validate.CampaignDays(*data.Days); err != nil {
			return nil, err
		}
		days = *data.Days
	}
	timezone := "Europe/London"
	if data.Timezone != nil {
		if !tz.Valid(*data.Timezone) {
			return nil, errx.ErrTimezone
		}
		timezone = *data.Timezone
	}
	startTime := "08:00"
	if data.StartTime != nil {
		if err := validate.CampaignTime(*data.StartTime); err != nil {
			return nil, err
		}
		startTime = *data.StartTime
	}
	endTime := "18:00"
	if data.EndTime != nil {
		if err := validate.CampaignTime(*data.EndTime); err != nil {
			return nil, err
		}
		endTime = *data.EndTime
	}
	if data.StartDate != nil {
		if err := validate.CampaignStartDate(*data.StartDate); err != nil {
			return nil, err
		}
	}
	if data.EndDate != nil {
		if err := validate.CampaignEndDate(*data.EndDate); err != nil {
			return nil, err
		}
	}
	dailyLimit := config.CampaignLimitDefault
	if data.DailyLimit != nil {
		if err := validate.CampaignDailyLimit(*data.DailyLimit); err != nil {
			return nil, err
		}
		dailyLimit = *data.DailyLimit
	}
	if data.CC != nil && !validate.EmailBulk(data.CC) {
		return nil, errx.ErrEmail
	}
	if data.BCC != nil && !validate.EmailBulk(data.BCC) {
		return nil, errx.ErrEmail
	}
	for i, seq := range data.Sequences {
		if len(seq.Name) > 50 {
			return nil, errx.ErrSequenceName
		}
		if len(seq.Subject) > config.SequenceSubjectLimit {
			return nil, errx.ErrSequenceSubject
		}
		if len(seq.BodyPlain) > config.SequenceBodyLimit || len(seq.BodyHTML) > config.SequenceBodyLimit {
			return nil, errx.ErrSequenceBody
		}
		if seq.Name == "" {
			data.Sequences[i].Name = fmt.Sprintf("Step %d", i+1)
		}
	}
	for _, v := range data.Variants {
		if v.Name == "" {
			return nil, errx.New(errx.BadRequest, "A/B variant name is required")
		}
		if len(v.Subject) > config.SequenceSubjectLimit {
			return nil, errx.ErrSequenceSubject
		}
		if len(v.BodyPlain) > config.SequenceBodyLimit || len(v.BodyHTML) > config.SequenceBodyLimit {
			return nil, errx.ErrSequenceBody
		}
	}

	cc := data.CC
	if cc == nil {
		cc = []string{}
	}
	bcc := data.BCC
	if bcc == nil {
		bcc = []string{}
	}

	stopOnReply := false
	if data.StopOnReply != nil {
		stopOnReply = *data.StopOnReply
	}
	openTracking := false
	if data.OpenTracking != nil {
		openTracking = *data.OpenTracking
	}
	linkTracking := false
	if data.LinkTracking != nil {
		linkTracking = *data.LinkTracking
	}
	textOnly := false
	if data.TextOnly != nil {
		textOnly = *data.TextOnly
	}
	unsubHeader := true
	if data.UnsubscribeHeader != nil {
		unsubHeader = *data.UnsubscribeHeader
	}
	riskyEmails := true
	if data.RiskyEmails != nil {
		riskyEmails = *data.RiskyEmails
	}

	// ── Net-new send controls. Defaults reproduce today's behavior exactly. ──
	senderStrategy := "tags"
	if data.SenderStrategy != nil {
		if err := validate.CampaignSenderStrategy(*data.SenderStrategy); err != nil {
			return nil, err
		}
		senderStrategy = *data.SenderStrategy
	}
	rotationMode := "weighted"
	if data.RotationMode != nil {
		if err := validate.CampaignRotationMode(*data.RotationMode); err != nil {
			return nil, err
		}
		rotationMode = *data.RotationMode
	}
	rampEnabled := false
	if data.RampEnabled != nil {
		rampEnabled = *data.RampEnabled
	}
	rampStart := config.CampaignRampStartDefault
	if data.RampStart != nil {
		rampStart = *data.RampStart
	}
	rampIncrement := config.CampaignRampIncrementDefault
	if data.RampIncrement != nil {
		rampIncrement = *data.RampIncrement
	}
	rampCeiling := config.CampaignRampCeilingDefault
	if data.RampCeiling != nil {
		rampCeiling = *data.RampCeiling
	}
	if data.RampEnabled != nil || data.RampStart != nil || data.RampIncrement != nil || data.RampCeiling != nil {
		if err := validate.CampaignRamp(rampStart, rampIncrement, rampCeiling); err != nil {
			return nil, err
		}
	}
	espMatchMode := "off"
	if data.ESPMatchMode != nil {
		if err := validate.CampaignESPMatchMode(*data.ESPMatchMode); err != nil {
			return nil, err
		}
		espMatchMode = *data.ESPMatchMode
	}
	maxNewLeads := 0
	if data.MaxNewLeadsPerDay != nil {
		if err := validate.CampaignMaxNewLeads(*data.MaxNewLeadsPerDay); err != nil {
			return nil, err
		}
		maxNewLeads = *data.MaxNewLeadsPerDay
	}
	prioritizeNewLeads := false
	if data.PrioritizeNewLeads != nil {
		prioritizeNewLeads = *data.PrioritizeNewLeads
	}
	trackingDomain := ""
	if data.TrackingDomain != nil {
		if err := validate.CampaignTrackingDomain(*data.TrackingDomain); err != nil {
			return nil, err
		}
		trackingDomain = strings.TrimSpace(strings.ToLower(*data.TrackingDomain))
	}
	// An explicit-strategy campaign must ship at least one sender (the scheduler
	// otherwise falls back to tags, but persisting an empty explicit pool is a
	// configuration error the caller should fix up front).
	if senderStrategy == "explicit" && len(data.Senders) == 0 {
		return nil, errx.New(errx.BadRequest, "explicit sender strategy requires at least one sender")
	}

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	var committed bool
	defer func() {
		if !committed {
			tx.Rollback(ctx)
		}
	}()

	var campaign models.Campaign

	insertSQL := fmt.Sprintf(`
		INSERT INTO campaigns (
			id, name, description, user_id, organization_id,
			stop_on_reply, open_tracking, link_tracking, text_only,
			daily_limit, unsubscribe_header, risky_emails,
			cc_addr, bcc_addr,
			start_date, end_date, timezone, days, start_time, end_time,
			sender_strategy, rotation_mode,
			ramp_enabled, ramp_start, ramp_increment, ramp_ceiling,
			esp_match_mode, max_new_leads_per_day, prioritize_new_leads,
			tracking_domain,
			created_at, updated_at
		) VALUES (
			gen_random_uuid(), $1, $2, $3, $4,
			$5, $6, $7, $8,
			$9, $10, $11,
			$12, $13,
			$14, $15, $16, $17, $18, $19,
			$20, $21,
			$22, $23, $24, $25,
			$26, $27, $28,
			$29,
			NOW(), NOW()
		)
		RETURNING %s
	`, CAMPAIGN_SELECT)

	params := []any{
		data.Name,          // $1
		data.Description,   // $2
		userID,             // $3
		orgID,              // $4 (nullable)
		stopOnReply,        // $5
		openTracking,       // $6
		linkTracking,       // $7
		textOnly,           // $8
		dailyLimit,         // $9
		unsubHeader,        // $10
		riskyEmails,        // $11
		cc,                 // $12
		bcc,                // $13
		data.StartDate,     // $14
		data.EndDate,       // $15
		timezone,           // $16
		days,               // $17
		startTime,          // $18
		endTime,            // $19
		senderStrategy,     // $20
		rotationMode,       // $21
		rampEnabled,        // $22
		rampStart,          // $23
		rampIncrement,      // $24
		rampCeiling,        // $25
		espMatchMode,       // $26
		maxNewLeads,        // $27
		prioritizeNewLeads, // $28
		trackingDomain,     // $29
	}

	row := tx.QueryRow(ctx, insertSQL, params...)
	if err := getCampaign(row, &campaign); err != nil {
		db.CaptureError(err, insertSQL, params, "queryrow")
		return nil, errx.InternalError()
	}
	if orgID != nil {
		campaign.OrganizationID = orgID
	}

	// Sender pool — email tag links.
	campaign.EmailTags = make([]string, 0)
	if len(data.EmailTagIDs) > 0 {
		tags, xerr := SyncCampaignEmailTags(ctx, tx, campaign.ID.String(), data.EmailTagIDs)
		if xerr != nil {
			return nil, xerr
		}
		campaign.EmailTags = tags
	}

	// Folder links.
	campaign.Folders = make([]string, 0)
	if len(data.FolderIDs) > 0 {
		folders, xerr := SyncCampaignFolders(ctx, tx, campaign.ID.String(), data.FolderIDs)
		if xerr != nil {
			return nil, xerr
		}
		campaign.Folders = folders
	}

	// Explicit sender pool (feature 1). Only persisted when the caller passed a
	// list; validation/ownership checks live in syncCampaignSendersTx.
	if len(data.Senders) > 0 {
		orgStr := ""
		if orgID != nil {
			orgStr = orgID.String()
		}
		senders, xerr := syncCampaignSendersTx(ctx, tx, campaign.ID, userID, orgStr, data.Senders)
		if xerr != nil {
			return nil, xerr
		}
		campaign.Senders = senders
	}

	// Initial sequences. Position is the array index; wait_after defaults
	// to 0 for the first step and 3 days for any follow-ups so a default
	// wizard run still produces something usable.
	if len(data.Sequences) > 0 {
		for i, seq := range data.Sequences {
			waitAfter := 0
			if i > 0 {
				waitAfter = 3
			}
			if seq.WaitAfter != nil {
				if *seq.WaitAfter < 0 || *seq.WaitAfter > config.SequenceWaitAfterMax {
					return nil, errx.ErrSequenceWaitAfter
				}
				waitAfter = *seq.WaitAfter
			}
			bodySync := true
			if seq.BodySync != nil {
				bodySync = *seq.BodySync
			}
			bodyCode := false
			if seq.BodyCode != nil {
				bodyCode = *seq.BodyCode
			}
			bodyHTML := seq.BodyHTML
			if bodyHTML == "" {
				bodyHTML = "<div></div>"
			}
			seqInsert := `
				INSERT INTO sequences (
					campaign_id, organization_id, name, subject,
					body_plain, body_html, body_sync, body_code,
					wait_after, position
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			`
			seqParams := []any{
				campaign.ID, orgID, seq.Name, seq.Subject,
				seq.BodyPlain, bodyHTML, bodySync, bodyCode,
				waitAfter, i + 1,
			}
			if _, err := tx.Exec(ctx, seqInsert, seqParams...); err != nil {
				db.CaptureError(err, seqInsert, seqParams, "exec")
				return nil, errx.InternalError()
			}
		}
	}

	// A/B variants.
	for _, v := range data.Variants {
		weight := v.Weight
		if weight <= 0 {
			weight = 100
		}
		isActive := true
		if v.IsActive != nil {
			isActive = *v.IsActive
		}
		metaBytes, mErr := json.Marshal(v.Metadata)
		if mErr != nil {
			metaBytes = []byte(`{}`)
		}
		variantInsert := `
			INSERT INTO campaign_ab_variants (
				campaign_id, name, weight, subject, body_html, body_plain,
				is_control, is_active, metadata, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
		`
		variantParams := []any{
			campaign.ID, v.Name, weight, v.Subject, v.BodyHTML, v.BodyPlain,
			v.IsControl, isActive, metaBytes,
		}
		if _, err := tx.Exec(ctx, variantInsert, variantParams...); err != nil {
			db.CaptureError(err, variantInsert, variantParams, "exec")
			return nil, errx.InternalError()
		}
	}

	// Advanced overrides (bounce policy, intent rules, dashboard toggles).
	if data.AdvancedOverrides != nil {
		settingsJSON, mErr := json.Marshal(data.AdvancedOverrides)
		if mErr != nil {
			settingsJSON = []byte(`{}`)
		}
		settingsSQL := `
			INSERT INTO campaign_advanced_settings (campaign_id, settings, updated_at)
			VALUES ($1, $2, NOW())
			ON CONFLICT (campaign_id) DO UPDATE
			  SET settings = EXCLUDED.settings, updated_at = NOW()
		`
		if _, err := tx.Exec(ctx, settingsSQL, campaign.ID, settingsJSON); err != nil {
			db.CaptureError(err, settingsSQL, []any{campaign.ID}, "exec")
			return nil, errx.InternalError()
		}
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}
	committed = true

	return &campaign, nil
}

func (r *campaignRepository) Get(ctx context.Context, userID, id string) (*models.Campaign, error) {
	var campaign models.Campaign

	query := fmt.Sprintf(
		`SELECT %s
		 FROM campaigns c
		 LEFT JOIN campaign_email_tags cet ON cet.campaign_id = c.id
		 LEFT JOIN campaign_folders cec ON cec.campaign_id = c.id
		 WHERE c.user_id = $1 AND c.id = $2
		 GROUP BY c.id`,
		CAMPAIGN_SELECT_FULL,
	)

	params := []any{
		userID,
		id,
	}

	row := r.DB.QueryRow(
		ctx,
		query,
		params...,
	)
	err := getCampaignFull(row, &campaign)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrResourceNotFound
		}
		db.CaptureError(err, query, params, "queryrow")
		return nil, err
	}

	return &campaign, nil
}

func (r *campaignRepository) Search(ctx context.Context, userID, query string, cursor, folder *string, limit int32) (*models.CampaignsResult, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, err
	}
	defer tx.Rollback(ctx)
	campaigns := make([]models.Campaign, 0, limit+1)

	sql := fmt.Sprintf(`
		SELECT %s
		FROM campaigns c
		LEFT JOIN campaign_email_tags cet ON cet.campaign_id = c.id
		LEFT JOIN campaign_folders cec ON cec.campaign_id = c.id
		WHERE user_id = $1
		 AND ($2::uuid IS NULL OR (c.created_at, c.id) < (
		  SELECT created_at, id
		  FROM campaigns
		  WHERE id = $2
		 ))
		 AND ($3 = '' OR c.name ILIKE '%%' || $3 || '%%')
		 AND ($4::uuid IS NULL OR EXISTS (
		  SELECT 1 FROM campaign_folders cf WHERE cf.campaign_id = c.id AND cf.folder_id = $4
		 ))
		GROUP BY c.id
		ORDER BY created_at DESC
		LIMIT %d`,
		CAMPAIGN_SELECT_FULL, limit,
	)

	var countSQL string
	if cursor == nil {
		countSQL = `
			SELECT COUNT(DISTINCT c.id)
			FROM campaigns c
			LEFT JOIN campaign_folders cec ON cec.campaign_id = c.id
			WHERE user_id = $1
			  AND ($2 = '' OR c.name ILIKE '%%' || $2 || '%%')
			  AND ($3::uuid IS NULL OR EXISTS (
				SELECT 1 FROM campaign_folders cf WHERE cf.campaign_id = c.id AND cf.folder_id = $3
			  ))
		`
	}

	params := []any{
		userID,
		cursor,
		query,
		folder,
	}

	rows, err := tx.Query(
		ctx,
		sql,
		params...,
	)
	if err != nil {
		db.CaptureError(err, sql, params, "query")
		return nil, err
	}
	defer rows.Close()

	// campaigns is a 0-length slice with capacity limit+1 — append (not
	// index assignment) is required, otherwise the first iteration panics
	// "index out of range". That bug blanked the Campaigns page for any
	// user who actually had campaigns.
	for rows.Next() {
		var campaign models.Campaign
		err = getCampaignFull(rows, &campaign)
		if err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, err
		}
		campaigns = append(campaigns, campaign)
	}

	var total *int64
	var nextCursor *uuid.UUID
	var hasMore bool
	if len(campaigns) > int(limit) {
		hasMore = true
		nextCursor = &campaigns[limit].ID
		campaigns = campaigns[:limit]
	}

	if cursor == nil && countSQL != "" {
		params := []any{
			userID,
			query,
			folder,
		}
		var tmp int64
		err = tx.QueryRow(ctx, countSQL, userID, query, folder).Scan(&tmp)
		if err != nil {
			db.CaptureError(err, countSQL, params, "queryrow")
			return nil, err
		}
		total = &tmp
	}

	return &models.CampaignsResult{
		Data: campaigns,
		Pagination: models.Pagination{
			Total:      total,
			NextCursor: nextCursor,
			HasMore:    hasMore,
		},
	}, nil
}

func (r *campaignRepository) Delete(ctx context.Context, userID, campaignID string) error {
	query := `
		DELETE FROM campaigns WHERE user_id = $1 AND id = $2
	`

	params := []any{
		userID,
		campaignID,
	}

	cmd, err := r.DB.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return err
	}

	if cmd.RowsAffected() == 0 {
		return errx.ErrResourceNotFound
	}

	return nil
}

func (r *campaignRepository) Update(ctx context.Context, userID, campaignID string, data *models.UpdateCampaign) (*models.Campaign, *errx.Error) {
	setClauses := []string{}
	args := []any{userID, campaignID}
	argPos := 3

	if data.Name != nil {
		if err := validate.CampaignName(*data.Name); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "name", argPos))
		args = append(args, *data.Name)
		argPos++
	}
	if data.Description != nil {
		if err := validate.CampaignDescription(*data.Description); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "description", argPos))
		args = append(args, *data.Description)
		argPos++
	}
	if data.Status != nil {
		// Valid statuses: draft, active, paused, completed, paused_trial_expired
		status := *data.Status
		validStatuses := map[string]bool{
			"draft": true, "active": true, "paused": true,
			"completed": true, "paused_trial_expired": true,
			"paused_no_accounts": true,
		}
		if !validStatuses[status] {
			return nil, errx.ErrInvalid
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "status", argPos))
		args = append(args, status)
		argPos++
	}
	if data.StopOnReply != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "stop_on_reply", argPos))
		args = append(args, *data.StopOnReply)
		argPos++
	}
	if data.OpenTracking != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "open_tracking", argPos))
		args = append(args, *data.OpenTracking)
		argPos++
	}
	if data.LinkTracking != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "link_tracking", argPos))
		args = append(args, *data.LinkTracking)
		argPos++
	}
	if data.TextOnly != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "text_only", argPos))
		args = append(args, *data.TextOnly)
		argPos++
	}
	if data.DailyLimit != nil {
		if err := validate.CampaignDailyLimit(*data.DailyLimit); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "daily_limit", argPos))
		args = append(args, *data.DailyLimit)
		argPos++
	}
	if data.UnsubscribeHeader != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "unsubscribe_header", argPos))
		args = append(args, *data.UnsubscribeHeader)
		argPos++
	}
	if data.RiskyEmails != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "risky_emails", argPos))
		args = append(args, *data.RiskyEmails)
		argPos++
	}
	if data.CC != nil {
		if !validate.EmailBulk(data.CC) {
			return nil, errx.ErrEmail
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "cc_addr", argPos))
		args = append(args, data.CC)
		argPos++
	}
	if data.BCC != nil {
		if !validate.EmailBulk(data.BCC) {
			return nil, errx.ErrEmail
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "bcc_addr", argPos))
		args = append(args, data.BCC)
		argPos++
	}
	if data.StartDate != nil {
		if err := validate.CampaignStartDate(*data.StartDate); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "start_date", argPos))
		args = append(args, *data.StartDate)
		argPos++
	}
	if data.EndDate != nil {
		if err := validate.CampaignEndDate(*data.EndDate); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "end_date", argPos))
		args = append(args, *data.EndDate)
		argPos++
	}
	if data.Timezone != nil {
		if !tz.Valid(*data.Timezone) {
			return nil, errx.ErrTimezone
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "timezone", argPos))
		args = append(args, *data.Timezone)
		argPos++
	}
	if data.Days != nil {
		if err := validate.CampaignDays(*data.Days); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "days", argPos))
		args = append(args, *data.Days)
		argPos++
	}
	if data.StartTime != nil {
		if err := validate.CampaignTime(*data.StartTime); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "start_time", argPos))
		args = append(args, *data.StartTime)
		argPos++
	}
	if data.EndTime != nil {
		if err := validate.CampaignTime(*data.EndTime); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "end_time", argPos))
		args = append(args, *data.EndTime)
		argPos++
	}
	if data.ScheduleWindows != nil {
		if err := validate.CampaignScheduleWindows(data.ScheduleWindows); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "schedule_windows", argPos))
		args = append(args, *data.ScheduleWindows)
		argPos++
	}
	if data.ContactOrderBy != nil {
		validOrderBy := map[string]bool{
			"created_at": true, "email": true, "name": true,
			"custom_field": true, "manual": true,
		}
		if !validOrderBy[*data.ContactOrderBy] {
			return nil, errx.ErrInvalid
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "contact_order_by", argPos))
		args = append(args, *data.ContactOrderBy)
		argPos++
	}
	if data.ContactOrderDir != nil {
		if *data.ContactOrderDir != "asc" && *data.ContactOrderDir != "desc" {
			return nil, errx.ErrInvalid
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "contact_order_dir", argPos))
		args = append(args, *data.ContactOrderDir)
		argPos++
	}
	if data.ContactOrderField != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "contact_order_field", argPos))
		args = append(args, *data.ContactOrderField)
		argPos++
	}

	// ── Net-new send controls ───────────────────────────────────────────
	if data.SenderStrategy != nil {
		if err := validate.CampaignSenderStrategy(*data.SenderStrategy); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "sender_strategy", argPos))
		args = append(args, *data.SenderStrategy)
		argPos++
	}
	if data.RotationMode != nil {
		if err := validate.CampaignRotationMode(*data.RotationMode); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "rotation_mode", argPos))
		args = append(args, *data.RotationMode)
		argPos++
	}
	if data.RampEnabled != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "ramp_enabled", argPos))
		args = append(args, *data.RampEnabled)
		argPos++
	}
	if data.RampStart != nil {
		if *data.RampStart < 1 || *data.RampStart > 100 {
			return nil, errx.New(errx.BadRequest, "ramp start must be between 1 and 100")
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "ramp_start", argPos))
		args = append(args, *data.RampStart)
		argPos++
	}
	if data.RampIncrement != nil {
		if *data.RampIncrement < 0 || *data.RampIncrement > 100 {
			return nil, errx.New(errx.BadRequest, "ramp increment must be between 0 and 100")
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "ramp_increment", argPos))
		args = append(args, *data.RampIncrement)
		argPos++
	}
	if data.RampCeiling != nil {
		if *data.RampCeiling < 1 || *data.RampCeiling > 100 {
			return nil, errx.New(errx.BadRequest, "ramp ceiling must be between 1 and 100")
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "ramp_ceiling", argPos))
		args = append(args, *data.RampCeiling)
		argPos++
	}
	if data.RampStart != nil && data.RampCeiling != nil && *data.RampStart > *data.RampCeiling {
		return nil, errx.New(errx.BadRequest, "ramp start cannot exceed ramp ceiling")
	}
	if data.ESPMatchMode != nil {
		if err := validate.CampaignESPMatchMode(*data.ESPMatchMode); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "esp_match_mode", argPos))
		args = append(args, *data.ESPMatchMode)
		argPos++
	}
	if data.MaxNewLeadsPerDay != nil {
		if err := validate.CampaignMaxNewLeads(*data.MaxNewLeadsPerDay); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "max_new_leads_per_day", argPos))
		args = append(args, *data.MaxNewLeadsPerDay)
		argPos++
	}
	if data.PrioritizeNewLeads != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "prioritize_new_leads", argPos))
		args = append(args, *data.PrioritizeNewLeads)
		argPos++
	}
	if data.TrackingDomain != nil {
		if err := validate.CampaignTrackingDomain(*data.TrackingDomain); err != nil {
			return nil, err
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "tracking_domain", argPos))
		args = append(args, *data.TrackingDomain)
		argPos++
		// Any change to the override invalidates a prior verification until
		// the CNAME is re-resolved (only a verified override is honored).
		setClauses = append(setClauses, "tracking_domain_verified = false", "tracking_domain_verified_at = NULL")
	}

	if argPos == 3 && data.EmailTags == nil && data.Folders == nil {
		return nil, errx.ErrNotEnough
	}

	setClauses = append(setClauses, "updated_at = now()")

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}

	var committed bool
	defer func() {
		if !committed {
			tx.Rollback(ctx)
		}
	}()

	var query string
	if argPos > 3 {
		query = fmt.Sprintf(`
			UPDATE campaigns
			SET %s
			WHERE user_id = $1 AND id = $2
			RETURNING %s
		`, strings.Join(setClauses, ", "), CAMPAIGN_SELECT)
	} else {
		query = fmt.Sprintf(`
			SELECT %s 
			FROM campaigns
			WHERE user_id = $1 AND id = $2
		`, CAMPAIGN_SELECT)
	}

	var campaign models.Campaign

	row := tx.QueryRow(ctx, query, args...)
	err = getCampaign(row, &campaign)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, query, args, "queryrow")
		return nil, errx.InternalError()
	}

	campaign.EmailTags = make([]string, 0)
	if data.EmailTags != nil {
		var err *errx.Error
		campaign.EmailTags, err = SyncCampaignEmailTags(ctx, tx, campaignID, data.EmailTags)
		if err != nil {
			return nil, err
		}
	}

	campaign.Folders = make([]string, 0)
	if data.Folders != nil {
		var err *errx.Error
		campaign.Folders, err = SyncCampaignFolders(ctx, tx, campaignID, data.Folders)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}
	committed = true

	return &campaign, nil
}

// GetByID retrieves a campaign by ID without requiring userID (for internal service use)
func (r *campaignRepository) GetByID(ctx context.Context, campaignID uuid.UUID) (*models.Campaign, error) {
	var campaign models.Campaign

	query := fmt.Sprintf(
		`SELECT c.user_id, %s
		 FROM campaigns c
		 LEFT JOIN campaign_email_tags cet ON cet.campaign_id = c.id
		 LEFT JOIN campaign_folders cec ON cec.campaign_id = c.id
		 WHERE c.id = $1
		 GROUP BY c.id`,
		CAMPAIGN_SELECT_FULL,
	)

	row := r.DB.QueryRow(ctx, query, campaignID)
	err := row.Scan(
		&campaign.UserID,
		&campaign.ID, &campaign.Name, &campaign.Description, &campaign.Status,
		&campaign.StopOnReply, &campaign.OpenTracking, &campaign.LinkTracking,
		&campaign.TextOnly, &campaign.DailyLimit, &campaign.UnsubscribeHeader, &campaign.RiskyEmails,
		&campaign.CC, &campaign.BCC, &campaign.StartDate, &campaign.EndDate, &campaign.Timezone, &campaign.Days,
		&campaign.StartTime, &campaign.EndTime,
		&campaign.ContactOrderBy, &campaign.ContactOrderDir, &campaign.ContactOrderField,
		&campaign.UpdatedAt, &campaign.CreatedAt,
		&campaign.SenderStrategy, &campaign.RotationMode,
		&campaign.RampEnabled, &campaign.RampStart, &campaign.RampIncrement, &campaign.RampCeiling, &campaign.RampLevel, &campaign.RampLevelDate,
		&campaign.ESPMatchMode, &campaign.MaxNewLeadsPerDay, &campaign.PrioritizeNewLeads,
		&campaign.TrackingDomain, &campaign.TrackingDomainVerified, &campaign.TrackingDomainVerifiedAt,
		&campaign.ScheduleWindows,
		&campaign.EmailTags, &campaign.Folders,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrResourceNotFound
		}
		db.CaptureError(err, query, []any{campaignID}, "queryrow")
		return nil, err
	}

	return &campaign, nil
}

// GetSequenceByID retrieves a sequence by ID
func (r *campaignRepository) GetSequenceByID(ctx context.Context, sequenceID uuid.UUID) (*models.Sequence, error) {
	query := `
		SELECT id, name, subject, body_plain, body_html, body_sync, body_code, wait_after, kind, action, updated_at, created_at
		FROM sequences
		WHERE id = $1
	`

	var seq models.Sequence
	err := r.DB.QueryRow(ctx, query, sequenceID).Scan(
		&seq.ID, &seq.Name, &seq.Subject, &seq.BodyPlain, &seq.BodyHTML,
		&seq.BodySync, &seq.BodyCode, &seq.WaitAfter, &seq.Kind, &seq.Action, &seq.UpdatedAt, &seq.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrResourceNotFound
		}
		db.CaptureError(err, query, []any{sequenceID}, "queryrow")
		return nil, err
	}

	return &seq, nil
}

// GetSequencesByCampaignID retrieves all sequences for a campaign ordered by position
func (r *campaignRepository) GetSequencesByCampaignID(ctx context.Context, campaignID uuid.UUID) ([]models.Sequence, error) {
	query := `
		SELECT id, name, subject, body_plain, body_html, body_sync, body_code, wait_after, position, updated_at, created_at
		FROM sequences
		WHERE campaign_id = $1
		ORDER BY position ASC, created_at ASC
	`

	rows, err := r.DB.Query(ctx, query, campaignID)
	if err != nil {
		db.CaptureError(err, query, []any{campaignID}, "query")
		return nil, err
	}
	defer rows.Close()

	var sequences []models.Sequence
	for rows.Next() {
		var seq models.Sequence
		err := rows.Scan(
			&seq.ID, &seq.Name, &seq.Subject, &seq.BodyPlain, &seq.BodyHTML,
			&seq.BodySync, &seq.BodyCode, &seq.WaitAfter, &seq.Position, &seq.UpdatedAt, &seq.CreatedAt,
		)
		if err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, err
		}
		sequences = append(sequences, seq)
	}

	return sequences, rows.Err()
}

// validCampaignTransitions defines which status transitions are allowed.
// Key is the current status, values are the statuses it can transition to.
var validCampaignTransitions = map[string]map[string]bool{
	"draft":                {"active": true},
	"active":               {"paused": true, "completed": true, "paused_no_accounts": true, "paused_trial_expired": true},
	"paused":               {"active": true, "draft": true},
	"paused_no_accounts":   {"active": true, "paused": true},
	"paused_trial_expired": {"active": true, "paused": true},
	"completed":            {}, // terminal state
}

// UpdateStatus updates only the status of a campaign with state machine validation
func (r *campaignRepository) UpdateStatus(ctx context.Context, campaignID uuid.UUID, status string) error {
	query := `UPDATE campaigns SET status = $1, updated_at = NOW() WHERE id = $2 AND status != $1`

	// Validate that the transition is allowed
	var currentStatus string
	if err := r.DB.QueryRow(ctx, `SELECT status FROM campaigns WHERE id = $1`, campaignID).Scan(&currentStatus); err != nil {
		return err
	}
	allowed, ok := validCampaignTransitions[currentStatus]
	if !ok || !allowed[status] {
		return fmt.Errorf("invalid campaign transition from %q to %q", currentStatus, status)
	}

	_, err := r.DB.Exec(ctx, query, status, campaignID)
	return err
}

// PauseAllByUserID pauses all active campaigns for a user
func (r *campaignRepository) PauseAllByUserID(ctx context.Context, userID uuid.UUID, reason string) error {
	query := `UPDATE campaigns SET status = $1, updated_at = NOW() WHERE user_id = $2 AND status = 'active'`
	_, err := r.DB.Exec(ctx, query, reason, userID)
	return err
}

// StartCampaign sets campaign status to active and updates last_status_change_at.
// When ramp is enabled and not yet started (ramp_level = 0), it also seeds the
// ramp at ramp_start for today so the first day sends at the ramp floor rather
// than ramp_start+increment. Pause/resume preserves ramp_level, so a resume
// continues from the persisted level (this only fires on a fresh start).
func (r *campaignRepository) StartCampaign(ctx context.Context, campaignID uuid.UUID) error {
	query := `
		UPDATE campaigns
		SET status = 'active',
		    last_status_change_at = NOW(),
		    updated_at = NOW(),
		    ramp_level = CASE WHEN ramp_enabled AND ramp_level = 0 THEN ramp_start ELSE ramp_level END,
		    ramp_level_date = CASE WHEN ramp_enabled AND ramp_level = 0 THEN CURRENT_DATE ELSE ramp_level_date END
		WHERE id = $1`
	_, err := r.DB.Exec(ctx, query, campaignID)
	return err
}

// StopCampaign sets campaign status to paused and updates last_status_change_at
func (r *campaignRepository) StopCampaign(ctx context.Context, campaignID uuid.UUID) error {
	query := `UPDATE campaigns SET status = 'paused', last_status_change_at = NOW(), updated_at = NOW() WHERE id = $1`
	_, err := r.DB.Exec(ctx, query, campaignID)
	return err
}

// ValidateCampaignReady checks if a campaign has sequences, contacts, and email tags
func (r *campaignRepository) ValidateCampaignReady(ctx context.Context, campaignID uuid.UUID) error {
	// Check sequences
	var seqCount int
	err := r.DB.QueryRow(ctx, `SELECT COUNT(*) FROM sequences WHERE campaign_id = $1`, campaignID).Scan(&seqCount)
	if err != nil {
		return err
	}
	if seqCount == 0 {
		return errx.New(errx.BadRequest, "campaign must have at least one sequence")
	}

	// Check contacts
	var contactCount int
	err = r.DB.QueryRow(ctx, `SELECT COUNT(*) FROM campaign_leads WHERE campaign_id = $1`, campaignID).Scan(&contactCount)
	if err != nil {
		return err
	}
	if contactCount == 0 {
		return errx.New(errx.BadRequest, "campaign must have at least one contact")
	}

	// Sender pool (unified): valid if it has any enabled explicit sender OR any
	// email tag OR — when neither is selected ("all") — at least one active
	// mailbox for the owner to fall back to.
	var senderCount int
	if err := r.DB.QueryRow(ctx, `SELECT COUNT(*) FROM campaign_senders WHERE campaign_id = $1 AND enabled`, campaignID).Scan(&senderCount); err != nil {
		return err
	}
	var tagCount int
	if err := r.DB.QueryRow(ctx, `SELECT COUNT(*) FROM campaign_email_tags WHERE campaign_id = $1`, campaignID).Scan(&tagCount); err != nil {
		return err
	}
	if senderCount > 0 || tagCount > 0 {
		return nil
	}
	var activeMailboxes int
	if err := r.DB.QueryRow(ctx, `
		SELECT COUNT(*) FROM email_accounts
		WHERE user_id = (SELECT user_id FROM campaigns WHERE id = $1) AND status = 'active'
	`, campaignID).Scan(&activeMailboxes); err != nil {
		return err
	}
	if activeMailboxes == 0 {
		return errx.New(errx.BadRequest, "campaign must have at least one active sending mailbox")
	}
	return nil
}

// GetPendingCampaignTasks returns all pending tasks for a campaign
func (r *campaignRepository) GetPendingCampaignTasks(ctx context.Context, campaignID uuid.UUID) ([]Task, error) {
	query := `
		SELECT t.id, t.task_type, t.email_account_id, t.status, t.message_id,
		       t.scheduled_at, t.completed_at, t.cloud_task_name, t.created_at, t.updated_at
		FROM tasks t
		JOIN campaign_tasks ct ON ct.task_id = t.id
		WHERE ct.campaign_id = $1 AND t.status = 'pending'
	`

	rows, err := r.DB.Query(ctx, query, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var task Task
		err := rows.Scan(
			&task.ID, &task.TaskType, &task.EmailAccountID, &task.Status, &task.MessageID,
			&task.ScheduledAt, &task.CompletedAt, &task.CloudTaskName, &task.CreatedAt, &task.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	return tasks, rows.Err()
}

func (r *campaignRepository) CountActiveForOrganization(ctx context.Context, orgID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM campaigns WHERE organization_id = $1 AND status = 'active'`
	var count int
	err := r.DB.QueryRow(ctx, query, orgID).Scan(&count)
	return count, err
}

// AccountHasActiveCampaign reports whether the mailbox backs at least one active
// campaign, counting BOTH tag-based campaigns AND explicit-sender campaigns
// (campaign_senders). The explicit lane matters for the warmup health-check
// floor: an explicit-sender mailbox sends cold and must keep its in-campaign
// warmup heartbeat just like a tag-based one.
func (r *campaignRepository) AccountHasActiveCampaign(ctx context.Context, accountID uuid.UUID) (bool, error) {
	query := `
		SELECT EXISTS (
			-- tag-resolved
			SELECT 1
			FROM email_accounts ea
			JOIN email_tags et ON et.email_id = ea.id
			JOIN campaign_email_tags cet ON cet.tag_id = et.tag_id
			JOIN campaigns c ON c.id = cet.campaign_id
			WHERE ea.id = $1
			  AND ea.status = 'active'
			  AND c.status = 'active'
			UNION ALL
			-- explicit sender
			SELECT 1
			FROM campaign_senders cs
			JOIN campaigns c ON c.id = cs.campaign_id
			JOIN email_accounts ea ON ea.id = cs.email_account_id
			WHERE cs.email_account_id = $1
			  AND cs.enabled
			  AND ea.status = 'active'
			  AND c.status = 'active'
			UNION ALL
			-- "all" campaigns (no tags, no enabled senders) back every active mailbox of their owner
			SELECT 1
			FROM campaigns c
			JOIN email_accounts ea ON ea.user_id = c.user_id
			WHERE ea.id = $1
			  AND ea.status = 'active'
			  AND c.status = 'active'
			  AND NOT EXISTS (SELECT 1 FROM campaign_email_tags cet2 WHERE cet2.campaign_id = c.id)
			  AND NOT EXISTS (SELECT 1 FROM campaign_senders cs2 WHERE cs2.campaign_id = c.id AND cs2.enabled)
		)
	`
	var exists bool
	err := r.DB.QueryRow(ctx, query, accountID).Scan(&exists)
	return exists, err
}

// CountActiveCampaignsForAccount counts distinct active campaigns the mailbox
// backs, via the campaign's email tags (tag strategy) OR an enabled
// campaign_senders row (explicit strategy).
func (r *campaignRepository) CountActiveCampaignsForAccount(ctx context.Context, accountID uuid.UUID) (int, error) {
	query := `
		SELECT COUNT(*) FROM (
			-- tag-resolved
			SELECT c.id
			FROM campaigns c
			JOIN campaign_email_tags cet ON cet.campaign_id = c.id
			JOIN email_tags et ON et.tag_id = cet.tag_id
			WHERE et.email_id = $1
			  AND c.status = 'active'
			UNION
			-- explicit sender
			SELECT c.id
			FROM campaigns c
			JOIN campaign_senders cs ON cs.campaign_id = c.id
			WHERE cs.email_account_id = $1
			  AND cs.enabled
			  AND c.status = 'active'
			UNION
			-- "all" campaigns (no tags, no enabled senders) count for every active mailbox of their owner
			SELECT c.id
			FROM campaigns c
			JOIN email_accounts ea ON ea.user_id = c.user_id
			WHERE ea.id = $1
			  AND ea.status = 'active'
			  AND c.status = 'active'
			  AND NOT EXISTS (SELECT 1 FROM campaign_email_tags cet2 WHERE cet2.campaign_id = c.id)
			  AND NOT EXISTS (SELECT 1 FROM campaign_senders cs2 WHERE cs2.campaign_id = c.id AND cs2.enabled)
		) AS active_campaigns`
	var count int
	err := r.DB.QueryRow(ctx, query, accountID).Scan(&count)
	return count, err
}

// syncCampaignSendersTx replaces the explicit sender pool inside an existing tx.
// It deletes the current rows and multi-inserts the new set, validating that
// every account is reachable in the campaign's context — by the campaign's
// organization (orgID) for org-scoped campaigns, or by the owner (userID) for a
// personal campaign with no org. rotation_position and last_sent_at are
// preserved for accounts that remain in the pool so a rewrite of weights/enabled
// flags does not reset the round-robin/LRU cursors.
func syncCampaignSendersTx(ctx context.Context, tx pgx.Tx, campaignID uuid.UUID, userID, orgID string, in []models.CampaignSenderInput) ([]models.CampaignSender, *errx.Error) {
	accountIDs := make([]uuid.UUID, 0, len(in))
	weights := make(map[uuid.UUID]int, len(in))
	enabledFor := make(map[uuid.UUID]bool, len(in))
	seen := make(map[uuid.UUID]struct{}, len(in))
	for _, s := range in {
		if s.EmailAccountID == uuid.Nil {
			return nil, errx.New(errx.BadRequest, "sender email_account_id is required")
		}
		if _, dup := seen[s.EmailAccountID]; dup {
			return nil, errx.New(errx.BadRequest, "duplicate sender email_account_id")
		}
		seen[s.EmailAccountID] = struct{}{}
		weight := config.CampaignSenderWeightDefault
		if s.Weight != nil {
			if err := validate.CampaignSenderWeight(*s.Weight); err != nil {
				return nil, err
			}
			weight = *s.Weight
		}
		enabled := true
		if s.Enabled != nil {
			enabled = *s.Enabled
		}
		accountIDs = append(accountIDs, s.EmailAccountID)
		weights[s.EmailAccountID] = weight
		enabledFor[s.EmailAccountID] = enabled
	}

	// Ownership check: every referenced mailbox must be reachable in the
	// campaign's context. Org-scoped campaigns (the org-gated senders route)
	// validate against the organization, so any member with PermManageCampaigns
	// can pick the org's mailboxes; a personal campaign with no org falls back to
	// the owner's user_id. (ownerCol is a fixed literal, never user input.)
	ownerCol, ownerVal := "user_id", userID
	if orgID != "" {
		ownerCol, ownerVal = "organization_id", orgID
	}
	var ownedCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM email_accounts WHERE `+ownerCol+` = $1 AND id = ANY($2)`,
		ownerVal, accountIDs,
	).Scan(&ownedCount); err != nil {
		db.CaptureError(err, "campaign_senders ownership", []any{ownerVal}, "queryrow")
		return nil, errx.InternalError()
	}
	if ownedCount != len(accountIDs) {
		return nil, errx.New(errx.BadRequest, "one or more sender mailboxes do not belong to this account")
	}

	// Delete rows that are no longer in the set (CASCADE-safe; preserves the
	// surviving rows so their cursors carry over).
	if _, err := tx.Exec(ctx,
		`DELETE FROM campaign_senders WHERE campaign_id = $1 AND email_account_id <> ALL($2)`,
		campaignID, accountIDs,
	); err != nil {
		db.CaptureError(err, "campaign_senders delete", []any{campaignID}, "exec")
		return nil, errx.InternalError()
	}

	// Upsert the desired rows. ON CONFLICT updates weight/enabled but leaves
	// rotation_position/last_sent_at untouched.
	for _, id := range accountIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO campaign_senders (campaign_id, email_account_id, weight, enabled)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (campaign_id, email_account_id)
			DO UPDATE SET weight = EXCLUDED.weight, enabled = EXCLUDED.enabled
		`, campaignID, id, weights[id], enabledFor[id]); err != nil {
			db.CaptureError(err, "campaign_senders upsert", []any{campaignID, id}, "exec")
			return nil, errx.InternalError()
		}
	}

	return readCampaignSendersTx(ctx, tx, campaignID)
}

// campaignSenderQuerier is satisfied by both *db.DB (via the embedded pool) and
// pgx.Tx, so readCampaignSendersTx works inside or outside a transaction.
type campaignSenderQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// readCampaignSendersTx loads the explicit sender pool inside a tx.
func readCampaignSendersTx(ctx context.Context, q campaignSenderQuerier, campaignID uuid.UUID) ([]models.CampaignSender, *errx.Error) {
	rows, err := q.Query(ctx, `
		SELECT email_account_id, weight, last_sent_at, enabled
		FROM campaign_senders
		WHERE campaign_id = $1
		ORDER BY created_at ASC, email_account_id ASC
	`, campaignID)
	if err != nil {
		db.CaptureError(err, "campaign_senders select", []any{campaignID}, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	senders := make([]models.CampaignSender, 0)
	for rows.Next() {
		var s models.CampaignSender
		if err := rows.Scan(&s.EmailAccountID, &s.Weight, &s.LastSentAt, &s.Enabled); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		senders = append(senders, s)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, "", nil, "rows")
		return nil, errx.InternalError()
	}
	return senders, nil
}

// GetCampaignSenders returns the explicit sender pool for a campaign.
func (r *campaignRepository) GetCampaignSenders(ctx context.Context, campaignID uuid.UUID) ([]models.CampaignSender, error) {
	senders, xerr := readCampaignSendersTx(ctx, r.DB, campaignID)
	if xerr != nil {
		return nil, xerr
	}
	return senders, nil
}

// ReplaceCampaignSenders atomically swaps the explicit sender pool. An empty
// list is rejected — clearing senders should be done by switching the campaign
// back to sender_strategy='tags'.
func (r *campaignRepository) ReplaceCampaignSenders(ctx context.Context, campaignID uuid.UUID, in []models.CampaignSenderInput) ([]models.CampaignSender, *errx.Error) {
	// An empty list is allowed: it clears the explicit sender pool, so the
	// campaign falls back to its email tags or, with neither, to every active
	// mailbox of the owner. syncCampaignSendersTx handles the empty set safely
	// (it deletes all current rows and inserts none).

	// Resolve the campaign owner + organization so we can validate mailbox
	// ownership against the org (the senders route is org-scoped).
	var userID string
	var orgID *uuid.UUID
	if err := r.DB.QueryRow(ctx, `SELECT user_id, organization_id FROM campaigns WHERE id = $1`, campaignID).Scan(&userID, &orgID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, "campaign owner lookup", []any{campaignID}, "queryrow")
		return nil, errx.InternalError()
	}
	orgStr := ""
	if orgID != nil {
		orgStr = orgID.String()
	}

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	var committed bool
	defer func() {
		if !committed {
			tx.Rollback(ctx)
		}
	}()

	senders, xerr := syncCampaignSendersTx(ctx, tx, campaignID, userID, orgStr, in)
	if xerr != nil {
		return nil, xerr
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}
	committed = true
	return senders, nil
}

// AdvanceCampaignSender bumps the round-robin cursor and stamps last_sent_at in
// a single atomic UPDATE (no read-modify-write), keeping cursors coherent when
// multiple campaign tasks for the same campaign run concurrently.
func (r *campaignRepository) AdvanceCampaignSender(ctx context.Context, campaignID, accountID uuid.UUID) error {
	_, err := r.DB.Exec(ctx, `
		UPDATE campaign_senders
		SET rotation_position = rotation_position + 1, last_sent_at = NOW()
		WHERE campaign_id = $1 AND email_account_id = $2
	`, campaignID, accountID)
	return err
}

// AdvanceRampLevel advances the persisted ramp level once per UTC day. It is a
// no-op when ramp is off or already advanced today. Applied via min() in the
// scheduler so it can only LOWER the effective per-mailbox cap.
func (r *campaignRepository) AdvanceRampLevel(ctx context.Context, campaignID uuid.UUID) error {
	_, err := r.DB.Exec(ctx, `
		UPDATE campaigns
		SET ramp_level = LEAST(ramp_ceiling, GREATEST(ramp_level, ramp_start) + ramp_increment),
		    ramp_level_date = CURRENT_DATE
		WHERE id = $1
		  AND ramp_enabled
		  AND (ramp_level_date IS NULL OR ramp_level_date < CURRENT_DATE)
	`, campaignID)
	return err
}

// IncrementCampaignDailySend bumps today's per-campaign send counters. newLead
// also increments new_leads_started (a position-1 send) so the new-lead cap can
// read it back.
func (r *campaignRepository) IncrementCampaignDailySend(ctx context.Context, campaignID uuid.UUID, newLead bool) error {
	newLeadInc := 0
	if newLead {
		newLeadInc = 1
	}
	_, err := r.DB.Exec(ctx, `
		INSERT INTO campaign_daily_sends (campaign_id, send_date, emails_sent, new_leads_started)
		VALUES ($1, CURRENT_DATE, 1, $2)
		ON CONFLICT (campaign_id, send_date)
		DO UPDATE SET emails_sent = campaign_daily_sends.emails_sent + 1,
		              new_leads_started = campaign_daily_sends.new_leads_started + $2
	`, campaignID, newLeadInc)
	return err
}

// CountNewLeadsStartedToday returns new_leads_started for the current UTC day.
func (r *campaignRepository) CountNewLeadsStartedToday(ctx context.Context, campaignID uuid.UUID) (int, error) {
	var n int
	err := r.DB.QueryRow(ctx, `
		SELECT COALESCE(new_leads_started, 0)
		FROM campaign_daily_sends
		WHERE campaign_id = $1 AND send_date = CURRENT_DATE
	`, campaignID).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// SetCampaignTrackingDomainVerified flips the verified flag/timestamp on the
// campaign-scoped tracking-domain override.
func (r *campaignRepository) SetCampaignTrackingDomainVerified(ctx context.Context, campaignID uuid.UUID, verified bool, at *time.Time) error {
	_, err := r.DB.Exec(ctx, `
		UPDATE campaigns
		SET tracking_domain_verified = $2, tracking_domain_verified_at = $3, updated_at = NOW()
		WHERE id = $1
	`, campaignID, verified, at)
	return err
}

// UpdateStatusWithLock updates campaign status using a PostgreSQL advisory lock to prevent concurrent updates.
// The WHERE clause guards against races: only updates if the campaign is currently 'active'.
func (r *campaignRepository) UpdateStatusWithLock(ctx context.Context, campaignID uuid.UUID, status string) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return err
	}

	var committed bool
	defer func() {
		if !committed {
			tx.Rollback(ctx)
		}
	}()

	// Acquire advisory lock scoped to this campaign's status updates
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('campaign_status_' || $1::text))`, campaignID)
	if err != nil {
		db.CaptureError(err, "", nil, "advisory_lock")
		return err
	}

	query := `UPDATE campaigns SET status = $1, last_status_change_at = NOW(), updated_at = NOW() WHERE id = $2 AND status = 'active'`
	_, err = tx.Exec(ctx, query, status, campaignID)
	if err != nil {
		db.CaptureError(err, query, []any{status, campaignID}, "exec")
		return err
	}

	err = tx.Commit(ctx)
	if err == nil {
		committed = true
	}
	return err
}
