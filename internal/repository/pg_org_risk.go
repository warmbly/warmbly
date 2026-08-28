package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// OrgRiskRepository stores an organization's fused abuse posture.
type OrgRiskRepository interface {
	GetOrgRisk(ctx context.Context, orgID uuid.UUID) (*models.OrgRisk, error)
	// GetOrgRiskStates resolves several organizations at once, for paths that
	// gate a pool rather than a single org.
	GetOrgRiskStates(ctx context.Context, orgIDs []uuid.UUID) (map[uuid.UUID]models.OrgRiskState, error)
	// UpdateOrgRiskSignals reads, derives and writes in one row-locked
	// transaction, so two detectors firing at once cannot each compute a band
	// from a stale signal set.
	UpdateOrgRiskSignals(ctx context.Context, orgID uuid.UUID,
		derive func(map[string]any) (map[string]any, models.OrgRiskState, int, string)) (*models.OrgRisk, error)
	// SetOrgRiskState is an operator override. It leaves the signals alone:
	// the evidence that led here is still the evidence.
	SetOrgRiskState(ctx context.Context, orgID uuid.UUID, state models.OrgRiskState, reason string) (*models.OrgRisk, error)
}

type orgRiskRepository struct {
	DB *db.DB
}

func NewOrgRiskRepository(database *db.DB) OrgRiskRepository {
	return &orgRiskRepository{DB: database}
}

func (r *orgRiskRepository) GetOrgRisk(ctx context.Context, orgID uuid.UUID) (*models.OrgRisk, error) {
	var (
		state       string
		score       int
		reason      *string
		rawSignals  []byte
		evaluatedAt *time.Time
	)
	err := r.DB.Pool.QueryRow(ctx, `
		SELECT risk_state, risk_score, risk_reason, risk_signals, risk_evaluated_at
		  FROM organizations WHERE id = $1
	`, orgID).Scan(&state, &score, &reason, &rawSignals, &evaluatedAt)
	if err != nil {
		return nil, err
	}
	return buildOrgRisk(orgID, state, score, reason, rawSignals, evaluatedAt), nil
}

func (r *orgRiskRepository) GetOrgRiskStates(ctx context.Context, orgIDs []uuid.UUID) (map[uuid.UUID]models.OrgRiskState, error) {
	out := make(map[uuid.UUID]models.OrgRiskState, len(orgIDs))
	if len(orgIDs) == 0 {
		return out, nil
	}
	rows, err := r.DB.Pool.Query(ctx,
		`SELECT id, risk_state FROM organizations WHERE id = ANY($1::uuid[])`, orgIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var state string
		if err := rows.Scan(&id, &state); err != nil {
			return nil, err
		}
		out[id] = models.OrgRiskState(state)
	}
	return out, rows.Err()
}

func (r *orgRiskRepository) UpdateOrgRiskSignals(ctx context.Context, orgID uuid.UUID,
	derive func(map[string]any) (map[string]any, models.OrgRiskState, int, string)) (*models.OrgRisk, error) {
	tx, err := r.DB.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var rawSignals []byte
	if err := tx.QueryRow(ctx,
		`SELECT risk_signals FROM organizations WHERE id = $1 FOR UPDATE`, orgID).Scan(&rawSignals); err != nil {
		return nil, err
	}
	signals := decodeSignals(rawSignals)

	next, state, score, reason := derive(signals)
	encoded, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}

	var (
		outState    string
		outScore    int
		outReason   *string
		outSignals  []byte
		evaluatedAt *time.Time
	)
	// An operator's suspension is not undone by a detector clearing: only
	// another operator lifts it. Every other band follows the score.
	if err := tx.QueryRow(ctx, `
		UPDATE organizations
		   SET risk_signals = $2,
		       risk_score = $3,
		       risk_state = CASE WHEN risk_state = 'suspended' THEN risk_state ELSE $4 END,
		       risk_reason = NULLIF($5, ''),
		       risk_evaluated_at = NOW()
		 WHERE id = $1
		RETURNING risk_state, risk_score, risk_reason, risk_signals, risk_evaluated_at
	`, orgID, encoded, score, string(state), reason).
		Scan(&outState, &outScore, &outReason, &outSignals, &evaluatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return buildOrgRisk(orgID, outState, outScore, outReason, outSignals, evaluatedAt), nil
}

func (r *orgRiskRepository) SetOrgRiskState(ctx context.Context, orgID uuid.UUID, state models.OrgRiskState, reason string) (*models.OrgRisk, error) {
	var (
		outState    string
		outScore    int
		outReason   *string
		outSignals  []byte
		evaluatedAt *time.Time
	)
	if err := r.DB.Pool.QueryRow(ctx, `
		UPDATE organizations
		   SET risk_state = $2, risk_reason = NULLIF($3, ''), risk_evaluated_at = NOW()
		 WHERE id = $1
		RETURNING risk_state, risk_score, risk_reason, risk_signals, risk_evaluated_at
	`, orgID, string(state), reason).
		Scan(&outState, &outScore, &outReason, &outSignals, &evaluatedAt); err != nil {
		return nil, err
	}
	return buildOrgRisk(orgID, outState, outScore, outReason, outSignals, evaluatedAt), nil
}

func decodeSignals(raw []byte) map[string]any {
	signals := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &signals)
	}
	return signals
}

func buildOrgRisk(orgID uuid.UUID, state string, score int, reason *string, rawSignals []byte, evaluatedAt *time.Time) *models.OrgRisk {
	risk := &models.OrgRisk{
		OrganizationID: orgID,
		State:          models.OrgRiskState(state),
		Score:          score,
		Signals:        decodeSignals(rawSignals),
		EvaluatedAt:    evaluatedAt,
	}
	if reason != nil {
		risk.Reason = *reason
	}
	return risk
}
