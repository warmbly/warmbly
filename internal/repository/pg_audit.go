package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// AuditRepository persists and queries the organization-wide audit trail.
//
// Writes are append-only: there is deliberately no Update/Delete-by-id method.
// The only removal path is PruneOlderThan, driven by the retention job.
type AuditRepository interface {
	Log(ctx context.Context, log *models.CreateAuditLog) error
	Search(ctx context.Context, params *models.AuditLogSearch) (*models.AuditLogsResult, error)
	PruneOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

type auditRepository struct {
	db *pgxpool.Pool
}

func NewAuditRepository(db *pgxpool.Pool) AuditRepository {
	return &auditRepository{db: db}
}

func (r *auditRepository) Log(ctx context.Context, log *models.CreateAuditLog) error {
	changesJSON := marshalAuditMap(log.Changes)
	metadataJSON := marshalAuditMap(log.Metadata)

	query := `
		INSERT INTO audit_logs (organization_id, actor_id, action, entity_type, entity_id, ip_address, user_agent, changes, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := r.db.Exec(ctx, query,
		log.OrgID, log.UserID, string(log.Action), string(log.EntityType), log.EntityID,
		log.IPAddress, log.UserAgent, changesJSON, metadataJSON,
	)
	return err
}

func (r *auditRepository) Search(ctx context.Context, params *models.AuditLogSearch) (*models.AuditLogsResult, error) {
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	// organization_id is mandatory and is the only tenancy boundary. A search
	// without it would scan every org's trail, so guard explicitly.
	if params.OrgID == nil {
		return &models.AuditLogsResult{
			Data:       make([]models.AuditLog, 0),
			Pagination: models.CPagination{HasMore: false},
		}, nil
	}

	args := []interface{}{*params.OrgID}
	argNum := 2
	whereClause := "WHERE al.organization_id = $1"

	if params.ActorID != nil {
		whereClause += " AND al.actor_id = $" + itoa(argNum)
		args = append(args, *params.ActorID)
		argNum++
	}
	if params.Action != nil {
		whereClause += " AND al.action = $" + itoa(argNum)
		args = append(args, string(*params.Action))
		argNum++
	}
	if params.EntityType != nil {
		whereClause += " AND al.entity_type = $" + itoa(argNum)
		args = append(args, string(*params.EntityType))
		argNum++
	}
	if params.EntityID != nil {
		whereClause += " AND al.entity_id = $" + itoa(argNum)
		args = append(args, *params.EntityID)
		argNum++
	}
	if params.Since != nil {
		whereClause += " AND al.created_at >= $" + itoa(argNum)
		args = append(args, *params.Since)
		argNum++
	}
	if params.Until != nil {
		whereClause += " AND al.created_at <= $" + itoa(argNum)
		args = append(args, *params.Until)
		argNum++
	}

	// Keyset pagination on (created_at, id) DESC. The cursor is the last row's
	// id; we resolve its (created_at, id) via subquery so ordering stays stable
	// even though ids are random UUIDs. Matches the campaigns listing pattern.
	if params.Cursor != "" {
		if cursorID, err := uuid.Parse(params.Cursor); err == nil {
			whereClause += " AND (al.created_at, al.id) < (SELECT created_at, id FROM audit_logs WHERE id = $" + itoa(argNum) + ")"
			args = append(args, cursorID)
			argNum++
		}
	}

	args = append(args, limit+1)

	query := `
		SELECT al.id, al.organization_id, al.actor_id, al.action, al.entity_type, al.entity_id,
			al.ip_address, al.user_agent, al.changes, al.metadata, al.created_at,
			u.id, u.first_name, u.last_name, u.email
		FROM audit_logs al
		LEFT JOIN users u ON u.id = al.actor_id
		` + whereClause + `
		ORDER BY al.created_at DESC, al.id DESC
		LIMIT $` + itoa(argNum)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]models.AuditLog, 0, limit)
	for rows.Next() {
		var (
			log          models.AuditLog
			actorID      *uuid.UUID
			actionStr    string
			entityType   string
			changesJSON  []byte
			metadataJSON []byte

			joinedID   *uuid.UUID
			firstName  *string
			lastName   *string
			actorEmail *string
		)

		if err := rows.Scan(
			&log.ID, &log.OrgID, &actorID, &actionStr, &entityType, &log.EntityID,
			&log.IPAddress, &log.UserAgent, &changesJSON, &metadataJSON, &log.Timestamp,
			&joinedID, &firstName, &lastName, &actorEmail,
		); err != nil {
			return nil, err
		}

		log.Action = models.AuditAction(actionStr)
		log.EntityType = models.AuditEntityType(entityType)
		log.ActionDate = log.Timestamp
		if actorID != nil {
			log.UserID = *actorID
		}
		if len(changesJSON) > 0 {
			_ = json.Unmarshal(changesJSON, &log.Changes)
		}
		if len(metadataJSON) > 0 {
			_ = json.Unmarshal(metadataJSON, &log.Metadata)
		}
		if joinedID != nil {
			log.Actor = &models.AuditActor{ID: *joinedID}
			if firstName != nil {
				log.Actor.FirstName = *firstName
			}
			if lastName != nil {
				log.Actor.LastName = *lastName
			}
			if actorEmail != nil {
				log.Actor.Email = *actorEmail
			}
		}

		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := &models.AuditLogsResult{
		Data:       logs,
		Pagination: models.CPagination{HasMore: len(logs) > limit},
	}
	if len(logs) > limit {
		result.Data = logs[:limit]
		next := logs[limit-1].ID.String()
		result.Pagination.NextCursor = &next
	}

	return result, nil
}

func (r *auditRepository) PruneOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM audit_logs WHERE created_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// marshalAuditMap renders a string map as JSON for a jsonb column, defaulting
// to an empty object so the NOT NULL DEFAULT '{}' contract always holds.
func marshalAuditMap(m map[string]string) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return b
}
