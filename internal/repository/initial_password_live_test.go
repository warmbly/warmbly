package repository

import (
	"context"
	"errors"
	"net/mail"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
)

// An issued password is stored capped, replaced only over itself, and a
// revoked tester loses the password it chose as well.
func TestLiveInitialPasswordLifecycle(t *testing.T) {
	db, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 336)
	ctx := context.Background()
	users := NewUserRepostory(db, nil)
	auth := NewAuthRepostory(db)

	u, err := users.CreateExemptUser(ctx, &mail.Address{Address: uuid.NewString() + "@example.test"}, "issued-hash", "review", nil, time.Now().Add(30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM admin_audit_logs WHERE target_id = $1`, u.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, u.ID)
	})
	if u.PasswordExpiresAt == nil || time.Until(*u.PasswordExpiresAt) > 48*time.Hour {
		t.Fatalf("issued password stored with expiry %v", u.PasswordExpiresAt)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET password_expires_at = password_expires_at + interval '1 hour' WHERE id = $1`, u.ID); err == nil {
		t.Fatal("an issued password was extended")
	}

	if pending, xerr := auth.InitialPasswordPending(ctx, u.ID); xerr != nil || !pending {
		t.Fatalf("pending = %v, %v", pending, xerr)
	}
	if ok, xerr := auth.ReplaceInitialPassword(ctx, u.ID, "another-hash", "chosen-hash"); xerr != nil || ok {
		t.Fatalf("replaced over a hash that is not stored: %v, %v", ok, xerr)
	}
	if ok, xerr := auth.ReplaceInitialPassword(ctx, u.ID, "issued-hash", "chosen-hash"); xerr != nil || !ok {
		t.Fatalf("replace = %v, %v", ok, xerr)
	}
	if pending, xerr := auth.InitialPasswordPending(ctx, u.ID); xerr != nil || pending {
		t.Fatalf("still pending after the change: %v, %v", pending, xerr)
	}
	if hash, xerr := auth.GetPasswordHash(ctx, u.ID); xerr != nil || hash != "chosen-hash" {
		t.Fatalf("hash = %q, %v", hash, xerr)
	}
	if ok, xerr := auth.ReplaceInitialPassword(ctx, u.ID, "chosen-hash", "third-hash"); xerr != nil || ok {
		t.Fatal("a chosen password was replaced as if it were issued")
	}

	if _, err := pool.Exec(ctx, `INSERT INTO admin_audit_logs (admin_user_id, action, target_type, target_id, details)
		VALUES ($1, 'create_tester', 'user', $1, '{}'::jsonb)`, u.ID); err != nil {
		t.Fatal(err)
	}
	cleared, err := users.RevokeTester(ctx, u.ID)
	if err != nil || !cleared {
		t.Fatalf("revoke cleared = %v, %v", cleared, err)
	}
	if hash, xerr := auth.GetPasswordHash(ctx, u.ID); xerr != nil || hash != "" {
		t.Fatalf("a revoked tester kept its chosen password: %q, %v", hash, xerr)
	}
}

// A chosen password written by any path never expires.
func TestLiveResetPasswordClearsAnIssuedExpiry(t *testing.T) {
	db, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 336)
	ctx := context.Background()
	users := NewUserRepostory(db, nil)
	auth := NewAuthRepostory(db)
	u, err := users.CreateExemptUser(ctx, &mail.Address{Address: uuid.NewString() + "@example.test"}, "issued-hash", "review", nil, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, u.ID) })
	if xerr := auth.ResetPassword(ctx, u.ID, "chosen-hash"); xerr != nil {
		t.Fatal(xerr)
	}
	if pending, xerr := auth.InitialPasswordPending(ctx, u.ID); xerr != nil || pending {
		t.Fatalf("pending after a reset: %v, %v", pending, xerr)
	}
	var expires *time.Time
	if err := pool.QueryRow(ctx, `SELECT password_expires_at FROM users WHERE id = $1`, u.ID).Scan(&expires); err != nil || expires != nil {
		t.Fatalf("expiry after a reset: %v, %v", expires, err)
	}
}

// A re-issue replaces whatever password a tester holds with a fresh 48-hour
// initial one, and only on an account created as a tester.
func TestLiveReissueTesterPassword(t *testing.T) {
	db, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 336)
	ctx := context.Background()
	users := NewUserRepostory(db, nil)
	auth := NewAuthRepostory(db)
	const old, fresh = "old-issued-passphrase-1180", "fresh-issued-passphrase-5521"
	oldHash, err := argon2.Hash(old)
	if err != nil {
		t.Fatal(err)
	}
	freshHash, err := argon2.Hash(fresh)
	if err != nil {
		t.Fatal(err)
	}
	address := uuid.NewString() + "@example.test"
	u, err := users.CreateExemptUser(ctx, &mail.Address{Address: address}, oldHash, "review", nil, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM admin_audit_logs WHERE target_id = $1`, u.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, u.ID)
	})

	if tester, err := users.IsCreatedTester(ctx, u.ID); err != nil || tester {
		t.Fatalf("an account with no create_tester row reads as a tester: %v, %v", tester, err)
	}
	if _, err := users.ReissueTesterPassword(ctx, u.ID, freshHash); !errors.Is(err, ErrNotTester) {
		t.Fatalf("re-issue on a non-tester answered %v", err)
	}
	if _, err := users.IsCreatedTester(ctx, uuid.New()); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("unknown account answered %v", err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO admin_audit_logs (admin_user_id, action, target_type, target_id, details)
		VALUES ($1, 'create_tester', 'user', $1, '{}'::jsonb)`, u.ID); err != nil {
		t.Fatal(err)
	}
	if tester, err := users.IsCreatedTester(ctx, u.ID); err != nil || !tester {
		t.Fatalf("tester = %v, %v", tester, err)
	}
	until, err := users.ReissueTesterPassword(ctx, u.ID, freshHash)
	if err != nil {
		t.Fatal(err)
	}
	if left := time.Until(until); left > 48*time.Hour || left < 47*time.Hour {
		t.Fatalf("re-issued password expires in %v", left)
	}
	if _, xerr := auth.IsValidCredentials(ctx, address, old); xerr == nil {
		t.Fatal("the old password still works")
	}
	if id, xerr := auth.IsValidCredentials(ctx, address, fresh); xerr != nil || id != u.ID {
		t.Fatalf("the new password answered %v, %v", id, xerr)
	}
	if pending, xerr := auth.InitialPasswordPending(ctx, u.ID); xerr != nil || !pending {
		t.Fatalf("the re-issued password is not an initial one: %v, %v", pending, xerr)
	}
}
