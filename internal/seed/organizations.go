package seed

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

func seedOrganizations(ctx context.Context, pool *pgxpool.Pool, r *Result) error {
	type org struct {
		id    uuid.UUID
		name  string
		slug  string
		owner uuid.UUID
		plan  string
	}
	// Slugs are namespaced "warmbly-..." to avoid colliding with main's
	// seedRich, which uses bare "acme"/"beta"/"gamma".
	orgs := []org{
		{OrgAcmeID, "Warmbly Pro Demo", "warmbly-pro-demo", UserOwnerID, "pro"},
		{OrgGlobexID, "Warmbly Trial Demo", "warmbly-trial-demo", UserFounderID, "free-trial"},
	}

	for _, o := range orgs {
		_, err := pool.Exec(ctx, `
			INSERT INTO organizations (id, name, slug, owner_user_id, created_at, updated_at)
			VALUES ($1,$2,$3,$4,NOW(),NOW())
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				slug = EXCLUDED.slug,
				owner_user_id = EXCLUDED.owner_user_id,
				updated_at = NOW()
		`, o.id, o.name, o.slug, o.owner)
		if err != nil {
			return err
		}
		r.Organizations = append(r.Organizations, SeededOrg{
			Name: o.name, Slug: o.slug, ID: o.id.String(), Plan: o.plan,
		})
	}

	// Memberships. The (organization_id, user_id) unique constraint lets us
	// upsert by it.
	type membership struct {
		mid    uuid.UUID
		orgID  uuid.UUID
		userID uuid.UUID
		role   models.Role
	}
	members := []membership{
		{uuid.MustParse("00000000-0000-0000-0000-000000000411"), OrgAcmeID, UserOwnerID, models.RoleOwner},
		{uuid.MustParse("00000000-0000-0000-0000-000000000412"), OrgAcmeID, UserManagerID, models.RoleManager},
		{uuid.MustParse("00000000-0000-0000-0000-000000000413"), OrgAcmeID, UserViewerID, models.RoleViewer},
		{uuid.MustParse("00000000-0000-0000-0000-000000000414"), OrgAcmeID, UserAdminID, models.RoleAdmin},
		{uuid.MustParse("00000000-0000-0000-0000-000000000415"), OrgGlobexID, UserFounderID, models.RoleOwner},
	}
	for _, m := range members {
		// permissions handles its own int16<->uint16 reinterpretation via
		// driver.Valuer (see models.OrganizationPermission.Value), so pass
		// the typed value through directly.
		_, err := pool.Exec(ctx, `
			INSERT INTO organization_members (id, organization_id, user_id, role, permissions, invited_at, accepted_at)
			VALUES ($1,$2,$3,$4,$5,NOW(),NOW())
			ON CONFLICT (organization_id, user_id) DO UPDATE SET
				role = EXCLUDED.role,
				permissions = EXCLUDED.permissions,
				accepted_at = COALESCE(organization_members.accepted_at, NOW())
		`, m.mid, m.orgID, m.userID, string(m.role), models.RolePermissions[m.role])
		if err != nil {
			return err
		}
	}

	// A pending invitation makes the invite-flow easy to demo.
	inviteID := uuid.MustParse("00000000-0000-0000-0000-000000000421")
	_, err := pool.Exec(ctx, `
		INSERT INTO organization_invitations (id, organization_id, email, role, permissions, invited_by, token, expires_at, created_at)
		VALUES ($1,$2,$3,'manager',$4,$5,$6, NOW() + INTERVAL '7 days', NOW())
		ON CONFLICT (organization_id, email) DO UPDATE SET
			token = EXCLUDED.token,
			expires_at = EXCLUDED.expires_at
	`, inviteID, OrgAcmeID, "pending-invite@warmbly.local",
		models.RolePermissions[models.RoleManager],
		UserOwnerID, "seed-invite-token-acme-pending-0001")
	return err
}
