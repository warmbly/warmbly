package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
)

func TestLivePasswordCredentials(t *testing.T) {
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	ctx := context.Background()
	handle, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(handle.Close)
	repo := NewAuthRepostory(handle)
	email := "auth-" + uuid.NewString() + "@fixture.invalid"
	user, xerr := repo.ExternalLogin(ctx, email)
	if xerr != nil {
		t.Fatal(xerr)
	}
	t.Cleanup(func() {
		if _, err := handle.Exec(ctx, `DELETE FROM users WHERE id = $1`, user.ID); err != nil {
			t.Error(err)
		}
	})

	assertRejected := func(t *testing.T, email, password string) {
		t.Helper()
		id, xerr := repo.IsValidCredentials(ctx, email, password)
		if id != uuid.Nil || xerr != errx.ErrCredentials {
			t.Fatalf("credentials = (%v, %v), want nil ID and invalid credentials", id, xerr)
		}
	}

	t.Run("external account has no password", func(t *testing.T) {
		assertRejected(t, email, "")
		assertRejected(t, email, "arbitrary password")
	})

	t.Run("nullable password is also passwordless", func(t *testing.T) {
		if _, err := handle.Exec(ctx, `UPDATE users SET password_hash = NULL WHERE id = $1`, user.ID); err != nil {
			t.Fatal(err)
		}
		assertRejected(t, email, "")
		assertRejected(t, email, "arbitrary password")
	})

	t.Run("unknown account", func(t *testing.T) {
		assertRejected(t, "missing-"+email, "arbitrary password")
	})

	t.Run("malformed nonempty hash cannot be verified", func(t *testing.T) {
		if xerr := repo.ResetPassword(ctx, user.ID, "malformed"); xerr != nil {
			t.Fatal(xerr)
		}
		// The unparseable hash is reported to error tracking; the caller gets the uniform refusal.
		assertRejected(t, email, "arbitrary password")
	})

	t.Run("a database fault is an internal error, not a refusal", func(t *testing.T) {
		failed, cancel := context.WithCancel(ctx)
		cancel()
		id, xerr := repo.IsValidCredentials(failed, email, "arbitrary password")
		if id != uuid.Nil || xerr == nil || xerr.Code != errx.InternalError().Code {
			t.Fatalf("credentials = (%v, %v), want nil ID and internal error", id, xerr)
		}
	})

	t.Run("setting a password enables only matching credentials", func(t *testing.T) {
		const password = "CorrectPassword123!"
		hash, err := argon2.Hash(password)
		if err != nil {
			t.Fatal(err)
		}
		if xerr := repo.ResetPassword(ctx, user.ID, hash); xerr != nil {
			t.Fatal(xerr)
		}
		id, xerr := repo.IsValidCredentials(ctx, email, password)
		if xerr != nil || id != user.ID {
			t.Fatalf("credentials = (%v, %v), want %v and no error", id, xerr, user.ID)
		}
		assertRejected(t, email, "wrong password")
		assertRejected(t, email, "")
		if _, xerr := repo.ExternalLogin(ctx, email); xerr != nil {
			t.Fatal(xerr)
		}
		id, xerr = repo.IsValidCredentials(ctx, email, password)
		if xerr != nil || id != user.ID {
			t.Fatalf("external sign-in changed password credentials: (%v, %v)", id, xerr)
		}
	})
}
