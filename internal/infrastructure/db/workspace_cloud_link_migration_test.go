package db

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestLiveWorkspaceCloudLinkMigrationPreservesExistingEnrollments(t *testing.T) {
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	schema := "cloud_links_" + uuid.New().String()[:8]
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") }()
	if _, err := conn.Exec(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	const fixture = `
 CREATE TABLE organizations (id uuid PRIMARY KEY);
 CREATE TABLE pool_link_codes (id uuid PRIMARY KEY);
 CREATE TABLE pool_link_instances (id uuid PRIMARY KEY, revoked_at timestamptz);
 CREATE TABLE cloud_link (id boolean PRIMARY KEY DEFAULT true CHECK (id), instance_id uuid UNIQUE NOT NULL, token text NOT NULL);
 CREATE TABLE cloud_link_mailboxes (email_account_id uuid PRIMARY KEY, remote_id uuid NOT NULL, health_state text);
 CREATE TABLE placement_tests (id uuid PRIMARY KEY, remote_test_id uuid, remote_instance_id uuid);
 CREATE TABLE domain_redirects (id uuid PRIMARY KEY, served_by text, linked_instance_id uuid);
 INSERT INTO organizations VALUES ('10000000-0000-0000-0000-000000000001');
 INSERT INTO cloud_link (instance_id, token) VALUES ('20000000-0000-0000-0000-000000000001', 'sealed-fixture');
 INSERT INTO cloud_link_mailboxes VALUES ('30000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000001', 'quarantined');
 INSERT INTO placement_tests VALUES ('50000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', NULL);
 INSERT INTO domain_redirects VALUES ('70000000-0000-0000-0000-000000000001', 'cloud', NULL);
 `
	if _, err := conn.Exec(ctx, fixture); err != nil {
		t.Fatal(err)
	}
	up, err := migrationsFS.ReadFile("migrations/000266_workspace_cloud_links.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := migrationsFS.ReadFile("migrations/000266_workspace_cloud_links.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	var preserved bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM cloud_link l JOIN cloud_link_mailboxes m ON m.instance_id = l.instance_id
 JOIN placement_tests p ON p.remote_instance_id = l.instance_id
 JOIN domain_redirects d ON d.cloud_link_instance_id = l.instance_id
 WHERE l.organization_id IS NULL AND l.token = 'sealed-fixture' AND m.health_state = 'quarantined'
 )`).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("legacy data not preserved: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO cloud_link (instance_id, token, organization_id)
 VALUES ('80000000-0000-0000-0000-000000000001', 'new-fixture', '10000000-0000-0000-0000-000000000001')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO cloud_link (instance_id, token, organization_id)
 VALUES ('90000000-0000-0000-0000-000000000001', 'duplicate-fixture', '10000000-0000-0000-0000-000000000001')`); err == nil {
		t.Fatal("workspace accepted two links")
	}
	if _, err := conn.Exec(ctx, string(down)); err == nil {
		t.Fatal("unsafe downgrade was permitted")
	}
	if _, err := conn.Exec(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "DELETE FROM cloud_link WHERE organization_id IS NOT NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	var token, state string
	if err := conn.QueryRow(ctx, "SELECT token FROM cloud_link WHERE id = true").Scan(&token); err != nil || token != "sealed-fixture" {
		t.Fatalf("downgrade lost token: %v", err)
	}
	if err := conn.QueryRow(ctx, "SELECT health_state FROM cloud_link_mailboxes").Scan(&state); err != nil || state != "quarantined" {
		t.Fatalf("downgrade lost standing: %v", err)
	}
	if _, err := conn.Exec(ctx, "DELETE FROM cloud_link"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(up)); err == nil {
		t.Fatal("upgrade discarded orphaned enrollments")
	}
	if _, err := conn.Exec(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, "SELECT health_state FROM cloud_link_mailboxes").Scan(&state); err != nil || state != "quarantined" {
		t.Fatalf("failed upgrade lost legacy enrollment: %v", err)
	}
}
