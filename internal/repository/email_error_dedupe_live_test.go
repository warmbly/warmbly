package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Issue #405: a self-hosted instance whose IMAP server refused the same
// command every pass ended up with an error list holding one identical row per
// minute. The insert is conditional in SQL, so this proves it against the real
// table and the partial index it leans on.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveEmailErrorDedupe -v

func TestLiveEmailErrorDedupeKeepsOneUnresolvedRowPerCode(t *testing.T) {
	handle, pool := liveContactDB(t)
	repo := NewEmailAccountErrorRepository(handle)
	ctx := context.Background()

	account, user, org := uuid.New(), uuid.New(), uuid.New()
	tag := "i405-" + org.String()[:8]
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash)
	      VALUES ($1, 'Ilse', 'Live', $2, 'x')`, user, tag+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id)
	      VALUES ($1, 'Issue 405', $2, $3)`, org, tag, user)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider)
	      VALUES ($1, $2, $3, $4, 'Ilse', '', '', 'smtp_imap')`,
		account, user, org, tag+"-mb@test.local")

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM email_account_errors WHERE email_account_id = $1`, account},
			{`DELETE FROM email_accounts WHERE organization_id = $1`, org},
			{`DELETE FROM organizations WHERE id = $1`, org},
			{`DELETE FROM users WHERE id = $1`, user},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})

	row := func(code string) *CreateEmailAccountError {
		return &CreateEmailAccountError{
			EmailAccountID: account,
			UserID:         user,
			ErrorCode:      code,
			Severity:       "WARNING",
			ResolveMethod:  "RETRY",
			Title:          "Email Error",
			Message:        "Something went wrong: NO System Error",
		}
	}

	unresolved := func(code string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM email_account_errors
			 WHERE email_account_id = $1 AND error_code = $2 AND resolved_at IS NULL`,
			account, code).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	first, xerr := repo.CreateOnce(ctx, row("IMAP_UNKNOWN"))
	if xerr != nil {
		t.Fatalf("CreateOnce: %v", xerr.Message)
	}
	if first == nil {
		t.Fatal("the first occurrence of an error must be recorded")
	}
	exec(`UPDATE email_account_errors SET created_at=NOW()-interval '3 days' WHERE id=$1`, first.ID)
	before := time.Now()

	// The sync loop relaying the same refusal for the next hour.
	for i := 0; i < 5; i++ {
		again, xerr := repo.CreateOnce(ctx, row("IMAP_UNKNOWN"))
		if xerr != nil {
			t.Fatalf("CreateOnce repeat: %v", xerr.Message)
		}
		if again != nil {
			t.Fatalf("repeat %d wrote a second unresolved row for the same code", i+1)
		}
	}
	if n := unresolved("IMAP_UNKNOWN"); n != 1 {
		t.Errorf("unresolved IMAP_UNKNOWN rows = %d, want 1", n)
	}
	rows, xerr := repo.GetByAccountID(ctx, account, true)
	if xerr != nil || len(rows) != 1 || rows[0].CreatedAt.Before(before) {
		t.Fatalf("ongoing failure observation expired: %+v %v", rows, xerr)
	}
	if xerr := repo.ResolveByCodesBefore(ctx, account, []string{"IMAP_UNKNOWN"}, before, "stale success"); xerr != nil {
		t.Fatal(xerr)
	}
	if n := unresolved("IMAP_UNKNOWN"); n != 1 {
		t.Fatal("old success cleared a newly observed failure")
	}

	// A different failure is a different row: dedupe must not swallow it.
	other, xerr := repo.CreateOnce(ctx, row("SERVER_UNREACHABLE"))
	if xerr != nil {
		t.Fatalf("CreateOnce other code: %v", xerr.Message)
	}
	if other == nil {
		t.Fatal("a different error code must still be recorded")
	}

	// Once the mailbox recovers and the row is resolved, the next occurrence
	// is news again, otherwise a mailbox that breaks twice reports once.
	if xerr := repo.Resolve(ctx, first.ID, "test"); xerr != nil {
		t.Fatalf("Resolve: %v", xerr.Message)
	}
	rearmed, xerr := repo.CreateOnce(ctx, row("IMAP_UNKNOWN"))
	if xerr != nil {
		t.Fatalf("CreateOnce after resolve: %v", xerr.Message)
	}
	if rearmed == nil {
		t.Error("an error recurring after it was resolved must be recorded again")
	}
}

// CodeRabbit's catch on #406: a conditional insert is not atomic on its own.
// Two workers relaying the same failure in the same instant both find no row
// and both write one. Migration 000145 makes that impossible and ON CONFLICT
// turns the loser into the ordinary "already on screen" answer instead of a
// logged storage failure.
func TestLiveEmailErrorDedupeSurvivesConcurrentRelays(t *testing.T) {
	handle, pool := liveContactDB(t)
	repo := NewEmailAccountErrorRepository(handle)
	ctx := context.Background()

	account, user, org := uuid.New(), uuid.New(), uuid.New()
	tag := "i405c-" + org.String()[:8]
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash)
	      VALUES ($1, 'Ilse', 'Live', $2, 'x')`, user, tag+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id)
	      VALUES ($1, 'Issue 405', $2, $3)`, org, tag, user)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider)
	      VALUES ($1, $2, $3, $4, 'Ilse', '', '', 'smtp_imap')`,
		account, user, org, tag+"-mb@test.local")
	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM email_account_errors WHERE email_account_id = $1`, account},
			{`DELETE FROM email_accounts WHERE organization_id = $1`, org},
			{`DELETE FROM organizations WHERE id = $1`, org},
			{`DELETE FROM users WHERE id = $1`, user},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})

	const racers = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		written int
	)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			row, xerr := repo.CreateOnce(ctx, &CreateEmailAccountError{
				EmailAccountID: account,
				UserID:         user,
				ErrorCode:      "IMAP_UNKNOWN",
				Severity:       "WARNING",
				ResolveMethod:  "RETRY",
				Title:          "Email Error",
				Message:        "Something went wrong: NO System Error",
			})
			mu.Lock()
			defer mu.Unlock()
			if xerr != nil {
				t.Errorf("a losing racer must not surface a storage failure: %v", xerr.Message)
				return
			}
			if row != nil {
				written++
			}
		}()
	}
	close(start)
	wg.Wait()

	if written != 1 {
		t.Errorf("%d of %d racers believed they wrote the row, want exactly 1", written, racers)
	}
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM email_account_errors
		 WHERE email_account_id = $1 AND resolved_at IS NULL`, account).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("unresolved rows = %d after %d concurrent relays, want 1", n, racers)
	}
}
