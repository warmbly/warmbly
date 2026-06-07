package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// OrganizationRepository defines the interface for organization data access
type OrganizationRepository interface {
	// Organization CRUD
	Create(ctx context.Context, org *models.Organization) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.Organization, error)
	GetBySlug(ctx context.Context, slug string) (*models.Organization, error)
	Update(ctx context.Context, org *models.Organization) error
	UpdateAvatar(ctx context.Context, orgID uuid.UUID, avatarURL *string) error
	Delete(ctx context.Context, id uuid.UUID) error

	// User's organizations
	GetUserOrganizations(ctx context.Context, userID uuid.UUID) ([]models.OrganizationMember, error)
	GetUserDefaultOrganization(ctx context.Context, userID uuid.UUID) (*models.Organization, error)

	// Member management
	GetMembers(ctx context.Context, orgID uuid.UUID) ([]models.OrganizationMember, error)
	GetMember(ctx context.Context, orgID, userID uuid.UUID) (*models.OrganizationMember, error)
	GetMemberByID(ctx context.Context, memberID uuid.UUID) (*models.OrganizationMember, error)
	AddMember(ctx context.Context, member *models.OrganizationMember) error
	UpdateMember(ctx context.Context, member *models.OrganizationMember) error
	RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error
	GetMemberCount(ctx context.Context, orgID uuid.UUID) (int, error)

	// Invitations
	CreateInvitation(ctx context.Context, inv *models.OrganizationInvitation) error
	GetInvitationByToken(ctx context.Context, token string) (*models.OrganizationInvitation, error)
	GetInvitationByEmail(ctx context.Context, orgID uuid.UUID, email string) (*models.OrganizationInvitation, error)
	GetPendingInvitations(ctx context.Context, orgID uuid.UUID) ([]models.OrganizationInvitation, error)
	GetUserPendingInvitations(ctx context.Context, email string) ([]models.OrganizationInvitation, error)
	DeleteInvitation(ctx context.Context, id uuid.UUID) error
	DeleteExpiredInvitations(ctx context.Context) error

	// Ownership transfer
	TransferOwnership(ctx context.Context, orgID, newOwnerUserID uuid.UUID) error

	// Resource counts
	GetCampaignCounts(ctx context.Context, orgID uuid.UUID) (total int, active int, err error)
	GetMemberCounts(ctx context.Context, orgID uuid.UUID) (int, error)
	GetEmailAccountCount(ctx context.Context, orgID uuid.UUID) (int, error)
	GetContactCount(ctx context.Context, orgID uuid.UUID) (int, error)
	GetEmailsSentTodayCount(ctx context.Context, orgID uuid.UUID) (int, error)

	// Ownership counts
	GetUserOwnedOrganizationCount(ctx context.Context, userID uuid.UUID) (int, error)

	// Enterprise inquiries
	CreateEnterpriseInquiry(ctx context.Context, inquiry *models.EnterpriseInquiry) error
	GetEnterpriseInquiry(ctx context.Context, id uuid.UUID) (*models.EnterpriseInquiry, error)
	ListEnterpriseInquiries(ctx context.Context, status string, limit, offset int) ([]models.EnterpriseInquiry, error)
	UpdateEnterpriseInquiryStatus(ctx context.Context, id uuid.UUID, status string, processedBy uuid.UUID) error

	// Admin permissions
	GetUserAdminPermissions(ctx context.Context, userID uuid.UUID) (uint32, error)

	// Admin: org-wide search/listing for the admin panel
	SearchOrganizationsForAdmin(ctx context.Context, search *models.AdminOrgSearch) (*models.AdminOrgsResult, error)
	GetOrganizationAdminDetail(ctx context.Context, orgID uuid.UUID) (*models.AdminOrgDetail, error)
	GetOrganizationMembersForAdmin(ctx context.Context, orgID uuid.UUID) ([]models.AdminOrgMember, error)

	// Admin: per-org limit overrides. Read returns nil when no row
	// exists (org has never been touched). Upsert always returns the
	// post-write row so callers get the authoritative timestamps back.
	GetOrganizationLimitOverrides(ctx context.Context, orgID uuid.UUID) (*models.OrganizationLimitOverrides, error)
	UpsertOrganizationLimitOverrides(ctx context.Context, orgID uuid.UUID, req *models.UpdateOrgOverridesRequest, grantedBy uuid.UUID) (*models.OrganizationLimitOverrides, error)

	// Limit-increase requests. CreateLimitRequest enforces the partial
	// unique index (one open request per (org, field)) by surfacing the
	// pgx unique-violation error code. UpdateLimitRequestStatus stamps
	// reviewer + timestamp atomically.
	CreateLimitRequest(ctx context.Context, req *models.LimitIncreaseRequest) error
	GetLimitRequest(ctx context.Context, id uuid.UUID) (*models.LimitIncreaseRequest, error)
	ListLimitRequestsForOrg(ctx context.Context, orgID uuid.UUID) ([]models.LimitIncreaseRequest, error)
	ListLimitRequestsForAdmin(ctx context.Context, search *models.AdminLimitRequestSearch) (*models.AdminLimitRequestsResult, error)
	UpdateLimitRequestStatus(ctx context.Context, id uuid.UUID, status models.LimitRequestStatus, reviewedBy uuid.UUID, notes string) error
}

type organizationRepository struct {
	db *pgxpool.Pool
}

// NewOrganizationRepository creates a new organization repository
func NewOrganizationRepository(db *pgxpool.Pool) OrganizationRepository {
	return &organizationRepository{db: db}
}

