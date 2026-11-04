package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// A mailbox import is worked off in the background: rows are claimed under a
// lease, settled, and the import closes when nothing is left. Rows waiting on a
// sign-in close when that address is connected in the same workspace, and the
// duplicate check at connect time is workspace-wide.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<scratch>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveMailboxImport -v

type importFixture struct {
	org, other, owner, teammate uuid.UUID
}

func newImportFixture(t *testing.T, pool *pgxpool.Pool) *importFixture {
	t.Helper()
	requireSchemaVersion(t, pool, 205)
	ctx := context.Background()
	f := &importFixture{org: uuid.New(), other: uuid.New(), owner: uuid.New(), teammate: uuid.New()}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	tag := f.org.String()[:8]
	for i, u := range []uuid.UUID{f.owner, f.teammate} {
		exec(`INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Import', 'Live', $2, 'x')`,
			u, "import-"+tag+"-"+string(rune('a'+i))+"@fixture.invalid")
	}
	for i, org := range []uuid.UUID{f.org, f.other} {
		exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Import', $2, $3)`,
			org, "import-"+tag+"-"+string(rune('a'+i)), f.owner)
	}
	t.Cleanup(func() {
		c := context.Background()
		orgs := []uuid.UUID{f.org, f.other}
		for _, q := range []string{
			`DELETE FROM mailbox_imports WHERE organization_id = ANY($1)`,
			`DELETE FROM mailbox_import_mappings WHERE organization_id = ANY($1)`,
			`DELETE FROM email_accounts WHERE organization_id = ANY($1)`,
			`DELETE FROM organizations WHERE id = ANY($1)`,
		} {
			if _, err := pool.Exec(c, q, orgs); err != nil {
				t.Errorf("cleanup %q: %v", q, err)
			}
		}
		if _, err := pool.Exec(c, `DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{f.owner, f.teammate}); err != nil {
			t.Errorf("cleanup users: %v", err)
		}
	})
	return f
}

func (f *importFixture) mailbox(t *testing.T, pool *pgxpool.Pool, org, user uuid.UUID, email string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider)
		 VALUES ($1, $2, $3, $4, 'Box', '', '', 'smtp_imap')`, id, user, org, email); err != nil {
		t.Fatalf("insert mailbox: %v", err)
	}
	return id
}

func (f *importFixture) newImport(t *testing.T, repo MailboxImportRepository, org uuid.UUID, emails ...string) uuid.UUID {
	t.Helper()
	imp := &models.MailboxImport{ID: uuid.New(), OrganizationID: org, CreatedBy: &f.owner, Status: models.ImportRunning,
		Source: "file", Filename: "boxes.csv", OnExisting: "update", Total: len(emails)}
	rows := make([]MailboxImportRowInsert, len(emails))
	for i, e := range emails {
		rows[i] = MailboxImportRowInsert{Line: i + 2, Email: e, Domain: "acme.io", MailHost: "google_workspace",
			Status: models.ImportRowQueued, Payload: "sealed", Fields: map[string]string{"email": e}}
	}
	if err := repo.Create(context.Background(), imp, []byte(`{}`), []byte(`["email"]`), rows); err != nil {
		t.Fatalf("create import: %v", err)
	}
	return imp.ID
}

func TestLiveMailboxImportLifecycle(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newImportFixture(t, pool)
	repo := NewMailboxImportRepository(handle)
	ctx := context.Background()

	id := f.newImport(t, repo, f.org, "a@acme.io", "b@acme.io", "c@acme.io")

	t.Run("another workspace cannot read it", func(t *testing.T) {
		got, err := repo.Get(ctx, f.other, id)
		if err != nil || got != nil {
			t.Fatalf("got %+v, %v; want nothing", got, err)
		}
	})

	t.Run("rows are claimed once under a lease", func(t *testing.T) {
		claimed, err := repo.Claim(ctx, 10, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		mine := 0
		for _, w := range claimed {
			if w.ImportID == id {
				mine++
				if w.OrgID != f.org || w.Attempts != 1 || w.Payload != "sealed" {
					t.Fatalf("claimed row = %+v", w)
				}
			}
		}
		if mine != 3 {
			t.Fatalf("claimed %d rows of this import, want 3", mine)
		}
		again, err := repo.Claim(ctx, 10, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range again {
			if w.ImportID == id {
				t.Fatalf("row %d handed out twice under a live lease", w.Line)
			}
		}
	})

	t.Run("settled rows close the import with counts and causes", func(t *testing.T) {
		acc := f.mailbox(t, pool, f.org, f.owner, "a@acme.io")
		// A replica whose claim lapsed cannot record over the current one.
		mustImport(t, repo.FinishRow(ctx, id, 2, 7, models.ImportRowSkipped, "", "", "late", nil, false))
		if row, _, _ := repo.GetRow(ctx, f.org, id, 2); row.Status != models.ImportRowRunning {
			t.Fatalf("a stale claim recorded %s", row.Status)
		}
		if ok, _ := repo.Touch(ctx, id, 2, 7, time.Minute); ok {
			t.Fatal("a stale claim renewed the lease")
		}
		if ok, _ := repo.Touch(ctx, id, 2, 1, time.Minute); !ok {
			t.Fatal("the current claim could not renew its lease")
		}
		mustImport(t, repo.FinishRow(ctx, id, 2, 1, models.ImportRowConnected, "", "", "", &acc, false))
		mustImport(t, repo.FinishRow(ctx, id, 3, 1, models.ImportRowFailed, "mailbox_auth_refused", "google_bad_credentials", "refused", nil, true))
		mustImport(t, repo.FinishRow(ctx, id, 4, 1, models.ImportRowNeedsSignin, "microsoft_signin", "microsoft_signin", "waiting", nil, true))

		done, err := repo.CompleteFinished(ctx, 7)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, d := range done {
			found = found || d.ID == id
		}
		if !found {
			t.Fatal("the import was not closed")
		}
		got, err := repo.Get(ctx, f.org, id)
		if err != nil || got == nil {
			t.Fatalf("get: %v", err)
		}
		if got.Status != models.ImportCompleted || got.CredentialsExpireAt == nil {
			t.Fatalf("status = %s expire = %v", got.Status, got.CredentialsExpireAt)
		}
		if got.Counts.Connected != 1 || got.Counts.Failed != 1 || got.Counts.NeedsSignin != 1 {
			t.Fatalf("counts = %+v", got.Counts)
		}
		if len(got.Causes) != 2 {
			t.Fatalf("causes = %+v", got.Causes)
		}
		for _, c := range got.Causes {
			if !c.Google {
				t.Fatalf("cause %s on Google rows is not marked google", c.Cause)
			}
		}
		rows, err := repo.ListRows(ctx, f.org, id, []string{models.ImportRowConnected}, "", 0, 10)
		if err != nil || len(rows) != 1 || rows[0].Retryable || rows[0].EmailAccountID == nil {
			t.Fatalf("connected rows = %+v, %v", rows, err)
		}
	})

	t.Run("a failed row is retried with a new payload", func(t *testing.T) {
		retry, err := repo.Retryable(ctx, f.org, id, "google_bad_credentials", nil)
		if err != nil || len(retry) != 1 || retry[0].Line != 3 {
			t.Fatalf("retryable = %+v, %v", retry, err)
		}
		if other, _ := repo.Retryable(ctx, f.other, id, "", nil); len(other) != 0 {
			t.Fatal("another workspace saw retryable rows")
		}
		if queued, err := repo.Requeue(ctx, id, 3, "resealed"); err != nil || !queued {
			t.Fatalf("requeue = %v, %v", queued, err)
		}
		mustImport(t, repo.Reopen(ctx, f.org, id))
		row, payload, err := repo.GetRow(ctx, f.org, id, 3)
		if err != nil || row.Status != models.ImportRowQueued || payload != "resealed" || row.Cause != "" {
			t.Fatalf("row = %+v payload = %q err = %v", row, payload, err)
		}
	})

	t.Run("a sign-in in the same workspace closes the waiting row", func(t *testing.T) {
		acc := f.mailbox(t, pool, f.org, f.teammate, "C@acme.io")
		pending, err := repo.PendingSignins(ctx, 1000)
		if err != nil {
			t.Fatal(err)
		}
		seen := false
		for _, p := range pending {
			seen = seen || (p.OrgID == f.org && p.AccountID == acc)
		}
		if !seen {
			t.Fatal("the connected address was not offered for reconciling")
		}
		if rows, err := repo.ResolveSignin(ctx, f.other, "c@acme.io", acc); err != nil || len(rows) != 0 {
			t.Fatalf("another workspace resolved %d rows (%v)", len(rows), err)
		}
		rows, err := repo.ResolveSignin(ctx, f.org, "c@acme.io", acc)
		if err != nil || len(rows) != 1 || rows[0].Payload != "sealed" {
			t.Fatalf("resolved = %+v, %v", rows, err)
		}
		row, payload, _ := repo.GetRow(ctx, f.org, id, 4)
		if row.Status != models.ImportRowConnected || payload != "" {
			t.Fatalf("row = %+v payload = %q", row, payload)
		}
	})

	t.Run("a row waiting for sign-in is requeued unless its vendor is authorizing it", func(t *testing.T) {
		park := func(cause string) {
			t.Helper()
			if _, err := pool.Exec(ctx, `UPDATE mailbox_import_rows SET status = 'needs_signin', cause = $3, payload = 'sealed'
				WHERE import_id = $1 AND line = $2`, id, 4, cause); err != nil {
				t.Fatal(err)
			}
		}
		park(ParkedVendorCause)
		if queued, err := repo.Requeue(ctx, id, 4, "resealed"); err != nil || queued {
			t.Fatalf("a vendor-parked row requeued = %v, %v", queued, err)
		}
		if row, payload, _ := repo.GetRow(ctx, f.org, id, 4); row.Status != models.ImportRowNeedsSignin || payload != "sealed" {
			t.Fatalf("row = %+v payload = %q", row, payload)
		}
		park("google_signin")
		if queued, err := repo.Requeue(ctx, id, 4, "resealed"); err != nil || !queued {
			t.Fatalf("a sign-in row requeued = %v, %v", queued, err)
		}
		if row, payload, _ := repo.GetRow(ctx, f.org, id, 4); row.Status != models.ImportRowQueued || payload != "resealed" || row.Cause != "" {
			t.Fatalf("row = %+v payload = %q", row, payload)
		}
	})

	t.Run("cancel stops what has not started and drops its credentials", func(t *testing.T) {
		mustImport(t, repo.Cancel(ctx, f.org, id))
		row, payload, _ := repo.GetRow(ctx, f.org, id, 3)
		if row.Status != models.ImportRowCancelled || payload != "" {
			t.Fatalf("row = %+v payload = %q", row, payload)
		}
		got, _ := repo.Get(ctx, f.org, id)
		if got.Status != models.ImportCancelled {
			t.Fatalf("status = %s", got.Status)
		}
	})

	t.Run("expired credentials are purged", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE mailbox_imports SET credentials_expire_at = now() - interval '1 minute' WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		mustImport(t, repo.PurgeExpired(ctx, 30))
		var left int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM mailbox_import_rows WHERE import_id = $1 AND payload <> ''`, id).Scan(&left); err != nil || left != 0 {
			t.Fatalf("%d rows still hold credentials (%v)", left, err)
		}
	})

	t.Run("a confirmed mapping is saved per header set", func(t *testing.T) {
		m := models.MailboxImportMapping{"0": models.ImportFieldEmail, "1": models.ImportFieldAppPassword}
		mustImport(t, repo.SaveMapping(ctx, f.org, "sig", m))
		got, ok, err := repo.GetMapping(ctx, f.org, "sig")
		if err != nil || !ok || got["1"] != models.ImportFieldAppPassword {
			t.Fatalf("mapping = %v ok = %v err = %v", got, ok, err)
		}
		if _, ok, _ := repo.GetMapping(ctx, f.other, "sig"); ok {
			t.Fatal("another workspace read the mapping")
		}
	})
}

func TestLiveMailboxDuplicateIsWorkspaceWide(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newImportFixture(t, pool)
	repo := NewEmailRepostory(handle, nil).(*emailRepository)
	ctx := context.Background()

	f.mailbox(t, pool, f.org, f.teammate, "Shared@acme.io")

	t.Run("a teammate's mailbox is found case-insensitively", func(t *testing.T) {
		ref, xerr := repo.FindInOrganization(ctx, f.org, " shared@ACME.io ")
		if xerr != nil || ref == nil {
			t.Fatalf("ref = %v, %v", ref, xerr)
		}
		if ref, _ := repo.FindInOrganization(ctx, f.other, "shared@acme.io"); ref != nil {
			t.Fatal("another workspace's mailbox matched")
		}
		many, xerr := repo.FindManyInOrganization(ctx, f.org, []string{"SHARED@acme.io", "none@acme.io"})
		if xerr != nil || len(many) != 1 {
			t.Fatalf("many = %v, %v", many, xerr)
		}
	})

	t.Run("the insert transaction refuses the same address", func(t *testing.T) {
		tx, err := handle.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		xerr := ensureMailboxAbsentTx(ctx, tx, &f.org, "shared@acme.io")
		if xerr == nil || xerr.Message != errx.ErrEmailOnboardAlreadyExists.Message {
			t.Fatalf("got %v, want already exists", xerr)
		}
		if xerr := ensureMailboxAbsentTx(ctx, tx, &f.other, "shared@acme.io"); xerr != nil {
			t.Fatalf("another workspace was refused: %v", xerr)
		}
	})

	t.Run("an unclassified domain is classified", func(t *testing.T) {
		domains, xerr := repo.ListUnclassifiedDomains(ctx, 10000)
		if xerr != nil {
			t.Fatal(xerr)
		}
		found := false
		for _, d := range domains {
			found = found || d == "acme.io"
		}
		if !found {
			t.Fatalf("acme.io not listed in %v", domains)
		}
		if xerr := repo.SetDomainMailHost(ctx, "ACME.io", "google_workspace", models.MailAuthAppPassword); xerr != nil {
			t.Fatal(xerr)
		}
		var host, auth string
		if err := pool.QueryRow(ctx, `SELECT mail_host, auth_method FROM email_accounts WHERE organization_id = $1`, f.org).Scan(&host, &auth); err != nil {
			t.Fatal(err)
		}
		if host != "google_workspace" || auth != models.MailAuthAppPassword {
			t.Fatalf("host = %q auth = %q", host, auth)
		}
	})
}

func mustImport(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
