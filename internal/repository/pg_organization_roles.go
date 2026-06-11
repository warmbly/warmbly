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
			rl.id, rl.organization_id, rl.name, rl.description, rl.color, rl.permissions,
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
			&role.ID, &role.OrganizationID, &role.Name, &role.Description, &role.Color, &role.Permissions,
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
			rl.id, rl.organization_id, rl.name, rl.description, rl.color, rl.permissions,
			rl.created_at, rl.updated_at,
			(SELECT COUNT(*) FROM organization_members om WHERE om.role_id = rl.id) AS member_count
		FROM organization_roles rl
		WHERE rl.organization_id = $1 AND rl.id = $2
	`
	var role models.OrganizationRole
	err := r.db.QueryRow(ctx, query, orgID, roleID).Scan(
		&role.ID, &role.OrganizationID, &role.Name, &role.Description, &role.Color, &role.Permissions,
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
		INSERT INTO organization_roles (id, organization_id, name, description, color, permissions)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := r.db.Exec(ctx, query, role.ID, role.OrganizationID, role.Name, role.Description, role.Color, role.Permissions)
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
		SET name = $3, description = $4, color = $5, permissions = $6, updated_at = NOW()
		WHERE organization_id = $1 AND id = $2
	`, role.OrganizationID, role.ID, role.Name, role.Description, role.Color, role.Permissions); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE organization_members
		SET role = $2, permissions = $3
		WHERE role_id = $1
	`, role.ID, role.Name, role.Permissions); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// DeleteRole removes a custom role. Returns inUse=true (and deletes nothing)
// while members are still assigned, so an assignment is always deliberate.
func (r *organizationRepository) DeleteRole(ctx context.Context, orgID, roleID uuid.UUID) (bool, error) {
	var inUse bool
	if err := r.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM organization_members WHERE role_id = $1)`, roleID,
	).Scan(&inUse); err != nil {
		return false, err
	}
	if inUse {
		return true, nil
	}

	_, err := r.db.Exec(ctx,
		`DELETE FROM organization_roles WHERE organization_id = $1 AND id = $2`, orgID, roleID)
	return false, err
}
