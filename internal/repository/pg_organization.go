package repository

import (
	"context"
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
	_, err := r.db.Exec(ctx, query,
		org.ID, org.Name, org.Slug, org.OwnerUserID, now, now,
	)
	return err
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
