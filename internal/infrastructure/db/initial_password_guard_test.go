package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// An issued password lives at most 48 hours from the write that issued it,
// whichever path writes the row, and is never made to last longer.
func TestInitialPasswordGuardCapsAndRefusesExtension(t *testing.T) {
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
	schema := "initial_pw_" + uuid.New().String()[:8]
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") }()
	if _, err := conn.Exec(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `CREATE TABLE users (id text PRIMARY KEY, password_hash text, password_expires_at timestamptz, updated_at timestamptz NOT NULL DEFAULT now());
		INSERT INTO users (id, password_hash, password_expires_at) VALUES
			('legacy', 'h0', now() + interval '30 days'),
			('legacy_short', 'h0', now() + interval '6 hours'),
			('legacy_ended', 'h0', now() - interval '1 day'),
			('legacy_chosen', 'h0', NULL)`); err != nil {
		t.Fatal(err)
	}
	up, err := migrationsFS.ReadFile("migrations/000336_initial_password_guard.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := conn.Exec(ctx, string(up)); err != nil {
			t.Fatal(err)
		}
	}

	exec := func(q string, args ...any) error {
		_, err := conn.Exec(ctx, q, args...)
		return err
	}
	left := func(id string) *time.Duration {
		t.Helper()
		var at *time.Time
		if err := conn.QueryRow(ctx, `SELECT password_expires_at FROM users WHERE id = $1`, id).Scan(&at); err != nil {
			t.Fatal(err)
		}
		if at == nil {
			return nil
		}
		d := time.Until(*at)
		return &d
	}
	capped := func(id string) {
		t.Helper()
		if d := left(id); d == nil || *d > 48*time.Hour || *d < 47*time.Hour {
			t.Fatalf("%s: %v left, want just under 48 hours", id, d)
		}
	}
	refused := func(what string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "cannot be extended") {
			t.Fatalf("%s: got %v, want a refusal", what, err)
		}
	}

	capped("legacy")
	if d := left("legacy_short"); d == nil || *d > 6*time.Hour || *d < 5*time.Hour {
		t.Fatalf("a short expiry moved to %v", d)
	}
	if d := left("legacy_ended"); d == nil || *d > -23*time.Hour {
		t.Fatalf("an ended password moved to %v", d)
	}
	if left("legacy_chosen") != nil {
		t.Fatal("the migration gave a chosen password an expiry")
	}

	if err := exec(`INSERT INTO users VALUES ('issued', 'h1', now() + interval '30 days'), ('member', 'h2', NULL)`); err != nil {
		t.Fatal(err)
	}
	capped("issued")
	if left("member") != nil {
		t.Fatal("a chosen password gained an expiry")
	}

	refused("extending", exec(`UPDATE users SET password_expires_at = password_expires_at + interval '1 minute' WHERE id = 'issued'`))
	refused("clearing without a new password", exec(`UPDATE users SET password_expires_at = NULL WHERE id = 'issued'`))
	refused("extending with the same hash", exec(`UPDATE users SET password_hash = 'h1', password_expires_at = now() + interval '30 days' WHERE id = 'issued'`))

	if err := exec(`UPDATE users SET password_expires_at = now() + interval '1 hour' WHERE id = 'issued'`); err != nil {
		t.Fatalf("shortening was refused: %v", err)
	}
	if d := left("issued"); d == nil || *d > time.Hour {
		t.Fatalf("shortened to %v", d)
	}
	refused("extending a shortened one", exec(`UPDATE users SET password_expires_at = now() + interval '2 hours' WHERE id = 'issued'`))

	if err := exec(`UPDATE users SET password_hash = 'h3', password_expires_at = now() + interval '30 days' WHERE id = 'issued'`); err != nil {
		t.Fatalf("a new issue was refused: %v", err)
	}
	capped("issued")

	if err := exec(`UPDATE users SET password_hash = 'h4', password_expires_at = NULL WHERE id = 'issued'`); err != nil {
		t.Fatalf("the holder's own password was refused: %v", err)
	}
	if left("issued") != nil {
		t.Fatal("the holder's own password still expires")
	}

	if err := exec(`UPDATE users SET password_expires_at = now() + interval '9 days' WHERE id = 'member'`); err != nil {
		t.Fatal(err)
	}
	capped("member")
}
