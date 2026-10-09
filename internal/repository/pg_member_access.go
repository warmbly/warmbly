package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
)

// memberScope is the scope a new membership row is written with.
func memberScope(m *models.OrganizationMember) models.AccessScope {
	if m.AccessScope.Valid() {
		return m.AccessScope
	}
	return models.AccessScopeWorkspace
}

func textUUIDs(raw []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(raw))
	for _, v := range raw {
		if id, err := uuid.Parse(v); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// invitationAccessCols reads an invitation's scope; the table is aliased i.
const invitationAccessCols = `i.access_scope, i.access_folder_ids::text[], i.access_campaign_ids::text[], i.access_email_account_ids::text[]`

type invitationAccess struct {
	scope                         models.AccessScope
	folders, campaigns, mailboxes []string
}

func (a invitationAccess) access() *models.MemberAccess {
	if a.scope != models.AccessScopeRestricted {
		return models.WorkspaceAccess()
	}
	return &models.MemberAccess{Scope: a.scope, FolderIDs: textUUIDs(a.folders), CampaignIDs: textUUIDs(a.campaigns), EmailAccountIDs: textUUIDs(a.mailboxes)}
}

// memberAccessSelect lists each member's scope and grants; $1 is the organization.
const memberAccessSelect = `
	SELECT om.user_id, om.access_scope,
		COALESCE((SELECT array_agg(a.folder_id::text ORDER BY a.created_at) FROM organization_member_campaign_access a
			WHERE a.organization_id = om.organization_id AND a.user_id = om.user_id AND a.folder_id IS NOT NULL), '{}'),
		COALESCE((SELECT array_agg(a.campaign_id::text ORDER BY a.created_at) FROM organization_member_campaign_access a
			WHERE a.organization_id = om.organization_id AND a.user_id = om.user_id AND a.campaign_id IS NOT NULL), '{}'),
		COALESCE((SELECT array_agg(a.email_account_id::text ORDER BY a.created_at) FROM organization_member_mailbox_access a
			WHERE a.organization_id = om.organization_id AND a.user_id = om.user_id), '{}')
	FROM organization_members om
	WHERE om.organization_id = $1`

func scanMemberAccess(row pgx.Row) (uuid.UUID, *models.MemberAccess, error) {
	var userID uuid.UUID
	var scope models.AccessScope
	var folders, campaigns, mailboxes []string
	if err := row.Scan(&userID, &scope, &folders, &campaigns, &mailboxes); err != nil {
		return uuid.Nil, nil, err
	}
	a := &models.MemberAccess{Scope: scope, FolderIDs: textUUIDs(folders), CampaignIDs: textUUIDs(campaigns), EmailAccountIDs: textUUIDs(mailboxes)}
	if !a.Restricted() {
		a = models.WorkspaceAccess()
	}
	return userID, a, nil
}

// GetMemberAccess is one member's scope and grants, or nil when they are not a member.
func (r *organizationRepository) GetMemberAccess(ctx context.Context, orgID, userID uuid.UUID) (*models.MemberAccess, error) {
	_, a, err := scanMemberAccess(r.db.QueryRow(ctx, memberAccessSelect+` AND om.user_id = $2`, orgID, userID))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return a, err
}

// ListMemberAccess is every member's scope and grants, by user id.
func (r *organizationRepository) ListMemberAccess(ctx context.Context, orgID uuid.UUID) (map[uuid.UUID]*models.MemberAccess, error) {
	rows, err := r.db.Query(ctx, memberAccessSelect, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]*models.MemberAccess{}
	for rows.Next() {
		userID, a, err := scanMemberAccess(rows)
		if err != nil {
			return nil, err
		}
		out[userID] = a
	}
	return out, rows.Err()
}

// ResolveMemberScope is what a restricted member reaches right now: folder grants follow the folder's current contents.
func (r *organizationRepository) ResolveMemberScope(ctx context.Context, orgID, userID uuid.UUID) (*models.ResourceScope, error) {
	var campaigns, folders, mailboxes, tags []string
	err := r.db.QueryRow(ctx, `
		WITH granted_folders AS (
			SELECT f.id FROM folders f
			JOIN organization_member_campaign_access a ON a.folder_id = f.id
			WHERE f.organization_id = $1 AND a.organization_id = $1 AND a.user_id = $2
		), in_scope AS (
			SELECT c.id FROM campaigns c
			WHERE c.organization_id = $1 AND (
				c.id IN (SELECT a.campaign_id FROM organization_member_campaign_access a
					WHERE a.organization_id = $1 AND a.user_id = $2 AND a.campaign_id IS NOT NULL)
				OR c.id IN (SELECT cf.campaign_id FROM campaign_folders cf WHERE cf.folder_id IN (SELECT id FROM granted_folders)))
		), boxes AS (
			SELECT ea.id FROM email_accounts ea
			JOIN organization_member_mailbox_access a ON a.email_account_id = ea.id
			WHERE ea.organization_id = $1 AND a.organization_id = $1 AND a.user_id = $2
		)
		SELECT
			COALESCE((SELECT array_agg(id::text) FROM in_scope), '{}'),
			COALESCE((SELECT array_agg(DISTINCT id::text) FROM (
				SELECT id FROM granted_folders
				UNION SELECT cf.folder_id FROM campaign_folders cf WHERE cf.campaign_id IN (SELECT id FROM in_scope)) v), '{}'),
			COALESCE((SELECT array_agg(id::text) FROM boxes), '{}'),
			COALESCE((SELECT array_agg(DISTINCT et.tag_id::text) FROM email_tags et WHERE et.email_id IN (SELECT id FROM boxes)), '{}')
	`, orgID, userID).Scan(&campaigns, &folders, &mailboxes, &tags)
	if err != nil {
		return nil, err
	}
	return &models.ResourceScope{Campaigns: textUUIDs(campaigns), Folders: textUUIDs(folders), Mailboxes: textUUIDs(mailboxes), MailboxTags: textUUIDs(tags)}, nil
}

// CountOwnedAccessResources counts how many of the ids belong to the organization, per kind.
func (r *organizationRepository) CountOwnedAccessResources(ctx context.Context, orgID uuid.UUID, a *models.MemberAccess) (folders, campaigns, mailboxes int, err error) {
	err = r.db.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM folders WHERE organization_id = $1 AND id = ANY($2::uuid[])),
			(SELECT COUNT(*) FROM campaigns WHERE organization_id = $1 AND id = ANY($3::uuid[])),
			(SELECT COUNT(*) FROM email_accounts WHERE organization_id = $1 AND id = ANY($4::uuid[]))
	`, orgID, nonNilUUIDs(a.FolderIDs), nonNilUUIDs(a.CampaignIDs), nonNilUUIDs(a.EmailAccountIDs)).Scan(&folders, &campaigns, &mailboxes)
	return folders, campaigns, mailboxes, err
}

// SetMemberAccess replaces a member's scope and grants in one transaction and recomputes their permissions.
func (r *organizationRepository) SetMemberAccess(ctx context.Context, orgID, userID uuid.UUID, a *models.MemberAccess, grantedBy uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tag, err := tx.Exec(ctx, `UPDATE organization_members SET access_scope = $3
		WHERE organization_id = $1 AND user_id = $2 AND role <> 'owner'`, orgID, userID, a.Scope)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if err := writeMemberGrants(ctx, tx, orgID, userID, a, grantedBy); err != nil {
		return err
	}
	if err := recomputeMemberPermissions(ctx, tx, orgID, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// writeMemberGrants replaces a member's grants, keeping only resources the organization owns.
func writeMemberGrants(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID, a *models.MemberAccess, grantedBy uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM organization_member_campaign_access WHERE organization_id = $1 AND user_id = $2`, orgID, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM organization_member_mailbox_access WHERE organization_id = $1 AND user_id = $2`, orgID, userID); err != nil {
		return err
	}
	if !a.Restricted() {
		return nil
	}
	by := &grantedBy
	if grantedBy == uuid.Nil {
		by = nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO organization_member_campaign_access (organization_id, user_id, folder_id, granted_by)
		SELECT $1, $2, f.id, $4 FROM folders f WHERE f.organization_id = $1 AND f.id = ANY($3::uuid[])
		ON CONFLICT DO NOTHING`, orgID, userID, nonNilUUIDs(a.FolderIDs), by); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO organization_member_campaign_access (organization_id, user_id, campaign_id, granted_by)
		SELECT $1, $2, c.id, $4 FROM campaigns c WHERE c.organization_id = $1 AND c.id = ANY($3::uuid[])
		ON CONFLICT DO NOTHING`, orgID, userID, nonNilUUIDs(a.CampaignIDs), by); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO organization_member_mailbox_access (organization_id, user_id, email_account_id, granted_by)
		SELECT $1, $2, ea.id, $4 FROM email_accounts ea WHERE ea.organization_id = $1 AND ea.id = ANY($3::uuid[])
		ON CONFLICT DO NOTHING`, orgID, userID, nonNilUUIDs(a.EmailAccountIDs), by)
	return err
}

