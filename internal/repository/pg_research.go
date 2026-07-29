package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// ResearchRepository persists contact research runs.
type ResearchRepository interface {
	// CreateRun inserts a run. If idempotencyKey is non-empty and a run already
	// exists for it, the existing run is returned with replayed=true (no insert).
	CreateRun(ctx context.Context, run *models.ContactResearchRun, idempotencyKey string) (created *models.ContactResearchRun, replayed bool, err error)
	GetRun(ctx context.Context, orgID, id uuid.UUID) (*models.ContactResearchRun, error)
	ListByContact(ctx context.Context, orgID, contactID uuid.UUID, limit int) ([]models.ContactResearchRun, error)
	// UpdateRun writes the terminal state (status, result, error, credits, model,
	// tokens).
	UpdateRun(ctx context.Context, run *models.ContactResearchRun) error
	// ClaimNextQueued atomically claims the oldest queued run (FOR UPDATE SKIP
	// LOCKED) and flips it to running. Returns nil when the queue is empty.
	ClaimNextQueued(ctx context.Context) (*models.ContactResearchRun, error)
}

type researchRepository struct {
	DB *db.DB
}

func NewResearchRepository(database *db.DB) ResearchRepository {
	return &researchRepository{DB: database}
}

const researchCols = `id, org_id, contact_id, requested_by, status, objective, result, error, credits_charged, model_used, tokens_used, created_at, updated_at`

func scanRun(row pgx.Row, r *models.ContactResearchRun) error {
	var resultRaw []byte
	if err := row.Scan(&r.ID, &r.OrgID, &r.ContactID, &r.RequestedBy, &r.Status, &r.Objective, &resultRaw, &r.Error, &r.CreditsCharged, &r.ModelUsed, &r.TokensUsed, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return err
	}
	if len(resultRaw) > 0 {
		_ = json.Unmarshal(resultRaw, &r.Result)
	}
	return nil
}

func (r *researchRepository) CreateRun(ctx context.Context, run *models.ContactResearchRun, idempotencyKey string) (*models.ContactResearchRun, bool, error) {
	if idempotencyKey != "" {
		// Scope the replay lookup to the caller's org so a client-supplied key
		// can never return another tenant's run.
		existing := &models.ContactResearchRun{}
		err := scanRun(r.DB.QueryRow(ctx, `SELECT `+researchCols+` FROM contact_research_runs WHERE idempotency_key = $1 AND org_id = $2`, idempotencyKey, run.OrgID), existing)
		if err == nil {
			return existing, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, false, err
		}
	}

	resultRaw, err := json.Marshal(run.Result)
	if err != nil {
		return nil, false, err
	}
	var keyArg *string
	if idempotencyKey != "" {
		keyArg = &idempotencyKey
	}
	out := &models.ContactResearchRun{}
	err = scanRun(r.DB.QueryRow(ctx, `
		INSERT INTO contact_research_runs (org_id, contact_id, requested_by, status, objective, result, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+researchCols,
		run.OrgID, run.ContactID, run.RequestedBy, run.Status, run.Objective, resultRaw, keyArg), out)
	if err != nil {
		return nil, false, err
	}
	return out, false, nil
}

func (r *researchRepository) GetRun(ctx context.Context, orgID, id uuid.UUID) (*models.ContactResearchRun, error) {
	run := &models.ContactResearchRun{}
	err := scanRun(r.DB.QueryRow(ctx, `SELECT `+researchCols+` FROM contact_research_runs WHERE id = $1 AND org_id = $2`, id, orgID), run)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return run, nil
}

func (r *researchRepository) ListByContact(ctx context.Context, orgID, contactID uuid.UUID, limit int) ([]models.ContactResearchRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.DB.Query(ctx, `SELECT `+researchCols+` FROM contact_research_runs WHERE org_id = $1 AND contact_id = $2 ORDER BY created_at DESC LIMIT $3`, orgID, contactID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ContactResearchRun, 0)
	for rows.Next() {
		var run models.ContactResearchRun
		if err := scanRun(rows, &run); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (r *researchRepository) UpdateRun(ctx context.Context, run *models.ContactResearchRun) error {
	resultRaw, err := json.Marshal(run.Result)
	if err != nil {
		return err
	}
	_, err = r.DB.Exec(ctx, `
		UPDATE contact_research_runs
		SET status = $2, result = $3, error = $4, credits_charged = $5, model_used = $6, tokens_used = $7, updated_at = now()
		WHERE id = $1`,
		run.ID, run.Status, resultRaw, run.Error, run.CreditsCharged, run.ModelUsed, run.TokensUsed)
	return err
}

func (r *researchRepository) ClaimNextQueued(ctx context.Context) (*models.ContactResearchRun, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id FROM contact_research_runs
		WHERE status = 'queued'
		ORDER BY created_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	run := &models.ContactResearchRun{}
	err = scanRun(tx.QueryRow(ctx, `
		UPDATE contact_research_runs SET status = 'running', updated_at = now()
		WHERE id = $1
		RETURNING `+researchCols, id), run)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return run, nil
}
