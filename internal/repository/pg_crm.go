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
	"github.com/warmbly/warmbly/internal/utils/paging"
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
	CreateStage(ctx context.Context, orgID, pipelineID uuid.UUID, data *models.CreatePipelineStage) (*models.PipelineStage, error)
	UpdateStage(ctx context.Context, orgID, stageID uuid.UUID, data *models.UpdatePipelineStage) (*models.PipelineStage, error)
	DeleteStage(ctx context.Context, orgID, stageID uuid.UUID) error

	// Deals
	CreateDeal(ctx context.Context, orgID uuid.UUID, data *models.CreateDeal) (*models.Deal, error)
	GetDeal(ctx context.Context, orgID, dealID uuid.UUID) (*models.Deal, error)
	ListDeals(ctx context.Context, orgID uuid.UUID, pipelineID, stageID *uuid.UUID, status *string, limit int, cursor *uuid.UUID) (*models.DealsResult, error)
	SearchDeals(ctx context.Context, orgID uuid.UUID, filters models.SearchDeals, limit, offset int) (*models.DealsSearchResult, error)
	DealsSummary(ctx context.Context, orgID uuid.UUID, filters models.SearchDeals) (*models.DealsSummary, error)
	UpdateDeal(ctx context.Context, orgID, dealID uuid.UUID, data *models.UpdateDeal) (*models.Deal, error)
	DeleteDeal(ctx context.Context, orgID, dealID uuid.UUID) error
	GetDealsByContact(ctx context.Context, orgID, contactID uuid.UUID) ([]models.Deal, error)

	// CRM Tasks
	CreateCRMTask(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateCRMTask) (*models.CRMTask, error)
	GetCRMTask(ctx context.Context, orgID, taskID uuid.UUID) (*models.CRMTask, error)
	ListCRMTasks(ctx context.Context, orgID uuid.UUID, contactID, dealID, assignedTo *uuid.UUID, status *string, limit int, cursor *uuid.UUID) (*models.CRMTasksResult, error)
	SearchCRMTasks(ctx context.Context, orgID uuid.UUID, filters models.SearchTasks, limit, offset int) (*models.TasksSearchResult, error)
	TasksSummary(ctx context.Context, orgID uuid.UUID, filters models.SearchTasks) (*models.TasksSummary, error)
	UpdateCRMTask(ctx context.Context, orgID, taskID uuid.UUID, data *models.UpdateCRMTask) (*models.CRMTask, error)
	DeleteCRMTask(ctx context.Context, orgID, taskID uuid.UUID) error

	// CRM Task Types (user-managed)
	ListTaskTypes(ctx context.Context, orgID uuid.UUID) ([]models.CRMTaskType, error)
	CreateTaskType(ctx context.Context, orgID uuid.UUID, data *models.CreateCRMTaskType) (*models.CRMTaskType, error)
	UpdateTaskType(ctx context.Context, orgID, typeID uuid.UUID, data *models.UpdateCRMTaskType) (*models.CRMTaskType, error)
	DeleteTaskType(ctx context.Context, orgID, typeID uuid.UUID) error
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

	var nextCursor *string
	hasMore := false
	if len(notes) > limit {
		hasMore = true
		nextCursor = paging.EncodeUUID(notes[limit].ID)
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

	var nextCursor *string
	hasMore := false
	if len(activities) > limit {
		hasMore = true
		nextCursor = paging.EncodeUUID(activities[limit].ID)
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

	pipelines := []models.Pipeline{}
	indexByID := make(map[uuid.UUID]int)
	for rows.Next() {
		var p models.Pipeline
		if err := rows.Scan(
			&p.ID, &p.OrganizationID, &p.Name,
			&p.Position, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		p.Stages = []models.PipelineStage{}
		indexByID[p.ID] = len(pipelines)
		pipelines = append(pipelines, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(pipelines) == 0 {
		return pipelines, nil
	}

	// Hydrate stages (with open-deal counts) for every pipeline in one pass and
	// attach each to its owner, so the list view renders stages just like
	// GetPipeline. Without this the Pipelines tab and the deals board always saw
	// empty stage arrays — a newly added stage "succeeded" but never appeared.
	pipelineIDs := make([]uuid.UUID, 0, len(pipelines))
	for _, p := range pipelines {
		pipelineIDs = append(pipelineIDs, p.ID)
	}

	stageQuery := `
		SELECT ps.id, ps.pipeline_id, ps.name, ps.color, ps.position, ps.created_at, ps.updated_at,
		       COUNT(d.id) AS deal_count
		FROM pipeline_stages ps
		LEFT JOIN deals d ON d.stage_id = ps.id AND d.status = 'open'
		WHERE ps.pipeline_id = ANY($1)
		GROUP BY ps.id
		ORDER BY ps.position ASC
	`
	stageRows, err := r.db.Query(ctx, stageQuery, pipelineIDs)
	if err != nil {
		return nil, err
	}
	defer stageRows.Close()

	for stageRows.Next() {
		var stage models.PipelineStage
		if err := stageRows.Scan(
			&stage.ID, &stage.PipelineID, &stage.Name, &stage.Color,
			&stage.Position, &stage.CreatedAt, &stage.UpdatedAt, &stage.DealCount,
		); err != nil {
			return nil, err
		}
		if idx, ok := indexByID[stage.PipelineID]; ok {
			pipelines[idx].Stages = append(pipelines[idx].Stages, stage)
		}
	}
	if err := stageRows.Err(); err != nil {
		return nil, err
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

func (r *crmRepository) CreateStage(ctx context.Context, orgID, pipelineID uuid.UUID, data *models.CreatePipelineStage) (*models.PipelineStage, error) {
	var maxPos int
	_ = r.db.QueryRow(ctx, `SELECT COALESCE(MAX(position), -1) FROM pipeline_stages WHERE pipeline_id = $1`, pipelineID).Scan(&maxPos)

	// pipeline_stages has no organization_id; scope through the owning pipeline so
	// a cross-org pipeline id inserts nothing (and maps to not-found below).
	query := `
		INSERT INTO pipeline_stages (pipeline_id, name, color, position)
		SELECT $1, $2, $3, $4
		WHERE EXISTS (SELECT 1 FROM pipelines WHERE id = $1 AND organization_id = $5)
		RETURNING id, pipeline_id, name, color, position, created_at, updated_at
	`
	var stage models.PipelineStage
	err := r.db.QueryRow(ctx, query, pipelineID, data.Name, data.Color, maxPos+1, orgID).Scan(
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

func (r *crmRepository) UpdateStage(ctx context.Context, orgID, stageID uuid.UUID, data *models.UpdatePipelineStage) (*models.PipelineStage, error) {
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

	// Scope through the owning pipeline (no organization_id on pipeline_stages) so
	// a cross-org stage matches no row and returns not-found.
	orgPos := argPos
	args = append(args, orgID)
	query := fmt.Sprintf(`
		UPDATE pipeline_stages SET %s
		WHERE id = $1 AND pipeline_id IN (SELECT id FROM pipelines WHERE organization_id = $%d)
		RETURNING id, pipeline_id, name, color, position, created_at, updated_at
	`, strings.Join(setClauses, ", "), orgPos)

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

func (r *crmRepository) DeleteStage(ctx context.Context, orgID, stageID uuid.UUID) error {
	cmd, err := r.db.Exec(ctx, `DELETE FROM pipeline_stages WHERE id = $1 AND pipeline_id IN (SELECT id FROM pipelines WHERE organization_id = $2)`, stageID, orgID)
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
		INSERT INTO deals (organization_id, pipeline_id, stage_id, contact_id, name, value, currency, expected_close_date, assigned_to, campaign_id, source_mailbox_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, organization_id, pipeline_id, stage_id, contact_id, name, value, currency, status,
		          expected_close_date, won_at, lost_at, lost_reason, assigned_to, campaign_id, source_mailbox_id, created_at, updated_at
	`
	var deal models.Deal
	err := r.db.QueryRow(ctx, query,
		orgID, data.PipelineID, data.StageID, data.ContactID,
		data.Name, data.Value, currency, data.ExpectedCloseDate, data.AssignedTo,
		data.CampaignID, data.SourceMailboxID,
	).Scan(
		&deal.ID, &deal.OrganizationID, &deal.PipelineID, &deal.StageID,
		&deal.ContactID, &deal.Name, &deal.Value, &deal.Currency, &deal.Status,
		&deal.ExpectedCloseDate, &deal.WonAt, &deal.LostAt, &deal.LostReason,
		&deal.AssignedTo, &deal.CampaignID, &deal.SourceMailboxID, &deal.CreatedAt, &deal.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &deal, nil
}

func (r *crmRepository) GetDeal(ctx context.Context, orgID, dealID uuid.UUID) (*models.Deal, error) {
	query := `
		SELECT id, organization_id, pipeline_id, stage_id, contact_id, name, value, currency, status,
		       expected_close_date, won_at, lost_at, lost_reason, assigned_to, campaign_id, source_mailbox_id, created_at, updated_at
		FROM deals
		WHERE organization_id = $1 AND id = $2
	`
	var deal models.Deal
	err := r.db.QueryRow(ctx, query, orgID, dealID).Scan(
		&deal.ID, &deal.OrganizationID, &deal.PipelineID, &deal.StageID,
		&deal.ContactID, &deal.Name, &deal.Value, &deal.Currency, &deal.Status,
		&deal.ExpectedCloseDate, &deal.WonAt, &deal.LostAt, &deal.LostReason,
		&deal.AssignedTo, &deal.CampaignID, &deal.SourceMailboxID, &deal.CreatedAt, &deal.UpdatedAt,
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
		       expected_close_date, won_at, lost_at, lost_reason, assigned_to, campaign_id, source_mailbox_id, created_at, updated_at
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
			&deal.AssignedTo, &deal.CampaignID, &deal.SourceMailboxID, &deal.CreatedAt, &deal.UpdatedAt,
		); err != nil {
			return nil, err
		}
		deals = append(deals, deal)
	}

	var nextCursor *string
	hasMore := false
	if len(deals) > limit {
		hasMore = true
		nextCursor = paging.EncodeUUID(deals[limit].ID)
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
		          expected_close_date, won_at, lost_at, lost_reason, assigned_to, campaign_id, source_mailbox_id, created_at, updated_at
	`, strings.Join(setClauses, ", "))

	var deal models.Deal
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&deal.ID, &deal.OrganizationID, &deal.PipelineID, &deal.StageID,
		&deal.ContactID, &deal.Name, &deal.Value, &deal.Currency, &deal.Status,
		&deal.ExpectedCloseDate, &deal.WonAt, &deal.LostAt, &deal.LostReason,
		&deal.AssignedTo, &deal.CampaignID, &deal.SourceMailboxID, &deal.CreatedAt, &deal.UpdatedAt,
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

func (r *crmRepository) GetDealsByContact(ctx context.Context, orgID, contactID uuid.UUID) ([]models.Deal, error) {
	query := `
		SELECT id, organization_id, pipeline_id, stage_id, contact_id, name, value, currency, status,
		       expected_close_date, won_at, lost_at, lost_reason, assigned_to, campaign_id, source_mailbox_id, created_at, updated_at
		FROM deals
		WHERE organization_id = $1 AND contact_id = $2
		ORDER BY created_at DESC
	`
	rows, err := r.db.Query(ctx, query, orgID, contactID)
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
			&deal.AssignedTo, &deal.CampaignID, &deal.SourceMailboxID, &deal.CreatedAt, &deal.UpdatedAt,
		); err != nil {
			return nil, err
		}
		deals = append(deals, deal)
	}
	return deals, nil
}

// =====================
// Deal search + summary
// =====================

// dealSearchWhere builds the shared WHERE clause for SearchDeals / DealsSummary
// from a filter body. It returns the clause list and the bound args, starting
// at $1 = orgID. Both the paginated search and the aggregate summary call this
// so a header total always reflects the exact same filter as the rows below it.
func dealSearchWhere(orgID uuid.UUID, f models.SearchDeals) ([]string, []any) {
	clauses := []string{"d.organization_id = $1"}
	args := []any{orgID}
	pos := 2

	if f.Query != "" {
		clauses = append(clauses, fmt.Sprintf("d.name ILIKE $%d", pos))
		args = append(args, "%"+f.Query+"%")
		pos++
	}
	appendIn := func(col string, vals []string) {
		if len(vals) == 0 {
			return
		}
		ph := make([]string, len(vals))
		for i, v := range vals {
			ph[i] = fmt.Sprintf("$%d", pos)
			args = append(args, v)
			pos++
		}
		clauses = append(clauses, fmt.Sprintf("d.%s IN (%s)", col, strings.Join(ph, ",")))
	}
	appendIn("status", f.Statuses)
	appendIn("pipeline_id", f.PipelineIDs)
	appendIn("stage_id", f.StageIDs)
	appendIn("assigned_to", f.AssignedTo)
	appendIn("campaign_id", f.CampaignIDs)

	if f.MinValue != nil {
		clauses = append(clauses, fmt.Sprintf("d.value >= $%d", pos))
		args = append(args, *f.MinValue)
		pos++
	}
	if f.MaxValue != nil {
		clauses = append(clauses, fmt.Sprintf("d.value <= $%d", pos))
		args = append(args, *f.MaxValue)
		pos++
	}
	if f.CloseAfter != nil {
		clauses = append(clauses, fmt.Sprintf("d.expected_close_date >= $%d", pos))
		args = append(args, *f.CloseAfter)
		pos++
	}
	if f.CloseBefore != nil {
		clauses = append(clauses, fmt.Sprintf("d.expected_close_date <= $%d", pos))
		args = append(args, *f.CloseBefore)
		pos++
	}
	if f.CreatedAfter != nil {
		clauses = append(clauses, fmt.Sprintf("d.created_at >= $%d", pos))
		args = append(args, *f.CreatedAfter)
		pos++
	}
	if f.CreatedBefore != nil {
		clauses = append(clauses, fmt.Sprintf("d.created_at <= $%d", pos))
		args = append(args, *f.CreatedBefore)
		pos++
	}

	return clauses, args
}

// dealSortColumn whitelists the sortable columns so SortBy can never inject SQL.
func dealSortColumn(sortBy string) string {
	switch sortBy {
	case "value":
		return "d.value"
	case "expected_close_date":
		return "d.expected_close_date"
	case "name":
		return "d.name"
	case "updated_at":
		return "d.updated_at"
	default:
		return "d.created_at"
	}
}

func (r *crmRepository) SearchDeals(ctx context.Context, orgID uuid.UUID, filters models.SearchDeals, limit, offset int) (*models.DealsSearchResult, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	clauses, args := dealSearchWhere(orgID, filters)
	whereSQL := strings.Join(clauses, " AND ")

	// Exact total over the same filter, so the UI can show "N of M" honestly.
	var total int64
	if err := r.db.QueryRow(ctx, "SELECT COUNT(*) FROM deals d WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, err
	}

	sortCol := dealSortColumn(filters.SortBy)
	dir := "DESC"
	if filters.Reverse {
		dir = "ASC"
	}

	limitPos := len(args) + 1
	offsetPos := len(args) + 2
	query := fmt.Sprintf(`
		SELECT d.id, d.organization_id, d.pipeline_id, d.stage_id, d.contact_id, d.name, d.value, d.currency, d.status,
		       d.expected_close_date, d.won_at, d.lost_at, d.lost_reason, d.assigned_to, d.campaign_id, d.source_mailbox_id,
		       d.created_at, d.updated_at,
		       co.id, co.first_name, co.last_name, co.email, co.company,
		       ps.name, ps.color, ps.position,
		       cam.name
		FROM deals d
		LEFT JOIN contacts co ON co.id = d.contact_id
		LEFT JOIN pipeline_stages ps ON ps.id = d.stage_id
		LEFT JOIN campaigns cam ON cam.id = d.campaign_id
		WHERE %s
		ORDER BY %s %s NULLS LAST, d.id DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, sortCol, dir, limitPos, offsetPos)
	args = append(args, limit, offset)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	deals := make([]models.Deal, 0, limit)
	for rows.Next() {
		var deal models.Deal
		var (
			coID                            *uuid.UUID
			coFirst, coLast, coEmail, coCmp *string
			stName, stColor                 *string
			stPos                           *int
			camName                         *string
		)
		if err := rows.Scan(
			&deal.ID, &deal.OrganizationID, &deal.PipelineID, &deal.StageID,
			&deal.ContactID, &deal.Name, &deal.Value, &deal.Currency, &deal.Status,
			&deal.ExpectedCloseDate, &deal.WonAt, &deal.LostAt, &deal.LostReason,
			&deal.AssignedTo, &deal.CampaignID, &deal.SourceMailboxID, &deal.CreatedAt, &deal.UpdatedAt,
			&coID, &coFirst, &coLast, &coEmail, &coCmp,
			&stName, &stColor, &stPos,
			&camName,
		); err != nil {
			return nil, err
		}
		if coID != nil {
			deal.Contact = &models.Contact{ID: *coID}
			if coFirst != nil {
				deal.Contact.FirstName = *coFirst
			}
			if coLast != nil {
				deal.Contact.LastName = *coLast
			}
			if coEmail != nil {
				deal.Contact.Email = *coEmail
			}
			if coCmp != nil {
				deal.Contact.Company = *coCmp
			}
		}
		if stName != nil {
			deal.Stage = &models.PipelineStage{ID: deal.StageID, Name: *stName}
			if stColor != nil {
				deal.Stage.Color = *stColor
			}
			if stPos != nil {
				deal.Stage.Position = *stPos
			}
		}
		deal.CampaignName = camName
		deals = append(deals, deal)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	hasMore := int64(offset+len(deals)) < total
	pag := models.Pagination{Total: &total, HasMore: hasMore}
	if hasMore {
		pag.NextCursor = paging.EncodeOffset(offset + limit)
	}

	return &models.DealsSearchResult{
		Data:       deals,
		Pagination: pag,
	}, nil
}

func (r *crmRepository) DealsSummary(ctx context.Context, orgID uuid.UUID, filters models.SearchDeals) (*models.DealsSummary, error) {
	clauses, args := dealSearchWhere(orgID, filters)
	whereSQL := strings.Join(clauses, " AND ")

	var s models.DealsSummary
	headline := fmt.Sprintf(`
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE d.status = 'open'),
			COALESCE(SUM(d.value) FILTER (WHERE d.status = 'open'), 0),
			COUNT(*) FILTER (WHERE d.status = 'won'),
			COALESCE(SUM(d.value) FILTER (WHERE d.status = 'won'), 0),
			COUNT(*) FILTER (WHERE d.status = 'lost'),
			COALESCE(SUM(d.value) FILTER (WHERE d.status = 'lost'), 0),
			COUNT(DISTINCT d.currency),
			COALESCE(MAX(d.currency), 'USD')
		FROM deals d
		WHERE %s
	`, whereSQL)
	var distinctCurrencies int64
	if err := r.db.QueryRow(ctx, headline, args...).Scan(
		&s.Total, &s.OpenCount, &s.OpenValue, &s.WonCount, &s.WonValue,
		&s.LostCount, &s.LostValue, &distinctCurrencies, &s.Currency,
	); err != nil {
		return nil, err
	}
	// More than one currency in the matched set means a single blended SUM is
	// not meaningful; flag it so the UI can warn instead of lying with a total.
	s.MixedCurrency = distinctCurrencies > 1

	// Per-stage rollup for accurate board column headers (count + open value).
	stageQuery := fmt.Sprintf(`
		SELECT d.stage_id, COUNT(*), COALESCE(SUM(d.value) FILTER (WHERE d.status = 'open'), 0)
		FROM deals d
		WHERE %s
		GROUP BY d.stage_id
	`, whereSQL)
	rows, err := r.db.Query(ctx, stageQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	s.Stages = []models.DealStageSummary{}
	for rows.Next() {
		var st models.DealStageSummary
		if err := rows.Scan(&st.StageID, &st.Count, &st.Value); err != nil {
			return nil, err
		}
		s.Stages = append(s.Stages, st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &s, nil
}

// =====================
// CRM Tasks
// =====================

func (r *crmRepository) CreateCRMTask(ctx context.Context, orgID, userID uuid.UUID, data *models.CreateCRMTask) (*models.CRMTask, error) {
	priority := data.Priority
	if priority == "" {
		priority = "medium"
	}
	// Empty type = untyped; types are user-managed, so no enum coercion here.
	taskType := data.Type

	query := `
		INSERT INTO crm_tasks (organization_id, contact_id, deal_id, assigned_to, assigned_team_id, created_by, title, description, due_date, priority, type)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, organization_id, contact_id, deal_id, assigned_to, assigned_team_id, created_by, title, description,
		          due_date, priority, type, status, completed_at, created_at, updated_at
	`
	var task models.CRMTask
	err := r.db.QueryRow(ctx, query,
		orgID, data.ContactID, data.DealID, data.AssignedTo, data.AssignedTeamID, userID,
		data.Title, data.Description, data.DueDate, priority, taskType,
	).Scan(
		&task.ID, &task.OrganizationID, &task.ContactID, &task.DealID,
		&task.AssignedTo, &task.AssignedTeamID, &task.CreatedBy, &task.Title, &task.Description,
		&task.DueDate, &task.Priority, &task.Type, &task.Status, &task.CompletedAt,
		&task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *crmRepository) GetCRMTask(ctx context.Context, orgID, taskID uuid.UUID) (*models.CRMTask, error) {
	query := `
		SELECT id, organization_id, contact_id, deal_id, assigned_to, assigned_team_id, created_by, title, description,
		       due_date, priority, type, status, completed_at, created_at, updated_at
		FROM crm_tasks
		WHERE organization_id = $1 AND id = $2
	`
	var task models.CRMTask
	err := r.db.QueryRow(ctx, query, orgID, taskID).Scan(
		&task.ID, &task.OrganizationID, &task.ContactID, &task.DealID,
		&task.AssignedTo, &task.AssignedTeamID, &task.CreatedBy, &task.Title, &task.Description,
		&task.DueDate, &task.Priority, &task.Type, &task.Status, &task.CompletedAt,
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
		SELECT id, organization_id, contact_id, deal_id, assigned_to, assigned_team_id, created_by, title, description,
		       due_date, priority, type, status, completed_at, created_at, updated_at
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
			&task.AssignedTo, &task.AssignedTeamID, &task.CreatedBy, &task.Title, &task.Description,
			&task.DueDate, &task.Priority, &task.Type, &task.Status, &task.CompletedAt,
			&task.CreatedAt, &task.UpdatedAt,
		); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	var nextCursor *string
	hasMore := false
	if len(tasks) > limit {
		hasMore = true
		nextCursor = paging.EncodeUUID(tasks[limit].ID)
		tasks = tasks[:limit]
	}

	return &models.CRMTasksResult{
		Data:       tasks,
		Pagination: models.Pagination{NextCursor: nextCursor, HasMore: hasMore},
	}, nil
}

// =====================
// Task search + summary
// =====================

// taskSearchWhere builds the shared WHERE clause for SearchCRMTasks / TasksSummary
// from a filter body. It returns the clause list and the bound args, starting at
// $1 = orgID. Both the paginated search and the aggregate summary call this so a
// header total always reflects the exact same filter as the rows below it.
func taskSearchWhere(orgID uuid.UUID, f models.SearchTasks) ([]string, []any) {
	clauses := []string{"t.organization_id = $1"}
	args := []any{orgID}
	pos := 2

	if f.Query != "" {
		clauses = append(clauses, fmt.Sprintf("t.title ILIKE $%d", pos))
		args = append(args, "%"+f.Query+"%")
		pos++
	}
	appendIn := func(col string, vals []string) {
		if len(vals) == 0 {
			return
		}
		ph := make([]string, len(vals))
		for i, v := range vals {
			ph[i] = fmt.Sprintf("$%d", pos)
			args = append(args, v)
			pos++
		}
		clauses = append(clauses, fmt.Sprintf("t.%s IN (%s)", col, strings.Join(ph, ",")))
	}
	appendIn("status", f.Statuses)
	appendIn("priority", f.Priorities)
	appendIn("type", f.Types)
	appendIn("assigned_to", f.AssignedTo)

	// Team filter: a task matches when it is directly assigned to one of the
	// given teams, OR its individual assignee is a member of one of them. Bound
	// once as a uuid[] so the same predicate (and the same $N) is reused
	// identically by the search rows query and the summary aggregate.
	if len(f.TeamIDs) > 0 {
		clauses = append(clauses, fmt.Sprintf(
			"(t.assigned_team_id = ANY($%d) OR t.assigned_to IN (SELECT user_id FROM team_members WHERE team_id = ANY($%d)))",
			pos, pos,
		))
		args = append(args, f.TeamIDs)
		pos++
	}

	if f.ContactID != nil {
		clauses = append(clauses, fmt.Sprintf("t.contact_id = $%d", pos))
		args = append(args, *f.ContactID)
		pos++
	}
	if f.DealID != nil {
		clauses = append(clauses, fmt.Sprintf("t.deal_id = $%d", pos))
		args = append(args, *f.DealID)
		pos++
	}
	if f.DueAfter != nil {
		clauses = append(clauses, fmt.Sprintf("t.due_date >= $%d", pos))
		args = append(args, *f.DueAfter)
		pos++
	}
	if f.DueBefore != nil {
		clauses = append(clauses, fmt.Sprintf("t.due_date <= $%d", pos))
		args = append(args, *f.DueBefore)
		pos++
	}
	if f.Overdue {
		// Overdue = a deadline in the past that is still actionable. Completed and
		// cancelled tasks are never overdue, and tasks without a due_date can't be.
		clauses = append(clauses, "t.due_date IS NOT NULL AND t.due_date < now() AND t.status NOT IN ('completed', 'cancelled')")
	}

	return clauses, args
}

// taskSortColumn whitelists the sortable columns so SortBy can never inject SQL.
// priority sorts by its semantic weight (urgent first when DESC), not the enum
// string, so "priority" ordering matches user intent rather than alphabetical.
func taskSortColumn(sortBy string) string {
	switch sortBy {
	case "due_date":
		return "t.due_date"
	case "title":
		return "t.title"
	case "updated_at":
		return "t.updated_at"
	case "priority":
		return "CASE t.priority WHEN 'urgent' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END"
	default:
		return "t.created_at"
	}
}

func (r *crmRepository) SearchCRMTasks(ctx context.Context, orgID uuid.UUID, filters models.SearchTasks, limit, offset int) (*models.TasksSearchResult, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	clauses, args := taskSearchWhere(orgID, filters)
	whereSQL := strings.Join(clauses, " AND ")

	// Exact total over the same filter, so the UI can show "N of M" honestly.
	var total int64
	if err := r.db.QueryRow(ctx, "SELECT COUNT(*) FROM crm_tasks t WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, err
	}

	sortCol := taskSortColumn(filters.SortBy)
	dir := "DESC"
	if filters.Reverse {
		dir = "ASC"
	}

	limitPos := len(args) + 1
	offsetPos := len(args) + 2
	query := fmt.Sprintf(`
		SELECT t.id, t.organization_id, t.contact_id, t.deal_id, t.assigned_to, t.assigned_team_id, t.created_by, t.title, t.description,
		       t.due_date, t.priority, t.type, t.status, t.completed_at, t.created_at, t.updated_at
		FROM crm_tasks t
		WHERE %s
		ORDER BY %s %s NULLS LAST, t.id DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, sortCol, dir, limitPos, offsetPos)
	args = append(args, limit, offset)

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
			&task.AssignedTo, &task.AssignedTeamID, &task.CreatedBy, &task.Title, &task.Description,
			&task.DueDate, &task.Priority, &task.Type, &task.Status, &task.CompletedAt,
			&task.CreatedAt, &task.UpdatedAt,
		); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	hasMore := int64(offset+len(tasks)) < total
	pag := models.Pagination{Total: &total, HasMore: hasMore}
	if hasMore {
		pag.NextCursor = paging.EncodeOffset(offset + limit)
	}

	return &models.TasksSearchResult{
		Data:       tasks,
		Pagination: pag,
	}, nil
}

func (r *crmRepository) TasksSummary(ctx context.Context, orgID uuid.UUID, filters models.SearchTasks) (*models.TasksSummary, error) {
	clauses, args := taskSearchWhere(orgID, filters)
	whereSQL := strings.Join(clauses, " AND ")

	var s models.TasksSummary
	query := fmt.Sprintf(`
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE t.status = 'pending'),
			COUNT(*) FILTER (WHERE t.status = 'in_progress'),
			COUNT(*) FILTER (WHERE t.status = 'completed'),
			COUNT(*) FILTER (WHERE t.status = 'cancelled'),
			COUNT(*) FILTER (WHERE t.due_date IS NOT NULL AND t.due_date < now() AND t.status NOT IN ('completed', 'cancelled')),
			COUNT(*) FILTER (WHERE t.priority IN ('high', 'urgent'))
		FROM crm_tasks t
		WHERE %s
	`, whereSQL)
	if err := r.db.QueryRow(ctx, query, args...).Scan(
		&s.Total, &s.PendingCount, &s.InProgress, &s.CompletedCount,
		&s.CancelledCount, &s.OverdueCount, &s.HighPriority,
	); err != nil {
		return nil, err
	}

	return &s, nil
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
	if data.AssignedTeamID != nil {
		setClauses = append(setClauses, fmt.Sprintf("assigned_team_id = $%d", argPos))
		args = append(args, *data.AssignedTeamID)
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
	if data.Type != nil {
		setClauses = append(setClauses, fmt.Sprintf("type = $%d", argPos))
		args = append(args, *data.Type)
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
		RETURNING id, organization_id, contact_id, deal_id, assigned_to, assigned_team_id, created_by, title, description,
		          due_date, priority, type, status, completed_at, created_at, updated_at
	`, strings.Join(setClauses, ", "))

	var task models.CRMTask
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&task.ID, &task.OrganizationID, &task.ContactID, &task.DealID,
		&task.AssignedTo, &task.AssignedTeamID, &task.CreatedBy, &task.Title, &task.Description,
		&task.DueDate, &task.Priority, &task.Type, &task.Status, &task.CompletedAt,
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

// =====================
// CRM Task Types
// =====================

func (r *crmRepository) scanTaskType(row interface {
	Scan(dest ...any) error
}) (*models.CRMTaskType, error) {
	var t models.CRMTaskType
	if err := row.Scan(&t.ID, &t.OrganizationID, &t.Name, &t.Color, &t.Position, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *crmRepository) ListTaskTypes(ctx context.Context, orgID uuid.UUID) ([]models.CRMTaskType, error) {
	// Types are seeded at org creation (see SeedDefaultTaskTypes), so this is a
	// plain read — it never re-creates defaults, which would resurrect types the
	// user deliberately deleted.
	rows, err := r.db.Query(ctx, `
		SELECT id, organization_id, name, color, position, created_at, updated_at
		FROM crm_task_types
		WHERE organization_id = $1
		ORDER BY position ASC, name ASC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	types := []models.CRMTaskType{}
	for rows.Next() {
		var t models.CRMTaskType
		if err := rows.Scan(&t.ID, &t.OrganizationID, &t.Name, &t.Color, &t.Position, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		types = append(types, t)
	}
	return types, rows.Err()
}

// SeedDefaultTaskTypes inserts the org's starter task types (idempotent). Called
// when an organization is created so the CRM tasks UI is usable out of the box;
// the user can rename, recolour, or delete them afterwards.
func SeedDefaultTaskTypes(ctx context.Context, db *pgxpool.Pool, orgID uuid.UUID) error {
	for i, d := range models.DefaultCRMTaskTypes {
		if _, err := db.Exec(ctx,
			`INSERT INTO crm_task_types (organization_id, name, color, position)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (organization_id, name) DO NOTHING`,
			orgID, d.Name, d.Color, i,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *crmRepository) CreateTaskType(ctx context.Context, orgID uuid.UUID, data *models.CreateCRMTaskType) (*models.CRMTaskType, error) {
	color := data.Color
	if color == "" {
		color = "#94a3b8"
	}
	row := r.db.QueryRow(ctx, `
		INSERT INTO crm_task_types (organization_id, name, color, position)
		VALUES ($1, $2, $3, COALESCE((SELECT MAX(position) + 1 FROM crm_task_types WHERE organization_id = $1), 0))
		RETURNING id, organization_id, name, color, position, created_at, updated_at
	`, orgID, data.Name, color)
	return r.scanTaskType(row)
}

func (r *crmRepository) UpdateTaskType(ctx context.Context, orgID, typeID uuid.UUID, data *models.UpdateCRMTaskType) (*models.CRMTaskType, error) {
	setClauses := []string{}
	args := []any{orgID, typeID}
	argPos := 3
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
	if data.Position != nil {
		setClauses = append(setClauses, fmt.Sprintf("position = $%d", argPos))
		args = append(args, *data.Position)
		argPos++
	}
	if len(setClauses) == 0 {
		return nil, errx.ErrNotEnough
	}
	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`
		UPDATE crm_task_types SET %s
		WHERE organization_id = $1 AND id = $2
		RETURNING id, organization_id, name, color, position, created_at, updated_at
	`, strings.Join(setClauses, ", "))
	row := r.db.QueryRow(ctx, query, args...)
	t, err := r.scanTaskType(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		return nil, err
	}
	return t, nil
}

func (r *crmRepository) DeleteTaskType(ctx context.Context, orgID, typeID uuid.UUID) error {
	cmd, err := r.db.Exec(ctx, `DELETE FROM crm_task_types WHERE organization_id = $1 AND id = $2`, orgID, typeID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}
	return nil
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
