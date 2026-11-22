package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// OrgRiskDerive re-derives the stored posture from the signal set. It runs
// inside the repository's row lock.
type OrgRiskDerive func(map[string]any) (map[string]any, models.OrgRiskState, int, string)

// OrgRiskRepository stores an organization's fused abuse posture.
type OrgRiskRepository interface {
	// GetOrgRisk returns nil, nil when no such organization exists, so a
	// caller can answer "not found" instead of an internal error.
	GetOrgRisk(ctx context.Context, orgID uuid.UUID) (*models.OrgRisk, error)
	// GetOrgRiskStates resolves several organizations at once, for paths that
	// gate a pool rather than a single org.
	GetOrgRiskStates(ctx context.Context, orgIDs []uuid.UUID) (map[uuid.UUID]models.OrgRiskState, error)
	// UpdateOrgRiskSignals reads, derives and writes in one row-locked
	// transaction, so two detectors firing at once cannot each compute a band
	// from a stale signal set. An operator override, when set, wins over the
	// derived band. Returns nil, nil when no such organization exists.
	UpdateOrgRiskSignals(ctx context.Context, orgID uuid.UUID, derive OrgRiskDerive) (*models.OrgRisk, error)
	// SetOrgRiskOverride pins the posture until ClearOrgRiskOverride. It leaves
	// the signals alone: the evidence that led here is still the evidence.
	// Returns nil, nil when no such organization exists.
	SetOrgRiskOverride(ctx context.Context, orgID uuid.UUID, state models.OrgRiskState, reason string, by uuid.UUID) (*models.OrgRisk, error)
	// ClearOrgRiskOverride removes the pin and re-derives the posture from the
	// signals in the same transaction, so the row never reads as pinned-to-
	// nothing.
	ClearOrgRiskOverride(ctx context.Context, orgID uuid.UUID, derive OrgRiskDerive) (*models.OrgRisk, error)
	// OrgsWithSignal lists organizations currently carrying a detector's
	// finding. A recurring sweep needs this to retract findings that no longer
	// hold; without it a signal recorded once would never decay.
	OrgsWithSignal(ctx context.Context, key string) ([]uuid.UUID, error)
	// OrgsWithExpiringSignals lists organizations carrying at least one signal
	// with an expires_at, for the sweep that ages evidence out.
	OrgsWithExpiringSignals(ctx context.Context) ([]uuid.UUID, error)
}

type orgRiskRepository struct {
	DB *db.DB
}

func NewOrgRiskRepository(database *db.DB) OrgRiskRepository {
	return &orgRiskRepository{DB: database}
}

// riskColumns is the RETURNING / SELECT list every read shares.
const riskColumns = `risk_state, risk_score, risk_reason, risk_signals, risk_evaluated_at,
	risk_override, risk_override_reason, risk_override_by, risk_override_at`

// riskRow is one organizations row's risk columns as they come off the wire.
type riskRow struct {
	state          string
	score          int
	reason         *string
	rawSignals     []byte
	evaluatedAt    *time.Time
	override       *string
	overrideReason *string
	overrideBy     *uuid.UUID
	overrideAt     *time.Time
}

func (row *riskRow) dest() []any {
	return []any{&row.state, &row.score, &row.reason, &row.rawSignals, &row.evaluatedAt,
		&row.override, &row.overrideReason, &row.overrideBy, &row.overrideAt}
}

