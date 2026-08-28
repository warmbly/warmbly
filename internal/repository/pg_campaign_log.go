package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// CampaignLogEntry is the input for creating a campaign log
type CampaignLogEntry struct {
	CampaignID uuid.UUID
	EventType  string
	Message    string
	Metadata   map[string]interface{}
}

type CampaignLogRepository interface {
	CreateLog(ctx context.Context, entry *CampaignLogEntry) error
	GetLogs(ctx context.Context, campaignID uuid.UUID, limit int, cursor *string) (*models.CampaignLogsResult, error)
	// HasRecentLogFor reports whether an entry of this type already exists for
	// the campaign with metadata.<key> = value since `since`. Used to keep a
	// per-send warning from writing one feed entry per recipient.
	HasRecentLogFor(ctx context.Context, campaignID uuid.UUID, eventType, key, value string, since time.Time) (bool, error)
}

type campaignLogRepository struct {
	DB *db.DB
}

func NewCampaignLogRepository(db *db.DB) CampaignLogRepository {
	return &campaignLogRepository{DB: db}
}

func (r *campaignLogRepository) CreateLog(ctx context.Context, entry *CampaignLogEntry) error {
	metadata := entry.Metadata
	if metadata == nil {
		metadata = map[string]interface{}{}
	}

	query := `
		INSERT INTO campaign_logs (campaign_id, event_type, message, metadata)
		VALUES ($1, $2, $3, $4)
	`

	_, err := r.DB.Exec(ctx, query, entry.CampaignID, entry.EventType, entry.Message, metadata)
	if err != nil {
		db.CaptureError(err, query, []any{entry.CampaignID, entry.EventType}, "exec")
	}
	return err
}

func (r *campaignLogRepository) GetLogs(ctx context.Context, campaignID uuid.UUID, limit int, cursor *string) (*models.CampaignLogsResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var cursorTime *time.Time
	if cursor != nil && *cursor != "" {
		t, err := time.Parse(time.RFC3339Nano, *cursor)
		if err == nil {
			cursorTime = &t
		}
	}

	query := `
		SELECT id, campaign_id, event_type, message, metadata, created_at
		FROM campaign_logs
		WHERE campaign_id = $1
		  AND ($2::timestamptz IS NULL OR created_at < $2)
		ORDER BY created_at DESC
		LIMIT $3
	`

	rows, err := r.DB.Query(ctx, query, campaignID, cursorTime, limit+1)
	if err != nil {
		db.CaptureError(err, query, []any{campaignID}, "query")
		return nil, err
	}
	defer rows.Close()

	logs := make([]models.CampaignLog, 0, limit)
	for rows.Next() {
		var l models.CampaignLog
		if err := rows.Scan(&l.ID, &l.CampaignID, &l.EventType, &l.Message, &l.Metadata, &l.CreatedAt); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, err
		}
		logs = append(logs, l)
	}

	var nextCursor *string
	hasMore := len(logs) > limit
	if hasMore {
		logs = logs[:limit]
		c := logs[limit-1].CreatedAt.Format(time.RFC3339Nano)
		nextCursor = &c
	}

	return &models.CampaignLogsResult{
		Data: logs,
		Pagination: models.CPagination{
			NextCursor: nextCursor,
			HasMore:    hasMore,
		},
	}, nil
}

func (r *campaignLogRepository) HasRecentLogFor(ctx context.Context, campaignID uuid.UUID, eventType, key, value string, since time.Time) (bool, error) {
	var exists bool
	err := r.DB.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM campaign_logs
			WHERE campaign_id = $1
			  AND event_type = $2
			  AND metadata ->> $3 = $4
			  AND created_at >= $5
		)
	`, campaignID, eventType, key, value, since).Scan(&exists)
	return exists, err
}
