package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
)

type GroupRepository interface {
	Create(ctx context.Context, userID uuid.UUID, data *models.GroupCreate) (*models.Group, *errx.Error)
	Delete(ctx context.Context, userID, id uuid.UUID) *errx.Error
	Move(ctx context.Context, userID, id uuid.UUID, position int32) ([]models.Order, *errx.Error)
	Update(ctx context.Context, userID, id uuid.UUID, data *models.GroupUpdate) (*models.Group, *errx.Error)
	List(ctx context.Context, userID uuid.UUID) ([]models.Group, *errx.Error)
}

type groupRepository struct {
	Group   models.GroupType
	DB      *db.DB
	Encrypt *encrypt.Encrypter
}

func NewGroupRepostory(db *db.DB, group models.GroupType) GroupRepository {
	return &groupRepository{
		Group: group,
		DB:    db,
	}
}

// Default palette for groups (folders / tags / categories) that omit a
// color in the create request. Picking server-side keeps the API
// forgiving for clients that just want "any reasonable color" while
// still allowing clients to set a specific one. The list is rotated
// per group so two consecutive creates don't end up identical.
var groupDefaultPalette = []string{
	"#94a3b8", "#38bdf8", "#10b981", "#f59e0b",
	"#ef4444", "#a855f7", "#ec4899", "#14b8a6",
}

func defaultGroupColor(position int32) string {
	if position < 0 {
		position = 0
	}
	return groupDefaultPalette[int(position)%len(groupDefaultPalette)]
}

func (r *groupRepository) Create(ctx context.Context, userID uuid.UUID, data *models.GroupCreate) (*models.Group, *errx.Error) {
	title := strings.TrimSpace(data.Title)
	l := len(title)
	if l < 1 || l > 50 {
		return nil, errx.ErrGroupTitle
	}
	data.Title = title

	// Empty color → pick a sensible default from the palette below
	// (selected after we know the position). Non-empty but invalid
	// still 400s — that's a client bug worth surfacing.
	if data.Color != "" && !crypt.IsValidHexColor(data.Color) {
		return nil, errx.ErrColor
	}

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	var position int32

	query := fmt.Sprintf(`
		SELECT COUNT(*) FROM %s WHERE user_id = $1
	`, r.Group)

	var params = []any{
		userID,
	}

	err = tx.QueryRow(
		ctx,
		query,
		params...,
	).Scan(&position)
	if err != nil {
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	if position >= 100 {
		return nil, errx.ErrGroupMax
	}

	if data.Color == "" {
		data.Color = defaultGroupColor(position)
	}

	t := time.Now()
	id := uuid.New()

	// INSERT without RETURNING returns zero rows, so tx.Exec is the
	// right call here. The previous tx.QueryRow + Scan would always
	// fail with "sql: no rows in result set" once it got this far.
	query = fmt.Sprintf(`
		INSERT INTO %s (id, user_id, title, color, position, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
	`, r.Group)

	params = []any{
		id,
		userID,
		data.Title,
		data.Color,
		position,
		t,
	}

	_, err = tx.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return nil, errx.InternalError()
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, query, nil, "commit")
		return nil, errx.InternalError()
	}

	return &models.Group{
		ID: id,

		Title:    data.Title,
		Color:    data.Color,
		Position: position,

		CreatedAt: t,
		UpdatedAt: t,
	}, nil
}

func (r *groupRepository) Delete(ctx context.Context, userID, id uuid.UUID) *errx.Error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return errx.InternalError()
	}
	defer tx.Rollback(ctx)

	var pos int32

	query := fmt.Sprintf(`
		DELETE FROM %s
		WHERE user_id = $1 AND id = $2
		RETURNING position
	`, r.Group)

	params := []any{
		userID,
		id,
	}

	err = tx.QueryRow(
		ctx,
		query,
		params...,
	).Scan(&pos)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errx.ErrNotFound
		}
		db.CaptureError(err, query, params, "queryrow")
		return errx.InternalError()
	}

	query = `
		UPDATE tags
		SET position = position - 1
		WHERE user_id = $1 AND position > $2
	`

	params = []any{
		userID, pos,
	}

	if _, err := tx.Exec(
		ctx,
		query,
		params...,
	); err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return errx.InternalError()
	}

	return nil
}

