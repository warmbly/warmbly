package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// AdminOutreachRepository is the persistence layer for the
// admin-outreach audit log. Writes happen in two phases:
//  1. Insert the row with status='queued' so the audit story exists
//     even if the SMTP/SES call hangs or panics.
//  2. After the send returns, MarkSent / MarkFailed transitions the
//     row to its terminal state and records the error if any.
type AdminOutreachRepository interface {
	Insert(ctx context.Context, m *models.AdminOutreachMessage) error
	MarkSent(ctx context.Context, id uuid.UUID) error
	MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error
	Search(ctx context.Context, search *models.AdminOutreachSearch) (*models.AdminOutreachResult, error)
}

type adminOutreachRepository struct {
	db *pgxpool.Pool
}

func NewAdminOutreachRepository(db *pgxpool.Pool) AdminOutreachRepository {
	return &adminOutreachRepository{db: db}
}

func (r *adminOutreachRepository) Insert(ctx context.Context, m *models.AdminOutreachMessage) error {
	const q = `
		INSERT INTO admin_outreach_messages
			(id, sent_by, to_user_id, to_org_id, to_email, reply_to, subject, body, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'queued', NOW())
		RETURNING created_at`
	return r.db.QueryRow(ctx, q,
		m.ID, m.SentBy, m.ToUserID, m.ToOrgID, m.ToEmail, m.ReplyTo, m.Subject, m.Body,
	).Scan(&m.CreatedAt)
}

