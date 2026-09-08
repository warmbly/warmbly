package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// The stamp stores "<id@host>" but an inbound In-Reply-To arrives either way,
// and cleanMessageID strips the brackets before the lookup. Under the old exact
// compare the bare-form case found nothing, so replied_at was never stamped and
// stop_on_reply kept mailing a contact who had already answered.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveTaskByMessageID -v
func TestLiveTaskByMessageIDMatchesBracketedAndBareForms(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()

	org := uuid.New()
	user := uuid.New()
	account := uuid.New()
	task := uuid.New()
	bare := "regression-" + uuid.NewString() + "@example.test"
	stored := "<" + bare + ">"

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("setup %q: %v", sql, err)
		}
	}

	// The user exists first: organizations.owner_user_id points at it.
	exec(`INSERT INTO users (id, first_name, last_name, email)
	      VALUES ($1, 'MsgID', 'Regression', $2)`,
		user, "msgid-"+uuid.NewString()+"@example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id)
	      VALUES ($1, 'msgid regression', $2)`, org, user)
	exec(`INSERT INTO email_accounts
	        (id, user_id, organization_id, email, name, signature_plain, signature_html, provider)
	      VALUES ($1, $2, $3, $4, 'MsgID Regression', '', '', 'smtp_imap')`,
		account, user, org, "box-"+uuid.NewString()+"@example.test")
	exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id)
	      VALUES ($1, 'campaign', $2, 'completed', $3)`, task, account, stored)

	t.Cleanup(func() {
		// Unwound in dependency order: tasks cascade from the mailbox, the
		// mailbox and the org do not cascade from each other, and the org
		// owns the user by foreign key, so the user goes last.
		for _, stmt := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM email_accounts WHERE id = $1`, account},
			{`DELETE FROM organizations WHERE id = $1`, org},
			{`DELETE FROM users WHERE id = $1`, user},
		} {
			if _, err := pool.Exec(context.Background(), stmt.sql, stmt.arg); err != nil {
				t.Errorf("cleanup %q: %v", stmt.sql, err)
			}
		}
	})

	repo := NewTaskRepository(pool)

	for _, tc := range []struct {
		name   string
		lookup string
	}{
		{"bare form, as cleanMessageID hands it over", bare},
		{"bracketed form, as the header carried it", stored},
		{"bracketed with surrounding space", "  " + stored + "  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repo.GetTaskByMessageID(ctx, tc.lookup)
			if err != nil {
				t.Fatalf("lookup %q: %v", tc.lookup, err)
			}
			if got == nil {
				t.Fatalf("lookup %q found no task; the reply would never link to its campaign", tc.lookup)
			}
			if got.ID != task {
				t.Fatalf("lookup %q returned task %s, want %s", tc.lookup, got.ID, task)
			}
		})
	}

	t.Run("empty message id is not a wildcard", func(t *testing.T) {
		for _, empty := range []string{"", "   ", "<>"} {
			got, err := repo.GetTaskByMessageID(ctx, empty)
			if err != nil {
				t.Fatalf("lookup %q: %v", empty, err)
			}
			if got != nil {
				t.Fatalf("lookup %q returned task %s; an absent header must match nothing", empty, got.ID)
			}
		}
	})
}