func (r *groupRepository) Move(ctx context.Context, userID, id uuid.UUID, newPos int32) ([]models.Order, *errx.Error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	query := fmt.Sprintf(`
		SELECT id, position FROM %s
		WHERE user_id = $1
		ORDER BY position FOR UPDATE
	`, r.Group)

	params := []any{
		userID,
	}

	rows, err := tx.Query(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	type cat struct {
		id  uuid.UUID
		pos int32
	}
	var ordered []cat
	var oldPos int32 = -1
	for rows.Next() {
		var c cat
		if err := rows.Scan(&c.id, &c.pos); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		if c.id == id {
			oldPos = c.pos
		}
		ordered = append(ordered, c)
	}
	if oldPos < 0 {
		return nil, errx.ErrNotFound
	}

	newOrdered := make([]cat, 0, len(ordered))
	for _, c := range ordered {
		if c.id == id {
			continue
		}
		newOrdered = append(newOrdered, c)
	}
	if newPos < 0 || newPos > int32(len(newOrdered)) {
		return nil, errx.ErrPosition
	}
	newOrdered = append(newOrdered[:newPos], append([]cat{{id: id}}, newOrdered[newPos:]...)...)

	updateQuery := fmt.Sprintf(`
		WITH new_values AS (
			SELECT unnest($1::uuid[]) AS id,
			       unnest($2::int[])  AS pos
		)
		UPDATE %s e
		SET position = n.pos
		FROM new_values n
		WHERE e.id = n.id AND e.user_id = $3
	`, r.Group)
	ids := make([]uuid.UUID, len(newOrdered))
	poss := make([]int32, len(newOrdered))
	for i, c := range newOrdered {
		ids[i] = c.id
		poss[i] = int32(i)
	}
	if _, err := tx.Exec(ctx, updateQuery, ids, poss, userID); err != nil {
		db.CaptureError(err, query, params, "exec")
		return nil, errx.InternalError()
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}

	var resp []models.Order = make([]models.Order, len(newOrdered))
	for i := range newOrdered {
		resp = append(resp, models.Order{
			ID:       ids[i],
			Position: poss[i],
		})
	}

	return resp, nil
}

func (r *groupRepository) Update(ctx context.Context, userID, id uuid.UUID, data *models.GroupUpdate) (*models.Group, *errx.Error) {
	setClauses := []string{}
	args := []any{userID, id}
	argPos := 3
	if data.Title != nil {
		trimmed := strings.TrimSpace(*data.Title)
		l := len(trimmed)
		if l < 1 || l > 50 {
			return nil, errx.ErrGroupTitle
		}
		*data.Title = trimmed
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "title", argPos))
		args = append(args, *data.Title)
		argPos++
	}
	if data.Color != nil {
		if !crypt.IsValidHexColor(*data.Color) {
			return nil, errx.ErrColor
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "color", argPos))
		args = append(args, *data.Color)
		argPos++
	}

	setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "updated_at", argPos))
	args = append(args, time.Now())
	argPos++

	query := fmt.Sprintf(
		`
			UPDATE %s SET %s
			WHERE user_id = $1 AND id = $2
			RETURNING id, title, color, position, created_at, updated_at
		`,
		r.Group,
		strings.Join(setClauses, ", "),
	)

	var t models.Group
	row := r.DB.QueryRow(
		ctx,
		query,
		args...,
	)

	err := row.Scan(&t.ID, &t.Title, &t.Color, &t.Position, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		db.CaptureError(err, "", nil, "scan")
		return nil, errx.InternalError()
	}

	return &t, nil
}

// List returns every group of the repository's type belonging to userID,
// ordered by position. The /auth/me handler calls this once per group
// type (folders, tags, categories) to populate the User payload so the
// frontend doesn't have to issue three extra requests on every page
// load — and so created items still appear after a refresh.
func (r *groupRepository) List(ctx context.Context, userID uuid.UUID) ([]models.Group, *errx.Error) {
	query := fmt.Sprintf(
		`SELECT id, title, color, position, created_at, updated_at
		   FROM %s
		  WHERE user_id = $1
		  ORDER BY position ASC, created_at ASC`,
		r.Group,
	)

	rows, err := r.DB.Query(ctx, query, userID)
	if err != nil {
		db.CaptureError(err, query, []any{userID}, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	// Non-nil so JSON marshals as [] not null when the user has none.
	out := make([]models.Group, 0)
	for rows.Next() {
		var g models.Group
		if err := rows.Scan(&g.ID, &g.Title, &g.Color, &g.Position, &g.CreatedAt, &g.UpdatedAt); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, errx.InternalError()
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, query, nil, "rows")
		return nil, errx.InternalError()
	}
	return out, nil
}
