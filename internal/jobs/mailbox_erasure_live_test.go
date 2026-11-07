package jobs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/pkg/oauthrevoke"
	"github.com/warmbly/warmbly/internal/repository"
)

// End-to-end cover for erasure: a real mailbox row, a real body in a real blob
// store, a real delete, and the real job. Skipped unless WARMBLY_TEST_DB is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/jobs/ -run Live -v
//
// The unit tests above stub the repository and the store, so they can show the
// job's decisions but not that the pieces fit: that the delete writes a row the
// job can claim, that the prefix it records is the one the bodies were written
// under, and that the row goes when the work is done. Each of those is a place
// this could be wrong while every unit test passed.

func TestLiveErasureRemovesTheStoredMailAndTheQueueRow(t *testing.T) {
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
	user, org, mailbox := uuid.New(), uuid.New(), uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := handle.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql, err)
		}
	}
	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Erase', 'Test')`,
		user, "erase-"+user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Erase Test', $2, $3)`,
		org, "erase-"+org.String()[:8], user)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time)
	      VALUES ($1, $2, $3, $4, 'Erase', '', '', 'gmail', 'active', 50, 600)`,
		mailbox, user, org, "erase-"+mailbox.String()[:8]+"@test.local")
	exec(`INSERT INTO email_accounts_oauth (email_account_id, access_token, refresh_token, expires_at)
	      VALUES ($1, 'sealed-access', 'plaintext-refresh', now() + interval '1 hour')`, mailbox)
	t.Cleanup(func() {
		c := context.Background()
		for _, q := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM mailbox_erasures WHERE email_account_id = $1`, mailbox},
			{`DELETE FROM email_accounts WHERE id = $1`, mailbox},
			{`DELETE FROM organizations WHERE id = $1`, org},
			{`DELETE FROM users WHERE id = $1`, user},
		} {
			if _, err := handle.Pool.Exec(c, q.sql, q.arg); err != nil {
				t.Errorf("cleanup %q: %v", q.sql, err)
			}
		}
	})

	// Three message bodies under the mailbox's prefix, written the way the
	// worker writes them, and one under a mailbox the owner keeps.
	root := t.TempDir()
	store, err := storage.NewFilesystem(root, "")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	neighbour := uuid.New()
	var mine []string
	for i := 0; i < 3; i++ {
		key := config.StorageEndpointEmailBody(user, mailbox, uuid.New())
		mine = append(mine, key)
		if err := store.Put(ctx, key, strings.NewReader("a message"), ""); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	kept := config.StorageEndpointEmailBody(user, neighbour, uuid.New())
	if err := store.Put(ctx, kept, strings.NewReader("another mailbox"), ""); err != nil {
		t.Fatalf("put: %v", err)
	}

	// A provider that accepts the revocation, and records what reached it.
	var revokedWith string
	serveRevocations(t, http.StatusOK, &revokedWith)

	// The delete itself, through the repository the API uses.
	emails := repository.NewEmailRepostory(handle, nil)
	if xerr := emails.Delete(ctx, org.String(), mailbox.String(), 1); xerr != nil {
		t.Fatalf("delete mailbox: %v", xerr)
	}

	// The job, wired to the real queue and the real store. No encrypter, which
	// is the shape of an instance with no CREDENTIALS_ENCRYPTION_KEY: the
	// stored value is sent as-is, which is what this fixture wrote.
	erasures := repository.NewMailboxErasureRepository(handle)
	if err := NewMailboxErasureJob(erasures, store, nil).Run(ctx); err != nil {
		t.Fatalf("erasure run: %v", err)
	}

	if revokedWith != "plaintext-refresh" {
		t.Errorf("provider received %q, want the mailbox's refresh token", revokedWith)
	}
	for _, key := range mine {
		if has, _ := store.Has(ctx, key); has {
			t.Errorf("%s is still in the store after the mailbox was erased", key)
		}
	}
	if has, _ := store.Has(ctx, kept); !has {
		t.Error("a different mailbox's stored mail went with this erasure")
	}
	if _, err := os.Stat(filepath.Join(root, "users", user.String(), "emails", mailbox.String())); !os.IsNotExist(err) {
		t.Error("the mailbox's directory is still there")
	}

	var left int
	if err := handle.Pool.QueryRow(ctx,
		`SELECT count(*) FROM mailbox_erasures WHERE email_account_id = $1`, mailbox).Scan(&left); err != nil {
		t.Fatalf("read queue: %v", err)
	}
	if left != 0 {
		t.Errorf("the queue still holds %d rows naming an erased mailbox", left)
	}

	// A second pass must find this mailbox already done. A job that failed to
	// clear its row would otherwise keep revoking and re-walking it forever,
	// which no stubbed test can see. Scoped to this mailbox rather than to the
	// queue as a whole, because the database is shared and someone else's
	// pending erasure is not this test's business to claim or to fail on.
	revokedWith = ""
	if err := NewMailboxErasureJob(erasures, store, nil).Run(ctx); err != nil {
		t.Fatalf("second erasure run: %v", err)
	}
	if revokedWith == "plaintext-refresh" {
		t.Error("the grant was revoked a second time, so the queue row outlived the work")
	}
}

// serveRevocations points the revoker at a stub for one test and records the
// token that reached it, so the test can tell a real revocation apart from one
// that sent ciphertext or nothing at all.
func serveRevocations(t *testing.T, status int, got *string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		*got = r.Form.Get("token")
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	prev := oauthrevoke.GoogleRevokeURL
	oauthrevoke.GoogleRevokeURL = srv.URL
	t.Cleanup(func() { oauthrevoke.GoogleRevokeURL = prev })
}
