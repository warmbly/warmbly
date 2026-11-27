package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
)

// emptyBodyHTML is what a step with no body carries: the dashboard composer's
// own empty document, so opening a blank step in the editor shows an empty
// canvas rather than nothing at all. It is NOT an empty string, which is why
// every "does this step have a body" check goes through mailhtml.HasContent.
const emptyBodyHTML = "<div></div>"

type SequenceRepository interface {
	Create(ctx context.Context, orgID, campaignID string) (*models.Sequence, *errx.Error)
	Get(ctx context.Context, orgID, campaignID string) ([]models.Sequence, *errx.Error)
	Update(ctx context.Context, orgID, campaignID, sequenceID string, data *models.UpdateSequence) (*models.Sequence, *errx.Error)
	// UpdateLayout merges only x/y for the given steps under a campaign, in one
	// statement, without bumping updated_at — so a drag never reads as a content
	// change. Cosmetic and unaudited.
	UpdateLayout(ctx context.Context, orgID, campaignID string, positions []models.SequencePosition) *errx.Error
	// Reorder writes the position, wait and connections plan computes from the
	// steps read under the campaign lock, refusing once the campaign has sent a
	// step. Returns the step now at position 1.
	Reorder(ctx context.Context, orgID, campaignID string, plan ReorderPlan) (*models.Sequence, *errx.Error)
	Delete(ctx context.Context, orgID, campaignID, sequenceID string) *errx.Error
}

// SequenceReorder is one step's new position, wait and connections for a reorder.
type SequenceReorder struct {
	ID         uuid.UUID
	Position   int
	WaitAfter  int
	Conditions json.RawMessage
}

// ReorderPlan turns the campaign's steps, read under the campaign lock, into the
// updates to write. Returning an error aborts the transaction unchanged.
type ReorderPlan func(steps []models.Sequence) ([]SequenceReorder, *errx.Error)

type sequenceRepository struct {
	DB      *db.DB
	Encrypt *encrypt.Encrypter
}

func NewSequenceRepostory(db *db.DB) SequenceRepository {
	return &sequenceRepository{
		DB: db,
	}
}

var SequenceSelections []string = []string{
	"id",
	"name",
	"subject",
	"body_plain",
	"body_html",
	"body_sync",
	"body_code",
	"wait_after",
	"position",
	"thread_reply",
	"x",
	"y",
	"conditions",
	"kind",
	"action",
	"updated_at",
	"created_at",
}

func getSequenceSelect(join bool) string {
	sel := SequenceSelections
	if join {
		for i := range sel {
			sel[i] = "s." + sel[i]
		}
	}
	return strings.Join(sel, ", ")
}

var (
	SequenceSelect     = getSequenceSelect(false)
	SequenceSelectJoin = getSequenceSelect(true)
)

func GetSequence(row db.Scannable, seq *models.Sequence) error {
	return row.Scan(
		&seq.ID, &seq.Name, &seq.Subject, &seq.BodyPlain, &seq.BodyHTML, &seq.BodySync,
		&seq.BodyCode, &seq.WaitAfter, &seq.Position, &seq.ThreadReply, &seq.X, &seq.Y, &seq.Conditions, &seq.Kind, &seq.Action,
		&seq.UpdatedAt, &seq.CreatedAt,
	)
}

func (r *sequenceRepository) Get(ctx context.Context, orgID string, campaignID string) ([]models.Sequence, *errx.Error) {
	query := fmt.Sprintf(
		`SELECT %s
		 FROM sequences s
		 JOIN campaigns c ON s.campaign_id = c.id
		 WHERE s.campaign_id = $1
		  AND c.organization_id = $2
		 ORDER BY s.position ASC, s.created_at ASC`,
		SequenceSelectJoin,
	)

	params := []any{
		campaignID,
		orgID,
	}

	rows, err := r.DB.Query(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}

	defer rows.Close()

	var sequences []models.Sequence = make([]models.Sequence, 0)

	for rows.Next() {
		var seq models.Sequence
		err = GetSequence(rows, &seq)
		if err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		sequences = append(sequences, seq)
	}

	if err := rows.Err(); err != nil {
		db.CaptureError(err, query, params, "rows")
		return nil, errx.InternalError()
	}
	return sequences, nil
}

