package seed

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedGroups(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	type entry struct {
		table  string
		id     uuid.UUID
		userID uuid.UUID
		title  string
		color  string
		pos    int
	}
	entries := []entry{
		{"folders", FolderInboxID, UserOwnerID, "Inbox", "#3b82f6", 0},
		{"folders", FolderClosedID, UserOwnerID, "Closed Won", "#10b981", 1},
		{"tags", TagVIPID, UserOwnerID, "VIP", "#a855f7", 0},
		{"tags", TagColdID, UserOwnerID, "Cold", "#64748b", 1},
		{"categories", CategoryLeadID, UserOwnerID, "Lead", "#f97316", 0},
		{"categories", CategoryChurnID, UserOwnerID, "Churn risk", "#ef4444", 1},
	}
	for _, e := range entries {
		_, err := pool.Exec(ctx, `
			INSERT INTO `+e.table+` (id, user_id, title, color, position, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,NOW(),NOW())
			ON CONFLICT (id) DO UPDATE SET
				title = EXCLUDED.title,
				color = EXCLUDED.color,
				position = EXCLUDED.position,
				updated_at = NOW()
		`, e.id, e.userID, e.title, e.color, e.pos)
		if err != nil {
			return err
		}
	}
	return nil
}
