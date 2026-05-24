package seed

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedAdminAudit(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	type entry struct {
		id         uuid.UUID
		action     string
		targetType string
		targetID   uuid.UUID
		details    string
	}
	entries := []entry{
		{uuid.MustParse("00000000-0000-0000-0000-0000000000f1"), "user.banned", "user", UserViewerID, `{"reason":"seed example"}`},
		{uuid.MustParse("00000000-0000-0000-0000-0000000000f2"), "user.unbanned", "user", UserViewerID, `{"reason":"seed example"}`},
		{uuid.MustParse("00000000-0000-0000-0000-0000000000f3"), "worker.activated", "worker", WorkerFreeID, `{}`},
		{uuid.MustParse("00000000-0000-0000-0000-0000000000f4"), "plan.created", "plan", PlanEnterpriseID, `{"name":"Enterprise"}`},
	}
	for _, e := range entries {
		_, err := pool.Exec(ctx, `
			INSERT INTO admin_audit_logs (id, admin_user_id, action, target_type, target_id, details, ip_address, user_agent, created_at)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb, '127.0.0.1', 'seed/1.0', NOW())
			ON CONFLICT (id) DO NOTHING
		`, e.id, UserAdminID, e.action, e.targetType, e.targetID, e.details)
		if err != nil {
			return err
		}
	}
	return nil
}

func seedEnterpriseInquiries(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	volume := 50_000
	team := 25
	_, err := pool.Exec(ctx, `
		INSERT INTO enterprise_inquiries (id, company_name, contact_name, contact_email, estimated_volume, team_size, notes, status, created_at)
		VALUES ($1, 'Stark Industries', 'Pepper Potts', 'pepper@stark.test', $2, $3, 'Interested in dedicated workers and SAML SSO.', 'pending', NOW())
		ON CONFLICT (id) DO NOTHING
	`, EnterpriseInquiryID, volume, team)
	return err
}
