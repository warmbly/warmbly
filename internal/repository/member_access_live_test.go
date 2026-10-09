package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Issue #861: a restricted member reaches only their grants, read-only, whatever writes their row.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveMemberAccess -v
func TestLiveMemberAccessScope(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 278)
	ctx := context.Background()
	repo := NewOrganizationRepository(pool)

	owner, member, invitee := uuid.New(), uuid.New(), uuid.New()
	org, foreignOrg := uuid.New(), uuid.New()
	inFolder, direct, outside, foreignCampaign := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	folder, mailbox, otherMailbox, foreignMailbox := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	role := uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM campaigns WHERE organization_id = ANY($1)`, []uuid.UUID{org, foreignOrg})
		_, _ = pool.Exec(c, `DELETE FROM email_accounts WHERE organization_id = ANY($1)`, []uuid.UUID{org, foreignOrg})
		_, _ = pool.Exec(c, `DELETE FROM organizations WHERE id = ANY($1)`, []uuid.UUID{org, foreignOrg})
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{owner, member, invitee})
	})
	for _, u := range []uuid.UUID{owner, member, invitee} {
		exec(`INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Scope', 'Live', $2, 'x')`, u, "i861-"+u.String()[:8]+"@test.local")
	}
	for _, o := range []uuid.UUID{org, foreignOrg} {
		exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Issue 861', $2, $3)`, o, "i861-"+o.String()[:8], owner)
	}
	exec(`INSERT INTO organization_roles (id, organization_id, name, permissions) VALUES ($1, $2, 'Manager861', $3)`,
		role, org, int64(int16(models.GetRolePermissions(models.RoleManager))))
	exec(`INSERT INTO organization_members (organization_id, user_id, role, permissions, accepted_at) VALUES ($1, $2, 'owner', -1, NOW())`, org, owner)
	exec(`INSERT INTO organization_members (organization_id, user_id, role, role_id, permissions, accepted_at) VALUES ($1, $2, 'Manager861', $3, $4, NOW())`,
		org, member, role, int64(int16(models.GetRolePermissions(models.RoleManager))))
	exec(`INSERT INTO organization_member_roles (organization_id, user_id, role_id) VALUES ($1, $2, $3)`, org, member, role)
	for _, c := range []struct{ id, org uuid.UUID }{{inFolder, org}, {direct, org}, {outside, org}, {foreignCampaign, foreignOrg}} {
		exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, updated_at, created_at) VALUES ($1, $2, $3, 'Scope', '', 62, NOW(), NOW())`, c.id, owner, c.org)
	}
	exec(`INSERT INTO folders (id, organization_id, title, color, position) VALUES ($1, $2, 'Client A', '#000000', 0)`, folder, org)
	exec(`INSERT INTO campaign_folders (campaign_id, folder_id) VALUES ($1, $2)`, inFolder, folder)
	for _, m := range []struct{ id, org uuid.UUID }{{mailbox, org}, {otherMailbox, org}, {foreignMailbox, foreignOrg}} {
		exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider) VALUES ($1, $2, $3, $4, 'Scope', '', '', 'smtp_imap')`,
			m.id, owner, m.org, "i861-"+m.id.String()[:8]+"@test.local")
	}

	restricted := &models.MemberAccess{Scope: models.AccessScopeRestricted, FolderIDs: []uuid.UUID{folder}, CampaignIDs: []uuid.UUID{direct}, EmailAccountIDs: []uuid.UUID{mailbox}}

	t.Run("foreign ids are counted out", func(t *testing.T) {
		f, c, m, err := repo.CountOwnedAccessResources(ctx, org, &models.MemberAccess{
			FolderIDs: []uuid.UUID{folder}, CampaignIDs: []uuid.UUID{direct, foreignCampaign}, EmailAccountIDs: []uuid.UUID{foreignMailbox},
		})
		if err != nil || f != 1 || c != 1 || m != 0 {
			t.Fatalf("owned = %d/%d/%d, %v; want 1/1/0", f, c, m, err)
		}
	})

	t.Run("restricting masks the role to read bits and resolves the grants", func(t *testing.T) {
		if err := repo.SetMemberAccess(ctx, org, member, restricted, owner); err != nil {
			t.Fatal(err)
		}
		m, err := repo.GetMember(ctx, org, member)
		if err != nil || m == nil {
			t.Fatalf("member: %v", err)
		}
		if !m.IsRestricted() || m.Permissions&^models.RestrictedPermissionMask != 0 {
			t.Fatalf("member = scope %q perms %b; want restricted within %b", m.AccessScope, m.Permissions, models.RestrictedPermissionMask)
		}
		scope, err := repo.ResolveMemberScope(ctx, org, member)
		if err != nil {
			t.Fatal(err)
		}
		if !scope.HasCampaign(inFolder) || !scope.HasCampaign(direct) || scope.HasCampaign(outside) || len(scope.Campaigns) != 2 {
			t.Fatalf("campaigns = %v", scope.Campaigns)
		}
		if !scope.HasMailbox(mailbox) || len(scope.Mailboxes) != 1 || !scope.HasFolder(folder) {
			t.Fatalf("scope = %+v", scope)
		}
	})

	t.Run("a role edit cannot widen a restricted member", func(t *testing.T) {
		exec(`UPDATE organization_members SET permissions = -1 WHERE organization_id = $1 AND user_id = $2`, org, member)
		m, _ := repo.GetMember(ctx, org, member)
		if m.Permissions&^models.RestrictedPermissionMask != 0 {
			t.Fatalf("permissions = %b after a direct write", m.Permissions)
		}
	})

	t.Run("folder grants follow the folder's contents", func(t *testing.T) {
		exec(`INSERT INTO campaign_folders (campaign_id, folder_id) VALUES ($1, $2)`, outside, folder)
		scope, _ := repo.ResolveMemberScope(ctx, org, member)
		if !scope.HasCampaign(outside) {
			t.Fatal("a campaign moved into a granted folder is not in scope")
		}
		exec(`DELETE FROM campaign_folders WHERE campaign_id = $1 AND folder_id = $2`, outside, folder)
		scope, _ = repo.ResolveMemberScope(ctx, org, member)
		if scope.HasCampaign(outside) {
			t.Fatal("a campaign moved out of a granted folder stayed in scope")
		}
	})

	t.Run("the scope lists only allowed campaigns", func(t *testing.T) {
		scope, _ := repo.ResolveMemberScope(ctx, org, member)
		res, err := NewCampaignRepostory(handle).Search(ctx, org.String(), "", nil, nil, "", 50, scope.Campaigns)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Data) != 2 || res.Pagination.Total == nil || *res.Pagination.Total != 2 {
			t.Fatalf("search = %d campaigns (total %v), want 2", len(res.Data), res.Pagination.Total)
		}
		none, err := NewCampaignRepostory(handle).Search(ctx, org.String(), "", nil, nil, "", 50, models.NoneMatch())
		if err != nil || len(none.Data) != 0 {
			t.Fatalf("an empty grant listed %d campaigns, %v", len(none.Data), err)
		}
	})

	t.Run("widening restores the role and drops the grants", func(t *testing.T) {
		if err := repo.SetMemberAccess(ctx, org, member, models.WorkspaceAccess(), owner); err != nil {
			t.Fatal(err)
		}
		m, _ := repo.GetMember(ctx, org, member)
		if m.IsRestricted() || m.Permissions != models.GetRolePermissions(models.RoleManager) {
			t.Fatalf("member = scope %q perms %b, want the role back", m.AccessScope, m.Permissions)
		}
		a, _ := repo.GetMemberAccess(ctx, org, member)
		if a.Restricted() || len(a.CampaignIDs)+len(a.FolderIDs)+len(a.EmailAccountIDs) != 0 {
			t.Fatalf("access = %+v after widening", a)
		}
	})

	t.Run("the owner is never restricted", func(t *testing.T) {
		exec(`UPDATE organization_members SET access_scope = 'restricted' WHERE organization_id = $1 AND user_id = $2`, org, owner)
		m, _ := repo.GetMember(ctx, org, owner)
		if m.AccessScope != models.AccessScopeWorkspace {
			t.Fatalf("owner scope = %q", m.AccessScope)
		}
	})

	t.Run("an invitee joins already restricted, without foreign grants", func(t *testing.T) {
		inv := &models.OrganizationInvitation{
			ID: uuid.New(), OrganizationID: org, Email: "i861-" + invitee.String()[:8] + "@test.local", Role: "Manager861", RoleID: &role,
			Permissions: models.GetRolePermissions(models.RoleManager), InvitedBy: owner, Token: uuid.NewString(),
			ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now(),
			Access: &models.MemberAccess{Scope: models.AccessScopeRestricted, EmailAccountIDs: []uuid.UUID{mailbox, foreignMailbox}},
		}
		if err := repo.CreateInvitation(ctx, inv); err != nil {
			t.Fatal(err)
		}
		stored, err := repo.GetInvitationByID(ctx, inv.ID)
		if err != nil || stored == nil || !stored.Access.Restricted() {
			t.Fatalf("stored invitation access = %+v, %v", stored, err)
		}
		now := time.Now()
		joined := &models.OrganizationMember{ID: uuid.New(), OrganizationID: org, UserID: invitee, Role: "Manager861", RoleID: &role,
			Permissions: models.GetRolePermissions(models.RoleManager), InvitedBy: &owner, InvitedAt: now, AcceptedAt: &now, Access: stored.Access}
		if err := repo.AddMemberWithRoles(ctx, joined, []uuid.UUID{role}); err != nil {
			t.Fatal(err)
		}
		m, _ := repo.GetMember(ctx, org, invitee)
		if !m.IsRestricted() || m.Permissions&^models.RestrictedPermissionMask != 0 {
			t.Fatalf("invitee = scope %q perms %b", m.AccessScope, m.Permissions)
		}
		scope, _ := repo.ResolveMemberScope(ctx, org, invitee)
		if len(scope.Mailboxes) != 1 || scope.Mailboxes[0] != mailbox {
			t.Fatalf("invitee mailboxes = %v, want only the workspace's own", scope.Mailboxes)
		}
	})

	t.Run("deleting a granted resource drops its grant", func(t *testing.T) {
		exec(`DELETE FROM email_accounts WHERE id = $1`, mailbox)
		scope, _ := repo.ResolveMemberScope(ctx, org, invitee)
		if len(scope.Mailboxes) != 0 {
			t.Fatalf("mailboxes = %v after deletion", scope.Mailboxes)
		}
	})
}
