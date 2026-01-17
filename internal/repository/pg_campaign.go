package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/app/tz"
	"github.com/warmbly/warmbly/internal/bitmask"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

type CampaignRepository interface {
	Create(ctx context.Context, userID string, data *models.CreateCampaign) (*models.Campaign, error)
	Get(ctx context.Context, userID, id string) (*models.Campaign, error)
	Search(ctx context.Context, userID, query string, cursor, folder *string, limit int32) (*models.CampaignsResult, error)
	Update(ctx context.Context, userID, query string, data *models.UpdateCampaign) (*models.Campaign, *errx.Error)
	Delete(ctx context.Context, userID, id string) error
}

type campaignRepository struct {
	DB *db.DB
}

func NewCampaignRepostory(db *db.DB) CampaignRepository {
	return &campaignRepository{
		DB: db,
	}
}

const CAMPAIGN_SELECT = `id, name, description, status,
		  stop_on_reply, open_tracking, link_tracking,
		  text_only, daily_limit, unsubscribe_header, risky_emails,
		  cc_addr, bcc_addr, start_date, end_date, timezone, days,
		  start_time, end_time,
		  updated_at, created_at`

func getCampaign(rows db.Scannable, campaign *models.Campaign, extra ...any) error {
	var dest []any = []any{
		&campaign.ID, &campaign.Name, &campaign.Description, &campaign.Status,
		&campaign.StopOnReply, &campaign.OpenTracking, &campaign.LinkTracking,
		&campaign.TextOnly, &campaign.DailyLimit, &campaign.UnsubscribeHeader, &campaign.RiskyEmails,
		&campaign.CC, &campaign.BCC, &campaign.StartDate, &campaign.EndDate, &campaign.Timezone, &campaign.Days,
		&campaign.StartTime, &campaign.EndTime,
		&campaign.UpdatedAt, &campaign.CreatedAt,
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
	c.updated_at, c.created_at,
	COALESCE(array_agg(cet.tag_id) FILTER (WHERE cet.tag IS NOT NULL), '{}') AS email_tag_ids,
	COALESCE(array_agg(cec.folder_id) FILTER (WHERE cec.folder IS NOT NULL), '{}') AS email_folder_ids
`

func getCampaignFull(rows db.Scannable, campaign *models.Campaign) error {
	return getCampaign(rows, campaign, &campaign.EmailTags, &campaign.Folders)
}

func (r *campaignRepository) Create(ctx context.Context, userID string, data *models.CreateCampaign) (*models.Campaign, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, err
	}
	defer tx.Rollback(ctx)

	var campaign models.Campaign
	campaign.EmailTags = make([]string, 0)

	query := fmt.Sprintf(
		`INSERT INTO campaigns 
		  (id, name, description, user_id, days)
		 VALUES
		  (gen_random_uuid(), $1, $2, $3, $4)
		 RETURNING %s`,
		CAMPAIGN_SELECT,
	)

	params := []any{
		data.Name,
		data.Description,
		userID,
		bitmask.DefaultDays(),
	}

	row := tx.QueryRow(
		ctx,
		query,
		params...,
	)
	err = getCampaign(row, &campaign)
	if err != nil {
		db.CaptureError(err, query, params, "queryrow")
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, err
	}

	return &campaign, nil
}

func (r *campaignRepository) Get(ctx context.Context, userID, id string) (*models.Campaign, error) {
	var campaign models.Campaign

	query := fmt.Sprintf(
		`SELECT %s
		 FROM campaigns c
		 LEFT JOIN campaign_email_tags cet ON cet.campaign = c.id
		 LEFT JOIN campaign_folders cec ON cec.campaign = c.id
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

	var i int
	for rows.Next() {
		var campaign models.Campaign
		err = getCampaignFull(rows, &campaign)
		if err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, err
		}
		campaigns[i] = campaign
		i++
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

	if argPos == 3 && data.EmailTags == nil {
		return nil, errx.ErrNotEnough
	}

	setClauses = append(setClauses, "updated_at = now()")

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

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

	return &campaign, nil
}
