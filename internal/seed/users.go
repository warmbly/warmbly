package seed

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
)

func seedUsers(ctx context.Context, pool *pgxpool.Pool, r *Result) error {
	hash, err := argon2.Hash(TestPassword)
	if err != nil {
		return err
	}

	type user struct {
		id        uuid.UUID
		first     string
		last      string
		email     string
		adminPerm uint32
		role      string
	}
	users := []user{
		{UserAdminID, "Super", "Admin", "admin@warmbly.local", uint32(models.AllAdminPermissions), "super-admin"},
		{UserOwnerID, "Olivia", "Owner", "owner@warmbly.local", 0, "org-owner"},
		{UserFounderID, "Frank", "Founder", "founder@warmbly.local", 0, "trial-owner"},
		{UserManagerID, "Marco", "Manager", "manager@warmbly.local", 0, "org-manager"},
		{UserViewerID, "Vera", "Viewer", "viewer@warmbly.local", 0, "org-viewer"},
	}

	for _, u := range users {
		_, err := pool.Exec(ctx, `
			INSERT INTO users (id, first_name, last_name, email, password_hash, max_organizations, free_trial_used, admin_permissions, admin_granted_at, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,5,TRUE,$6,
				CASE WHEN $6 > 0 THEN NOW() ELSE NULL END,
				NOW(), NOW())
			ON CONFLICT (email) DO UPDATE SET
				first_name = EXCLUDED.first_name,
				last_name = EXCLUDED.last_name,
				password_hash = EXCLUDED.password_hash,
				admin_permissions = EXCLUDED.admin_permissions,
				updated_at = NOW()
		`, u.id, u.first, u.last, u.email, hash, u.adminPerm)
		if err != nil {
			return err
		}
		r.Users = append(r.Users, SeededUser{Role: u.role, Email: u.email, ID: u.id.String()})
	}

	// admin_granted_by is a self-FK that has to point to a real user. Backfill
	// it for the super admin row after the row exists.
	_, err = pool.Exec(ctx, `
		UPDATE users SET admin_granted_by = id WHERE id = $1 AND admin_granted_by IS NULL
	`, UserAdminID)
	return err
}