// Create creates a new organization
func (r *organizationRepository) Create(ctx context.Context, org *models.Organization) error {
	query := `
		INSERT INTO organizations (id, name, slug, owner_user_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	now := time.Now()
	if _, err := r.db.Exec(ctx, query,
		org.ID, org.Name, org.Slug, org.OwnerUserID, now, now,
	); err != nil {
		return err
	}

	// Seed the org's default CRM task types so the tasks UI is usable from day
	// one. Best-effort: a failure here shouldn't block org creation.
	if err := SeedDefaultTaskTypes(ctx, r.db, org.ID); err != nil {
		return err
	}
	return nil
}

// GetByID retrieves an organization by ID
func (r *organizationRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Organization, error) {
	query := `
		SELECT id, name, slug, avatar_url, owner_user_id, created_at, updated_at,
		       deletion_scheduled_at, deletion_scheduled_for
		FROM organizations WHERE id = $1
	`
	return r.scanOrganization(ctx, query, id)
}

// GetBySlug retrieves an organization by slug
func (r *organizationRepository) GetBySlug(ctx context.Context, slug string) (*models.Organization, error) {
	query := `
		SELECT id, name, slug, avatar_url, owner_user_id, created_at, updated_at,
		       deletion_scheduled_at, deletion_scheduled_for
		FROM organizations WHERE slug = $1
	`
	return r.scanOrganization(ctx, query, slug)
}

func (r *organizationRepository) scanOrganization(ctx context.Context, query string, args ...interface{}) (*models.Organization, error) {
	row := r.db.QueryRow(ctx, query, args...)
	var org models.Organization
	err := row.Scan(&org.ID, &org.Name, &org.Slug, &org.AvatarURL, &org.OwnerUserID, &org.CreatedAt, &org.UpdatedAt, &org.DeletionScheduledAt, &org.DeletionScheduledFor)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &org, nil
}

// Update updates an organization
func (r *organizationRepository) Update(ctx context.Context, org *models.Organization) error {
	query := `
		UPDATE organizations SET name = $2, slug = $3, updated_at = $4
		WHERE id = $1
	`
	_, err := r.db.Exec(ctx, query, org.ID, org.Name, org.Slug, time.Now())
	return err
}

// UpdateAvatar sets (or clears) the organization's avatar URL.
func (r *organizationRepository) UpdateAvatar(ctx context.Context, orgID uuid.UUID, avatarURL *string) error {
	const q = `UPDATE organizations SET avatar_url = $2, updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, q, orgID, avatarURL)
	return err
}

// Delete deletes an organization
func (r *organizationRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, id)
	return err
}

// GetUserOrganizations retrieves all organizations a user is a member of
func (r *organizationRepository) GetUserOrganizations(ctx context.Context, userID uuid.UUID) ([]models.OrganizationMember, error) {
	query := `
		SELECT
			om.id, om.organization_id, om.user_id, om.role, om.permissions,
			om.invited_by, om.invited_at, om.accepted_at,
			o.id, o.name, o.slug, o.avatar_url, o.owner_user_id, o.created_at, o.updated_at,
			o.deletion_scheduled_at, o.deletion_scheduled_for
		FROM organization_members om
		JOIN organizations o ON o.id = om.organization_id
		WHERE om.user_id = $1
		ORDER BY om.invited_at DESC
	`
	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []models.OrganizationMember
	for rows.Next() {
		var m models.OrganizationMember
		var org models.Organization
		err := rows.Scan(
			&m.ID, &m.OrganizationID, &m.UserID, &m.Role, &m.Permissions,
			&m.InvitedBy, &m.InvitedAt, &m.AcceptedAt,
			&org.ID, &org.Name, &org.Slug, &org.AvatarURL, &org.OwnerUserID, &org.CreatedAt, &org.UpdatedAt,
			&org.DeletionScheduledAt, &org.DeletionScheduledFor,
		)
		if err != nil {
			return nil, err
		}
		m.Organization = &org
		members = append(members, m)
	}
	return members, nil
}

// GetUserDefaultOrganization retrieves the first organization a user owns
func (r *organizationRepository) GetUserDefaultOrganization(ctx context.Context, userID uuid.UUID) (*models.Organization, error) {
	query := `
		SELECT id, name, slug, avatar_url, owner_user_id, created_at, updated_at,
		       deletion_scheduled_at, deletion_scheduled_for
		FROM organizations WHERE owner_user_id = $1
		ORDER BY created_at ASC LIMIT 1
	`
	return r.scanOrganization(ctx, query, userID)
}

