package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type CRMRepository interface {
	// Notes
	CreateNote(ctx context.Context, orgID, contactID, userID uuid.UUID, content string) (*models.ContactNote, error)
	GetNote(ctx context.Context, orgID, noteID uuid.UUID) (*models.ContactNote, error)
	ListNotes(ctx context.Context, orgID, contactID uuid.UUID, limit int, cursor *uuid.UUID) (*models.ContactNotesResult, error)
	UpdateNote(ctx context.Context, orgID, noteID uuid.UUID, content string) (*models.ContactNote, error)
	DeleteNote(ctx context.Context, orgID, noteID uuid.UUID) error

	// Activities
	RecordActivity(ctx context.Context, orgID, contactID uuid.UUID, userID *uuid.UUID, actType models.ActivityType, metadata map[string]interface{}) error
	ListActivities(ctx context.Context, orgID, contactID uuid.UUID, limit int, cursor *uuid.UUID) (*models.ContactActivitiesResult, error)

	// Pipelines
	CreatePipeline(ctx context.Context, orgID uuid.UUID, data *models.CreatePipeline) (*models.Pipeline, error)
	GetPipeline(ctx context.Context, orgID, pipelineID uuid.UUID) (*models.Pipeline, error)
	ListPipelines(ctx context.Context, orgID uuid.UUID) ([]models.Pipeline, error)
	UpdatePipeline(ctx context.Context, orgID, pipelineID uuid.UUID, name string) (*models.Pipeline, error)
	DeletePipeline(ctx context.Context, orgID, pipelineID uuid.UUID) error

	// Pipeline Stages
	CreateStage(ctx context.Context, pipelineID uuid.UUID, data *models.CreatePipelineStage) (*models.PipelineStage, error)
	UpdateStage(ctx context.Context, stageID uuid.UUID, data *models.UpdatePipelineStage) (*models.PipelineStage, error)
	DeleteStage(ctx context.Context, stageID uuid.UUID) error

	// Deals
	CreateDeal(ctx context.Context, orgID uuid.UUID, data *models.CreateDeal) (*models.Deal, error)
	GetDeal(ctx context.Context, orgID, dealID uuid.UUID) (*models.Deal, error)
	ListDeals(ctx context.Context, orgID uuid.UUID, pipelineID, stageID *uuid.UUID, status *string, limit int, cursor *uuid.UUID) (*models.DealsResult, error)
	UpdateDeal(ctx context.Context, orgID, dealID uuid.UUID, data *models.UpdateDeal) (*models.Deal, error)
	DeleteDeal(ctx context.Context, orgID, dealID uuid.UUID) error
	GetDealsByContact(ctx context.Context, contactID uuid.UUID) ([]models.Deal, error)

	// CRM Tasks
	CreateCRMTask(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateCRMTask) (*models.CRMTask, error)
	GetCRMTask(ctx context.Context, orgID, taskID uuid.UUID) (*models.CRMTask, error)
	ListCRMTasks(ctx context.Context, orgID uuid.UUID, contactID, dealID, assignedTo *uuid.UUID, status *string, limit int, cursor *uuid.UUID) (*models.CRMTasksResult, error)
	UpdateCRMTask(ctx context.Context, orgID, taskID uuid.UUID, data *models.UpdateCRMTask) (*models.CRMTask, error)
	DeleteCRMTask(ctx context.Context, orgID, taskID uuid.UUID) error
}

type crmRepository struct {
	db *pgxpool.Pool
}

func NewCRMRepository(db *pgxpool.Pool) CRMRepository {
	return &crmRepository{db: db}
}

// =====================
// Notes
// =====================