func (r *adminOutreachRepository) MarkSent(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE admin_outreach_messages SET status = 'sent', sent_at = NOW() WHERE id = $1`, id)
	return err
}

func (r *adminOutreachRepository) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	_, err := r.db.Exec(ctx, `UPDATE admin_outreach_messages SET status = 'failed', error = $2 WHERE id = $1`, id, errMsg)
	return err
}

// Search is the faceted + cursor-paginated admin outreach log query. Mirrors
// SearchOrganizationsForAdmin (incremental WHERE builder, id keyset, LIMIT+1
// has_more, separate COUNT).
func (r *adminOutreachRepository) Search(ctx context.Context, search *models.AdminOutreachSearch) (*models.AdminOutreachResult, error) {
	limit := search.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{}
	argNum := 1
	where := "WHERE 1=1"

	if search.Query != "" {
		where += ` AND (m.to_email ILIKE $` + itoa(argNum) + ` OR m.subject ILIKE $` + itoa(argNum) + ` OR m.reply_to ILIKE $` + itoa(argNum) + `)`
		args = append(args, "%"+search.Query+"%")
		argNum++
	}
	switch search.Status {
	case "queued", "sent", "failed":
		where += ` AND m.status::text = $` + itoa(argNum)
		args = append(args, search.Status)
		argNum++
	}
	switch search.RecipientType {
	case "user":
		where += ` AND m.to_user_id IS NOT NULL`
	case "org":
		where += ` AND m.to_org_id IS NOT NULL`
	case "email":
		where += ` AND m.to_user_id IS NULL AND m.to_org_id IS NULL`
	}
	if search.SentByQuery != "" {
		where += ` AND (s.email ILIKE $` + itoa(argNum) + ` OR s.first_name ILIKE $` + itoa(argNum) + ` OR s.last_name ILIKE $` + itoa(argNum) + `)`
		args = append(args, "%"+search.SentByQuery+"%")
		argNum++
	}
	if search.HasReplyTo {
		where += ` AND m.reply_to IS NOT NULL AND m.reply_to <> ''`
	}
	if search.HasError {
		where += ` AND m.error IS NOT NULL AND m.error <> ''`
	}
	if search.HasUser {
		where += ` AND m.to_user_id IS NOT NULL`
	}
	if search.HasOrg {
		where += ` AND m.to_org_id IS NOT NULL`
	}

	addAfter := func(col string, v *time.Time) {
		if v != nil {
			where += " AND " + col + " >= $" + itoa(argNum)
			args = append(args, *v)
			argNum++
		}
	}
	addBefore := func(col string, v *time.Time) {
		if v != nil {
			where += " AND " + col + " < ($" + itoa(argNum) + "::timestamptz + INTERVAL '1 day')"
			args = append(args, *v)
			argNum++
		}
	}
	if search.CreatedWithin > 0 {
		where += ` AND m.created_at >= NOW() - ($` + itoa(argNum) + `::int * INTERVAL '1 day')`
		args = append(args, search.CreatedWithin)
		argNum++
	}
	addAfter("m.created_at", search.CreatedAfter)
	addBefore("m.created_at", search.CreatedBefore)
	addAfter("m.sent_at", search.SentAtAfter)
	addBefore("m.sent_at", search.SentAtBefore)

	offset := search.Offset

	orderCol := "m.created_at"
	switch search.SortBy {
	case "sent_at":
		orderCol = "m.sent_at"
	case "status":
		orderCol = "m.status::text"
	case "to_email":
		orderCol = "m.to_email"
	case "subject":
		orderCol = "m.subject"
	}
	orderDir := "DESC"
	if search.SortBy != "" && !search.SortDesc {
		orderDir = "ASC"
	}
	orderBy := "ORDER BY " + orderCol + " " + orderDir + ", m.id DESC"

	args = append(args, limit+1)

	query := `
		SELECT m.id, m.sent_by, m.to_user_id, m.to_org_id, m.to_email, m.reply_to,
			m.subject, m.body, m.status, m.error, m.sent_at, m.created_at,
			COALESCE(s.id, '00000000-0000-0000-0000-000000000000'::uuid), COALESCE(s.first_name, ''), COALESCE(s.last_name, ''), COALESCE(s.email, ''),
			COALESCE(u.id, '00000000-0000-0000-0000-000000000000'::uuid),
			COALESCE(u.first_name, ''), COALESCE(u.last_name, ''), COALESCE(u.email, '')
		FROM admin_outreach_messages m
		LEFT JOIN users s ON s.id = m.sent_by
		LEFT JOIN users u ON u.id = m.to_user_id
		` + where + `
		` + orderBy + `
		` + adminLimitOffset("$"+itoa(argNum), offset)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []models.AdminOutreachMessage{}
	for rows.Next() {
		var m models.AdminOutreachMessage
		var sender models.AdminUserSummary
		var toUserID uuid.UUID
		var toFN, toLN, toEmail string
		if err := rows.Scan(
			&m.ID, &m.SentBy, &m.ToUserID, &m.ToOrgID, &m.ToEmail, &m.ReplyTo,
			&m.Subject, &m.Body, &m.Status, &m.Error, &m.SentAt, &m.CreatedAt,
			&sender.ID, &sender.FirstName, &sender.LastName, &sender.Email,
			&toUserID, &toFN, &toLN, &toEmail,
		); err != nil {
			return nil, err
		}
		// A sender whose account was deleted has no summary.
		if sender.ID != uuid.Nil {
			m.SentByUser = &sender
		}
		if m.ToUserID != nil {
			m.ToUserSummary = &models.AdminUserSummary{
				ID: toUserID, FirstName: toFN, LastName: toLN, Email: toEmail,
			}
		}
		items = append(items, m)
	}

	result := &models.AdminOutreachResult{
		Data:       items,
		Pagination: models.Pagination{HasMore: len(items) > limit},
	}
	if len(items) > limit {
		result.Data = items[:limit]
		result.Pagination.NextCursor = adminNextCursor(offset, limit)
	}

	countQuery := `SELECT COUNT(*) FROM admin_outreach_messages m LEFT JOIN users s ON s.id = m.sent_by LEFT JOIN users u ON u.id = m.to_user_id ` + where
	var total int64
	if err := r.db.QueryRow(ctx, countQuery, args[:len(args)-1]...).Scan(&total); err == nil {
		result.Pagination.Total = &total
	}

	return result, nil
}

// compile-time check
var _ = pgx.ErrNoRows
