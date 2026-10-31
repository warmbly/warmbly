package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// An identity comes off an account only while another way to sign in remains.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveIdentityUnlink -v
func TestLiveIdentityUnlinkKeepsAWayIn(t *testing.T) {
	handle := liveFolderDB(t)
	pool := handle.Pool
	ctx := context.Background()
	repo := NewIdentityRepository(pool)

	user := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, first_name, last_name, email)
		VALUES ($1, 'Unlink', 'Live', $2)`, user, "unlink-"+user.String()[:8]+"@test.local"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM user_identities WHERE user_id = $1`, user)
		_, _ = pool.Exec(context.Background(), `DELETE FROM webauthn_credentials WHERE user_id = $1`, user)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user)
	})
	link := func(provider, issuer string) {
		t.Helper()
		if err := repo.Link(ctx, user, models.UserIdentity{Provider: provider, Issuer: issuer, Subject: uuid.NewString()}); err != nil {
			t.Fatal(err)
		}
	}
	link(models.IdentityProviderApple, "https://appleid.apple.com")
	link(models.IdentityProviderGoogle, "https://accounts.google.com")

	if err := repo.Unlink(ctx, user, models.IdentityProviderGoogle); err != nil {
		t.Fatalf("unlink with Apple remaining: %v", err)
	}
	if err := repo.Unlink(ctx, user, models.IdentityProviderGoogle); !errors.Is(err, ErrIdentityNotLinked) {
		t.Fatalf("second unlink: %v, want ErrIdentityNotLinked", err)
	}
	if err := repo.Unlink(ctx, user, models.IdentityProviderApple); !errors.Is(err, ErrLastSignInMethod) {
		t.Fatalf("unlink of the only method: %v, want ErrLastSignInMethod", err)
	}

	// A passkey is a way in too, and the last one cannot go either.
	passkeys := NewWebAuthnRepository(handle)
	passkey := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO webauthn_credentials (id, user_id, credential_id, public_key)
		VALUES ($1, $2, $3, $4)`, passkey, user, passkey[:], []byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Unlink(ctx, user, models.IdentityProviderApple); err != nil {
		t.Fatalf("unlink with a passkey remaining: %v", err)
	}
	if xerr := passkeys.Delete(ctx, user, passkey); xerr != errx.ErrLastSignInMethod {
		t.Fatalf("delete of the only passkey: %v, want ErrLastSignInMethod", xerr)
	}
	link(models.IdentityProviderApple, "https://appleid.apple.com")
	if xerr := passkeys.Delete(ctx, user, passkey); xerr != nil {
		t.Fatalf("delete with Apple remaining: %v", xerr)
	}

	if _, err := pool.Exec(ctx, `UPDATE users SET password_hash = 'x' WHERE id = $1`, user); err != nil {
		t.Fatal(err)
	}
	if err := repo.Unlink(ctx, user, models.IdentityProviderApple); err != nil {
		t.Fatalf("unlink with a password set: %v", err)
	}
}