func (r *crmRepository) CreateNote(ctx context.Context, orgID, contactID, userID uuid.UUID, content string) (*models.ContactNote, error) {
	query := `
		INSERT INTO contact_notes (contact_id, organization_id, user_id, content)
		VALUES ($1, $2, $3, $4)
		RETURNING id, contact_id, organization_id, user_id, content, created_at, updated_at
	`
	var note models.ContactNote
	err := r.db.QueryRow(ctx, query, contactID, orgID, userID, content).Scan(
		&note.ID, &note.ContactID, &note.OrganizationID, &note.UserID,
		&note.Content, &note.CreatedAt, &note.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &note, nil
}

func (r *crmRepository) GetNote(ctx context.Context, orgID, noteID uuid.UUID) (*models.ContactNote, error) {
	query := `
		SELECT id, contact_id, organization_id, user_id, content, created_at, updated_at
		FROM contact_notes
		WHERE organization_id = $1 AND id = $2
	`
	var note models.ContactNote
	err := r.db.QueryRow(ctx, query, orgID, noteID).Scan(
		&note.ID, &note.ContactID, &note.OrganizationID, &note.UserID,
		&note.Content, &note.CreatedAt, &note.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		return nil, err
	}
	return &note, nil
}

func (r *crmRepository) ListNotes(ctx context.Context, orgID, contactID uuid.UUID, limit int, cursor *uuid.UUID) (*models.ContactNotesResult, error) {
	query := `
		SELECT cn.id, cn.contact_id, cn.organization_id, cn.user_id, cn.content, cn.created_at, cn.updated_at
		FROM contact_notes cn
		JOIN contacts c ON c.id = cn.contact_id
		WHERE cn.contact_id = $1
		  AND cn.organization_id = $4
		  AND ($2::uuid IS NULL OR (cn.created_at, cn.id) < (
			SELECT created_at, id FROM contact_notes WHERE id = $2
		  ))
		ORDER BY cn.created_at DESC, cn.id DESC
		LIMIT $3
	`
	rows, err := r.db.Query(ctx, query, contactID, cursor, limit+1, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := make([]models.ContactNote, 0, limit)
	for rows.Next() {
		var note models.ContactNote
		if err := rows.Scan(
			&note.ID, &note.ContactID, &note.OrganizationID, &note.UserID,
			&note.Content, &note.CreatedAt, &note.UpdatedAt,
		); err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}

	var nextCursor *uuid.UUID
	hasMore := false
	if len(notes) > limit {
		hasMore = true
		nextCursor = &notes[limit].ID
		notes = notes[:limit]
	}

	return &models.ContactNotesResult{
		Data:       notes,
		Pagination: models.Pagination{NextCursor: nextCursor, HasMore: hasMore},
	}, nil
}

func (r *crmRepository) UpdateNote(ctx context.Context, orgID, noteID uuid.UUID, content string) (*models.ContactNote, error) {
	query := `
		UPDATE contact_notes SET content = $3, updated_at = NOW()
		WHERE organization_id = $1 AND id = $2
		RETURNING id, contact_id, organization_id, user_id, content, created_at, updated_at
	`
	var note models.ContactNote
	err := r.db.QueryRow(ctx, query, orgID, noteID, content).Scan(
		&note.ID, &note.ContactID, &note.OrganizationID, &note.UserID,
		&note.Content, &note.CreatedAt, &note.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		return nil, err
	}
	return &note, nil
}

func (r *crmRepository) DeleteNote(ctx context.Context, orgID, noteID uuid.UUID) error {
	query := `DELETE FROM contact_notes WHERE organization_id = $1 AND id = $2`
	cmd, err := r.db.Exec(ctx, query, orgID, noteID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}
	return nil
}

// =====================
// Activities
// =====================

func (r *crmRepository) RecordActivity(ctx context.Context, orgID, contactID uuid.UUID, userID *uuid.UUID, actType models.ActivityType, metadata map[string]interface{}) error {
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	query := `
		INSERT INTO contact_activities (contact_id, organization_id, user_id, activity_type, metadata)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := r.db.Exec(ctx, query, contactID, orgID, userID, actType, metadata)
	return err
}

func (r *crmRepository) ListActivities(ctx context.Context, orgID, contactID uuid.UUID, limit int, cursor *uuid.UUID) (*models.ContactActivitiesResult, error) {
	query := `
		SELECT id, contact_id, organization_id, user_id, activity_type, metadata, created_at
		FROM contact_activities
		WHERE contact_id = $1
		  AND organization_id = $4
		  AND ($2::uuid IS NULL OR (created_at, id) < (
			SELECT created_at, id FROM contact_activities WHERE id = $2
		  ))
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`
	rows, err := r.db.Query(ctx, query, contactID, cursor, limit+1, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	activities := make([]models.ContactActivity, 0, limit)
	for rows.Next() {
		var a models.ContactActivity
		if err := rows.Scan(
			&a.ID, &a.ContactID, &a.OrganizationID, &a.UserID,
			&a.ActivityType, &a.Metadata, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		activities = append(activities, a)
	}

	var nextCursor *uuid.UUID
	hasMore := false
	if len(activities) > limit {
		hasMore = true
		nextCursor = &activities[limit].ID
		activities = activities[:limit]
	}

	return &models.ContactActivitiesResult{
		Data:       activities,
		Pagination: models.Pagination{NextCursor: nextCursor, HasMore: hasMore},
	}, nil
}

// =====================
// Pipelines
// =====================

func (r *crmRepository) CreatePipeline(ctx context.Context, orgID uuid.UUID, data *models.CreatePipeline) (*models.Pipeline, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Get next position
	var maxPos int
	_ = tx.QueryRow(ctx, `SELECT COALESCE(MAX(position), -1) FROM pipelines WHERE organization_id = $1`, orgID).Scan(&maxPos)

	query := `
		INSERT INTO pipelines (organization_id, name, position)
		VALUES ($1, $2, $3)
		RETURNING id, organization_id, name, position, created_at, updated_at
	`
	var pipeline models.Pipeline
	err = tx.QueryRow(ctx, query, orgID, data.Name, maxPos+1).Scan(
		&pipeline.ID, &pipeline.OrganizationID, &pipeline.Name,
		&pipeline.Position, &pipeline.CreatedAt, &pipeline.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Create stages if provided
	pipeline.Stages = make([]models.PipelineStage, 0, len(data.Stages))
	for i, stageData := range data.Stages {
		stageQuery := `
			INSERT INTO pipeline_stages (pipeline_id, name, color, position)
			VALUES ($1, $2, $3, $4)
			RETURNING id, pipeline_id, name, color, position, created_at, updated_at
		`
		var stage models.PipelineStage
		err = tx.QueryRow(ctx, stageQuery, pipeline.ID, stageData.Name, stageData.Color, i).Scan(
			&stage.ID, &stage.PipelineID, &stage.Name, &stage.Color,
			&stage.Position, &stage.CreatedAt, &stage.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		pipeline.Stages = append(pipeline.Stages, stage)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &pipeline, nil
}

func (r *crmRepository) GetPipeline(ctx context.Context, orgID, pipelineID uuid.UUID) (*models.Pipeline, error) {
	query := `
		SELECT id, organization_id, name, position, created_at, updated_at
		FROM pipelines
		WHERE organization_id = $1 AND id = $2
	`
	var pipeline models.Pipeline
	err := r.db.QueryRow(ctx, query, orgID, pipelineID).Scan(
		&pipeline.ID, &pipeline.OrganizationID, &pipeline.Name,
		&pipeline.Position, &pipeline.CreatedAt, &pipeline.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		return nil, err
	}

	// Load stages
	stageQuery := `
		SELECT ps.id, ps.pipeline_id, ps.name, ps.color, ps.position, ps.created_at, ps.updated_at,
		       COUNT(d.id) AS deal_count
		FROM pipeline_stages ps
		LEFT JOIN deals d ON d.stage_id = ps.id AND d.status = 'open'
		WHERE ps.pipeline_id = $1
		GROUP BY ps.id
		ORDER BY ps.position ASC
	`
	rows, err := r.db.Query(ctx, stageQuery, pipelineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pipeline.Stages = make([]models.PipelineStage, 0)
	for rows.Next() {
		var stage models.PipelineStage
		if err := rows.Scan(
			&stage.ID, &stage.PipelineID, &stage.Name, &stage.Color,
			&stage.Position, &stage.CreatedAt, &stage.UpdatedAt, &stage.DealCount,
		); err != nil {
			return nil, err
		}
		pipeline.Stages = append(pipeline.Stages, stage)
	}

	return &pipeline, nil
}

func (r *crmRepository) ListPipelines(ctx context.Context, orgID uuid.UUID) ([]models.Pipeline, error) {
	query := `
		SELECT id, organization_id, name, position, created_at, updated_at
		FROM pipelines
		WHERE organization_id = $1
		ORDER BY position ASC
		LIMIT 100
	`
	rows, err := r.db.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pipelines []models.Pipeline
	for rows.Next() {
		var p models.Pipeline
		if err := rows.Scan(
			&p.ID, &p.OrganizationID, &p.Name,
			&p.Position, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		p.Stages = []models.PipelineStage{}
		pipelines = append(pipelines, p)
	}

	return pipelines, nil
}

func (r *crmRepository) UpdatePipeline(ctx context.Context, orgID, pipelineID uuid.UUID, name string) (*models.Pipeline, error) {
	query := `
		UPDATE pipelines SET name = $3, updated_at = NOW()
		WHERE organization_id = $1 AND id = $2
		RETURNING id, organization_id, name, position, created_at, updated_at
	`
	var pipeline models.Pipeline
	err := r.db.QueryRow(ctx, query, orgID, pipelineID, name).Scan(
		&pipeline.ID, &pipeline.OrganizationID, &pipeline.Name,
		&pipeline.Position, &pipeline.CreatedAt, &pipeline.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		return nil, err
	}
	pipeline.Stages = []models.PipelineStage{}
	return &pipeline, nil
}

func (r *crmRepository) DeletePipeline(ctx context.Context, orgID, pipelineID uuid.UUID) error {
	cmd, err := r.db.Exec(ctx, `DELETE FROM pipelines WHERE organization_id = $1 AND id = $2`, orgID, pipelineID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}
	return nil
}

// =====================
// Pipeline Stages
// =====================

func (r *crmRepository) CreateStage(ctx context.Context, pipelineID uuid.UUID, data *models.CreatePipelineStage) (*models.PipelineStage, error) {
	var maxPos int
	_ = r.db.QueryRow(ctx, `SELECT COALESCE(MAX(position), -1) FROM pipeline_stages WHERE pipeline_id = $1`, pipelineID).Scan(&maxPos)

	query := `
		INSERT INTO pipeline_stages (pipeline_id, name, color, position)
		VALUES ($1, $2, $3, $4)
		RETURNING id, pipeline_id, name, color, position, created_at, updated_at
	`
	var stage models.PipelineStage
	err := r.db.QueryRow(ctx, query, pipelineID, data.Name, data.Color, maxPos+1).Scan(
		&stage.ID, &stage.PipelineID, &stage.Name, &stage.Color,
		&stage.Position, &stage.CreatedAt, &stage.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &stage, nil
}

func (r *crmRepository) UpdateStage(ctx context.Context, stageID uuid.UUID, data *models.UpdatePipelineStage) (*models.PipelineStage, error) {
	setClauses := []string{}
	args := []any{stageID}
	argPos := 2

	if data.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argPos))
		args = append(args, *data.Name)
		argPos++
	}
	if data.Color != nil {
		setClauses = append(setClauses, fmt.Sprintf("color = $%d", argPos))
		args = append(args, *data.Color)
		argPos++
	}

	if len(setClauses) == 0 {
		return nil, errx.ErrNotEnough
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`
		UPDATE pipeline_stages SET %s
		WHERE id = $1
		RETURNING id, pipeline_id, name, color, position, created_at, updated_at
	`, strings.Join(setClauses, ", "))

	var stage models.PipelineStage
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&stage.ID, &stage.PipelineID, &stage.Name, &stage.Color,
		&stage.Position, &stage.CreatedAt, &stage.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		return nil, err
	}
	return &stage, nil
}

func (r *crmRepository) DeleteStage(ctx context.Context, stageID uuid.UUID) error {
	cmd, err := r.db.Exec(ctx, `DELETE FROM pipeline_stages WHERE id = $1`, stageID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}
	return nil
}

// =====================
// Deals
// =====================

func (r *crmRepository) CreateDeal(ctx context.Context, orgID uuid.UUID, data *models.CreateDeal) (*models.Deal, error) {
	currency := data.Currency
	if currency == "" {
		currency = "USD"
	}

	query := `
		INSERT INTO deals (organization_id, pipeline_id, stage_id, contact_id, name, value, currency, expected_close_date, assigned_to)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, organization_id, pipeline_id, stage_id, contact_id, name, value, currency, status,
		          expected_close_date, won_at, lost_at, lost_reason, assigned_to, created_at, updated_at
	`
	var deal models.Deal
	err := r.db.QueryRow(ctx, query,
		orgID, data.PipelineID, data.StageID, data.ContactID,
		data.Name, data.Value, currency, data.ExpectedCloseDate, data.AssignedTo,
	).Scan(
		&deal.ID, &deal.OrganizationID, &deal.PipelineID, &deal.StageID,
		&deal.ContactID, &deal.Name, &deal.Value, &deal.Currency, &deal.Status,
		&deal.ExpectedCloseDate, &deal.WonAt, &deal.LostAt, &deal.LostReason,
		&deal.AssignedTo, &deal.CreatedAt, &deal.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &deal, nil
}

func (r *crmRepository) GetDeal(ctx context.Context, orgID, dealID uuid.UUID) (*models.Deal, error) {
	query := `
		SELECT id, organization_id, pipeline_id, stage_id, contact_id, name, value, currency, status,
		       expected_close_date, won_at, lost_at, lost_reason, assigned_to, created_at, updated_at
		FROM deals
		WHERE organization_id = $1 AND id = $2
	`
	var deal models.Deal
	err := r.db.QueryRow(ctx, query, orgID, dealID).Scan(
		&deal.ID, &deal.OrganizationID, &deal.PipelineID, &deal.StageID,
		&deal.ContactID, &deal.Name, &deal.Value, &deal.Currency, &deal.Status,
		&deal.ExpectedCloseDate, &deal.WonAt, &deal.LostAt, &deal.LostReason,
		&deal.AssignedTo, &deal.CreatedAt, &deal.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		return nil, err
	}
	return &deal, nil
}

func (r *crmRepository) ListDeals(ctx context.Context, orgID uuid.UUID, pipelineID, stageID *uuid.UUID, status *string, limit int, cursor *uuid.UUID) (*models.DealsResult, error) {
	whereClauses := []string{"organization_id = $1"}
	args := []any{orgID}
	argPos := 2

	if pipelineID != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("pipeline_id = $%d", argPos))
		args = append(args, *pipelineID)
		argPos++
	}
	if stageID != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("stage_id = $%d", argPos))
		args = append(args, *stageID)
		argPos++
	}
	if status != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", argPos))
		args = append(args, *status)
		argPos++
	}

	if cursor != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("(created_at, id) < (SELECT created_at, id FROM deals WHERE id = $%d)", argPos))
		args = append(args, *cursor)
		argPos++
	}

	query := fmt.Sprintf(`
		SELECT id, organization_id, pipeline_id, stage_id, contact_id, name, value, currency, status,
		       expected_close_date, won_at, lost_at, lost_reason, assigned_to, created_at, updated_at
		FROM deals
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d
	`, strings.Join(whereClauses, " AND "), argPos)
	args = append(args, limit+1)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	deals := make([]models.Deal, 0, limit)
	for rows.Next() {
		var deal models.Deal
		if err := rows.Scan(
			&deal.ID, &deal.OrganizationID, &deal.PipelineID, &deal.StageID,
			&deal.ContactID, &deal.Name, &deal.Value, &deal.Currency, &deal.Status,
			&deal.ExpectedCloseDate, &deal.WonAt, &deal.LostAt, &deal.LostReason,
			&deal.AssignedTo, &deal.CreatedAt, &deal.UpdatedAt,
		); err != nil {
			return nil, err
		}
		deals = append(deals, deal)
	}

	var nextCursor *uuid.UUID
	hasMore := false
	if len(deals) > limit {
		hasMore = true
		nextCursor = &deals[limit].ID
		deals = deals[:limit]
	}

	return &models.DealsResult{
		Data:       deals,
		Pagination: models.Pagination{NextCursor: nextCursor, HasMore: hasMore},
	}, nil
}

func (r *crmRepository) UpdateDeal(ctx context.Context, orgID, dealID uuid.UUID, data *models.UpdateDeal) (*models.Deal, error) {
	setClauses := []string{}
	args := []any{orgID, dealID}
	argPos := 3

	if data.StageID != nil {
		setClauses = append(setClauses, fmt.Sprintf("stage_id = $%d", argPos))
		args = append(args, *data.StageID)
		argPos++
	}
	if data.ContactID != nil {
		setClauses = append(setClauses, fmt.Sprintf("contact_id = $%d", argPos))
		args = append(args, *data.ContactID)
		argPos++
	}
	if data.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argPos))
		args = append(args, *data.Name)
		argPos++
	}
	if data.Value != nil {
		setClauses = append(setClauses, fmt.Sprintf("value = $%d", argPos))
		args = append(args, *data.Value)
		argPos++
	}
	if data.Currency != nil {
		setClauses = append(setClauses, fmt.Sprintf("currency = $%d", argPos))
		args = append(args, *data.Currency)
		argPos++
	}
	if data.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argPos))
		args = append(args, *data.Status)
		argPos++

		if *data.Status == "won" {
			setClauses = append(setClauses, "won_at = NOW()")
		} else if *data.Status == "lost" {
			setClauses = append(setClauses, "lost_at = NOW()")
			if data.LostReason != nil {
				setClauses = append(setClauses, fmt.Sprintf("lost_reason = $%d", argPos))
				args = append(args, *data.LostReason)
				argPos++
			}
		}
	}
	if data.ExpectedCloseDate != nil {
		setClauses = append(setClauses, fmt.Sprintf("expected_close_date = $%d", argPos))
		args = append(args, *data.ExpectedCloseDate)
		argPos++
	}
	if data.AssignedTo != nil {
		setClauses = append(setClauses, fmt.Sprintf("assigned_to = $%d", argPos))
		args = append(args, *data.AssignedTo)
		argPos++
	}

	if len(setClauses) == 0 {
		return nil, errx.ErrNotEnough
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`
		UPDATE deals SET %s
		WHERE organization_id = $1 AND id = $2
		RETURNING id, organization_id, pipeline_id, stage_id, contact_id, name, value, currency, status,
		          expected_close_date, won_at, lost_at, lost_reason, assigned_to, created_at, updated_at
	`, strings.Join(setClauses, ", "))

	var deal models.Deal
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&deal.ID, &deal.OrganizationID, &deal.PipelineID, &deal.StageID,
		&deal.ContactID, &deal.Name, &deal.Value, &deal.Currency, &deal.Status,
		&deal.ExpectedCloseDate, &deal.WonAt, &deal.LostAt, &deal.LostReason,
		&deal.AssignedTo, &deal.CreatedAt, &deal.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		return nil, err
	}
	return &deal, nil
}

func (r *crmRepository) DeleteDeal(ctx context.Context, orgID, dealID uuid.UUID) error {
	cmd, err := r.db.Exec(ctx, `DELETE FROM deals WHERE organization_id = $1 AND id = $2`, orgID, dealID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}
	return nil
}

func (r *crmRepository) GetDealsByContact(ctx context.Context, contactID uuid.UUID) ([]models.Deal, error) {
	query := `
		SELECT id, organization_id, pipeline_id, stage_id, contact_id, name, value, currency, status,
		       expected_close_date, won_at, lost_at, lost_reason, assigned_to, created_at, updated_at
		FROM deals
		WHERE contact_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.Query(ctx, query, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deals []models.Deal
	for rows.Next() {
		var deal models.Deal
		if err := rows.Scan(
			&deal.ID, &deal.OrganizationID, &deal.PipelineID, &deal.StageID,
			&deal.ContactID, &deal.Name, &deal.Value, &deal.Currency, &deal.Status,
			&deal.ExpectedCloseDate, &deal.WonAt, &deal.LostAt, &deal.LostReason,
			&deal.AssignedTo, &deal.CreatedAt, &deal.UpdatedAt,
		); err != nil {
			return nil, err
		}
		deals = append(deals, deal)
	}
	return deals, nil
}

// =====================
// CRM Tasks
// =====================

func (r *crmRepository) CreateCRMTask(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateCRMTask) (*models.CRMTask, error) {
	priority := data.Priority
	if priority == "" {
		priority = "medium"
	}

	query := `
		INSERT INTO crm_tasks (organization_id, contact_id, deal_id, assigned_to, created_by, title, description, due_date, priority)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, organization_id, contact_id, deal_id, assigned_to, created_by, title, description,
		          due_date, priority, status, completed_at, created_at, updated_at
	`
	var task models.CRMTask
	err := r.db.QueryRow(ctx, query,
		orgID, data.ContactID, data.DealID, data.AssignedTo, userID,
		data.Title, data.Description, data.DueDate, priority,
	).Scan(
		&task.ID, &task.OrganizationID, &task.ContactID, &task.DealID,
		&task.AssignedTo, &task.CreatedBy, &task.Title, &task.Description,
		&task.DueDate, &task.Priority, &task.Status, &task.CompletedAt,
		&task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *crmRepository) GetCRMTask(ctx context.Context, orgID, taskID uuid.UUID) (*models.CRMTask, error) {
	query := `
		SELECT id, organization_id, contact_id, deal_id, assigned_to, created_by, title, description,
		       due_date, priority, status, completed_at, created_at, updated_at
		FROM crm_tasks
		WHERE organization_id = $1 AND id = $2
	`
	var task models.CRMTask
	err := r.db.QueryRow(ctx, query, orgID, taskID).Scan(
		&task.ID, &task.OrganizationID, &task.ContactID, &task.DealID,
		&task.AssignedTo, &task.CreatedBy, &task.Title, &task.Description,
		&task.DueDate, &task.Priority, &task.Status, &task.CompletedAt,
		&task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		return nil, err
	}
	return &task, nil
}

func (r *crmRepository) ListCRMTasks(ctx context.Context, orgID uuid.UUID, contactID, dealID, assignedTo *uuid.UUID, status *string, limit int, cursor *uuid.UUID) (*models.CRMTasksResult, error) {
	whereClauses := []string{"organization_id = $1"}
	args := []any{orgID}
	argPos := 2

	if contactID != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("contact_id = $%d", argPos))
		args = append(args, *contactID)
		argPos++
	}
	if dealID != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("deal_id = $%d", argPos))
		args = append(args, *dealID)
		argPos++
	}
	if assignedTo != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("assigned_to = $%d", argPos))
		args = append(args, *assignedTo)
		argPos++
	}
	if status != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", argPos))
		args = append(args, *status)
		argPos++
	}

	if cursor != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("(created_at, id) < (SELECT created_at, id FROM crm_tasks WHERE id = $%d)", argPos))
		args = append(args, *cursor)
		argPos++
	}

	query := fmt.Sprintf(`
		SELECT id, organization_id, contact_id, deal_id, assigned_to, created_by, title, description,
		       due_date, priority, status, completed_at, created_at, updated_at
		FROM crm_tasks
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d
	`, strings.Join(whereClauses, " AND "), argPos)
	args = append(args, limit+1)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]models.CRMTask, 0, limit)
	for rows.Next() {
		var task models.CRMTask
		if err := rows.Scan(
			&task.ID, &task.OrganizationID, &task.ContactID, &task.DealID,
			&task.AssignedTo, &task.CreatedBy, &task.Title, &task.Description,
			&task.DueDate, &task.Priority, &task.Status, &task.CompletedAt,
			&task.CreatedAt, &task.UpdatedAt,
		); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	var nextCursor *uuid.UUID
	hasMore := false
	if len(tasks) > limit {
		hasMore = true
		nextCursor = &tasks[limit].ID
		tasks = tasks[:limit]
	}

	return &models.CRMTasksResult{
		Data:       tasks,
		Pagination: models.Pagination{NextCursor: nextCursor, HasMore: hasMore},
	}, nil
}

