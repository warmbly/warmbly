package repository

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

var ErrInvitationInvalid = errors.New("invitation is missing, expired, or no longer grants access")

// CreateInvitedUser commits the account, access grants and consumed invitation together.
func (r *organizationRepository) CreateInvitedUser(ctx context.Context, tokenHash string, email *mail.Address, passwordHash string) (*models.User, *models.OrganizationMember, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var inv models.OrganizationInvitation
	var acc invitationAccess
	err = tx.QueryRow(ctx, `
		SELECT id, organization_id, email, invited_by, created_at, expires_at,
			access_scope, access_folder_ids::text[], access_campaign_ids::text[], access_email_account_ids::text[]
		FROM organization_invitations
		WHERE token = $1 OR link_token_hash = $1
		FOR UPDATE
	`, tokenHash).Scan(&inv.ID, &inv.OrganizationID, &inv.Email, &inv.InvitedBy, &inv.CreatedAt, &inv.ExpiresAt,
		&acc.scope, &acc.folders, &acc.campaigns, &acc.mailboxes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrInvitationInvalid
	}
	if err != nil {
		return nil, nil, err
	}
	if inv.IsExpired() || !strings.EqualFold(inv.Email, normalizeUserEmail(email.Address)) {
		return nil, nil, ErrInvitationInvalid
	}

	rows, err := tx.Query(ctx, `
		SELECT r.id, r.name, r.permissions
		FROM organization_invitation_roles ir
		JOIN organization_roles r ON r.id = ir.role_id AND r.organization_id = $2
		WHERE ir.invitation_id = $1
		ORDER BY r.created_at, r.id
		FOR SHARE OF r
	`, inv.ID, inv.OrganizationID)
	if err != nil {
		return nil, nil, err
	}
	roles, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.OrganizationRole, error) {
		var role models.OrganizationRole
		err := row.Scan(&role.ID, &role.Name, &role.Permissions)
		return role, err
	})
	if err != nil {
		return nil, nil, err
	}
	if len(roles) == 0 {
		return nil, nil, ErrInvitationInvalid
	}

	u, err := createUser(ctx, tx, email, passwordHash)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	member := &models.OrganizationMember{
		ID: uuid.New(), OrganizationID: inv.OrganizationID, UserID: u.ID,
		Role: roles[0].Name, RoleID: &roles[0].ID, InvitedBy: &inv.InvitedBy,
		InvitedAt: inv.CreatedAt, AcceptedAt: &now, Access: acc.access(),
	}
	roleIDs := make([]uuid.UUID, 0, len(roles))
	for _, role := range roles {
		roleIDs = append(roleIDs, role.ID)
		member.Permissions |= role.Permissions
	}
	if err := addMemberWithRoles(ctx, tx, member, roleIDs); err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM organization_invitations WHERE id = $1`, inv.ID); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return u, member, nil
}