func (r *orgRiskRepository) GetOrgRisk(ctx context.Context, orgID uuid.UUID) (*models.OrgRisk, error) {
	var row riskRow
	err := r.DB.Pool.QueryRow(ctx, `SELECT `+riskColumns+` FROM organizations WHERE id = $1`, orgID).
		Scan(row.dest()...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return buildOrgRisk(orgID, &row), nil
}

func (r *orgRiskRepository) GetOrgRiskStates(ctx context.Context, orgIDs []uuid.UUID) (map[uuid.UUID]models.OrgRiskState, error) {
	out := make(map[uuid.UUID]models.OrgRiskState, len(orgIDs))
	if len(orgIDs) == 0 {
		return out, nil
	}
	// A workspace its reseller suspended or cancelled reads as suspended here, so every send path holds it without parking campaigns.
	rows, err := r.DB.Pool.Query(ctx, `
		SELECT o.id, CASE WHEN po.state IN ('suspended', 'cancelled') THEN 'suspended' ELSE o.risk_state END
		FROM organizations o LEFT JOIN partner_organizations po ON po.organization_id = o.id
		WHERE o.id = ANY($1::uuid[])`, orgIDs)
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

func (r *orgRiskRepository) UpdateOrgRiskSignals(ctx context.Context, orgID uuid.UUID, derive OrgRiskDerive) (*models.OrgRisk, error) {
	return r.rederive(ctx, orgID, false, derive)
}

func (r *orgRiskRepository) ClearOrgRiskOverride(ctx context.Context, orgID uuid.UUID, derive OrgRiskDerive) (*models.OrgRisk, error) {
	return r.rederive(ctx, orgID, true, derive)
}

// rederive is the one read/derive/write path. With clearOverride the pin is
// dropped first, so the derived band lands; otherwise the pin, if any, wins.
func (r *orgRiskRepository) rederive(ctx context.Context, orgID uuid.UUID, clearOverride bool, derive OrgRiskDerive) (*models.OrgRisk, error) {
	tx, err := r.DB.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var rawSignals []byte
	err = tx.QueryRow(ctx,
		`SELECT risk_signals FROM organizations WHERE id = $1 FOR UPDATE`, orgID).Scan(&rawSignals)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	signals := decodeSignals(rawSignals)

	next, state, score, reason := derive(signals)
	encoded, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}

	if clearOverride {
		if _, err := tx.Exec(ctx, `
			UPDATE organizations
			   SET risk_override = NULL, risk_override_reason = NULL,
			       risk_override_by = NULL, risk_override_at = NULL
			 WHERE id = $1`, orgID); err != nil {
			return nil, err
		}
	}

	// While an operator's pin is set it is the state, and their reason is the
	// reason the customer reads; the detectors only refresh the evidence.
	var row riskRow
	if err := tx.QueryRow(ctx, `
		UPDATE organizations
		   SET risk_signals = $2,
		       risk_score = $3,
		       risk_state = COALESCE(risk_override, $4),
		       risk_reason = CASE WHEN risk_override IS NOT NULL
		                          THEN COALESCE(NULLIF(risk_override_reason, ''), NULLIF($5, ''))
		                          ELSE NULLIF($5, '') END,
		       risk_evaluated_at = NOW()
		 WHERE id = $1
		RETURNING `+riskColumns, orgID, encoded, score, string(state), reason).
		Scan(row.dest()...); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return buildOrgRisk(orgID, &row), nil
}

func (r *orgRiskRepository) SetOrgRiskOverride(ctx context.Context, orgID uuid.UUID, state models.OrgRiskState, reason string, by uuid.UUID) (*models.OrgRisk, error) {
	var byID *uuid.UUID
	if by != uuid.Nil {
		byID = &by
	}
	var row riskRow
	err := r.DB.Pool.QueryRow(ctx, `
		UPDATE organizations
		   SET risk_state = $2,
		       risk_reason = NULLIF($3, ''),
		       risk_override = $2,
		       risk_override_reason = NULLIF($3, ''),
		       risk_override_by = $4,
		       risk_override_at = NOW(),
		       risk_evaluated_at = NOW()
		 WHERE id = $1
		RETURNING `+riskColumns, orgID, string(state), reason, byID).
		Scan(row.dest()...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return buildOrgRisk(orgID, &row), nil
}

func decodeSignals(raw []byte) map[string]any {
	signals := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &signals)
	}
	return signals
}

func buildOrgRisk(orgID uuid.UUID, row *riskRow) *models.OrgRisk {
	risk := &models.OrgRisk{
		OrganizationID: orgID,
		State:          models.OrgRiskState(row.state),
		Score:          row.score,
		Signals:        decodeSignals(row.rawSignals),
		EvaluatedAt:    row.evaluatedAt,
	}
	if row.reason != nil {
		risk.Reason = *row.reason
	}
	if row.override != nil {
		risk.Override = &models.OrgRiskOverride{
			State: models.OrgRiskState(*row.override),
			By:    row.overrideBy,
			At:    row.overrideAt,
		}
		if row.overrideReason != nil {
			risk.Override.Reason = *row.overrideReason
		}
	}
	return risk
}

func (r *orgRiskRepository) OrgsWithSignal(ctx context.Context, key string) ([]uuid.UUID, error) {
	return r.orgIDs(ctx, `SELECT id FROM organizations WHERE risk_signals ? $1`, key)
}

func (r *orgRiskRepository) OrgsWithExpiringSignals(ctx context.Context) ([]uuid.UUID, error) {
	// The timestamp is compared in Go, where a malformed value is skipped
	// rather than failing the whole sweep on a cast.
	return r.orgIDs(ctx, `SELECT id FROM organizations
		WHERE risk_signals <> '{}'::jsonb
		  AND EXISTS (SELECT 1 FROM jsonb_each(risk_signals) s WHERE s.value ? 'expires_at')`)
}

func (r *orgRiskRepository) orgIDs(ctx context.Context, query string, args ...any) ([]uuid.UUID, error) {
	rows, err := r.DB.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