func (r *crmRepository) UpdateCRMTask(ctx context.Context, orgID, taskID uuid.UUID, data *models.UpdateCRMTask) (*models.CRMTask, error) {
	setClauses := []string{}
	args := []any{orgID, taskID}
	argPos := 3

	if data.AssignedTo != nil {
		setClauses = append(setClauses, fmt.Sprintf("assigned_to = $%d", argPos))
		args = append(args, *data.AssignedTo)
		argPos++
	}
	if data.Title != nil {
		setClauses = append(setClauses, fmt.Sprintf("title = $%d", argPos))
		args = append(args, *data.Title)
		argPos++
	}
	if data.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argPos))
		args = append(args, *data.Description)
		argPos++
	}
	if data.DueDate != nil {
		setClauses = append(setClauses, fmt.Sprintf("due_date = $%d", argPos))
		args = append(args, *data.DueDate)
		argPos++
	}
	if data.Priority != nil {
		setClauses = append(setClauses, fmt.Sprintf("priority = $%d", argPos))
		args = append(args, *data.Priority)
		argPos++
	}
	if data.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argPos))
		args = append(args, *data.Status)
		argPos++

		if *data.Status == "completed" {
			setClauses = append(setClauses, "completed_at = NOW()")
		}
	}

	if len(setClauses) == 0 {
		return nil, errx.ErrNotEnough
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`
		UPDATE crm_tasks SET %s
		WHERE organization_id = $1 AND id = $2
		RETURNING id, organization_id, contact_id, deal_id, assigned_to, created_by, title, description,
		          due_date, priority, status, completed_at, created_at, updated_at
	`, strings.Join(setClauses, ", "))

	var task models.CRMTask
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&task.ID, &task.OrganizationID, &task.ContactID, &task.DealID,
		&task.AssignedTo, &task.CreatedBy, &task.Title, &task.Description,
		&task.DueDate, &task.Priority, &task.Status, &task.CompletedAt,
		&task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		return nil, err
	}
	return &task, nil
}

func (r *crmRepository) DeleteCRMTask(ctx context.Context, orgID, taskID uuid.UUID) error {
	cmd, err := r.db.Exec(ctx, `DELETE FROM crm_tasks WHERE organization_id = $1 AND id = $2`, orgID, taskID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}
	return nil
}
