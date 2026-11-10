package repository

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// The portal zone reads verified agency portal domains only and keeps its own rows, so the tracking zone's are never touched.
func TestLiveEdgeHostnameZonesStaySeparate(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 378)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	tag := uuid.New().String()[:8]
	owner, org, agency, unverified := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	forms, portal, pending := "forms-"+tag+".acme.test", "portal-"+tag+".agency.test", "pending-"+tag+".agency.test"
	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Edge', 'Live', $2, 'x')`, owner, "edge-"+tag+"@test.local")
	exec(`INSERT INTO organizations (id, name, owner_user_id, forms_domain, forms_domain_verified) VALUES ($1, 'Edge Live', $2, $3, true)`, org, owner, forms)
	exec(`INSERT INTO agencies (id, name, owner_user_id) VALUES ($1, 'Edge agency', $2), ($3, 'Pending agency', $2)`, agency, owner, unverified)
	exec(`INSERT INTO agency_brands (agency_id, portal_domain, portal_domain_verified) VALUES ($1, $2, true)`, agency, portal)
	exec(`INSERT INTO agency_brands (agency_id, portal_domain) VALUES ($1, $2)`, unverified, pending)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM portal_edge_hostnames WHERE hostname = ANY($1)`, []string{forms, portal})
		_, _ = pool.Exec(ctx, `DELETE FROM agencies WHERE owner_user_id = $1`, owner)
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, org)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, owner)
	})

	portalRepo, trackingRepo := NewEdgeHostnameRepository(pool, EdgePortal), NewEdgeHostnameRepository(pool, EdgeTracking)
	wanted, err := portalRepo.Wanted(ctx)
	if err != nil || slices.Contains(wanted, forms) || !slices.Contains(wanted, portal) || slices.Contains(wanted, pending) {
		t.Fatalf("portal wanted = %v, %v", wanted, err)
	}
	if tw, err := trackingRepo.Wanted(ctx); err != nil || slices.Contains(tw, forms) || slices.Contains(tw, portal) {
		t.Fatalf("tracking zone wants a portal domain: %v", err)
	}

	if err := portalRepo.Save(ctx, EdgeHostname{Hostname: portal, Zone: "zone-p", Status: "pending", OwnershipName: "_cf-custom-hostname." + portal, OwnershipValue: "v"}); err != nil {
		t.Fatal(err)
	}
	if err := portalRepo.Failed(ctx, forms, "zone-p", "down"); err != nil {
		t.Fatal(err)
	}
	if rows, _ := trackingRepo.Get(ctx, []string{portal, forms}); len(rows) != 0 {
		t.Fatalf("a portal row landed in the tracking table: %+v", rows)
	}
	rows, err := portalRepo.Get(ctx, []string{portal, forms})
	if err != nil || len(rows) != 2 {
		t.Fatalf("portal rows = %+v, %v", rows, err)
	}
	if err := portalRepo.MarkUnwanted(ctx, []string{portal}); err != nil {
		t.Fatal(err)
	}
	if gone, err := portalRepo.DeleteUnwanted(ctx, portal); err != nil || !gone {
		t.Fatalf("delete = %v, %v", gone, err)
	}
	if gone, _ := portalRepo.DeleteUnwanted(ctx, forms); gone {
		t.Fatal("a name with no removal clock was deleted")
	}
}
