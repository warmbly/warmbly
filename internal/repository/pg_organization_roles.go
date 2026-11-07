package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

// Custom-role storage on the organization repository. Effective member
// permissions stay denormalized on organization_members.permissions: role
// edits write through to assigned members inside one transaction, so every
// permission reader (Go middleware, realtime auth) stays JOIN-free.

// ListRoles returns the org's custom roles with live member counts.
func (r *organizationRepository) ListRoles(ctx context.Context, orgID uuid.UUID) ([]models.OrganizationRole, error) {
	query := `
		SELECT
			rl.id, rl.organization_id, rl.name, rl.description, rl.color, rl.permissions, rl.acts_in_scope,
			rl.created_at, rl.updated_at,
			(SELECT COUNT(*) FROM organization_members om WHERE om.role_id = rl.id) AS member_count
		FROM organization_roles rl
		WHERE rl.organization_id = $1
		ORDER BY rl.created_at ASC
	`
	rows, err := r.db.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []models.OrganizationRole
	for rows.Next() {
		var role models.OrganizationRole
		if err := rows.Scan(
			&role.ID, &role.OrganizationID, &role.Name, &role.Description, &role.Color, &role.Permissions, &role.ActsInScope,
			&role.CreatedAt, &role.UpdatedAt, &role.MemberCount,
		); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, nil
}

// GetRoleByID loads one custom role, org-scoped. nil, nil when unknown.
func (r *organizationRepository) GetRoleByID(ctx context.Context, orgID, roleID uuid.UUID) (*models.OrganizationRole, error) {
	query := `
		SELECT
			rl.id, rl.organization_id, rl.name, rl.description, rl.color, rl.permissions, rl.acts_in_scope,
			rl.created_at, rl.updated_at,
			(SELECT COUNT(*) FROM organization_members om WHERE om.role_id = rl.id) AS member_count
		FROM organization_roles rl
		WHERE rl.organization_id = $1 AND rl.id = $2
	`
	var role models.OrganizationRole
	err := r.db.QueryRow(ctx, query, orgID, roleID).Scan(
		&role.ID, &role.OrganizationID, &role.Name, &role.Description, &role.Color, &role.Permissions, &role.ActsInScope,
		&role.CreatedAt, &role.UpdatedAt, &role.MemberCount,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &role, nil
}

// CountRoles returns how many custom roles the org has (for the cap check).
func (r *organizationRepository) CountRoles(ctx context.Context, orgID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM organization_roles WHERE organization_id = $1`, orgID).Scan(&count)
	return count, err
}

// CreateRole inserts a custom role.
func (r *organizationRepository) CreateRole(ctx context.Context, role *models.OrganizationRole) error {
	query := `
		INSERT INTO organization_roles (id, organization_id, name, description, color, permissions, acts_in_scope)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := r.db.Exec(ctx, query, role.ID, role.OrganizationID, role.Name, role.Description, role.Color, role.Permissions, role.ActsInScope)
	return err
}

// UpdateRole edits a custom role and writes the new name + permissions
// through to every assigned member in the same transaction, keeping the
// denormalized member snapshots authoritative.
func (r *organizationRepository) UpdateRole(ctx context.Context, role *models.OrganizationRole) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `
		UPDATE organization_roles
		SET name = $3, description = $4, color = $5, permissions = $6, acts_in_scope = $7, updated_at = NOW()
		WHERE organization_id = $1 AND id = $2
	`, role.OrganizationID, role.ID, role.Name, role.Description, role.Color, role.Permissions, role.ActsInScope); err != nil {
		return err
	}

	// Recompute the effective OR snapshot for every member assigned this
	// role (they may hold others), excluding the owner.
	if _, err := tx.Exec(ctx, `
		UPDATE organization_members om
		SET permissions = COALESCE((
			SELECT bit_or(r.permissions)
			FROM organization_member_roles mr
			JOIN organization_roles r ON r.id = mr.role_id
			WHERE mr.organization_id = om.organization_id AND mr.user_id = om.user_id
		), 0),
		role = COALESCE((
			SELECT r2.name FROM organization_member_roles mr2
			JOIN organization_roles r2 ON r2.id = mr2.role_id
			WHERE mr2.organization_id = om.organization_id AND mr2.user_id = om.user_id
			ORDER BY r2.created_at ASC LIMIT 1
		), om.role)
		WHERE om.role <> 'owner' AND EXISTS (
			SELECT 1 FROM organization_member_roles mx
			WHERE mx.organization_id = om.organization_id
			  AND mx.user_id = om.user_id AND mx.role_id = $1
		)
	`, role.ID); err != nil {
		return err
	}

	// Pending invitations snapshot the role name for display; keep in sync.
	if _, err := tx.Exec(ctx, `
		UPDATE organization_invitations
		SET role = $2
		WHERE role_id = $1
	`, role.ID, role.Name); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// DeleteRole removes a role. Members keep their other roles; the join FK
// cascades the assignment rows away and each affected member's effective
// snapshot is recomputed in the same transaction. Roles are freely
// deletable (a member left with no roles simply has no permissions).
func (r *organizationRepository) DeleteRole(ctx context.Context, orgID, roleID uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Capture who holds this role before the cascade clears the rows.
	rows, err := tx.Query(ctx,
		`SELECT user_id FROM organization_member_roles WHERE role_id = $1`, roleID)
	if err != nil {
		return err
	}
	var affected []uuid.UUID
	for rows.Next() {
		var uid uuid.UUID
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return err
		}
		affected = append(affected, uid)
	}
	rows.Close()

	if _, err := tx.Exec(ctx,
		`DELETE FROM organization_roles WHERE organization_id = $1 AND id = $2`, orgID, roleID); err != nil {
		return err
	}
	// FK ON DELETE CASCADE already removed the member/invitation role rows;
	// recompute each affected member's snapshot.
	for _, uid := range affected {
		if err := recomputeMemberPermissions(ctx, tx, orgID, uid); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
