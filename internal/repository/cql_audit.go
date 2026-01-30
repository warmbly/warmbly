package repository

import (
	"context"
	"encoding/base64"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cdb"
	"github.com/warmbly/warmbly/internal/models"
)

type AuditRepository interface {
	Log(ctx context.Context, log *models.CreateAuditLog) error
	GetByUser(ctx context.Context, userID uuid.UUID, date time.Time, limit int, cursor string) (*models.AuditLogsResult, error)
	GetByEntity(ctx context.Context, entityType string, entityID uuid.UUID, limit int, cursor string) (*models.AuditLogsResult, error)
	Search(ctx context.Context, params *models.AuditLogSearch) (*models.AuditLogsResult, error)
}

type auditRepository struct {
	db *cdb.Client
}

func NewAuditRepository(db *cdb.Client) AuditRepository {
	return &auditRepository{db: db}
}

func (r *auditRepository) Log(ctx context.Context, log *models.CreateAuditLog) error {
	id := uuid.New()
	now := time.Now()
	actionDate := now.Truncate(24 * time.Hour)

	batch := r.db.Session.NewBatch(gocql.LoggedBatch).WithContext(ctx)

	// Insert into main audit_logs table
	mainQuery := `
		INSERT INTO audit_logs (user_id, action_date, timestamp, id, action, entity_type, entity_id, ip_address, user_agent, changes, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	batch.Query(mainQuery,
		log.UserID, actionDate, now, id,
		string(log.Action), string(log.EntityType), log.EntityID,
		log.IPAddress, log.UserAgent,
		log.Changes, log.Metadata,
	)

	// Insert into entity lookup table if entity ID is present
	if log.EntityID != nil {
		entityQuery := `
			INSERT INTO audit_logs_by_entity (entity_type, entity_id, timestamp, id, user_id, action)
			VALUES (?, ?, ?, ?, ?, ?)
		`
		batch.Query(entityQuery,
			string(log.EntityType), log.EntityID,
			now, id, log.UserID, string(log.Action),
		)
	}

	return r.db.Session.ExecuteBatch(batch)
}

func (r *auditRepository) GetByUser(ctx context.Context, userID uuid.UUID, date time.Time, limit int, cursor string) (*models.AuditLogsResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	actionDate := date.Truncate(24 * time.Hour)

	query := `
		SELECT user_id, action_date, timestamp, id, action, entity_type, entity_id, ip_address, user_agent, changes, metadata
		FROM audit_logs
		WHERE user_id = ? AND action_date = ?
	`

	q := r.db.Session.Query(query, userID, actionDate).WithContext(ctx).PageSize(limit)

	// Set cursor if provided
	if cursor != "" {
		pageState, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil {
			q = q.PageState(pageState)
		}
	}

	iter := q.Iter()
	logs := make([]models.AuditLog, 0, limit)

	scanner := iter.Scanner()
	for scanner.Next() {
		var log models.AuditLog
		var actionStr, entityTypeStr string
		err := scanner.Scan(
			&log.UserID, &log.ActionDate, &log.Timestamp, &log.ID,
			&actionStr, &entityTypeStr, &log.EntityID,
			&log.IPAddress, &log.UserAgent,
			&log.Changes, &log.Metadata,
		)
		if err != nil {
			continue
		}
		log.Action = models.AuditAction(actionStr)
		log.EntityType = models.AuditEntityType(entityTypeStr)
		logs = append(logs, log)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Get next cursor
	var nextCursor *string
	pageState := iter.PageState()
	if len(pageState) > 0 {
		encoded := base64.StdEncoding.EncodeToString(pageState)
		nextCursor = &encoded
	}

	return &models.AuditLogsResult{
		Data: logs,
		Pagination: models.CPagination{
			NextCursor: nextCursor,
			HasMore:    nextCursor != nil,
		},
	}, nil
}

func (r *auditRepository) GetByEntity(ctx context.Context, entityType string, entityID uuid.UUID, limit int, cursor string) (*models.AuditLogsResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `
		SELECT entity_type, entity_id, timestamp, id, user_id, action
		FROM audit_logs_by_entity
		WHERE entity_type = ? AND entity_id = ?
	`

	q := r.db.Session.Query(query, entityType, entityID).WithContext(ctx).PageSize(limit)

	// Set cursor if provided
	if cursor != "" {
		pageState, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil {
			q = q.PageState(pageState)
		}
	}

	iter := q.Iter()
	logs := make([]models.AuditLog, 0, limit)

	scanner := iter.Scanner()
	for scanner.Next() {
		var log models.AuditLog
		var actionStr, entityTypeStr string
		err := scanner.Scan(
			&entityTypeStr, &log.EntityID,
			&log.Timestamp, &log.ID,
			&log.UserID, &actionStr,
		)
		if err != nil {
			continue
		}
		log.Action = models.AuditAction(actionStr)
		log.EntityType = models.AuditEntityType(entityTypeStr)
		logs = append(logs, log)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Get next cursor
	var nextCursor *string
	pageState := iter.PageState()
	if len(pageState) > 0 {
		encoded := base64.StdEncoding.EncodeToString(pageState)
		nextCursor = &encoded
	}

	return &models.AuditLogsResult{
		Data: logs,
		Pagination: models.CPagination{
			NextCursor: nextCursor,
			HasMore:    nextCursor != nil,
		},
	}, nil
}

func (r *auditRepository) Search(ctx context.Context, params *models.AuditLogSearch) (*models.AuditLogsResult, error) {
	// If entity search, use entity table
	if params.EntityType != nil && params.EntityID != nil {
		return r.GetByEntity(ctx, string(*params.EntityType), *params.EntityID, params.Limit, params.Cursor)
	}

	// If user search, iterate through dates
	if params.UserID != nil {
		// Default to today if no date range
		date := time.Now()
		if params.Since != nil {
			date = *params.Since
		}
		return r.GetByUser(ctx, *params.UserID, date, params.Limit, params.Cursor)
	}

	return &models.AuditLogsResult{
		Data: make([]models.AuditLog, 0),
		Pagination: models.CPagination{
			HasMore: false,
		},
	}, errx.ErrNotEnough
}