// GetMembers retrieves all members of an organization
func (r *organizationRepository) GetMembers(ctx context.Context, orgID uuid.UUID) ([]models.OrganizationMember, error) {
	query := `
		SELECT
			om.id, om.organization_id, om.user_id, om.role, om.permissions,
			om.invited_by, om.invited_at, om.accepted_at,
			u.id, u.first_name, u.last_name, u.email, u.created_at, u.updated_at
		FROM organization_members om
		JOIN users u ON u.id = om.user_id
		WHERE om.organization_id = $1
		ORDER BY om.role = 'owner' DESC, om.invited_at ASC
	`
	rows, err := r.db.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []models.OrganizationMember
	for rows.Next() {
		var m models.OrganizationMember
		var u models.User
		err := rows.Scan(
			&m.ID, &m.OrganizationID, &m.UserID, &m.Role, &m.Permissions,
			&m.InvitedBy, &m.InvitedAt, &m.AcceptedAt,
			&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.CreatedAt, &u.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		m.User = &u
		m.Email = u.Email
		m.Name = strings.TrimSpace(u.FirstName + " " + u.LastName)
		members = append(members, m)
	}
	return members, nil
}

// GetMember retrieves a specific member of an organization
func (r *organizationRepository) GetMember(ctx context.Context, orgID, userID uuid.UUID) (*models.OrganizationMember, error) {
	query := `
		SELECT id, organization_id, user_id, role, permissions, invited_by, invited_at, accepted_at
		FROM organization_members
		WHERE organization_id = $1 AND user_id = $2
	`
	row := r.db.QueryRow(ctx, query, orgID, userID)
	var m models.OrganizationMember
	err := row.Scan(&m.ID, &m.OrganizationID, &m.UserID, &m.Role, &m.Permissions, &m.InvitedBy, &m.InvitedAt, &m.AcceptedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// GetMemberByID retrieves a member by their membership ID
func (r *organizationRepository) GetMemberByID(ctx context.Context, memberID uuid.UUID) (*models.OrganizationMember, error) {
	query := `
		SELECT id, organization_id, user_id, role, permissions, invited_by, invited_at, accepted_at
		FROM organization_members WHERE id = $1
	`
	row := r.db.QueryRow(ctx, query, memberID)
	var m models.OrganizationMember
	err := row.Scan(&m.ID, &m.OrganizationID, &m.UserID, &m.Role, &m.Permissions, &m.InvitedBy, &m.InvitedAt, &m.AcceptedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// AddMember adds a member to an organization
func (r *organizationRepository) AddMember(ctx context.Context, member *models.OrganizationMember) error {
	query := `
		INSERT INTO organization_members (id, organization_id, user_id, role, permissions, invited_by, invited_at, accepted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := r.db.Exec(ctx, query,
		member.ID, member.OrganizationID, member.UserID, member.Role, member.Permissions,
		member.InvitedBy, member.InvitedAt, member.AcceptedAt,
	)
	return err
}

// UpdateMember updates a member's role and permissions
func (r *organizationRepository) UpdateMember(ctx context.Context, member *models.OrganizationMember) error {
	query := `
		UPDATE organization_members SET role = $3, permissions = $4
		WHERE organization_id = $1 AND user_id = $2
	`
	_, err := r.db.Exec(ctx, query, member.OrganizationID, member.UserID, member.Role, member.Permissions)
	return err
}

// RemoveMember removes a member from an organization
func (r *organizationRepository) RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM organization_members WHERE organization_id = $1 AND user_id = $2`, orgID, userID)
	return err
}

// GetMemberCount returns the number of members in an organization
func (r *organizationRepository) GetMemberCount(ctx context.Context, orgID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM organization_members WHERE organization_id = $1`, orgID).Scan(&count)
	return count, err
}

// CreateInvitation creates a new invitation
func (r *organizationRepository) CreateInvitation(ctx context.Context, inv *models.OrganizationInvitation) error {
	query := `
		INSERT INTO organization_invitations (id, organization_id, email, role, permissions, invited_by, token, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (organization_id, email) DO UPDATE SET
			role = EXCLUDED.role,
			permissions = EXCLUDED.permissions,
			invited_by = EXCLUDED.invited_by,
			token = EXCLUDED.token,
			expires_at = EXCLUDED.expires_at
	`
	_, err := r.db.Exec(ctx, query,
		inv.ID, inv.OrganizationID, inv.Email, inv.Role, inv.Permissions,
		inv.InvitedBy, inv.Token, inv.ExpiresAt, inv.CreatedAt,
	)
	return err
}

// GetInvitationByToken retrieves an invitation by token
func (r *organizationRepository) GetInvitationByToken(ctx context.Context, token string) (*models.OrganizationInvitation, error) {
	query := `
		SELECT
			i.id, i.organization_id, i.email, i.role, i.permissions, i.invited_by, i.token, i.expires_at, i.created_at,
			o.id, o.name, o.slug, o.avatar_url, o.owner_user_id, o.created_at, o.updated_at,
			o.deletion_scheduled_at, o.deletion_scheduled_for
		FROM organization_invitations i
		JOIN organizations o ON o.id = i.organization_id
		WHERE i.token = $1
	`
	row := r.db.QueryRow(ctx, query, token)
	var inv models.OrganizationInvitation
	var org models.Organization
	err := row.Scan(
		&inv.ID, &inv.OrganizationID, &inv.Email, &inv.Role, &inv.Permissions,
		&inv.InvitedBy, &inv.Token, &inv.ExpiresAt, &inv.CreatedAt,
		&org.ID, &org.Name, &org.Slug, &org.AvatarURL, &org.OwnerUserID, &org.CreatedAt, &org.UpdatedAt,
		&org.DeletionScheduledAt, &org.DeletionScheduledFor,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	inv.Organization = &org
	return &inv, nil
}

// GetInvitationByEmail retrieves an invitation by email for a specific organization
func (r *organizationRepository) GetInvitationByEmail(ctx context.Context, orgID uuid.UUID, email string) (*models.OrganizationInvitation, error) {
	query := `
		SELECT id, organization_id, email, role, permissions, invited_by, token, expires_at, created_at
		FROM organization_invitations
		WHERE organization_id = $1 AND email = $2
	`
	row := r.db.QueryRow(ctx, query, orgID, email)
	var inv models.OrganizationInvitation
	err := row.Scan(&inv.ID, &inv.OrganizationID, &inv.Email, &inv.Role, &inv.Permissions, &inv.InvitedBy, &inv.Token, &inv.ExpiresAt, &inv.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// GetPendingInvitations retrieves all pending invitations for an organization
func (r *organizationRepository) GetPendingInvitations(ctx context.Context, orgID uuid.UUID) ([]models.OrganizationInvitation, error) {
	query := `
		SELECT id, organization_id, email, role, permissions, invited_by, token, expires_at, created_at
		FROM organization_invitations
		WHERE organization_id = $1 AND expires_at > NOW()
		ORDER BY created_at DESC
	`
	rows, err := r.db.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invitations []models.OrganizationInvitation
	for rows.Next() {
		var inv models.OrganizationInvitation
		err := rows.Scan(&inv.ID, &inv.OrganizationID, &inv.Email, &inv.Role, &inv.Permissions, &inv.InvitedBy, &inv.Token, &inv.ExpiresAt, &inv.CreatedAt)
		if err != nil {
			return nil, err
		}
		invitations = append(invitations, inv)
	}
	return invitations, nil
}

// GetUserPendingInvitations retrieves all pending invitations for a user's email
func (r *organizationRepository) GetUserPendingInvitations(ctx context.Context, email string) ([]models.OrganizationInvitation, error) {
	query := `
		SELECT
			i.id, i.organization_id, i.email, i.role, i.permissions, i.invited_by, i.token, i.expires_at, i.created_at,
			o.id, o.name, o.slug, o.avatar_url, o.owner_user_id, o.created_at, o.updated_at,
			o.deletion_scheduled_at, o.deletion_scheduled_for
		FROM organization_invitations i
		JOIN organizations o ON o.id = i.organization_id
		WHERE i.email = $1 AND i.expires_at > NOW()
		ORDER BY i.created_at DESC
	`
	rows, err := r.db.Query(ctx, query, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invitations []models.OrganizationInvitation
	for rows.Next() {
		var inv models.OrganizationInvitation
		var org models.Organization
		err := rows.Scan(
			&inv.ID, &inv.OrganizationID, &inv.Email, &inv.Role, &inv.Permissions,
			&inv.InvitedBy, &inv.Token, &inv.ExpiresAt, &inv.CreatedAt,
			&org.ID, &org.Name, &org.Slug, &org.AvatarURL, &org.OwnerUserID, &org.CreatedAt, &org.UpdatedAt,
			&org.DeletionScheduledAt, &org.DeletionScheduledFor,
		)
		if err != nil {
			return nil, err
		}
		inv.Organization = &org
		invitations = append(invitations, inv)
	}
	return invitations, nil
}

// DeleteInvitation deletes an invitation
func (r *organizationRepository) DeleteInvitation(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM organization_invitations WHERE id = $1`, id)
	return err
}

// DeleteExpiredInvitations deletes all expired invitations
func (r *organizationRepository) DeleteExpiredInvitations(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `DELETE FROM organization_invitations WHERE expires_at < NOW()`)
	return err
}

// TransferOwnership transfers organization ownership to a new user
func (r *organizationRepository) TransferOwnership(ctx context.Context, orgID, newOwnerUserID uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Get current owner
	var currentOwnerID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT owner_user_id FROM organizations WHERE id = $1`, orgID).Scan(&currentOwnerID)
	if err != nil {
		return err
	}

	// Update organization owner
	_, err = tx.Exec(ctx, `UPDATE organizations SET owner_user_id = $2, updated_at = $3 WHERE id = $1`, orgID, newOwnerUserID, time.Now())
	if err != nil {
		return err
	}

	// Update old owner to admin
	_, err = tx.Exec(ctx, `UPDATE organization_members SET role = 'admin', permissions = $3 WHERE organization_id = $1 AND user_id = $2`,
		orgID, currentOwnerID, models.RolePermissions[models.RoleAdmin])
	if err != nil {
		return err
	}

	// Update new owner to owner
	_, err = tx.Exec(ctx, `UPDATE organization_members SET role = 'owner', permissions = $3 WHERE organization_id = $1 AND user_id = $2`,
		orgID, newOwnerUserID, models.RolePermissions[models.RoleOwner])
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// GetCampaignCounts returns total and active campaign counts for an organization
func (r *organizationRepository) GetCampaignCounts(ctx context.Context, orgID uuid.UUID) (total int, active int, err error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE status = 'active') as active
		FROM campaigns WHERE organization_id = $1
	`
	err = r.db.QueryRow(ctx, query, orgID).Scan(&total, &active)
	return
}

// GetMemberCounts returns the member count for an organization
func (r *organizationRepository) GetMemberCounts(ctx context.Context, orgID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM organization_members WHERE organization_id = $1`, orgID).Scan(&count)
	return count, err
}

// GetEmailAccountCount returns the email account count for an organization
func (r *organizationRepository) GetEmailAccountCount(ctx context.Context, orgID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM email_accounts WHERE organization_id = $1`, orgID).Scan(&count)
	return count, err
}

// GetContactCount returns the contact count for an organization
func (r *organizationRepository) GetContactCount(ctx context.Context, orgID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM contacts WHERE organization_id = $1`, orgID).Scan(&count)
	return count, err
}

// GetEmailsSentTodayCount counts campaign emails sent today by an organization.
func (r *organizationRepository) GetEmailsSentTodayCount(ctx context.Context, orgID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM tasks t
		JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE ea.organization_id = $1
		  AND t.task_type = 'campaign'
		  AND t.status = 'completed'
		  AND t.completed_at >= CURRENT_DATE
	`, orgID).Scan(&count)
	return count, err
}

// GetUserOwnedOrganizationCount returns the number of organizations owned by a user
func (r *organizationRepository) GetUserOwnedOrganizationCount(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM organizations WHERE owner_user_id = $1`, userID).Scan(&count)
	return count, err
}

// CreateEnterpriseInquiry creates a new enterprise inquiry
func (r *organizationRepository) CreateEnterpriseInquiry(ctx context.Context, inquiry *models.EnterpriseInquiry) error {
	query := `
		INSERT INTO enterprise_inquiries (id, company_name, contact_name, contact_email, estimated_volume, team_size, notes, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := r.db.Exec(ctx, query,
		inquiry.ID, inquiry.CompanyName, inquiry.ContactName, inquiry.ContactEmail,
		inquiry.EstimatedVolume, inquiry.TeamSize, inquiry.Notes, inquiry.Status, inquiry.CreatedAt,
	)
	return err
}

// GetEnterpriseInquiry retrieves an enterprise inquiry by ID
func (r *organizationRepository) GetEnterpriseInquiry(ctx context.Context, id uuid.UUID) (*models.EnterpriseInquiry, error) {
	query := `
		SELECT id, company_name, contact_name, contact_email, estimated_volume, team_size, notes, status, created_at, processed_at, processed_by
		FROM enterprise_inquiries WHERE id = $1
	`
	row := r.db.QueryRow(ctx, query, id)
	var inq models.EnterpriseInquiry
	err := row.Scan(
		&inq.ID, &inq.CompanyName, &inq.ContactName, &inq.ContactEmail,
		&inq.EstimatedVolume, &inq.TeamSize, &inq.Notes, &inq.Status,
		&inq.CreatedAt, &inq.ProcessedAt, &inq.ProcessedBy,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &inq, nil
}

// ListEnterpriseInquiries lists enterprise inquiries with optional status filter
func (r *organizationRepository) ListEnterpriseInquiries(ctx context.Context, status string, limit, offset int) ([]models.EnterpriseInquiry, error) {
	query := `
		SELECT id, company_name, contact_name, contact_email, estimated_volume, team_size, notes, status, created_at, processed_at, processed_by
		FROM enterprise_inquiries
		WHERE ($1 = '' OR status = $1)
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Query(ctx, query, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var inquiries []models.EnterpriseInquiry
	for rows.Next() {
		var inq models.EnterpriseInquiry
		err := rows.Scan(
			&inq.ID, &inq.CompanyName, &inq.ContactName, &inq.ContactEmail,
			&inq.EstimatedVolume, &inq.TeamSize, &inq.Notes, &inq.Status,
			&inq.CreatedAt, &inq.ProcessedAt, &inq.ProcessedBy,
		)
		if err != nil {
			return nil, err
		}
		inquiries = append(inquiries, inq)
	}
	return inquiries, nil
}

// UpdateEnterpriseInquiryStatus updates the status of an enterprise inquiry
func (r *organizationRepository) UpdateEnterpriseInquiryStatus(ctx context.Context, id uuid.UUID, status string, processedBy uuid.UUID) error {
	query := `
		UPDATE enterprise_inquiries SET status = $2, processed_at = $3, processed_by = $4
		WHERE id = $1
	`
	_, err := r.db.Exec(ctx, query, id, status, time.Now(), processedBy)
	return err
}

// GetUserAdminPermissions retrieves the admin permissions for a user
func (r *organizationRepository) GetUserAdminPermissions(ctx context.Context, userID uuid.UUID) (uint32, error) {
	var perms uint32
	err := r.db.QueryRow(ctx, `SELECT admin_permissions FROM users WHERE id = $1`, userID).Scan(&perms)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return perms, err
}

// adminOrgListColumns is the projection used by both the list and detail
// queries so the AdminOrgListItem scan stays identical across both.
const adminOrgListColumns = `
	o.id, o.name, o.slug, o.owner_user_id,
	u.email, u.first_name, u.last_name, u.banned_at,
	o.created_at, o.deletion_scheduled_for,
	(SELECT COUNT(*) FROM organization_members om WHERE om.organization_id = o.id) AS member_count,
	(SELECT COUNT(*) FROM email_accounts ea WHERE ea.organization_id = o.id) AS email_account_count,
	(SELECT COUNT(*) FROM campaigns c WHERE c.organization_id = o.id) AS campaign_count,
	(SELECT COUNT(*) FROM campaigns c WHERE c.organization_id = o.id AND c.status = 'active') AS active_campaigns`

// SearchOrganizationsForAdmin lists orgs for the admin panel with cursor
// pagination. The cursor is the last seen org id; rows are returned in
// descending created_at order by default. Counts are computed inline so
// the table can render usage without an extra fetch per row.
func (r *organizationRepository) SearchOrganizationsForAdmin(ctx context.Context, search *models.AdminOrgSearch) (*models.AdminOrgsResult, error) {
	limit := search.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{}
	argNum := 1
	where := "WHERE 1=1"

	if search.Query != "" {
		where += ` AND (o.name ILIKE $` + itoa(argNum) +
			` OR o.slug ILIKE $` + itoa(argNum) +
			` OR u.email ILIKE $` + itoa(argNum) + `)`
		args = append(args, "%"+search.Query+"%")
		argNum++
	}

	switch search.Status {
	case "pending_deletion":
		where += ` AND o.deletion_scheduled_for IS NOT NULL`
	case "active":
		where += ` AND o.deletion_scheduled_for IS NULL`
	}

	if search.PlanID != nil {
		where += ` AND s.plan_id = $` + itoa(argNum)
		args = append(args, *search.PlanID)
		argNum++
	}
	switch search.PlanVisibility {
	case "public":
		where += ` AND p.public = TRUE`
	case "private":
		where += ` AND p.public = FALSE`
	case "none":
		where += ` AND s.id IS NULL`
	}
	if search.Enterprise {
		where += ` AND s.is_enterprise = TRUE`
	}
	if search.CreatedWithin > 0 {
		where += ` AND o.created_at >= NOW() - ($` + itoa(argNum) + `::int * INTERVAL '1 day')`
		args = append(args, search.CreatedWithin)
		argNum++
	}
	if search.HasOverrides {
		where += ` AND EXISTS (SELECT 1 FROM organization_limit_overrides olo WHERE olo.organization_id = o.id)`
	}

	// Local helpers, same style as the rest of this builder.
	addInt := func(frag string, v *int) {
		if v != nil {
			where += " AND " + fmt.Sprintf(frag, argNum)
			args = append(args, *v)
			argNum++
		}
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
			where += " AND " + col + " < ($" + itoa(argNum) + " + INTERVAL '1 day')"
			args = append(args, *v)
			argNum++
		}
	}

	// Subscription state
	if search.SubscriptionStatus != "" {
		where += ` AND s.status::text = $` + itoa(argNum)
		args = append(args, search.SubscriptionStatus)
		argNum++
	}
	if search.CancelAtPeriodEnd {
		where += ` AND s.cancel_at_period_end = TRUE`
	}
	if search.HasActiveSubscription {
		where += ` AND EXISTS (SELECT 1 FROM subscriptions s2 WHERE s2.organization_id = o.id AND s2.status IN ('active','trialing'))`
	}
	if search.NoSubscription {
		where += ` AND NOT EXISTS (SELECT 1 FROM subscriptions s2 WHERE s2.organization_id = o.id)`
	}
	if search.OwnerBanned {
		where += ` AND u.banned_at IS NOT NULL`
	}

	// Relationship existence
	if search.HasActiveCampaigns {
		where += ` AND EXISTS (SELECT 1 FROM campaigns c WHERE c.organization_id = o.id AND c.status = 'active')`
	}
	if search.HasEmailAccounts {
		where += ` AND EXISTS (SELECT 1 FROM email_accounts ea WHERE ea.organization_id = o.id)`
	}

	// Count ranges
	addInt(`(SELECT COUNT(*) FROM organization_members om WHERE om.organization_id = o.id) >= $%d`, search.MemberCountMin)
	addInt(`(SELECT COUNT(*) FROM organization_members om WHERE om.organization_id = o.id) <= $%d`, search.MemberCountMax)
	addInt(`(SELECT COUNT(*) FROM email_accounts ea WHERE ea.organization_id = o.id) >= $%d`, search.EmailAccountCountMin)
	addInt(`(SELECT COUNT(*) FROM email_accounts ea WHERE ea.organization_id = o.id) <= $%d`, search.EmailAccountCountMax)
	addInt(`(SELECT COUNT(*) FROM campaigns c WHERE c.organization_id = o.id) >= $%d`, search.CampaignCountMin)
	addInt(`(SELECT COUNT(*) FROM campaigns c WHERE c.organization_id = o.id) <= $%d`, search.CampaignCountMax)

	// Date ranges
	addAfter("o.created_at", search.CreatedAfter)
	addBefore("o.created_at", search.CreatedBefore)
	addAfter("s.trial_end", search.TrialEndAfter)
	addBefore("s.trial_end", search.TrialEndBefore)
	addAfter("s.current_period_end", search.CurrentPeriodEndAfter)
	addBefore("s.current_period_end", search.CurrentPeriodEndBefore)
	addAfter("o.updated_at", search.UpdatedAfter)
	addBefore("o.updated_at", search.UpdatedBefore)

	if search.Cursor != nil {
		where += ` AND o.id < $` + itoa(argNum)
		args = append(args, *search.Cursor)
		argNum++
	}

	orderCol := "o.created_at"
	switch search.SortBy {
	case "name":
		orderCol = "o.name"
	case "owner_email":
		orderCol = "u.email"
	case "member_count":
		orderCol = "(SELECT COUNT(*) FROM organization_members om WHERE om.organization_id = o.id)"
	case "email_account_count":
		orderCol = "(SELECT COUNT(*) FROM email_accounts ea WHERE ea.organization_id = o.id)"
	case "campaign_count":
		orderCol = "(SELECT COUNT(*) FROM campaigns c WHERE c.organization_id = o.id)"
	}
	orderDir := "DESC"
	if search.SortBy != "" && !search.SortDesc {
		orderDir = "ASC"
	}
	orderBy := "ORDER BY " + orderCol + " " + orderDir

	args = append(args, limit+1)

	query := `
		SELECT ` + adminOrgListColumns + `,
			p.name, p.public, COALESCE(s.is_enterprise, FALSE)
		FROM organizations o
		JOIN users u ON u.id = o.owner_user_id
		LEFT JOIN subscriptions s ON s.organization_id = o.id
		LEFT JOIN plans p ON p.id = s.plan_id
		` + where + `
		` + orderBy + `
		LIMIT $` + itoa(argNum)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []models.AdminOrgListItem{}
	for rows.Next() {
		var item models.AdminOrgListItem
		var planName *string
		var planPublic *bool
		var isEnterprise bool
		if err := rows.Scan(
			&item.ID, &item.Name, &item.Slug, &item.OwnerUserID,
			&item.OwnerEmail, &item.OwnerFirstName, &item.OwnerLastName, &item.OwnerBannedAt,
			&item.CreatedAt, &item.DeletionScheduledFor,
			&item.MemberCount, &item.EmailAccountCount, &item.CampaignCount, &item.ActiveCampaigns,
			&planName, &planPublic, &isEnterprise,
		); err != nil {
			return nil, err
		}
		item.PlanName = planName
		item.PlanPublic = planPublic
		item.IsEnterprise = isEnterprise
		items = append(items, item)
	}

	result := &models.AdminOrgsResult{
		Data:       items,
		Pagination: models.Pagination{HasMore: len(items) > limit},
	}
	if len(items) > limit {
		result.Data = items[:limit]
		last := items[limit-1].ID
		result.Pagination.NextCursor = &last
	}

	// Total count for the same filter — drop the trailing LIMIT arg.
	countQuery := `SELECT COUNT(*) FROM organizations o JOIN users u ON u.id = o.owner_user_id LEFT JOIN subscriptions s ON s.organization_id = o.id LEFT JOIN plans p ON p.id = s.plan_id ` + where
	var total int64
	if err := r.db.QueryRow(ctx, countQuery, args[:len(args)-1]...).Scan(&total); err == nil {
		result.Pagination.Total = &total
	}

	return result, nil
}

// GetOrganizationAdminDetail returns the per-org detail payload. Plan
// and subscription fields are LEFT JOINed so an org without an active
// subscription still resolves.
func (r *organizationRepository) GetOrganizationAdminDetail(ctx context.Context, orgID uuid.UUID) (*models.AdminOrgDetail, error) {
	query := `
		SELECT ` + adminOrgListColumns + `,
			o.updated_at, o.deletion_scheduled_at,
			p.name, s.status::text, s.is_enterprise, s.current_period_end, s.trial_end
		FROM organizations o
		JOIN users u ON u.id = o.owner_user_id
		LEFT JOIN subscriptions s ON s.organization_id = o.id
		LEFT JOIN plans p ON p.id = s.plan_id
		WHERE o.id = $1`

	var detail models.AdminOrgDetail
	var isEnterprise *bool
	err := r.db.QueryRow(ctx, query, orgID).Scan(
		&detail.ID, &detail.Name, &detail.Slug, &detail.OwnerUserID,
		&detail.OwnerEmail, &detail.OwnerFirstName, &detail.OwnerLastName, &detail.OwnerBannedAt,
		&detail.CreatedAt, &detail.DeletionScheduledFor,
		&detail.MemberCount, &detail.EmailAccountCount, &detail.CampaignCount, &detail.ActiveCampaigns,
		&detail.UpdatedAt, &detail.DeletionScheduledAt,
		&detail.PlanName, &detail.SubscriptionStatus, &isEnterprise, &detail.CurrentPeriodEnd, &detail.TrialEnd,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if isEnterprise != nil {
		detail.IsEnterprise = *isEnterprise
	}
	return &detail, nil
}

// GetOrganizationMembersForAdmin returns the members of an org with their
// joined user info, for admin consumption.
func (r *organizationRepository) GetOrganizationMembersForAdmin(ctx context.Context, orgID uuid.UUID) ([]models.AdminOrgMember, error) {
	query := `
		SELECT
			om.id, om.organization_id, om.user_id, om.role, om.permissions,
			om.invited_by, om.invited_at, om.accepted_at,
			u.id, u.first_name, u.last_name, u.email
		FROM organization_members om
		JOIN users u ON u.id = om.user_id
		WHERE om.organization_id = $1
		ORDER BY om.invited_at ASC`

	rows, err := r.db.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := []models.AdminOrgMember{}
	for rows.Next() {
		var m models.AdminOrgMember
		var summary models.AdminUserSummary
		if err := rows.Scan(
			&m.ID, &m.OrganizationID, &m.UserID, &m.Role, &m.Permissions,
			&m.InvitedBy, &m.InvitedAt, &m.AcceptedAt,
			&summary.ID, &summary.FirstName, &summary.LastName, &summary.Email,
		); err != nil {
			return nil, err
		}
		m.User = &summary
		members = append(members, m)
	}
	return members, nil
}

// GetOrganizationLimitOverrides reads the override row for an org.
// Returns (nil, nil) when no row exists — callers should treat that as
// "no overrides set, inherit everything from plan."
func (r *organizationRepository) GetOrganizationLimitOverrides(ctx context.Context, orgID uuid.UUID) (*models.OrganizationLimitOverrides, error) {
	query := `
		SELECT organization_id, max_campaigns, max_active_campaigns, max_team_members,
			max_email_accounts, max_contacts, daily_campaign_limit,
			granted_by, granted_at, updated_at, notes
		FROM organization_limit_overrides
		WHERE organization_id = $1`

	var o models.OrganizationLimitOverrides
	err := r.db.QueryRow(ctx, query, orgID).Scan(
		&o.OrganizationID, &o.MaxCampaigns, &o.MaxActiveCampaigns, &o.MaxTeamMembers,
		&o.MaxEmailAccounts, &o.MaxContacts, &o.DailyCampaignLimit,
		&o.GrantedBy, &o.GrantedAt, &o.UpdatedAt, &o.Notes,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// UpsertOrganizationLimitOverrides applies a partial update. Nil fields
// in the request are left untouched on existing rows; on first-insert
// they default to 0 (= no override) via the table defaults.
//
// granted_by / granted_at are stamped on every write so the audit trail
// reflects the most recent admin who touched any field. updated_at is
// always advanced.
func (r *organizationRepository) UpsertOrganizationLimitOverrides(ctx context.Context, orgID uuid.UUID, req *models.UpdateOrgOverridesRequest, grantedBy uuid.UUID) (*models.OrganizationLimitOverrides, error) {
	maxCampaigns := nullableInt(req.MaxCampaigns)
	maxActive := nullableInt(req.MaxActiveCampaigns)
	maxMembers := nullableInt(req.MaxTeamMembers)
	maxEmails := nullableInt(req.MaxEmailAccounts)
	maxContacts := nullableInt(req.MaxContacts)
	dailyLimit := nullableInt(req.DailyCampaignLimit)
	notes := req.Notes

	query := `
		INSERT INTO organization_limit_overrides (
			organization_id,
			max_campaigns, max_active_campaigns, max_team_members,
			max_email_accounts, max_contacts, daily_campaign_limit,
			granted_by, granted_at, updated_at, notes
		) VALUES (
			$1,
			COALESCE($2, 0), COALESCE($3, 0), COALESCE($4, 0),
			COALESCE($5, 0), COALESCE($6, 0), COALESCE($7, 0),
			$8, NOW(), NOW(), COALESCE($9, '')
		)
		ON CONFLICT (organization_id) DO UPDATE SET
			max_campaigns        = COALESCE($2, organization_limit_overrides.max_campaigns),
			max_active_campaigns = COALESCE($3, organization_limit_overrides.max_active_campaigns),
			max_team_members     = COALESCE($4, organization_limit_overrides.max_team_members),
			max_email_accounts   = COALESCE($5, organization_limit_overrides.max_email_accounts),
			max_contacts         = COALESCE($6, organization_limit_overrides.max_contacts),
			daily_campaign_limit = COALESCE($7, organization_limit_overrides.daily_campaign_limit),
			granted_by = $8,
			granted_at = NOW(),
			updated_at = NOW(),
			notes      = COALESCE($9, organization_limit_overrides.notes)
		RETURNING organization_id, max_campaigns, max_active_campaigns, max_team_members,
			max_email_accounts, max_contacts, daily_campaign_limit,
			granted_by, granted_at, updated_at, notes`

	var o models.OrganizationLimitOverrides
	err := r.db.QueryRow(ctx, query,
		orgID,
		maxCampaigns, maxActive, maxMembers,
		maxEmails, maxContacts, dailyLimit,
		grantedBy, notes,
	).Scan(
		&o.OrganizationID, &o.MaxCampaigns, &o.MaxActiveCampaigns, &o.MaxTeamMembers,
		&o.MaxEmailAccounts, &o.MaxContacts, &o.DailyCampaignLimit,
		&o.GrantedBy, &o.GrantedAt, &o.UpdatedAt, &o.Notes,
	)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func nullableInt(p *int) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

// CreateLimitRequest inserts a new limit-increase request row. The
// (organization_id, field) WHERE status = 'pending' partial unique
// index in migration 000046 makes duplicate-pending rejection a
// constraint violation rather than a service-side query.
func (r *organizationRepository) CreateLimitRequest(ctx context.Context, req *models.LimitIncreaseRequest) error {
	const query = `
		INSERT INTO limit_increase_requests
			(id, organization_id, field, current_effective, requested,
			 reason, status, submitted_by, submitted_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7, NOW())
		RETURNING submitted_at`
	return r.db.QueryRow(ctx, query,
		req.ID, req.OrganizationID, req.Field, req.CurrentEffective, req.Requested,
		req.Reason, req.SubmittedBy,
	).Scan(&req.SubmittedAt)
}

func (r *organizationRepository) GetLimitRequest(ctx context.Context, id uuid.UUID) (*models.LimitIncreaseRequest, error) {
	const query = `
		SELECT id, organization_id, field, current_effective, requested,
			reason, status, submitted_by, submitted_at,
			reviewed_by, reviewed_at, review_notes
		FROM limit_increase_requests
		WHERE id = $1`
	var lr models.LimitIncreaseRequest
	err := r.db.QueryRow(ctx, query, id).Scan(
		&lr.ID, &lr.OrganizationID, &lr.Field, &lr.CurrentEffective, &lr.Requested,
		&lr.Reason, &lr.Status, &lr.SubmittedBy, &lr.SubmittedAt,
		&lr.ReviewedBy, &lr.ReviewedAt, &lr.ReviewNotes,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &lr, nil
}

func (r *organizationRepository) ListLimitRequestsForOrg(ctx context.Context, orgID uuid.UUID) ([]models.LimitIncreaseRequest, error) {
	const query = `
		SELECT id, organization_id, field, current_effective, requested,
			reason, status, submitted_by, submitted_at,
			reviewed_by, reviewed_at, review_notes
		FROM limit_increase_requests
		WHERE organization_id = $1
		ORDER BY submitted_at DESC`
	rows, err := r.db.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.LimitIncreaseRequest{}
	for rows.Next() {
		var lr models.LimitIncreaseRequest
		if err := rows.Scan(
			&lr.ID, &lr.OrganizationID, &lr.Field, &lr.CurrentEffective, &lr.Requested,
			&lr.Reason, &lr.Status, &lr.SubmittedBy, &lr.SubmittedAt,
			&lr.ReviewedBy, &lr.ReviewedAt, &lr.ReviewNotes,
		); err != nil {
			return nil, err
		}
		out = append(out, lr)
	}
	return out, nil
}

// ListLimitRequestsForAdmin joins org + submitter so the admin queue can show
// context per row without an extra fan-out fetch. Faceted + cursor paginated,
// mirroring SearchOrganizationsForAdmin (incremental WHERE builder, id keyset,
// LIMIT+1 has_more, separate COUNT).
func (r *organizationRepository) ListLimitRequestsForAdmin(ctx context.Context, search *models.AdminLimitRequestSearch) (*models.AdminLimitRequestsResult, error) {
	limit := search.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{}
	argNum := 1
	where := "WHERE 1=1"

	if search.Query != "" {
		where += ` AND (o.name ILIKE $` + itoa(argNum) + ` OR o.slug ILIKE $` + itoa(argNum) + ` OR u.email ILIKE $` + itoa(argNum) + ` OR lr.reason ILIKE $` + itoa(argNum) + `)`
		args = append(args, "%"+search.Query+"%")
		argNum++
	}
	if search.Status != "" && search.Status != "all" {
		where += " AND lr.status = $" + itoa(argNum)
		args = append(args, search.Status)
		argNum++
	}
	if search.Field != "" {
		where += " AND lr.field = $" + itoa(argNum)
		args = append(args, search.Field)
		argNum++
	}
	if search.OrgID != nil {
		where += " AND lr.organization_id = $" + itoa(argNum)
		args = append(args, *search.OrgID)
		argNum++
	}
	if search.SubmittedBy != nil {
		where += " AND lr.submitted_by = $" + itoa(argNum)
		args = append(args, *search.SubmittedBy)
		argNum++
	}
	if search.Reviewed {
		where += " AND lr.reviewed_at IS NOT NULL"
	}
	if search.Unreviewed {
		where += " AND lr.reviewed_at IS NULL"
	}

	addInt := func(frag string, v *int) {
		if v != nil {
			where += " AND " + fmt.Sprintf(frag, argNum)
			args = append(args, *v)
			argNum++
		}
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
			where += " AND " + col + " < ($" + itoa(argNum) + " + INTERVAL '1 day')"
			args = append(args, *v)
			argNum++
		}
	}

	addInt(`lr.requested >= $%d`, search.RequestedMin)
	addInt(`lr.requested <= $%d`, search.RequestedMax)
	addInt(`lr.current_effective >= $%d`, search.CurrentEffectiveMin)
	addInt(`lr.current_effective <= $%d`, search.CurrentEffectiveMax)

	if search.SubmittedWithin > 0 {
		where += ` AND lr.submitted_at >= NOW() - ($` + itoa(argNum) + `::int * INTERVAL '1 day')`
		args = append(args, search.SubmittedWithin)
		argNum++
	}
	addAfter("lr.submitted_at", search.SubmittedAfter)
	addBefore("lr.submitted_at", search.SubmittedBefore)
	addAfter("lr.reviewed_at", search.ReviewedAfter)
	addBefore("lr.reviewed_at", search.ReviewedBefore)

	// Keyset on id (mirrors the org explorer; default sort is submitted_at).
	if search.Cursor != nil {
		where += " AND lr.id < $" + itoa(argNum)
		args = append(args, *search.Cursor)
		argNum++
	}

	orderCol := "lr.submitted_at"
	switch search.SortBy {
	case "requested":
		orderCol = "lr.requested"
	case "current_effective":
		orderCol = "lr.current_effective"
	case "reviewed_at":
		orderCol = "lr.reviewed_at"
	case "status":
		orderCol = "lr.status::text"
	case "field":
		orderCol = "lr.field"
	case "org_name":
		orderCol = "o.name"
	}
	orderDir := "DESC"
	if search.SortBy != "" && !search.SortDesc {
		orderDir = "ASC"
	}
	orderBy := "ORDER BY " + orderCol + " " + orderDir + ", lr.id DESC"

	args = append(args, limit+1)

	query := `
		SELECT lr.id, lr.organization_id, lr.field, lr.current_effective, lr.requested,
			lr.reason, lr.status, lr.submitted_by, lr.submitted_at,
			lr.reviewed_by, lr.reviewed_at, lr.review_notes,
			o.id, o.name, o.slug, o.owner_user_id, o.created_at, o.updated_at,
			u.id, u.first_name, u.last_name, u.email, u.created_at, u.updated_at
		FROM limit_increase_requests lr
		JOIN organizations o ON o.id = lr.organization_id
		JOIN users u ON u.id = lr.submitted_by
		` + where + `
		` + orderBy + `
		LIMIT $` + itoa(argNum)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []models.LimitIncreaseRequest{}
	for rows.Next() {
		var lr models.LimitIncreaseRequest
		var org models.Organization
		var user models.User
		if err := rows.Scan(
			&lr.ID, &lr.OrganizationID, &lr.Field, &lr.CurrentEffective, &lr.Requested,
			&lr.Reason, &lr.Status, &lr.SubmittedBy, &lr.SubmittedAt,
			&lr.ReviewedBy, &lr.ReviewedAt, &lr.ReviewNotes,
			&org.ID, &org.Name, &org.Slug, &org.OwnerUserID, &org.CreatedAt, &org.UpdatedAt,
			&user.ID, &user.FirstName, &user.LastName, &user.Email, &user.CreatedAt, &user.UpdatedAt,
		); err != nil {
			return nil, err
		}
		lr.Organization = &org
		lr.SubmittedByUser = &user
		items = append(items, lr)
	}

	result := &models.AdminLimitRequestsResult{
		Data:       items,
		Pagination: models.Pagination{HasMore: len(items) > limit},
	}
	if len(items) > limit {
		result.Data = items[:limit]
		last := items[limit-1].ID
		result.Pagination.NextCursor = &last
	}

	// Total count for the same filter — drop the trailing LIMIT arg.
	countQuery := `SELECT COUNT(*) FROM limit_increase_requests lr JOIN organizations o ON o.id = lr.organization_id JOIN users u ON u.id = lr.submitted_by ` + where
	var total int64
	if err := r.db.QueryRow(ctx, countQuery, args[:len(args)-1]...).Scan(&total); err == nil {
		result.Pagination.Total = &total
	}

	return result, nil
}

func (r *organizationRepository) UpdateLimitRequestStatus(ctx context.Context, id uuid.UUID, status models.LimitRequestStatus, reviewedBy uuid.UUID, notes string) error {
	const query = `
		UPDATE limit_increase_requests
		SET status = $2, reviewed_by = $3, reviewed_at = NOW(), review_notes = $4
		WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id, status, reviewedBy, notes)
	return err
}