func (r *sequenceRepository) Create(ctx context.Context, orgID string, campaignID string) (*models.Sequence, *errx.Error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	// FOR UPDATE serialises step creation per campaign, so two concurrent
	// inserts cannot take the same position.
	query := `
		SELECT id
		FROM campaigns WHERE id = $1 AND organization_id = $2
		FOR UPDATE
	`

	params := []any{
		campaignID,
		orgID,
	}
	var locked uuid.UUID
	err = tx.QueryRow(
		ctx,
		query,
		params...,
	).Scan(&locked)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	// Get the next position for this campaign's sequences
	var nextPos int
	_ = tx.QueryRow(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM sequences WHERE campaign_id = $1`, campaignID).Scan(&nextPos)

	// organization_id is inherited from the campaign: a step that lost it is
	// invisible to every org-scoped read and skips org-scoped send gates.
	query = fmt.Sprintf(
		`INSERT INTO sequences (
			campaign_id, organization_id, name, subject, body_plain, body_html, position
		 ) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		 ) RETURNING %s`, SequenceSelect,
	)

	params = []any{
		campaignID,
		orgID,
		config.SequenceDefaultName,
		"",
		"",
		emptyBodyHTML,
		nextPos,
	}

	row := tx.QueryRow(
		ctx,
		query,
		params...,
	)
	var seq models.Sequence
	err = GetSequence(row, &seq)
	if err != nil {
		db.CaptureError(err, query, params, "scan")
		return nil, errx.InternalError()
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}

	return &seq, nil
}

func (r *sequenceRepository) Update(ctx context.Context, orgID, campaignID, sequenceID string, data *models.UpdateSequence) (*models.Sequence, *errx.Error) {
	setClauses := []string{}
	args := []any{orgID, campaignID, sequenceID}
	argPos := 4

	if data.Name != nil {
		if len(*data.Name) > 50 {
			return nil, errx.ErrSequenceName
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "name", argPos))
		args = append(args, *data.Name)
		argPos++
	}
	if data.Subject != nil {
		if len(*data.Subject) > 100 {
			return nil, errx.ErrSequenceSubject
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "subject", argPos))
		args = append(args, *data.Subject)
		argPos++
	}
	if data.BodyPlain != nil {
		if len(*data.BodyPlain) > config.SequenceBodyLimit {
			return nil, errx.ErrSequenceBody
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "body_plain", argPos))
		args = append(args, *data.BodyPlain)
		argPos++
	}
	if data.BodyHTML != nil {
		if len(*data.BodyHTML) > config.SequenceBodyLimit {
			return nil, errx.ErrSequenceBody
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "body_html", argPos))
		args = append(args, *data.BodyHTML)
		argPos++
	}
	if data.BodySync != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "body_sync", argPos))
		args = append(args, *data.BodySync)
		argPos++
	}
	if data.BodyCode != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "body_code", argPos))
		args = append(args, *data.BodyCode)
		argPos++
	}
	if data.WaitAfter != nil {
		if *data.WaitAfter < 0 || *data.WaitAfter > config.SequenceWaitAfterMax {
			return nil, errx.ErrSequenceWaitAfter
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "wait_after", argPos))
		args = append(args, *data.WaitAfter)
		argPos++
	}
	if data.ThreadReply != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "thread_reply", argPos))
		args = append(args, *data.ThreadReply)
		argPos++
	}
	if data.Conditions != nil {
		// Validate shape (operators/fields/values) before persisting. Cross-step
		// validation (targets-in-campaign + no cycles) happens in the service
		// layer where the full sequence set is available.
		if verr := validateBranchConditions(data.Conditions); verr != nil {
			return nil, verr
		}
		raw, merr := json.Marshal(data.Conditions)
		if merr != nil {
			db.CaptureError(merr, "", nil, "marshal_conditions")
			return nil, errx.InternalError()
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "conditions", argPos))
		args = append(args, raw)
		argPos++
	}
	if data.Kind != nil {
		if *data.Kind != "email" && *data.Kind != "action" && *data.Kind != "wait" {
			return nil, errx.ErrSequenceKind
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "kind", argPos))
		args = append(args, *data.Kind)
		argPos++
	}
	if data.Action != nil {
		if verr := validateActionConfig(data.Action); verr != nil {
			return nil, verr
		}
		raw, merr := json.Marshal(data.Action)
		if merr != nil {
			db.CaptureError(merr, "", nil, "marshal_action")
			return nil, errx.InternalError()
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "action", argPos))
		args = append(args, raw)
		argPos++
	}

	if argPos == 4 {
		return nil, errx.ErrNotEnough
	}

	var seq models.Sequence
	query := fmt.Sprintf(
		`UPDATE sequences s
		 SET %s
		 FROM campaigns c
		 WHERE c.organization_id = $1
		  AND c.id = $2
		  AND s.campaign_id = c.id
		  AND s.id = $3
		 RETURNING %s`,
		strings.Join(setClauses, ", "),
		SequenceSelectJoin,
	)

	row := r.DB.QueryRow(
		ctx,
		query,
		args...,
	)
	err := GetSequence(row, &seq)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, query, args, "queryrow")
		return nil, errx.InternalError()
	}

	return &seq, nil
}

// UpdateLayout changes only workspace-scoped step coordinates; unknown ids are ignored.
func (r *sequenceRepository) UpdateLayout(ctx context.Context, orgID, campaignID string, positions []models.SequencePosition) *errx.Error {
	ids := make([]uuid.UUID, 0, len(positions))
	xs := make([]float64, 0, len(positions))
	ys := make([]float64, 0, len(positions))
	for _, p := range positions {
		id, err := uuid.Parse(p.ID)
		if err != nil {
			continue
		}
		ids = append(ids, id)
		xs = append(xs, p.X)
		ys = append(ys, p.Y)
	}
	if len(ids) == 0 {
		return nil
	}

	query := `
		UPDATE sequences AS s
		SET x = v.x, y = v.y
		FROM unnest($3::uuid[], $4::float8[], $5::float8[]) AS v(id, x, y)
		WHERE s.id = v.id
		  AND s.campaign_id = $2
		  AND EXISTS (SELECT 1 FROM campaigns c WHERE c.id = $2 AND c.organization_id = $1)`
	args := []any{orgID, campaignID, ids, xs, ys}
	if _, err := r.DB.Exec(ctx, query, args...); err != nil {
		db.CaptureError(err, query, args, "exec")
		return errx.InternalError()
	}
	return nil
}

// Reorder runs plan against the steps read under the campaign row lock and writes its result in one transaction.
func (r *sequenceRepository) Reorder(ctx context.Context, orgID, campaignID string, plan ReorderPlan) (*models.Sequence, *errx.Error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	lockQuery := `
		SELECT id
		FROM campaigns WHERE id = $1 AND organization_id = $2
		FOR UPDATE
	`
	var locked uuid.UUID
	if err := tx.QueryRow(ctx, lockQuery, campaignID, orgID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, lockQuery, []any{campaignID, orgID}, "queryrow")
		return nil, errx.InternalError()
	}

	// A contact mid-sequence would skip or repeat steps under a new order.
	var started bool
	startedQuery := `SELECT EXISTS (SELECT 1 FROM campaign_contact_progress WHERE campaign_id = $1 AND (sent_at IS NOT NULL OR dispatched_at IS NOT NULL))`
	if err := tx.QueryRow(ctx, startedQuery, campaignID).Scan(&started); err != nil {
		db.CaptureError(err, startedQuery, []any{campaignID}, "queryrow")
		return nil, errx.InternalError()
	}
	if started {
		return nil, errx.ErrSequenceOrderStarted
	}

	// Read the steps under the campaign lock. Locking the sequence rows too holds
	// a concurrent branch edit until this transaction commits, so the conditions
	// written below are computed from state nothing else changed underneath.
	lockStepsQuery := `SELECT id FROM sequences WHERE campaign_id = $1 ORDER BY position ASC, created_at ASC FOR UPDATE`
	if _, err := tx.Exec(ctx, lockStepsQuery, campaignID); err != nil {
		db.CaptureError(err, lockStepsQuery, []any{campaignID}, "exec")
		return nil, errx.InternalError()
	}

	readQuery := fmt.Sprintf(`
		SELECT %s
		FROM sequences s
		WHERE s.campaign_id = $1
		ORDER BY s.position ASC, s.created_at ASC`, SequenceSelectJoin)
	rows, err := tx.Query(ctx, readQuery, campaignID)
	if err != nil {
		db.CaptureError(err, readQuery, []any{campaignID}, "query")
		return nil, errx.InternalError()
	}
	steps := make([]models.Sequence, 0)
	for rows.Next() {
		var seq models.Sequence
		if err := GetSequence(rows, &seq); err != nil {
			rows.Close()
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		steps = append(steps, seq)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		db.CaptureError(err, readQuery, []any{campaignID}, "rows")
		return nil, errx.InternalError()
	}

	updates, xerr := plan(steps)
	if xerr != nil {
		return nil, xerr
	}
	if len(updates) == 0 {
		return nil, errx.ErrNotFound
	}

	// SequenceSelectJoin is a column list, not a bind value: format it into the
	// statement rather than passing it as an argument.
	updateQuery := fmt.Sprintf(`
		UPDATE sequences s
		SET position = $3, conditions = $4, wait_after = $6
		FROM campaigns c
		WHERE c.organization_id = $1
		 AND c.id = $2
		 AND s.campaign_id = c.id
		 AND s.id = $5
		RETURNING %s`, SequenceSelectJoin)

	var entry *models.Sequence
	for _, u := range updates {
		row := tx.QueryRow(ctx, updateQuery, orgID, campaignID, u.Position, u.Conditions, u.ID, u.WaitAfter)
		var seq models.Sequence
		if err := GetSequence(row, &seq); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, errx.ErrNotFound
			}
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		if seq.Position == 1 {
			entry = &seq
		}
	}
	if entry == nil {
		return nil, errx.InternalError()
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}
	return entry, nil
}

func (r *sequenceRepository) Delete(ctx context.Context, orgID, campaignID, sequenceID string) *errx.Error {
	query := `
		DELETE FROM sequences s
		USING campaigns c
		WHERE c.organization_id = $1
		 AND c.id = $2
		 AND s.campaign_id = c.id
		 AND s.id = $3
	`

	params := []any{
		orgID,
		campaignID,
		sequenceID,
	}

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return errx.InternalError()
	}
	defer tx.Rollback(ctx)

	cmd, err := tx.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}

	if stepID, err := uuid.Parse(sequenceID); err == nil {
		if err := ResolveAdvisorFindingsFor(ctx, tx, orgID, []uuid.UUID{stepID}); err != nil {
			db.CaptureError(err, "", nil, "exec")
			return errx.InternalError()
		}
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return errx.InternalError()
	}
	return nil
}
