package repository

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

func TestLiveInvitedSignup(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 278)
	ctx := context.Background()
	repo := NewOrganizationRepository(pool)
	owner, org, role, secondRole, folder := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	prefix := "i908-" + org.String()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, org)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE email LIKE $1`, prefix+"%@test.local")
	})
	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Owner', '908', $2, 'x')`, owner, prefix+"-owner@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Issue 908', $2, $3)`, org, prefix, owner)
	for i, id := range []uuid.UUID{role, secondRole} {
		exec(`INSERT INTO organization_roles (id, organization_id, name, permissions) VALUES ($1, $2, $3, $4)`, id, org, fmt.Sprintf("Role%d", i), int64(int16(models.GetRolePermissions(models.RoleManager))))
	}
	exec(`INSERT INTO folders (id, organization_id, title, color, position) VALUES ($1, $2, 'Client', '#000000', 0)`, folder, org)

	newInvite := func(t *testing.T) (*models.OrganizationInvitation, *mail.Address) {
		t.Helper()
		token := uuid.NewString()
		email := &mail.Address{Name: "Invitee 908", Address: prefix + "-" + uuid.NewString() + "@test.local"}
		inv := &models.OrganizationInvitation{
			ID: uuid.New(), OrganizationID: org, Email: email.Address, Role: "Role0", RoleID: &role,
			Permissions: models.GetRolePermissions(models.RoleManager), InvitedBy: owner,
			Token: crypt.SHA256(token), ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now(),
			Access: &models.MemberAccess{Scope: models.AccessScopeRestricted, FolderIDs: []uuid.UUID{folder}},
		}
		if err := repo.CreateInvitation(ctx, inv); err != nil {
			t.Fatal(err)
		}
		if err := repo.SetInvitationRoles(ctx, inv.ID, []uuid.UUID{role, secondRole}); err != nil {
			t.Fatal(err)
		}
		return inv, email
	}
	assertUnchanged := func(t *testing.T, inv *models.OrganizationInvitation, email *mail.Address) {
		t.Helper()
		var count int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE email = $1`, email.Address).Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed signup left %d accounts: %v", count, err)
		}
		stored, err := repo.GetInvitationByID(ctx, inv.ID)
		if err != nil || stored == nil {
			t.Fatalf("failed signup consumed invitation: %v", err)
		}
	}
	assertJoined := func(t *testing.T, inv *models.OrganizationInvitation, u *models.User) {
		t.Helper()
		member, err := repo.GetMember(ctx, org, u.ID)
		if err != nil || member == nil || !member.IsRestricted() {
			t.Fatalf("membership, roles or restricted scope missing: %+v %v", member, err)
		}
		roles, err := repo.GetMemberRoles(ctx, org, u.ID)
		if err != nil || len(roles) != 2 {
			t.Fatalf("membership roles = %+v, %v", roles, err)
		}
		access, err := repo.GetMemberAccess(ctx, org, u.ID)
		if err != nil || access == nil || len(access.FolderIDs) != 1 || access.FolderIDs[0] != folder {
			t.Fatalf("resource grant missing: %+v %v", access, err)
		}
		stored, err := repo.GetInvitationByID(ctx, inv.ID)
		if err != nil || stored != nil {
			t.Fatalf("successful signup did not consume invitation: %+v %v", stored, err)
		}
	}

	t.Run("legacy token accepts once with live roles and grants", func(t *testing.T) {
		inv, email := newInvite(t)
		exec(`UPDATE organization_roles SET permissions = $2 WHERE id = ANY($1)`, []uuid.UUID{role, secondRole}, int64(int16(models.GetRolePermissions(models.RoleViewer))))
		u, member, err := repo.CreateInvitedUser(ctx, inv.Token, email, "hash")
		if err != nil || u == nil || member == nil {
			t.Fatalf("signup: %v", err)
		}
		assertJoined(t, inv, u)
		stored, err := repo.GetMember(ctx, org, u.ID)
		if err != nil || stored.Permissions != models.GetRolePermissions(models.RoleViewer)&models.RestrictedPermissionMask {
			t.Fatalf("signup used stale invitation permissions: %+v %v", stored, err)
		}
		_, _, err = repo.CreateInvitedUser(ctx, inv.Token, email, "hash")
		if !errors.Is(err, ErrInvitationInvalid) {
			t.Fatalf("replayed invite: %v", err)
		}
	})

	t.Run("new link token is accepted", func(t *testing.T) {
		inv, email := newInvite(t)
		token := crypt.SHA256(uuid.NewString())
		exec(`UPDATE organization_invitations SET link_token_hash = $2 WHERE id = $1`, inv.ID, token)
		u, _, err := repo.CreateInvitedUser(ctx, token, email, "hash")
		if err != nil || u == nil {
			t.Fatalf("new invitation token: %v", err)
		}
		assertJoined(t, inv, u)
	})

	for _, reason := range []string{"expired", "wrong email", "no roles", "deleted", "duplicate email"} {
		t.Run(reason, func(t *testing.T) {
			inv, email := newInvite(t)
			want := ErrInvitationInvalid
			switch reason {
			case "expired":
				exec(`UPDATE organization_invitations SET expires_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, inv.ID)
			case "wrong email":
				exec(`UPDATE organization_invitations SET email = 'someone-else@test.local' WHERE id = $1`, inv.ID)
			case "no roles":
				exec(`DELETE FROM organization_invitation_roles WHERE invitation_id = $1`, inv.ID)
			case "deleted":
				exec(`DELETE FROM organization_invitations WHERE id = $1`, inv.ID)
			case "duplicate email":
				exec(`INSERT INTO users (first_name, last_name, email, password_hash) VALUES ('Existing', '908', $1, 'x')`, email.Address)
				want = ErrUserEmailTaken
			}
			u, member, err := repo.CreateInvitedUser(ctx, inv.Token, email, "hash")
			if !errors.Is(err, want) || u != nil || member != nil {
				t.Fatalf("invalid invitation provisioned: user=%+v member=%+v error=%v", u, member, err)
			}
			if reason != "deleted" && reason != "duplicate email" {
				assertUnchanged(t, inv, email)
			}
		})
	}

	for _, table := range []string{"organization_members", "organization_member_roles", "organization_member_campaign_access", "organization_invitations"} {
		t.Run("rollback at "+table, func(t *testing.T) {
			inv, email := newInvite(t)
			name := "fail_i908_" + crypt.SHA256(inv.ID.String())[:12]
			operation, row := "INSERT", "NEW"
			if table == "organization_invitations" {
				operation, row = "DELETE", "OLD"
			}
			exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
				IF %s.organization_id = '%s'::uuid THEN RAISE EXCEPTION 'injected invited signup failure'; END IF;
				RETURN %s; END $$`, name, row, org, row))
			exec(fmt.Sprintf(`CREATE TRIGGER %s BEFORE %s ON %s FOR EACH ROW EXECUTE FUNCTION %s()`, name, operation, table, name))
			defer exec(fmt.Sprintf(`DROP FUNCTION %s() CASCADE`, name))
			u, member, err := repo.CreateInvitedUser(ctx, inv.Token, email, "hash")
			if err == nil || errors.Is(err, ErrInvitationInvalid) || u != nil || member != nil {
				t.Fatalf("write failure was not propagated: user=%+v member=%+v error=%v", u, member, err)
			}
			assertUnchanged(t, inv, email)
			exec(fmt.Sprintf(`DROP TRIGGER %s ON %s`, name, table))
			u, _, err = repo.CreateInvitedUser(ctx, inv.Token, email, "hash")
			if err != nil || u == nil {
				t.Fatalf("same address could not retry rolled-back signup: %v", err)
			}
			assertJoined(t, inv, u)
		})
	}

	t.Run("concurrent signup consumes invitation once", func(t *testing.T) {
		inv, email := newInvite(t)
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			wg.Go(func() {
				_, _, err := repo.CreateInvitedUser(ctx, inv.Token, email, "hash")
				results <- err
			})
		}
		wg.Wait()
		close(results)
		successes := 0
		for err := range results {
			if err == nil {
				successes++
			} else if !errors.Is(err, ErrInvitationInvalid) {
				t.Fatalf("concurrent signup: %v", err)
			}
		}
		if successes != 1 {
			t.Fatalf("successful signups = %d, want 1", successes)
		}
	})
}
