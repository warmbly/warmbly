package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

// CategoriesForMailboxes lists only categories applied to accessible messages, including filed or snoozed mail.
func (r *uniboxRepository) CategoriesForMailboxes(ctx context.Context, orgID uuid.UUID, accountIDs []uuid.UUID) ([]models.Group, error) {
	out := make([]models.Group, 0)
	if len(accountIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT c.id, c.title, c.color, c.position, c.created_at, c.updated_at
		FROM categories c
		WHERE c.organization_id = $1 AND EXISTS (
			SELECT 1 FROM unibox_thread_labels utl
			JOIN unibox_emails ue ON ue.thread_id = utl.thread_id
			JOIN email_accounts ea ON ea.id = ue.email_id AND ea.organization_id = utl.organization_id
			WHERE utl.organization_id = c.organization_id AND utl.category_id = c.id
			  AND ea.id = ANY($2::uuid[])
		)
		ORDER BY c.position ASC, c.title ASC
	`, orgID, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var g models.Group
		if err := rows.Scan(&g.ID, &g.Title, &g.Color, &g.Position, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ListThreadLabelsWithin distinguishes an accessible unlabelled thread from a thread the caller cannot read.
func (r *uniboxRepository) ListThreadLabelsWithin(ctx context.Context, orgID uuid.UUID, threadID string, accountIDs []uuid.UUID) ([]models.MiniCategory, error) {
	var allowed any
	if accountIDs != nil {
		allowed = accountIDs
	}
	rows, err := r.db.Query(ctx, `
		WITH visible_thread AS (
			SELECT 1 FROM unibox_emails ue
			JOIN email_accounts ea ON ea.id = ue.email_id
			WHERE ea.organization_id = $1 AND ue.thread_id = $2
			  AND ($3::uuid[] IS NULL OR ea.id = ANY($3::uuid[]))
			LIMIT 1
		)
		SELECT c.id, c.title, c.color
		FROM visible_thread
		LEFT JOIN (
			SELECT c.id, c.title, c.color, c.position
			FROM unibox_thread_labels utl
			JOIN categories c ON c.id = utl.category_id AND c.organization_id = utl.organization_id
			WHERE utl.organization_id = $1 AND utl.thread_id = $2
		) c ON TRUE
		ORDER BY c.position ASC, c.title ASC
	`, orgID, threadID, allowed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.MiniCategory, 0)
	visible := false
	for rows.Next() {
		var id *uuid.UUID
		var title, color *string
		if err := rows.Scan(&id, &title, &color); err != nil {
			return nil, err
		}
		visible = true
		if id != nil {
			out = append(out, models.MiniCategory{ID: *id, Title: *title, Color: *color})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !visible {
		return nil, pgx.ErrNoRows
	}
	return out, nil
}
