package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// Multi-role assignment storage. organization_members.permissions stays the
// effective OR snapshot across every assigned role, recomputed in the same
// transaction as any membership/role change so all permission readers stay
// JOIN-free.

// recomputeMemberPermissions sets a member's permission snapshot to the
// bitwise OR of its assigned roles (0 when none). Owner is never touched.
func recomputeMemberPermissions(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		UPDATE organization_members om
		SET permissions = COALESCE((
			SELECT bit_or(r.permissions)
			FROM organization_member_roles mr
			JOIN organization_roles r ON r.id = mr.role_id
			WHERE mr.organization_id = om.organization_id AND mr.user_id = om.user_id
		), 0),
		role = COALESCE((
			SELECT r.name FROM organization_member_roles mr
			JOIN organization_roles r ON r.id = mr.role_id
			WHERE mr.organization_id = om.organization_id AND mr.user_id = om.user_id
			ORDER BY r.created_at ASC LIMIT 1
		), om.role),
		role_id = (
			SELECT r.id FROM organization_member_roles mr
			JOIN organization_roles r ON r.id = mr.role_id
			WHERE mr.organization_id = om.organization_id AND mr.user_id = om.user_id
			ORDER BY r.created_at ASC LIMIT 1
		)
		WHERE om.organization_id = $1 AND om.user_id = $2 AND om.role <> 'owner'
	`, orgID, userID)
	return err
}

// AddMemberWithRoles inserts a membership row and its role assignments and
// recomputes the effective permission snapshot, all in one transaction.
// Used by invite-accept so a partial failure can never strand a member with
// no role rows.
func (r *organizationRepository) AddMemberWithRoles(ctx context.Context, member *models.OrganizationMember, roleIDs []uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `
		INSERT INTO organization_members (id, organization_id, user_id, role, role_id, permissions, invited_by, invited_at, accepted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, member.ID, member.OrganizationID, member.UserID, member.Role, member.RoleID,
		member.Permissions, member.InvitedBy, member.InvitedAt, member.AcceptedAt); err != nil {
		return err
	}
	for _, roleID := range roleIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO organization_member_roles (organization_id, user_id, role_id)
			VALUES ($1, $2, $3) ON CONFLICT DO NOTHING
		`, member.OrganizationID, member.UserID, roleID); err != nil {
			return err
		}
	}
	if err := recomputeMemberPermissions(ctx, tx, member.OrganizationID, member.UserID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// HydrateInvitationRoles fills the Roles slice on each pending invitation
// from one query (mirrors HydrateMemberRoles for the roster).
func (r *organizationRepository) HydrateInvitationRoles(ctx context.Context, invitations []models.OrganizationInvitation) error {
	if len(invitations) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(invitations))
	for _, inv := range invitations {
		ids = append(ids, inv.ID)
	}
	rows, err := r.db.Query(ctx, `
		SELECT ir.invitation_id, r.id, r.name, r.color
		FROM organization_invitation_roles ir
		JOIN organization_roles r ON r.id = ir.role_id
		WHERE ir.invitation_id = ANY($1)
		ORDER BY r.created_at ASC
	`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()

	byInvite := make(map[uuid.UUID][]models.MemberRole)
	for rows.Next() {
		var invID uuid.UUID
		var mr models.MemberRole
		if err := rows.Scan(&invID, &mr.ID, &mr.Name, &mr.Color); err != nil {
			return err
		}
		byInvite[invID] = append(byInvite[invID], mr)
	}
	for i := range invitations {
		invitations[i].Roles = byInvite[invitations[i].ID]
	}
	return nil
}

// SetMemberRoles replaces a member's assigned role set and recomputes the
// effective permission snapshot atomically. All role ids must belong to the
// org (enforced by the FK + the caller's validation).
func (r *organizationRepository) SetMemberRoles(ctx context.Context, orgID, userID uuid.UUID, roleIDs []uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx,
		`DELETE FROM organization_member_roles WHERE organization_id = $1 AND user_id = $2`,
		orgID, userID); err != nil {
		return err
	}
	for _, roleID := range roleIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO organization_member_roles (organization_id, user_id, role_id)
			VALUES ($1, $2, $3) ON CONFLICT DO NOTHING
		`, orgID, userID, roleID); err != nil {
			return err
		}
	}
	if err := recomputeMemberPermissions(ctx, tx, orgID, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// GetMemberRoles returns a member's assigned role refs (for display chips).
func (r *organizationRepository) GetMemberRoles(ctx context.Context, orgID, userID uuid.UUID) ([]models.MemberRole, error) {
	return scanMemberRoles(ctx, r.db, `
		SELECT r.id, r.name, r.color
		FROM organization_member_roles mr
		JOIN organization_roles r ON r.id = mr.role_id
		WHERE mr.organization_id = $1 AND mr.user_id = $2
		ORDER BY r.created_at ASC
	`, orgID, userID)
}

func scanMemberRoles(ctx context.Context, db *pgxpool.Pool, query string, args ...any) ([]models.MemberRole, error) {
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.MemberRole
	for rows.Next() {
		var mr models.MemberRole
		if err := rows.Scan(&mr.ID, &mr.Name, &mr.Color); err != nil {
			return nil, err
		}
		out = append(out, mr)
	}
	return out, nil
}

// HydrateMemberRoles fills the Roles slice on each member from one query, so
// the roster shows every assigned role without an N+1.
func (r *organizationRepository) HydrateMemberRoles(ctx context.Context, orgID uuid.UUID, members []models.OrganizationMember) error {
	if len(members) == 0 {
		return nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT mr.user_id, r.id, r.name, r.color
		FROM organization_member_roles mr
		JOIN organization_roles r ON r.id = mr.role_id
		WHERE mr.organization_id = $1
		ORDER BY r.created_at ASC
	`, orgID)
	if err != nil {
		return err
	}
	defer rows.Close()

	byUser := make(map[uuid.UUID][]models.MemberRole)
	for rows.Next() {
		var userID uuid.UUID
		var mr models.MemberRole
		if err := rows.Scan(&userID, &mr.ID, &mr.Name, &mr.Color); err != nil {
			return err
		}
		byUser[userID] = append(byUser[userID], mr)
	}
	for i := range members {
		members[i].Roles = byUser[members[i].UserID]
	}
	return nil
}

// SetInvitationRoles replaces an invitation's role set (used at invite time).
func (r *organizationRepository) SetInvitationRoles(ctx context.Context, invitationID uuid.UUID, roleIDs []uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx,
		`DELETE FROM organization_invitation_roles WHERE invitation_id = $1`, invitationID); err != nil {
		return err
	}
	for _, roleID := range roleIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO organization_invitation_roles (invitation_id, role_id)
			VALUES ($1, $2) ON CONFLICT DO NOTHING
		`, invitationID, roleID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// GetInvitationRoles returns the role ids attached to an invitation.
func (r *organizationRepository) GetInvitationRoles(ctx context.Context, invitationID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx,
		`SELECT role_id FROM organization_invitation_roles WHERE invitation_id = $1`, invitationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
