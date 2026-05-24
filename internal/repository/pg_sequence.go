package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
)

type SequenceRepository interface {
	Create(ctx context.Context, userID, campaignID string) (*models.Sequence, *errx.Error)
	Get(ctx context.Context, userID, campaignID string) ([]models.Sequence, *errx.Error)
	Update(ctx context.Context, userID, campaignID, sequenceID string, data *models.UpdateSequence) (*models.Sequence, *errx.Error)
	Delete(ctx context.Context, userID, campaignID, sequenceID string) *errx.Error
}

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
		&seq.BodyCode, &seq.WaitAfter, &seq.Position, &seq.UpdatedAt, &seq.CreatedAt,
	)
}

func (r *sequenceRepository) Get(ctx context.Context, userID string, campaignID string) ([]models.Sequence, *errx.Error) {
	query := fmt.Sprintf(
		`SELECT %s
		 FROM sequences s
		 JOIN campaigns c ON s.campaign_id = c.id
		 WHERE s.campaign_id = $1
		  AND c.user_id = $2
		 ORDER BY s.position ASC, s.created_at ASC`,
		SequenceSelectJoin,
	)

	params := []any{
		campaignID,
		userID,
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

	return sequences, nil
}

func (r *sequenceRepository) Create(ctx context.Context, userID string, campaignID string) (*models.Sequence, *errx.Error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	query := `
		SELECT user_id
		FROM campaigns WHERE id = $1
	`

	params := []any{
		campaignID,
	}

	var ownerID string
	err = tx.QueryRow(
		ctx,
		query,
		params...,
	).Scan(&ownerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	if ownerID != userID {
		return nil, errx.ErrForbidden
	}

	// Get the next position for this campaign's sequences
	var nextPos int
	_ = tx.QueryRow(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM sequences WHERE campaign_id = $1`, campaignID).Scan(&nextPos)

	query = fmt.Sprintf(
		`INSERT INTO sequences (
			campaign_id, name, subject, body_plain, body_html, position
		 ) VALUES (
			$1, $2, $3, $4, $5, $6
		 ) RETURNING %s`, SequenceSelect,
	)

	params = []any{
		campaignID,
		config.SequenceDefaultName,
		"",
		"",
		"<div></div>",
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

func (r *sequenceRepository) Update(ctx context.Context, userID, campaignID, sequenceID string, data *models.UpdateSequence) (*models.Sequence, *errx.Error) {
	setClauses := []string{}
	args := []any{userID, campaignID, sequenceID}
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
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "wait_after", argPos))
		args = append(args, *data.WaitAfter)
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
		 WHERE c.user_id = $1
		  AND c.id = $2
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

func (r *sequenceRepository) Delete(ctx context.Context, userID, campaignID, sequenceID string) *errx.Error {
	query := `
		DELETE FROM sequences s
		USING campaigns c
		WHERE c.user_id = $1
		 AND c.id = $2
		 AND s.id = $3
	`

	params := []any{
		userID,
		campaignID,
		sequenceID,
	}

	cmd, err := r.DB.Exec(
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
	return nil
}
