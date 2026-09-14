package email

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Live cover for the send-identity round trip. Skipped unless WARMBLY_TEST_DB
// is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/app/email/ -run Live -v
//
// What a stub cannot see: the send-as list is jsonb and the stale-alias clear
// is a CASE in the same UPDATE, so both only really exist in SQL.
type identityLiveFixture struct {
	pool    *pgxpool.Pool
	repo    repository.EmailRepository
	user    uuid.UUID
	org     uuid.UUID
	mailbox uuid.UUID
}

func newIdentityLiveFixture(t *testing.T) *identityLiveFixture {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	handle, err := db.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { handle.Pool.Close() })

	ctx := context.Background()
	f := &identityLiveFixture{pool: handle.Pool, user: uuid.New(), org: uuid.New(), mailbox: uuid.New()}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Ident', 'Test')`,
		f.user, "ident-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Ident Test', $2, $3)`,
		f.org, "ident-"+f.org.String()[:8], f.user)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time)
	      VALUES ($1, $2, $3, $4, 'Ident', 'bye', '<p>bye</p>', 'gmail', 'active', 50, 600)`,
		f.mailbox, f.user, f.org, "ident-"+f.mailbox.String()[:8]+"@test.local")

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM email_accounts WHERE id = $1`, f.mailbox},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := f.pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})

	f.repo = repository.NewEmailRepostory(handle, nil)
	return f
}

func TestLiveSendIdentityRoundTrip(t *testing.T) {
	f := newIdentityLiveFixture(t)
	ctx := context.Background()

	ids := []models.SendAsIdentity{
		{Email: "primary@test.local", Name: "Primary", IsPrimary: true, IsDefault: true, Verified: true},
		{Email: "hello@test.local", Name: "Hello", Verified: true},
		{Email: "pending@test.local", Verified: false},
	}
	if xerr := f.repo.SetSendIdentity(ctx, f.mailbox, ids, nil); xerr != nil {
		t.Fatalf("store: %v", xerr)
	}

	got, xerr := f.repo.GetSendIdentity(ctx, f.org.String(), f.mailbox.String())
	if xerr != nil {
		t.Fatalf("read: %v", xerr)
	}
	if !got.Supported {
		t.Error("a gmail mailbox must report the send-as list as supported")
	}
	if len(got.Identities) != 3 || got.Identities[1].Email != "hello@test.local" {
		t.Fatalf("identities did not round-trip: %+v", got.Identities)
	}
	if got.SyncedAt == nil {
		t.Error("synced_at was not stamped")
	}
	if got.SignatureSource != models.SignatureSourceManual {
		t.Errorf("a list-only refresh must not touch the signature source: %q", got.SignatureSource)
	}

	// Choosing a verified alias, then a refresh that no longer lists it: the
	// mailbox has to fall back to its own address rather than keep sending as
	// an address the provider will now refuse.
	alias := "hello@test.local"
	if _, xerr := f.repo.Update(ctx, f.org.String(), f.mailbox.String(), &models.UpdateEmail{SendAsEmail: &alias}); xerr != nil {
		t.Fatalf("choose alias: %v", xerr)
	}
	if got, _ = f.repo.GetSendIdentity(ctx, f.org.String(), f.mailbox.String()); got.SendAsEmail != alias {
		t.Fatalf("alias was not stored: %q", got.SendAsEmail)
	}

	if xerr := f.repo.SetSendIdentity(ctx, f.mailbox, ids[:1], &models.ImportedSignature{HTML: "<p>Hi</p>", Plain: "Hi"}); xerr != nil {
		t.Fatalf("second store: %v", xerr)
	}
	got, xerr = f.repo.GetSendIdentity(ctx, f.org.String(), f.mailbox.String())
	if xerr != nil {
		t.Fatalf("read back: %v", xerr)
	}
	if got.SendAsEmail != "" {
		t.Errorf("an alias the provider stopped listing must be cleared, got %q", got.SendAsEmail)
	}
	if got.SignatureSource != models.SignatureSourceProvider || got.SignatureImportedAt == nil {
		t.Errorf("the imported signature was not recorded: source=%q at=%v", got.SignatureSource, got.SignatureImportedAt)
	}

	acc, xerr := f.repo.GetByID(ctx, f.mailbox)
	if xerr != nil {
		t.Fatalf("get mailbox: %v", xerr)
	}
	if acc.SignatureHTML != "<p>Hi</p>" || acc.SignaturePlain != "Hi" {
		t.Errorf("signature was not imported: %q / %q", acc.SignatureHTML, acc.SignaturePlain)
	}
	if acc.SendFrom() != acc.Email {
		t.Errorf("a cleared alias must send from the mailbox address, got %q", acc.SendFrom())
	}
}