// SuggestCampaignSenders lists the mailboxes the given campaigns, and the campaigns in the given folders, send from.
func (r *organizationRepository) SuggestCampaignSenders(ctx context.Context, orgID uuid.UUID, campaignIDs, folderIDs []uuid.UUID) ([]models.SuggestedSender, error) {
	rows, err := r.db.Query(ctx, `
		WITH picked AS (
			SELECT c.id FROM campaigns c
			WHERE c.organization_id = $1 AND (c.id = ANY($2::uuid[])
				OR c.id IN (SELECT cf.campaign_id FROM campaign_folders cf
					JOIN folders f ON f.id = cf.folder_id
					WHERE f.organization_id = $1 AND f.id = ANY($3::uuid[])))
		), senders AS (
			SELECT cs.email_account_id, cs.campaign_id FROM campaign_senders cs
			WHERE cs.campaign_id IN (SELECT id FROM picked) AND cs.enabled
			UNION
			SELECT et.email_id, cet.campaign_id FROM campaign_email_tags cet
			JOIN email_tags et ON et.tag_id = cet.tag_id
			WHERE cet.campaign_id IN (SELECT id FROM picked)
		)
		SELECT ea.id, ea.email, COALESCE(ea.name, ''), array_agg(DISTINCT s.campaign_id::text)
		FROM senders s
		JOIN email_accounts ea ON ea.id = s.email_account_id AND ea.organization_id = $1
		GROUP BY ea.id, ea.email, ea.name
		ORDER BY ea.email
	`, orgID, nonNilUUIDs(campaignIDs), nonNilUUIDs(folderIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.SuggestedSender{}
	for rows.Next() {
		var s models.SuggestedSender
		var campaigns []string
		if err := rows.Scan(&s.ID, &s.Email, &s.Name, &campaigns); err != nil {
			return nil, err
		}
		s.CampaignIDs = textUUIDs(campaigns)
		out = append(out, s)
	}
	return out, rows.Err()
}
