package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// A workspace has exactly one owner, a member, unscoped, moved only by the owner.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveOwnershipTransfer -v
func TestLiveOwnershipTransfer(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 278)
	ctx := context.Background()
	repo := NewOrganizationRepository(pool)

	owner, member, outsider, leaving, banned := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	org := uuid.New()
	admin, manager := uuid.New(), uuid.New()
	folder := uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM organizations WHERE id = $1`, org)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{owner, member, outsider, leaving, banned})
	})
	for _, u := range []uuid.UUID{owner, member, outsider, leaving, banned} {
		exec(`INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Owner', 'Live', $2, 'x')`, u, "own-"+u.String()[:8]+"@test.local")
	}
	exec(`UPDATE users SET deletion_scheduled_at = NOW(), deletion_scheduled_for = NOW() + INTERVAL '7 days' WHERE id = $1`, leaving)
	exec(`UPDATE users SET banned_at = NOW() WHERE id = $1`, banned)
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Ownership', $2, $3)`, org, "own-"+org.String()[:8], owner)
	adminMask := int64(int16(models.GetRolePermissions(models.RoleAdmin)))
	managerMask := int64(int16(models.GetRolePermissions(models.RoleManager)))
	exec(`INSERT INTO organization_roles (id, organization_id, name, permissions) VALUES ($1, $2, 'Admin', $3)`, admin, org, adminMask)
	exec(`INSERT INTO organization_roles (id, organization_id, name, permissions) VALUES ($1, $2, 'ManagerOwn', $3)`, manager, org, managerMask)
	exec(`INSERT INTO organization_members (organization_id, user_id, role, permissions, accepted_at) VALUES ($1, $2, 'owner', -1, NOW())`, org, owner)
	for _, u := range []uuid.UUID{member, leaving, banned} {
		exec(`INSERT INTO organization_members (organization_id, user_id, role, role_id, permissions, accepted_at) VALUES ($1, $2, 'ManagerOwn', $3, $4, NOW())`, org, u, manager, managerMask)
		exec(`INSERT INTO organization_member_roles (organization_id, user_id, role_id) VALUES ($1, $2, $3)`, org, u, manager)
	}
	exec(`INSERT INTO folders (id, organization_id, title, color, position) VALUES ($1, $2, 'Client', '#000000', 0)`, folder, org)
	if err := repo.SetMemberAccess(ctx, org, member, &models.MemberAccess{Scope: models.AccessScopeRestricted, FolderIDs: []uuid.UUID{folder}}, owner); err != nil {
		t.Fatalf("restrict member: %v", err)
	}

	ownerOf := func() uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT owner_user_id FROM organizations WHERE id = $1`, org).Scan(&id); err != nil {
			t.Fatalf("read owner: %v", err)
		}
		return id
	}
	owners := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM organization_members WHERE organization_id = $1 AND role = 'owner'`, org).Scan(&n); err != nil {
			t.Fatalf("count owners: %v", err)
		}
		return n
	}

	for name, tc := range map[string]struct {
		actor, target uuid.UUID
		want          error
	}{
		"a member who is not the owner": {member, member, ErrOwnershipNotHeld},
		"an account outside the roster": {owner, outsider, ErrNewOwnerNotMember},
		"an unknown account":            {owner, uuid.New(), ErrNewOwnerNotMember},
		"an account leaving":            {owner, leaving, ErrNewOwnerUnavailable},
		"a banned account":              {owner, banned, ErrNewOwnerUnavailable},
		"the owner themselves":          {owner, owner, ErrNewOwnerUnavailable},
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			if err := repo.TransferOwnership(ctx, org, tc.actor, tc.target); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if got := ownerOf(); got != owner || owners() != 1 {
				t.Fatalf("owner = %s with %d owner rows, want %s with 1", got, owners(), owner)
			}
		})
	}

	if err := repo.TransferOwnership(ctx, org, owner, member); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if got := ownerOf(); got != member || owners() != 1 {
		t.Fatalf("owner = %s with %d owner rows, want %s with 1", got, owners(), member)
	}

	t.Run("the new owner is unscoped and holds no role", func(t *testing.T) {
		m, err := repo.GetMember(ctx, org, member)
		if err != nil || m == nil {
			t.Fatalf("get member: %v", err)
		}
		if !m.IsOwner() || m.RoleID != nil || m.AccessScope != models.AccessScopeWorkspace {
			t.Fatalf("member = role %q, role_id %v, scope %q", m.Role, m.RoleID, m.AccessScope)
		}
		var roles, grants int
		if err := pool.QueryRow(ctx, `
			SELECT (SELECT COUNT(*) FROM organization_member_roles WHERE organization_id = $1 AND user_id = $2),
			       (SELECT COUNT(*) FROM organization_member_campaign_access WHERE organization_id = $1 AND user_id = $2)`,
			org, member).Scan(&roles, &grants); err != nil {
			t.Fatalf("count: %v", err)
		}
		if roles != 0 || grants != 0 {
			t.Fatalf("roles = %d, grants = %d; want 0/0", roles, grants)
		}
	})

	t.Run("the previous owner is an Admin through a role assignment", func(t *testing.T) {
		m, err := repo.GetMember(ctx, org, owner)
		if err != nil || m == nil {
			t.Fatalf("get member: %v", err)
		}
		if m.IsOwner() || m.RoleID == nil || *m.RoleID != admin || int64(int16(m.Permissions)) != adminMask {
			t.Fatalf("member = role %q, role_id %v, permissions %d", m.Role, m.RoleID, m.Permissions)
		}
		var assigned bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organization_member_roles WHERE organization_id = $1 AND user_id = $2 AND role_id = $3)`,
			org, owner, admin).Scan(&assigned); err != nil || !assigned {
			t.Fatalf("admin assignment = %v, %v", assigned, err)
		}
	})

	t.Run("the previous owner cannot move it again", func(t *testing.T) {
		if err := repo.TransferOwnership(ctx, org, owner, owner); !errors.Is(err, ErrOwnershipNotHeld) {
			t.Fatalf("err = %v, want %v", err, ErrOwnershipNotHeld)
		}
	})
}
